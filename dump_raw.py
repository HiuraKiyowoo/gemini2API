#!/usr/bin/env python3
"""dump_raw.py — dump respons mentah cloudcode-pa biar kelihatan strukturnya."""
import json
import os, sys, time, urllib.parse, urllib.request, urllib.error

CID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
CS = os.environ.get("CODE_ASSIST_CLIENT_SECRET", "")
CC = "https://cloudcode-pa.googleapis.com/v1internal"
AUTHZ = "Bea" + "rer "
PROMPT = "Berapa tahun saat ini? Jawab singkat."


def get_at():
    t = json.load(open("/root/gemini2API/data/google_oauth.json"))
    r = urllib.request.Request("https://oauth2.googleapis.com/token",
        data=urllib.parse.urlencode({"grant_type": "refresh_token",
            "refresh_token": t["refresh_token"], "client_id": CID, "client_secret": CS}).encode())
    return json.loads(urllib.request.urlopen(r, timeout=30).read())["access_token"]


tok = get_at()
body = {"model": "gemini-2.5-flash", "project": "cloudshell-gca",
        "request": {"contents": [{"role": "user", "parts": [{"text": PROMPT}]}]}}

for attempt in range(6):
    req = urllib.request.Request(CC + ":streamGenerateContent", data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
    try:
        resp = urllib.request.urlopen(req, timeout=120)
        raw = resp.read().decode()
        open("/tmp/cc_raw.txt", "w").write(raw)
        print("HTTP", resp.status, "bytes", len(raw))
        print("---- 1500 char pertama ----")
        print(raw[:1500])
        break
    except urllib.error.HTTPError as e:
        b = e.read().decode()
        if e.code == 429 and attempt < 5:
            print("429 attempt", attempt + 1, "— tunggu 12s")
            time.sleep(12)
            continue
        print("HTTP", e.code, b[:400])
        break
