package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	oauthDefaultScope     = "telegram"
	oauthAccessTokenTTL   = 30 * 24 * time.Hour
	oauthAuthorizationTTL = 10 * time.Minute
)

type oauthRuntimeState struct {
	mu      sync.Mutex
	clients map[string]oauthClient
	codes   map[string]oauthAuthorizationCode
	tokens  map[string]oauthAccessToken
}

type oauthClient struct {
	ID                string   `json:"id"`
	Secret            string   `json:"secret"`
	RedirectURIs      []string `json:"redirect_uris"`
	TokenEndpointAuth string   `json:"token_endpoint_auth_method"`
	ClientName        string   `json:"client_name"`
	ClientURI         string   `json:"client_uri"`
	CreatedAtUnix     int64    `json:"created_at_unix"`
}

type oauthAuthorizationCode struct {
	Code                string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	Resource            string
	CodeChallenge       string
	CodeChallengeMethod string
	ExpiresAt           time.Time
}

type oauthAccessToken struct {
	Token     string    `json:"token"`
	ClientID  string    `json:"client_id"`
	Scope     string    `json:"scope"`
	Resource  string    `json:"resource"`
	ExpiresAt time.Time `json:"expires_at"`
}

type oauthRegisterRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

type oauthDiskState struct {
	Clients map[string]oauthClient      `json:"clients"`
	Tokens  map[string]oauthAccessToken `json:"tokens"`
}

var globalOAuthState = &oauthRuntimeState{
	clients: make(map[string]oauthClient),
	codes:   make(map[string]oauthAuthorizationCode),
	tokens:  make(map[string]oauthAccessToken),
}

var oauthStateLoadedOnce = loadOAuthStateOnce()

func loadOAuthStateOnce() bool {
	loadOAuthStateFromDisk()
	return true
}

func registerOAuthHTTPHandlers(mux *http.ServeMux) {
	_ = oauthStateLoadedOnce

	mux.HandleFunc("/.well-known/oauth-protected-resource", handleOAuthProtectedResourceMetadata)
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", handleOAuthProtectedResourceMetadata)
	mux.HandleFunc("/.well-known/oauth-authorization-server", handleOAuthAuthorizationServerMetadata)
	mux.HandleFunc("/.well-known/oauth-authorization-server/mcp", handleOAuthAuthorizationServerMetadata)

	mux.HandleFunc("/oauth/register", handleOAuthRegister)
	mux.HandleFunc("/oauth/authorize", handleOAuthAuthorize)
	mux.HandleFunc("/oauth/token", handleOAuthToken)
}

