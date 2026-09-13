#!/usr/bin/env python3
"""tes_gateway.py — tes end-to-end lewat gateway sendiri (bukan probe langsung).

Bukti yang harus didapat:
  1. /v1/models memuat model asli
  2. /v1/chat/completions non-stream mengembalikan teks nyata
  3. pertanyaan tahun -> 2026 (bukti injeksi tanggal lewat gateway)
  4. streaming mengeluarkan delta bertahap
"""
import json, time, urllib.request, urllib.error

BASE = "http://127.0.0.1:7860"
KEY = "sk-tes-lokal"


def req(path, body, stream=False, timeout=180):
    r = urllib.request.Request(BASE + path, data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", "Authorization": "Bearer " + KEY})
    try:
        resp = urllib.request.urlopen(r, timeout=timeout)
        return resp.status, resp.read().decode(), resp
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()[:400], None


print("=== 1. /v1/models ===")
try:
    r = urllib.request.Request(BASE + "/v1/models", headers={"Authorization": "Bearer " + KEY})
    d = json.loads(urllib.request.urlopen(r, timeout=30).read().decode())
    ids = [m["id"] for m in d.get("data", [])]
    print("total %d model:" % len(ids))
    print(", ".join(ids[:24]))
except Exception as e:
    print("EXC", str(e)[:200])

print("\n=== 2. chat non-stream ===")
st, body, _ = req("/v1/chat/completions", {
    "model": "gemini-3-flash-preview", "stream": False,
    "messages": [{"role": "user", "content": "Balas satu kata: OK"}]})
print("HTTP", st)
print(body[:600])

print("\n=== 3. pertanyaan tahun (uji injeksi tanggal) ===")
st, body, _ = req("/v1/chat/completions", {
    "model": "gemini-2.5-flash", "stream": False,
    "messages": [{"role": "user", "content": "Berapa tahun saat ini? Jawab satu angka saja."}]})
print("HTTP", st)
try:
    txt = json.loads(body)["choices"][0]["message"]["content"]
    print("JAWABAN:", txt.strip()[:200])
    print("BENAR (2026)?" , "2026" in txt)
except Exception as e:
    print(body[:400])

print("\n=== 4. streaming (hitung delta) ===")
st, body, _ = req("/v1/chat/completions", {
    "model": "gemini-2.5-flash", "stream": True,
    "messages": [{"role": "user", "content": "Hitung 1 sampai 5, satu angka per baris."}]})
print("HTTP", st)
chunks = 0
text = ""
for line in body.splitlines():
    if line.startswith("data: ") and "[DONE]" not in line:
        try:
            j = json.loads(line[6:])
            delta = j.get("choices", [{}])[0].get("delta", {}).get("content") or ""
            if delta:
                chunks += 1
                text += delta
        except Exception:
            pass
print("jumlah delta:", chunks)
print("teks:", text.strip()[:200].replace("\n", " | "))
