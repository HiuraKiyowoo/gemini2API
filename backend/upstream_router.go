package main

// Upstream client selection.
//
// The gateway can talk to Google through two different upstreams at once:
//
//   - cookie transport (*QwenClient): the reverse-engineered
//     gemini.google.com BardChatUi path, authenticated with browser cookies
//     (__Secure-1PSID / SAPISID + SAPISIDHASH). Unchanged behaviour.
//   - Code Assist transport (*upstream.CodeAssistClient): pure HTTP OAuth
//     against cloudcode-pa.googleapis.com/v1internal using a Google OAuth
//     refresh token (the gemini-cli path).
//
// upstreamRouter picks the transport per account, so OAuth accounts never fall
// back into the cookie path and cookie accounts keep working exactly as before.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gemini2api-go/upstream"
)

// UpstreamClient is the seam every request path uses to reach Google.
type UpstreamClient interface {
	CreateChat(ctx context.Context, token, model, chatType string) (string, error)
	DeleteChat(ctx context.Context, token, chatID string) bool
	StreamChat(ctx context.Context, token, chatID string, payload map[string]any, onEvent func(upstream.Event) error) error
	StreamChatLegacy(ctx context.Context, token, chatID string, payload map[string]any, onEvent func(UpstreamEvent) error) error
	requestJSON(ctx context.Context, method, path, token string, body any, timeout time.Duration) (int, string, error)
	SignIn(ctx context.Context, email, password string) (*SignInResult, error)
	PostChatCompletionOnce(ctx context.Context, token, chatID string, payload map[string]any, timeout time.Duration) (int, string, error)
	GetVisionTaskStatus(ctx context.Context, token, taskID string, timeout time.Duration) (int, string, error)
	GetChatDetail(ctx context.Context, token, chatID string, timeout time.Duration) (int, string, error)
	ListChats(ctx context.Context, token string, limit int) ([]map[string]any, error)
	ListModelsFromPool(ctx context.Context) ([]map[string]any, error)
	VerifyToken(ctx context.Context, token string) bool
	VerifyTokenDetail(ctx context.Context, token string) TokenVerifyResult
}

var (
	_ UpstreamClient = (*QwenClient)(nil)
	_ UpstreamClient = (*upstreamRouter)(nil)
)

// upstreamRouter dispatches one account's traffic to the right transport.
type upstreamRouter struct {
	cookie   *QwenClient
	oauth    *upstream.CodeAssistClient
	pool     *AccountPool
	logger   *slog.Logger
	settings Settings

	mu         sync.Mutex
	refreshByF map[string]string // oauth file path -> refresh token
}

// NewUpstreamClient builds the client-selection point used by App. When an
// OAuth Code Assist credential is available it becomes a real transport
// alongside the legacy cookie client.
func NewUpstreamClient(pool *AccountPool, settings Settings, logger *slog.Logger) UpstreamClient {
	cookie := NewQwenClient(pool, settings, logger)
	router := &upstreamRouter{cookie: cookie, pool: pool, logger: logger, settings: settings, refreshByF: map[string]string{}}

	oauthFile := settings.resolvedCodeAssistOAuthFile()
	if !settings.CodeAssistEnabled {
		if oauthFile != "" {
			logInfo(logger, context.Background(), "Code Assist transport dinonaktifkan lewat konfigurasi", "oauth_file", filepath.Base(oauthFile))
		}
		return router
	}
	if oauthFile == "" {
		logInfo(logger, context.Background(), "Code Assist transport tidak aktif: tidak ada data/google_oauth.json")
		return router
	}
	transport, err := upstream.NewCodeAssistClient(upstream.CodeAssistOptions{
		TokenURL:      strings.TrimSpace(os.Getenv("CODE_ASSIST_TOKEN_URL")),
		Endpoint:      strings.TrimSpace(os.Getenv("CODE_ASSIST_ENDPOINT")),
		ClientID:      strings.TrimSpace(os.Getenv("CODE_ASSIST_CLIENT_ID")),
		ClientSecret:  strings.TrimSpace(os.Getenv("CODE_ASSIST_CLIENT_SECRET")),
		OAuthFile:     oauthFile,
		Attempts:      maxInt(settings.CodeAssistAttempts, 1),
		RetryDelay:    time.Duration(maxInt(settings.CodeAssistRetryDelaySeconds, 1)) * time.Second,
		ModelCooldown: time.Duration(maxInt(settings.CodeAssistModelCooldownSeconds, 1)) * time.Second,
		RetryBudget:   time.Duration(maxInt(settings.CodeAssistRetryBudgetSeconds, 1)) * time.Second,
	})
	if err != nil {
		logError(logger, context.Background(), "Gagal menyiapkan Code Assist transport", "oauth_file", filepath.Base(oauthFile), "error", err)
		return router
	}
	transport.RefreshTokenFor = router.refreshTokenFor
	router.oauth = transport
	logInfo(logger, context.Background(), "Code Assist transport aktif (OAuth cloudcode-pa)",
		"oauth_file", filepath.Base(oauthFile), "accounts", len(router.oauthAccounts()))
	return router
}

