package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const resourceMIMEJSON = "application/json"

func registerResources(server *mcpsdk.Server, deps ServerDeps) {
	server.AddResource(&mcpsdk.Resource{
		URI:         "telegram://sources",
		Name:        "telegram.sources",
		Title:       "Telegram Sources",
		Description: "Configured Telegram sources.",
		MIMEType:    resourceMIMEJSON,
	}, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		items, err := deps.Sources.List(ctx)
		if err != nil {
			return nil, err
		}
		return jsonResource(readResourceURI(req, "telegram://sources"), map[string]any{"sources": mapSources(items)})
	})

	server.AddResource(&mcpsdk.Resource{
		URI:         "telegram://messages/recent",
		Name:        "telegram.messages.recent",
		Title:       "Recent Telegram Messages",
		Description: "Recent non-hidden Telegram messages.",
		MIMEType:    resourceMIMEJSON,
	}, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		items, err := deps.Messages.RecentFiltered(ctx, 50, false, messages.Filter{})
		if err != nil {
			return nil, err
		}
		return jsonResource(readResourceURI(req, "telegram://messages/recent"), map[string]any{"messages": mapMessages(items)})
	})

	server.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		URITemplate: "telegram://source/{id}/messages",
		Name:        "telegram.source.messages",
		Title:       "Telegram Source Messages",
		Description: "Recent non-hidden messages for one Telegram source.",
		MIMEType:    resourceMIMEJSON,
	}, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		uri := readResourceURI(req, "")
		sourceID, ok := sourceMessagesResourceID(uri)
		if !ok {
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}

		items, err := deps.Messages.RecentFiltered(ctx, 50, false, messages.Filter{SourceID: sourceID})
		if err != nil {
			return nil, err
		}
		return jsonResource(uri, map[string]any{
			"source_id": sourceID,
			"messages":  mapMessages(items),
		})
	})

	server.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		URITemplate: "telegram://message/{external_id}",
		Name:        "telegram.message",
		Title:       "Telegram Message",
		Description: "One stored Telegram message by external id.",
		MIMEType:    resourceMIMEJSON,
	}, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		uri := readResourceURI(req, "")
		externalID, ok := messageResourceExternalID(uri)
		if !ok {
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}

		message, found, err := deps.Messages.Get(ctx, externalID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}
		return jsonResource(uri, map[string]any{
			"found":   true,
			"message": mapMessage(message),
		})
	})

	server.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		URITemplate: "telegram://message/{source_label}/{source_id}/{message_id}",
		Name:        "telegram.message.by_parts",
		Title:       "Telegram Message By Parts",
		Description: "One stored Telegram message by source label, source id, and message id.",
		MIMEType:    resourceMIMEJSON,
	}, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		uri := readResourceURI(req, "")
		externalID, ok := messagePathResourceExternalID(uri)
		if !ok {
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}

		message, found, err := deps.Messages.Get(ctx, externalID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}
		return jsonResource(uri, map[string]any{
			"found":       true,
			"external_id": externalID,
			"message":     mapMessage(message),
		})
	})

	server.AddResource(&mcpsdk.Resource{
		URI:         "telegram://spam-list",
		Name:        "telegram.spam_list",
		Title:       "Telegram Spam List",
		Description: "All locally excluded Telegram senders.",
		MIMEType:    resourceMIMEJSON,
	}, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		items, err := deps.Exclusions.List(ctx)
		if err != nil {
			return nil, err
		}
		return jsonResource(readResourceURI(req, "telegram://spam-list"), map[string]any{"senders": mapExcludedSenders(items)})
	})

	server.AddResource(&mcpsdk.Resource{
		URI:         "telegram://spam-list/global",
		Name:        "telegram.spam_list.global",
		Title:       "Global Telegram Spam List",
		Description: "Global locally excluded Telegram senders.",
		MIMEType:    resourceMIMEJSON,
	}, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		items, err := deps.Exclusions.List(ctx)
		if err != nil {
			return nil, err
		}
		return jsonResource(readResourceURI(req, "telegram://spam-list/global"), map[string]any{
			"senders": mapExcludedSenders(filterExcludedSenders(items, func(item domain.ExcludedSender) bool {
				return item.Scope == domain.ExclusionScopeGlobal
			})),
		})
	})

	server.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		URITemplate: "telegram://spam-list/source/{source_id}",
		Name:        "telegram.spam_list.source",
		Title:       "Source Telegram Spam List",
		Description: "Source-scoped locally excluded Telegram senders.",
		MIMEType:    resourceMIMEJSON,
	}, func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		uri := readResourceURI(req, "")
		sourceID, ok := spamListSourceResourceID(uri)
		if !ok {
			return nil, mcpsdk.ResourceNotFoundError(uri)
		}

		items, err := deps.Exclusions.List(ctx)
		if err != nil {
			return nil, err
		}
		return jsonResource(uri, map[string]any{
			"source_id": sourceID,
			"senders": mapExcludedSenders(filterExcludedSenders(items, func(item domain.ExcludedSender) bool {
				return item.Scope == domain.ExclusionScopeSource && item.SourceID == sourceID
			})),
		})
	})
}

