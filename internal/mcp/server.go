package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/exclusions"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/state"
	tgclient "github.com/bogachenko/telegram-mcp-server/internal/telegram"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerDeps contains storage-backed services used by MCP tools.
type ServerDeps struct {
	Sources          *sources.Repository
	Messages         *messages.Repository
	Exclusions       *exclusions.Repository
	ExclusionService *exclusions.Service
	States           *state.Repository
	Telegram         *tgclient.Client
}

// NewHTTPHandler builds the Streamable HTTP MCP handler.
func NewHTTPHandler(deps ServerDeps) http.Handler {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "telegram-mcp-server",
		Version: "0.1.0",
	}, nil)

	registerTools(server, deps)
	registerResources(server, deps)

	mux := http.NewServeMux()
	registerOAuthHTTPHandlers(mux)

	mcpHandler := mcpsdk.NewStreamableHTTPHandler(
		func(req *http.Request) *mcpsdk.Server {
			return server
		},
		&mcpsdk.StreamableHTTPOptions{
			JSONResponse:               true,
			DisableLocalhostProtection: true,
		},
	)

	mux.Handle("/mcp", oauthProtectedMCPHandler(mcpHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})

	return mux
}

type sourcesListInput struct{}

type sourceAddInput struct {
	ID             string `json:"id" jsonschema:"Stable local source id"`
	Type           string `json:"type" jsonschema:"channel or group"`
	EntityRef      string `json:"entity_ref" jsonschema:"Telegram username, invite reference, or numeric entity id"`
	PublicUsername string `json:"public_username,omitempty" jsonschema:"Public username without @ if available"`
	Title          string `json:"title,omitempty" jsonschema:"Human-readable source title"`
	Enabled        *bool  `json:"enabled,omitempty" jsonschema:"Whether this source is enabled. Default true."`
}

type sourceRemoveInput struct {
	ID    string `json:"id" jsonschema:"Stable local source id"`
	Purge bool   `json:"purge,omitempty" jsonschema:"Also delete source state, stored messages, and source-scoped exclusions"`
}

type messagesRecentInput struct {
	Limit         int    `json:"limit,omitempty" jsonschema:"Maximum number of messages, default 50, maximum 200"`
	SourceID      string `json:"source_id,omitempty" jsonschema:"Optional source id filter"`
	SourceLabel   string `json:"source_label,omitempty" jsonschema:"Optional source label filter: POST or COMMENT"`
	IncludeHidden bool   `json:"include_hidden,omitempty" jsonschema:"Include messages hidden by spam exclusion"`
}

type messagesSearchInput struct {
	Query         string `json:"query" jsonschema:"Case-insensitive text search query"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Maximum number of messages, default 50, maximum 200"`
	SourceID      string `json:"source_id,omitempty" jsonschema:"Optional source id filter"`
	SourceLabel   string `json:"source_label,omitempty" jsonschema:"Optional source label filter: POST or COMMENT"`
	IncludeHidden bool   `json:"include_hidden,omitempty" jsonschema:"Include messages hidden by spam exclusion"`
}

type messageGetInput struct {
	ExternalID string `json:"external_id" jsonschema:"Message external id"`
}

type spamListInput struct{}

type spamAddSenderInput struct {
	SenderID    int64  `json:"sender_id,omitempty" jsonschema:"Telegram sender id"`
	Username    string `json:"username,omitempty" jsonschema:"Telegram username with or without @"`
	DisplayName string `json:"display_name,omitempty" jsonschema:"Human-readable sender display name"`
	Reason      string `json:"reason,omitempty" jsonschema:"Reason for excluding this sender"`
	ScopeType   string `json:"scope_type,omitempty" jsonschema:"global or source. Default global."`
	SourceID    string `json:"source_id,omitempty" jsonschema:"Source id for source-scoped exclusion"`
}

