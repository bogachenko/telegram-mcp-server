package app

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/exclusions"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/state"
	tgclient "github.com/bogachenko/telegram-mcp-server/internal/telegram"
)

const (
	defaultWatchIntervalSeconds  = 300
	defaultWatchLimit            = 1000
	sourceErrorDisableThreshold  = 3
	floodWaitExtraPauseSeconds   = 5
	floodWaitMinimumPauseSeconds = 1800
)

var floodWaitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`FLOOD_WAIT \((\d+)\)`),
	regexp.MustCompile(`FLOOD_WAIT_(\d+)`),
}

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

	selected, skippedPaused := watcherReadySources(items, time.Now())
	if len(selected) == 0 {
		_, _ = fmt.Fprintf(stdout, "telegram watcher skipped: no ready sources paused=%d\n", skippedPaused)
		return
	}

	started := time.Now()
	results, err := telegramClient.SyncSources(ctx, selected, tgclient.SyncRepos{
		States:     stateRepo,
		Messages:   messageRepo,
		Exclusions: exclusionService,
	}, tgclient.SyncOptions{
		Limit:           limit,
		StopOnFloodWait: true,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "telegram watcher sync error: %v\n", err)
		return
	}

	updateSourceHealth(ctx, stdout, sourceRepo, results)

	var saved int
	var comments int
	var errors int
	var truncated int
	var floodWaits int
	for _, result := range results {
		saved += result.SavedMessages
		comments += result.SavedComments
		if result.Error != "" {
			errors++
		}
		if result.Truncated || result.CommentsTruncated {
			truncated++
		}
		if isFloodWaitError(result.Error) {
			floodWaits++
		}
	}

	_, _ = fmt.Fprintf(
		stdout,
		"telegram watcher sync done sources=%d ready=%d paused=%d saved=%d comments=%d errors=%d flood_waits=%d truncated=%d duration=%s\n",
		len(results),
		len(selected),
		skippedPaused,
		saved,
		comments,
		errors,
		floodWaits,
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

func updateSourceHealth(ctx context.Context, stdout io.Writer, sourceRepo *sources.Repository, results []tgclient.SourceSyncResult) {
	now := time.Now()

	for _, result := range results {
		if result.Error == "" {
			if err := sourceRepo.MarkHealthy(ctx, result.Source.ID); err != nil {
				_, _ = fmt.Fprintf(stdout, "telegram watcher source health success update error source=%s error=%v\n", result.Source.ID, err)
			}
			continue
		}

		pauseUntil := sourcePauseUntil(now, result.Error)
		disable := shouldDisableSource(result.Source, result.Error)
		if err := sourceRepo.MarkUnhealthy(ctx, result.Source.ID, result.Error, pauseUntil, disable); err != nil {
			_, _ = fmt.Fprintf(stdout, "telegram watcher source health error update error source=%s error=%v\n", result.Source.ID, err)
			continue
		}

		if disable {
			_, _ = fmt.Fprintf(stdout, "telegram watcher source disabled source=%s error_count=%d error=%s\n", result.Source.ID, result.Source.ErrorCount+1, result.Error)
		}
		if !pauseUntil.IsZero() {
			_, _ = fmt.Fprintf(stdout, "telegram watcher source paused source=%s until=%s error=%s\n", result.Source.ID, pauseUntil.UTC().Format(time.RFC3339), result.Error)
		}
	}
}

func watcherReadySources(items []domain.Source, now time.Time) ([]domain.Source, int) {
	result := make([]domain.Source, 0, len(items))
	skippedPaused := 0

	for _, item := range items {
		if !item.Enabled {
			continue
		}
		if !item.PausedUntil.IsZero() && item.PausedUntil.After(now) {
			skippedPaused++
			continue
		}
		result = append(result, item)
	}

	return result, skippedPaused
}

func shouldDisableSource(source domain.Source, message string) bool {
	if isFloodWaitError(message) {
		return false
	}
	if !isPermanentSourceError(message) {
		return false
	}
	return source.ErrorCount+1 >= sourceErrorDisableThreshold
}

func isPermanentSourceError(message string) bool {
	value := strings.ToLower(message)
	for _, marker := range []string{
		"username_not_occupied",
		"contact not found",
		"chat_id_invalid",
		"can't resolve peerchannel",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func sourcePauseUntil(now time.Time, message string) time.Time {
	seconds, ok := floodWaitSeconds(message)
	if !ok {
		return time.Time{}
	}
	pauseSeconds := seconds + floodWaitExtraPauseSeconds
	if pauseSeconds < floodWaitMinimumPauseSeconds {
		pauseSeconds = floodWaitMinimumPauseSeconds
	}
	return now.Add(time.Duration(pauseSeconds) * time.Second)
}

func isFloodWaitError(message string) bool {
	_, ok := floodWaitSeconds(message)
	return ok
}

func floodWaitSeconds(message string) (int, bool) {
	for _, pattern := range floodWaitPatterns {
		matches := pattern.FindStringSubmatch(message)
		if len(matches) != 2 {
			continue
		}
		seconds, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		return seconds, true
	}
	return 0, false
}