// ---- account lookup ----

// oauthAccounts lists accounts that should use the Code Assist transport.
func (r *upstreamRouter) oauthAccounts() []Account {
	if r.pool == nil {
		return nil
	}
	out := []Account{}
	for _, acc := range r.pool.Snapshot() {
		if acc.IsOAuth() {
			out = append(out, acc)
		}
	}
	return out
}

// usesCodeAssist reports whether a token handle belongs to an OAuth account.
func (r *upstreamRouter) usesCodeAssist(token string) bool {
	if r.oauth == nil {
		return false
	}
	trimmed := strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(trimmed), oauthTokenPrefix) {
		return true
	}
	if r.pool == nil {
		return false
	}
	for _, acc := range r.pool.Snapshot() {
		if acc.Token != "" && acc.Token == trimmed {
			return acc.IsOAuth()
		}
	}
	return false
}

// accountFor resolves the pool account behind a token handle.
func (r *upstreamRouter) accountFor(token string) *Account {
	if r.pool == nil {
		return nil
	}
	trimmed := strings.TrimSpace(token)
	snapshot := r.pool.Snapshot()
	for i, acc := range snapshot {
		if acc.Token != "" && acc.Token == trimmed {
			return &snapshot[i]
		}
	}
	for i, acc := range snapshot {
		if acc.IsOAuth() && (acc.Email == trimmed || strings.TrimPrefix(trimmed, oauthTokenPrefix) == acc.Email) {
			return &snapshot[i]
		}
	}
	return nil
}

