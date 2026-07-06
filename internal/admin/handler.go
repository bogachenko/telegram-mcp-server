// Package admin exposes a small local web UI for Telegram MCP maintenance.
package admin

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bogachenko/telegram-mcp-server/internal/domain"
	"github.com/bogachenko/telegram-mcp-server/internal/exclusions"
	"github.com/bogachenko/telegram-mcp-server/internal/messages"
	"github.com/bogachenko/telegram-mcp-server/internal/sources"
	"github.com/bogachenko/telegram-mcp-server/internal/state"
	tgclient "github.com/bogachenko/telegram-mcp-server/internal/telegram"
)

const adminCookieName = "telegram_mcp_admin"

// Deps contains storage-backed services used by the admin UI.
type Deps struct {
	Sources          *sources.Repository
	Messages         *messages.Repository
	Exclusions       *exclusions.Repository
	ExclusionService *exclusions.Service
	States           *state.Repository
	Telegram         *tgclient.Client
}

// NewHandler creates the admin web UI handler.
func NewHandler(deps Deps) http.Handler {
	return &handler{deps: deps}
}

type handler struct {
	deps Deps
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimRight(r.URL.Path, "/")
	if path == "" {
		path = "/admin"
	}

	switch {
	case path == "/admin":
		h.handleIndex(w, r)
	case path == "/admin/login":
		h.handleLogin(w, r)
	case path == "/admin/logout":
		h.handleLogout(w, r)
	case strings.HasPrefix(path, "/admin/api/"):
		if !h.isAuthenticated(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h.handleAPI(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !h.isAuthenticated(r) {
		http.Redirect(w, r, "/admin/login", http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminHTML))
}

func (h *handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeLoginPage(w, "")
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			writeLoginPage(w, "invalid form")
			return
		}

		token := adminOwnerToken()
		if token == "" {
			writeLoginPage(w, "TGMCP_OAUTH_OWNER_TOKEN is not configured")
			return
		}
		if !constantTimeEqual(r.PostForm.Get("owner_token"), token) {
			writeLoginPage(w, "invalid owner token")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     adminCookieName,
			Value:    adminCookieValue(token),
			Path:     "/admin",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isHTTPS(r),
			MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		})
		http.Redirect(w, r, "/admin", http.StatusFound)
	default:
		methodNotAllowed(w)
	}
}

func (h *handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS(r),
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/admin/login", http.StatusFound)
}

func (h *handler) handleAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimRight(r.URL.Path, "/")

	switch {
	case path == "/admin/api/sources":
		h.handleSources(w, r)
	case path == "/admin/api/sources/import":
		h.handleSourcesImport(w, r)
	case strings.HasPrefix(path, "/admin/api/sources/"):
		h.handleSource(w, r)
	case path == "/admin/api/sync":
		h.handleSync(w, r)
	case path == "/admin/api/messages/recent":
		h.handleRecentMessages(w, r)
	case path == "/admin/api/messages/search":
		h.handleSearchMessages(w, r)
	case path == "/admin/api/spam-list":
		h.handleSpamList(w, r)
	case path == "/admin/api/spam-list/from-message":
		h.handleSpamFromMessage(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *handler) handleSources(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.deps.Sources.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sources": mapSources(items)})
	case http.MethodPost:
		var input sourceInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}

		source := domain.Source{
			ID:             strings.TrimSpace(input.ID),
			Type:           domain.SourceType(strings.TrimSpace(input.Type)),
			EntityRef:      strings.TrimSpace(input.EntityRef),
			PublicUsername: cleanUsernameString(input.PublicUsername),
			Title:          strings.TrimSpace(input.Title),
			Enabled:        enabled,
		}
		if source.Type == "" {
			source.Type = domain.SourceTypeGroup
		}

		if err := h.deps.Sources.Upsert(r.Context(), source); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"source": mapSource(source)})
	default:
		methodNotAllowed(w)
	}
}

