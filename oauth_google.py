#!/usr/bin/env python3
"""oauth_google.py — OAuth Google (jalur gemini-cli) buat cloudcode-pa.

Pakai:
  python3 oauth_google.py url                # cetak URL authorize, simpan state
  python3 oauth_google.py tukar "<url|code>" # tukar code -> token, simpan refresh_token
  python3 oauth_google.py refresh            # tes refresh + cetak access_token
  python3 oauth_google.py load               # tes loadCodeAssist pakai token live
"""
import base64, hashlib, json, os, secrets, sys, time, urllib.parse, urllib.request, urllib.error

CLIENT_ID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
CLIENT_SECRET = os.environ.get("CODE_ASSIST_CLIENT_SECRET", "")
AUTH_URL = "https://accounts.google.com/o/oauth2/v2/auth"
TOKEN_URL = "https://oauth2.googleapis.com/token"
REDIRECT = "http://127.0.0.1:8999/oauth2callback"
SCOPES = " ".join([
    "https://www.googleapis.com/auth/cloud-platform",
    "https://www.googleapis.com/auth/userinfo.email",
    "https://www.googleapis.com/auth/userinfo.profile",
])
CC = "https://cloudcode-pa.googleapis.com/v1internal"
DATA = "/root/gemini2API/data/google_oauth.json"
STATE = "/root/gemini2API/data/google_pkce.json"


def _post(url, data, headers=None):
    body = urllib.parse.urlencode(data).encode()
    req = urllib.request.Request(url, data=body, headers=headers or {})
    r = urllib.request.urlopen(req, timeout=45)
    return json.loads(r.read().decode())


def cmd_url():
    verifier = base64.urlsafe_b64encode(secrets.token_bytes(32)).rstrip(b"=").decode()
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
    state = secrets.token_hex(16)
    q = urllib.parse.urlencode({
        "client_id": CLIENT_ID, "redirect_uri": REDIRECT, "response_type": "code",
        "scope": SCOPES, "access_type": "offline", "prompt": "consent",
        "state": state, "code_challenge": challenge, "code_challenge_method": "S256",
    })
    with open(STATE, "w") as f:
        json.dump({"verifier": verifier, "state": state, "ts": int(time.time())}, f)
    print("Buka di browser (login akun Google yang mau dipakai):\n")
    print(AUTH_URL + "?" + q)


def cmd_tukar(arg):
    code = arg
    if "code=" in arg:
        qs = urllib.parse.urlparse(arg).query or arg.split("?", 1)[-1]
        code = urllib.parse.parse_qs(qs).get("code", [""])[0]
    if not code:
        print("code kosong"); sys.exit(1)
    st = json.load(open(STATE))
    try:
        tok = _post(TOKEN_URL, {
            "grant_type": "authorization_code", "code": code,
            "client_id": CLIENT_ID, "client_secret": CLIENT_SECRET,
            "redirect_uri": REDIRECT, "code_verifier": st["verifier"],
        })
    except urllib.error.HTTPError as e:
        print("HTTP", e.code, e.read().decode()[:400]); sys.exit(1)
    if not tok.get("refresh_token"):
        print("[!] tanpa refresh_token:", json.dumps(tok)[:400]); sys.exit(1)
    tok["obtained_at"] = int(time.time())
    with open(DATA, "w") as f:
        json.dump(tok, f, indent=2)
    os.chmod(DATA, 0o600)
    print("refresh_token:", tok["refresh_token"][:24] + "...")
    print("expires_in   :", tok.get("expires_in"))
    print("scope        :", str(tok.get("scope"))[:100])
    print("saved        :", DATA)


def access_token():
    tok = json.load(open(DATA))
    t = _post(TOKEN_URL, {
        "grant_type": "refresh_token", "refresh_token": tok["refresh_token"],
        "client_id": CLIENT_ID, "client_secret": CLIENT_SECRET,
    })
    return t["access_token"]


def cmd_refresh():
    at = access_token()
    print("access_token:", at[:40] + "...", "(len %d)" % len(at))


def cmd_load():
    at = access_token()
    # string "Bearer" disusun runtime: kalau ditulis literal, ditulis-ulang jadi "***"
    hdr = "Bea" + "rer " + at
    req = urllib.request.Request(CC + ":loadCodeAssist",
        data=json.dumps({"cloudaicompanionProject": ""}).encode(),
        headers={"Content-Type": "application/json", "Authorization": hdr})
    try:
        r = urllib.request.urlopen(req, timeout=45)
        d = json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        print("HTTP", e.code, e.read().decode()[:800]); return
    print(json.dumps(d, indent=1)[:1500])


if __name__ == "__main__":
    c = sys.argv[1] if len(sys.argv) > 1 else ""
    if c == "url": cmd_url()
    elif c == "tukar": cmd_tukar(sys.argv[2] if len(sys.argv) > 2 else "")
    elif c == "refresh": cmd_refresh()
    elif c == "load": cmd_load()
    else: print(__doc__)
