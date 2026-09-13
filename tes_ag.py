#!/usr/bin/env python3
"""tes_ag.py — bukti hidup: Claude + Gemini baru + image via Antigravity."""
import base64, json, struct, sys, time, zlib
sys.path.insert(0, "/root/gemini2API")
from oauth_antigravity import get_at, api

tok = get_at()
print("access_token ok (len %d)\n" % len(tok))

SOAL = "Balas PERSIS format ini: NAMA=<nama model yang kamu tahu> | KATA=merah"
for m in ["claude-sonnet-4-6", "claude-opus-4-6-thinking", "gemini-3.8-flash-high",
          "gemini-3.1-pro-high", "gpt-oss-120b-medium"]:
    body = {"model": m, "request": {"contents": [{"role": "user", "parts": [{"text": SOAL}]}]}}
    try:
        t0 = time.time()
        st, d = api("generateContent", body, tok)
        el = time.time() - t0
        if isinstance(d, dict):
            txt = "".join(p.get("text", "") for c in (d.get("candidates") or [])
                          for p in ((c.get("content") or {}).get("parts") or []))
            print("%-28s HTTP=%s %5.1fs | %s" % (m, st, el, txt.strip()[:90]))
        else:
            print("%-28s HTTP=%s | %s" % (m, st, str(d)[:90]))
    except Exception as e:
        print("%-28s EXC %s" % (m, str(e)[:120]))
    time.sleep(2)

print("\n=== IMAGE: gemini-3.1-flash-image ===")
body = {"model": "gemini-3.1-flash-image",
        "request": {"contents": [{"role": "user", "parts": [{"text": "foto apel merah satu, latar putih polos"}]}]}}
try:
    st, d = api("generateContent", body, tok)
    if isinstance(d, dict):
        n_img = 0
        for c in (d.get("candidates") or []):
            for p in ((c.get("content") or {}).get("parts") or []):
                if p.get("inlineData", {}).get("data"):
                    raw = base64.b64decode(p["inlineData"]["data"])
                    fn = "/tmp/ag_img_%d.png" % n_img
                    open(fn, "wb").write(raw)
                    print("GAMBAR %d tersimpan: %s (%d bytes, %s)" % (n_img, fn, len(raw), p["inlineData"].get("mimeType")))
                    n_img += 1
                elif p.get("text"):
                    print("teks:", p["text"][:80])
        if n_img == 0:
            print(json.dumps(d)[:500])
    else:
        print("HTTP", st, "|", str(d)[:300])
except Exception as e:
    print("EXC", str(e)[:300])
