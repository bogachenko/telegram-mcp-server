package telegram

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	querymessages "github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

var errDryRunLimitReached = stderrors.New("dry-run limit reached")

// SourcePreview contains a resolved source and recent message preview.
type SourcePreview struct {
	Source   domain.Source
	Resolved ResolvedPeer
	Messages []MessagePreview
	Error    string
}

// ResolvedPeer describes a Telegram peer resolved through MTProto.
type ResolvedPeer struct {
	ID       int64
	TDLibID  int64
	Name     string
	Username string
}

// MessagePreview describes a recent Telegram message without storing it.
type MessagePreview struct {
	ID     int
	Date   time.Time
	Text   string
	PeerID string
}

// DryRunSources resolves sources and reads recent messages without saving anything.
func (c *Client) DryRunSources(ctx context.Context, items []domain.Source, limit int) ([]SourcePreview, error) {
	if c == nil {
		return nil, fmt.Errorf("telegram client is required")
	}
	if err := c.config.validateBase(); err != nil {
		return nil, err
	}

	limit = normalizeDryRunLimit(limit)

	client, err := c.newGotdClient()
	if err != nil {
		return nil, err
	}

	result := make([]SourcePreview, 0, len(items))
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
			preview := SourcePreview{Source: source}
			resolved, err := peerManager.Resolve(ctx, sourceResolveRef(source))
			if err != nil {
				preview.Error = err.Error()
				result = append(result, preview)
				continue
			}

			username, _ := resolved.Username()
			preview.Resolved = ResolvedPeer{
				ID:       resolved.ID(),
				TDLibID:  int64(resolved.TDLibPeerID()),
				Name:     resolved.VisibleName(),
				Username: username,
			}

			messages, err := recentMessages(ctx, api, resolved.InputPeer(), limit)
			if err != nil {
				preview.Error = err.Error()
			}
			preview.Messages = messages
			result = append(result, preview)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func recentMessages(ctx context.Context, api *tg.Client, input tg.InputPeerClass, limit int) ([]MessagePreview, error) {
	collected := make([]MessagePreview, 0, limit)
	err := query.Messages(api).GetHistory(input).BatchSize(limit).ForEach(ctx, func(ctx context.Context, elem querymessages.Elem) error {
		if len(collected) >= limit {
			return errDryRunLimitReached
		}

		msg, ok := elem.Msg.(*tg.Message)
		if !ok {
			return nil
		}

		collected = append(collected, MessagePreview{
			ID:     msg.ID,
			Date:   time.Unix(int64(msg.Date), 0).UTC(),
			Text:   msg.Message,
			PeerID: peerIDString(msg.PeerID),
		})

		if len(collected) >= limit {
			return errDryRunLimitReached
		}
		return nil
	})
	if err != nil && !stderrors.Is(err, errDryRunLimitReached) {
		return collected, err
	}
	return collected, nil
}

func sourceResolveRef(source domain.Source) string {
	for _, value := range []string{
		source.EntityRef,
		source.PublicUsername,
		source.ID,
	} {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeDryRunLimit(limit int) int {
	if limit <= 0 {
		return 5
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func peerIDString(peer tg.PeerClass) string {
	switch p := peer.(type) {
	case *tg.PeerUser:
		return fmt.Sprintf("user:%d", p.UserID)
	case *tg.PeerChat:
		return fmt.Sprintf("chat:%d", p.ChatID)
	case *tg.PeerChannel:
		return fmt.Sprintf("channel:%d", p.ChannelID)
	default:
		return fmt.Sprintf("%T", peer)
	}
}
