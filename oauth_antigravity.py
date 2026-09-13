#!/usr/bin/env python3
"""oauth_antigravity.py — OAuth Antigravity (client_id resmi antigravity CLI).

Pakai:
  python3 oauth_antigravity.py url                # cetak URL authorize
  python3 oauth_antigravity.py tukar "<url|code>" # tukar code -> token, simpan
  python3 oauth_antigravity.py models             # fetchAvailableModels (daftar model + Claude?)
  python3 oauth_antigravity.py tes "prompt"       # generateContent cepat
"""
import json, os, sys, time, urllib.parse, urllib.request, urllib.error

CID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
CS = os.environ.get("ANTIGRAVITY_CLIENT_SECRET", "")
REDIRECT = "http://localhost:51121/oauth-callback"
SCOPES = " ".join([
    "https://www.googleapis.com/auth/cloud-platform",
    "https://www.googleapis.com/auth/userinfo.email",
    "https://www.googleapis.com/auth/userinfo.profile",
    "https://www.googleapis.com/auth/cclog",
    "https://www.googleapis.com/auth/experimentsandconfigs",
])
AUTH = "https://accounts.google.com/o/oauth2/v2/auth"
TOKEN = "https://oauth2.googleapis.com/token"
EP = "https://daily-cloudcode-pa.googleapis.com/v1internal"
UA = ("antigravity/cli/1.1.13 (aidev_client; os_type=linux; arch=arm64; cl=964361259; auth_method=consumer)")
DATA = "/root/gemini2API/data/antigravity_oauth.json"


def _post(url, data, headers=None):
    body = urllib.parse.urlencode(data).encode()
    req = urllib.request.Request(url, data=body, headers=headers or {})
    return json.loads(urllib.request.urlopen(req, timeout=45).read().decode())


def get_at():
    t = json.load(open(DATA))
    tk = _post(TOKEN, {"grant_type": "refresh_token", "refresh_token": t["refresh_token"],
                       "client_id": CID, "client_secret": CS})
    return tk["access_token"]


def api(method, body, tok, stream=False):
    r = urllib.request.Request("%s:%s" % (EP, method), data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json",
                 "Authorization": ("Bea" + "rer ") + tok,
                 "User-Agent": UA})
    resp = urllib.request.urlopen(r, timeout=120)
    raw = resp.read().decode()
    try:
        return resp.status, json.loads(raw)
    except Exception:
        return resp.status, raw[:500]


def cmd_url():
    q = urllib.parse.urlencode({"client_id": CID, "redirect_uri": REDIRECT,
        "response_type": "code", "scope": SCOPES, "access_type": "offline",
        "prompt": "consent"})
    print("Buka di browser (login akun Google target):\n")
    print(AUTH + "?" + q)


def cmd_tukar(arg):
    code = arg
    if "code=" in arg:
        qs = urllib.parse.urlparse(arg).query or arg.split("?", 1)[-1]
        code = urllib.parse.parse_qs(qs).get("code", [""])[0]
    if not code:
        print("code kosong"); sys.exit(1)
    try:
        tok = _post(TOKEN, {"grant_type": "authorization_code", "code": code,
            "client_id": CID, "client_secret": CS, "redirect_uri": REDIRECT})
    except urllib.error.HTTPError as e:
        print("HTTP", e.code, e.read().decode()[:400]); sys.exit(1)
    if not tok.get("refresh_token"):
        print("[!] tanpa refresh_token:", json.dumps(tok)[:300]); sys.exit(1)
    tok["obtained_at"] = int(time.time())
    with open(DATA, "w") as f:
        json.dump(tok, f, indent=2)
    os.chmod(DATA, 0o600)
    print("OK refresh_token:", tok["refresh_token"][:20] + "...")
    print("scope:", str(tok.get("scope"))[:120])
    print("saved:", DATA)


def cmd_models():
    tok = get_at()
    st, d = api("fetchAvailableModels", {}, tok)
    print("HTTP", st)
    if isinstance(d, dict):
        models = d.get("models", {})
        print("default:", d.get("defaultAgentModelId"))
        print("TOTAL %d model:\n" % len(models))
        for mid, info in sorted(models.items()):
            print("  %-52s %s" % (mid, (info or {}).get("displayName", "")))
    else:
        print(str(d)[:400])


def cmd_tes(prompt):
    tok = get_at()
    body = {"model": "gemini-3-flash",
            "request": {"contents": [{"role": "user", "parts": [{"text": prompt}]}]}}
    st, d = api("generateContent", body, tok)
    print("HTTP", st)
    if isinstance(d, dict):
        for c in (d.get("candidates") or []):
            for p in ((c.get("content") or {}).get("parts") or []):
                if p.get("text"):
                    print("TEKS:", p["text"][:200])
    else:
        print(str(d)[:300])


if __name__ == "__main__":
    c = sys.argv[1] if len(sys.argv) > 1 else ""
    if c == "url": cmd_url()
    elif c == "tukar": cmd_tukar(sys.argv[2] if len(sys.argv) > 2 else "")
    elif c == "models": cmd_models()
    elif c == "tes": cmd_tes(sys.argv[2] if len(sys.argv) > 2 else "jawab OK saja")
    else: print(__doc__)
