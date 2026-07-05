package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/telegram-mcp-server/internal/exclusions"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/storage"
)

func TestAdminAPIRequiresLogin(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/api/sources", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAdminLoginAndSourcesAPI(t *testing.T) {
	t.Setenv("TGMCP_OAUTH_OWNER_TOKEN", "owner-secret")
	h := newTestHandler(t)
	cookie := loginCookie(t, h, "owner-secret")
	body, _ := json.Marshal(map[string]any{"id": "mpwb_chat", "type": "group", "entity_ref": "mpwb_chat", "title": "Chat"})
	req := httptest.NewRequest(http.MethodPost, "/admin/api/sources", bytes.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post status = %d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/admin/api/sources", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":"mpwb_chat"`) {
		t.Fatalf("missing source: %s", rec.Body.String())
	}
}

func loginCookie(t *testing.T, h http.Handler, token string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("owner_token="+token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == adminCookieName {
			return c
		}
	}
	t.Fatal("admin cookie not set")
	return nil
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	db, err := storage.Open(context.Background(), t.TempDir()+"/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	sr := sources.NewRepository(db)
	mr := messages.NewRepository(db)
	er := exclusions.NewRepository(db)
	return NewHandler(Deps{Sources: sr, Messages: mr, Exclusions: er, ExclusionService: exclusions.NewService(er, mr)})
}
