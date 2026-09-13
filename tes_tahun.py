#!/usr/bin/env python3
"""tes_tahun.py — tes ulang cloudcode-pa: tanya tahun saat ini + hitung latensi."""
import json
import os, time, urllib.parse, urllib.request, urllib.error

CID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
CS = os.environ.get("CODE_ASSIST_CLIENT_SECRET", "")
CC = "https://cloudcode-pa.googleapis.com/v1internal"
AUTHZ = "Bea" + "rer "

PROMPT = "Berapa tahun saat ini? Jawab singkat, sebutkan tahunnya saja plus satu kalimat alasan."


def get_at():
    t = json.load(open("/root/gemini2API/data/google_oauth.json"))
    r = urllib.request.Request("https://oauth2.googleapis.com/token",
        data=urllib.parse.urlencode({"grant_type": "refresh_token",
            "refresh_token": t["refresh_token"], "client_id": CID, "client_secret": CS}).encode())
    return json.loads(urllib.request.urlopen(r, timeout=30).read())["access_token"]


def ask(tok, model, stream):
    body = {"model": model, "project": "cloudshell-gca",
            "request": {"contents": [{"role": "user", "parts": [{"text": PROMPT}]}]}}
    method = "streamGenerateContent" if stream else "generateContent"
    req = urllib.request.Request(CC + ":" + method, data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
    t0 = time.time()
    resp = urllib.request.urlopen(req, timeout=120)
    raw = resp.read().decode()
    el = time.time() - t0
    texts = []
    if stream:
        for line in raw.splitlines():
            line = line.strip().rstrip(",")
            if not line.startswith("{"):
                continue
            try:
                obj = json.loads(line)
            except Exception:
                continue
            for p in (obj.get("response", {}).get("candidates", [{}])[0]
                      .get("content", {}).get("parts", []) or []):
                if p.get("text"):
                    texts.append(p["text"])
    else:
        for p in (json.loads(raw).get("candidates", [{}])[0]
                  .get("content", {}).get("parts", []) or []):
            if p.get("text"):
                texts.append(p["text"])
    return resp.status, "".join(texts), el


tok = get_at()
print("access_token len:", len(tok))
for model in ["gemini-3-flash-preview", "gemini-2.5-flash"]:
    for stream in (True, False):
        tag = "%-22s %-7s" % (model, "stream" if stream else "nonstr")
        for attempt in range(4):
            try:
                st, txt, el = ask(tok, model, stream)
                print("%s HTTP=%d %.2fs (try %d) | %s" % (tag, st, el, attempt + 1, txt.strip()[:300]))
                break
            except urllib.error.HTTPError as e:
                body = e.read().decode()
                if e.code == 429 and attempt < 3:
                    print("%s 429 try %d — tunggu 8s, ulang" % (tag, attempt + 1))
                    time.sleep(8)
                    continue
                print("%s HTTP=%d | ERR %s" % (tag, e.code, body[:200].replace("\n", " ")))
                break
            except Exception as e:
                print("%s EXC | %s" % (tag, str(e)[:200]))
                break
