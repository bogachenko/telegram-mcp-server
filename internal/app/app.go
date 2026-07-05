// Package app wires the telegram MCP server.
package app

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/bogachenko/telegram-mcp-server/internal/config"
	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/exclusions"
	"github.com/bogachenko/telegram-mcp-server/internal/mcp"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/state"
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

	case "source-add":
		fs := flag.NewFlagSet("source-add", flag.ContinueOnError)
		fs.SetOutput(stdout)
		id := fs.String("id", "", "stable local source id")
		sourceType := fs.String("type", string(domain.SourceTypeChannel), "source type: channel or group")
		entity := fs.String("entity", "", "Telegram username, t.me link, or resolvable entity reference")
		username := fs.String("username", "", "public username without @")
		title := fs.String("title", "", "human-readable title")
		disabled := fs.Bool("disabled", false, "save source as disabled")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return application.SourceAdd(context.Background(), stdout, domain.Source{
			ID:             strings.TrimSpace(*id),
			Type:           domain.SourceType(strings.TrimSpace(*sourceType)),
			EntityRef:      strings.TrimSpace(*entity),
			PublicUsername: strings.TrimPrefix(strings.TrimSpace(*username), "@"),
			Title:          strings.TrimSpace(*title),
			Enabled:        !*disabled,
		})

	case "source-remove":
		fs := flag.NewFlagSet("source-remove", flag.ContinueOnError)
		fs.SetOutput(stdout)
		id := fs.String("id", "", "source id")
		purge := fs.Bool("purge", false, "also delete source state, messages and source-scoped exclusions")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return application.SourceRemove(context.Background(), stdout, strings.TrimSpace(*id), *purge)

	case "source-list":
		return application.SourceList(context.Background(), stdout)

	case "messages-recent":
		fs := flag.NewFlagSet("messages-recent", flag.ContinueOnError)
		fs.SetOutput(stdout)
		limit := fs.Int("limit", 20, "maximum messages to print")
		sourceID := fs.String("source", "", "optional source id filter")
		sourceLabel := fs.String("label", "", "optional source label filter: POST or COMMENT")
		includeHidden := fs.Bool("include-hidden", false, "include messages hidden by exclusions")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return application.MessagesRecent(context.Background(), stdout, *limit, *includeHidden, strings.TrimSpace(*sourceID), strings.TrimSpace(*sourceLabel))

	case "messages-search":
		fs := flag.NewFlagSet("messages-search", flag.ContinueOnError)
		fs.SetOutput(stdout)
		query := fs.String("query", "", "text search query")
		limit := fs.Int("limit", 20, "maximum messages to print")
		sourceID := fs.String("source", "", "optional source id filter")
		sourceLabel := fs.String("label", "", "optional source label filter: POST or COMMENT")
		includeHidden := fs.Bool("include-hidden", false, "include messages hidden by exclusions")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return application.MessagesSearch(context.Background(), stdout, strings.TrimSpace(*query), *limit, *includeHidden, strings.TrimSpace(*sourceID), strings.TrimSpace(*sourceLabel))

	case "telegram-auth":
		return application.TelegramAuth(context.Background(), stdin, stdout)

	case "telegram-me":
		return application.TelegramMe(context.Background(), stdout)

	case "telegram-dry-run":
		fs := flag.NewFlagSet("telegram-dry-run", flag.ContinueOnError)
		fs.SetOutput(stdout)
		limit := fs.Int("limit", 5, "messages per source, max 50")
		sourceID := fs.String("source", "", "optional source id filter")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return application.TelegramDryRun(context.Background(), stdout, *limit, strings.TrimSpace(*sourceID))

	case "telegram-sync":
		fs := flag.NewFlagSet("telegram-sync", flag.ContinueOnError)
		fs.SetOutput(stdout)
		limit := fs.Int("limit", 200, "maximum new messages per source, max 1000")
		backfill := fs.Int("backfill", 0, "save latest N messages even if source has no state")
		sourceID := fs.String("source", "", "optional source id filter")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return application.TelegramSync(context.Background(), stdout, tgclient.SyncOptions{
			SourceID: strings.TrimSpace(*sourceID),
			Limit:    *limit,
			Backfill: *backfill,
		})

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

	db, err := a.openStorage(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	sourceRepo := sources.NewRepository(db)
	messageRepo := messages.NewRepository(db)
	exclusionRepo := exclusions.NewRepository(db)
	exclusionService := exclusions.NewService(exclusionRepo, messageRepo)
	stateRepo := state.NewRepository(db)
	telegramClient := tgclient.NewClient(tgclient.Config{
		APIID:       a.config.TelegramAPIID,
		APIHash:     a.config.TelegramAPIHash,
		SessionPath: a.config.TelegramSessionPath,
	})

	handler := mcp.NewHTTPHandler(mcp.ServerDeps{
		Sources:          sourceRepo,
		Messages:         messageRepo,
		Exclusions:       exclusionRepo,
		ExclusionService: exclusionService,
		States:           stateRepo,
		Telegram:         telegramClient,
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

// SourceAdd saves a Telegram source configuration.
func (a *App) SourceAdd(ctx context.Context, stdout io.Writer, source domain.Source) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}
	if source.Type == "" {
		source.Type = domain.SourceTypeChannel
	}

	db, err := a.openStorage(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := sources.NewRepository(db).Upsert(ctx, source); err != nil {
		return err
	}

	_, err = fmt.Fprintf(stdout, "source saved\nid: %s\ntype: %s\nentity_ref: %s\nenabled: %t\n", source.ID, source.Type, source.EntityRef, source.Enabled)
	if err != nil {
		return fmt.Errorf("write source status: %w", err)
	}
	return nil
}

// SourceRemove deletes a configured source. With purge it also deletes dependent local data.
func (a *App) SourceRemove(ctx context.Context, stdout io.Writer, id string, purge bool) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}
	if id == "" {
		return fmt.Errorf("source id is required")
	}

	db, err := a.openStorage(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	sourceRepo := sources.NewRepository(db)
	_, found, err := sourceRepo.Get(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("source %q not found", id)
	}

	if !purge {
		if err := sourceRepo.Remove(ctx, id); err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "source removed\nid: %s\npurged: false\n", id)
		if err != nil {
			return fmt.Errorf("write source remove status: %w", err)
		}
		return nil
	}

	purged, err := sourceRepo.Purge(ctx, id)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"source removed\nid: %s\npurged: true\nmessages: %d\nsource_states: %d\nsource_scoped_exclusions: %d\n",
		id,
		purged.Messages,
		purged.SourceStates,
		purged.SourceScopedExclusions,
	)
	if err != nil {
		return fmt.Errorf("write source purge status: %w", err)
	}
	return nil
}

