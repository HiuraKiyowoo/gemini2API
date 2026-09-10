# 🚀 gemini2API - Self-Hosted Google Gemini API Gateway

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8.svg?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.24+" />
  <img src="https://img.shields.io/badge/React-19-61DAFB.svg?style=for-the-badge&logo=react&logoColor=black" alt="React 19" />
  <img src="https://img.shields.io/badge/API-OpenAI%20%7C%20Anthropic%20%7C%20Gemini-green.svg?style=for-the-badge" alt="Multi-Protocol API" />
  <img src="https://img.shields.io/badge/Engine-Google%20StreamGenerate-orange.svg?style=for-the-badge" alt="Google StreamGenerate" />
  <img src="https://img.shields.io/badge/Platform-Termux%20%7C%20Linux%20VPS-blue.svg?style=for-the-badge" alt="Termux & Linux" />
  <img src="https://img.shields.io/badge/License-GPL--3.0-blue.svg?style=for-the-badge" alt="License GPL-3.0" />
</p>

Gateway AI mandiri berkinerja tinggi (*High-Performance Self-Hosted Gateway*) yang mengonversi kemampuan antarmuka web resmi **Google Gemini** (`gemini.google.com`) menjadi API standar yang kompatibel penuh dengan **OpenAI API**, **Anthropic Messages**, dan **Google Gemini REST API**.

Dilengkapi reverse-engine protokol web **Google StreamGenerate RPC**, bypass signature **SAPISIDHASH**, ekstraksi CSRF otomatis (`SNlM0e`), dukungan **Tool Calling / Function Calling** untuk AI Coding Agent, serta WebUI modern berbahasa Indonesia yang ringan dan siap jalan di **Android Termux** maupun **Server VPS Linux**.

---

## 🌟 Fitur Utama

- ⚡ **Google StreamGenerate RPC Native**: Mengintegrasikan protokol resmi web Google Gemini (`BardFrontendService/StreamGenerate`) dengan signature `SAPISIDHASH` dan ekstraksi token CSRF (`SNlM0e`) dinamis.
- 🍪 **Manajemen Cookie Fleksibel**: Dukungan penuh input cookie via WebUI dashboard — bisa paste string cookie mentah maupun ekspor **JSON array langsung dari Cookie-Editor / EditThisCookie**.
- 🛠️ **AI Agent & Tool Calling Support (100% Works)**: Kompatibel penuh dengan spesifikasi Function Calling OpenAI (`tools` & `tool_calls`) dan Anthropic (`tool_use`). Teruji sukses untuk autonomous agent seperti **Cline**, **Roo Code**, **Claude Code**, **Cursor**, dan **LangChain**.
- 🎨 **Image Lab (Google Imagen 3)**: Mendukung pembuatan gambar AI via prompt teks maupun endpoint `/v1/images/generations` berbasis Google Imagen 3 bawaan Gemini.
- 🔄 **Account Pool & Rotasi Otomatis**: Manajemen multi-akun dengan auto-failover, pelacakan rate limit, cooldown handling, dan rotasi round-robin.
- 🎨 **WebUI Dashboard Modern**: Dashboard berbasis React 19 + Tailwind CSS + Lucide Icons yang bersih, responsif, dan 100% berbahasa Indonesia untuk mengelola akun, API key, konfigurasi runtime, dan uji coba interaktif.
- 📱 **Termux & VPS Native**: Backend ditulis murni dalam bahasa **Go** dengan penggunaan RAM sangat minim (< 30MB) tanpa ketergantungan browser headless berat (Playwright/Chromium) untuk operasional API harian.

---

## 🏗️ Arsitektur & Alur Kerja

```
[AI Client / Agent (Cline, Roo Code, Chatbox, SDK)]
                       │
                       ▼  (OpenAI / Anthropic / Gemini Format)
             [gemini2API Gateway :7860]
  ├── 1. Autentikasi API Key Klien & Rate Limiter
  ├── 2. Pool Manager: Pilih Akun Google Sehat (Round-Robin)
  ├── 3. Injeksi Skema Tool Calling & System Prompt
  └── 4. Format Protokol Web Google Gemini:
         ├── Cookie Header (__Secure-1PSID, SAPISID, SID, dll.)
         ├── Authorization: SAPISIDHASH {timestamp}_{sha1}
         ├── Ekstraksi CSRF Token (SNlM0e / at) & Server Build (bl)
         └── Payload 80-element Array (f.req)
                       │
                       ▼  (Direct HTTPS StreamGenerate)
           [Google Gemini Web Server (gemini.google.com)]
                       │
                       ▼  (RPC Chunk Stream / wrb.fr packets)
             [gemini2API Gateway :7860]
  ├── 1. Parser wrb.fr Stream & Ekstraksi Teks Delta
  ├── 2. Parser Tool Calling & Ekstraksi Argumen JSON
  └── 3. Streaming Server-Sent Events (SSE) Standar OpenAI ke Klien
                       │
                       ▼
[AI Client / Agent Menerima Respons / Eksekusi Tool Sempurna! 🎉]
```

---

## ⚡ Panduan Instalasi Cepat

### 1. Instalasi di HP Android (Termux)

