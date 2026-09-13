#!/usr/bin/env python3
"""probe_all_models.py — tes semua model asli gemini-cli + cari jalur image gen."""
import json
import os, time, urllib.parse, urllib.request, urllib.error

CID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
CS = "" + os.environ.get("CODE_ASSIST_CLIENT_SECRET", "") + ""
CC = "https://cloudcode-pa.googleapis.com/v1internal"
AUTHZ = "Bea" + "rer "

MODELS = [
    "gemini-3-pro-preview",
    "gemini-3.1-pro-preview",
    "gemini-3.1-pro-preview-customtools",
    "gemini-3-flash-preview",
    "gemini-3-flash",
    "gemini-3.5-flash",
    "gemini-2.5-pro",
    "gemini-2.5-flash",
    "gemini-3.1-flash-lite",
    "gemini-2.5-flash-lite",
    "gemini-3.1-flash-lite-preview",
]


def get_at():
    t = json.load(open("/root/gemini2API/data/google_oauth.json"))
    r = urllib.request.Request("https://oauth2.googleapis.com/token",
        data=urllib.parse.urlencode({"grant_type": "refresh_token",
            "refresh_token": t["refresh_token"], "client_id": CID, "client_secret": CS}).encode())
    return json.loads(urllib.request.urlopen(r, timeout=30).read())["access_token"]


def extract(raw):
    out = []
    data = json.loads(raw)
    for ch in (data if isinstance(data, list) else [data]):
        resp = ch.get("response") or ch
        for c in (resp.get("candidates") or []):
            for p in ((c.get("content") or {}).get("parts") or []):
                if p.get("text"):
                    out.append(p["text"])
    return "".join(out)


def probe(tok, model, prompt="Balas satu kata: OK", tries=4, wait=13, project="cloudshell-gca"):
    body = {"model": model, "project": project,
            "request": {"contents": [{"role": "user", "parts": [{"text": prompt}]}]}}
    for i in range(tries):
        r = urllib.request.Request(CC + ":streamGenerateContent", data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
        try:
            resp = urllib.request.urlopen(r, timeout=90)
            return resp.status, extract(resp.read().decode())[:80], None
        except urllib.error.HTTPError as e:
            err = e.read().decode()
            if e.code == 429 and i < tries - 1:
                time.sleep(wait); continue
            try:
                j = json.loads(err); msg = j.get("error", {}).get("message", err[:120])
            except Exception:
                msg = err[:120]
            return e.code, None, msg.replace("\n", " ")
        except Exception as e:
            return 0, None, str(e)[:120]
    return 0, None, "gagal"


tok = get_at()
print("token ok, len", len(tok), "\n--- MODEL ---")
ok_models = []
for m in MODELS:
    st, txt, err = probe(tok, m)
    flag = "OK " if st == 200 and txt else "   "
    print("%s %-38s %s | %s" % (flag, m, st, (txt or err or "")[:90]))
    if st == 200 and txt:
        ok_models.append(m)

print("\nHIDUP:", ", ".join(ok_models) or "(kosong)")
json.dump(ok_models, open("/tmp/ok_models.json", "w"))
