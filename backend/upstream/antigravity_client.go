package upstream

// Antigravity (daily-cloudcode-pa.googleapis.com) transport.
//
// Antigravity is Google's unified gateway: ONE OAuth account serves both Claude
// models (claude-sonnet-4-6, claude-opus-4-6-thinking), Gemini 3.x
// (gemini-3.1-pro-high, gemini-3.8-flash-high) and gpt-oss-120b-medium through
// the Gemini-style /v1internal API. It is a different upstream from the plain
// Code Assist endpoint this repo already speaks (cloudcode-pa.googleapis.com).
//
// WHY THIS FILE IS USUALLY NOT ENOUGH
//
// This account currently gets, on every generateContent/streamGenerateContent
// call, HTTP 403:
//
//	{"error":{"code":403,"message":"Verify your account to continue.",
//	 "status":"PERMISSION_DENIED",
//	 "details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",
//	   "reason":"VALIDATION_REQUIRED","domain":"cloudcode-pa.googleapis.com",
//	   "metadata":{"validation_url":"https://accounts.google.com/signin/continue?...",
//	   "validation_learn_more_url":"https://support.google.com/accounts?p=al_alert"}}]}}
//
// :loadCodeAssist answers 200 and names the cause explicitly:
//
//	{"allowedTiers":[{"id":"standard-tier","userDefinedCloudaicompanionProject":true,
//	                  "isDefault":true,"usesGcpTos":true}],
//	 "ineligibleTiers":[{"reasonCode":"VALIDATION_REQUIRED",
//	   "tierId":"free-tier",
//	   "reasonMessage":"Your current account is not eligible for Antigravity.
//	                    Verify your account to continue."}]}
//
// So the gate is an ACCOUNT-LEVEL anti-abuse lock on the free tier, applied by
// Google. No header, body, project or endpoint change clears it: the very same
// 403 is returned by the production endpoint, with a GCP project attached, with
// x-goog-user-project, and for non-Claude models. The owner of the Google
// account must complete the identity check at the validation_url.
//
// This transport exists so the moment the account is verified the path is live
// with no rewrite. Until then DetectValidationRequired tells the caller exactly
// that instead of pretending the model answered.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// Antigravity OAuth client (the Antigravity CLI client id).
	DefaultAntigravityClientID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"

	// daily-cloudcode-pa is the sandbox/frontend host the Antigravity client
	// talks to; cloudcode-pa is the production host. A verified account works
	// on either, so callers fall back in this order.
	DefaultAntigravityEndpoint     = "https://daily-cloudcode-pa.googleapis.com/v1internal"
	DefaultAntigravityProdEndpoint = "https://cloudcode-pa.googleapis.com/v1internal"

	DefaultAntigravityOAuthFile = "data/antigravity_oauth.json"

	// Matches oauth_antigravity.py UA= and the reference proxy constant.
	DefaultAntigravityUserAgent = "antigravity/1.15.8 linux/amd64"

	// oauth_antigravity.py requests cloud-platform + userinfo + cclog +
	// experimentsandconfigs; auth/cclog/v1:append is what shows up in activity.
	DefaultAntigravityLogEndpoint = "https://cloudcode-pa.googleapis.com/v1internal:logActivity"

	// Antigravity returns its SSE frames as "data: {...}" with a JSON array in
	// non-SSE mode; long model turns (opus-4-6-thinking) run for minutes.
	antigravityHTTPTimeout  = 10 * time.Minute
	antigravityMaxFallbacks = 2
)

// AntigravityValidationRequiredReason is the ErrorInfo.reason Google returns
// when the Google account needs identity verification before Antigravity will
// answer. It is not a transient failure and retrying cannot fix it.
const AntigravityValidationRequiredReason = "VALIDATION_REQUIRED"

// AntigravityFreeTierNotEligibleReason is returned by onboardUser for accounts
// whose free tier is blocked (the same accounts that carry VALIDATION_REQUIRED).
const AntigravityFreeTierNotEligibleReason = "FREE_TIER_USER_NOT_ELIGIBLE"

