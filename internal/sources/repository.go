// Package sources stores configured Telegram sources.
package sources

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
)

// Repository persists Telegram source configuration.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a source repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// PurgeResult contains counts deleted during source purge.
type PurgeResult struct {
	Messages               int64
	SourceStates           int64
	SourceScopedExclusions int64
	Sources                int64
}

// Upsert inserts or updates a configured source.
func (r *Repository) Upsert(ctx context.Context, source domain.Source) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("source repository is required")
	}
	if source.ID == "" {
		return fmt.Errorf("source id is required")
	}
	if source.EntityRef == "" {
		return fmt.Errorf("source entity ref is required")
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO sources (id, type, entity_ref, public_username, title, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		 ON CONFLICT(id) DO UPDATE SET
		     type = excluded.type,
		     entity_ref = excluded.entity_ref,
		     public_username = excluded.public_username,
		     title = excluded.title,
		     enabled = excluded.enabled,
		     updated_at = datetime('now')`,
		source.ID,
		string(source.Type),
		source.EntityRef,
		nullString(source.PublicUsername),
		nullString(source.Title),
		boolInt(source.Enabled),
	)
	if err != nil {
		return fmt.Errorf("upsert source: %w", err)
	}
	return nil
}

// Get returns one configured source by id.
func (r *Repository) Get(ctx context.Context, id string) (domain.Source, bool, error) {
	if r == nil || r.db == nil {
		return domain.Source{}, false, fmt.Errorf("source repository is required")
	}

	row := r.db.QueryRowContext(ctx, selectSourcesSQL()+` WHERE id = ?`, id)
	source, err := scanSource(row)
	if err == nil {
		return source, true, nil
	}
	if err == sql.ErrNoRows {
		return domain.Source{}, false, nil
	}
	return domain.Source{}, false, fmt.Errorf("get source: %w", err)
}

// List returns all configured sources ordered by title and id.
func (r *Repository) List(ctx context.Context) ([]domain.Source, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("source repository is required")
	}

	rows, err := r.db.QueryContext(ctx, selectSourcesSQL()+` ORDER BY title, id`)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()

	var result []domain.Source
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan sources: %w", err)
	}
	return result, nil
}

// Remove deletes a configured source.
func (r *Repository) Remove(ctx context.Context, id string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("source repository is required")
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM sources WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("remove source: %w", err)
	}
	return nil
}

// Purge deletes a source and dependent local data in a safe order.
func (r *Repository) Purge(ctx context.Context, id string) (PurgeResult, error) {
	var purged PurgeResult
	if r == nil || r.db == nil {
		return purged, fmt.Errorf("source repository is required")
	}
	if id == "" {
		return purged, fmt.Errorf("source id is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return purged, fmt.Errorf("begin purge source: %w", err)
	}
	defer tx.Rollback()

	steps := []struct {
		name  string
		query string
		set   func(int64)
	}{
		{
			name:  "source_states",
			query: `DELETE FROM source_states WHERE source_id = ?`,
			set: func(count int64) {
				purged.SourceStates = count
			},
		},
		{
			name:  "messages",
			query: `DELETE FROM messages WHERE source_id = ?`,
			set: func(count int64) {
				purged.Messages = count
			},
		},
		{
			name:  "source_scoped_exclusions",
			query: `DELETE FROM excluded_senders WHERE scope_type = 'source' AND source_id = ?`,
			set: func(count int64) {
				purged.SourceScopedExclusions = count
			},
		},
		{
			name:  "sources",
			query: `DELETE FROM sources WHERE id = ?`,
			set: func(count int64) {
				purged.Sources = count
			},
		},
	}

	for _, step := range steps {
		result, err := tx.ExecContext(ctx, step.query, id)
		if err != nil {
			return purged, fmt.Errorf("purge %s: %w", step.name, err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return purged, fmt.Errorf("count purge %s: %w", step.name, err)
		}
		step.set(count)
	}

	if err := tx.Commit(); err != nil {
		return purged, fmt.Errorf("commit purge source: %w", err)
	}

	return purged, nil
}

func selectSourcesSQL() string {
	return `SELECT id, type, entity_ref, public_username, title, enabled,
	       coalesce(last_error, ''), coalesce(last_error_at, ''), coalesce(error_count, 0), coalesce(paused_until, '')
	       FROM sources`
}

// MarkHealthy clears watcher health state after a successful sync.
func (r *Repository) MarkHealthy(ctx context.Context, id string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("source repository is required")
	}
	if id == "" {
		return fmt.Errorf("source id is required")
	}

	_, err := r.db.ExecContext(
		ctx,
		`UPDATE sources
		 SET last_error = NULL,
		     last_error_at = NULL,
		     error_count = 0,
		     paused_until = NULL,
		     updated_at = datetime('now')
		 WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("mark source healthy: %w", err)
	}
	return nil
}

// MarkUnhealthy records watcher failure state and can disable a broken source.
func (r *Repository) MarkUnhealthy(ctx context.Context, id string, message string, pausedUntil time.Time, disable bool) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("source repository is required")
	}
	if id == "" {
		return fmt.Errorf("source id is required")
	}

	_, err := r.db.ExecContext(
		ctx,
		`UPDATE sources
		 SET last_error = ?,
		     last_error_at = datetime('now'),
		     error_count = coalesce(error_count, 0) + 1,
		     paused_until = ?,
		     enabled = CASE WHEN ? THEN 0 ELSE enabled END,
		     updated_at = datetime('now')
		 WHERE id = ?`,
		nullString(message),
		nullTime(pausedUntil),
		boolInt(disable),
		id,
	)
	if err != nil {
		return fmt.Errorf("mark source unhealthy: %w", err)
	}
	return nil
}

type sourceScanner interface {
	Scan(dest ...any) error
}

func scanSource(scanner sourceScanner) (domain.Source, error) {
	var source domain.Source
	var sourceType string
	var publicUsername sql.NullString
	var title sql.NullString
	var lastError sql.NullString
	var lastErrorAt sql.NullString
	var errorCount int
	var pausedUntil sql.NullString
	var enabled int

	if err := scanner.Scan(
		&source.ID,
		&sourceType,
		&source.EntityRef,
		&publicUsername,
		&title,
		&enabled,
		&lastError,
		&lastErrorAt,
		&errorCount,
		&pausedUntil,
	); err != nil {
		return domain.Source{}, err
	}

	source.Type = domain.SourceType(sourceType)
	source.PublicUsername = publicUsername.String
	source.Title = title.String
	source.Enabled = enabled != 0
	source.LastError = lastError.String
	source.LastErrorAt = parseTime(lastErrorAt.String)
	source.ErrorCount = errorCount
	source.PausedUntil = parseTime(pausedUntil.String)
	return source, nil
}
func nullString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullTime(value time.Time) sql.NullString {
	if value.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: value.UTC().Format(time.RFC3339), Valid: true}
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
