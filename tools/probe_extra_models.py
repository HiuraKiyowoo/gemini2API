#!/usr/bin/env python3
"""probe_extra_models.py — cek model tambahan (claude/antigravity) + quota reset time."""
import json
import os, time, urllib.parse, urllib.request, urllib.error

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


tok = get_at()
MODELS = ["claude-sonnet-4-5", "claude-3-5-sonnet", "claude-opus-4-1",
          "antigravity", "gemini-antigravity", "auto", "pro", "flash", "flash-lite"]
for m in MODELS:
    body = json.dumps({"model": m, "project": "cloudshell-gca",
        "request": {"contents": [{"role": "user", "parts": [{"text": "jawab OK saja"}]}]}}).encode()
    r = urllib.request.Request(CC + ":generateContent", data=body,
        headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
    try:
        resp = urllib.request.urlopen(r, timeout=60)
        d = json.loads(resp.read().decode())
        txt = "".join(p.get("text", "") for p in
                      (d.get("candidates", [{}])[0].get("content", {}).get("parts", []) or []))
        print("%-24s HTTP=200 | %s" % (m, txt[:60]))
    except urllib.error.HTTPError as e:
        b = e.read().decode().replace("\n", " ")
        try:
            msg = json.loads(b)["error"]["message"]
        except Exception:
            msg = b[:90]
        print("%-24s HTTP=%s | %s" % (m, e.code, msg[:110]))
    time.sleep(1)

print("\n=== retrieveUserQuota ===")
r = urllib.request.Request(CC + ":retrieveUserQuota",
    data=json.dumps({"project": "cloudshell-gca"}).encode(),
    headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
try:
    print(json.dumps(json.loads(urllib.request.urlopen(r, timeout=45).read().decode()), indent=1)[:1800])
except urllib.error.HTTPError as e:
    print("HTTP", e.code, e.read().decode()[:400])
