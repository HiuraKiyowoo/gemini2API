package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Image generation is a PLUGGABLE backend, selected per request by the type of
// credential that is actually capable of producing images.
//
// Verified upstream facts that drive this design (see TEMUAN.md):
//   - Google OAuth (Code Assist / cloudcode-pa) credentials CANNOT generate
//     images. generativelanguage.googleapis.com returns HTTP 403
//     ACCESS_TOKEN_SCOPE_INSUFFICIENT for the model list and for every
//     imagen-*-generate-*:predict call, and cloudcode-pa returns HTTP 404 for
//     gemini-*-image* models. Requesting more OAuth scopes does not help.
//   - Imagen works only with an API key:
//     POST https://generativelanguage.googleapis.com/v1beta/models/<model>:predict
//     header x-goog-api-key: <key>
//     body   {"instances":[{"prompt":"..."}],"parameters":{"sampleCount":1}}
//   - The cookie-based Gemini/Qwen web path can emit image URLs on
//     cdn.qwenlm.ai, so it stays available for cookie-type accounts.
//
// When no capable backend exists the endpoint must fail loudly with an
// actionable message; it must never fake image output.

// Provider identifiers reported by the backend.
const (
	ImageProviderNone     = ""
	ImageProviderNotFound = "not_found"
)

const (
	// EnvImagenAPIKey is the primary env var for the Imagen API key.
	EnvImagenAPIKey = "IMAGEN_API_KEY"
	// EnvImagenModel overrides which Imagen model is called.
	EnvImagenModel = "IMAGEN_MODEL"
	// EnvImageOutDir overrides where generated images are written.
	EnvImageOutDir = "IMAGE_OUTPUT_DIR"
	// DefaultImagenModel is verified to be the documented Imagen predict model.
	DefaultImagenModel = "imagen-4.0-generate-001"

	imagenEndpointBase   = "https://generativelanguage.googleapis.com/v1beta/models/"
	imagenAPIKeyMaxBytes = 64 * 1024
	imagenResponseMax    = 48 << 20
	chatAnswerMax        = 2 << 20
)

// ErrNoImageBackend is returned when no backend can serve the request. Callers
// surface this as a 503 with the wrapped reason, which names the fix.
var ErrNoImageBackend = errors.New("no image backend configured")

// Config carries everything the image backends need, resolved from env and
// runtime admin settings.
type Config struct {
	ImagenAPIKey    string
	ImagenModel     string
	ImageOutputDir  string
	PublicImageBase string
	UserAgent       string
	HTTPClient      *http.Client
}

// ConfigFromEnv builds Config from process environment.
func ConfigFromEnv(baseDir string) Config {
	out := strings.TrimSpace(os.Getenv(EnvImageOutDir))
	if out == "" {
		out = filepath.Join(baseDir, "data", "generated", "images")
	}
	return Config{
		ImagenAPIKey:   firstEnvValue(EnvImagenAPIKey, "GOOGLE_IMAGEN_API_KEY", "GEMINI_IMAGEN_API_KEY"),
		ImagenModel:    normalizeImagenModel(os.Getenv(EnvImagenModel)),
		ImageOutputDir: out,
		UserAgent:      "Mozilla/5.0 gemini2api-go",
		HTTPClient:     &http.Client{Timeout: 3 * time.Minute},
	}
}

func normalizeImagenModel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return DefaultImagenModel
	}
	return strings.TrimPrefix(value, "models/")
}

