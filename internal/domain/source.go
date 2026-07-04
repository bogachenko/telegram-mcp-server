// Package domain contains core Telegram MCP models.
package domain

// SourceType identifies the kind of Telegram source.
type SourceType string

const (
	// SourceTypeChannel is a Telegram channel.
	SourceTypeChannel SourceType = "channel"

	// SourceTypeGroup is a Telegram group or supergroup.
	SourceTypeGroup SourceType = "group"

	// SourceTypeDiscussion is a linked discussion chat.
	SourceTypeDiscussion SourceType = "discussion"
)

// Source is a configured Telegram channel or group.
type Source struct {
	ID             string
	Type           SourceType
	EntityRef      string
	PublicUsername string
	Title          string
	Enabled        bool
}