func jsonResource(uri string, value any) (*mcpsdk.ReadResourceResult, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal resource %s: %w", uri, err)
	}

	return &mcpsdk.ReadResourceResult{
		Contents: []*mcpsdk.ResourceContents{
			{
				URI:      uri,
				MIMEType: resourceMIMEJSON,
				Text:     string(data),
			},
		},
	}, nil
}

func readResourceURI(req *mcpsdk.ReadResourceRequest, fallback string) string {
	if req == nil || req.Params == nil || strings.TrimSpace(req.Params.URI) == "" {
		return fallback
	}
	return strings.TrimSpace(req.Params.URI)
}

func sourceMessagesResourceID(uri string) (string, bool) {
	const prefix = "telegram://source/"
	const suffix = "/messages"

	value, ok := pathResourceValue(uri, prefix, suffix)
	return value, ok
}

func messageResourceExternalID(uri string) (string, bool) {
	const prefix = "telegram://message/"

	value, ok := pathResourceValue(uri, prefix, "")
	return value, ok
}

func messagePathResourceExternalID(uri string) (string, bool) {
	const prefix = "telegram://message/"

	if !strings.HasPrefix(uri, prefix) {
		return "", false
	}

	value := strings.Trim(strings.TrimPrefix(uri, prefix), "/")
	parts := strings.Split(value, "/")
	if len(parts) != 3 {
		return "", false
	}

	sourceLabel, ok := cleanPathPart(parts[0])
	if !ok {
		return "", false
	}
	sourceID, ok := cleanPathPart(parts[1])
	if !ok {
		return "", false
	}
	messageID, ok := cleanPathPart(parts[2])
	if !ok {
		return "", false
	}

	sourceLabel = strings.ToUpper(sourceLabel)
	if sourceLabel != string(domain.SourceLabelPost) && sourceLabel != string(domain.SourceLabelComment) {
		return "", false
	}

	return fmt.Sprintf("telegram:%s:%s:%s", sourceLabel, sourceID, messageID), true
}

func cleanPathPart(value string) (string, bool) {
	decoded, err := url.PathUnescape(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}

	decoded = strings.TrimSpace(decoded)
	if decoded == "" || strings.Contains(decoded, "/") {
		return "", false
	}
	return decoded, true
}

func spamListSourceResourceID(uri string) (string, bool) {
	const prefix = "telegram://spam-list/source/"

	value, ok := pathResourceValue(uri, prefix, "")
	return value, ok
}

func pathResourceValue(uri string, prefix string, suffix string) (string, bool) {
	if !strings.HasPrefix(uri, prefix) {
		return "", false
	}

	value := strings.TrimPrefix(uri, prefix)
	if suffix != "" {
		if !strings.HasSuffix(value, suffix) {
			return "", false
		}
		value = strings.TrimSuffix(value, suffix)
	}

	value = strings.Trim(value, "/")
	if value == "" {
		return "", false
	}

	decoded, err := url.PathUnescape(value)
	if err != nil || strings.TrimSpace(decoded) == "" {
		return "", false
	}
	return strings.TrimSpace(decoded), true
}

func filterExcludedSenders(items []domain.ExcludedSender, keep func(domain.ExcludedSender) bool) []domain.ExcludedSender {
	result := make([]domain.ExcludedSender, 0, len(items))
	for _, item := range items {
		if keep(item) {
			result = append(result, item)
		}
	}
	return result
}