func firstEnvValue(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

// ImageRequest is a single image generation request.
type ImageRequest struct {
	Prompt     string
	Model      string
	Account    string // account kind ("cookie"/"qwen") or "" for any
	N          int
	Width      int
	Height     int
	Ratio      string
	Size       string
	Provider   string // optional forced provider selection
	Credential any   // opaque upstream client for the chat-based backend
}

// ImageResult holds the generated images.
type ImageResult struct {
	Provider  string
	Model     string
	URLs      []string
	MimeTypes []string
}

// ImageBackend generates images for one credential/provider type.
type ImageBackend interface {
	Name() string
	Available() error
	Generate(ctx context.Context, req ImageRequest) (*ImageResult, error)
}

// ImageDispatcher picks a backend by credential/provider type.
type ImageDispatcher struct {
	mu       sync.RWMutex
	byName   map[string]ImageBackend
	byKind   map[string]string
	order    []string
	chatFunc func(context.Context, ImageRequest) (*ImageResult, error)
}

func NewImageDispatcher() *ImageDispatcher {
	return &ImageDispatcher{
		byName: map[string]ImageBackend{},
		byKind: map[string]string{
			"cookie":      "chat",
			"qwen":        "chat",
			"gemini":      "chat",
			"oauth":       "imagen",
			"google":      "imagen",
			"cloudcode":   "imagen",
			"api-key":     "imagen",
			"api_key":     "imagen",
			"generative":  "imagen",
			"google-api":  "imagen",
			"imagen":      "imagen",
			"service_acc": "imagen",
		},
	}
}

// Register adds or replaces a backend. Registration order is the fallback
// preference order.
func (d *ImageDispatcher) Register(backend ImageBackend) {
	if d == nil || backend == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	name := strings.ToLower(strings.TrimSpace(backend.Name()))
	if name == "" {
		return
	}
	if _, exists := d.byName[name]; !exists {
		d.order = append(d.order, name)
	}
	d.byName[name] = backend
}

// RegisterChat wires the cookie-based web-chat image path (extracts
// cdn.qwenlm.ai URLs from a completed chat turn).
func (d *ImageDispatcher) RegisterChat(fn func(context.Context, ImageRequest) (*ImageResult, error)) {
	if d == nil || fn == nil {
		return
	}
	d.mu.Lock()
	d.chatFunc = fn
	d.mu.Unlock()
	d.RegisterFunc("chat", func() error { return nil }, func(ctx context.Context, req ImageRequest) (*ImageResult, error) {
		d.mu.RLock()
		inner := d.chatFunc
		d.mu.RUnlock()
		if inner == nil {
			return nil, fmt.Errorf("chat image backend not wired")
		}
		return inner(ctx, req)
	})
}

type funcBackend struct {
	name      string
	available func() error
	generate  func(context.Context, ImageRequest) (*ImageResult, error)
}

func (b funcBackend) Name() string                    { return b.name }
func (b funcBackend) Available() error                { return b.available() }
func (b funcBackend) Generate(ctx context.Context, r ImageRequest) (*ImageResult, error) {
	return b.generate(ctx, r)
}

// RegisterFunc registers an adapter without requiring a concrete type.
func (d *ImageDispatcher) RegisterFunc(name string, available func() error, generate func(context.Context, ImageRequest) (*ImageResult, error)) {
	if d == nil || strings.TrimSpace(name) == "" || generate == nil {
		return
	}
	if available == nil {
		available = func() error { return nil }
	}
	d.Register(funcBackend{name: name, available: available, generate: generate})
}

// Status reports availability per backend (never includes secrets).
func (d *ImageDispatcher) Status() []map[string]any {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := []map[string]any{}
	for _, name := range d.order {
		backend := d.byName[name]
		err := backend.Available()
		entry := map[string]any{"provider": name, "available": err == nil}
		if err != nil {
			entry["reason"] = err.Error()
		}
		out = append(out, entry)
	}
	return out
}

// Generate selects the backend and produces images.
func (d *ImageDispatcher) Generate(ctx context.Context, req ImageRequest) (*ImageResult, error) {
	if d == nil {
		return nil, fmt.Errorf("%w: image dispatcher is not initialised", ErrNoImageBackend)
	}
	d.mu.RLock()
	names := append([]string(nil), d.order...)
	byName := make(map[string]ImageBackend, len(d.byName))
	for k, v := range d.byName {
		byName[k] = v
	}
	byKind := make(map[string]string, len(d.byKind))
	for k, v := range d.byKind {
		byKind[k] = v
	}
	d.mu.RUnlock()

	preferred := strings.ToLower(strings.TrimSpace(req.Provider))
	if preferred == "" {
		preferred = byKind[strings.ToLower(strings.TrimSpace(req.Account))]
	}
	if preferred == ImageProviderNotFound {
		preferred = ""
	}
	if preferred != "" {
		backend, ok := byName[preferred]
		if !ok {
			return nil, fmt.Errorf("%w: unknown image backend %q (available: %s)",
				ErrNoImageBackend, preferred, strings.Join(names, ", "))
		}
		if err := backend.Available(); err != nil {
			return nil, fmt.Errorf("%w: backend %q: %v", ErrNoImageBackend, preferred, err)
		}
		res, err := backend.Generate(ctx, req)
		if err != nil {
			return nil, err
		}
		if res != nil && res.Provider == "" {
			res.Provider = preferred
		}
		return res, nil
	}

	// No credential type to route on: try every available backend in order.
	var reasons []string
	for _, name := range names {
		backend := byName[name]
		if err := backend.Available(); err != nil {
			reasons = append(reasons, name+": "+err.Error())
			continue
		}
		res, err := backend.Generate(ctx, req)
		if err != nil {
			reasons = append(reasons, name+": "+err.Error())
			continue
		}
		if res == nil || len(res.URLs) == 0 {
			reasons = append(reasons, name+": produced no image URL")
			continue
		}
		if res.Provider == "" {
			res.Provider = name
		}
		return res, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrNoImageBackend, strings.Join(reasons, " | "))
}

// ---- Imagen backend (API key) ----

type ImagenBackend struct {
	cfg Config
	mu  sync.Mutex
}

func NewImagenBackend(cfg Config) *ImagenBackend {
	return &ImagenBackend{cfg: cfg}
}

func (b *ImagenBackend) Name() string { return "imagen" }

func (b *ImagenBackend) SetAPIKey(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cfg.ImagenAPIKey = strings.TrimSpace(key)
}

func (b *ImagenBackend) SetModel(model string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cfg.ImagenModel = normalizeImagenModel(model)
}

func (b *ImagenBackend) SetOutputDir(dir string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if strings.TrimSpace(dir) != "" {
		b.cfg.ImageOutputDir = dir
	}
}

func (b *ImagenBackend) snapshot() Config {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg
}

// Available reports whether an Imagen API key is configured. The error is the
// actionable message returned to API clients.
func (b *ImagenBackend) Available() error {
	cfg := b.snapshot()
	if strings.TrimSpace(cfg.ImagenAPIKey) == "" {
		return fmt.Errorf(
			"Imagen API key is required for image generation: set the %s environment "+
				"variable to a Google AI Studio (generativelanguage) API key, or supply "+
				"one via PUT /api/admin/settings {\"imagen_api_key\": \"...\"}. "+
				"Google OAuth / cloudcode-pa credentials cannot generate images "+
				"(HTTP 403 ACCESS_TOKEN_SCOPE_INSUFFICIENT), so image requests will keep "+
				"failing until a key is configured.",
			EnvImagenAPIKey)
	}
	if len(cfg.ImagenAPIKey) > imagenAPIKeyMaxBytes {
		return fmt.Errorf("%s is too large to be a valid API key", EnvImagenAPIKey)
	}
	return nil
}

func (b *ImagenBackend) Model() string {
	cfg := b.snapshot()
	if cfg.ImagenModel != "" {
		return cfg.ImagenModel
	}
	return DefaultImagenModel
}

func imagenAspectRatio(width, height int, ratio string) string {
	if v := strings.TrimSpace(ratio); v != "" {
		return v
	}
	if width > height && height > 0 {
		return "16:9"
	}
	if height > width && width > 0 {
		return "9:16"
	}
	return "1:1"
}

type imagenResponse struct {
	Predictions []struct {
		BytesBase64Encoded string `json:"bytesBase64Encoded"`
		MimeType           string `json:"mimeType"`
	} `json:"predictions"`
}

type ImageError struct {
	HTTPStatus int
	Message    string
}

func (e *ImageError) Error() string { return e.Message }

// Generate calls Imagen predict and returns accessible URLs.
func (b *ImagenBackend) Generate(ctx context.Context, req ImageRequest) (*ImageResult, error) {
	if err := b.Available(); err != nil {
		return nil, err
	}
	cfg := b.snapshot()
	n := req.N
	if n < 1 {
		n = 1
	}
	if n > 4 {
		n = 4
	}
	model := b.Model()
	if override := strings.ToLower(strings.TrimSpace(req.Model)); override != "" &&
		strings.HasPrefix(override, "imagen") {
		model = strings.TrimPrefix(override, "models/")
	}
	body, err := json.Marshal(map[string]any{
		"instances": []map[string]any{{"prompt": req.Prompt}},
		"parameters": map[string]any{
			"sampleCount":  n,
			"aspectRatio":  imagenAspectRatio(req.Width, req.Height, req.Ratio),
		},
	})
	if err != nil {
		return nil, &ImageError{HTTPStatus: http.StatusInternalServerError, Message: "failed to encode Imagen request: " + err.Error()}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, imagenEndpointBase+model+":predict", bytes.NewReader(body))
	if err != nil {
		return nil, &ImageError{HTTPStatus: http.StatusInternalServerError, Message: "failed to build Imagen request: " + err.Error()}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", cfg.ImagenAPIKey)
	if cfg.UserAgent != "" {
		httpReq.Header.Set("User-Agent", cfg.UserAgent)
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &ImageError{HTTPStatus: http.StatusBadGateway, Message: "Imagen request failed: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, imagenResponseMax))
	if err != nil {
		return nil, &ImageError{HTTPStatus: http.StatusBadGateway, Message: "failed reading Imagen response: " + err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &ImageError{HTTPStatus: imagenStatusClass(resp.StatusCode), Message: imagenErrorMessage(resp.StatusCode, raw, model)}
	}
	var decoded imagenResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, &ImageError{HTTPStatus: http.StatusBadGateway, Message: "Imagen returned a response that is not valid JSON: " + truncateForError(string(raw), 300)}
	}
	if len(decoded.Predictions) == 0 {
		return nil, &ImageError{HTTPStatus: http.StatusBadGateway, Message: "Imagen returned no predictions for model " + model}
	}
	if err := os.MkdirAll(cfg.ImageOutputDir, 0o755); err != nil {
		return nil, &ImageError{HTTPStatus: http.StatusInternalServerError, Message: "cannot create image output directory " + cfg.ImageOutputDir + ": " + err.Error()}
	}

	result := &ImageResult{Provider: "imagen", Model: model}
	for i, prediction := range decoded.Predictions {
		if len(result.URLs) >= n {
			break
		}
		data, err := base64.StdEncoding.DecodeString(prediction.BytesBase64Encoded)
		if err != nil {
			return nil, &ImageError{HTTPStatus: http.StatusBadGateway, Message: fmt.Sprintf("Imagen prediction %d is not valid base64: %v", i, err)}
		}
		if len(data) == 0 {
			continue
		}
		mimeType := prediction.MimeType
		if mimeType == "" {
			mimeType = "image/png"
		}
		path, err := writeImageFile(cfg.ImageOutputDir, model, i, mimeType, data)
		if err != nil {
			return nil, &ImageError{HTTPStatus: http.StatusInternalServerError, Message: "failed to store generated image: " + err.Error()}
		}
		result.URLs = append(result.URLs, publicImageURL(cfg, path))
		result.MimeTypes = append(result.MimeTypes, mimeType)
	}
	if len(result.URLs) == 0 {
		return nil, &ImageError{HTTPStatus: http.StatusBadGateway, Message: "Imagen returned predictions with no image bytes"}
	}
	return result, nil
}

// imagenStatusClass maps an Imagen HTTP status to the gateway status we return.
// 401/403 is misconfiguration (missing/invalid key or scope), which callers
// must see as 503 with the actionable reason, not as 200.
func imagenStatusClass(status int) int {
	switch {
	case status == http.StatusTooManyRequests:
		return http.StatusTooManyRequests
	case status == http.StatusNotFound:
		return http.StatusNotFound
	case status == http.StatusBadRequest:
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

func imagenErrorMessage(status int, raw []byte, model string) string {
	message := truncateForError(strings.TrimSpace(string(raw)), 400)
	var parsed struct {
		Error struct {
			Message string         `json:"message"`
			Status  string         `json:"status"`
			Details []map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err == nil && strings.TrimSpace(parsed.Error.Message) != "" {
		message = parsed.Error.Message
		if parsed.Error.Status != "" {
			message += " (" + parsed.Error.Status + ")"
		}
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("Imagen rejected the %s key for model %s: %s. The key must be a Google AI Studio API key; Google OAuth tokens are rejected with ACCESS_TOKEN_SCOPE_INSUFFICIENT.",
			EnvImagenAPIKey, model, message)
	case http.StatusTooManyRequests:
		return fmt.Sprintf("Imagen quota exceeded for model %s: %s", model, message)
	case http.StatusNotFound:
		return fmt.Sprintf("Imagen model %q not found: %s", model, message)
	default:
		return fmt.Sprintf("Imagen HTTP %d for model %s: %s", status, model, message)
	}
}

func writeImageFile(dir, model string, index int, mimeType string, data []byte) (string, error) {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%d-%s.%s", sanitizeForFilename(model), index, hex.EncodeToString(random[:]), imageExt(mimeType))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return name, nil
}

func imageExt(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	default:
		return "png"
	}
}

func sanitizeForFilename(value string) string {
	out := make([]rune, 0, len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "image"
	}
	return string(out)
}

func publicImageURL(cfg Config, filename string) string {
	rel := strings.TrimPrefix(filepath.ToSlash(filename), "/")
	if base := strings.TrimRight(strings.TrimSpace(cfg.PublicImageBase), "/"); base != "" {
		return base + "/api/images/files/" + rel
	}
	return "/api/images/files/" + rel
}

func truncateForError(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}

var (
	_ ImageBackend = (*ImagenBackend)(nil)
	_ ImageBackend = funcBackend{}
	_ error        = (*ImageError)(nil)
)
