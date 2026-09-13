# 🚀 gemini2API — Gateway API Google Gemini Self-Hosted

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8.svg?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+" />
  <img src="https://img.shields.io/badge/React-19-61DAFB.svg?style=for-the-badge&logo=react&logoColor=black" alt="React 19" />
  <img src="https://img.shields.io/badge/API-OpenAI%20%7C%20Anthropic%20%7C%20Gemini-green.svg?style=for-the-badge" alt="Multi-Protocol API" />
  <img src="https://img.shields.io/badge/Upstream-cloudcode--pa-orange.svg?style=for-the-badge" alt="cloudcode-pa" />
  <img src="https://img.shields.io/badge/License-GPL--3.0-blue.svg?style=for-the-badge" alt="License GPL-3.0" />
</p>

Gateway AI mandiri yang mengubah **Google Gemini Code Assist (cloudcode-pa)**
menjadi API yang kompatibel dengan **OpenAI API**, **Anthropic Messages**, dan
**Google Gemini REST API**. Backend Go, WebUI React, jalan minimal di Android
Termux maupun VPS Linux.

> **Kejujuran model.** Daftar model yang diiklankan berasal dari **satu sumber
> kebenaran**: `backend/services/model_catalog.go:VerifiedModelCatalog`. Hanya
> model yang benar-benar ada di backend upstream yang diiklankan — lengkap
> dengan status `live` / `quota_exhausted`. Nama lama yang terbukti 404 tidak
> lagi diiklankan, hanya tetap diterima sebagai alias agar klien lama tak
> putus. AutoGen/SDK yang memakai nama model asli akan selalu dapat model asli.

---

## 🌟 Fitur Utama

- ⚡ **Transport cloudcode-pa (Code Assist) via OAuth Google**: refresh token
  murni HTTP (tanpa browser), single-flight per token, auto write-back saat
  Google merotasi refresh token.
- 🔀 **Routing model dari katalog**: `model_catalog.go` menentukan transport
  code, alias, dan varian mode (`-thinking`, `-search`) — bukan switch
  hardcode. Nama 404 tidak pernah dikirim ke upstream.
- 🧠 **Thinking / reasoning**: `generationConfig.thinkingConfig` (`8192` budget).
  Teks reasoning muncul terpisah (part `"thought": true`) dan tidak disajikan
  sebagai jawaban akhir.
- 👁️ **Vision**: gambar via `inlineData` (input image), bukan image generation.
- 📅 **Injeksi tanggal**: systemInstruction berisi tanggal berjalan supaya model
  tidak menjawab tahun yang salah.
- 🔄 **Account pool**: multi-akun, failover, cooldown rate-limit per model.
- 🎨 **WebUI Dashboard**: React 19 + Tailwind + Lucide, bahasa Indonesia.
- 🖼️ **Image generation (Imagen)**: **butuh `IMAGEN_API_KEY`** — lihat catatan
  di bawah. Tanpa key itu, endpoint image **gagal loud**, bukan mengembalikan
  output palsu.

---

## ⚡ Instalasi & Menjalankan

### Build

```bash
# Backend Go
cd backend && go build -o ../bin/gemini2api-backend . && cd ..

# Frontend (hasil ke frontend/dist, disajikan oleh backend)
cd frontend && npm install && npm run build && cd ..
```

### Jalankan

```bash
./start.sh -d      # daemon di background (log: logs/output.log)
./start.sh         # foreground (Ctrl+C untuk stop)
./stop.sh          # hentikan
```

WebUI & API: **`http://127.0.0.1:7860`**. Version update: `./update.sh`.

Untuk mode dev (build backend + Vite dev server sekaligus) ada
`go run start-all.go` — dijalankan dari root repo.

---

## 🔑 Konfigurasi Environment

Salin `.env.example` ke `.env` dan isi. Env var penting:

