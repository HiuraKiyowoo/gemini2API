import { API_BASE } from "./api"
import { getAuthHeader } from "./auth"

export type ModelCapability = {
  thinking?: boolean
  search?: boolean
  vision?: boolean
  deep_research?: boolean
  image_gen?: boolean
  video_gen?: boolean
  web_dev?: boolean
  slides?: boolean
}

export type ModelOption = {
  id: string
  base_model?: string
  family?: string
  mode?: string
  display_name?: string
  description?: string
  status?: "live" | "quota_exhausted" | "absent" | string
  upstream_verified?: boolean
  quota_limited?: boolean
  alias_of?: string
  capabilities?: ModelCapability
}

export type ModelGroup = {
  family: string
  models: ModelOption[]
}

// These are the REAL model ids probed against the upstream backend
// (cloudcode-pa, standard Code Assist tier). They mirror
// backend/services/model_catalog.go:VerifiedModelCatalog — one source of truth.
//
//   live            = probe returned HTTP 200 with generated text
//   quota_exhausted = model exists upstream but only ever answered HTTP 429
//
// The old fictional ids (gemini-3.6-flash, gemini-3.5-flash-thinking,
// gemini-3.5-flash-thinking-lite, gemini-3.1-pro) returned HTTP 404 upstream and
// are no longer offered here. The backend still accepts them as aliases.
export const VERIFIED_CHAT_MODELS: ModelOption[] = [
  {
    id: "gemini-3-flash-preview", base_model: "gemini-3-flash-preview", family: "Gemini 3", mode: "chat",
    display_name: "gemini-3-flash-preview", status: "live", upstream_verified: true,
    description: "HTTP 200 + text. Thinking verified.",
    capabilities: { thinking: true, search: true, vision: true },
  },
  {
    id: "gemini-3.1-flash-lite", base_model: "gemini-3.1-flash-lite", family: "Gemini 3", mode: "chat",
    display_name: "gemini-3.1-flash-lite", status: "live", upstream_verified: true,
    description: "HTTP 200 + text. Fast lightweight variant.",
    capabilities: { vision: true },
  },
  {
    id: "gemini-2.5-flash-lite", base_model: "gemini-2.5-flash-lite", family: "Gemini 2.5", mode: "chat",
    display_name: "gemini-2.5-flash-lite", status: "live", upstream_verified: true,
    description: "HTTP 200 + text.",
    capabilities: { vision: true },
  },
  {
    id: "gemini-2.5-flash", base_model: "gemini-2.5-flash", family: "Gemini 2.5", mode: "chat",
    display_name: "gemini-2.5-flash", status: "quota_exhausted", quota_limited: true,
    description: "Exists upstream; probes returned HTTP 429 quota. Thinking verified when quota allows.",
    capabilities: { thinking: true, search: true, vision: true },
  },
  {
    id: "gemini-3.1-flash-lite-preview", base_model: "gemini-3.1-flash-lite-preview", family: "Gemini 3", mode: "chat",
    display_name: "gemini-3.1-flash-lite-preview", status: "quota_exhausted", quota_limited: true,
    description: "Exists upstream; probes returned HTTP 429 quota.",
    capabilities: {},
  },
  {
    id: "gemini-2.5-pro", base_model: "gemini-2.5-pro", family: "Gemini 2.5", mode: "chat",
    display_name: "gemini-2.5-pro", status: "quota_exhausted", quota_limited: true,
    description: "Exists upstream; every probe returned HTTP 429 quota. Never verified to produce text.",
    capabilities: { thinking: true, search: true, vision: true },
  },
  {
    id: "gemini-3-pro-preview", base_model: "gemini-3-pro-preview", family: "Gemini 3", mode: "chat",
    display_name: "gemini-3-pro-preview", status: "quota_exhausted", quota_limited: true,
    description: "Exists upstream; every probe returned HTTP 429 quota. Never verified to produce text.",
    capabilities: { thinking: true, search: true, vision: true },
  },
  {
    id: "gemini-3.1-pro-preview", base_model: "gemini-3.1-pro-preview", family: "Gemini 3", mode: "chat",
    display_name: "gemini-3.1-pro-preview", status: "quota_exhausted", quota_limited: true,
    description: "Exists upstream; every probe returned HTTP 429 quota. Never verified to produce text.",
    capabilities: { thinking: true, search: true, vision: true },
  },
]