// AntigravityModels are the ids fetchAvailableModels/loadCodeAssist expose for
// this upstream. Verified live against daily-cloudcode-pa 2026-09-13:
// gemini-3.7-flash-low, gemini-3.7-flash-high, gemini-3.1-pro-high, ... —
// the Claude ids below come from ANTIGRAVITY_API_SPEC.md and were accepted by
// the endpoint (the call reached the account gate rather than 404).
var AntigravityModels = []string{
	"claude-sonnet-4-6",
	"claude-opus-4-6-thinking",
	"gemini-3.8-flash-high",
	"gemini-3.1-pro-high",
	"gpt-oss-120b-medium",
}

// AntigravityModelMap maps the gateway's public lineup onto upstream ids.
var AntigravityModelMap = map[string]string{
	"claude-4.6-sonnet":        "claude-sonnet-4-6",
	"claude-sonnet-4.6":        "claude-sonnet-4-6",
	"claude-4.6-opus-thinking": "claude-opus-4-6-thinking",
	"claude-opus-4.6-thinking": "claude-opus-4-6-thinking",
	"gemini-3.8-flash-high":    "gemini-3.8-flash-high",
	"gemini-3.1-pro-high":      "gemini-3.1-pro-high",
	"gemini-3-pro-high":        "gemini-3.1-pro-high",
	"gpt-oss-120b":             "gpt-oss-120b-medium",
	"gpt-oss-120b-medium":      "gpt-oss-120b-medium",
}

// AntigravityThinkingModels need maxOutputTokens > thinkingBudget.
var AntigravityThinkingModels = map[string]bool{
	"claude-opus-4-6-thinking": true,
}

// ResolveAntigravityModel translates a gateway model id to a real upstream id.
func ResolveAntigravityModel(model string) string {
	trimmed := strings.ToLower(strings.TrimSpace(model))
	if trimmed == "" {
		return AntigravityModels[0]
	}
	if mapped, ok := AntigravityModelMap[trimmed]; ok {
		return mapped
	}
	return trimmed
}

// AntigravityOptions configures an AntigravityClient.
type AntigravityOptions struct {
	TokenURL     string
	Endpoint     string // daily; defaults to DefaultAntigravityEndpoint
	ProdEndpoint string // fallback; defaults to DefaultAntigravityProdEndpoint
	ClientID     string
	ClientSecret string // env ANTIGRAVITY_CLIENT_SECRET; never committed
	Project      string
	OAuthFile    string

	Attempts    int
	RetryDelay  time.Duration
	RetryBudget time.Duration
	HTTPTimeout time.Duration
}

func (o AntigravityOptions) withDefaults() AntigravityOptions {
	if o.TokenURL == "" {
		o.TokenURL = DefaultCodeAssistTokenURL
	}
	if o.Endpoint == "" {
		o.Endpoint = DefaultAntigravityEndpoint
	}
	if o.ProdEndpoint == "" {
		o.ProdEndpoint = DefaultAntigravityProdEndpoint
	}
	if o.ClientID == "" {
		o.ClientID = DefaultAntigravityClientID
	}
	if o.OAuthFile == "" {
		// Only adopt the conventional path when it exists. A pool that supplies
		// per-account refresh tokens through RefreshTokenFor must still boot
		// with no file on disk.
		if _, err := os.Stat(DefaultAntigravityOAuthFile); err == nil {
			o.OAuthFile = DefaultAntigravityOAuthFile
		}
	}
	if o.Attempts <= 0 {
		o.Attempts = 2
	}
	if o.RetryDelay <= 0 {
		o.RetryDelay = 10 * time.Second
	}
	if o.RetryBudget <= 0 {
		o.RetryBudget = 150 * time.Second
	}
	if o.HTTPTimeout <= 0 {
		o.HTTPTimeout = antigravityHTTPTimeout
	}
	return o
}

// AntigravityClient implements ChatClient against the Antigravity gateway.
type AntigravityClient struct {
	opts  AntigravityOptions
	http  *http.Client
	oauth *OAuthRefresher

	// RefreshTokenFor maps a pool token handle onto an account refresh token.
	RefreshTokenFor RefreshTokenSource

	// DateNow / DateLocation are overridable for tests.
	DateNow      func() time.Time
	DateLocation *time.Location
}

var _ ChatClient = (*AntigravityClient)(nil)