type spamAddFromMessageInput struct {
	MessageExternalID string `json:"message_external_id" jsonschema:"Message external id used as evidence"`
	Reason            string `json:"reason,omitempty" jsonschema:"Reason for excluding the message author"`
	ScopeType         string `json:"scope_type,omitempty" jsonschema:"global or source. Default global."`
	SourceID          string `json:"source_id,omitempty" jsonschema:"Source id for source-scoped exclusion. Defaults to message source."`
}

type spamRemoveSenderInput struct {
	SenderID  int64  `json:"sender_id,omitempty" jsonschema:"Telegram sender id"`
	Username  string `json:"username,omitempty" jsonschema:"Telegram username with or without @"`
	ScopeType string `json:"scope_type,omitempty" jsonschema:"global or source. Default global."`
	SourceID  string `json:"source_id,omitempty" jsonschema:"Source id for source-scoped exclusion"`
}

type syncInput struct {
	SourceID string `json:"source_id,omitempty" jsonschema:"Optional source id filter"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum new messages per source, default 200, maximum 1000"`
	Backfill int    `json:"backfill,omitempty" jsonschema:"Save latest N messages even if source has no state"`
}

func registerTools(server *mcpsdk.Server, deps ServerDeps) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.sources_list",
		Description: "List configured Telegram channels and groups.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *sourcesListInput) (*mcpsdk.CallToolResult, any, error) {
		items, err := deps.Sources.List(ctx)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"sources": mapSources(items)}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.sources_add",
		Description: "Add or update a local Telegram source configuration.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *sourceAddInput) (*mcpsdk.CallToolResult, any, error) {
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		source := domain.Source{
			ID:             strings.TrimSpace(input.ID),
			Type:           domain.SourceType(strings.TrimSpace(input.Type)),
			EntityRef:      strings.TrimSpace(input.EntityRef),
			PublicUsername: strings.TrimPrefix(strings.TrimSpace(input.PublicUsername), "@"),
			Title:          strings.TrimSpace(input.Title),
			Enabled:        enabled,
		}
		if source.Type == "" {
			source.Type = domain.SourceTypeChannel
		}
		if err := deps.Sources.Upsert(ctx, source); err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"source": mapSource(source)}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.sources_remove",
		Description: "Remove a local Telegram source configuration.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *sourceRemoveInput) (*mcpsdk.CallToolResult, any, error) {
		id := strings.TrimSpace(input.ID)
		if id == "" {
			return nil, nil, fmt.Errorf("source id is required")
		}
		if input.Purge {
			source, found, err := deps.Sources.Get(ctx, id)
			if err != nil {
				return nil, nil, err
			}
			if !found {
				return nil, nil, fmt.Errorf("source %q not found", id)
			}

			purged, err := deps.Sources.Purge(ctx, id)
			if err != nil {
				return nil, nil, err
			}
			return nil, map[string]any{
				"removed":                  true,
				"id":                       id,
				"source":                   mapSource(source),
				"purged":                   true,
				"messages":                 purged.Messages,
				"source_states":            purged.SourceStates,
				"source_scoped_exclusions": purged.SourceScopedExclusions,
			}, nil
		}

		if err := deps.Sources.Remove(ctx, id); err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"removed": true, "id": id, "purged": false}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.messages_recent",
		Description: "Return recent non-hidden Telegram messages.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *messagesRecentInput) (*mcpsdk.CallToolResult, any, error) {
		items, err := deps.Messages.RecentFiltered(ctx, input.Limit, input.IncludeHidden, mcpMessageFilter(input.SourceID, input.SourceLabel))
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"messages": mapMessages(items)}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.messages_search",
		Description: "Search stored non-hidden Telegram messages.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *messagesSearchInput) (*mcpsdk.CallToolResult, any, error) {
		items, err := deps.Messages.SearchFiltered(ctx, input.Query, input.Limit, input.IncludeHidden, mcpMessageFilter(input.SourceID, input.SourceLabel))
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"messages": mapMessages(items)}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.message_get",
		Description: "Return one stored Telegram message by external id.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *messageGetInput) (*mcpsdk.CallToolResult, any, error) {
		message, ok, err := deps.Messages.Get(ctx, strings.TrimSpace(input.ExternalID))
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"found": ok, "message": mapMessage(message)}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.spam_list_senders",
		Description: "List locally excluded senders.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *spamListInput) (*mcpsdk.CallToolResult, any, error) {
		items, err := deps.Exclusions.List(ctx)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"senders": mapExcludedSenders(items)}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.spam_add_sender",
		Description: "Add a sender to the local spam list.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *spamAddSenderInput) (*mcpsdk.CallToolResult, any, error) {
		result, err := deps.ExclusionService.AddSender(ctx, exclusions.AddSenderParams{
			Sender: domain.Sender{
				ID:          input.SenderID,
				Username:    strings.TrimSpace(input.Username),
				DisplayName: strings.TrimSpace(input.DisplayName),
			},
			Reason:    strings.TrimSpace(input.Reason),
			Scope:     parseScope(input.ScopeType),
			SourceID:  strings.TrimSpace(input.SourceID),
			CreatedBy: "mcp",
		})
		if err != nil {
			return nil, nil, err
		}
		return nil, mapAddResult(result), nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.spam_add_from_message",
		Description: "Add a message author to the spam list and store the evidence message.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *spamAddFromMessageInput) (*mcpsdk.CallToolResult, any, error) {
		result, err := deps.ExclusionService.AddFromMessage(
			ctx,
			strings.TrimSpace(input.MessageExternalID),
			strings.TrimSpace(input.Reason),
			"mcp",
			parseScope(input.ScopeType),
			strings.TrimSpace(input.SourceID),
		)
		if err != nil {
			return nil, nil, err
		}
		return nil, mapAddResult(result), nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.spam_remove_sender",
		Description: "Remove a sender from the local spam list.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *spamRemoveSenderInput) (*mcpsdk.CallToolResult, any, error) {
		removed, err := deps.ExclusionService.RemoveSender(ctx, domain.Sender{
			ID:       input.SenderID,
			Username: strings.TrimSpace(input.Username),
		}, parseScope(input.ScopeType), strings.TrimSpace(input.SourceID))
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"removed": removed}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "telegram.sync",
		Description: "Sync new messages from configured Telegram sources.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input *syncInput) (*mcpsdk.CallToolResult, any, error) {
		if input == nil {
			input = &syncInput{}
		}
		if deps.Telegram == nil {
			return nil, nil, fmt.Errorf("telegram client is required")
		}
		if deps.States == nil {
			return nil, nil, fmt.Errorf("state repository is required")
		}

		sourceID := strings.TrimSpace(input.SourceID)
		sources, err := deps.Sources.List(ctx)
		if err != nil {
			return nil, nil, err
		}

		selected := filterEnabledSources(sources, sourceID)
		if len(selected) == 0 {
			if sourceID != "" {
				return nil, nil, fmt.Errorf("source %q not found", sourceID)
			}
			return nil, map[string]any{"results": []map[string]any{}, "message": "no enabled sources configured"}, nil
		}

		results, err := deps.Telegram.SyncSources(ctx, selected, tgclient.SyncRepos{
			States:     deps.States,
			Messages:   deps.Messages,
			Exclusions: deps.ExclusionService,
		}, tgclient.SyncOptions{
			SourceID: sourceID,
			Limit:    input.Limit,
			Backfill: input.Backfill,
		})
		if err != nil {
			return nil, nil, err
		}

		return nil, map[string]any{"results": mapSyncResults(results)}, nil
	})
}