// Thinking variants exist only for models where thinking=on was observed to
// return thought text: gemini-3-flash-preview and gemini-2.5-flash.
export const VERIFIED_THINKING_MODELS: ModelOption[] = [
  {
    id: "gemini-3-flash-preview-thinking", base_model: "gemini-3-flash-preview", family: "Gemini 3", mode: "thinking",
    display_name: "gemini-3-flash-preview thinking", status: "live", upstream_verified: true,
    description: "Verified: thinking=on returned thought text. Can exceed 100s.",
    capabilities: { thinking: true },
  },
  {
    id: "gemini-2.5-flash-thinking", base_model: "gemini-2.5-flash", family: "Gemini 2.5", mode: "thinking",
    display_name: "gemini-2.5-flash thinking", status: "quota_exhausted", quota_limited: true,
    description: "Verified: thinking=on returned up to 289k thought chars (129s). Quota-limited.",
    capabilities: { thinking: true },
  },
]

export const FALLBACK_CHAT_MODELS: ModelOption[] = [
  ...VERIFIED_CHAT_MODELS,
  ...VERIFIED_THINKING_MODELS,
]

// No Gemini model on this tier generates images or video (all gemini-*-image* /
// *-preview-image-generation ids return HTTP 404, and the OAuth credential gets
// HTTP 403 ACCESS_TOKEN_SCOPE_INSUFFICIENT from generativelanguage). Image
// output requires the Imagen backend with an IMAGEN_API_KEY, so the selectable
// ids here are the real Imagen predict models.
export const FALLBACK_IMAGE_MODELS: ModelOption[] = [
  {
    id: "imagen-4.0-generate-001", base_model: "imagen-4.0-generate-001", family: "Imagen", mode: "image",
    display_name: "imagen-4.0-generate-001", status: "live",
    description: "Requires IMAGEN_API_KEY (Google AI Studio key). OAuth credentials are rejected with ACCESS_TOKEN_SCOPE_INSUFFICIENT.",
    capabilities: { image_gen: true },
  },
]

export const FALLBACK_VIDEO_MODELS: ModelOption[] = []

export const CAPABILITY_LABELS: Array<{ key: keyof ModelCapability; label: string }> = [
  { key: "thinking", label: "Penalaran" },
  { key: "search", label: "Pencarian" },
  { key: "vision", label: "Visual" },
  { key: "deep_research", label: "Riset" },
  { key: "image_gen", label: "Gambar" },
  { key: "video_gen", label: "Video" },
  { key: "web_dev", label: "Web Dev" },
  { key: "slides", label: "Presentasi" },
]

const MODEL_MODE_SUFFIX_RE = /-(thinking|search|deep-research|deep_research|image|video|webdev|web-dev|slides|t2i|t2v)$/i
const TEXT_TEST_MODES = new Set(["chat", "thinking", "search", "deep_research"])
const GENERATION_MODES = new Set(["image", "video", "webdev", "slides"])
const MODE_NAME_SUFFIX: Record<string, string> = {
  thinking: "thinking",
  search: "search",
  deep_research: "deep_research",
  image: "image",
  video: "video",
  webdev: "webdev",
  slides: "slides",
}

function asText(value: unknown): string {
  return typeof value === "string" ? value : ""
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" ? value as Record<string, unknown> : {}
}

function modelMode(option: ModelOption): string {
  return option.mode || inferModeFromId(option.id)
}