func (h *handler) handleSourcesImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var input sourceImportInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	imported := make([]domain.Source, 0, len(input.Channels)+len(input.Groups))
	seen := map[string]bool{}
	skipped := 0

	for _, item := range input.Channels {
		source, ok := sourceFromImportItem(domain.SourceTypeChannel, item, enabled)
		if !ok || seen[source.ID] {
			skipped++
			continue
		}
		seen[source.ID] = true
		imported = append(imported, source)
	}

	for _, item := range input.Groups {
		source, ok := sourceFromImportItem(domain.SourceTypeGroup, item, enabled)
		if !ok || seen[source.ID] {
			skipped++
			continue
		}
		seen[source.ID] = true
		imported = append(imported, source)
	}

	for _, source := range imported {
		if err := h.deps.Sources.Upsert(r.Context(), source); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"imported": len(imported),
		"skipped":  skipped,
		"sources":  mapSources(imported),
	})
}

func (h *handler) handleSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}

	id, err := pathTail(r.URL.Path, "/admin/api/sources/")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if boolQuery(r, "purge") {
		purged, err := h.deps.Sources.Purge(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"removed":                  true,
			"purged":                   true,
			"id":                       id,
			"messages":                 purged.Messages,
			"source_states":            purged.SourceStates,
			"source_scoped_exclusions": purged.SourceScopedExclusions,
		})
		return
	}

	if err := h.deps.Sources.Remove(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": true, "purged": false, "id": id})
}

func (h *handler) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if h.deps.Telegram == nil || h.deps.States == nil || h.deps.ExclusionService == nil {
		writeError(w, http.StatusServiceUnavailable, "telegram sync is not configured")
		return
	}

	var input syncInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	items, err := h.deps.Sources.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	selected := filterEnabledSources(items, strings.TrimSpace(input.SourceID))
	if len(selected) == 0 {
		writeError(w, http.StatusNotFound, "source not found or disabled")
		return
	}

	results, err := h.deps.Telegram.SyncSources(r.Context(), selected, tgclient.SyncRepos{
		States:     h.deps.States,
		Messages:   h.deps.Messages,
		Exclusions: h.deps.ExclusionService,
	}, tgclient.SyncOptions{
		SourceID: strings.TrimSpace(input.SourceID),
		Limit:    input.Limit,
		Backfill: input.Backfill,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (h *handler) handleRecentMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	items, err := h.deps.Messages.RecentFiltered(
		r.Context(),
		intQuery(r, "limit", 50),
		boolQuery(r, "include_hidden"),
		messageFilter(r),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": mapMessages(items)})
}

func (h *handler) handleSearchMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}

	items, err := h.deps.Messages.SearchFiltered(
		r.Context(),
		query,
		intQuery(r, "limit", 50),
		boolQuery(r, "include_hidden"),
		messageFilter(r),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": mapMessages(items)})
}

func (h *handler) handleSpamList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.deps.Exclusions.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"senders": mapExcludedSenders(items)})
	case http.MethodPost:
		var input spamInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		result, err := h.deps.ExclusionService.AddSender(r.Context(), exclusions.AddSenderParams{
			Sender: domain.Sender{
				ID:          input.SenderID,
				Username:    strings.TrimSpace(input.Username),
				DisplayName: strings.TrimSpace(input.DisplayName),
			},
			Reason:    strings.TrimSpace(input.Reason),
			Scope:     parseScope(input.ScopeType),
			SourceID:  strings.TrimSpace(input.SourceID),
			CreatedBy: "admin",
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, mapAddResult(result))
	case http.MethodDelete:
		senderID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("sender_id")), 10, 64)
		removed, err := h.deps.ExclusionService.RemoveSender(r.Context(), domain.Sender{
			ID:       senderID,
			Username: strings.TrimSpace(r.URL.Query().Get("username")),
		}, parseScope(r.URL.Query().Get("scope_type")), strings.TrimSpace(r.URL.Query().Get("source_id")))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
	default:
		methodNotAllowed(w)
	}
}

func (h *handler) handleSpamFromMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var input spamFromMessageInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.deps.ExclusionService.AddFromMessage(
		r.Context(),
		strings.TrimSpace(input.MessageExternalID),
		strings.TrimSpace(input.Reason),
		"admin",
		parseScope(input.ScopeType),
		strings.TrimSpace(input.SourceID),
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mapAddResult(result))
}

