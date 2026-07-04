package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateAppliesSchema(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate twice: %v", err)
	}

	for _, table := range []string{"sources", "messages", "source_states", "excluded_senders", "schema_migrations"} {
		if !tableExists(t, db, table) {
			t.Fatalf("table %s does not exist", table)
		}
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()

	var name string
	err := db.QueryRowContext(context.Background(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if err == nil {
		return true
	}
	if err == sql.ErrNoRows {
		return false
	}
	t.Fatalf("check table %s: %v", table, err)
	return false
}
