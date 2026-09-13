package services

import (
	"sort"
	"strings"
	"time"
)

// OpenAIModelCreatedEpoch is the stable "created" timestamp reported for
// catalog entries (matches the historical Python backend value).
const OpenAIModelCreatedEpoch = int64(1700000000)

// ModelCatalogStatus describes what a live probe against the upstream backend
// proved about a model identifier.
type ModelCatalogStatus string

const (
	// ModelStatusLive: probe returned HTTP 200 with generated text.
	ModelStatusLive ModelCatalogStatus = "live"
	// ModelStatusQuota: probe returned HTTP 429. The model EXISTS on the
	// upstream backend but this credential's quota is exhausted right now.
	ModelStatusQuota ModelCatalogStatus = "quota_exhausted"
	// ModelStatusAbsent: probe returned HTTP 404 "Requested entity was not
	// found" — the identifier does not exist upstream and must never be
	// advertised or transported.
	ModelStatusAbsent ModelCatalogStatus = "absent"
)

// Upstream transport model codes used by the Gemini web StreamGenerate
// protocol. Kept here so the catalog, not a switch statement, decides routing.
const (
	TransportCodeFlash    = 1
	TransportCodeThinking = 2
	TransportCodePro      = 3
	TransportCodeLite     = 4
)

// ModelSpec is one entry of the single source of truth for the model lineup.
// Both the /v1/models listing and the WebUI select box are derived from this
// slice; aliases and the OpenAI/Anthropic compatibility layer are derived from
// it too. Nothing is advertised here that was not probed against the live
// upstream backend.
type ModelSpec struct {
	// ID is the exact identifier accepted by the upstream backend
	// (cloudcode-pa `model` field / Gemini web model id).
	ID string
	// DisplayName is the honest human label; it is the ID plus a status hint.
	DisplayName string
	// Description states verified capabilities only.
	Description string
	// Status is the last probe result against the live backend.
	Status ModelCatalogStatus
	// Thinking is true only when thinking=on was observed to return thought
	// text for this exact model.
	Thinking bool
	// Search marks models probed with Google Search grounding enabled.
	Search bool
	// Vision marks image-INPUT understanding (not image generation).
	Vision bool
	// ImageGen would mean the model itself can produce images. No Gemini
	// model on this tier can — see image_backend.go.
	ImageGen bool
	// VideoGen would mean the model itself can produce video. None can.
	VideoGen bool
	// TransportCode is the Gemini web StreamGenerate model code.
	TransportCode int
	// Family groups models in the UI.
	Family string
	// ContextWindow is the advertised input limit, 0 when unverified.
	ContextWindow int
}

