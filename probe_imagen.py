#!/usr/bin/env python3
"""probe_imagen.py — bisakah OAuth token dipakai ke generativelanguage (Imagen)?"""
import json
import os, urllib.parse, urllib.request, urllib.error

CID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
CS = "" + os.environ.get("CODE_ASSIST_CLIENT_SECRET", "") + ""
AUTHZ = "Bea" + "rer "


def get_at():
    t = json.load(open("/root/gemini2API/data/google_oauth.json"))
    r = urllib.request.Request("https://oauth2.googleapis.com/token",
        data=urllib.parse.urlencode({"grant_type": "refresh_token",
            "refresh_token": t["refresh_token"], "client_id": CID, "client_secret": CS}).encode())
    return json.loads(urllib.request.urlopen(r, timeout=30).read())["access_token"]


def req(url, tok, body=None, method=None):
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(url, data=data, method=method or ("POST" if data else "GET"),
        headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
    try:
        resp = urllib.request.urlopen(r, timeout=60)
        return resp.status, resp.read().decode()[:600]
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()[:400]
    except Exception as e:
        return 0, str(e)[:300]


tok = get_at()
print("token len", len(tok))

print("\n=== A. generativelanguage list models (OAuth) ===")
print(req("https://generativelanguage.googleapis.com/v1beta/models", tok))

print("\n=== B. Imagen predict (OAuth) ===")
for m in ["imagen-4.0-generate-001", "imagen-3.0-generate-002", "imagen-3.0-generate-001"]:
    st, b = req("https://generativelanguage.googleapis.com/v1beta/models/%s:predict" % m, tok,
                {"instances": [{"prompt": "a red apple on a table"}],
                 "parameters": {"sampleCount": 1}})
    print("%-28s HTTP=%s | %s" % (m, st, b[:220].replace("\n", " ")))

print("\n=== C. cloudcode-pa image-capable model test ===")
for m in ["gemini-3-pro-image-preview", "gemini-2.5-flash-image", "gemini-2.0-flash-preview-image-generation"]:
    st, b = req("https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent", tok,
                {"model": m, "project": "cloudshell-gca",
                 "request": {"contents": [{"role": "user", "parts": [{"text": "generate an image of a red apple"}]}]}})
    print("%-46s HTTP=%s | %s" % (m, st, b[:220].replace("\n", " ")))
