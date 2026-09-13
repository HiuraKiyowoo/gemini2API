package upstream

// Code Assist (cloudcode-pa.googleapis.com) transport.
//
// This is a *different* upstream from the cookie/SAPISIDHASH reverse-engineered
// gemini.google.com path: it authenticates with an OAuth refresh token and
// speaks the internal Code Assist API that gemini-cli uses.
//
// Verified facts this file depends on (see TEMUAN.md):
//   - POST https://oauth2.googleapis.com/token with grant_type=refresh_token
//     returns an access_token with ~3600s lifetime.
//   - POST {endpoint}:generateContent / :streamGenerateContent with
//     Authorization: Bearer <access_token> works with project "cloudshell-gca".
//   - :streamGenerateContent returns a PRETTY-PRINTED JSON ARRAY that spans
//     many lines. It is neither JSON-lines nor SSE, so line-based parsing
//     silently yields empty text on a healthy HTTP 200. We therefore never
//     parse per line: we stream-decode the array with json.Decoder and, if
//     that ever fails, fall back to json.Unmarshal over the whole body.
//   - Without a date in systemInstruction the model answers "2024" for the
//     current year, so date injection is mandatory.
//   - 429 RESOURCE_EXHAUSTED is frequent; the body carries
//     "quota will reset after Ns".

import (
	"bufio"
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultCodeAssistTokenURL     = "https://oauth2.googleapis.com/token"
	DefaultCodeAssistEndpoint     = "https://cloudcode-pa.googleapis.com/v1internal"
	DefaultCodeAssistClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	DefaultCodeAssistClientSecret = "" // set CODE_ASSIST_CLIENT_SECRET; the Gemini CLI OAuth client secret is not committed
	DefaultCodeAssistProject      = "cloudshell-gca"

	// Refresh this early so a request never races the expiry.
	codeAssistTokenSkew = 120 * time.Second

	codeAssistAuthHeaderPrefix = "Bearer" + " "

	// Max additional live models tried after the requested one goes 429/404.
	codeAssistMaxFallbacks = 2
)

// CodeAssistLiveModels are the models proven to answer on the standard Code
// Assist tier (HTTP 200 + text). Order is the preferred fallback order.
var CodeAssistLiveModels = []string{
	"gemini-3-flash-preview",
	"gemini-2.5-flash",
	"gemini-3.1-flash-lite",
	"gemini-3.1-flash-lite-preview",
}

// CodeAssistQuotaModels exist upstream but were only ever seen rate limited.
var CodeAssistQuotaModels = []string{
	"gemini-2.5-pro",
	"gemini-3-pro-preview",
	"gemini-3.1-pro-preview",
}

// CodeAssistModelMap maps the gateway's public lineup onto real upstream
// model ids. Anything not listed is passed through unchanged.
var CodeAssistModelMap = map[string]string{
	"gemini-3.6-flash":               "gemini-3-flash-preview",
	"gemini-3.5-flash":               "gemini-3-flash-preview",
	"gemini-3.5-flash-thinking":      "gemini-2.5-flash",
	"gemini-3.5-flash-thinking-lite": "gemini-3.1-flash-lite",
	"gemini-flash-lite":              "gemini-3.1-flash-lite",
	"gemini-auto":                    "gemini-3-flash-preview",
	"gemini-3.1-pro":                 "gemini-3-pro-preview",
	"gemini-2.0-flash":               "gemini-2.5-flash",
	"gemini-1.5-flash":               "gemini-2.5-flash",
	"gemini-1.5-pro":                 "gemini-2.5-pro",
	"gemini-flash":                   "gemini-3-flash-preview",
	"gemini-2.5-flash-lite":          "gemini-3.1-flash-lite",
	"gpt-4o":                         "gemini-3-pro-preview",
	"gpt-4o-mini":                    "gemini-2.5-flash",
	"gpt-4":                          "gemini-3-pro-preview",
	"gpt-3.5-turbo":                  "gemini-2.5-flash",
	"claude-3-5-sonnet":              "gemini-3-pro-preview",
	"claude-3-sonnet":                "gemini-3-pro-preview",
	"claude-3-haiku":                 "gemini-2.5-flash",
}

// ResolveCodeAssistModel translates a gateway model id to a real upstream id.
func ResolveCodeAssistModel(model string) string {
	trimmed := strings.ToLower(strings.TrimSpace(model))
	if trimmed == "" {
		return CodeAssistLiveModels[0]
	}
	if mapped, ok := CodeAssistModelMap[trimmed]; ok {
		return mapped
	}
	return trimmed
}

// CodeAssistOptions configures a CodeAssistClient.
type CodeAssistOptions struct {
	TokenURL       string
	Endpoint       string
	ClientID       string
	ClientSecret   string
	Project        string
	OAuthFile      string
	Attempts       int           // tries per model against 429/5xx
	RetryDelay     time.Duration // pause between tries
	ModelCooldown  time.Duration // minimum park time for a throttled model
	MaxCooldown    time.Duration // cap for a throttled model park time
	RetryBudget    time.Duration // total wall-clock budget across fallback models
	HTTPTimeout    time.Duration
	CookieAuthOnly bool
}

