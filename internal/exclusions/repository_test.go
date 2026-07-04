package exclusions

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/storage"
)

func TestServiceAddFromMessageStoresEvidenceAndHidesMessages(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedSource(t, db, "telegram:channel:market")

	messageRepo := messages.NewRepository(db)
	exclusionRepo := NewRepository(db)
	service := NewService(exclusionRepo, messageRepo)

	message := domain.Message{
		ExternalID:  "telegram:COMMENT:market:77",
		SourceID:    "telegram:channel:market",
		SourceLabel: domain.SourceLabelComment,
		ChatID:      -1001,
		ChatTitle:   "Market",
		MessageID:   77,
		Sender: domain.Sender{
			ID:          123,
			Username:    "SpamUser",
			DisplayName: "@SpamUser | id:123",
		},
		Text: "spam evidence text",
		Link: "https://t.me/c/1/77",
		Date: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}
	if err := messageRepo.Save(context.Background(), message); err != nil {
		t.Fatalf("save message: %v", err)
	}

	result, err := service.AddFromMessage(context.Background(), message.ExternalID, "spam", "test", domain.ExclusionScopeGlobal, "")
	if err != nil {
		t.Fatalf("add from message: %v", err)
	}
	if result.AlreadyExcluded {
		t.Fatal("sender unexpectedly already excluded")
	}
	if result.HiddenExistingMessages != 1 {
		t.Fatalf("hidden messages = %d, want 1", result.HiddenExistingMessages)
	}
	if result.Sender.Evidence.Text != message.Text || result.Sender.Evidence.ExternalID != message.ExternalID {
		t.Fatalf("evidence = %+v", result.Sender.Evidence)
	}

	excluded, err := service.IsExcluded(context.Background(), domain.Sender{ID: 123}, "telegram:channel:market")
	if err != nil {
		t.Fatalf("is excluded: %v", err)
	}
	if !excluded {
		t.Fatal("sender is not excluded")
	}

	again, err := service.AddFromMessage(context.Background(), message.ExternalID, "spam again", "test", domain.ExclusionScopeGlobal, "")
	if err != nil {
		t.Fatalf("add existing from message: %v", err)
	}
	if !again.AlreadyExcluded {
		t.Fatal("sender should already be excluded")
	}
}

func TestServiceRemoveSender(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	messageRepo := messages.NewRepository(db)
	exclusionRepo := NewRepository(db)
	service := NewService(exclusionRepo, messageRepo)

	_, err := service.AddSender(context.Background(), AddSenderParams{
		Sender: domain.Sender{ID: 123, Username: "spam"},
		Reason: "spam",
		Scope:  domain.ExclusionScopeGlobal,
	})
	if err != nil {
		t.Fatalf("add sender: %v", err)
	}

	removed, err := service.RemoveSender(context.Background(), domain.Sender{ID: 123}, domain.ExclusionScopeGlobal, "")
	if err != nil {
		t.Fatalf("remove sender: %v", err)
	}
	if !removed {
		t.Fatal("sender not removed")
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
