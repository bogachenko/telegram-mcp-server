package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/exclusions"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/state"
	tgclient "github.com/bogachenko/telegram-mcp-server/internal/telegram"
)

const (
	defaultWatchIntervalSeconds = 300
	defaultWatchLimit           = 1000
)

func startTelegramWatcher(
	ctx context.Context,
	stdout io.Writer,
	sourceRepo *sources.Repository,
	messageRepo *messages.Repository,
	exclusionService *exclusions.Service,
	stateRepo *state.Repository,
	telegramClient *tgclient.Client,
	intervalSeconds int,
	limit int,
) {
	if stdout == nil {
		stdout = io.Discard
	}
	if intervalSeconds <= 0 {
		intervalSeconds = defaultWatchIntervalSeconds
	}
	if limit <= 0 {
		limit = defaultWatchLimit
	}

	interval := time.Duration(intervalSeconds) * time.Second

	go func() {
		_, _ = fmt.Fprintf(stdout, "telegram watcher enabled interval=%s limit=%d\n", interval, limit)

		runTelegramWatcherSync(ctx, stdout, sourceRepo, messageRepo, exclusionService, stateRepo, telegramClient, limit)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runTelegramWatcherSync(ctx, stdout, sourceRepo, messageRepo, exclusionService, stateRepo, telegramClient, limit)
			}
		}
	}()
}

func runTelegramWatcherSync(
	ctx context.Context,
	stdout io.Writer,
	sourceRepo *sources.Repository,
	messageRepo *messages.Repository,
	exclusionService *exclusions.Service,
	stateRepo *state.Repository,
	telegramClient *tgclient.Client,
	limit int,
) {
	if sourceRepo == nil || messageRepo == nil || exclusionService == nil || stateRepo == nil || telegramClient == nil {
		_, _ = fmt.Fprintln(stdout, "telegram watcher skipped: dependencies are not configured")
		return
	}

	items, err := sourceRepo.List(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "telegram watcher sources error: %v\n", err)
		return
	}

	selected := enabledSources(items)
	if len(selected) == 0 {
		_, _ = fmt.Fprintln(stdout, "telegram watcher skipped: no enabled sources")
		return
	}

	started := time.Now()
	results, err := telegramClient.SyncSources(ctx, selected, tgclient.SyncRepos{
		States:     stateRepo,
		Messages:   messageRepo,
		Exclusions: exclusionService,
	}, tgclient.SyncOptions{
		Limit: limit,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "telegram watcher sync error: %v\n", err)
		return
	}

	var saved int
	var comments int
	var errors int
	var truncated int
	for _, result := range results {
		saved += result.SavedMessages
		comments += result.SavedComments
		if result.Error != "" {
			errors++
		}
		if result.Truncated || result.CommentsTruncated {
			truncated++
		}
	}

	_, _ = fmt.Fprintf(
		stdout,
		"telegram watcher sync done sources=%d saved=%d comments=%d errors=%d truncated=%d duration=%s\n",
		len(results),
		saved,
		comments,
		errors,
		truncated,
		time.Since(started).Round(time.Millisecond),
	)

	for _, result := range results {
		if result.Error == "" && result.SavedMessages == 0 && result.SavedComments == 0 && !result.Baselined && !result.CommentsBaselined && !result.Truncated && !result.CommentsTruncated {
			continue
		}

		_, _ = fmt.Fprintf(
			stdout,
			"telegram watcher source=%s saved=%d comments=%d latest=%d comments_latest=%d baselined=%t comments_baselined=%t truncated=%t comments_truncated=%t error=%s\n",
			result.Source.ID,
			result.SavedMessages,
			result.SavedComments,
			result.LatestMessageID,
			result.LatestCommentMessageID,
			result.Baselined,
			result.CommentsBaselined,
			result.Truncated,
			result.CommentsTruncated,
			result.Error,
		)
	}
}

func enabledSources(items []domain.Source) []domain.Source {
	result := make([]domain.Source, 0, len(items))
	for _, item := range items {
		if item.Enabled {
			result = append(result, item)
		}
	}
	return result
}