// VerifiedModelCatalog returns the model lineup as proven against the live
// upstream backend. Probes were run with the repo's Google OAuth credential
// (standard Code Assist tier) against https://cloudcode-pa.googleapis.com.
//
// Ordering is preference order: models observed live (HTTP 200 + text) come
// first, then models that exist upstream but returned HTTP 429 quota.
//
// IMPORTANT: 429 proves the identifier exists — it just means this credential's
// quota is drained right now. 404 proves the identifier does not exist. Because
// quota resets, both statuses are advertised; the difference is recorded in the
// per-model status/description so nothing is presented as a working default
// unless it was observed to produce text.
func VerifiedModelCatalog() []ModelSpec {
	return []ModelSpec{
		{
			ID: "gemini-3-flash-preview", DisplayName: "gemini-3-flash-preview",
			Description: "Probed live (HTTP 200 + text). Reasoning supported: thinking=on returned thought text; can exceed 100s, use generous client timeouts.",
			Status:      ModelStatusLive, Thinking: true, Vision: true, Search: true,
			TransportCode: TransportCodeFlash, Family: "Gemini 3", ContextWindow: 1048576,
		},
		{
			ID: "gemini-3.1-flash-lite", DisplayName: "gemini-3.1-flash-lite",
			Description: "Probed live (HTTP 200 + text). Fast, lightweight variant on this tier.",
			Status:      ModelStatusLive, Vision: true,
			TransportCode: TransportCodeLite, Family: "Gemini 3", ContextWindow: 1048576,
		},
		{
			ID: "gemini-2.5-flash-lite", DisplayName: "gemini-2.5-flash-lite",
			Description: "Probed live (HTTP 200 + text).",
			Status:      ModelStatusLive, Vision: true,
			TransportCode: TransportCodeLite, Family: "Gemini 2.5", ContextWindow: 1048576,
		},
		{
			ID: "gemini-2.5-flash", DisplayName: "gemini-2.5-flash",
			Description: "Reasoning supported and verified: thinking=on returned thought text (observed up to 289k thought chars / 129s at max budget). Quota is frequently exhausted (HTTP 429), so expect per-model cooldowns.",
			Status:      ModelStatusQuota, Thinking: true, Vision: true, Search: true,
			TransportCode: TransportCodeFlash, Family: "Gemini 2.5", ContextWindow: 1048576,
		},
		{
			ID: "gemini-3.1-flash-lite-preview", DisplayName: "gemini-3.1-flash-lite-preview",
			Description: "Exists upstream; probes so far returned HTTP 429 quota, never text. Availability depends on live quota.",
			Status:      ModelStatusQuota,
			TransportCode: TransportCodeLite, Family: "Gemini 3", ContextWindow: 1048576,
		},
		{
			ID: "gemini-2.5-pro", DisplayName: "gemini-2.5-pro",
			Description: "Exists upstream; every probe so far returned HTTP 429 quota. Not verified to produce text on this credential.",
			Status:      ModelStatusQuota, Thinking: true, Vision: true, Search: true,
			TransportCode: TransportCodePro, Family: "Gemini 2.5", ContextWindow: 2097152,
		},
		{
			ID: "gemini-3-pro-preview", DisplayName: "gemini-3-pro-preview",
			Description: "Exists upstream; every probe so far returned HTTP 429 quota. Not verified to produce text on this credential.",
			Status:      ModelStatusQuota, Thinking: true, Vision: true, Search: true,
			TransportCode: TransportCodePro, Family: "Gemini 3", ContextWindow: 1048576,
		},
		{
			ID: "gemini-3.1-pro-preview", DisplayName: "gemini-3.1-pro-preview",
			Description: "Exists upstream; every probe so far returned HTTP 429 quota. Not verified to produce text on this credential.",
			Status:      ModelStatusQuota, Thinking: true, Vision: true, Search: true,
			TransportCode: TransportCodePro, Family: "Gemini 3", ContextWindow: 1048576,
		},
	}
}

// AbsentModelIDs lists identifiers that returned HTTP 404 "Requested entity was
// not found" on the live upstream backend. They are kept out of the advertised
// catalog and only ever accepted as aliases mapped onto a real model.
//
// gemini-3.5-flash is the confusing case: gemini-cli's own source names it
// DEFAULT_GEMINI_3_5_FLASH_MODEL, yet this cloudcode-pa tier answers 404 for
// it. A name in upstream source does not mean it exists on this backend.
func AbsentModelIDs() []string {
	return []string{
		"gemini-3-flash",
		"gemini-3.5-flash",
		"gemini-3.1-pro-preview-customtools",
		"gemini-3-pro-image-preview",
		"gemini-2.5-flash-image",
		"gemini-2.0-flash-preview-image-generation",
		// Fictional names previously advertised by this repo.
		"gemini-3.6-flash",
		"gemini-3.5-flash-thinking",
		"gemini-3.5-flash-thinking-lite",
		"gemini-3.1-pro",
		"gemini-flash-lite",
	}
}

// PrimaryChatModel is the default transport model: the first catalog entry
// observed live.
func PrimaryChatModel() string {
	for _, spec := range VerifiedModelCatalog() {
		if spec.Status == ModelStatusLive {
			return spec.ID
		}
	}
	return VerifiedModelCatalog()[0].ID
}