func (h *handler) isAuthenticated(r *http.Request) bool {
	token := adminOwnerToken()
	if token == "" {
		return false
	}

	cookie, err := r.Cookie(adminCookieName)
	if err != nil {
		return false
	}
	return constantTimeEqual(cookie.Value, adminCookieValue(token))
}

type sourceInput struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	EntityRef      string `json:"entity_ref"`
	PublicUsername string `json:"public_username"`
	Title          string `json:"title"`
	Enabled        *bool  `json:"enabled"`
}

type sourceImportInput struct {
	Channels []sourceImportItem `json:"channels"`
	Groups   []sourceImportItem `json:"groups"`
	Enabled  *bool              `json:"enabled"`
}

type sourceImportItem struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	Username  *string `json:"username"`
	Megagroup bool    `json:"megagroup"`
}

type syncInput struct {
	SourceID string `json:"source_id"`
	Limit    int    `json:"limit"`
	Backfill int    `json:"backfill"`
}

type spamInput struct {
	SenderID    int64  `json:"sender_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Reason      string `json:"reason"`
	ScopeType   string `json:"scope_type"`
	SourceID    string `json:"source_id"`
}

type spamFromMessageInput struct {
	MessageExternalID string `json:"message_external_id"`
	Reason            string `json:"reason"`
	ScopeType         string `json:"scope_type"`
	SourceID          string `json:"source_id"`
}

func adminOwnerToken() string {
	if value := strings.TrimSpace(os.Getenv("TGMCP_OAUTH_OWNER_TOKEN")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("MCP_OAUTH_OWNER_TOKEN"))
}

func adminCookieValue(token string) string {
	sum := sha256.Sum256([]byte("telegram-mcp-admin:" + token))
	return hex.EncodeToString(sum[:])
}

func constantTimeEqual(a string, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func writeLoginPage(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	errorHTML := ""
	if message != "" {
		errorHTML = `<p class="error">` + escapeHTML(message) + `</p>`
	}

	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><title>Telegram MCP Admin Login</title>
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:linear-gradient(135deg,#eef7ff,#f8fbff);color:#102033}.box{width:min(420px,calc(100%% - 32px));padding:28px;border:1px solid #c9e5ff;border-radius:24px;background:rgba(255,255,255,.9);box-shadow:0 24px 80px rgba(35,116,186,.16)}h1{margin:0 0 8px;font-size:26px}p{color:#5a7088;line-height:1.45}input{box-sizing:border-box;width:100%%;padding:13px 14px;border:1px solid #b8d7f4;border-radius:14px;font-size:16px}button{margin-top:14px;width:100%%;padding:13px 16px;border:0;border-radius:14px;background:#1677d2;color:white;font-weight:700;font-size:16px;cursor:pointer}.error{color:#b42318;font-weight:600}code{background:#eef6ff;padding:2px 6px;border-radius:8px}</style>
</head><body><form class="box" method="post" action="/admin/login"><h1>Telegram MCP Admin</h1><p>Введите owner token из <code>TGMCP_OAUTH_OWNER_TOKEN</code>.</p>%s<input name="owner_token" type="password" autocomplete="current-password" autofocus><button type="submit">Войти</button></form></body></html>`, errorHTML)
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
}

func pathTail(path string, prefix string) (string, error) {
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("invalid path")
	}

	value := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if value == "" {
		return "", fmt.Errorf("id is required")
	}

	decoded, err := url.PathUnescape(value)
	if err != nil {
		return "", fmt.Errorf("invalid id")
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" || strings.Contains(decoded, "/") {
		return "", fmt.Errorf("invalid id")
	}
	return decoded, nil
}

func boolQuery(r *http.Request, key string) bool {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func intQuery(r *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func messageFilter(r *http.Request) messages.Filter {
	return messages.Filter{
		SourceID:    strings.TrimSpace(r.URL.Query().Get("source_id")),
		SourceLabel: domain.SourceLabel(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("source_label")))),
	}
}