| Env var | Wajib | Keterangan |
|---|---|---|
| `ADMIN_KEY` | Ya (untuk WebUI/admin) | Kunci admin WebUI & API admin. Kosong → warning di log, endpoint admin menolak. |
| `CODE_ASSIST_CLIENT_SECRET` | Ya (untuk OAuth upstream) | Client secret OAuth gemini-cli. **Tidak di-commit.** |
| `CODE_ASSIST_CLIENT_ID` | Opsional | Default memakai client ID gemini-cli bawaan. |
| `IMAGEN_API_KEY` | Untuk image gen | Google AI Studio key. Tanpa ini image gen gagal loud. |
| `BASE_DIR` | Opsional | Base dir untuk `data/` & `logs/` di luar Docker. Docker mengeset `/app`. |
| `PORT` | Opsional | Default `7860`. |
| `LOG_LEVEL` | Opsional | Default `INFO`. |

Env lain (tuning streaming/retry/pool/context) ada di `.env.example` dengan
komentar masing-masing. Docker Compose memakai `HOST_PORT`, `HOST_DATA_DIR`,
`HOST_LOGS_DIR`.

### Login OAuth Google

Kredensial upstream memakai OAuth Google. Helper ada di root repo:

```bash
export CODE_ASSIST_CLIENT_SECRET="<client-secret>"
python3 oauth_google.py url                # cetak URL authorize
python3 oauth_google.py tukar "<url|code>" # tukar code -> simpan refresh_token
python3 oauth_google.py refresh            # tes refresh token
python3 daftar_akun_oauth.py               # daftarkan akun OAuth ke data/accounts.json
```

Refresh token disimpan di `data/google_oauth.json` (chmod 600, **gitignored** —
jangan pernah di-commit atau di-echo). Helper Antigravity (opsional):
`oauth_antigravity.py`.

---

## 🌐 Endpoint API

| Protocol | Method | Endpoint | Deskripsi | Auth |
|---|---|---|---|---|
| **OpenAI** | `POST` | `/v1/chat/completions` | Chat Completions (stream & non-stream, tool calling) | `Bearer <KEY>` |
| **OpenAI** | `GET` | `/v1/models` | Daftar model dari katalog terverifikasi | `Bearer <KEY>` |
| **OpenAI** | `GET` | `/v1/models/{model}` | Detail kapabilitas model | `Bearer <KEY>` |
| **OpenAI** | `POST` | `/v1/images/generations` | Image gen (Imagen; butuh `IMAGEN_API_KEY`) | `Bearer <KEY>` |
| **OpenAI** | `POST` | `/v1/videos/generations` | Kompatibilitas klien video (tidak ada model video di tier ini) | `Bearer <KEY>` |
| **Anthropic** | `POST` | `/v1/messages` | Format Anthropic (`tool_use`) | `x-api-key` |
| **Gemini** | `POST` | `/v1beta/models/{model}:generateContent` | Format Google Gemini | `x-goog-api-key` |
| **Sistem** | `GET` | `/healthz`, `/readyz` | Healthcheck | Bebas |

Contoh:

```bash
curl http://127.0.0.1:7860/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-lokal" \
  -d '{"model":"gemini-3-flash-preview","messages":[{"role":"user","content":"Halo"}],"stream":true}'
```

---

## 📋 Model yang Tersedia

Katalog = `backend/services/model_catalog.go:VerifiedModelCatalog`. Status per
probe live terhadap cloudcode-pa (OAuth Code Assist tier):