func (o CodeAssistOptions) withDefaults() CodeAssistOptions {
	if o.TokenURL == "" {
		o.TokenURL = DefaultCodeAssistTokenURL
	}
	if o.Endpoint == "" {
		o.Endpoint = DefaultCodeAssistEndpoint
	}
	if o.ClientID == "" {
		o.ClientID = DefaultCodeAssistClientID
	}
	if o.ClientSecret == "" {
		o.ClientSecret = DefaultCodeAssistClientSecret
	}
	if o.Project == "" {
		o.Project = DefaultCodeAssistProject
	}
	if o.Attempts <= 0 {
		o.Attempts = 3
	}
	if o.RetryDelay <= 0 {
		o.RetryDelay = 10 * time.Second
	}
	if o.ModelCooldown <= 0 {
		o.ModelCooldown = 60 * time.Second
	}
	if o.MaxCooldown <= 0 {
		o.MaxCooldown = 6 * time.Minute
	}
	if o.RetryBudget <= 0 {
		o.RetryBudget = 150 * time.Second
	}
	if o.HTTPTimeout <= 0 {
		o.HTTPTimeout = 5 * time.Minute
	}
	return o
}

// RefreshTokenSource resolves an account credential handle to a refresh token.
type RefreshTokenSource func(token string) string

// CodeAssistClient implements ChatClient against Google Code Assist.
type CodeAssistClient struct {
	opts     CodeAssistOptions
	http     *http.Client
	oauth    *OAuthRefresher
	cooldown *modelCooldown

	// RefreshTokenFor maps the pool token handle onto an account refresh token.
	// When it returns "" the client falls back to its own file-loaded token.
	RefreshTokenFor RefreshTokenSource

	// DateNow / DateLocation are overridable for tests.
	DateNow      func() time.Time
	DateLocation *time.Location
}

// NewCodeAssistClient builds the transport. OAuthFile may be empty when
// RefreshTokenFor supplies per-account refresh tokens.
func NewCodeAssistClient(opts CodeAssistOptions) (*CodeAssistClient, error) {
	opt := opts.withDefaults()
	oauth, err := NewOAuthRefresher(opt.TokenURL, opt.ClientID, opt.ClientSecret, opt.OAuthFile)
	if err != nil {
		return nil, err
	}
	return &CodeAssistClient{
		opts:     opt,
		http:     newCodeAssistHTTPClient(opt.HTTPTimeout),
		oauth:    oauth,
		cooldown: newModelCooldown(),
	}, nil
}

// Options exposes the resolved configuration (read-only copy).
func (c *CodeAssistClient) Options() CodeAssistOptions { return c.opts }

func newCodeAssistHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       60 * time.Second,
			ForceAttemptHTTP2:     true,
			ResponseHeaderTimeout: 180 * time.Second,
		},
	}
}

var _ ChatClient = (*CodeAssistClient)(nil)

// ---- OAuth ----

// OAuthToken is a cached Google access token.
type OAuthToken struct {
	AccessToken string
	ExpiresAt   time.Time
}

// OAuthRefresher turns refresh tokens into cached access tokens. The cache is
// keyed by refresh token so a pool of OAuth accounts never thrashes one slot.
type OAuthRefresher struct {
	mu             sync.Mutex
	tokenURL       string
	clientID       string
	secret         string
	defaultRefresh string
	cache          map[string]OAuthToken
	client         *http.Client
}

// NewOAuthRefresher loads a refresh token from a JSON file written by the
// gemini-cli style OAuth dance (fields: refresh_token, expires_in, ...).
func NewOAuthRefresher(tokenURL, clientID, secret, refreshTokenFile string) (*OAuthRefresher, error) {
	r := &OAuthRefresher{
		tokenURL: firstNonEmpty(tokenURL, DefaultCodeAssistTokenURL),
		clientID: firstNonEmpty(clientID, DefaultCodeAssistClientID),
		secret:   firstNonEmpty(secret, DefaultCodeAssistClientSecret),
		cache:    map[string]OAuthToken{},
		client:   &http.Client{Timeout: 45 * time.Second},
	}
	if strings.TrimSpace(refreshTokenFile) == "" {
		return r, nil
	}
	refresh, err := LoadRefreshTokenFile(refreshTokenFile)
	if err != nil {
		return nil, err
	}
	r.defaultRefresh = refresh
	return r, nil
}

// LoadRefreshTokenFile reads the refresh_token out of an OAuth JSON file.
func LoadRefreshTokenFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var payload struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("parse oauth file %s: %w", path, err)
	}
	refresh := strings.TrimSpace(payload.RefreshToken)
	if refresh == "" {
		return "", fmt.Errorf("oauth file %s has no refresh_token", path)
	}
	return refresh, nil
}