// NewAntigravityClient builds the transport. OAuthFile may be empty when
// RefreshTokenFor supplies per-account refresh tokens.
func NewAntigravityClient(opts AntigravityOptions) (*AntigravityClient, error) {
	opt := opts.withDefaults()
	oauth, err := NewOAuthRefresher(opt.TokenURL, opt.ClientID, opt.ClientSecret, opt.OAuthFile)
	if err != nil {
		return nil, err
	}
	return &AntigravityClient{
		opts:  opt,
		http:  newCodeAssistHTTPClient(opt.HTTPTimeout),
		oauth: oauth,
	}, nil
}

// Options exposes the resolved configuration (read-only copy).
func (c *AntigravityClient) Options() AntigravityOptions { return c.opts }

// Endpoints is the daily-then-prod fallback order.
func (c *AntigravityClient) Endpoints() []string {
	out := []string{}
	for _, ep := range []string{c.opts.Endpoint, c.opts.ProdEndpoint} {
		if strings.TrimSpace(ep) != "" {
			out = append(out, ep)
		}
	}
	return out
}

// ---- OAuth ----

// AntigravityAccessToken returns a live access token for a pool handle.
func (c *AntigravityClient) AntigravityAccessToken(ctx context.Context, token string) (string, error) {
	refresh, err := c.refreshTokenFor(token)
	if err != nil {
		return "", err
	}
	return c.oauth.AccessTokenFor(ctx, refresh)
}

func (c *AntigravityClient) refreshTokenFor(token string) (string, error) {
	if c.RefreshTokenFor != nil {
		if refresh := strings.TrimSpace(c.RefreshTokenFor(token)); refresh != "" {
			return refresh, nil
		}
	}
	if c.oauth.HasRefreshToken() {
		return "", nil // OAuthRefresher falls back to its own file-loaded token.
	}
	return "", errors.New("antigravity account has no OAuth refresh_token")
}

// ---- ChatClient implementation ----

// CreateChat mints a local conversation id. Antigravity is stateless per
// request (history travels inside `contents`).
func (c *AntigravityClient) CreateChat(ctx context.Context, token, model, chatType string) (string, error) {
	if _, err := c.AntigravityAccessToken(ctx, token); err != nil {
		return "", err
	}
	return fmt.Sprintf("ag_%d_%s", time.Now().UnixNano(), randomHex(6)), nil
}

func (c *AntigravityClient) DeleteChat(ctx context.Context, token, chatID string) bool {
	return true
}

// StreamChat runs one Antigravity turn across the endpoint fallback order.
//
// A VALIDATION_REQUIRED gate is returned immediately and untouched: it is not
// transient, so neither retrying nor switching models can help, and swallowing
// it would produce a silent empty answer.
func (c *AntigravityClient) StreamChat(ctx context.Context, token, chatID string, payload map[string]any, onEvent func(Event) error) error {
	request := BuildAntigravityRequest(payload, c.todayText())
	body := request.Body
	models := c.candidates(request.Model)
	if opts, ok := payload["antigravity_thinking"].(map[string]any); ok && len(opts) > 0 {
		mergeThinkingOptions(body, opts)
	}

	deadline := time.Now().Add(c.opts.RetryBudget)
	var lastErr error
	tried := 0

	for _, endpoint := range c.Endpoints() {
		for _, model := range models {
			if tried > 0 && tried > antigravityMaxFallbacks {
				break
			}
			tried++
			if lastErr != nil && time.Now().After(deadline) {
				break
			}
			body["model"] = model
			emitted, status, err := c.streamAgainst(ctx, endpoint, token, body, model, deadline, onEvent)
			if err == nil {
				return nil
			}
			if emitted > 0 {
				// Partial text already reached the client; retrying another
				// model would duplicate output.
				return err
			}
			lastErr = err
			if IsAntigravityValidationRequired(err) {
				// Account gate: identical on every endpoint and model.
				return err
			}
			if errors.Is(err, errAntigravityEmptyResponse) {
				// A verified account can still answer with empty candidates
				// when a model is unavailable. The caller's payload pinned the
				// model explicitly, so substituting a different one would be a
				// lie rather than a fix.
				break
			}
			if status == http.StatusUnauthorized || status == http.StatusForbidden {
				break // this endpoint will not change its mind; try the next one
			}
		}
	}
	return lastErr
}

func (c *AntigravityClient) candidates(model string) []string {
	out := []string{model}
	for _, live := range AntigravityModels {
		if live == model {
			continue
		}
		out = append(out, live)
	}
	return out
}