func mcpMessageFilter(sourceID string, sourceLabel string) messages.Filter {
	return messages.Filter{
		SourceID:    strings.TrimSpace(sourceID),
		SourceLabel: domain.SourceLabel(strings.ToUpper(strings.TrimSpace(sourceLabel))),
	}
}

func filterEnabledSources(items []domain.Source, sourceID string) []domain.Source {
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

func mapSyncResults(items []tgclient.SourceSyncResult) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, mapSyncResult(item))
	}
	return result
}

func mapSyncResult(item tgclient.SourceSyncResult) map[string]any {
	status := "synced"
	switch {
	case item.Baselined:
		status = "baselined"
	case item.Backfilled:
		status = "backfilled"
	case item.Truncated:
		status = "truncated"
	}

	return map[string]any{
		"source":                    mapSource(item.Source),
		"resolved":                  mapResolvedPeer(item.Resolved),
		"status":                    status,
		"latest_message_id":         item.LatestMessageID,
		"latest_comment_message_id": item.LatestCommentMessageID,
		"saved_messages":            item.SavedMessages,
		"saved_comments":            item.SavedComments,
		"skipped_excluded":          item.SkippedExcluded,
		"skipped_excluded_comments": item.SkippedExcludedComments,
		"baselined":                 item.Baselined,
		"comments_baselined":        item.CommentsBaselined,
		"backfilled":                item.Backfilled,
		"comments_available":        item.CommentsAvailable,
		"truncated":                 item.Truncated,
		"comments_truncated":        item.CommentsTruncated,
		"state_advanced":            item.StateAdvanced,
		"comments_state_advanced":   item.CommentsStateAdvanced,
		"error":                     item.Error,
	}
}

