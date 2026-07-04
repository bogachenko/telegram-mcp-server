// Package exclusions manages local spam/excluded sender policy.
package exclusions

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
)

// AddSenderParams describes an excluded sender write.
type AddSenderParams struct {
	Sender    domain.Sender
	Reason    string
	Evidence  domain.EvidenceMessage
	Scope     domain.ExclusionScope
	SourceID  string
	CreatedBy string
}

// Repository persists excluded senders.
type Repository struct {
	db *sql.DB
}

// NewRepository creates an excluded sender repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Add inserts an excluded sender or returns the existing matching entry.
func (r *Repository) Add(ctx context.Context, params AddSenderParams) (domain.ExcludedSender, bool, error) {
	if r == nil || r.db == nil {
		return domain.ExcludedSender{}, false, fmt.Errorf("exclusion repository is required")
	}
	params.Scope = normalizeScope(params.Scope)
	params.Sender.UsernameNormalized = normalizeUsername(params.Sender.UsernameNormalized, params.Sender.Username)
	if params.Sender.ID == 0 && params.Sender.UsernameNormalized == "" {
		return domain.ExcludedSender{}, false, fmt.Errorf("sender id or username is required")
	}
	if params.Scope == domain.ExclusionScopeSource && params.SourceID == "" {
		return domain.ExcludedSender{}, false, fmt.Errorf("source id is required for source scope")
	}

	existing, found, err := r.FindMatching(ctx, params.Sender, params.Scope, params.SourceID)
	if err != nil {
		return domain.ExcludedSender{}, false, err
	}
	if found {
		updated, err := r.fillExisting(ctx, existing.ID, params)
		return updated, true, err
	}

	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO excluded_senders (
		     sender_id, username, username_normalized, display_name,
		     reason,
		     evidence_message_external_id, evidence_message_text, evidence_message_link,
		     evidence_message_date, evidence_source_id, evidence_source_title,
		     scope_type, source_id, created_at, created_by
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), ?)`,
		nullInt64(params.Sender.ID),
		nullString(params.Sender.Username),
		nullString(params.Sender.UsernameNormalized),
		nullString(params.Sender.DisplayName),
		nullString(params.Reason),
		nullString(params.Evidence.ExternalID),
		nullString(params.Evidence.Text),
		nullString(params.Evidence.Link),
		nullTime(params.Evidence.Date),
		nullString(params.Evidence.SourceID),
		nullString(params.Evidence.SourceTitle),
		string(params.Scope),
		nullString(params.SourceID),
		nullString(params.CreatedBy),
	)
	if err != nil {
		return domain.ExcludedSender{}, false, fmt.Errorf("add excluded sender: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return domain.ExcludedSender{}, false, fmt.Errorf("get excluded sender id: %w", err)
	}

	created, ok, err := r.Get(ctx, id)
	if err != nil {
		return domain.ExcludedSender{}, false, err
	}
	if !ok {
		return domain.ExcludedSender{}, false, fmt.Errorf("created excluded sender not found")
	}
	return created, false, nil
}

// Get returns one excluded sender by repository id.
func (r *Repository) Get(ctx context.Context, id int64) (domain.ExcludedSender, bool, error) {
	if r == nil || r.db == nil {
		return domain.ExcludedSender{}, false, fmt.Errorf("exclusion repository is required")
	}
	row := r.db.QueryRowContext(ctx, selectExcludedSendersSQL()+` WHERE id = ?`, id)
	entry, err := scanExcludedSender(row)
	if err == nil {
		return entry, true, nil
	}
	if err == sql.ErrNoRows {
		return domain.ExcludedSender{}, false, nil
	}
	return domain.ExcludedSender{}, false, fmt.Errorf("get excluded sender: %w", err)
}

// FindMatching finds a sender exclusion by id first, then username.
func (r *Repository) FindMatching(ctx context.Context, sender domain.Sender, scope domain.ExclusionScope, sourceID string) (domain.ExcludedSender, bool, error) {
	if r == nil || r.db == nil {
		return domain.ExcludedSender{}, false, fmt.Errorf("exclusion repository is required")
	}
	scope = normalizeScope(scope)
	username := normalizeUsername(sender.UsernameNormalized, sender.Username)

	queries := make([]struct {
		where string
		args  []any
	}, 0, 2)
	if sender.ID != 0 {
		where, args := scopeWhere(`sender_id = ?`, []any{sender.ID}, scope, sourceID)
		queries = append(queries, struct {
			where string
			args  []any
		}{where: where, args: args})
	}
	if username != "" {
		where, args := scopeWhere(`username_normalized = ?`, []any{username}, scope, sourceID)
		queries = append(queries, struct {
			where string
			args  []any
		}{where: where, args: args})
	}

	for _, query := range queries {
		row := r.db.QueryRowContext(ctx, selectExcludedSendersSQL()+` WHERE `+query.where+` LIMIT 1`, query.args...)
		entry, err := scanExcludedSender(row)
		if err == nil {
			return entry, true, nil
		}
		if err != sql.ErrNoRows {
			return domain.ExcludedSender{}, false, fmt.Errorf("find excluded sender: %w", err)
		}
	}
	return domain.ExcludedSender{}, false, nil
}

// RemoveMatching deletes a matching excluded sender.
func (r *Repository) RemoveMatching(ctx context.Context, sender domain.Sender, scope domain.ExclusionScope, sourceID string) (bool, error) {
	entry, ok, err := r.FindMatching(ctx, sender, scope, sourceID)
	if err != nil || !ok {
		return ok, err
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM excluded_senders WHERE id = ?`, entry.ID)
	if err != nil {
		return false, fmt.Errorf("remove excluded sender: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count removed sender: %w", err)
	}
	return count > 0, nil
}