func (c *AntigravityClient) streamAgainst(ctx context.Context, endpoint, token string, body map[string]any, model string, deadline time.Time, onEvent func(Event) error) (int, int, error) {
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
		emitted, status, err := c.streamOnce(ctx, endpoint, token, body, onEvent)
		if err == nil {
			return emitted, status, nil
		}
		lastErr, lastStatus = err, status
		if emitted > 0 {
			return emitted, status, err
		}
		if status == 0 {
			lastStatus = StatusCodeOf(err)
		}
		if IsAntigravityValidationRequired(err) {
			return 0, lastStatus, err
		}
		if errors.Is(err, errAntigravityEmptyResponse) {
			return 0, lastStatus, err
		}
		if lastStatus == http.StatusTooManyRequests ||
			lastStatus == http.StatusNotFound ||
			lastStatus == http.StatusBadRequest {
			break
		}
		if lastStatus != 0 && lastStatus < http.StatusInternalServerError {
			break
		}
	}
	return 0, lastStatus, lastErr
}

func (c *AntigravityClient) streamOnce(ctx context.Context, endpoint, token string, body map[string]any, onEvent func(Event) error) (int, int, error) {
	accessToken, err := c.AntigravityAccessToken(ctx, token)
	if err != nil {
		return 0, 0, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal antigravity request: %w", err)
	}

	// Stream with SSE: the upstream accepts `?alt=sse` and emits
	// "data: {\"response\":{...}}"; the non-SSE form returns a pretty-printed
	// JSON array, which buildAntigravityHTTPRequest also tolerates.
	url := endpoint + ":streamGenerateContent?alt=sse"
	req, err := c.buildAntigravityHTTPRequest(ctx, url, raw, accessToken, true)
	if err != nil {
		return 0, 0, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("antigravity request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode == http.StatusUnauthorized {
			c.oauth.Invalidate()
		}
		return 0, resp.StatusCode, antigravityHTTPError(resp.StatusCode, errBody)
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		emitted, err := c.consumeAntigravitySSE(ctx, resp.Body, onEvent)
		if err != nil {
			return emitted, resp.StatusCode, err
		}
		if emitted == 0 {
			return 0, resp.StatusCode, errAntigravityEmptyResponse
		}
		return emitted, resp.StatusCode, nil
	}

	// Non-SSE: reuse the Code Assist array parser (same element shape).
	proxy := &CodeAssistClient{opts: c.optsAsCodeAssist(), http: c.http, oauth: c.oauth, cooldown: newModelCooldown()}
	emitted, err := proxy.consumeStream(ctx, resp.Body, onEvent)
	if err != nil {
		return emitted, resp.StatusCode, err
	}
	if emitted == 0 {
		return 0, resp.StatusCode, errAntigravityEmptyResponse
	}
	return emitted, resp.StatusCode, nil
}

// errAntigravityEmptyResponse marks an HTTP 200 that carried no text. It stops
// model substitution: the caller pinned a model, and answering from a different
// one would be a silent lie.
var errAntigravityEmptyResponse = errors.New("antigravity returned HTTP 200 with no text deltas (empty candidates)")

func (c *AntigravityClient) optsAsCodeAssist() CodeAssistOptions {
	return CodeAssistOptions{
		TokenURL: c.opts.TokenURL, ClientID: c.opts.ClientID, ClientSecret: c.opts.ClientSecret,
		Project: c.opts.Project, HTTPTimeout: c.opts.HTTPTimeout,
	}.withDefaults()
}

func (c *AntigravityClient) buildAntigravityHTTPRequest(ctx context.Context, url string, raw []byte, accessToken string, stream bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", DefaultAntigravityUserAgent)
	req.Header.Set("X-Client-Name", "antigravity")
	req.Header.Set("X-Client-Version", antigravityClientVersion())
	req.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudshelleditor/0.1")
	req.Header.Set("Client-Metadata", `{"ideType":"ANTIGRAVITY","platform":"LINUX","pluginType":"GEMINI"}`)
	req.Header.Set("Authorization", codeAssistAuthHeaderPrefix+accessToken)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	return req, nil
}

func antigravityClientVersion() string {
	parts := strings.Split(DefaultAntigravityUserAgent, "/")
	if len(parts) > 1 {
		return strings.Fields(parts[1])[0]
	}
	return "1.15.8"
}