// SetDefaultRefreshToken installs the fallback refresh token.
func (r *OAuthRefresher) SetDefaultRefreshToken(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(token) != "" {
		r.defaultRefresh = strings.TrimSpace(token)
	}
}

// HasRefreshToken reports whether any refresh token is available.
func (r *OAuthRefresher) HasRefreshToken() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.defaultRefresh != ""
}

// AccessToken returns the cached access token for the default refresh token.
func (r *OAuthRefresher) AccessToken(ctx context.Context) (string, error) {
	return r.AccessTokenFor(ctx, "")
}

// AccessTokenFor returns a valid access token for refreshToken ("" = default),
// refreshing proactively when within codeAssistTokenSkew of expiry.
func (r *OAuthRefresher) AccessTokenFor(ctx context.Context, refreshToken string) (string, error) {
	r.mu.Lock()
	key := strings.TrimSpace(refreshToken)
	if key == "" {
		key = r.defaultRefresh
	}
	if key == "" {
		r.mu.Unlock()
		return "", errors.New("code assist: no OAuth refresh_token configured")
	}
	if cached, ok := r.cache[key]; ok && cached.AccessToken != "" && time.Until(cached.ExpiresAt) > codeAssistTokenSkew {
		token := cached.AccessToken
		r.mu.Unlock()
		return token, nil
	}
	r.mu.Unlock()

	// Deliberately not holding the mutex across the network call: a slow
	// refresh for one account must not block the others.
	token, expiresAt, err := r.fetch(ctx, key)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	r.cache[key] = OAuthToken{AccessToken: token, ExpiresAt: expiresAt}
	r.mu.Unlock()
	return token, nil
}

func (r *OAuthRefresher) fetch(ctx context.Context, refreshToken string) (string, time.Time, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", r.clientID)
	form.Set("client_secret", r.secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("code assist oauth refresh failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", time.Time{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("code assist oauth refresh HTTP %d: %s", resp.StatusCode, truncateOneLine(string(body), 300))
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("decode oauth response: %w", err)
	}
	access := strings.TrimSpace(tokenResp.AccessToken)
	if access == "" {
		return "", time.Time{}, errors.New("code assist oauth refresh returned an empty access_token")
	}
	ttl := time.Duration(tokenResp.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 3500 * time.Second
	}
	return access, time.Now().Add(ttl), nil
}

// Invalidate drops every cached access token (used after 401/403).
func (r *OAuthRefresher) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = map[string]OAuthToken{}
}

// ---- per-model cooldown ----

type modelCooldown struct {
	mu     sync.Mutex
	until  map[string]time.Time
	reason map[string]string
}

func newModelCooldown() *modelCooldown {
	return &modelCooldown{until: map[string]time.Time{}, reason: map[string]string{}}
}

func (c *modelCooldown) wait(model string) (time.Duration, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.until[model]
	if !ok || time.Now().After(until) {
		return 0, ""
	}
	return time.Until(until), c.reason[model]
}

func (c *modelCooldown) park(model string, d time.Duration, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.until[model] = time.Now().Add(d)
	c.reason[model] = reason
}

func (c *modelCooldown) clear(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.until, model)
	delete(c.reason, model)
}

// Snapshot lists models still parked with their remaining seconds.
func (c *modelCooldown) Snapshot() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]any{}
	for model, until := range c.until {
		remaining := time.Until(until)
		if remaining <= 0 {
			continue
		}
		out[model] = map[string]any{"cooldown_seconds": int(remaining.Seconds()), "reason": truncateOneLine(c.reason[model], 200)}
	}
	return out
}

// ---- ChatClient implementation ----

// CreateChat mints a local conversation id. Code Assist is stateless per
// request (history travels inside `contents`), so there is nothing to create.
func (c *CodeAssistClient) CreateChat(ctx context.Context, token, model, chatType string) (string, error) {
	refresh, err := c.refreshTokenFor(token)
	if err != nil {
		return "", err
	}
	if _, err := c.oauth.AccessTokenFor(ctx, refresh); err != nil {
		return "", err
	}
	return fmt.Sprintf("ca_%d_%s", time.Now().UnixNano(), randomHex(6)), nil
}

func (c *CodeAssistClient) DeleteChat(ctx context.Context, token, chatID string) bool {
	return true
}