// refreshTokenFor maps a token handle onto the account's OAuth refresh token.
// An empty return lets the transport fall back to its own file-loaded token.
func (r *upstreamRouter) refreshTokenFor(token string) string {
	acc := r.accountFor(token)
	if acc == nil {
		return ""
	}
	if inline := strings.TrimSpace(acc.OAuthRefresh); inline != "" {
		return inline
	}
	file := strings.TrimSpace(acc.OAuthFile)
	if file == "" {
		return ""
	}
	// Account records store the bare file name (e.g. "google_oauth.json"); it is
	// only meaningful relative to the data directory. Without this join the
	// process tries to open it against the CWD and fails with ENOENT.
	if !filepath.IsAbs(file) {
		dir := strings.TrimSpace(r.settings.DataDir)
		if dir == "" {
			dir = "data"
		}
		file = filepath.Join(dir, file)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.refreshByF[file]; ok {
		return cached
	}
	refresh, err := upstream.LoadRefreshTokenFile(file)
	if err != nil {
		logWarn(r.logger, context.Background(), "Gagal membaca berkas OAuth Code Assist akun", "file", filepath.Base(file), "error", err)
		r.refreshByF[file] = ""
		return ""
	}
	r.refreshByF[file] = refresh
	return refresh
}

// ---- UpstreamClient implementation ----

func (r *upstreamRouter) selectClient(token string) (upstream.ChatClient, bool) {
	if r.usesCodeAssist(token) {
		return r.oauth, true
	}
	return r.cookie, false
}

func (r *upstreamRouter) CreateChat(ctx context.Context, token, model, chatType string) (string, error) {
	client, _ := r.selectClient(token)
	return client.CreateChat(ctx, token, model, chatType)
}

func (r *upstreamRouter) DeleteChat(ctx context.Context, token, chatID string) bool {
	client, _ := r.selectClient(token)
	return client.DeleteChat(ctx, token, chatID)
}

func (r *upstreamRouter) StreamChatLegacy(ctx context.Context, token, chatID string, payload map[string]any, onEvent func(UpstreamEvent) error) error {
	client, isOAuth := r.selectClient(token)
	if !isOAuth {
		return r.cookie.StreamChatLegacy(ctx, token, chatID, payload, onEvent)
	}
	model := r.oauthModelName(payload)
	logInfo(r.logger, ctx, "Mulai aliran Code Assist", "chat_id", chatID, "requested_model", model, "resolved_model", upstream.ResolveCodeAssistModel(model))
	err := client.StreamChat(ctx, token, chatID, payload, func(evt upstream.Event) error {
		return onEvent(UpstreamEvent{
			Type:          evt.Type,
			Phase:         evt.Phase,
			Content:       evt.Content,
			ReasoningText: evt.ReasoningText,
			Status:        evt.Status,
			Extra:         evt.Extra,
			Raw:           evt.Raw,
		})
	})
	if err != nil {
		if status := upstream.StatusCodeOf(err); status != 0 {
			return fmt.Errorf("code assist HTTP %d: %w", status, err)
		}
	}
	return err
}

// StreamChat is the interface-shaped entry point: it adapts the legacy
// UpstreamEvent callback into the shared upstream.Event contract.
func (r *upstreamRouter) StreamChat(ctx context.Context, token, chatID string, payload map[string]any, onEvent func(upstream.Event) error) error {
	return r.StreamChatLegacy(ctx, token, chatID, payload, func(evt UpstreamEvent) error {
		return onEvent(upstream.Event{
			Type:          evt.Type,
			Phase:         evt.Phase,
			Content:       evt.Content,
			ReasoningText: evt.ReasoningText,
			Status:        evt.Status,
			Extra:         evt.Extra,
			Raw:           evt.Raw,
		})
	})
}

// oauthModelName reports the model name the caller asked for, preferring the
// original public id over the gateway-internal cookie-path alias.
func (r *upstreamRouter) oauthModelName(payload map[string]any) string {
	if requested := strings.TrimSpace(anyString(payload["requested_model"], "")); requested != "" {
		return requested
	}
	return extractModelFromPayload(payload, defaultGeminiModel)
}

// requestJSON stays on the cookie transport: it is the Qwen/OSS file-upload
// helper used by the context pipeline and has no Code Assist equivalent.
func (r *upstreamRouter) requestJSON(ctx context.Context, method, path, token string, body any, timeout time.Duration) (int, string, error) {
	if r.usesCodeAssist(token) {
		return 0, "", fmt.Errorf("jalur unggah berkas cookie tidak tersedia untuk akun OAuth Code Assist")
	}
	return r.cookie.requestJSON(ctx, method, path, token, body, timeout)
}

func (r *upstreamRouter) SignIn(ctx context.Context, email, password string) (*SignInResult, error) {
	return r.cookie.SignIn(ctx, email, password)
}

func (r *upstreamRouter) PostChatCompletionOnce(ctx context.Context, token, chatID string, payload map[string]any, timeout time.Duration) (int, string, error) {
	var sb strings.Builder
	err := r.StreamChatLegacy(ctx, token, chatID, payload, func(evt UpstreamEvent) error {
		if evt.Type == "delta" && evt.Content != "" {
			sb.WriteString(evt.Content)
		}
		return nil
	})
	if err != nil {
		return httpStatusFromError(err, 500), err.Error(), err
	}
	raw := mustJSON(map[string]any{
		"choices": []map[string]any{{
			"message":       map[string]any{"role": "assistant", "content": sb.String()},
			"finish_reason": "stop",
		}},
	})
	return 200, raw, nil
}

func (r *upstreamRouter) GetVisionTaskStatus(ctx context.Context, token, taskID string, timeout time.Duration) (int, string, error) {
	return r.cookie.GetVisionTaskStatus(ctx, token, taskID, timeout)
}

func (r *upstreamRouter) GetChatDetail(ctx context.Context, token, chatID string, timeout time.Duration) (int, string, error) {
	return r.cookie.GetChatDetail(ctx, token, chatID, timeout)
}

func (r *upstreamRouter) ListChats(ctx context.Context, token string, limit int) ([]map[string]any, error) {
	return r.cookie.ListChats(ctx, token, limit)
}

// ListModelsFromPool serves the real upstream lineup when the pool is served by
// Code Assist, otherwise the cookie-path lineup.
func (r *upstreamRouter) ListModelsFromPool(ctx context.Context) ([]map[string]any, error) {
	if r.oauth != nil && len(r.oauthAccounts()) > 0 {
		return CodeAssistModelCatalog(), nil
	}
	return r.cookie.ListModelsFromPool(ctx)
}

func (r *upstreamRouter) VerifyToken(ctx context.Context, token string) bool {
	return r.VerifyTokenDetail(ctx, token).Valid
}

func (r *upstreamRouter) VerifyTokenDetail(ctx context.Context, token string) TokenVerifyResult {
	if r.usesCodeAssist(token) {
		ok, detail := r.oauth.Verify(ctx, token)
		if ok {
			return TokenVerifyResult{Valid: true, StatusCode: "active", Error: detail}
		}
		status := "unauthorized"
		if strings.Contains(strings.ToLower(detail), "http 429") {
			status = "rate_limited"
		}
		return TokenVerifyResult{Valid: false, StatusCode: status, Error: detail}
	}
	return r.cookie.VerifyTokenDetail(ctx, token)
}

// httpStatusFromError maps an upstream error onto an HTTP status.
func httpStatusFromError(err error, fallback int) int {
	if err == nil {
		return fallback
	}
	if status := upstream.StatusCodeOf(err); status != 0 {
		return status
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "http 429"), strings.Contains(lower, "resource_exhausted"):
		return 429
	case strings.Contains(lower, "http 401"), strings.Contains(lower, "http 403"):
		return 401
	case strings.Contains(lower, "http 404"), strings.Contains(lower, "not found"):
		return 404
	default:
		return fallback
	}
}

