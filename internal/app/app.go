// Package app wires the telegram MCP server.
package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/bogachenko/telegram-mcp-server/internal/config"
	"github.com/bogachenko/telegram-mcp-server/internal/exclusions"
	"github.com/bogachenko/telegram-mcp-server/internal/mcp"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/storage"
	tgclient "github.com/bogachenko/telegram-mcp-server/internal/telegram"
)

// App is the composed application instance.
type App struct {
	config config.Config
	tools  []mcp.Tool
}

// New builds the application graph from config.
func New(cfg config.Config) *App {
	return &App{
		config: cfg,
		tools:  mcp.ListTools(),
	}
}

// Run executes a small CLI wrapper around the MCP server.
func Run(args []string, stdout io.Writer) error {
	return RunWithIO(args, os.Stdin, stdout)
}

// RunWithIO executes a small CLI wrapper around the MCP server with explicit IO.
func RunWithIO(args []string, stdin io.Reader, stdout io.Writer) error {
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}
	if stdin == nil {
		stdin = os.Stdin
	}

	cfg := config.LoadFromEnv()
	application := New(cfg)

	if len(args) == 0 {
		return application.PrintStatus(stdout)
	}

	switch args[0] {
	case "status":
		return application.PrintStatus(stdout)

	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		fs.SetOutput(stdout)
		listenAddr := fs.String("listen-addr", cfg.ListenAddr, "HTTP listen address")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		application.config.ListenAddr = *listenAddr
		return application.Serve(context.Background(), stdout)

	case "telegram-auth":
		return application.TelegramAuth(context.Background(), stdin, stdout)

	case "telegram-me":
		return application.TelegramMe(context.Background(), stdout)

	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// PrintStatus writes a non-secret startup summary.
func (a *App) PrintStatus(stdout io.Writer) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}

	_, err := fmt.Fprintf(
		stdout,
		"telegram-mcp-server configured\n"+
			"data dir: %s\n"+
			"database path: %s\n"+
			"telegram session dir: %s\n"+
			"telegram session path: %s\n"+
			"telegram api configured: %t\n"+
			"telegram phone configured: %t\n"+
			"listen addr: %s\n"+
			"public base URL: %s\n"+
			"mcp endpoint: /mcp\n"+
			"health endpoint: /healthz\n"+
			"mcp tools planned: %d\n",
		a.config.DataDir,
		a.config.DatabasePath,
		a.config.TelegramSessionDir,
		a.config.TelegramSessionPath,
		a.config.TelegramAPIID != 0 && a.config.TelegramAPIHash != "",
		a.config.TelegramPhone != "",
		a.config.ListenAddr,
		displayPublicBaseURL(a.config.PublicBaseURL),
		len(a.tools),
	)
	if err != nil {
		return fmt.Errorf("write status: %w", err)
	}

	return nil
}

// Serve starts the Streamable HTTP MCP server.
func (a *App) Serve(ctx context.Context, stdout io.Writer) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}

	db, err := storage.Open(ctx, a.config.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := storage.Migrate(ctx, db); err != nil {
		return err
	}

	sourceRepo := sources.NewRepository(db)
	messageRepo := messages.NewRepository(db)
	exclusionRepo := exclusions.NewRepository(db)
	exclusionService := exclusions.NewService(exclusionRepo, messageRepo)

	handler := mcp.NewHTTPHandler(mcp.ServerDeps{
		Sources:          sourceRepo,
		Messages:         messageRepo,
		Exclusions:       exclusionRepo,
		ExclusionService: exclusionService,
	})

	server := &http.Server{
		Addr:    a.config.ListenAddr,
		Handler: handler,
	}

	if _, err := fmt.Fprintf(stdout, "telegram-mcp-server listening on http://%s/mcp\n", a.config.ListenAddr); err != nil {
		return fmt.Errorf("write serve status: %w", err)
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve mcp http: %w", err)
	}

	return nil
}

// TelegramAuth starts interactive Telegram user-client authorization.
func (a *App) TelegramAuth(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	client := tgclient.NewClient(tgclient.Config{
		APIID:       a.config.TelegramAPIID,
		APIHash:     a.config.TelegramAPIHash,
		Phone:       a.config.TelegramPhone,
		Password:    a.config.TelegramPassword,
		SessionPath: a.config.TelegramSessionPath,
	})

	self, err := client.Auth(ctx, stdin, stdout)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"telegram authorized\n"+
			"id: %d\n"+
			"username: %s\n"+
			"name: %s\n"+
			"bot: %t\n",
		self.ID,
		self.Username,
		self.DisplayName(),
		self.Bot,
	)
	if err != nil {
		return fmt.Errorf("write telegram auth status: %w", err)
	}

	return nil
}

// TelegramMe prints the authorized Telegram user from the saved session.
func (a *App) TelegramMe(ctx context.Context, stdout io.Writer) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	client := tgclient.NewClient(tgclient.Config{
		APIID:       a.config.TelegramAPIID,
		APIHash:     a.config.TelegramAPIHash,
		SessionPath: a.config.TelegramSessionPath,
	})

	self, authorized, err := client.Me(ctx)
	if err != nil {
		return err
	}
	if !authorized {
		return fmt.Errorf("telegram session is not authorized; run telegram-auth first")
	}

	_, err = fmt.Fprintf(
		stdout,
		"telegram session authorized\n"+
			"id: %d\n"+
			"username: %s\n"+
			"name: %s\n"+
			"bot: %t\n",
		self.ID,
		self.Username,
		self.DisplayName(),
		self.Bot,
	)
	if err != nil {
		return fmt.Errorf("write telegram me status: %w", err)
	}

	return nil
}

func displayPublicBaseURL(value string) string {
	if value == "" {
		return "<auto>"
	}
	return value
}