// consumeAntigravitySSE parses "data:" frames whose payload is a
// {"response":{"candidates":[...]}} envelope (sometimes a bare
// {"candidates":[...]}), which is what this gateway actually sends.
//
// It deliberately does NOT go through ConsumeSSE: that normalizer only
// recognises Qwen-shaped `choices`/`content` payloads and silently produces no
// events for the Antigravity envelope, which would look like an empty answer.
func (c *AntigravityClient) consumeAntigravitySSE(ctx context.Context, body io.Reader, onEvent func(Event) error) (int, error) {
	accumulator := &codeAssistTextAccumulator{}
	emitted := 0
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return emitted, err
		}
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var element map[string]any
		if json.Unmarshal([]byte(data), &element) != nil || element == nil {
			continue
		}
		if msg := CodeAssistElementError(element); msg != "" {
			return emitted, errors.New(msg)
		}
		for _, piece := range extractCodeAssistParts(element, accumulator) {
			if piece.text == "" {
				continue
			}
			out := Event{Type: "delta", Phase: piece.phase, Content: piece.text}
			if piece.reasoning {
				out.Content = ""
				out.ReasoningText = piece.text
				out.Phase = "thinking"
			}
			emitted++
			if err := onEvent(out); err != nil {
				return emitted, err
			}
		}
	}
	return emitted, scanner.Err()
}

// todayText renders the current date for systemInstruction injection.
func (c *AntigravityClient) todayText() string {
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

// ---- discovery / diagnostics ----

// AntigravityTier mirrors one element of allowedTiers / ineligibleTiers.
type AntigravityTier struct {
	ID            string
	Name          string
	Description   string
	IsDefault     bool
	ReasonCode    string
	ReasonMsg     string
	ValidationURL string
	LearnMoreURL  string
}

// AntigravityStatus is the decoded loadCodeAssist answer, i.e. the account's
// real eligibility picture. It costs one cheap authenticated round trip.
type AntigravityStatus struct {
	Raw             map[string]any
	AllowedTiers    []AntigravityTier
	IneligibleTiers []AntigravityTier
	CurrentTier     string
	PaidTier        string
	Project         string
	ValidationURL   string
	ReasonCode      string
}

// VerificationRequired reports that Antigravity is blocked for this account
// until the owner completes the identity check at ValidationURL.
func (s AntigravityStatus) VerificationRequired() bool {
	if s.ReasonCode == AntigravityValidationRequiredReason {
		return true
	}
	for _, tier := range s.IneligibleTiers {
		if strings.EqualFold(tier.ReasonCode, AntigravityValidationRequiredReason) {
			return true
		}
	}
	return false
}

// HasUsableTier reports whether any tier would let generateContent run.
func (s AntigravityStatus) HasUsableTier() bool {
	return len(s.AllowedTiers) > 0 && !s.VerificationRequired()
}

// StatusSummary renders a single-line diagnosis for logs and the admin UI.
func (s AntigravityStatus) StatusSummary() string {
	if s.VerificationRequired() {
		return fmt.Sprintf("antigravity BLOCKED: account needs verification (reason=%s tier=%s validation_url=%s)",
			s.ReasonCode, firstNonEmpty(s.CurrentTier, "-"), truncateOneLine(s.ValidationURL, 200))
	}
	return fmt.Sprintf("antigravity ok (currentTier=%s project=%s allowedTiers=%d)",
		firstNonEmpty(s.CurrentTier, "-"), firstNonEmpty(s.Project, "-"), len(s.AllowedTiers))
}

// statusProbeBody is the exact loadCodeAssist body reference clients send.
func statusProbeBody(project string) map[string]any {
	metadata := map[string]any{"ideType": 6, "platform": 1, "pluginType": 2}
	if project != "" {
		metadata["duetProject"] = project
	}
	return map[string]any{"metadata": metadata, "mode": 1}
}

// LoadStatus probes loadCodeAssist on every endpoint until one answers.
func (c *AntigravityClient) LoadStatus(ctx context.Context, token string) (AntigravityStatus, error) {
	accessToken, err := c.AntigravityAccessToken(ctx, token)
	if err != nil {
		return AntigravityStatus{}, err
	}
	raw, _ := json.Marshal(statusProbeBody(c.opts.Project))

	var lastErr error
	for _, endpoint := range c.Endpoints() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+":loadCodeAssist", strings.NewReader(string(raw)))
		if err != nil {
			return AntigravityStatus{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", DefaultAntigravityUserAgent)
		req.Header.Set("Authorization", codeAssistAuthHeaderPrefix+accessToken)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("antigravity loadCodeAssist %s: %w", endpoint, err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("antigravity loadCodeAssist HTTP %d: %s", resp.StatusCode, truncateOneLine(string(body), 240))
			continue
		}
		var parsed map[string]any
		if json.Unmarshal(body, &parsed) != nil {
			return AntigravityStatus{}, errors.New("antigravity loadCodeAssist returned non-JSON")
		}
		return decodeAntigravityStatus(parsed), nil
	}
	if lastErr == nil {
		lastErr = errors.New("antigravity has no configured endpoint")
	}
	return AntigravityStatus{}, lastErr
}

func decodeAntigravityStatus(parsed map[string]any) AntigravityStatus {
	status := AntigravityStatus{Raw: parsed}
	status.AllowedTiers = decodeTierList(parsed["allowedTiers"])
	status.IneligibleTiers = decodeTierList(parsed["ineligibleTiers"])
	status.CurrentTier = firstString(tierID(parsed["currentTier"]))
	status.PaidTier = firstString(tierID(parsed["paidTier"]))
	status.Project = firstString(parsed["cloudaicompanionProject"])
	for _, tier := range status.IneligibleTiers {
		if strings.EqualFold(tier.ReasonCode, AntigravityValidationRequiredReason) {
			status.ReasonCode = tier.ReasonCode
			status.ValidationURL = tier.ValidationURL
			break
		}
	}
	return status
}

func tierID(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		return firstString(typed["id"])
	case string:
		return typed
	}
	return ""
}

