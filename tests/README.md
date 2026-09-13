# tests/ — end-to-end verification scripts

Skrip verifikasi manual yang memanggil gateway yang sudah jalan di
`http://127.0.0.1:7860` (auth `Authorization: Bearer <ADMIN_KEY / API key>`).
Tidak ada test runner otomatis di sini; ini bukti hidup untuk perilaku yang
sudah diklaim README.

Prasyarat: backend sudah jalan (`./start.sh -d`).

```bash
python3 tests/tes_gateway.py      # /v1/models memuat model asli + chat non-stream + injeksi tanggal (2026)
python3 tests/tes_stream.py       # dump mentah SSE streaming dari /v1/chat/completions
python3 tests/tes_tanggal.py      # buktikan injeksi tanggal ke systemInstruction -> jawab 2026
python3 tests/tes_tahun2.py       # parser benar: stream = JSON array multi-line, bukan JSONL
python3 tests/tes_image_gw.py     # buktikan image gen GAGAL LOUD tanpa IMAGEN_API_KEY (bukan output palsu)
python3 tests/tes_thinking_gw.py  # uji mode penalaran lewat surface Anthropic/OpenAI
python3 tests/tes_ag.py           # probe jalur Antigravity OAuth (claude/gemini baru)
```

Catatan: `tes_image_gw.py` **harus** gagal loud (HTTP 4xx/5xx dengan pesan
"IMAGEN_API_KEY" yang actionable) selama `IMAGEN_API_KEY` belum dipasang. Itu
perilaku yang benar — jangan "diperbaiki" jadi output palsu.