func oauthProtectedMCPHandler(next http.Handler) http.Handler {
	if oauthOwnerToken() == "" && oauthStaticBearerToken() == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerTokenFromRequest(r)
		if !validateOAuthBearerToken(token, canonicalResourceURI(r)) {
			resourceMetadataURL := publicBaseURL(r) + "/.well-known/oauth-protected-resource"
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+resourceMetadataURL+`"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func handleOAuthProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	base := publicBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{oauthDefaultScope},
		"resource_name":            "Telegram MCP",
		"resource_documentation":   base + "/healthz",
	})
}

func handleOAuthAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	base := publicBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256", "plain"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		"scopes_supported":                      []string{oauthDefaultScope},
	})
}

func handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var input oauthRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON")
		return
	}

	redirectURIs := cleanStringSlice(input.RedirectURIs)
	if len(redirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}

	for _, redirectURI := range redirectURIs {
		if !isAllowedRedirectURI(redirectURI) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri must be https or localhost")
			return
		}
	}

	authMethod := strings.TrimSpace(input.TokenEndpointAuthMethod)
	if authMethod == "" {
		authMethod = "none"
	}
	if authMethod != "none" && authMethod != "client_secret_post" && authMethod != "client_secret_basic" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported token_endpoint_auth_method")
		return
	}

	client := oauthClient{
		ID:                "client_" + randomURLToken(24),
		Secret:            randomURLToken(32),
		RedirectURIs:      redirectURIs,
		TokenEndpointAuth: authMethod,
		ClientName:        strings.TrimSpace(input.ClientName),
		ClientURI:         strings.TrimSpace(input.ClientURI),
		CreatedAtUnix:     time.Now().Unix(),
	}

	globalOAuthState.mu.Lock()
	globalOAuthState.clients[client.ID] = client
	globalOAuthState.mu.Unlock()

	saveOAuthStateToDisk()

	response := map[string]any{
		"client_id":                  client.ID,
		"client_id_issued_at":        client.CreatedAtUnix,
		"redirect_uris":              client.RedirectURIs,
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": client.TokenEndpointAuth,
		"scope":                      oauthDefaultScope,
	}

	if client.TokenEndpointAuth != "none" {
		response["client_secret"] = client.Secret
		response["client_secret_expires_at"] = 0
	}

	writeJSON(w, http.StatusCreated, response)
}

func handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if strings.TrimSpace(r.URL.Query().Get("owner_token")) == "" {
			writeAuthorizeForm(w, r, "")
			return
		}
		completeOAuthAuthorize(w, r, r.URL.Query().Get("owner_token"))

	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form")
			return
		}
		completeOAuthAuthorize(w, r, r.PostForm.Get("owner_token"))

	default:
		methodNotAllowed(w)
	}
}

func completeOAuthAuthorize(w http.ResponseWriter, r *http.Request, ownerToken string) {
	if !constantTimeEqual(ownerToken, oauthOwnerToken()) {
		writeAuthorizeForm(w, r, "Неверный owner token")
		return
	}

	q := r.URL.Query()
	responseType := q.Get("response_type")
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	scope := strings.TrimSpace(q.Get("scope"))
	state := q.Get("state")
	resource := strings.TrimSpace(q.Get("resource"))
	codeChallenge := strings.TrimSpace(q.Get("code_challenge"))
	codeChallengeMethod := strings.TrimSpace(q.Get("code_challenge_method"))

	if scope == "" {
		scope = oauthDefaultScope
	}
	if resource == "" {
		resource = canonicalResourceURI(r)
	}
	if codeChallengeMethod == "" {
		codeChallengeMethod = "plain"
	}

	if responseType != "code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_response_type", "response_type must be code")
		return
	}
	if clientID == "" || redirectURI == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id and redirect_uri are required")
		return
	}
	if codeChallenge == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "PKCE code_challenge is required")
		return
	}
	if codeChallengeMethod != "S256" && codeChallengeMethod != "plain" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "unsupported code_challenge_method")
		return
	}

	globalOAuthState.mu.Lock()
	client, exists := globalOAuthState.clients[clientID]
	globalOAuthState.mu.Unlock()

	if !exists {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "unknown client_id")
		return
	}
	if !stringInSlice(redirectURI, client.RedirectURIs) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri is not registered")
		return
	}

	code := "code_" + randomURLToken(32)
	authCode := oauthAuthorizationCode{
		Code:                code,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               scope,
		State:               state,
		Resource:            resource,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ExpiresAt:           time.Now().Add(oauthAuthorizationTTL),
	}

	globalOAuthState.mu.Lock()
	globalOAuthState.codes[code] = authCode
	globalOAuthState.mu.Unlock()

	redirect, err := url.Parse(redirectURI)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "invalid redirect_uri")
		return
	}

	values := redirect.Query()
	values.Set("code", code)
	if state != "" {
		values.Set("state", state)
	}
	redirect.RawQuery = values.Encode()

	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form")
		return
	}

	grantType := r.PostForm.Get("grant_type")
	code := r.PostForm.Get("code")
	redirectURI := r.PostForm.Get("redirect_uri")
	clientID := r.PostForm.Get("client_id")
	clientSecret := r.PostForm.Get("client_secret")
	codeVerifier := r.PostForm.Get("code_verifier")
	resource := strings.TrimSpace(r.PostForm.Get("resource"))

	if grantType != "authorization_code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code")
		return
	}
	if code == "" || redirectURI == "" || clientID == "" || codeVerifier == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code, redirect_uri, client_id and code_verifier are required")
		return
	}

	globalOAuthState.mu.Lock()
	authCode, codeExists := globalOAuthState.codes[code]
	client, clientExists := globalOAuthState.clients[clientID]
	if codeExists {
		delete(globalOAuthState.codes, code)
	}
	globalOAuthState.mu.Unlock()

	if !clientExists {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		return
	}
	if client.TokenEndpointAuth != "none" && !constantTimeEqual(clientSecret, client.Secret) {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "invalid client_secret")
		return
	}
	if !codeExists {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid code")
		return
	}
	if time.Now().After(authCode.ExpiresAt) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "expired code")
		return
	}
	if authCode.ClientID != clientID || authCode.RedirectURI != redirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id or redirect_uri mismatch")
		return
	}
	if !validatePKCE(codeVerifier, authCode.CodeChallenge, authCode.CodeChallengeMethod) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid code_verifier")
		return
	}

	if resource == "" {
		resource = authCode.Resource
	}
	if resource == "" {
		resource = canonicalResourceURI(r)
	}

	access := oauthAccessToken{
		Token:     "mcp_" + randomURLToken(48),
		ClientID:  clientID,
		Scope:     authCode.Scope,
		Resource:  resource,
		ExpiresAt: time.Now().Add(oauthAccessTokenTTL),
	}

	globalOAuthState.mu.Lock()
	globalOAuthState.tokens[access.Token] = access
	globalOAuthState.mu.Unlock()

	saveOAuthStateToDisk()

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access.Token,
		"token_type":   "Bearer",
		"expires_in":   int(oauthAccessTokenTTL.Seconds()),
		"scope":        access.Scope,
	})
}

func validateOAuthBearerToken(token string, resource string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}

	if static := oauthStaticBearerToken(); static != "" && constantTimeEqual(token, static) {
		return true
	}

	now := time.Now()

	globalOAuthState.mu.Lock()
	access, exists := globalOAuthState.tokens[token]
	if exists && now.After(access.ExpiresAt) {
		delete(globalOAuthState.tokens, token)
		exists = false
	}
	globalOAuthState.mu.Unlock()

	if !exists {
		saveOAuthStateToDisk()
		return false
	}

	if access.Resource == "" || resource == "" {
		return true
	}

	return strings.EqualFold(strings.TrimRight(access.Resource, "/"), strings.TrimRight(resource, "/"))
}

func bearerTokenFromRequest(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func writeAuthorizeForm(w http.ResponseWriter, r *http.Request, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	query := html.EscapeString(r.URL.RawQuery)
	msg := ""
	if message != "" {
		msg = `<p style="color:#b00020">` + html.EscapeString(message) + `</p>`
	}

	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <title>Authorize Telegram MCP</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 560px; margin: 48px auto; line-height: 1.45; }
    input { box-sizing: border-box; width: 100%%; padding: 12px; font-size: 16px; margin: 8px 0 16px; }
    button { padding: 10px 16px; font-size: 16px; cursor: pointer; }
    .box { border: 1px solid #ddd; border-radius: 12px; padding: 20px; }
  </style>
</head>
<body>
  <div class="box">
    <h1>Authorize Telegram MCP</h1>
    <p>Введите owner token из переменной <code>TGMCP_OAUTH_OWNER_TOKEN</code>.</p>
    %s
    <form method="post" action="/oauth/authorize?%s">
      <label>Owner token</label>
      <input name="owner_token" type="password" autocomplete="current-password" autofocus>
      <button type="submit">Authorize</button>
    </form>
  </div>
</body>
</html>`, msg, query)
}