func decodeTierList(value any) []AntigravityTier {
	list, _ := value.([]any)
	out := make([]AntigravityTier, 0, len(list))
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		isDefault, _ := entry["isDefault"].(bool)
		out = append(out, AntigravityTier{
			ID:            firstString(entry["id"]),
			Name:          firstString(entry["name"]),
			Description:   firstString(entry["description"]),
			IsDefault:     isDefault,
			ReasonCode:    firstString(entry["reasonCode"]),
			ReasonMsg:     firstString(entry["reasonMessage"]),
			ValidationURL: firstString(entry["validationUrl"]),
			LearnMoreURL:  firstString(entry["validationLearnMoreUrl"]),
		})
	}
	return out
}

// OnboardResult is the decoded onboardUser answer.
type OnboardResult struct {
	Done    bool
	Project string
	Reason  string
	Message string
	Raw     map[string]any
}

// Onboard calls onboardUser for the given tier. tierId "free-tier" is refused
// with FREE_TIER_USER_NOT_ELIGIBLE for accounts under a VALIDATION_REQUIRED
// lock; "standard-tier" needs an owner-supplied project (userDefinedCloudic
// companionProject=true) and then simply echoes it back.
func (c *AntigravityClient) Onboard(ctx context.Context, token, tierID, project string) (OnboardResult, error) {
	accessToken, err := c.AntigravityAccessToken(ctx, token)
	if err != nil {
		return OnboardResult{}, err
	}
	if tierID == "" {
		tierID = "free-tier"
	}
	body := map[string]any{"tierId": tierID, "metadata": map[string]any{"ideType": 6, "platform": 1, "pluginType": 2}}
	if project != "" {
		body["metadata"].(map[string]any)["duetProject"] = project
	}
	raw, _ := json.Marshal(body)

	var lastErr error
	for _, endpoint := range c.Endpoints() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+":onboardUser", strings.NewReader(string(raw)))
		if err != nil {
			return OnboardResult{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", DefaultAntigravityUserAgent)
		req.Header.Set("Authorization", codeAssistAuthHeaderPrefix+accessToken)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		var parsed map[string]any
		_ = json.Unmarshal(payload, &parsed)
		if resp.StatusCode != http.StatusOK {
			result := OnboardResult{Raw: parsed, Message: truncateOneLine(string(payload), 300)}
			if info, ok := parsed["error"].(map[string]any); ok {
				result.Message = firstString(info["message"])
				if details, ok := info["details"].([]any); ok {
					for _, d := range details {
						entry, ok := d.(map[string]any)
						if !ok {
							continue
						}
						if reason := firstString(entry["reason"]); reason != "" {
							result.Reason = reason
						}
					}
				}
			}
			lastErr = fmt.Errorf("antigravity onboardUser HTTP %d (%s): %s", resp.StatusCode, firstNonEmpty(result.Reason, "-"), result.Message)
			continue
		}
		done, _ := parsed["done"].(bool)
		projectID := onboardProjectID(parsed)
		return OnboardResult{Done: done, Project: firstNonEmpty(projectID, project), Raw: parsed}, nil
	}
	return OnboardResult{}, lastErr
}