// CooldownStatus surfaces the per-model Code Assist cooldown table.
func (r *upstreamRouter) CooldownStatus() map[string]any {
	if r.oauth == nil {
		return map[string]any{}
	}
	return r.oauth.CooldownStatus()
}

// ---- OAuth accounts ----

// loadOAuthAccounts turns the stored Google OAuth credential into pool
// accounts. The refresh token itself is never copied into the pool entry that
// gets persisted; only the file path is, and the transport re-reads it.
func loadOAuthAccounts(settings Settings) []Account {
	accounts := []Account{}
	seen := map[string]bool{}

	add := func(email, oauthFile string) {
		email = strings.TrimSpace(email)
		oauthFile = strings.TrimSpace(oauthFile)
		if email == "" || oauthFile == "" || seen[email] {
			return
		}
		if _, err := os.Stat(oauthFile); err != nil {
			return
		}
		seen[email] = true
		accounts = append(accounts, Account{
			Email:      email,
			Token:      oauthTokenPrefix + email,
			AuthType:   authTypeCodeAssist,
			OAuthFile:  oauthFile,
			Source:     "env",
			EnvName:    "CODE_ASSIST_OAUTH",
			Username:   email,
			StatusCode: "valid",
		})
	}

	if settings.CodeAssistEnabled {
		if file := settings.resolvedCodeAssistOAuthFile(); file != "" {
			add(codeAssistAccountEmail(file), file)
		}
	}
	for _, item := range numberedEnvValues(codeAssistAccountEnvRe) {
		parts := strings.SplitN(item.value, ";", 3)
		email := strings.TrimSpace(parts[0])
		oauthFile := settings.resolvedCodeAssistOAuthFile()
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
			oauthFile = strings.TrimSpace(parts[1])
		}
		if email == "" {
			email = fmt.Sprintf("oauth_%d@codeassist", item.index)
		}
		add(email, oauthFile)
	}
	return accounts
}