func validatePKCE(verifier string, challenge string, method string) bool {
	if verifier == "" || challenge == "" {
		return false
	}

	switch method {
	case "plain":
		return constantTimeEqual(verifier, challenge)

	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		calculated := base64.RawURLEncoding.EncodeToString(sum[:])
		return constantTimeEqual(calculated, challenge)

	default:
		return false
	}
}

func oauthOwnerToken() string {
	for _, name := range []string{
		"TGMCP_OAUTH_OWNER_TOKEN",
		"MCP_OAUTH_OWNER_TOKEN",
	} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func oauthStaticBearerToken() string {
	for _, name := range []string{
		"TGMCP_BEARER_TOKEN",
		"MCP_BEARER_TOKEN",
	} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func publicBaseURL(r *http.Request) string {
	for _, name := range []string{
		"TGMCP_PUBLIC_BASE_URL",
		"MCP_PUBLIC_BASE_URL",
	} {
		if configured := strings.TrimSpace(os.Getenv(name)); configured != "" {
			return strings.TrimRight(configured, "/")
		}
	}

	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}

	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}

	return scheme + "://" + host
}

func canonicalResourceURI(r *http.Request) string {
	return publicBaseURL(r) + "/mcp"
}

func isAllowedRedirectURI(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}

	if parsed.Scheme == "https" {
		return true
	}

	if parsed.Scheme == "http" {
		host := strings.ToLower(parsed.Hostname())
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}

	return false
}

func cleanStringSlice(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func stringInSlice(value string, values []string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeOAuthError(w http.ResponseWriter, status int, code string, description string) {
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func randomURLToken(bytesCount int) string {
	buf := make([]byte, bytesCount)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func constantTimeEqual(actual string, expected string) bool {
	if len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func oauthStatePath() string {
	for _, name := range []string{
		"TGMCP_OAUTH_STATE_FILE",
		"MCP_OAUTH_STATE_FILE",
	} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "telegram-mcp-server-oauth-state.json"
	}

	return filepath.Join(home, ".config", "telegram-mcp-server", "oauth-state.json")
}

func loadOAuthStateFromDisk() {
	content, err := os.ReadFile(oauthStatePath())
	if err != nil {
		return
	}

	var disk oauthDiskState
	if err := json.Unmarshal(content, &disk); err != nil {
		return
	}

	now := time.Now()

	globalOAuthState.mu.Lock()
	defer globalOAuthState.mu.Unlock()

	if disk.Clients != nil {
		for id, client := range disk.Clients {
			if strings.TrimSpace(id) != "" {
				globalOAuthState.clients[id] = client
			}
		}
	}

	if disk.Tokens != nil {
		for token, access := range disk.Tokens {
			if strings.TrimSpace(token) != "" && now.Before(access.ExpiresAt) {
				globalOAuthState.tokens[token] = access
			}
		}
	}
}

func saveOAuthStateToDisk() {
	path := oauthStatePath()

	globalOAuthState.mu.Lock()
	disk := oauthDiskState{
		Clients: make(map[string]oauthClient, len(globalOAuthState.clients)),
		Tokens:  make(map[string]oauthAccessToken, len(globalOAuthState.tokens)),
	}

	now := time.Now()

	for id, client := range globalOAuthState.clients {
		disk.Clients[id] = client
	}

	for token, access := range globalOAuthState.tokens {
		if now.Before(access.ExpiresAt) {
			disk.Tokens[token] = access
		}
	}
	globalOAuthState.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}

	content, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return
	}

	_ = os.WriteFile(path, append(content, '\n'), 0600)
}