// StreamChat runs one Code Assist turn, emitting normalized delta events as
// upstream chunks arrive.
func (c *CodeAssistClient) StreamChat(ctx context.Context, token, chatID string, payload map[string]any, onEvent func(Event) error) error {
	refresh, err := c.refreshTokenFor(token)
	if err != nil {
		return err
	}
	request := BuildCodeAssistRequest(payload, c.todayText())
	body := request.Body
	candidates := c.candidates(request.Model)
	deadline := time.Now().Add(c.opts.RetryBudget)

	var lastErr error
	tried := 0
	for _, model := range candidates {
		if tried > 0 && tried > codeAssistMaxFallbacks {
			break
		}
		tried++
		if time.Now().After(deadline) && lastErr != nil {
			break
		}
		if wait, reason := c.cooldown.wait(model); wait > 0 {
			lastErr = fmt.Errorf("code assist model %s cooling down for another %ds after upstream 429 (%s)", model, int(wait.Seconds()), truncateOneLine(reason, 120))
			continue
		}
		body["model"] = model
		emitted, status, err := c.streamWithRetries(ctx, refresh, body, model, deadline, onEvent)
		if err == nil {
			c.cooldown.clear(model)
			return nil
		}
		if emitted > 0 {
			// Partial text already reached the client; retrying another model
			// would duplicate output. Surface the failure instead.
			return err
		}
		lastErr = err
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return err
		}
	}
	return lastErr
}

func (c *CodeAssistClient) streamWithRetries(ctx context.Context, refresh string, body map[string]any, model string, deadline time.Time, onEvent func(Event) error) (int, int, error) {
	var lastErr error
	var lastStatus int
	for attempt := 1; attempt <= c.opts.Attempts; attempt++ {
		if attempt > 1 {
			remaining := time.Until(deadline)
			if remaining <= c.opts.RetryDelay {
				break
			}
			select {
			case <-ctx.Done():
				return 0, 0, ctx.Err()
			case <-time.After(c.opts.RetryDelay):
			}
		}
		emitted, status, err := c.streamOnce(ctx, refresh, body, onEvent)
		if err == nil {
			return emitted, status, nil
		}
		lastErr, lastStatus = err, status
		if emitted > 0 {
			return emitted, status, err
		}
		if status == 0 {
			// In-body error element: recover the HTTP code for classification.
			lastStatus = StatusCodeOf(err)
		}
		if lastStatus == http.StatusTooManyRequests {
			park := retryAfterSeconds(err.Error(), c.opts.ModelCooldown, c.opts.MaxCooldown)
			c.cooldown.park(model, park, err.Error())
			// The quota window is longer than our retry delay: do not burn
			// attempts against the same wall.
			break
		}
		if lastStatus == http.StatusNotFound || lastStatus == http.StatusBadRequest {
			break
		}
		if lastStatus != 0 && lastStatus < http.StatusInternalServerError {
			break
		}
	}
	return 0, lastStatus, lastErr
}

// candidates is the requested model first, then the other known-live models.
func (c *CodeAssistClient) candidates(model string) []string {
	out := []string{model}
	for _, live := range CodeAssistLiveModels {
		if live == model {
			continue
		}
		if parked, _ := c.cooldown.wait(live); parked > 0 {
			continue
		}
		out = append(out, live)
	}
	return out
}

func (c *CodeAssistClient) streamOnce(ctx context.Context, refresh string, body map[string]any, onEvent func(Event) error) (int, int, error) {
	accessToken, err := c.oauth.AccessTokenFor(ctx, refresh)
	if err != nil {
		return 0, 0, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal code assist request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.Endpoint+":streamGenerateContent", bytes.NewReader(raw))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "google-api-nodejs-client/9.15.1 (gcp:cloud-code-assist)")
	req.Header.Set("Authorization", codeAssistAuthHeaderPrefix+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("code assist request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			c.oauth.Invalidate()
		}
		return 0, resp.StatusCode, codeAssistHTTPError(resp.StatusCode, errBody)
	}

	emitted, err := c.consumeStream(ctx, resp.Body, onEvent)
	if err != nil {
		return emitted, resp.StatusCode, err
	}
	if emitted == 0 {
		return 0, resp.StatusCode, errors.New("code assist returned HTTP 200 with no text deltas (empty candidates)")
	}
	return emitted, resp.StatusCode, nil
}

