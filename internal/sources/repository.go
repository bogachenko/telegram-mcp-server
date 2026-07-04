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
