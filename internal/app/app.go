// Package app wires the telegram MCP server.
package app

import (
	"fmt"
	"io"

	"github.com/bogachenko/telegram-mcp-server/internal/config"
	"github.com/bogachenko/telegram-mcp-server/internal/mcp"
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
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}

	cfg := config.LoadFromEnv()
	application := New(cfg)

	if len(args) == 0 {
		return application.PrintStatus(stdout)
	}

	switch args[0] {
	case "status":
		return application.PrintStatus(stdout)
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
			"mcp tools planned: %d\n",
		a.config.DataDir,
		a.config.DatabasePath,
		a.config.TelegramSessionDir,
		len(a.tools),
	)
	if err != nil {
		return fmt.Errorf("write status: %w", err)
	}

	return nil
}