// consumeStream parses the pretty-printed JSON array upstream.
//
// Primary path: stream-decode the array so text flows out as it arrives.
// Safety net: every byte is mirrored into a buffer, so if the streaming decode
// breaks we can json.Unmarshal the *entire* body exactly once — the documented
// correct shape for this endpoint.
func (c *CodeAssistClient) consumeStream(ctx context.Context, body io.Reader, onEvent func(Event) error) (int, error) {
	var mirrored bytes.Buffer
	reader := io.TeeReader(bufio.NewReaderSize(body, 64*1024), &mirrored)

	emitted := 0
	accumulator := &codeAssistTextAccumulator{}
	emit := func(piece codeAssistPiece) error {
		evt := Event{Type: "delta", Phase: piece.phase, Content: piece.text}
		if piece.reasoning {
			evt.Content = ""
			evt.ReasoningText = piece.text
			evt.Phase = "thinking"
		}
		emitted++
		return onEvent(evt)
	}

	parseErr := streamJSONArray(reader, func(element map[string]any) error {
		if msg := CodeAssistElementError(element); msg != "" {
			return errors.New(msg)
		}
		for _, piece := range extractCodeAssistParts(element, accumulator) {
			if piece.text == "" {
				continue
			}
			if err := emit(piece); err != nil {
				return err
			}
		}
		return nil
	})
	if parseErr == nil {
		return emitted, nil
	}
	if errors.Is(parseErr, context.Canceled) || errors.Is(parseErr, context.DeadlineExceeded) || errors.Is(parseErr, ctx.Err()) {
		return emitted, parseErr
	}
	if emitted > 0 {
		return emitted, parseErr
	}

	// Whole-body fallback: one json.Unmarshal over the complete body.
	if elements, err := decodeWholeCodeAssistArray(mirrored.Bytes()); err == nil && len(elements) > 0 {
		recovered := 0
		for _, element := range elements {
			if msg := CodeAssistElementError(element); msg != "" {
				return recovered, errors.New(msg)
			}
			for _, piece := range extractCodeAssistParts(element, accumulator) {
				if piece.text == "" {
					continue
				}
				if err := emit(piece); err != nil {
					return recovered, err
				}
				recovered++
			}
		}
		if recovered > 0 {
			return recovered, nil
		}
	}
	return 0, fmt.Errorf("code assist stream parse failed: %w (body=%s)", parseErr, truncateOneLine(mirrored.String(), 300))
}

func (c *CodeAssistClient) refreshTokenFor(token string) (string, error) {
	if c.RefreshTokenFor != nil {
		if refresh := strings.TrimSpace(c.RefreshTokenFor(token)); refresh != "" {
			return refresh, nil
		}
	}
	if c.oauth.HasRefreshToken() {
		return "", nil // OAuthRefresher falls back to its own file-loaded token.
	}
	return "", errors.New("code assist account has no OAuth refresh_token")
}

// todayText renders the current date for systemInstruction injection.
func (c *CodeAssistClient) todayText() string {
	now := time.Now()
	if c.DateNow != nil {
		now = c.DateNow()
	}
	loc := c.DateLocation
	if loc == nil {
		loc = time.Local
	}
	return now.In(loc).Format("2006-01-02 (Monday)")
}

// CooldownStatus exposes the per-model cooldown table.
func (c *CodeAssistClient) CooldownStatus() map[string]any { return c.cooldown.Snapshot() }

// Verify probes loadCodeAssist, the cheapest authenticated round trip.
func (c *CodeAssistClient) Verify(ctx context.Context, token string) (bool, string) {
	refresh, err := c.refreshTokenFor(token)
	if err != nil {
		return false, err.Error()
	}
	accessToken, err := c.oauth.AccessTokenFor(ctx, refresh)
	if err != nil {
		return false, err.Error()
	}
	raw, _ := json.Marshal(map[string]any{"cloudaicompanionProject": ""})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.Endpoint+":loadCodeAssist", bytes.NewReader(raw))
	if err != nil {
		return false, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", codeAssistAuthHeaderPrefix+accessToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("code assist loadCodeAssist HTTP %d: %s", resp.StatusCode, truncateOneLine(string(body), 240))
	}
	var parsed map[string]any
	if json.Unmarshal(body, &parsed) != nil {
		return false, "code assist loadCodeAssist returned non-JSON"
	}
	tier := ""
	if current, ok := parsed["currentTier"].(map[string]any); ok {
		tier = firstString(current["id"], current["name"])
	}
	project := firstString(parsed["cloudaicompanionProject"])
	return true, fmt.Sprintf("code assist ok (tier=%s project=%s)", firstNonEmpty(tier, "-"), firstNonEmpty(project, "-"))
}

// ---- request building ----

// CodeAssistRequest is a rendered upstream call.
type CodeAssistRequest struct {
	Model string
	Body  map[string]any
}

// BuildCodeAssistRequest converts the gateway's flattened payload into a Code
// Assist request: role-tagged contents plus a systemInstruction that always
// carries today's date, merged with the caller's system prompt.
func BuildCodeAssistRequest(payload map[string]any, today string) CodeAssistRequest {
	model := ResolveCodeAssistModel(stringFromMap(payload, "model"))
	prompt := PromptFromPayload(payload)

	systemParts := []string{}
	if today != "" {
		systemParts = append(systemParts,
			"Informasi waktu: hari ini tanggal "+today+". Gunakan tanggal ini untuk menjawab pertanyaan tentang waktu, tahun, dekade, atau umur; jangan menyebut tahun pengetahuan terakhir sebagai tahun sekarang.")
	}
	if prompt.systemText != "" {
		systemParts = append(systemParts, prompt.systemText)
	}

	contents := prompt.contents
	if len(contents) == 0 {
		contents = []map[string]any{{"role": "user", "parts": []map[string]any{{"text": strings.TrimSpace(prompt.raw)}}}}
	}
	request := map[string]any{"contents": contents}
	if parts := textParts(systemParts); len(parts) > 0 {
		request["systemInstruction"] = map[string]any{"parts": parts}
	}
	if genCfg := generationConfigFromPayload(payload); len(genCfg) > 0 {
		request["generationConfig"] = genCfg
	}
	return CodeAssistRequest{
		Model: model,
		Body:  map[string]any{"model": model, "project": projectFromPayload(payload), "request": request},
	}
}

