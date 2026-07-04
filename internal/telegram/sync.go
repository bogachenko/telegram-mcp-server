package telegram

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/exclusions"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	"github.com/bogachenko/telegram-mcp-server/internal/state"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	querymessages "github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

var (
	errSyncLimitReached = stderrors.New("sync limit reached")
	errSyncStateReached = stderrors.New("sync state reached")
)

// SyncOptions controls Telegram source sync behavior.
type SyncOptions struct {
	SourceID string
	Limit    int
	Backfill int
}

// SyncRepos contains storage dependencies for sync.
type SyncRepos struct {
	States     *state.Repository
	Messages   *messages.Repository
	Exclusions *exclusions.Service
}

// SourceSyncResult describes one source sync result.
type SourceSyncResult struct {
	Source          domain.Source
	Resolved        ResolvedPeer
	LatestMessageID int64
	SavedMessages   int
	SkippedExcluded int
	Baselined       bool
	Backfilled      bool
	Truncated       bool
	StateAdvanced   bool
	Error           string
}

// SyncSources resolves configured sources and saves new messages.
func (c *Client) SyncSources(ctx context.Context, items []domain.Source, repos SyncRepos, options SyncOptions) ([]SourceSyncResult, error) {
	if c == nil {
		return nil, fmt.Errorf("telegram client is required")
	}
	if err := c.config.validateBase(); err != nil {
		return nil, err
	}
	if repos.States == nil || repos.Messages == nil || repos.Exclusions == nil {
		return nil, fmt.Errorf("sync repositories are required")
	}

	limit := normalizeSyncLimit(options.Limit)
	backfill := normalizeBackfill(options.Backfill)

	client, err := c.newGotdClient()
	if err != nil {
		return nil, err
	}

	result := make([]SourceSyncResult, 0, len(items))
	err = client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("telegram auth status: %w", err)
		}
		if !status.Authorized {
			return fmt.Errorf("telegram session is not authorized; run telegram-auth first")
		}

		api := client.API()
		peerManager := peers.Options{}.Build(api)
		if err := peerManager.Init(ctx); err != nil {
			return fmt.Errorf("telegram peer manager init: %w", err)
		}

		for _, source := range items {
			syncResult := syncOneSource(ctx, api, peerManager, source, repos, limit, backfill)
			result = append(result, syncResult)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func syncOneSource(
	ctx context.Context,
	api *tg.Client,
	peerManager *peers.Manager,
	source domain.Source,
	repos SyncRepos,
	limit int,
	backfill int,
) SourceSyncResult {
	result := SourceSyncResult{Source: source}

	resolved, err := peerManager.Resolve(ctx, sourceResolveRef(source))
	if err != nil {
		result.Error = err.Error()
		return result
	}

	username, _ := resolved.Username()
	result.Resolved = ResolvedPeer{
		ID:       resolved.ID(),
		TDLibID:  int64(resolved.TDLibPeerID()),
		Name:     resolved.VisibleName(),
		Username: username,
	}

	input := resolved.InputPeer()

	if backfill > 0 {
		collected, _, truncated, err := collectHistory(ctx, api, input, backfill, 0)
		if err != nil {
			result.Error = err.Error()
			return result
		}

		saved, skipped, maxID, err := saveCollectedMessages(ctx, repos, source, result.Resolved, collected)
		if err != nil {
			result.Error = err.Error()
			return result
		}

		result.Backfilled = true
		result.Truncated = truncated
		result.SavedMessages = saved
		result.SkippedExcluded = skipped
		result.LatestMessageID = maxID
		if maxID > 0 {
			if err := repos.States.Save(ctx, domain.SourceState{
				SourceID:             source.ID,
				LastMessageID:        maxID,
				LastCommentMessageID: 0,
			}); err != nil {
				result.Error = err.Error()
				return result
			}
			result.StateAdvanced = true
		}
		return result
	}

	currentState, found, err := repos.States.Get(ctx, source.ID)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	if !found || currentState.LastMessageID <= 0 {
		latest, _, _, err := collectHistory(ctx, api, input, 1, 0)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Baselined = true
		if len(latest) == 0 {
			return result
		}

		latestID := int64(latest[0].Msg.GetID())
		result.LatestMessageID = latestID
		if err := repos.States.Save(ctx, domain.SourceState{
			SourceID:             source.ID,
			LastMessageID:        latestID,
			LastCommentMessageID: 0,
		}); err != nil {
			result.Error = err.Error()
			return result
		}
		result.StateAdvanced = true
		return result
	}

	collected, reachedState, truncated, err := collectHistory(ctx, api, input, limit, currentState.LastMessageID)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Truncated = truncated

	saved, skipped, maxID, err := saveCollectedMessages(ctx, repos, source, result.Resolved, collected)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.SavedMessages = saved
	result.SkippedExcluded = skipped
	result.LatestMessageID = maxID

	if maxID > currentState.LastMessageID && reachedState && !truncated {
		if err := repos.States.Save(ctx, domain.SourceState{
			SourceID:             source.ID,
			LastMessageID:        maxID,
			LastCommentMessageID: currentState.LastCommentMessageID,
		}); err != nil {
			result.Error = err.Error()
			return result
		}
		result.StateAdvanced = true
	}

	if maxID == 0 {
		result.LatestMessageID = currentState.LastMessageID
		result.StateAdvanced = true
	}

	return result
}

func collectHistory(ctx context.Context, api *tg.Client, input tg.InputPeerClass, limit int, stopAtOrBefore int64) ([]querymessages.Elem, bool, bool, error) {
	collected := make([]querymessages.Elem, 0, limit)
	reachedState := false
	truncated := false

	err := query.Messages(api).GetHistory(input).BatchSize(limit).ForEach(ctx, func(ctx context.Context, elem querymessages.Elem) error {
		msg, ok := elem.Msg.(*tg.Message)
		if !ok {
			return nil
		}

		if stopAtOrBefore > 0 && int64(msg.ID) <= stopAtOrBefore {
			reachedState = true
			return errSyncStateReached
		}

		if len(collected) >= limit {
			truncated = true
			return errSyncLimitReached
		}

		collected = append(collected, elem)

		if len(collected) >= limit {
			truncated = true
			return errSyncLimitReached
		}

		return nil
	})
	if err != nil && !stderrors.Is(err, errSyncStateReached) && !stderrors.Is(err, errSyncLimitReached) {
		return collected, reachedState, truncated, err
	}

	sort.Slice(collected, func(i, j int) bool {
		return collected[i].Msg.GetID() < collected[j].Msg.GetID()
	})

	return collected, reachedState, truncated, nil
}

func saveCollectedMessages(
	ctx context.Context,
	repos SyncRepos,
	source domain.Source,
	resolved ResolvedPeer,
	collected []querymessages.Elem,
) (int, int, int64, error) {
	saved := 0
	skipped := 0
	var maxID int64

	for _, elem := range collected {
		msg, ok := elem.Msg.(*tg.Message)
		if !ok {
			continue
		}

		if int64(msg.ID) > maxID {
			maxID = int64(msg.ID)
		}

		sender := senderFromMessage(msg, elem)
		excluded, err := repos.Exclusions.IsExcluded(ctx, sender, source.ID)
		if err != nil {
			return saved, skipped, maxID, err
		}
		if excluded {
			skipped++
			continue
		}

		if err := repos.Messages.Save(ctx, normalizeMessage(source, resolved, msg, sender)); err != nil {
			return saved, skipped, maxID, err
		}
		saved++
	}

	return saved, skipped, maxID, nil
}

func normalizeMessage(source domain.Source, resolved ResolvedPeer, msg *tg.Message, sender domain.Sender) domain.Message {
	return domain.Message{
		ExternalID:        externalMessageID(source.ID, domain.SourceLabelPost, int64(msg.ID)),
		SourceID:          source.ID,
		SourceLabel:       domain.SourceLabelPost,
		ChatID:            resolved.TDLibID,
		ChatTitle:         resolved.Name,
		MessageID:         int64(msg.ID),
		Sender:            sender,
		Text:              msg.Message,
		Link:              messageLink(source, int64(msg.ID)),
		Date:              time.Unix(int64(msg.Date), 0).UTC(),
		HiddenByExclusion: false,
	}
}

func senderFromMessage(msg *tg.Message, elem querymessages.Elem) domain.Sender {
	peer := msg.FromID
	if peer == nil {
		peer = msg.PeerID
	}

	switch p := peer.(type) {
	case *tg.PeerUser:
		if user, ok := elem.Entities.User(p.UserID); ok {
			return domain.Sender{
				ID:                 user.ID,
				Username:           user.Username,
				UsernameNormalized: normalizeUsername(user.Username),
				DisplayName:        strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " ")),
			}
		}
		return domain.Sender{ID: p.UserID}

	case *tg.PeerChat:
		if chat, ok := elem.Entities.Chat(p.ChatID); ok {
			return domain.Sender{
				ID:          chat.ID,
				DisplayName: chat.Title,
			}
		}
		return domain.Sender{ID: p.ChatID}

	case *tg.PeerChannel:
		if channel, ok := elem.Entities.Channel(p.ChannelID); ok {
			return domain.Sender{
				ID:                 channel.ID,
				Username:           channel.Username,
				UsernameNormalized: normalizeUsername(channel.Username),
				DisplayName:        channel.Title,
			}
		}
		return domain.Sender{ID: p.ChannelID}

	default:
		return domain.Sender{}
	}
}