// List returns excluded senders ordered by newest first.
func (r *Repository) List(ctx context.Context) ([]domain.ExcludedSender, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("exclusion repository is required")
	}
	rows, err := r.db.QueryContext(ctx, selectExcludedSendersSQL()+` ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list excluded senders: %w", err)
	}
	defer rows.Close()

	var result []domain.ExcludedSender
	for rows.Next() {
		entry, err := scanExcludedSender(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan excluded senders: %w", err)
	}
	return result, nil
}

func (r *Repository) fillExisting(ctx context.Context, id int64, params AddSenderParams) (domain.ExcludedSender, error) {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE excluded_senders SET
		     reason = CASE WHEN ? IS NOT NULL AND ? != '' THEN ? ELSE reason END,
		     evidence_message_external_id = CASE WHEN coalesce(evidence_message_external_id, '') = '' THEN ? ELSE evidence_message_external_id END,
		     evidence_message_text = CASE WHEN coalesce(evidence_message_text, '') = '' THEN ? ELSE evidence_message_text END,
		     evidence_message_link = CASE WHEN coalesce(evidence_message_link, '') = '' THEN ? ELSE evidence_message_link END,
		     evidence_message_date = CASE WHEN coalesce(evidence_message_date, '') = '' THEN ? ELSE evidence_message_date END,
		     evidence_source_id = CASE WHEN coalesce(evidence_source_id, '') = '' THEN ? ELSE evidence_source_id END,
		     evidence_source_title = CASE WHEN coalesce(evidence_source_title, '') = '' THEN ? ELSE evidence_source_title END
		 WHERE id = ?`,
		nullString(params.Reason), params.Reason, params.Reason,
		nullString(params.Evidence.ExternalID),
		nullString(params.Evidence.Text),
		nullString(params.Evidence.Link),
		nullTime(params.Evidence.Date),
		nullString(params.Evidence.SourceID),
		nullString(params.Evidence.SourceTitle),
		id,
	)
	if err != nil {
		return domain.ExcludedSender{}, fmt.Errorf("update existing excluded sender: %w", err)
	}
	entry, ok, err := r.Get(ctx, id)
	if err != nil {
		return domain.ExcludedSender{}, err
	}
	if !ok {
		return domain.ExcludedSender{}, fmt.Errorf("excluded sender disappeared")
	}
	return entry, nil
}

func selectExcludedSendersSQL() string {
	return `SELECT id, sender_id, username, username_normalized, display_name, reason,
	       evidence_message_external_id, evidence_message_text, evidence_message_link,
	       evidence_message_date, evidence_source_id, evidence_source_title,
	       scope_type, source_id, created_at, created_by FROM excluded_senders`
}

type excludedSenderScanner interface {
	Scan(dest ...any) error
}

func scanExcludedSender(scanner excludedSenderScanner) (domain.ExcludedSender, error) {
	var entry domain.ExcludedSender
	var senderID sql.NullInt64
	var username sql.NullString
	var usernameNormalized sql.NullString
	var displayName sql.NullString
	var reason sql.NullString
	var evidenceExternalID sql.NullString
	var evidenceText sql.NullString
	var evidenceLink sql.NullString
	var evidenceDate sql.NullString
	var evidenceSourceID sql.NullString
	var evidenceSourceTitle sql.NullString
	var scope string
	var sourceID sql.NullString
	var createdAt string
	var createdBy sql.NullString

	if err := scanner.Scan(
		&entry.ID,
		&senderID,
		&username,
		&usernameNormalized,
		&displayName,
		&reason,
		&evidenceExternalID,
		&evidenceText,
		&evidenceLink,
		&evidenceDate,
		&evidenceSourceID,
		&evidenceSourceTitle,
		&scope,
		&sourceID,
		&createdAt,
		&createdBy,
	); err != nil {
		return domain.ExcludedSender{}, err
	}

	entry.SenderID = senderID.Int64
	entry.Username = username.String
	entry.UsernameNormalized = usernameNormalized.String
	entry.DisplayName = displayName.String
	entry.Reason = reason.String
	entry.Evidence = domain.EvidenceMessage{
		ExternalID:  evidenceExternalID.String,
		Text:        evidenceText.String,
		Link:        evidenceLink.String,
		Date:        parseTime(evidenceDate.String),
		SourceID:    evidenceSourceID.String,
		SourceTitle: evidenceSourceTitle.String,
	}
	entry.Scope = domain.ExclusionScope(scope)
	entry.SourceID = sourceID.String
	entry.CreatedAt = parseTime(createdAt)
	entry.CreatedBy = createdBy.String
	return entry, nil
}

func scopeWhere(base string, args []any, scope domain.ExclusionScope, sourceID string) (string, []any) {
	if normalizeScope(scope) == domain.ExclusionScopeSource {
		return base + ` AND scope_type = ? AND source_id = ?`, append(args, string(domain.ExclusionScopeSource), sourceID)
	}
	return base + ` AND scope_type = ?`, append(args, string(domain.ExclusionScopeGlobal))
}

func normalizeScope(scope domain.ExclusionScope) domain.ExclusionScope {
	if scope == domain.ExclusionScopeSource {
		return scope
	}
	return domain.ExclusionScopeGlobal
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