// Verify probes loadCodeAssist (the cheapest authenticated round trip) and
// reports the account picture plus, when blocked, the verification URL.
func (c *AntigravityClient) Verify(ctx context.Context, token string) (bool, string) {
	status, err := c.LoadStatus(ctx, token)
	if err != nil {
		return false, err.Error()
	}
	if status.VerificationRequired() {
		return false, status.StatusSummary()
	}
	return true, status.StatusSummary()
}

// ---- error classification ----

// antigravityErrorDetail is the ErrorInfo detail Google attaches to failures.
type antigravityErrorDetail struct {
	Reason   string            `json:"reason"`
	Domain   string            `json:"domain"`
	Metadata map[string]string `json:"metadata"`
}

type antigravityError struct {
	Status  int
	Code    string
	Reason  string
	Message string
	URL     string
	Details []antigravityErrorDetail
}

func (e *antigravityError) Error() string {
	parts := []string{fmt.Sprintf("antigravity HTTP %d", e.Status)}
	if e.Reason != "" {
		parts = append(parts, "reason="+e.Reason)
	}
	if e.URL != "" {
		parts = append(parts, "verify_at="+e.URL)
	}
	parts = append(parts, truncateOneLine(redactCredentialMaterial(firstNonEmpty(e.Message, "upstream error"), ""), 300))
	return strings.Join(parts, " ")
}