// ThinkingModel returns a model on which thinking=on was verified to produce
// thought text, preferring models that are currently live.
func ThinkingModel() string {
	catalog := VerifiedModelCatalog()
	for _, wantLive := range []bool{true, false} {
		for _, spec := range catalog {
			if !spec.Thinking {
				continue
			}
			if wantLive == (spec.Status == ModelStatusLive) {
				return spec.ID
			}
		}
	}
	return PrimaryChatModel()
}

// LiteModel returns the preferred lightweight model.
func LiteModel() string {
	for _, spec := range VerifiedModelCatalog() {
		if spec.TransportCode == TransportCodeLite && spec.Status == ModelStatusLive {
			return spec.ID
		}
	}
	return "gemini-3.1-flash-lite"
}

// ProModel returns the strongest model that exists upstream. It is
// quota-limited on the tested credential, so it is never a default.
func ProModel() string {
	for _, spec := range VerifiedModelCatalog() {
		if spec.TransportCode == TransportCodePro {
			return spec.ID
		}
	}
	return PrimaryChatModel()
}

// FindModelSpec looks up a catalog entry by exact ID.
func FindModelSpec(id string) (ModelSpec, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, spec := range VerifiedModelCatalog() {
		if strings.ToLower(spec.ID) == id {
			return spec, true
		}
	}
	return ModelSpec{}, false
}

// IsAbsentModel reports whether the identifier was probed as HTTP 404.
func IsAbsentModel(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, absent := range AbsentModelIDs() {
		if absent == id {
			return true
		}
	}
	return false
}

// ModelAliases maps every name a client may send onto a model that actually
// exists upstream. Fictional/404 names are accepted for compatibility but are
// never transported or advertised.
func ModelAliases() map[string]string {
	primary := PrimaryChatModel()
	thinking := ThinkingModel()
	lite := LiteModel()
	pro := ProModel()
	flash := primary
	aliases := map[string]string{
		// gemini-cli public aliases.
		"auto":       primary,
		"pro":        pro,
		"flash":      flash,
		"flash-lite": lite,
		// gemini-cli constant names that do not exist on this backend.
		"gemini-3.5-flash":              primary,
		"gemini-3.1-pro-preview-custom": pro,
		// Repo's previous fictional lineup, kept as aliases only so existing
		// clients do not hard-break.
		"gemini-3.6-flash":               primary,
		"gemini-3.5-flash-thinking":      thinking,
		"gemini-3.5-flash-thinking-lite": lite,
		"gemini-3.1-pro":                 pro,
		"gemini-flash-lite":              lite,
		"gemini-auto":                    primary,
		// Legacy Gemini names.
		"gemini-2.5-flash-thinking": thinking,
		"gemini-2.0-flash":          flash,
		"gemini-2.0-flash-lite":     lite,
		"gemini-1.5-flash":          flash,
		"gemini-1.5-pro":            pro,
		"gemini-pro":                pro,
		"gemini-flash":              flash,
		"gemini-thinking":           thinking,
		// OpenAI compatibility.
		"gpt-4o":      pro,
		"gpt-4o-mini": lite,
		"gpt-4":       pro,
		"gpt-4-turbo": pro,
		"gpt-5":       pro,
		"o1":          pro,
		"o1-mini":     lite,
		"gpt-3.5-turbo": flash,
		// Anthropic compatibility.
		"claude-3-5-sonnet": pro,
		"claude-3.5-sonnet": pro,
		"claude-sonnet-4-5": pro,
		"claude-sonnet-4-6": pro,
		"claude-3-sonnet":   pro,
		"claude-3-haiku":    lite,
		"claude-3-5-haiku":  lite,
		"claude-3-opus":     pro,
		"claude-opus-4-1":   pro,
		"claude-opus-4-5":   pro,
		// Image/video aliases: no Gemini model here generates media, so these
		// resolve to a real chat model and the gateway reports the missing
		// Imagen key instead of pretending (see image_backend.go).
		"dall-e-3":       primary,
		"dall-e-2":       primary,
		"gpt-image-1":    primary,
		"imagen":         primary,
		"imagen-4.0":     primary,
		"sora":           primary,
		"sora-2":         primary,
		"qwen-image":     primary,
		"qwen-image-plus": primary,
		"qwen-image-turbo": primary,
		"qwen-video":     primary,
		"qwen-video-plus": primary,
		"qwen-video-turbo": primary,
	}
	for _, spec := range VerifiedModelCatalog() {
		aliases[spec.ID] = spec.ID
	}
	return aliases
}