func textParts(values []string) []map[string]any {
	parts := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, map[string]any{"text": value})
	}
	return parts
}

func projectFromPayload(payload map[string]any) string {
	if project := strings.TrimSpace(stringFromMap(payload, "code_assist_project")); project != "" {
		return project
	}
	if project := strings.TrimSpace(os.Getenv("CODE_ASSIST_PROJECT")); project != "" {
		return project
	}
	return DefaultCodeAssistProject
}

func generationConfigFromPayload(payload map[string]any) map[string]any {
	cfg := map[string]any{}
	if value, ok := payload["temperature"].(float64); ok {
		cfg["temperature"] = value
	}
	if value, ok := payload["max_output_tokens"].(float64); ok && value > 0 {
		cfg["maxOutputTokens"] = int(value)
	}
	return cfg
}

// FlattenedPrompt is the result of de-flattening the gateway prompt.
type FlattenedPrompt struct {
	raw        string
	systemText string
	contents   []map[string]any
}

// PromptFromPayload pulls the flattened prompt out of the gateway payload and
// splits it back into role-tagged Code Assist contents plus system text.
func PromptFromPayload(payload map[string]any) FlattenedPrompt {
	raw := ""
	for _, key := range []string{"prompt", "content"} {
		if s, ok := payload[key].(string); ok && s != "" {
			raw = s
			break
		}
	}
	if raw == "" {
		for _, key := range []string{"messages", "contents"} {
			if list, ok := payload[key].([]any); ok {
				if text := textFromMessageList(list); text != "" {
					raw = text
					break
				}
			}
			if list, ok := payload[key].([]map[string]any); ok {
				if text := textFromMapMessageList(list); text != "" {
					raw = text
					break
				}
			}
		}
	}
	return UnflattenPrompt(raw)
}

func textFromMessageList(list []any) string {
	msgs := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			msgs = append(msgs, m)
		}
	}
	return textFromMapMessageList(msgs)
}

func textFromMapMessageList(msgs []map[string]any) string {
	blocks := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		role := strings.TrimSpace(firstString(msg["role"]))
		if role == "" {
			role = "user"
		}
		text := firstString(msg["content"], msg["text"])
		if text == "" {
			if parts, ok := msg["parts"].([]any); ok {
				for _, p := range parts {
					if pm, ok := p.(map[string]any); ok {
						text += firstString(pm["text"])
					}
				}
			}
		}
		if text == "" {
			continue
		}
		blocks = append(blocks, "["+titleRole(role)+"]\n"+text)
	}
	return strings.Join(blocks, "\n\n")
}

func titleRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "developer":
		return "System"
	case "assistant", "model":
		return "Assistant"
	case "tool", "function":
		return "Tool Result"
	default:
		return "User"
	}
}

var flattenedRoleMarker = regexp.MustCompile(`(?m)^\[(System|User|Assistant|Tool Result|Developer)\]\n`)

// UnflattenPrompt reverses adapter.MessagesToPrompt: [System] blocks become
// systemInstruction text and the rest becomes role-tagged contents.
func UnflattenPrompt(prompt string) FlattenedPrompt {
	out := FlattenedPrompt{raw: prompt}
	matches := flattenedRoleMarker.FindAllStringSubmatchIndex(prompt, -1)
	if len(matches) == 0 {
		out.contents = []map[string]any{{"role": "user", "parts": []map[string]any{{"text": strings.TrimSpace(prompt)}}}}
		return out
	}
	type block struct {
		role string
		text string
	}
	blocks := make([]block, 0, len(matches))
	for i, m := range matches {
		role := strings.ToLower(prompt[m[2]:m[3]])
		start := m[1]
		end := len(prompt)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		text := strings.TrimSpace(prompt[start:end])
		if text == "" {
			continue
		}
		blocks = append(blocks, block{role: role, text: text})
	}
	if len(blocks) == 0 {
		out.contents = []map[string]any{{"role": "user", "parts": []map[string]any{{"text": strings.TrimSpace(prompt)}}}}
		return out
	}
	for _, b := range blocks {
		switch b.role {
		case "system", "developer":
			if out.systemText == "" {
				out.systemText = b.text
			} else {
				out.systemText += "\n\n" + b.text
			}
		case "assistant", "model":
			out.contents = append(out.contents, map[string]any{"role": "model", "parts": []map[string]any{{"text": b.text}}})
		case "tool", "tool result", "function":
			out.contents = append(out.contents, map[string]any{"role": "user", "parts": []map[string]any{{"text": "[Tool Result]\n" + b.text}}})
		default:
			out.contents = append(out.contents, map[string]any{"role": "user", "parts": []map[string]any{{"text": b.text}}})
		}
	}
	if len(out.contents) == 0 {
		if out.systemText == "" {
			out.contents = []map[string]any{{"role": "user", "parts": []map[string]any{{"text": strings.TrimSpace(prompt)}}}}
			return out
		}
		out.contents = []map[string]any{{"role": "user", "parts": []map[string]any{{"text": out.systemText}}}}
		out.systemText = ""
		return out
	}
	// The Code Assist API rejects a turn list that does not open and close with
	// a user turn.
	if firstString(out.contents[0]["role"]) != "user" {
		out.contents = append([]map[string]any{{"role": "user", "parts": []map[string]any{{"text": "Berikut konteks percakapan sebelumnya."}}}}, out.contents...)
	}
	if firstString(out.contents[len(out.contents)-1]["role"]) != "user" {
		out.contents = append(out.contents, map[string]any{"role": "user", "parts": []map[string]any{{"text": "Lanjutkan."}}})
	}
	return out
}

