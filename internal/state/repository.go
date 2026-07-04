// Package state stores Telegram scan cursors.
package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
)

// Repository persists per-source incremental scan state.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a source-state repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Get returns state for sourceID.
func (r *Repository) Get(ctx context.Context, sourceID string) (domain.SourceState, bool, error) {
	if r == nil || r.db == nil {
		return domain.SourceState{}, false, fmt.Errorf("state repository is required")
	}

	var state domain.SourceState
	var updatedAt string
	err := r.db.QueryRowContext(
		ctx,
		`SELECT source_id, last_message_id, last_comment_message_id, updated_at FROM source_states WHERE source_id = ?`,
		sourceID,
	).Scan(&state.SourceID, &state.LastMessageID, &state.LastCommentMessageID, &updatedAt)
	if err == sql.ErrNoRows {
		return domain.SourceState{}, false, nil
	}
	if err != nil {
		return domain.SourceState{}, false, fmt.Errorf("get source state: %w", err)
	}
	state.UpdatedAt = parseTime(updatedAt)
	return state, true, nil
}

// Save inserts or updates source state.
func (r *Repository) Save(ctx context.Context, state domain.SourceState) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("state repository is required")
	}
	if state.SourceID == "" {
		return fmt.Errorf("source id is required")
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO source_states (source_id, last_message_id, last_comment_message_id, updated_at)
		 VALUES (?, ?, ?, datetime('now'))
		 ON CONFLICT(source_id) DO UPDATE SET
		     last_message_id = excluded.last_message_id,
		     last_comment_message_id = excluded.last_comment_message_id,
		     updated_at = datetime('now')`,
		state.SourceID,
		state.LastMessageID,
		state.LastCommentMessageID,
	)
	if err != nil {
		return fmt.Errorf("save source state: %w", err)
	}
	return nil
}

func parseTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}
