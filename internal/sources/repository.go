// Package sources stores configured Telegram sources.
package sources

import (
	"context"
	"database/sql"
	"fmt"

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

	row := r.db.QueryRowContext(ctx, `SELECT id, type, entity_ref, public_username, title, enabled FROM sources WHERE id = ?`, id)
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

	rows, err := r.db.QueryContext(ctx, `SELECT id, type, entity_ref, public_username, title, enabled FROM sources ORDER BY title, id`)
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

type sourceScanner interface {
	Scan(dest ...any) error
}

func scanSource(scanner sourceScanner) (domain.Source, error) {
	var source domain.Source
	var sourceType string
	var publicUsername sql.NullString
	var title sql.NullString
	var enabled int

	if err := scanner.Scan(&source.ID, &sourceType, &source.EntityRef, &publicUsername, &title, &enabled); err != nil {
		return domain.Source{}, err
	}

	source.Type = domain.SourceType(sourceType)
	source.PublicUsername = publicUsername.String
	source.Title = title.String
	source.Enabled = enabled != 0
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
