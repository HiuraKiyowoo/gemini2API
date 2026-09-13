#!/usr/bin/env python3
"""probe_models.py — daftar model asli yang tersedia di cloudcode-pa + gemini2API."""
import json
import os, urllib.parse, urllib.request, urllib.error

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


def post(method, body, tok):
    r = urllib.request.Request(CC + ":" + method, data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
    try:
        resp = urllib.request.urlopen(r, timeout=60)
        return resp.status, json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()[:400]


tok = get_at()

print("=== 1. loadCodeAssist (tier + model yang diizinkan) ===")
st, d = post("loadCodeAssist", {"cloudaicompanionProject": ""}, tok)
print("HTTP", st)
print(json.dumps(d, indent=1)[:2500] if isinstance(d, dict) else d)

for m in ["listModels", "fetchAvailableModels", "getModels"]:
    print("\n=== POST %s ===" % m)
    st, d = post(m, {}, tok)
    print("HTTP", st, "|", (json.dumps(d) if isinstance(d, dict) else str(d))[:500])

print("\n=== GET v1internal:listModels ===")
r = urllib.request.Request(CC + ":listModels" if False else CC.replace("v1internal", "v1internal") + ":listModels",
    headers={"Authorization": AUTHZ + tok})
try:
    resp = urllib.request.urlopen(r, timeout=40)
    print("HTTP", resp.status, resp.read().decode()[:800])
except urllib.error.HTTPError as e:
    print("HTTP", e.code, e.read().decode()[:300])
