// Package mcp contains MCP tool and resource declarations.
package mcp

// Tool describes an MCP tool planned for registration.
type Tool struct {
	Name        string
	Description string
	ReadOnly    bool
}

// ListTools returns the initial Telegram MCP tool catalog.
func ListTools() []Tool {
	return []Tool{
		{
			Name:        "telegram.sources_list",
			Description: "List configured Telegram channels and groups.",
			ReadOnly:    true,
		},
		{
			Name:        "telegram.sources_add",
			Description: "Add a local Telegram source configuration.",
			ReadOnly:    false,
		},
		{
			Name:        "telegram.sources_remove",
			Description: "Remove a local Telegram source configuration.",
			ReadOnly:    false,
		},
		{
			Name:        "telegram.sync",
			Description: "Sync new messages from configured Telegram sources.",
			ReadOnly:    false,
		},
		{
			Name:        "telegram.messages_recent",
			Description: "Return recent non-hidden Telegram messages.",
			ReadOnly:    true,
		},
		{
			Name:        "telegram.messages_search",
			Description: "Search stored non-hidden Telegram messages.",
			ReadOnly:    true,
		},
		{
			Name:        "telegram.message_get",
			Description: "Return one stored Telegram message by external id.",
			ReadOnly:    true,
		},
		{
			Name:        "telegram.spam_add_sender",
			Description: "Add a sender to the local spam list.",
			ReadOnly:    false,
		},
		{
			Name:        "telegram.spam_add_from_message",
			Description: "Add a message author to the spam list and store the evidence message.",
			ReadOnly:    false,
		},
		{
			Name:        "telegram.spam_remove_sender",
			Description: "Remove a sender from the local spam list.",
			ReadOnly:    false,
		},
		{
			Name:        "telegram.spam_list_senders",
			Description: "List locally excluded senders.",
			ReadOnly:    true,
		},
	}
}
