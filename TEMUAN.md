# gemini2API — Temuan Terverifikasi (probe live, 2026-09-12)

## 1. Jalur yang HIDUP: OAuth Google → cloudcode-pa

Proven dengan probe nyata (bukan asumsi):

| Tes | Hasil |
|---|---|
| `loadCodeAssist` | HTTP 200, `standard-tier` ("Gemini Code Assist") |
| `generateContent` | HTTP 200, teks kembali |
| `streamGenerateContent` | HTTP 200, JSON array |
| Refresh access token | HTTP 200, murni HTTP (tanpa browser) |

Kredensial: `/root/gemini2API/data/google_oauth.json` (refresh_token, chmod 600, gitignored).

```
CLIENT_ID     = 681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com
CLIENT_SECRET = <dari env CODE_ASSIST_CLIENT_SECRET; tidak di-commit>
TOKEN_URL     = https://oauth2.googleapis.com/token
ENDPOINT      = https://cloudcode-pa.googleapis.com/v1internal
HEADER        = Authorization: Bearer <access_token>   (access_token umur ~3600s)
```

Body request:
```json
{"model": "<model>", "project": "cloudshell-gca",
 "request": {"contents": [{"role":"user","parts":[{"text":"..."}]}],
             "systemInstruction": {"parts":[{"text":"..."}]}}}
```

Ekstraksi teks: `json.loads(seluruh_body)` → list → `ch["response"]` atau `ch` → `candidates[0].content.parts[].text`

## 2. Model yang BENAR-BENAR HIDUP di tier ini

HIDUP (HTTP 200 + teks):
- `gemini-3-flash-preview`
- `gemini-2.5-flash`
- `gemini-3.1-flash-lite`
- `gemini-3.1-flash-lite-preview`

429 (rate limit, belum pernah tembus):
- `gemini-2.5-pro`, `gemini-3-pro-preview`, `gemini-3.1-pro-preview`

404 (tidak ada di backend cloudcode ini):
- `gemini-3-flash`, `gemini-3.5-flash`, `gemini-3.1-pro-preview-customtools`,
  `gemini-3-pro-image-preview`, `gemini-2.5-flash-image`,
  `gemini-2.0-flash-preview-image-generation`

⚠️ Model lineup repo sekarang (`gemini-3.6-flash`, `gemini-3.5-flash-thinking`, `gemini-3.1-pro`)
**tidak ada satu pun yang cocok** dengan model asli upstream. Ini yang diminta user disamakan.

Alias publik gemini-cli (untuk mapping): `auto`, `pro`, `flash`, `flash-lite`.

## 3. IMAGE GENERATION — jalur OAuth TIDAK BISA

Proven gagal:

| Percobaan | Hasil |
|---|---|
| `generativelanguage.googleapis.com/v1beta/models` pakai OAuth | **403** `ACCESS_TOKEN_SCOPE_INSUFFICIENT` |
| Imagen predict (imagen-4.0-generate-001 / 3.0-generate-002 / 3.0-generate-001) | **403** scope insufficient |
| cloudcode-pa model image (`gemini-*-image*`) | **404** — model tidak ada |

Artinya: scope OAuth cuma `cloud-platform` + userinfo, **tanpa** scope generative-language.
Minta scope tambahan tidak akan menolong karena tier Code Assist tidak menyediakan model image.

**Fix yang benar:** image gen harus lewat jalur TERPISAH dengan Imagen API key:
```
POST https://generativelanguage.googleapis.com/v1beta/models/imagen-4.0-generate-001:predict
Header: x-goog-api-key: <IMAGEN_API_KEY>
Body:   {"instances":[{"prompt":"..."}],"parameters":{"sampleCount":1}}
```
Kode sekarang di `main.go:5860 createImageURLs` hardcode Qwen (`cdn.qwenlm.ai`) → mati total.
Solusinya: adapter image pluggable yang pilih backend by tipe akun
(OAuth/cloudcode → Imagen API key; akun cookie Qwen → jalur lama).

## 4. Bug parser (sudah dibuktikan)

`streamGenerateContent` balikin **JSON array pretty-printed multi-line**, BUKAN JSONL/SSE.
Parser per-baris gagal total → teks kosong padahal HTTP 200. Wajib `json.loads(seluruh body)`.

## 5. Tanggal tidak ada di model

Tanpa systemInstruction: tanya tahun → jawab **2024** (salah, sekarang 2026).
Dengan systemInstruction berisi tanggal → jawab **2026** (benar).
Wajib inject tanggal ke `request.systemInstruction`.

## 6. Titik integrasi di backend Go

Repo sudah punya seam bagus:
- `upstream/qwen_executor.go:10` — interface `ChatClient`: `CreateChat`, `DeleteChat`, `StreamChat`
- `upstream/sse_consumer.go:165` — `ConsumeGeminiStream` (format `wrb.fr`, KHUSUS jalur web)
- `main.go:7986` — `type QwenClient struct` + `type GeminiClient = QwenClient`
- `main.go:8280/8286/8290` — `CreateChat`, `DeleteChat`, `StreamChat`
- `main.go:172` — `app.client = NewQwenClient(...)` (titik pemilihan client)
- `main.go:1891-1914` — `modelAliases` map
- `main.go:8517-8519` — daftar display model untuk UI
- `main.go:5860` — `createImageURLs` (image gen, hardcode Qwen)
- `main.go:8077` — `SignIn`, `main.go:8153` — `fetchAtAndBl` (jalur cookie SAPISIDHASH)

## 7. Kondisi operasional

- Backend jalan di :7860 (v2.0.0-go), binary `bin/gemini2api-backend`
- `data/accounts.json` = `[]` (kosong, belum ada akun)
- `ADMIN_KEY` belum di-set (warning di log)
- 429 sering → wajib retry + cooldown per model di pool

## 8. THINKING / REASONING — SUPPORT (probe live 2026-09-12)

Kontrol via `request.generationConfig.thinkingConfig`:
```json
{"includeThoughts": true, "thinkingBudget": 8192}   // thinking ON
{"thinkingBudget": 0}                                // thinking OFF
```
Teks reasoning muncul di part dengan flag `"thought": true` (terpisah dari jawaban akhir).
`thoughtSignature` = artefank internal, JANGAN disajikan sebagai konten.

Hasil (soal jebakan +20%/-20%):

| Model | default | thinking=on | thinking=off |
|---|---|---|---|
| gemini-2.5-flash | 11.1s, tanpa thought | **289.441 char thought, 129.0s** | 4.0s |
| gemini-3-flash-preview | 9.7s | 525 char, 8.6s | 5.2s |
| gemini-3.1-flash-lite | 3.4s | 745 char, 6.3s | 5.5s |
| semua model pro | 429, ga pernah tembus | | |

⚠️ 2.5-flash thinking bisa 129 detik → timeout client wajib panjang.
⚠️ Jangan advertise varian `-thinking` untuk model pro (429 permanen di tier ini).

## 9. VISION — SUPPORT (probe live)

Gambar PNG dikirim via `inlineData` → gemini-2.5-flash & gemini-3.1-flash-lite
identifikasi warna/posisi dengan benar. `image_url` di request OpenAI bisa dipakai.
Script: `probe_vision.py`.