// ---- stream parsing ----

// streamJSONArray decodes a top-level JSON array element by element. This
// works for the pretty-printed multi-line array because json.Decoder is token
// based, not line based.
func streamJSONArray(r io.Reader, handle func(map[string]any) error) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	first, err := dec.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("empty body")
		}
		return err
	}
	tok, ok := first.(json.Delim)
	if !ok {
		return fmt.Errorf("unexpected first token %v", first)
	}
	if tok != '[' {
		return fmt.Errorf("unexpected opening token %q", tok.String())
	}
	for dec.More() {
		var element map[string]any
		if err := dec.Decode(&element); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		if err := handle(element); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func decodeWholeCodeAssistArray(raw []byte) ([]map[string]any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("empty body")
	}
	if raw[0] == '[' {
		var list []map[string]any
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	var single map[string]any
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, err
	}
	return []map[string]any{single}, nil
}

type codeAssistPiece struct {
	text      string
	phase     string
	reasoning bool
}

type codeAssistTextAccumulator struct {
	total string
}

// extractCodeAssistParts pulls text out of one stream element. Each element is
// either {response: {...}} or the candidate payload directly.
func extractCodeAssistParts(element map[string]any, acc *codeAssistTextAccumulator) []codeAssistPiece {
	if element == nil {
		return nil
	}
	response := element
	if inner, ok := element["response"].(map[string]any); ok && inner != nil {
		response = inner
	}
	candidates, _ := response["candidates"].([]any)
	pieces := []codeAssistPiece{}
	for _, rawCandidate := range candidates {
		candidate, ok := rawCandidate.(map[string]any)
		if !ok {
			continue
		}
		content, ok := candidate["content"].(map[string]any)
		if !ok {
			continue
		}
		parts, _ := content["parts"].([]any)
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			text := firstString(part["text"], part["textContent"])
			if text == "" {
				continue
			}
			reasoning, _ := part["thought"].(bool)
			if acc != nil {
				text = acc.dedupe(text, reasoning)
				if text == "" {
					continue
				}
			}
			phase := "answer"
			if reasoning {
				phase = "thinking"
			}
			pieces = append(pieces, codeAssistPiece{text: text, phase: phase, reasoning: reasoning})
		}
	}
	return pieces
}

// dedupe guards against upstream ever sending cumulative text: when a chunk
// starts with everything already seen, only the new tail is emitted.
func (a *codeAssistTextAccumulator) dedupe(text string, reasoning bool) string {
	if reasoning {
		return text
	}
	if a.total == "" {
		a.total = text
		return text
	}
	if strings.HasPrefix(text, a.total) {
		delta := text[len(a.total):]
		a.total = text
		return delta
	}
	a.total += text
	return text
}

// CodeAssistElementError turns an upstream error element into a message.
func CodeAssistElementError(element map[string]any) string {
	if element == nil {
		return ""
	}
	if errObj, ok := element["error"].(map[string]any); ok {
		code := firstString(errObj["status"], errObj["code"])
		message := firstString(errObj["message"])
		if code == "" && message == "" {
			return ""
		}
		status := codeAssistStatusFromError(code, message)
		if status != 0 {
			return fmt.Sprintf("code assist HTTP %d: %s %s", status, firstNonEmpty(code, "-"), truncateOneLine(message, 300))
		}
		return fmt.Sprintf("code assist upstream error code=%s message=%s", firstNonEmpty(code, "-"), truncateOneLine(message, 300))
	}
	if feedback, ok := element["promptFeedback"].(map[string]any); ok {
		if reason := firstString(feedback["blockReason"]); reason != "" {
			return "code assist blocked the prompt (blockReason=" + reason + ")"
		}
	}
	if response, ok := element["response"].(map[string]any); ok {
		if msg := CodeAssistElementError(response); msg != "" {
			return msg
		}
		if candidates, ok := response["candidates"].([]any); ok && len(candidates) > 0 {
			if candidate, ok := candidates[0].(map[string]any); ok {
				if reason, ok := candidate["finishReason"].(string); ok {
					switch strings.ToUpper(reason) {
					case "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "RECITATION", "SPII":
						return "code assist finished with " + strings.ToUpper(reason)
					}
				}
			}
		}
	}
	return ""
}