func mapResolvedPeer(item tgclient.ResolvedPeer) map[string]any {
	return map[string]any{
		"id":       item.ID,
		"tdlib_id": item.TDLibID,
		"name":     item.Name,
		"username": item.Username,
	}
}

func parseScope(value string) domain.ExclusionScope {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(domain.ExclusionScopeSource):
		return domain.ExclusionScopeSource
	default:
		return domain.ExclusionScopeGlobal
	}
}

func mapSources(items []domain.Source) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, mapSource(item))
	}
	return result
}

func mapSource(item domain.Source) map[string]any {
	return map[string]any{
		"id":              item.ID,
		"type":            item.Type,
		"entity_ref":      item.EntityRef,
		"public_username": item.PublicUsername,
		"title":           item.Title,
		"enabled":         item.Enabled,
	}
}

func mapMessages(items []domain.Message) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, mapMessage(item))
	}
	return result
}

func mapMessage(item domain.Message) map[string]any {
	if item.ExternalID == "" {
		return nil
	}
	return map[string]any{
		"external_id":         item.ExternalID,
		"source_id":           item.SourceID,
		"source_label":        item.SourceLabel,
		"chat_id":             item.ChatID,
		"chat_title":          item.ChatTitle,
		"message_id":          item.MessageID,
		"sender":              mapSender(item.Sender),
		"text":                item.Text,
		"link":                item.Link,
		"date":                formatTime(item.Date),
		"hidden_by_exclusion": item.HiddenByExclusion,
	}
}

func mapSender(item domain.Sender) map[string]any {
	return map[string]any{
		"id":                  item.ID,
		"username":            item.Username,
		"username_normalized": item.UsernameNormalized,
		"display_name":        item.DisplayName,
	}
}

func mapExcludedSenders(items []domain.ExcludedSender) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, mapExcludedSender(item))
	}
	return result
}

func mapExcludedSender(item domain.ExcludedSender) map[string]any {
	return map[string]any{
		"id":                  item.ID,
		"sender_id":           item.SenderID,
		"username":            item.Username,
		"username_normalized": item.UsernameNormalized,
		"display_name":        item.DisplayName,
		"reason":              item.Reason,
		"evidence": map[string]any{
			"message_external_id": item.Evidence.ExternalID,
			"message_text":        item.Evidence.Text,
			"message_link":        item.Evidence.Link,
			"message_date":        formatTime(item.Evidence.Date),
			"source_id":           item.Evidence.SourceID,
			"source_title":        item.Evidence.SourceTitle,
		},
		"scope_type": item.Scope,
		"source_id":  item.SourceID,
		"created_at": formatTime(item.CreatedAt),
		"created_by": item.CreatedBy,
	}
}

func mapAddResult(result exclusions.AddResult) map[string]any {
	return map[string]any{
		"sender":                   mapExcludedSender(result.Sender),
		"already_excluded":         result.AlreadyExcluded,
		"hidden_existing_messages": result.HiddenExistingMessages,
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