// SourceList prints configured Telegram sources.
func (a *App) SourceList(ctx context.Context, stdout io.Writer) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}

	db, err := a.openStorage(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	items, err := sources.NewRepository(db).List(ctx)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		_, err = fmt.Fprintln(stdout, "no sources configured")
		return err
	}

	for _, source := range items {
		_, err := fmt.Fprintf(stdout, "%s\t%s\t%s\t%t\t%s\n", source.ID, source.Type, source.EntityRef, source.Enabled, source.Title)
		if err != nil {
			return fmt.Errorf("write source list: %w", err)
		}
	}
	return nil
}

// MessagesRecent prints recent stored messages.
func (a *App) MessagesRecent(ctx context.Context, stdout io.Writer, limit int, includeHidden bool, sourceID string, sourceLabel string) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}

	db, err := a.openStorage(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	items, err := messages.NewRepository(db).RecentFiltered(ctx, limit, includeHidden, messageFilter(sourceID, sourceLabel))
	if err != nil {
		return err
	}

	return printMessages(stdout, items)
}

// MessagesSearch prints stored messages matching text query.
func (a *App) MessagesSearch(ctx context.Context, stdout io.Writer, query string, limit int, includeHidden bool, sourceID string, sourceLabel string) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}

	db, err := a.openStorage(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	items, err := messages.NewRepository(db).SearchFiltered(ctx, query, limit, includeHidden, messageFilter(sourceID, sourceLabel))
	if err != nil {
		return err
	}

	return printMessages(stdout, items)
}

func messageFilter(sourceID string, sourceLabel string) messages.Filter {
	return messages.Filter{
		SourceID:    strings.TrimSpace(sourceID),
		SourceLabel: domain.SourceLabel(strings.ToUpper(strings.TrimSpace(sourceLabel))),
	}
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

// TelegramDryRun resolves configured sources and prints recent messages without saving them.
func (a *App) TelegramDryRun(ctx context.Context, stdout io.Writer, limit int, sourceID string) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}

	db, err := a.openStorage(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	allSources, err := sources.NewRepository(db).List(ctx)
	if err != nil {
		return err
	}

	selected := filterSources(allSources, sourceID)
	if len(selected) == 0 {
		if sourceID != "" {
			return fmt.Errorf("source %q not found", sourceID)
		}
		_, err = fmt.Fprintln(stdout, "no enabled sources configured")
		return err
	}

	client := tgclient.NewClient(tgclient.Config{
		APIID:       a.config.TelegramAPIID,
		APIHash:     a.config.TelegramAPIHash,
		SessionPath: a.config.TelegramSessionPath,
	})

	previews, err := client.DryRunSources(ctx, selected, limit)
	if err != nil {
		return err
	}

	for _, preview := range previews {
		if preview.Error != "" {
			_, err = fmt.Fprintf(stdout, "\n[%s] ERROR %s\n", preview.Source.ID, preview.Error)
			if err != nil {
				return fmt.Errorf("write dry-run source error: %w", err)
			}
			continue
		}

		_, err = fmt.Fprintf(
			stdout,
			"\n[%s] %s id=%d username=%s messages=%d\n",
			preview.Source.ID,
			preview.Resolved.Name,
			preview.Resolved.ID,
			preview.Resolved.Username,
			len(preview.Messages),
		)
		if err != nil {
			return fmt.Errorf("write dry-run source: %w", err)
		}

		for _, message := range preview.Messages {
			_, err = fmt.Fprintf(stdout, "  #%d %s %s\n", message.ID, message.Date.Format("2006-01-02 15:04:05"), oneLine(message.Text))
			if err != nil {
				return fmt.Errorf("write dry-run message: %w", err)
			}
		}
	}

	return nil
}

