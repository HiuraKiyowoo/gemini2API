# tools/ — diagnostic & probe scripts

Skrip utilitas untuk memverifikasi jalur upstream `cloudcode-pa` (OAuth Code Assist
tier) dan memeriksa ulang katalog model. **Bukan bagian dari runtime gateway** —
gateway tidak pernah mengimpor skrip di sini.

Semua skrip memakai kredensial yang sama dengan backend:
- `data/google_oauth.json` (refresh_token, gitignored, jangan di-commit)
- `CODE_ASSIST_CLIENT_SECRET` dari environment

Cara pakai (dari root repo):

```bash
export CODE_ASSIST_CLIENT_SECRET="<client-secret>"
python3 tools/probe_catalog.go.py     # verifikasi ulang lineup model -> /tmp/catalog_probe.json
python3 tools/probe_thinking.py       # uji thinking=on mengembalikan thought text
python3 tools/probe_vision.py         # uji image-INPUT (vision) via inlineData
python3 tools/probe_imagen.py         # buktikan OAuth TIDAK bisa ke Imagen (403 scope)
python3 tools/probe_all_models.py     # sapu seluruh id model + cari jalur image gen
python3 tools/probe_extra_models.py   # id tambahan (claude/antigravity) + waktu reset kuota
python3 tools/probe_cc.py             # smoke test endpoint cloudcode-pa dasar
python3 tools/daftar_akun_oauth.py    # daftarkan akun OAuth ke data/accounts.json (pool)
```

Hasil probe terakhir dirangkum di `../TEMUAN.md`. Model yang diiklankan hanya yang
ada di `backend/services/model_catalog.go:VerifiedModelCatalog` — skrip di sini
dipakai untuk memverifikasi ulang katalog itu, bukan menambah model baru.
