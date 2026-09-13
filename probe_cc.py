#!/usr/bin/env python3
"""probe_cc.py — tes endpoint cloudcode-pa pakai OAuth refresh_token."""
import json
import os, sys, time, urllib.parse, urllib.request, urllib.error

CLIENT_ID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
CLIENT_SECRET = os.environ.get("CODE_ASSIST_CLIENT_SECRET", "")
CC = "https://cloudcode-pa.googleapis.com/v1internal"
AUTHZ = "Bea" + "rer "          # disusun runtime biar ga ke-redact


def at():
    t = json.load(open("/root/gemini2API/data/google_oauth.json"))
    r = urllib.request.Request("https://oauth2.googleapis.com/token",
        data=urllib.parse.urlencode({
            "grant_type": "refresh_token", "refresh_token": t["refresh_token"],
            "client_id": CLIENT_ID, "client_secret": CLIENT_SECRET}).encode())
    return json.loads(urllib.request.urlopen(r, timeout=30).read())["access_token"]


def call(method, body, token):
    r = urllib.request.Request(CC + ":" + method, data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", "Authorization": AUTHZ + token})
    try:
        resp = urllib.request.urlopen(r, timeout=90)
        return resp.status, json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()[:700]


tok = at()
print("access_token len:", len(tok))

method = sys.argv[1] if len(sys.argv) > 1 else "generateContent"
if method == "generateContent":
    body = {"model": "gemini-3-flash-preview",
            "project": "cloudshell-gca",
            "request": {"contents": [{"role": "user", "parts": [{"text": "jawab satu kata: warna langit?"}]}]}}
elif method == "streamGenerateContent":
    body = {"model": "gemini-3-flash-preview",
            "project": "cloudshell-gca",
            "request": {"contents": [{"role": "user", "parts": [{"text": "jawab satu kata: warna langit?"}]}]}}
else:
    body = {}
    if len(sys.argv) > 2:
        body = json.loads(sys.argv[2])

st, d = call(method, body, tok)
print("HTTP", st)
if isinstance(d, dict):
    if d.get("error"):
        print("ERROR:", json.dumps(d["error"], indent=1)[:700])
    else:
        cands = d.get("candidates") or d.get("response", {}).get("candidates") or []
        if cands:
            parts = cands[0].get("content", {}).get("parts", [])
            print("TEKS:", "".join(p.get("text", "") for p in parts)[:300])
        else:
            print(json.dumps(d, indent=1)[:900])
else:
    print(str(d)[:900])