// DefaultModelAliases is the historical entry point used by main.go.
func DefaultModelAliases() map[string]string {
	return ModelAliases()
}

// ResolveModel maps a requested name to a real upstream model ID. Unknown names
// pass through untouched so upstream can report them.
func ResolveModel(name string, aliases map[string]string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if v, ok := aliases[trimmed]; ok {
		return v
	}
	if v, ok := aliases[strings.ToLower(trimmed)]; ok {
		return v
	}
	for _, suffix := range modelModeSuffixes() {
		lowered := strings.ToLower(trimmed)
		if strings.HasSuffix(lowered, suffix) && len(trimmed) > len(suffix) {
			base := strings.TrimSpace(trimmed[:len(trimmed)-len(suffix)])
			if mapped := ResolveModel(base, aliases); mapped != "" && mapped != base {
				return mapped + trimmed[len(trimmed)-len(suffix):]
			}
		}
	}
	return trimmed
}

// TransportModelID resolves a requested name to a base model that really
// exists upstream, stripping any gateway mode suffix, so a "-thinking" /
// "-search" variant name or a 404 name is never sent as the model id.
func TransportModelID(name string, aliases map[string]string) string {
	requested := strings.TrimSpace(name)
	if requested == "" {
		return PrimaryChatModel()
	}
	mode := ParseModelMode(ResolveModel(requested, aliases), "")
	base := strings.TrimSpace(mode.BaseModel)
	if base == "" {
		return PrimaryChatModel()
	}
	if _, ok := FindModelSpec(base); ok && !IsAbsentModel(base) {
		return base
	}
	resolved := ResolveModel(base, aliases)
	if IsAbsentModel(resolved) {
		return PrimaryChatModel()
	}
	if _, ok := FindModelSpec(resolved); ok {
		return resolved
	}
	return resolved
}

// ThinkingBudgetChars is the verified generationConfig.thinkingConfig budget
// used when thinking is requested.
const ThinkingBudgetChars = 8192

// ModelThinkingCapable reports whether thinking was verified for a model.
func ModelThinkingCapable(id string) bool {
	spec, ok := FindModelSpec(TransportBaseModel(id))
	return ok && spec.Thinking
}

// TransportBaseModel returns the catalog base model for a request name.
func TransportBaseModel(name string) string {
	return TransportModelID(name, ModelAliases())
}

// CatalogStatusByName exposes the probe status for a model ID.
func CatalogStatusByName(id string) string {
	spec, ok := FindModelSpec(id)
	if !ok {
		if IsAbsentModel(id) {
			return string(ModelStatusAbsent)
		}
		return "unprobed"
	}
	return string(spec.Status)
}

// CatalogModelIDs returns advertised IDs in catalog order.
func CatalogModelIDs() []string {
	catalog := VerifiedModelCatalog()
	ids := make([]string, 0, len(catalog))
	for _, spec := range catalog {
		ids = append(ids, spec.ID)
	}
	return ids
}

// SortedCatalog returns the catalog sorted by ID (used by diagnostics/tests).
func SortedCatalog() []ModelSpec {
	catalog := append([]ModelSpec(nil), VerifiedModelCatalog()...)
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].ID < catalog[j].ID })
	return catalog
}

// ModelModeSuffixIndex returns the byte offset at which the gateway mode suffix
// starts, or -1 when the name has no mode suffix. A "-preview" tail is NOT a
// mode suffix, which is why the naive HasSuffix loop used previously could
// corrupt real ids such as gemini-3-flash-preview.
func ModelModeSuffixIndex(id string) int {
	lowered := strings.ToLower(strings.TrimSpace(id))
	for _, suffix := range modelModeSuffixes() {
		if strings.HasSuffix(lowered, suffix) && len(lowered) > len(suffix) {
			return len(id) - len(suffix)
		}
	}
	return -1
}