// ExtractCodeAssistText is the documented one-shot extractor: whole body ->
// json.Unmarshal -> per element response/candidates[].content.parts[].text.
func ExtractCodeAssistText(raw []byte) (string, error) {
	elements, err := decodeWholeCodeAssistArray(raw)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, element := range elements {
		if msg := CodeAssistElementError(element); msg != "" {
			return sb.String(), errors.New(msg)
		}
		for _, piece := range extractCodeAssistParts(element, nil) {
			if !piece.reasoning {
				sb.WriteString(piece.text)
			}
		}
	}
	return sb.String(), nil
}

// ---- errors ----

type codeAssistError struct {
	Status  int
	Message string
}

func (e *codeAssistError) Error() string {
	return fmt.Sprintf("code assist HTTP %d: %s", e.Status, e.Message)
}

// StatusCodeOf reports the upstream HTTP status carried by an error, or 0.
// It also understands errors that only mention the status in their message,
// because the router re-wraps transport errors before returning them.
func StatusCodeOf(err error) int {
	var caErr *codeAssistError
	if errors.As(err, &caErr) {
		return caErr.Status
	}
	if err == nil {
		return 0
	}
	lower := strings.ToLower(err.Error())
	for _, candidate := range []string{"429", "404", "403", "401", "400", "500", "502", "503", "504"} {
		if strings.Contains(lower, "http "+candidate) || strings.Contains(lower, "code="+candidate) {
			status, _ := strconv.Atoi(candidate)
			return status
		}
	}
	if strings.Contains(lower, "resource_exhausted") {
		return http.StatusTooManyRequests
	}
	return 0
}

// codeAssistStatusFromError maps a Code Assist error status/code onto HTTP.
func codeAssistStatusFromError(code, message string) int {
	combined := strings.ToLower(code + " " + message)
	switch {
	case strings.Contains(combined, "resource_exhausted"), strings.Contains(combined, "rate limit"),
		strings.Contains(combined, "quota"), strings.Contains(combined, "429"):
		return http.StatusTooManyRequests
	case strings.Contains(combined, "not_found"), strings.Contains(combined, "404"):
		return http.StatusNotFound
	case strings.Contains(combined, "permission_denied"), strings.Contains(combined, "403"):
		return http.StatusForbidden
	case strings.Contains(combined, "unauthenticated"), strings.Contains(combined, "invalid_grant"), strings.Contains(combined, "401"):
		return http.StatusUnauthorized
	case strings.Contains(combined, "internal"), strings.Contains(combined, "unavailable"),
		strings.Contains(combined, "500"), strings.Contains(combined, "503"):
		return http.StatusInternalServerError
	default:
		return 0
	}
}

func codeAssistHTTPError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		message = firstNonEmpty(parsed.Error.Status, "error") + " " + parsed.Error.Message
	}
	return &codeAssistError{Status: status, Message: truncateOneLine(message, 400)}
}

func isRateLimitStatus(err error) bool {
	var caErr *codeAssistError
	if errors.As(err, &caErr) {
		return caErr.Status == http.StatusTooManyRequests
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 429") || strings.Contains(msg, "resource_exhausted")
}

var quotaResetRe = regexp.MustCompile(`(?i)reset after\s+(\d+)\s*s`)

// retryAfterSeconds honours "quota will reset after 47s" hints, bounded by the
// configured cooldown window.
func retryAfterSeconds(message string, minWait, maxWait time.Duration) time.Duration {
	wait := minWait
	if m := quotaResetRe.FindStringSubmatch(message); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			wait = time.Duration(n+3) * time.Second
		}
	}
	if wait < minWait {
		wait = minWait
	}
	if maxWait > 0 && wait > maxWait {
		wait = maxWait
	}
	return wait
}

// RetryAfterSeconds is the exported probe helper for error messages.
func RetryAfterSeconds(message string) int {
	if m := quotaResetRe.FindStringSubmatch(message); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

// ---- small helpers ----

func stringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func truncateOneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit]
}

func randomHex(n int) string {
	const digits = "0123456789abcdef"
	buf := make([]byte, n)
	if _, err := crand.Read(buf); err != nil {
		return "000000"
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = digits[int(b)%len(digits)]
	}
	return string(out)
}