| Model ID | Status | Family | Konteks | Kapabilitas terverifikasi |
|---|---|---|---|---|
| `gemini-3-flash-preview` | **live** | Gemini 3 | 1.048.576 | chat, tool use, vision, search, thinking ✅ |
| `gemini-3.1-flash-lite` | **live** | Gemini 3 | 1.048.576 | chat, vision |
| `gemini-2.5-flash-lite` | **live** | Gemini 2.5 | 1.048.576 | chat, vision |
| `gemini-2.5-flash` | **kuota 429** | Gemini 2.5 | 1.048.576 | thinking terverifikasi (pernah 289k char / 129s) |
| `gemini-3.1-flash-lite-preview` | **kuota 429** | Gemini 3 | 1.048.576 | — |
| `gemini-2.5-pro` | **kuota 429** | Gemini 2.5 | 2.097.152 | thinking/vision/search (belum pernah tembus) |
| `gemini-3-pro-preview` | **kuota 429** | Gemini 3 | 1.048.576 | thinking/vision/search (belum pernah tembus) |
| `gemini-3.1-pro-preview` | **kuota 429** | Gemini 3 | 1.048.576 | thinking/vision/search (belum pernah tembus) |

Catatan:
- **`live`** = probe HTTP 200 + teks dihasilkan. **`kuota 429`** = model **ada**
  upstream tetapi kuota kredensial ini sedang habis; kuota reset, jadi model
  tetap diiklankan tetapi tidak pernah jadi default.
- Varian `-thinking` disediakan hanya untuk model yang terbukti mengembalikan
  thought text: `gemini-3-flash-preview` dan `gemini-2.5-flash`.
- Nama **404 (tidak diiklankan, hanya menerima sebagai alias)**:
  `gemini-3-flash`, `gemini-3.5-flash`, `gemini-3.1-pro-preview-customtools`,
  `gemini-3-pro-image-preview`, `gemini-2.5-flash-image`, serta nama fiktif
  lama `gemini-3.6-flash`, `gemini-3.5-flash-thinking`,
  `gemini-3.5-flash-thinking-lite`, `gemini-3.1-pro`. Semuanya dipetakan ke
  model asli bila dikirim klien.
- Alias publik gemini-cli: `auto`, `pro`, `flash`, `flash-lite`. Alias
  kompatibilitas OpenAI (`gpt-4o`, …) dan Anthropic (`claude-*`) juga didukung
  dan diarahkan ke model asli.

---

## 🖼️ Catatan Image Generation (WAJIB dibaca)

Image generation **tidak** bisa lewat kredensial OAuth Code Assist:

- `generativelanguage.googleapis.com` menolak OAuth dengan **HTTP 403
  `ACCESS_TOKEN_SCOPE_INSUFFICIENT`**.
- Model `gemini-*-image*` di cloudcode-pa mengembalikan **HTTP 404** (tidak ada).
- Tidak ada model Gemini di tier ini yang menghasilkan gambar/video.

Satu-satunya jalur adalah **Imagen dengan API key** (Google AI Studio):

```
POST https://generativelanguage.googleapis.com/v1beta/models/imagen-4.0-generate-001:predict
Header: x-goog-api-key: <IMAGEN_API_KEY>
```

**Tanpa `IMAGEN_API_KEY`, endpoint `/v1/images/generations` sengaja GAGAL
LOUD** (HTTP 500/503) dengan pesan actionable yang menyebut env var-nya:

```
no image backend configured: backend "imagen": Imagen API key is required for
image generation: set the IMAGEN_API_KEY environment variable to a Google AI
Studio (generativelanguage) API key, or supply one via
PUT /api/admin/settings {"imagen_api_key": "..."}.
```

Itu **perilaku yang benar** — gateway memilih gagal jelas daripada memberi
output palsu. **Jangan** mengubahnya menjadi gambar dummy. `IMAGEN_MODEL`
mengoverride model (default `imagen-4.0-generate-001`).

---

## 🧪 Verifikasi

Skrip bukti hidup ada di `tests/` (memanggil gateway yang sudah jalan) dan
skrip probe upstream di `tools/`. Lihat `tests/README.md` dan `tools/README.md`.
Ringkasan hasil probe live terakhir ada di `TEMUAN.md`.

---

## 📄 Lisensi

**GNU General Public License v3.0 (GPL-3.0).**
