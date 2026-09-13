#!/usr/bin/env python3
"""tes_image_gw.py — buktikan image gen gagal LOUD (bukan output palsu) tanpa IMAGEN_API_KEY."""
import json, urllib.request, urllib.error

BASE = "http://127.0.0.1:7860"
body = {"model": "dall-e-3", "prompt": "kucing oranye di kucing oranye", "n": 1, "size": "1024x1024"}

for path, hdrs in [("/v1/images/generations", {"Content-Type": "application/json", "Authorization": "Bearer sk-lokal"})]:
    r = urllib.request.Request(BASE + path, data=json.dumps(body).encode(), headers=hdrs)
    try:
        resp = urllib.request.urlopen(r, timeout=90)
        print("HTTP", resp.status)
        print(resp.read().decode()[:600])
    except urllib.error.HTTPError as e:
        payload = e.read().decode()
        print("HTTP", e.code, "(expected: gagal loud)")
        print(payload[:600])
