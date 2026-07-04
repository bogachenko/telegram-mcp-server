package state

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/storage"
)

func TestRepositorySaveGet(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedSource(t, db, "telegram:channel:market")

	repo := NewRepository(db)
	want := domain.SourceState{
		SourceID:             "telegram:channel:market",
		LastMessageID:        123,
		LastCommentMessageID: 456,
	}

	if err := repo.Save(context.Background(), want); err != nil {
		t.Fatalf("save state: %v", err)
	}

	got, ok, err := repo.Get(context.Background(), want.SourceID)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if !ok {
		t.Fatal("state not found")
	}
	if got.LastMessageID != want.LastMessageID || got.LastCommentMessageID != want.LastCommentMessageID {
		t.Fatalf("state = %+v", got)
	}
}

func seedSource(t *testing.T, db *sql.DB, id string) {
	t.Helper()

	repo := sources.NewRepository(db)
	if err := repo.Upsert(context.Background(), domain.Source{ID: id, Type: domain.SourceTypeChannel, EntityRef: "market", Enabled: true}); err != nil {
		t.Fatalf("seed source: %v", err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
