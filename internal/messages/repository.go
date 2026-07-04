// Package messages stores normalized Telegram messages.
package messages

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
)

// Repository persists normalized Telegram messages.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a message repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Save inserts or updates a normalized Telegram message.
func (r *Repository) Save(ctx context.Context, message domain.Message) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("message repository is required")
	}
	if message.ExternalID == "" {
		return fmt.Errorf("message external id is required")
	}
	if message.SourceID == "" {
		return fmt.Errorf("message source id is required")
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO messages (
		     external_id, source_id, source_label, chat_id, chat_title, message_id,
		     sender_id, sender_username, sender_username_normalized, sender_display_name,
		     text, link, date, hidden_by_exclusion, created_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(external_id) DO UPDATE SET
		     source_id = excluded.source_id,
		     source_label = excluded.source_label,
		     chat_id = excluded.chat_id,
		     chat_title = excluded.chat_title,
		     message_id = excluded.message_id,
		     sender_id = excluded.sender_id,
		     sender_username = excluded.sender_username,
		     sender_username_normalized = excluded.sender_username_normalized,
		     sender_display_name = excluded.sender_display_name,
		     text = excluded.text,
		     link = excluded.link,
		     date = excluded.date,
		     hidden_by_exclusion = excluded.hidden_by_exclusion`,
		message.ExternalID,
		message.SourceID,
		string(message.SourceLabel),
		message.ChatID,
		nullString(message.ChatTitle),
		message.MessageID,
		nullInt64(message.Sender.ID),
		nullString(message.Sender.Username),
		nullString(normalizeUsername(message.Sender.UsernameNormalized, message.Sender.Username)),
		nullString(message.Sender.DisplayName),
		nullString(message.Text),
		nullString(message.Link),
		nullTime(message.Date),
		boolInt(message.HiddenByExclusion),
	)
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}
	return nil
}

// Get returns one message by external id.
func (r *Repository) Get(ctx context.Context, externalID string) (domain.Message, bool, error) {
	if r == nil || r.db == nil {
		return domain.Message{}, false, fmt.Errorf("message repository is required")
	}

	row := r.db.QueryRowContext(ctx, selectMessagesSQL()+` WHERE external_id = ?`, externalID)
	message, err := scanMessage(row)
	if err == nil {
		return message, true, nil
	}
	if err == sql.ErrNoRows {
		return domain.Message{}, false, nil
	}
	return domain.Message{}, false, fmt.Errorf("get message: %w", err)
}

// Recent returns recent messages ordered newest first.
func (r *Repository) Recent(ctx context.Context, limit int, includeHidden bool) ([]domain.Message, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("message repository is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := selectMessagesSQL()
	args := []any{}
	if !includeHidden {
		query += ` WHERE hidden_by_exclusion = 0`
	}
	query += ` ORDER BY date DESC, message_id DESC LIMIT ?`
	args = append(args, limit)
	return r.list(ctx, query, args...)
}

// Search returns messages whose text contains query.
func (r *Repository) Search(ctx context.Context, queryText string, limit int, includeHidden bool) ([]domain.Message, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("message repository is required")
	}
	queryText = strings.TrimSpace(queryText)
	if queryText == "" {
		return nil, fmt.Errorf("search query is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := selectMessagesSQL() + ` WHERE lower(coalesce(text, '')) LIKE ?`
	args := []any{"%" + strings.ToLower(queryText) + "%"}
	if !includeHidden {
		query += ` AND hidden_by_exclusion = 0`
	}
	query += ` ORDER BY date DESC, message_id DESC LIMIT ?`
	args = append(args, limit)
	return r.list(ctx, query, args...)
}

// HideBySender marks matching messages hidden by exclusion and returns affected count.
func (r *Repository) HideBySender(ctx context.Context, sender domain.Sender, scope domain.ExclusionScope, sourceID string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("message repository is required")
	}

	where, args, err := senderWhere(sender)
	if err != nil {
		return 0, err
	}
	if scope == domain.ExclusionScopeSource {
		if sourceID == "" {
			return 0, fmt.Errorf("source id is required for source scope")
		}
		where += ` AND source_id = ?`
		args = append(args, sourceID)
	}

	result, err := r.db.ExecContext(ctx, `UPDATE messages SET hidden_by_exclusion = 1 WHERE hidden_by_exclusion = 0 AND `+where, args...)
	if err != nil {
		return 0, fmt.Errorf("hide messages by sender: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count hidden messages: %w", err)
	}
	return count, nil
}

func (r *Repository) list(ctx context.Context, query string, args ...any) ([]domain.Message, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var result []domain.Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan messages: %w", err)
	}
	return result, nil
}

func selectMessagesSQL() string {
	return `SELECT external_id, source_id, source_label, chat_id, chat_title, message_id,
	       sender_id, sender_username, sender_username_normalized, sender_display_name,
	       text, link, date, hidden_by_exclusion FROM messages`
}

type messageScanner interface {
	Scan(dest ...any) error
}

func scanMessage(scanner messageScanner) (domain.Message, error) {
	var message domain.Message
	var sourceLabel string
	var chatTitle sql.NullString
	var senderID sql.NullInt64
	var senderUsername sql.NullString
	var senderUsernameNormalized sql.NullString
	var senderDisplayName sql.NullString
	var text sql.NullString
	var link sql.NullString
	var date sql.NullString
	var hidden int

	if err := scanner.Scan(
		&message.ExternalID,
		&message.SourceID,
		&sourceLabel,
		&message.ChatID,
		&chatTitle,
		&message.MessageID,
		&senderID,
		&senderUsername,
		&senderUsernameNormalized,
		&senderDisplayName,
		&text,
		&link,
		&date,
		&hidden,
	); err != nil {
		return domain.Message{}, err
	}

	message.SourceLabel = domain.SourceLabel(sourceLabel)
	message.ChatTitle = chatTitle.String
	message.Sender.ID = senderID.Int64
	message.Sender.Username = senderUsername.String
	message.Sender.UsernameNormalized = senderUsernameNormalized.String
	message.Sender.DisplayName = senderDisplayName.String
	message.Text = text.String
	message.Link = link.String
	message.Date = parseTime(date.String)
	message.HiddenByExclusion = hidden != 0
	return message, nil
}

func senderWhere(sender domain.Sender) (string, []any, error) {
	if sender.ID != 0 {
		return `sender_id = ?`, []any{sender.ID}, nil
	}
	username := normalizeUsername(sender.UsernameNormalized, sender.Username)
	if username != "" {
		return `sender_username_normalized = ?`, []any{username}, nil
	}
	return "", nil, fmt.Errorf("sender id or username is required")
}

func normalizeUsername(normalized string, username string) string {
	value := strings.TrimSpace(normalized)
	if value == "" {
		value = strings.TrimSpace(username)
	}
	value = strings.TrimPrefix(value, "@")
	return strings.ToLower(value)
}

func nullString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullInt64(value int64) sql.NullInt64 {
	if value == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value, Valid: true}
}

func nullTime(value time.Time) sql.NullString {
	if value.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: value.UTC().Format(time.RFC3339Nano), Valid: true}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}
