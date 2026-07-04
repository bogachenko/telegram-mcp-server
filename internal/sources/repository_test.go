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