func parseScope(value string) domain.ExclusionScope {
	if strings.EqualFold(strings.TrimSpace(value), string(domain.ExclusionScopeSource)) {
		return domain.ExclusionScopeSource
	}
	return domain.ExclusionScopeGlobal
}

func filterEnabledSources(items []domain.Source, sourceID string) []domain.Source {
	result := make([]domain.Source, 0, len(items))
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		if sourceID != "" && item.ID != sourceID {
			continue
		}
		result = append(result, item)
	}
	return result
}

func sourceFromImportItem(sourceType domain.SourceType, item sourceImportItem, enabled bool) (domain.Source, bool) {
	username := cleanTelegramUsername(item.Username)
	id := sourceIDFromImport(sourceType, username, item.ID)
	if id == "" {
		return domain.Source{}, false
	}

	entityRef := username
	if entityRef == "" && item.ID > 0 {
		entityRef = strconv.FormatInt(item.ID, 10)
	}
	if entityRef == "" {
		return domain.Source{}, false
	}

	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = id
	}

	return domain.Source{
		ID:             id,
		Type:           sourceType,
		EntityRef:      entityRef,
		PublicUsername: username,
		Title:          title,
		Enabled:        enabled,
	}, true
}

func cleanTelegramUsername(value *string) string {
	if value == nil {
		return ""
	}
	return cleanUsernameString(*value)
}

func cleanUsernameString(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "@")
}

func sourceIDFromImport(sourceType domain.SourceType, username string, telegramID int64) string {
	if username != "" {
		return sanitizeSourceID(username)
	}
	if telegramID > 0 {
		return fmt.Sprintf("%s_%d", sourceType, telegramID)
	}
	return ""
}

func sanitizeSourceID(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
	var builder strings.Builder
	lastUnderscore := false

	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
		if ok {
			builder.WriteRune(r)
			lastUnderscore = r == '_'
			continue
		}

		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}

	return strings.Trim(builder.String(), "_")
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func mapSources(items []domain.Source) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, mapSource(item))
	}
	return result
}

func mapSource(item domain.Source) map[string]any {
	return map[string]any{
		"id":              item.ID,
		"type":            string(item.Type),
		"entity_ref":      item.EntityRef,
		"public_username": item.PublicUsername,
		"title":           item.Title,
		"enabled":         item.Enabled,
		"last_error":      item.LastError,
		"last_error_at":   formatOptionalTime(item.LastErrorAt),
		"error_count":     item.ErrorCount,
		"paused_until":    formatOptionalTime(item.PausedUntil),
	}
}
func mapMessages(items []domain.Message) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, mapMessage(item))
	}
	return result
}

func mapMessage(item domain.Message) map[string]any {
	return map[string]any{"external_id": item.ExternalID, "source_id": item.SourceID, "source_label": string(item.SourceLabel), "chat_id": item.ChatID, "chat_title": item.ChatTitle, "message_id": item.MessageID, "sender": mapSender(item.Sender), "text": item.Text, "link": item.Link, "date": item.Date.UTC().Format(time.RFC3339), "hidden_by_exclusion": item.HiddenByExclusion}
}

func mapSender(item domain.Sender) map[string]any {
	return map[string]any{"id": item.ID, "username": item.Username, "username_normalized": item.UsernameNormalized, "display_name": item.DisplayName}
}

func mapExcludedSenders(items []domain.ExcludedSender) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, mapExcludedSender(item))
	}
	return result
}

func mapExcludedSender(item domain.ExcludedSender) map[string]any {
	return map[string]any{"id": item.ID, "sender_id": item.SenderID, "username": item.Username, "username_normalized": item.UsernameNormalized, "display_name": item.DisplayName, "reason": item.Reason, "scope_type": string(item.Scope), "source_id": item.SourceID, "evidence_message_external_id": item.Evidence.ExternalID, "evidence_message_text": item.Evidence.Text, "evidence_message_link": item.Evidence.Link, "evidence_source_id": item.Evidence.SourceID, "evidence_source_title": item.Evidence.SourceTitle, "created_at": item.CreatedAt.UTC().Format(time.RFC3339), "created_by": item.CreatedBy}
}

