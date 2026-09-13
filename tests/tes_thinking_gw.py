#!/usr/bin/env python3
"""tes_thinking_gw.py — uji reasoning lewat gateway (Anthropic surface pakai thinking)."""
import json, urllib.request, urllib.error, time

BASE = "http://127.0.0.1:7860"


def call(model, prompt, extra=None):
    body = {"model": model, "stream": False,
            "messages": [{"role": "user", "content": prompt}]}
    if extra:
        body.update(extra)
    r = urllib.request.Request(BASE + "/v1/chat/completions", data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", "Authorization": "Bearer sk-lokal"})
    t0 = time.time()
    try:
        resp = urllib.request.urlopen(r, timeout=300)
        d = json.loads(resp.read().decode())
        msg = d["choices"][0]["message"]
        return time.time() - t0, msg.get("content", ""), msg.get("reasoning_content") or msg.get("reasoning") or ""
    except urllib.error.HTTPError as e:
        return time.time() - t0, "ERR " + e.read().decode()[:200], ""


Q = ("Sebuah toko menaikkan harga 20% lalu menurunkan harga baru itu 20%. "
     "Berapa persen perubahan akhir? Jelaskan penalaranmu.")

for m in ["gemini-3.5-flash-thinking", "gemini-2.5-flash", "gemini-3-flash-preview"]:
    el, ans, why = call(m, Q)
    print("%-28s %5.1fs | jawaban %4d char | reasoning %4d char" % (m, el, len(ans), len(why)))
    print("   jawaban: %s" % ans.strip()[:140].replace("\n", " "))
    if why:
        print("   reasoning: %s" % why.strip()[:140].replace("\n", " "))
    print()
