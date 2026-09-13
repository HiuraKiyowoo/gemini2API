#!/usr/bin/env python3
"""tes_tahun2.py — parser benar: stream = JSON array (multi-line), bukan JSONL."""
import json
import os, re, time, urllib.parse, urllib.request, urllib.error

CID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
CS = "" + os.environ.get("CODE_ASSIST_CLIENT_SECRET", "") + ""
CC = "https://cloudcode-pa.googleapis.com/v1internal"
AUTHZ = "Bea" + "rer "


def get_at():
    t = json.load(open("/root/gemini2API/data/google_oauth.json"))
    r = urllib.request.Request("https://oauth2.googleapis.com/token",
        data=urllib.parse.urlencode({"grant_type": "refresh_token",
            "refresh_token": t["refresh_token"], "client_id": CID, "client_secret": CS}).encode())
    return json.loads(urllib.request.urlopen(r, timeout=30).read())["access_token"]


def extract(raw):
    """Ambil semua text dari array stream ATAU objek tunggal."""
    out = []
    try:
        data = json.loads(raw)
    except Exception:
        return out
    chunks = data if isinstance(data, list) else [data]
    for ch in chunks:
        resp = ch.get("response") or ch
        for c in (resp.get("candidates") or []):
            for p in ((c.get("content") or {}).get("parts") or []):
                if p.get("text"):
                    out.append(p["text"])
                if p.get("thought"):
                    out.append("[thought]")
    return out


def ask(tok, model, prompt, stream=True):
    body = {"model": model, "project": "cloudshell-gca",
            "request": {"contents": [{"role": "user", "parts": [{"text": prompt}]}]}}
    method = "streamGenerateContent" if stream else "generateContent"
    req = urllib.request.Request(CC + ":" + method, data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
    t0 = time.time()
    resp = urllib.request.urlopen(req, timeout=120)
    raw = resp.read().decode()
    return resp.status, "".join(extract(raw)), time.time() - t0


def ask_retry(tok, model, prompt, stream=True, tries=6, wait=12):
    for i in range(tries):
        try:
            return ask(tok, model, prompt, stream) + ("try%d" % (i + 1),)
        except urllib.error.HTTPError as e:
            b = e.read().decode()
            if e.code == 429 and i < tries - 1:
                time.sleep(wait)
                continue
            return e.code, "ERR: " + b[:120].replace("\n", " "), 0.0, "try%d" % (i + 1)
    return 0, "gagal", 0.0, "-"


tok = get_at()
print("token len", len(tok), "| sekarang:", time.strftime("%Y-%m-%d"))

Q = "Berapa tahun saat ini? Jawab satu angka saja."
for model in ["gemini-3-flash-preview", "gemini-2.5-flash", "gemini-2.5-pro"]:
    st, txt, el, tag = ask_retry(tok, model, Q, stream=True)
    print("%-22s stream  HTTP=%s %5.2fs %s | %s" % (model, st, el, tag, txt.strip()[:120]))

print()
Q2 = "Hari ini tanggal berapa? Kalau tidak tahu pasti, bilang tidak tahu."
st, txt, el, tag = ask_retry(tok, "gemini-2.5-flash", Q2, stream=False)
print("non-stream %-14s HTTP=%s %5.2fs %s | %s" % ("2.5-flash", st, el, tag, txt.strip()[:200]))