var codeAssistAccountEnvRe = regexp.MustCompile(`^CODE_ASSIST_ACCOUNT_(\d+)$`)

// codeAssistAccountEmail derives a stable label for an OAuth credential file
// from the email claim inside its id_token. The refresh token is never read
// into a log line or an identifier.
func codeAssistAccountEmail(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "codeassist@oauth"
	}
	var payload struct {
		IDToken string `json:"id_token"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return "codeassist@oauth"
	}
	if email := emailFromJWTPayload(payload.IDToken); email != "" {
		return email
	}
	return "codeassist@oauth"
}

// emailFromJWTPayload decodes the middle segment of a JWT and returns its
// email claim without verifying the signature (it is only used as a label).
func emailFromJWTPayload(idToken string) string {
	idToken = strings.TrimSpace(idToken)
	segments := strings.Split(idToken, ".")
	if len(segments) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return ""
	}
	var body struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return strings.TrimSpace(body.Email)
}

// ---- model catalog / resolution ----

const defaultGeminiModel = "gemini-3.6-flash"

// CodeAssistModelCatalog is the real lineup served by the Code Assist tier.
func CodeAssistModelCatalog() []map[string]any {
	live := []map[string]any{}
	for _, model := range upstream.CodeAssistLiveModels {
		live = append(live, map[string]any{
			"id":           model,
			"display_name": codeAssistDisplayName(model),
			"description":  "Google " + model + " via Code Assist (live, streaming, date-aware)",
		})
	}
	for _, model := range upstream.CodeAssistQuotaModels {
		live = append(live, map[string]any{
			"id":           model,
			"display_name": codeAssistDisplayName(model),
			"description":  "Google " + model + " via Code Assist (upstream quota limited)",
		})
	}
	aliases := []struct{ id, target, display string }{
		{"gemini-flash", "gemini-3-flash-preview", "Gemini Flash"},
		{"gemini-flash-lite", "gemini-3.1-flash-lite", "Gemini Flash Lite"},
		{"gemini-pro", "gemini-3-pro-preview", "Gemini Pro"},
	}
	for _, entry := range aliases {
		live = append(live, map[string]any{
			"id":           entry.id,
			"display_name": entry.display,
			"description":  "Alias of " + entry.target + " on the Code Assist tier",
		})
	}
	return live
}

func codeAssistDisplayName(model string) string {
	return "Gemini " + strings.TrimPrefix(model, "gemini-")
}

// resolveCodeAssistModel maps a requested public model id onto a real Code
// Assist model id. It is used when the pool is served by OAuth accounts so the
// cookie-path alias table does not rewrite Gemini names into fakes.
func resolveCodeAssistModel(name string) string {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" {
		return upstream.CodeAssistLiveModels[0]
	}
	for _, suffix := range modelModeSuffixes() {
		if strings.HasSuffix(trimmed, suffix) && len(trimmed) > len(suffix) {
			trimmed = strings.TrimSpace(trimmed[:len(trimmed)-len(suffix)])
			break
		}
	}
	if trimmed == "" {
		return upstream.CodeAssistLiveModels[0]
	}
	return upstream.ResolveCodeAssistModel(trimmed)
}
