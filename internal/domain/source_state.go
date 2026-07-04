package domain

import "time"

// SourceState stores incremental scan cursors for a Telegram source.
type SourceState struct {
	SourceID             string
	LastMessageID        int64
	LastCommentMessageID int64
	UpdatedAt            time.Time
}