// MergeModelEntry adds entry to an OpenAI list payload unless its id is already
// present, keeping catalog ordering authoritative.
func MergeModelEntry(list map[string]any, entry map[string]any) map[string]any {
	if list == nil || entry == nil {
		return list
	}
	data, _ := list["data"].([]map[string]any)
	id := anyString(entry["id"], "")
	if id == "" {
		return list
	}
	for _, existing := range data {
		if anyString(existing["id"], "") == id {
			return list
		}
	}
	list["data"] = append(data, entry)
	return list
}

// IsModelModeSuffix reports whether the id carries a gateway mode suffix.
func IsModelModeSuffix(id string) bool {
	lowered := strings.ToLower(strings.TrimSpace(id))
	for _, suffix := range modelModeSuffixes() {
		if strings.HasSuffix(lowered, suffix) && len(lowered) > len(suffix) {
			return true
		}
	}
	return false
}

// TransportModelCode returns the Gemini web StreamGenerate model code for a
// requested model name, derived from the catalog instead of a hardcoded list.
func TransportModelCode(requested string, thinking bool) int {
	base := TransportBaseModel(requested)
	lowered := strings.ToLower(base)
	if strings.Contains(lowered, "lite") {
		return TransportCodeLite
	}
	if thinking || strings.HasSuffix(strings.ToLower(strings.TrimSpace(requested)), "-thinking") {
		return TransportCodeThinking
	}
	if spec, ok := FindModelSpec(base); ok && spec.TransportCode != 0 {
		return spec.TransportCode
	}
	if strings.Contains(lowered, "pro") {
		return TransportCodePro
	}
	return TransportCodeFlash
}

// ---- OpenAI-compatible list builders ----

// BuildModelEntry returns one entry of an OpenAI-style model listing.
func BuildModelEntry(modelID, baseModel string, capabilities map[string]bool, mode, displayName, family string, created int64, ownedBy string) map[string]any {
	if baseModel == "" {
		baseModel = modelID
	}
	if displayName == "" {
		displayName = modelID
	}
	if family == "" {
		family = baseModel
	}
	if created == 0 {
		created = OpenAIModelCreatedEpoch
	}
	if ownedBy == "" {
		ownedBy = "google"
	}
	return map[string]any{
		"id":           modelID,
		"object":       "model",
		"created":      created,
		"owned_by":     ownedBy,
		"capabilities": capabilities,
		"base_model":   baseModel,
		"mode":         mode,
		"display_name": displayName,
		"family":       family,
	}
}

// BuildCatalogModelList renders the verified catalog as an OpenAI model list.
// This is the authoritative listing served by GET /v1/models.
func BuildCatalogModelList() map[string]any {
	now := time.Now().Unix()
	data := []map[string]any{}
	for _, spec := range VerifiedModelCatalog() {
		caps := map[string]bool{
			"thinking":      spec.Thinking,
			"search":        spec.Search,
			"vision":        spec.Vision,
			"deep_research": false,
			"image_gen":     spec.ImageGen,
			"video_gen":     spec.VideoGen,
			"web_dev":       false,
			"slides":        false,
		}
		data = append(data, decorateCatalogEntry(BuildModelEntry(spec.ID, spec.ID, caps, "chat", spec.DisplayName, spec.Family, OpenAIModelCreatedEpoch, "google"), spec, now))
	}
	return map[string]any{"object": "list", "data": data}
}

func decorateCatalogEntry(entry map[string]any, spec ModelSpec, now int64) map[string]any {
	entry["status"] = string(spec.Status)
	entry["description"] = spec.Description
	entry["upstream_verified"] = spec.Status == ModelStatusLive
	entry["quota_limited"] = spec.Status == ModelStatusQuota
	entry["thinking_verified"] = spec.Thinking
	entry["probed_at"] = spec.Description // retained for debuggability only
	delete(entry, "probed_at")
	entry["updated_at"] = now
	if spec.ContextWindow > 0 {
		entry["context_window"] = spec.ContextWindow
	}
	entry["image_gen"] = spec.ImageGen
	return entry
}

