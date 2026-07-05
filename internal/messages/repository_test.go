package messages

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/storage"
)

func TestRepositorySaveGetRecentSearchAndHide(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedSource(t, db, "telegram:channel:market")

	repo := NewRepository(db)
	message := domain.Message{
		ExternalID:  "telegram:POST:market:10",
		SourceID:    "telegram:channel:market",
		SourceLabel: domain.SourceLabelPost,
		ChatID:      -1001,
		ChatTitle:   "Market",
		MessageID:   10,
		Sender: domain.Sender{
			ID:          123,
			Username:    "SpamUser",
			DisplayName: "@SpamUser | id:123",
		},
		Text: "hello marketplace",
		Link: "https://t.me/market/10",
		Date: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}

	if err := repo.Save(context.Background(), message); err != nil {
		t.Fatalf("save message: %v", err)
	}

	got, ok, err := repo.Get(context.Background(), message.ExternalID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if !ok {
		t.Fatal("message not found")
	}
	if got.Text != message.Text || got.Sender.UsernameNormalized != "spamuser" {
		t.Fatalf("message = %+v", got)
	}

	searched, err := repo.Search(context.Background(), "marketplace", 10, false)
	if err != nil {
		t.Fatalf("search messages: %v", err)
	}
	if len(searched) != 1 {
		t.Fatalf("searched len = %d, want 1", len(searched))
	}

	hidden, err := repo.HideBySender(context.Background(), domain.Sender{ID: 123}, domain.ExclusionScopeGlobal, "")
	if err != nil {
		t.Fatalf("hide by sender: %v", err)
	}
	if hidden != 1 {
		t.Fatalf("hidden = %d, want 1", hidden)
	}

	recent, err := repo.Recent(context.Background(), 10, false)
	if err != nil {
		t.Fatalf("recent messages: %v", err)
	}
	if len(recent) != 0 {
		t.Fatalf("visible recent len = %d, want 0", len(recent))
	}

	recent, err = repo.Recent(context.Background(), 10, true)
	if err != nil {
		t.Fatalf("recent with hidden: %v", err)
	}
	if len(recent) != 1 || !recent[0].HiddenByExclusion {
		t.Fatalf("recent with hidden = %+v", recent)
	}
}

func TestRepositoryRecentSearchFiltered(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedSource(t, db, "market")
	seedSource(t, db, "other")

	repo := NewRepository(db)
	items := []domain.Message{
		{
			ExternalID:  "telegram:POST:market:1",
			SourceID:    "market",
			SourceLabel: domain.SourceLabelPost,
			ChatID:      -1001,
			MessageID:   1,
			Text:        "hello post",
			Date:        time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
		},
		{
			ExternalID:  "telegram:COMMENT:market:2",
			SourceID:    "market",
			SourceLabel: domain.SourceLabelComment,
			ChatID:      -1002,
			MessageID:   2,
			Text:        "hello comment",
			Date:        time.Date(2026, 7, 4, 13, 0, 0, 0, time.UTC),
		},
		{
			ExternalID:  "telegram:POST:other:3",
			SourceID:    "other",
			SourceLabel: domain.SourceLabelPost,
			ChatID:      -1003,
			MessageID:   3,
			Text:        "hello other",
			Date:        time.Date(2026, 7, 4, 14, 0, 0, 0, time.UTC),
		},
	}

	for _, item := range items {
		if err := repo.Save(context.Background(), item); err != nil {
			t.Fatalf("save message: %v", err)
		}
	}

	recent, err := repo.RecentFiltered(context.Background(), 10, false, Filter{
		SourceID:    "market",
		SourceLabel: domain.SourceLabelComment,
	})
	if err != nil {
		t.Fatalf("recent filtered: %v", err)
	}
	if len(recent) != 1 || recent[0].ExternalID != "telegram:COMMENT:market:2" {
		t.Fatalf("recent filtered = %+v", recent)
	}

	searched, err := repo.SearchFiltered(context.Background(), "hello", 10, false, Filter{
		SourceID:    "market",
		SourceLabel: domain.SourceLabelPost,
	})
	if err != nil {
		t.Fatalf("search filtered: %v", err)
	}
	if len(searched) != 1 || searched[0].ExternalID != "telegram:POST:market:1" {
		t.Fatalf("search filtered = %+v", searched)
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
