package domain

import "time"

// SourceLabel identifies whether a Telegram item is a post or comment.
type SourceLabel string

const (
	// SourceLabelPost is a source post or regular group message.
	SourceLabelPost SourceLabel = "POST"

	// SourceLabelComment is a linked discussion comment.
	SourceLabelComment SourceLabel = "COMMENT"
)

// Sender describes the Telegram author of a message.
type Sender struct {
	ID                 int64
	Username           string
	UsernameNormalized string
	DisplayName        string
}

// Message is a normalized Telegram message exposed through MCP.
type Message struct {
	ExternalID        string
	SourceID          string
	SourceLabel       SourceLabel
	ChatID            int64
	ChatTitle         string
	MessageID         int64
	Sender            Sender
	Text              string
	Link              string
	Date              time.Time
	HiddenByExclusion bool
}
