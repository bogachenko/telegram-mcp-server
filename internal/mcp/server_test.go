package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/telegram-mcp-server/internal/exclusions"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/storage"
)

func TestNewHTTPHandlerHealthz(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestOAuthProtectedResourceMetadataUsesPublicBaseURL(t *testing.T) {
	t.Setenv("TGMCP_PUBLIC_BASE_URL", "https://tg-mcp.elektrosila-avtomatika.store")
	handler := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"resource":"https://tg-mcp.elektrosila-avtomatika.store/mcp"`,
		`"resource_name":"Telegram MCP"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metadata missing %q:\n%s", want, body)
		}
	}
}

func TestMCPHandlerCanRequireBearerToken(t *testing.T) {
	t.Setenv("TGMCP_BEARER_TOKEN", "secret")
	t.Setenv("TGMCP_PUBLIC_BASE_URL", "https://tg-mcp.elektrosila-avtomatika.store")
	handler := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), `resource_metadata="https://tg-mcp.elektrosila-avtomatika.store/.well-known/oauth-protected-resource"`) {
		t.Fatalf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()

	db, err := storage.Open(context.Background(), t.TempDir()+"/test.sqlite")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	sourceRepo := sources.NewRepository(db)
	messageRepo := messages.NewRepository(db)
	exclusionRepo := exclusions.NewRepository(db)
	exclusionService := exclusions.NewService(exclusionRepo, messageRepo)

	return NewHTTPHandler(ServerDeps{
		Sources:          sourceRepo,
		Messages:         messageRepo,
		Exclusions:       exclusionRepo,
		ExclusionService: exclusionService,
	})
}