function inferModeFromId(modelId: string): string {
  const id = modelId.toLowerCase()
  if (id.endsWith("-thinking")) return "thinking"
  if (id.endsWith("-search")) return "search"
  if (id.endsWith("-deep-research") || id.endsWith("-deep_research")) return "deep_research"
  if (id.endsWith("-image") || id.endsWith("-t2i")) return "image"
  if (id.endsWith("-video") || id.endsWith("-t2v")) return "video"
  if (id.endsWith("-webdev") || id.endsWith("-web-dev")) return "webdev"
  if (id.endsWith("-slides")) return "slides"
  return "chat"
}

function familyOf(option: ModelOption): string {
  if (option.family) return option.family
  const base = option.base_model || option.id.replace(MODEL_MODE_SUFFIX_RE, "")
  if (base.startsWith("gemini-")) {
    const parts = base.split("-", 2)
    if (parts.length >= 2) return `Gemini ${parts[1]}`
  }
  if (base.startsWith("imagen-")) return "Imagen"
  return base.split("-", 1)[0] || "Gemini"
}

export function normalizeModelOption(value: unknown): ModelOption | null {
  if (typeof value === "string" && value) return { id: value, mode: inferModeFromId(value), capabilities: {} }
  const record = asRecord(value)
  const id = asText(record.id)
  if (!id) return null
  return {
    id,
    base_model: asText(record.base_model) || undefined,
    family: asText(record.family) || undefined,
    mode: asText(record.mode) || inferModeFromId(id),
    display_name: asText(record.display_name) || undefined,
    description: asText(record.description) || undefined,
    status: asText(record.status) || undefined,
    upstream_verified: typeof record.upstream_verified === "boolean" ? record.upstream_verified : undefined,
    quota_limited: typeof record.quota_limited === "boolean" ? record.quota_limited : undefined,
    alias_of: asText(record.alias_of) || undefined,
    capabilities: asRecord(record.capabilities) as ModelCapability,
  }
}

export async function fetchModelOptions(): Promise<ModelOption[]> {
  const response = await fetch(`${API_BASE}/v1/models`, { headers: getAuthHeader() })
  if (!response.ok) return []
  const payload = await response.json()
  const rawItems = Array.isArray(payload?.data) ? payload.data : []
  return rawItems
    .map(normalizeModelOption)
    .filter((item: ModelOption | null): item is ModelOption => Boolean(item?.id))
}

export function isBaseModelOption(option: ModelOption): boolean {
  return option.base_model ? option.id === option.base_model : !MODEL_MODE_SUFFIX_RE.test(option.id)
}

export function isThinkingVariant(modelId: string): boolean {
  return /-thinking$/i.test(modelId)
}

export function capabilityBadges(option?: ModelOption): string[] {
  if (!option?.capabilities) return []
  return CAPABILITY_LABELS.filter(item => option.capabilities?.[item.key]).map(item => item.label)
}

// A model record is an alias when the backend says so, or when its base_model
// differs from its id. Aliases stay in the list (clients may already use them)
// but never become the default and are labelled in the UI.
export function isAliasOption(option: ModelOption): boolean {
  if (option.alias_of) return true
  return Boolean(option.base_model && option.base_model !== option.id)
}

export function filterTextTestModels(options: ModelOption[]): ModelOption[] {
  const real = options.filter(option => !isAliasOption(option))
  const filtered = real.filter(option => TEXT_TEST_MODES.has(modelMode(option)) && !GENERATION_MODES.has(modelMode(option)))
  const baseModels = filtered.filter(option => modelMode(option) === "chat")
  const existingIds = new Set(filtered.map(option => option.id))
  // Only add the -search variant when the catalog says the model was probed
  // with search grounding, so we never advertise an unverified capability.
  const searchVariants = baseModels
    .filter(option => option.capabilities?.search)
    .map(option => ({
      ...option,
      id: option.id.endsWith("-search") ? option.id : `${option.id}-search`,
      base_model: option.base_model || option.id,
      mode: "search",
      display_name: `${option.display_name || option.id} search`,
      capabilities: { ...option.capabilities, search: true },
    }))
    .filter(option => !existingIds.has(option.id))
  const withSearch = [...filtered, ...searchVariants]
  return withSearch.length ? withSearch : FALLBACK_CHAT_MODELS
}