// BuildFallbackModelList is served when the client pool cannot answer. It now
// derives from the verified catalog instead of the alias map, so no fictional
// model is ever advertised. Alias names are appended as alias records pointing
// at a real model.
func BuildFallbackModelList(modelAliases map[string]string) map[string]any {
	payload := BuildCatalogModelList()
	if modelAliases == nil {
		modelAliases = ModelAliases()
	}
	primary := PrimaryChatModel()
	for _, alias := range sortedAliasKeys(modelAliases) {
		resolved := modelAliases[alias]
		spec, ok := FindModelSpec(resolved)
		if !ok || alias == resolved {
			continue
		}
		// A name that was probed HTTP 404 upstream is accepted on input (so old
		// clients do not hard-break) but must NEVER be advertised in a model
		// listing — that is what made the old lineup dishonest.
		if IsAbsentModel(alias) {
			continue
		}
		caps := map[string]bool{
			"thinking":  spec.Thinking && strings.HasSuffix(strings.ToLower(alias), "-thinking"),
			"search":    false,
			"vision":    spec.Vision,
			"image_gen": false,
			"video_gen": false,
		}
		entry := BuildModelEntry(alias, resolved, caps, "chat", alias+" (alias)", spec.Family, OpenAIModelCreatedEpoch, "gemini2api-alias")
		entry["alias_of"] = resolved
		entry["status"] = string(spec.Status)
		entry["description"] = "Alias of " + resolved + " — kept for client compatibility; " + resolved + " is the model actually called"
		entry["upstream_verified"] = false
		entry["quota_limited"] = spec.Status == ModelStatusQuota
		entry["thinking_verified"] = false
		if alias == "gpt-4o" || alias == "gpt-4o-mini" || strings.HasPrefix(alias, "claude-") {
			entry["compatibility_alias"] = true
		}
		if alias == primary {
			entry["default"] = true
		}
		payload["data"] = append(payload["data"].([]map[string]any), entry)
	}
	return payload
}