// TelegramSync resolves sources, applies baseline/backfill rules and saves new messages.
func (a *App) TelegramSync(ctx context.Context, stdout io.Writer, options tgclient.SyncOptions) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	if stdout == nil {
		return fmt.Errorf("stdout writer is required")
	}

	db, err := a.openStorage(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	allSources, err := sources.NewRepository(db).List(ctx)
	if err != nil {
		return err
	}

	selected := filterSources(allSources, options.SourceID)
	if len(selected) == 0 {
		if options.SourceID != "" {
			return fmt.Errorf("source %q not found", options.SourceID)
		}
		_, err = fmt.Fprintln(stdout, "no enabled sources configured")
		return err
	}

	messageRepo := messages.NewRepository(db)
	exclusionRepo := exclusions.NewRepository(db)
	client := tgclient.NewClient(tgclient.Config{
		APIID:       a.config.TelegramAPIID,
		APIHash:     a.config.TelegramAPIHash,
		SessionPath: a.config.TelegramSessionPath,
	})

	results, err := client.SyncSources(ctx, selected, tgclient.SyncRepos{
		States:     state.NewRepository(db),
		Messages:   messageRepo,
		Exclusions: exclusions.NewService(exclusionRepo, messageRepo),
	}, options)
	if err != nil {
		return err
	}

	for _, result := range results {
		if result.Error != "" {
			_, err = fmt.Fprintf(stdout, "[%s] ERROR %s\n", result.Source.ID, result.Error)
			if err != nil {
				return fmt.Errorf("write sync error: %w", err)
			}
			continue
		}

		status := "synced"
		switch {
		case result.Baselined:
			status = "baselined"
		case result.Backfilled:
			status = "backfilled"
		case result.Truncated:
			status = "truncated"
		}

		_, err = fmt.Fprintf(
			stdout,
			"[%s] %s resolved=%s latest=%d saved=%d skipped_excluded=%d state_advanced=%t comments_available=%t comments_latest=%d comments_saved=%d comments_skipped_excluded=%d comments_state_advanced=%t\n",
			result.Source.ID,
			status,
			result.Resolved.Name,
			result.LatestMessageID,
			result.SavedMessages,
			result.SkippedExcluded,
			result.StateAdvanced,
			result.CommentsAvailable,
			result.LatestCommentMessageID,
			result.SavedComments,
			result.SkippedExcludedComments,
			result.CommentsStateAdvanced,
		)
		if err != nil {
			return fmt.Errorf("write sync result: %w", err)
		}
	}

	return nil
}

func (a *App) openStorage(ctx context.Context) (*sql.DB, error) {
	db, err := storage.Open(ctx, a.config.DatabasePath)
	if err != nil {
		return nil, err
	}

	if err := storage.Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func printMessages(stdout io.Writer, items []domain.Message) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(stdout, "no messages")
		return err
	}

	for _, item := range items {
		hidden := ""
		if item.HiddenByExclusion {
			hidden = " hidden"
		}

		_, err := fmt.Fprintf(
			stdout,
			"%s\t%s\t#%d%s\t%s\t%s\n",
			item.Date.Format("2006-01-02 15:04:05"),
			item.SourceID,
			item.MessageID,
			hidden,
			senderDisplay(item.Sender),
			oneLine(item.Text),
		)
		if err != nil {
			return fmt.Errorf("write messages: %w", err)
		}
	}

	return nil
}

func senderDisplay(sender domain.Sender) string {
	if strings.TrimSpace(sender.DisplayName) != "" {
		return strings.TrimSpace(sender.DisplayName)
	}
	if strings.TrimSpace(sender.Username) != "" {
		return "@" + strings.TrimPrefix(strings.TrimSpace(sender.Username), "@")
	}
	if sender.ID != 0 {
		return fmt.Sprintf("id:%d", sender.ID)
	}
	return "-"
}

func filterSources(items []domain.Source, sourceID string) []domain.Source {
	result := make([]domain.Source, 0, len(items))
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		if sourceID != "" && item.ID != sourceID {
			continue
		}
		result = append(result, item)
	}
	return result
}

func oneLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) <= 160 {
		return value
	}
	runes := []rune(value)
	return string(runes[:160]) + "..."
}

func displayPublicBaseURL(value string) string {
	if value == "" {
		return "<auto>"
	}
	return value
}