export function filterImageModels(options: ModelOption[]): ModelOption[] {
  const explicit = options.filter(option => modelMode(option) === "image")
  if (explicit.length) return explicit
  const capable = options
    .filter(option => option.capabilities?.image_gen && !GENERATION_MODES.has(modelMode(option)))
    .map(option => ({
      ...option,
      id: option.id.endsWith("-image") ? option.id : `${option.id}-image`,
      base_model: option.base_model || option.id,
      mode: "image",
      display_name: `${option.display_name || option.id} image`,
      capabilities: { ...option.capabilities, image_gen: true },
    }))
  return capable.length ? capable : FALLBACK_IMAGE_MODELS
}

export function filterVideoModels(options: ModelOption[]): ModelOption[] {
  const explicit = options.filter(option => modelMode(option) === "video")
  if (explicit.length) return explicit
  const capable = options
    .filter(option => option.capabilities?.video_gen && !GENERATION_MODES.has(modelMode(option)))
    .map(option => ({
      ...option,
      id: option.id.endsWith("-video") ? option.id : `${option.id}-video`,
      base_model: option.base_model || option.id,
      mode: "video",
      display_name: `${option.display_name || option.id} video`,
      capabilities: { ...option.capabilities, video_gen: true },
    }))
  return capable.length ? capable : FALLBACK_VIDEO_MODELS
}

// chooseDefaultModel prefers a model that was actually observed live, then a
// base (non-alias) model, and never defaults to a quota-only model if a live
// one is offered.
export function chooseDefaultModel(options: ModelOption[], currentModel?: string, preferredId?: string): string {
  const usable = options.filter(option => !isAliasOption(option))
  const pool = usable.length ? usable : options
  if (currentModel && options.some(option => option.id === currentModel)) return currentModel
  if (preferredId && options.some(option => option.id === preferredId)) return preferredId
  const live = pool.find(option => option.upstream_verified && modelMode(option) === "chat")
  const base = live || pool.find(isBaseModelOption) || pool.find(option => modelMode(option) === "chat")
  return base?.id || options[0]?.id || preferredId || ""
}

export function groupModelOptions(options: ModelOption[]): ModelGroup[] {
  const groups = new Map<string, ModelOption[]>()
  options.forEach(option => {
    const family = familyOf(option)
    groups.set(family, [...(groups.get(family) || []), option])
  })
  return Array.from(groups.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([family, models]) => ({
      family,
      models: models.sort((a, b) => Number(isAliasOption(a)) - Number(isAliasOption(b)) || a.id.localeCompare(b.id)),
    }))
}

export function formatModeLabel(mode?: string): string {
  switch (mode) {
    case "thinking": return "Penalaran"
    case "search": return "Pencarian"
    case "deep_research": return "Riset"
    case "image": return "Gambar"
    case "video": return "Video"
    case "webdev": return "Web Dev"
    case "slides": return "Presentasi"
    default: return "Chat"
  }
}

// Status tag keeps the UI honest: a model that has only ever answered 429 is
// shown as "kuota", and an alias is shown as "alias" instead of pretending to
// be its own model.
export function modelStatusTag(option: ModelOption): string | null {
  if (isAliasOption(option)) return "alias"
  if (option.status === "quota_exhausted" || option.quota_limited) return "kuota 429"
  if (option.status === "absent") return "tidak ada"
  return null
}

export function formatModelName(option: ModelOption): string {
  const mode = modelMode(option)
  const suffix = MODE_NAME_SUFFIX[mode]
  const rawName = option.display_name || option.id
  const name = suffix ? rawName.replace(new RegExp(`\\s+${suffix}$`, "i"), "") : rawName
  const tag = modelStatusTag(option)
  const label = tag ? `${name} [${tag}]` : name
  return label === option.id ? option.id : `${label} (${option.id})`
}

export function formatModelOptionLabel(option: ModelOption): string {
  return `${formatModelName(option)} · ${formatModeLabel(modelMode(option))}`
}