func mapAddResult(result exclusions.AddResult) map[string]any {
	return map[string]any{"excluded_sender": mapExcludedSender(result.Sender), "already_excluded": result.AlreadyExcluded, "hidden_existing_messages": result.HiddenExistingMessages}
}

func escapeHTML(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&#34;",
		"'", "&#39;",
	).Replace(value)
}

const adminHTML = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <title>Telegram MCP Admin</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    :root{--blue:#1677d2;--text:#102033;--muted:#60758c;--border:#c9e5ff;--danger:#c0342b}*{box-sizing:border-box}body{margin:0;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:radial-gradient(circle at top left,#dff1ff,transparent 38%),linear-gradient(135deg,#eef7ff,#f8fbff);color:var(--text)}header{position:sticky;top:0;z-index:4;backdrop-filter:blur(18px);background:rgba(245,250,255,.86);border-bottom:1px solid var(--border)}.wrap,main{max-width:1180px;margin:0 auto;padding:18px}.top{display:flex;align-items:center;justify-content:space-between;gap:16px}h1{margin:0;font-size:25px}h2{margin:0 0 14px;font-size:19px}.subtitle,.small{color:var(--muted);font-size:13px}main{display:grid;gap:18px}.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}.card{border:1px solid var(--border);border-radius:22px;background:rgba(255,255,255,.88);box-shadow:0 18px 60px rgba(31,111,180,.12);padding:18px}.row{display:flex;flex-wrap:wrap;gap:10px;align-items:center}input,select,textarea{width:100%;padding:10px 12px;border-radius:12px;border:1px solid #bad8f2;background:white;color:var(--text);font:inherit}textarea{min-height:120px;resize:vertical;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:13px}label{font-size:13px;color:var(--muted);font-weight:700;display:grid;gap:6px}.form-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}button{border:0;border-radius:12px;background:var(--blue);color:white;font-weight:800;padding:10px 13px;cursor:pointer}button.secondary{background:#e8f4ff;color:#105c9d}button.danger{background:#fff1f0;color:var(--danger);border:1px solid #ffd0ca}.pill{display:inline-flex;border-radius:999px;border:1px solid #cfe7ff;background:#f1f8ff;color:#145f9f;padding:4px 9px;font-size:12px;font-weight:800}table{width:100%;border-collapse:collapse}th,td{padding:10px 8px;border-bottom:1px solid #e6f1fb;text-align:left;vertical-align:top;font-size:14px}th{color:var(--muted);font-size:12px;text-transform:uppercase}.actions{display:flex;flex-wrap:wrap;gap:7px}.messages{display:grid;gap:10px}.message{border:1px solid #e0eefb;border-radius:16px;padding:12px;background:#fbfdff}.message-head{display:flex;justify-content:space-between;gap:10px;color:var(--muted);font-size:12px;margin-bottom:7px}.message-text{white-space:pre-wrap;line-height:1.4}.status{position:fixed;right:18px;bottom:18px;max-width:560px;padding:12px 14px;border-radius:16px;background:#102033;color:white;box-shadow:0 18px 50px rgba(0,0,0,.22);display:none}.status.error{background:#8f1d16}@media(max-width:900px){.grid,.form-grid{grid-template-columns:1fr}.top{align-items:flex-start;flex-direction:column}}
  </style>
</head>
<body>
<header><div class="wrap top"><div><h1>Telegram MCP Admin</h1><div class="subtitle">Чаты, сообщения и локальный spam-list</div></div><form method="post" action="/admin/logout"><button class="secondary" type="submit">Выйти</button></form></div></header>
<main>
<div class="card" style="display:flex;gap:10px;align-items:center;justify-content:space-between;flex-wrap:wrap">
  <div>
    <h2 style="margin:0">Панель управления</h2>
    <div class="small">Чаты отдельно от сообщений и spam-list.</div>
  </div>
  <div class="row">
    <button id="tab-sources" onclick="showAdminPage('sources')">Чаты</button>
    <button id="tab-messages" class="secondary" onclick="showAdminPage('messages')">Сообщения и spam-list</button>
  </div>
</div>
<section id="admin-page-sources" class="card admin-page"><h2>Чаты / источники</h2>
<div class="form-grid"><label>ID<input id="source-id" placeholder="mpwb_chat"></label><label>Тип<select id="source-type"><option value="group">group</option><option value="channel">channel</option></select></label><label>Entity ref<input id="source-entity" placeholder="mpwb_chat или numeric id"></label><label>Title<input id="source-title" placeholder="Название"></label><label>Public username<input id="source-public" placeholder="без @, можно пусто"></label><label>Enabled<select id="source-enabled"><option value="true">true</option><option value="false">false</option></select></label></div>
<div class="row" style="margin-top:12px"><button onclick="saveSource()">Добавить / обновить</button><button class="secondary" onclick="loadAll()">Обновить</button><button class="secondary" onclick="syncAll()">Обновить все</button></div>
<div style="margin-top:16px"><label>Массовый импорт JSON<textarea id="sources-import-json" placeholder='{"channels":[...],"groups":[...]}'></textarea></label><div class="row" style="margin-top:10px"><button class="secondary" onclick="importSources()">Импортировать JSON</button><span class="small">Поддерживает массивы channels и groups.</span></div></div>
<div style="overflow:auto;margin-top:14px"><table><thead><tr><th>ID</th><th>Тип</th><th>Entity</th><th>Title</th><th>Статус</th><th>Действия</th></tr></thead><tbody id="sources-body"></tbody></table></div></section>
<section id="admin-page-messages" class="grid admin-page" style="display:none">
<div class="card"><h2>Сообщения</h2><div class="form-grid"><label>Source<select id="message-source"></select></label><label>Label<select id="message-label"><option value="">all</option><option value="POST">POST</option><option value="COMMENT">COMMENT</option></select></label><label>Limit<input id="message-limit" type="number" value="30" min="1" max="200"></label></div><div class="row" style="margin-top:10px"><input id="message-query" placeholder="Поиск по тексту" style="flex:1;min-width:220px"><button onclick="loadRecent()">Последние</button><button class="secondary" onclick="searchMessages()">Искать</button></div><div id="messages" class="messages" style="margin-top:12px"></div></div>
<div class="card"><h2>Spam list</h2><div class="form-grid"><label>Sender ID<input id="spam-sender-id" placeholder="8303990114"></label><label>Username<input id="spam-username" placeholder="@username"></label><label>Scope<select id="spam-scope"><option value="global">global</option><option value="source">source</option></select></label><label>Source ID<input id="spam-source-id" placeholder="mpwb_chat"></label><label style="grid-column:span 2;">Reason<input id="spam-reason" placeholder="spam / irrelevant / noisy"></label></div><div class="row" style="margin-top:10px"><button onclick="addSpamSender()">Добавить в спам</button><button class="secondary" onclick="loadSpam()">Обновить</button></div><div style="overflow:auto;margin-top:14px"><table><thead><tr><th>Sender</th><th>Scope</th><th>Reason</th><th>Действия</th></tr></thead><tbody id="spam-body"></tbody></table></div></div>
</section>
</main>
<div id="status" class="status"></div>
<script>
let sources=[];
function showAdminPage(page){
  const sourcesPage=document.getElementById('admin-page-sources');
  const messagesPage=document.getElementById('admin-page-messages');
  const sourcesTab=document.getElementById('tab-sources');
  const messagesTab=document.getElementById('tab-messages');

  if(sourcesPage)sourcesPage.style.display=page==='sources'?'':'none';
  if(messagesPage)messagesPage.style.display=page==='messages'?'':'none';

  if(sourcesTab)sourcesTab.className=page==='sources'?'':'secondary';
  if(messagesTab)messagesTab.className=page==='messages'?'':'secondary';

  if(page==='messages'){
    loadRecent().catch(err=>show(err.message,true));
    loadSpam().catch(err=>show(err.message,true));
  }
}
function api(path,options){options=options||{};options.headers=Object.assign({'Content-Type':'application/json'},options.headers||{});return fetch(path,options).then(async(res)=>{if(res.status===401){location.href='/admin/login';throw new Error('unauthorized')}const text=await res.text();const data=text?JSON.parse(text):{};if(!res.ok)throw new Error(data.error||res.statusText);return data})}
function show(message,isError){const box=document.getElementById('status');box.textContent=message;box.className='status'+(isError?' error':'');box.style.display='block';clearTimeout(window._statusTimer);window._statusTimer=setTimeout(()=>box.style.display='none',4200)}
function esc(v){return String(v==null?'':v).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function js(v){return String(v==null?'':v).replace(/\\/g,'\\\\').replace(/'/g,"\\'")}
function qs(params){return Object.entries(params).filter(([,v])=>v!==''&&v!=null).map(([k,v])=>encodeURIComponent(k)+'='+encodeURIComponent(v)).join('&')}
function sourceStatus(s){
  let text=s.enabled?'enabled':'disabled';
  if(s.paused_until)text+=' · paused';
  if(s.error_count)text+=' · errors '+s.error_count;
  let html='<span class="pill">'+esc(text)+'</span>';
  if(s.paused_until)html+='<div class="small">paused until: '+esc(s.paused_until)+'</div>';
  if(s.last_error)html+='<div class="small">'+esc(s.last_error)+'</div>';
  return html;
}
async function loadAll(){await loadSources()}
async function loadSources(){const data=await api('/admin/api/sources');sources=data.sources||[];renderSources();renderSourceSelects()}
function renderSources(){const body=document.getElementById('sources-body');body.innerHTML=sources.map(s=>'<tr><td><b>'+esc(s.id)+'</b></td><td>'+esc(s.type)+'</td><td>'+esc(s.entity_ref)+'</td><td>'+esc(s.title)+'</td><td>'+sourceStatus(s)+'</td><td><div class="actions"><button class="secondary" onclick="syncSource(\''+js(s.id)+'\',0)">Обновить</button><button class="secondary" onclick="syncSource(\''+js(s.id)+'\',20)">Загрузить последние 20</button><button class="danger" onclick="removeSource(\''+js(s.id)+'\',false)">Удалить из списка</button><button class="danger" onclick="removeSource(\''+js(s.id)+'\',true)">Удалить с данными</button></div></td></tr>').join('')}
function renderSourceSelects(){const select=document.getElementById('message-source');const current=select.value;select.innerHTML='<option value="">all</option>'+sources.map(s=>'<option value="'+esc(s.id)+'">'+esc(s.id)+'</option>').join('');select.value=current}
async function saveSource(){const payload={id:document.getElementById('source-id').value.trim(),type:document.getElementById('source-type').value,entity_ref:document.getElementById('source-entity').value.trim(),public_username:document.getElementById('source-public').value.trim(),title:document.getElementById('source-title').value.trim(),enabled:document.getElementById('source-enabled').value==='true'};await api('/admin/api/sources',{method:'POST',body:JSON.stringify(payload)});show('Источник сохранён');await loadSources()}
async function importSources(){const raw=document.getElementById('sources-import-json').value.trim();if(!raw)return show('Вставь JSON с channels/groups',true);let parsed;try{parsed=JSON.parse(raw)}catch(err){return show('Некорректный JSON: '+err.message,true)}const data=await api('/admin/api/sources/import',{method:'POST',body:JSON.stringify(parsed)});show('Импортировано: '+(data.imported||0)+', пропущено: '+(data.skipped||0));await loadSources()}
async function removeSource(id,purge){if(!confirm((purge?'Удалить источник вместе с messages/state/source-spam? ':'')+id))return;const data=await api('/admin/api/sources/'+encodeURIComponent(id)+'?purge='+purge,{method:'DELETE'});show(purge?('Удалено с данными, messages: '+(data.messages||0)):'Источник удалён из списка');await loadAll()}
async function syncSource(id,backfill){const data=await api('/admin/api/sync',{method:'POST',body:JSON.stringify({source_id:id,backfill:backfill,limit:200})});show('Sync: '+JSON.stringify(data.results||data));await loadRecent()}
async function syncAll(){const data=await api('/admin/api/sync',{method:'POST',body:JSON.stringify({limit:200})});show('Sync all: '+JSON.stringify(data.results||data));await loadRecent()}
async function loadRecent(){const params={source_id:document.getElementById('message-source').value,source_label:document.getElementById('message-label').value,limit:document.getElementById('message-limit').value||'30'};const data=await api('/admin/api/messages/recent?'+qs(params));renderMessages(data.messages||[])}
async function searchMessages(){const q=document.getElementById('message-query').value.trim();if(!q)return show('Введите поисковый запрос',true);const params={q:q,source_id:document.getElementById('message-source').value,source_label:document.getElementById('message-label').value,limit:document.getElementById('message-limit').value||'30'};const data=await api('/admin/api/messages/search?'+qs(params));renderMessages(data.messages||[])}
function renderMessages(items){const box=document.getElementById('messages');box.innerHTML=items.map(m=>{const sender=m.sender||{};return '<div class="message"><div class="message-head"><span><b>'+esc(m.source_id)+'</b> #'+esc(m.message_id)+' · '+esc(m.source_label)+'</span><span>'+esc(m.date)+'</span></div><div class="small">Sender: '+esc(sender.display_name||sender.username||sender.id)+' · id:'+esc(sender.id||'')+'</div><div class="message-text">'+esc(m.text||'')+'</div><div class="actions" style="margin-top:10px"><button class="danger" onclick="spamFromMessage(\''+js(m.external_id)+'\')">В спам по сообщению</button>'+(m.link?'<a class="small" href="'+esc(m.link)+'" target="_blank">Открыть в Telegram</a>':'')+'</div></div>'}).join('')||'<p class="small">Нет сообщений</p>'}
async function loadSpam(){const data=await api('/admin/api/spam-list');renderSpam(data.senders||[])}
function renderSpam(items){const body=document.getElementById('spam-body');body.innerHTML=items.map(s=>'<tr><td><b>'+esc(s.display_name||s.username||s.sender_id)+'</b><div class="small">id:'+esc(s.sender_id||'')+' @'+esc(s.username_normalized||s.username||'')+'</div></td><td>'+esc(s.scope_type)+(s.source_id?'<div class="small">'+esc(s.source_id)+'</div>':'')+'</td><td>'+esc(s.reason||'')+'</td><td><button class="danger" onclick="removeSpam(\''+js(s.sender_id||'')+'\',\''+js(s.username||s.username_normalized||'')+'\',\''+js(s.scope_type||'global')+'\',\''+js(s.source_id||'')+'\')">Удалить</button></td></tr>').join('')||'<tr><td colspan="4" class="small">Spam list пуст</td></tr>'}
async function addSpamSender(){const payload={sender_id:Number(document.getElementById('spam-sender-id').value||0),username:document.getElementById('spam-username').value.trim(),reason:document.getElementById('spam-reason').value.trim(),scope_type:document.getElementById('spam-scope').value,source_id:document.getElementById('spam-source-id').value.trim()};await api('/admin/api/spam-list',{method:'POST',body:JSON.stringify(payload)});show('Sender добавлен в spam-list');await loadSpam();await loadRecent()}
async function spamFromMessage(externalID){const reason=prompt('Причина','spam');if(reason===null)return;await api('/admin/api/spam-list/from-message',{method:'POST',body:JSON.stringify({message_external_id:externalID,reason:reason})});show('Автор сообщения добавлен в spam-list');await loadSpam();await loadRecent()}
async function removeSpam(senderID,username,scope,sourceID){const params={sender_id:senderID,username:username,scope_type:scope,source_id:sourceID};await api('/admin/api/spam-list?'+qs(params),{method:'DELETE'});show('Sender удалён из spam-list');await loadSpam()}
loadAll().catch(err=>show(err.message,true));
</script>
</body>
</html>`