func externalMessageID(sourceID string, label domain.SourceLabel, messageID int64) string {
	return fmt.Sprintf("telegram:%s:%s:%d", label, sourceID, messageID)
}

func messageLink(source domain.Source, messageID int64) string {
	username := publicUsername(source)
	if username == "" || strings.HasPrefix(username, "+") {
		return ""
	}
	return fmt.Sprintf("https://t.me/%s/%d", username, messageID)
}

func publicUsername(source domain.Source) string {
	for _, value := range []string{source.PublicUsername, source.EntityRef} {
		value = strings.TrimSpace(value)
		value = strings.TrimPrefix(value, "@")
		value = strings.TrimRight(value, "/")

		if strings.HasPrefix(value, "https://t.me/") || strings.HasPrefix(value, "http://t.me/") || strings.HasPrefix(value, "t.me/") {
			if parsed, err := url.Parse(value); err == nil && parsed.Host == "t.me" {
				value = strings.Trim(parsed.Path, "/")
			} else {
				value = strings.TrimPrefix(value, "https://t.me/")
				value = strings.TrimPrefix(value, "http://t.me/")
				value = strings.TrimPrefix(value, "t.me/")
			}
		}

		if strings.Contains(value, "/") {
			value = strings.Split(value, "/")[0]
		}

		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeUsername(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "@")
	return strings.ToLower(value)
}

func normalizeSyncLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func normalizeBackfill(backfill int) int {
	if backfill < 0 {
		return 0
	}
	if backfill > 1000 {
		return 1000
	}
	return backfill
}
