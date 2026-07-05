package sources

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/storage"
)

func TestRepositoryUpsertGetListRemove(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	repo := NewRepository(db)
	source := domain.Source{
		ID:             "telegram:channel:market",
		Type:           domain.SourceTypeChannel,
		EntityRef:      "market",
		PublicUsername: "market",
		Title:          "Market",
		Enabled:        true,
	}

	if err := repo.Upsert(context.Background(), source); err != nil {
		t.Fatalf("upsert source: %v", err)
	}

	got, ok, err := repo.Get(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if !ok {
		t.Fatal("source not found")
	}
	if got.Title != source.Title || got.PublicUsername != source.PublicUsername || !got.Enabled {
		t.Fatalf("source = %+v", got)
	}

	source.Title = "Market Updated"
	if err := repo.Upsert(context.Background(), source); err != nil {
		t.Fatalf("upsert source update: %v", err)
	}

	sources, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 1 || sources[0].Title != "Market Updated" {
		t.Fatalf("sources = %+v", sources)
	}

	if err := repo.Remove(context.Background(), source.ID); err != nil {
		t.Fatalf("remove source: %v", err)
	}
	_, ok, err = repo.Get(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("get removed source: %v", err)
	}
	if ok {
		t.Fatal("removed source found")
	}
}

func TestRepositoryPurgeDeletesDependentData(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	repo := NewRepository(db)
	source := domain.Source{
		ID:        "mpwb_chat",
		Type:      domain.SourceTypeGroup,
		EntityRef: "mpwb_chat",
		Enabled:   true,
	}
	if err := repo.Upsert(context.Background(), source); err != nil {
		t.Fatalf("upsert source: %v", err)
	}

	if _, err := db.ExecContext(context.Background(), `INSERT INTO source_states (source_id, last_message_id, last_comment_message_id, updated_at) VALUES (?, 10, 20, datetime('now'))`, source.ID); err != nil {
		t.Fatalf("insert state: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO messages (
		external_id, source_id, source_label, chat_id, message_id, text, hidden_by_exclusion, created_at
	) VALUES (?, ?, 'POST', -1001, 10, 'hello', 0, datetime('now'))`, "telegram:POST:mpwb_chat:10", source.ID); err != nil {
		t.Fatalf("insert message: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO excluded_senders (
		sender_id, reason, scope_type, source_id, created_at, created_by
	) VALUES (123, 'spam', 'source', ?, datetime('now'), 'test')`, source.ID); err != nil {
		t.Fatalf("insert exclusion: %v", err)
	}

	purged, err := repo.Purge(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("purge source: %v", err)
	}
	if purged.Messages != 1 || purged.SourceStates != 1 || purged.SourceScopedExclusions != 1 || purged.Sources != 1 {
		t.Fatalf("purged = %+v", purged)
	}

	_, found, err := repo.Get(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("get source after purge: %v", err)
	}
	if found {
		t.Fatal("source found after purge")
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