func antigravityHTTPError(status int, body []byte) error {
	err := &antigravityError{Status: status, Message: strings.TrimSpace(string(body))}
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Details []struct {
				Type     string            `json:"@type"`
				Reason   string            `json:"reason"`
				Domain   string            `json:"domain"`
				Metadata map[string]string `json:"metadata"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return err
	}
	if parsed.Error.Message != "" {
		err.Message = parsed.Error.Message
	}
	err.Code = parsed.Error.Status
	for _, detail := range parsed.Error.Details {
		if strings.HasSuffix(detail.Type, "ErrorInfo") || detail.Reason != "" {
			err.Reason = detail.Reason
			err.Details = append(err.Details, antigravityErrorDetail{
				Reason: detail.Reason, Domain: detail.Domain, Metadata: detail.Metadata,
			})
			if detail.Metadata != nil {
				if url := detail.Metadata["validation_url"]; url != "" {
					err.URL = url
				}
			}
		}
	}
	return err
}

// IsAntigravityValidationRequired reports whether err is the account-level gate
// that no retry, header or project can clear.
func IsAntigravityValidationRequired(err error) bool {
	if err == nil {
		return false
	}
	var agErr *antigravityError
	if errors.As(err, &agErr) {
		if agErr.Reason == AntigravityValidationRequiredReason {
			return true
		}
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, strings.ToLower(AntigravityValidationRequiredReason)) ||
		strings.Contains(lower, "verify your account to continue")
}

// IsAntigravityValidationRequiredText classifies a message string (e.g. a
// Verify() detail line that no longer carries the typed error) as the gate.
func IsAntigravityValidationRequiredText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, strings.ToLower(AntigravityValidationRequiredReason)) ||
		strings.Contains(lower, "verify your account to continue")
}

// IsAntigravityFreeTierIneligible reports the onboardUser refusal that shadows
// the same account lock.
func IsAntigravityFreeTierIneligible(err error) bool {
	if err == nil {
		return false
	}
	var agErr *antigravityError
	if errors.As(err, &agErr) && agErr.Reason == AntigravityFreeTierNotEligibleReason {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(AntigravityFreeTierNotEligibleReason))
}

// ---- request building ----

// AntigravityRequest is a rendered upstream call.
type AntigravityRequest struct {
	Model string
	Body  map[string]any
}

// BuildAntigravityRequest converts the gateway's flattened payload into an
// Antigravity request: Gemini-style contents, a systemInstruction that always
// carries today's date, and the documented envelope fields.
//
// Response shape is {"response":{"candidates":[...] },"traceId":"..."} for
// both streaming and non-streaming, which extractCodeAssistParts already
// unwraps.
func BuildAntigravityRequest(payload map[string]any, today string) AntigravityRequest {
	model := ResolveAntigravityModel(stringFromMap(payload, "model"))
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
	if genCfg := antigravityGenerationConfig(payload, model); len(genCfg) > 0 {
		request["generationConfig"] = genCfg
	}
	if tools, ok := payload["tools"]; ok {
		request["tools"] = tools
	}

	return AntigravityRequest{
		Model: model,
		Body: map[string]any{
			"model":     model,
			"project":   antigravityProjectFromPayload(payload),
			"userAgent": "antigravity",
			"requestId": firstNonEmpty(stringFromMap(payload, "request_id"), UUID()),
			"request":   request,
		},
	}
}

// antigravityGenerationConfig adds maxOutputTokens and lets a thinking model's
// budget through. Upstream requires maxOutputTokens > thinkingBudget.
func antigravityGenerationConfig(payload map[string]any, model string) map[string]any {
	cfg := generationConfigFromPayload(payload)
	if thinking, ok := payload["thinking"].(bool); ok && AntigravityThinkingModels[model] && thinking {
		budget := 8000
		if value, ok := payload["thinking_budget"].(float64); ok && value > 0 {
			budget = int(value)
		}
		cfg["thinkingConfig"] = map[string]any{"thinkingBudget": budget, "includeThoughts": true}
		if current, ok := cfg["maxOutputTokens"].(int); !ok || current <= budget {
			cfg["maxOutputTokens"] = budget + 4000
		}
	}
	return cfg
}

func mergeThinkingOptions(body map[string]any, options map[string]any) {
	request, ok := body["request"].(map[string]any)
	if !ok {
		return
	}
	cfg, ok := request["generationConfig"].(map[string]any)
	if !ok {
		cfg = map[string]any{}
		request["generationConfig"] = cfg
	}
	cfg["thinkingConfig"] = options
	if budget, ok := options["thinkingBudget"].(float64); ok && budget > 0 {
		if current, ok := cfg["maxOutputTokens"].(int); !ok || float64(current) <= budget {
			cfg["maxOutputTokens"] = int(budget) + 4000
		}
	}
}

func antigravityProjectFromPayload(payload map[string]any) string {
	if project := strings.TrimSpace(stringFromMap(payload, "antigravity_project")); project != "" {
		return project
	}
	if project := strings.TrimSpace(stringFromMap(payload, "code_assist_project")); project != "" {
		return project
	}
	if project := strings.TrimSpace(getenv("ANTIGRAVITY_PROJECT")); project != "" {
		return project
	}
	return getenvOr("CODE_ASSIST_PROJECT", DefaultCodeAssistProject)
}

// ---- helpers ----

func getenv(key string) string {
	return getenvOr(key, "")
}

func getenvOr(key, fallback string) string {
	if value := strings.TrimSpace(envLookup(key)); value != "" {
		return value
	}
	return fallback
}

// envLookup is a var so tests can stub the environment.
var envLookup = func(key string) string { return strings.TrimSpace(os.Getenv(key)) }

func antigravityRetryAfterSeconds(message string, minWait, maxWait time.Duration) time.Duration {
	return retryAfterSeconds(message, minWait, maxWait)
}

// antigravityStatusFromError is used by the router to map upstream failures.
func antigravityStatusFromError(err error) int {
	status := StatusCodeOf(err)
	if status != 0 {
		return status
	}
	if IsAntigravityValidationRequired(err) {
		return http.StatusForbidden
	}
	return http.StatusBadGateway
}

// parseAntigravityRetryDelay reads a bare seconds value for tests.
func parseAntigravityRetryDelay(seconds string) time.Duration {
	value, err := strconv.Atoi(strings.TrimSpace(seconds))
	if err != nil || value <= 0 {
		return 0
	}
	return time.Duration(value) * time.Second
}

// onboardProjectID pulls the provisioned project out of an onboardUser answer.
func onboardProjectID(parsed map[string]any) string {
	envelope, ok := parsed["response"].(map[string]any)
	if !ok {
		return ""
	}
	return tierID(envelope["cloudaicompanionProject"])
}
