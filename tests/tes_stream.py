#!/usr/bin/env python3
"""tes_stream.py — dump mentah respons streaming dari gateway."""
import json, sys, urllib.request

r = urllib.request.Request("http://127.0.0.1:7860/v1/chat/completions",
    data=json.dumps({"model": "gemini-2.5-flash", "stream": True,
                     "messages": [{"role": "user", "content": "Hitung 1 sampai 3, satu angka per baris."}]}).encode(),
    headers={"Content-Type": "application/json", "Authorization": "Bearer sk-lokal"})
resp = urllib.request.urlopen(r, timeout=120)
print("HTTP", resp.status, "| content-type:", resp.headers.get("Content-Type"))
raw = resp.read().decode()
print("bytes:", len(raw))
print("---- 1200 char pertama ----")
print(raw[:1200])