```bash
# Update paket Termux dan instal dependensi
pkg update -y && pkg install git golang nodejs-lts -y

# Clone repositori
git clone https://github.com/HiuraKiyowoo/gemini2API.git
cd gemini2API

# Jalankan build backend dan frontend otomatis
cd frontend && npm install && npm run build && cd ..
cd backend && go build -trimpath -ldflags="-s -w" -o ../bin/gemini2api-backend . && cd ..

# Berikan izin eksekusi pada skrip manajemen
chmod +x start.sh stop.sh update.sh
```

### 2. Jalankan Layanan

```bash
# Menjalankan di background (Daemon)
./start.sh -d

# Memeriksa status log
tail -f logs/output.log

# Menghentikan layanan
./stop.sh
```

Akses Web Dashboard melalui browser di: **`http://localhost:7860`** (atau `http://IP_HP_ANDA:7860`).

### 3. Cara Update ke Versi Terbaru 🔄

```bash
./update.sh
```

---

## 🔑 Konfigurasi & Cara Input Akun Google Gemini

1. Buka WebUI di browser: `http://localhost:7860`.
2. Masukkan Admin Key default: `admin123456` (dapat diubah di `.env` atau menu Pengaturan).
3. Buka tab **Manajemen Akun**:
   - Buka browser Anda di [gemini.google.com](https://gemini.google.com) (pastikan sudah login).
   - Buka ekstensi **Cookie-Editor** (atau DevTools > Application > Cookies).
   - Salin cookie akun Anda:
     - **Opsi A (Paling Mudah)**: Klik tombol Export di Cookie-Editor (format JSON), lalu langsung paste ke kolom **Tempel Cookie Google / Token** di dashboard.
     - **Opsi B (Format String)**: Salin string cookie mentah yang mengandung `__Secure-1PSID` dan `SAPISID`.
   - Klik **Simpan / Login Akun**. Backend akan otomatis memvalidasi ke server Google dan mengaktifkan akun.
4. Buka tab **API Key** untuk membuat key akses baru yang akan digunakan di aplikasi AI Agent Anda.

---

## 🌐 Endpoint API Utama

Semua endpoint kompatibel penuh dengan format standar industri:

| Protocol | Method | Endpoint | Deskripsi | Auth |
|---|---|---|---|---|
| **OpenAI** | `POST` | `/v1/chat/completions` | Chat Completions (Stream & Non-Stream, Tool Calling) | `Bearer <KEY>` |
| **OpenAI** | `GET` | `/v1/models` | Daftar semua model yang tersedia | `Bearer <KEY>` |
| **OpenAI** | `GET` | `/v1/models/{model}` | Detail kapabilitas model spesifik | `Bearer <KEY>` |
| **OpenAI** | `POST` | `/v1/images/generations` | Pembuatan Gambar AI (Google Imagen 3) | `Bearer <KEY>` |
| **Anthropic** | `POST` | `/v1/messages` | Format Pesan Anthropic Claude (`tool_use`) | `x-api-key` |
| **Gemini** | `POST` | `/v1beta/models/{model}:generateContent` | Format Google Gemini API resmi | `x-goog-api-key` |
| **Sistem** | `GET` | `/healthz` & `/readyz` | Healthcheck gateway & pool akun | Bebas |

---

## 📋 Daftar Model yang Didukung

### 1. Model Dasar (Base Models)
| Model ID | Deskripsi & Rekomendasi Penggunaan | Kapabilitas Utama |
|---|---|---|
| `gemini-2.5-flash` | **Model Rekomendasi Utama** ⚡ — Sangat cepat, kuota melimpah, ideal untuk coding agent (Cline, Roo, Claude Code). | Chat, Tool Use, Vision, Web Search |
| `gemini-2.5-pro` | Model dengan kapasitas penalaran dan kedalaman logika tertinggi untuk instruksi kompleks. | Chat, Deep Reasoning, Tool Use |
| `gemini-2.5-flash-thinking` | Mode penalaran mendalam (*deep thinking process*) sebelum memberikan jawaban akhir. | Chat, Thinking Process, Tool Use |
| `gemini-2.0-flash` | Alias kompatibilitas versi 2.0 (diarahkan ke engine 2.5). | Chat, Tool Use |
| `gemini-1.5-pro` | Alias kompatibilitas versi 1.5 Pro. | Chat, Tool Use |

### 2. Suffix Fitur (Suffix Modes)
- `-thinking`: Mengaktifkan penalaran mendalam (*deep thinking*) (contoh: `gemini-2.5-pro-thinking`).
- `-search`: Memaksa Google Search live diaktifkan pada jawaban.

### 3. Alias Kompatibilitas Otomatis (Model Aliases)
- **OpenAI Aliases**: `gpt-4o`, `gpt-4-turbo`, `gpt-4`, `gpt-5`, `o1` ➔ diarahkan otomatis ke `gemini-2.5-pro`
- **OpenAI Mini Aliases**: `gpt-4o-mini`, `gpt-3.5-turbo`, `o1-mini` ➔ diarahkan otomatis ke `gemini-2.5-flash`
- **Anthropic Aliases**: `claude-3-5-sonnet`, `claude-3.5-sonnet`, `claude-sonnet-4-5` ➔ diarahkan otomatis ke `gemini-2.5-pro`; `claude-3-haiku` ➔ `gemini-2.5-flash`

---

## 📄 Lisensi
Proyek ini didistribusikan di bawah lisensi **GNU General Public License v3.0 (GPL-3.0)**.