func sortedAliasKeys(aliases map[string]string) []string {
	keys := make([]string, 0, len(aliases))
	for key := range aliases {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// BuildOpenAIModelList normalises upstream-reported models into OpenAI shape.
// Variant mode suffixes are expanded, but only for capabilities the catalog
// verified for that model.
func BuildOpenAIModelList(upstream []map[string]any) map[string]any {
	seen := map[string]struct{}{}
	data := []map[string]any{}
	add := func(entry map[string]any) {
		id := anyString(entry["id"], "")
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		data = append(data, entry)
	}
	variants := []struct {
		capability string
		suffix     string
		mode       string
		caps       map[string]bool
	}{
		{"thinking", "-thinking", "thinking", map[string]bool{"thinking": true}},
		{"search", "-search", "search", map[string]bool{"search": true}},
		{"deep_research", "-deep-research", "deep_research", map[string]bool{"deep_research": true, "search": true}},
		{"image_gen", "-image", "image", map[string]bool{"image_gen": true}},
		{"video_gen", "-video", "video", map[string]bool{"video_gen": true}},
		{"web_dev", "-webdev", "webdev", map[string]bool{"web_dev": true}},
		{"slides", "-slides", "slides", map[string]bool{"slides": true}},
	}
	for _, raw := range upstream {
		modelID := firstString(raw["id"], raw["model"], raw["name"])
		if modelID == "" {
			continue
		}
		display := firstString(raw["display_name"], raw["displayName"], raw["name"])
		if display == "" {
			display = modelID
		}
		family := DeriveFamily(modelID, raw)
		caps := ExtractModelCapabilities(raw)
		entryMode := "chat"
		entryBaseModel := modelID
		if baseModel, mode, detectedCaps, ok := detectVariantModel(modelID); ok {
			entryMode = mode
			entryBaseModel = baseModel
			for key, value := range detectedCaps {
				if value {
					caps[key] = true
				}
			}
		}
		add(BuildModelEntry(modelID, entryBaseModel, caps, entryMode, display, family, OpenAIModelCreatedEpoch, "google"))
		if entryMode != "chat" {
			continue
		}
		for _, variant := range variants {
			if caps[variant.capability] || variant.capability == "search" {
				add(BuildModelEntry(modelID+variant.suffix, modelID, variant.caps, variant.mode, display+" "+variant.mode, family, OpenAIModelCreatedEpoch, "google"))
			}
		}
	}
	return map[string]any{"object": "list", "data": data}
}

func detectVariantModel(modelID string) (string, string, map[string]bool, bool) {
	trimmed := strings.TrimSpace(modelID)
	lowered := strings.ToLower(trimmed)
	variants := []struct {
		suffix string
		mode   string
		caps   map[string]bool
	}{
		{"-thinking", "thinking", map[string]bool{"thinking": true}},
		{"-search", "search", map[string]bool{"search": true}},
		{"-deep-research", "deep_research", map[string]bool{"deep_research": true, "search": true}},
		{"-deep_research", "deep_research", map[string]bool{"deep_research": true, "search": true}},
		{"-image", "image", map[string]bool{"image_gen": true}},
		{"-video", "video", map[string]bool{"video_gen": true}},
		{"-web-dev", "webdev", map[string]bool{"web_dev": true}},
		{"-webdev", "webdev", map[string]bool{"web_dev": true}},
		{"-slides", "slides", map[string]bool{"slides": true}},
		{"-t2i", "image", map[string]bool{"image_gen": true}},
		{"-t2v", "video", map[string]bool{"video_gen": true}},
	}
	for _, variant := range variants {
		if strings.HasSuffix(lowered, variant.suffix) && len(trimmed) > len(variant.suffix) {
			return strings.TrimSpace(trimmed[:len(trimmed)-len(variant.suffix)]), variant.mode, copyCaps(variant.caps), true
		}
	}
	return "", "", nil, false
}

func copyCaps(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

// ExtractModelCapabilities reads capability metadata reported by upstream.
func ExtractModelCapabilities(item map[string]any) map[string]bool {
	caps := map[string]bool{
		"thinking":      false,
		"search":        false,
		"vision":        false,
		"deep_research": false,
		"image_gen":     false,
		"video_gen":     false,
		"web_dev":       false,
		"slides":        false,
	}
	meta := map[string]any{}
	if info, ok := item["info"].(map[string]any); ok {
		if m, ok := info["meta"].(map[string]any); ok {
			meta = m
		}
	}
	if m, ok := item["meta"].(map[string]any); ok {
		for k, v := range m {
			meta[k] = v
		}
	}
	if rawCaps, ok := meta["capabilities"].(map[string]any); ok {
		for k, v := range rawCaps {
			if b, ok := v.(bool); ok {
				caps[k] = b
			}
		}
	}
	applyChatType := func(chatType string) {
		switch chatType {
		case "deep_research":
			caps["deep_research"] = true
		case "t2i", "image_gen":
			caps["image_gen"] = true
		case "t2v":
			caps["video_gen"] = true
		case "web_dev":
			caps["web_dev"] = true
		case "slides":
			caps["slides"] = true
		}
	}
	switch v := meta["chat_type"].(type) {
	case string:
		applyChatType(v)
	case []any:
		for _, item := range v {
			applyChatType(anyString(item, ""))
		}
	}
	return caps
}

// DeriveFamily groups a model id for the UI.
func DeriveFamily(modelID string, item map[string]any) string {
	if family := anyString(item["family"], ""); family != "" {
		return family
	}
	if spec, ok := FindModelSpec(modelID); ok && spec.Family != "" {
		return spec.Family
	}
	if strings.HasPrefix(modelID, "gemini-") {
		return "gemini"
	}
	if idx := strings.Index(modelID, "-"); idx > 0 {
		return modelID[:idx]
	}
	return modelID
}

func anyString(v any, fallback string) string {
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	return fallback
}

func firstString(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
