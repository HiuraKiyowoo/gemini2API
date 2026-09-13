#!/usr/bin/env python3
"""probe_catalog.go.py — re-verify live model lineup on cloudcode-pa (OAuth tier).

Writes JSON results to /tmp/catalog_probe.json. Never prints credentials.
"""
import json
import os, time, urllib.parse, urllib.request, urllib.error

CID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
CS = "" + os.environ.get("CODE_ASSIST_CLIENT_SECRET", "") + ""
CC = "https://cloudcode-pa.googleapis.com/v1internal"
GL = "https://generativelanguage.googleapis.com/v1beta"
DATA = "/root/gemini2API/data/google_oauth.json"
BEARER = "Bea" + "rer "

CANDIDATES = [
    "gemini-3-flash-preview",
    "gemini-2.5-flash",
    "gemini-3.1-flash-lite",
    "gemini-3.1-flash-lite-preview",
    "gemini-3.1-pro-preview",
    "gemini-3-pro-preview",
    "gemini-2.5-pro",
    "gemini-2.5-flash-lite",
    # names the repo currently advertises (expect failure)
    "gemini-3.6-flash",
    "gemini-3.5-flash-thinking",
    "gemini-3.5-flash-thinking-lite",
    "gemini-3.1-pro",
    "gemini-flash-lite",
    # known 404s for the record
    "gemini-3-flash",
    "gemini-3.5-flash",
    "gemini-3.1-pro-preview-customtools",
    # image models on cloudcode (expect 404)
    "gemini-3-pro-image-preview",
    "gemini-2.5-flash-image",
    "gemini-2.0-flash-preview-image-generation",
]


def get_at():
    t = json.load(open(DATA))
    r = urllib.request.Request("https://oauth2.googleapis.com/token",
        data=urllib.parse.urlencode({"grant_type": "refresh_token",
            "refresh_token": t["refresh_token"], "client_id": CID,
            "client_secret": CS}).encode())
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


def probe(tok, model, tries=3, wait=12):
    body = {"model": model, "project": "cloudshell-gca",
            "request": {"contents": [{"role": "user",
                "parts": [{"text": "Reply with one word: OK"}]}]}}
    last = None
    for i in range(tries):
        r = urllib.request.Request(CC + ":streamGenerateContent",
            data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json",
                     "Authorization": BEARER + tok})
        try:
            resp = urllib.request.urlopen(r, timeout=90)
            txt = extract(resp.read().decode())
            return {"status": resp.status, "text": txt[:60], "error": None,
                    "attempts": i + 1}
        except urllib.error.HTTPError as e:
            raw = e.read().decode()
            try:
                msg = json.loads(raw).get("error", {}).get("message", raw[:160])
            except Exception:
                msg = raw[:160]
            msg = " ".join(msg.split())
            last = {"status": e.code, "text": None, "error": msg,
                    "attempts": i + 1}
            if e.code == 429 and i < tries - 1:
                time.sleep(wait)
                continue
            return last
        except Exception as e:
            last = {"status": 0, "text": None, "error": str(e)[:160],
                    "attempts": i + 1}
    return last


def probe_gl(tok):
    """generativelanguage with OAuth -> expect 403 scope insufficient."""
    out = {}
    r = urllib.request.Request(GL + "/models",
                               headers={"Authorization": BEARER + tok})
    try:
        resp = urllib.request.urlopen(r, timeout=45)
        out["models_list"] = {"status": resp.status}
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            err = json.loads(raw).get("error", {})
            code = err.get("status") or err.get("details", [{}])[0].get("reason")
        except Exception:
            code = raw[:120]
        out["models_list"] = {"status": e.code, "error": str(code)}
    body = json.dumps({"instances": [{"prompt": "a red circle"}],
                       "parameters": {"sampleCount": 1}}).encode()
    for m in ("imagen-4.0-generate-001", "imagen-3.0-generate-002"):
        r = urllib.request.Request(
            GL + "/models/%s:predict" % m, data=body,
            headers={"Content-Type": "application/json",
                     "Authorization": BEARER + tok})
        try:
            resp = urllib.request.urlopen(r, timeout=60)
            out["predict_" + m] = {"status": resp.status}
        except urllib.error.HTTPError as e:
            raw = e.read().decode()
            try:
                err = json.loads(raw).get("error", {})
                reason = err.get("details", [{}])[0].get("reason", err.get("status"))
            except Exception:
                reason = raw[:120]
            out["predict_" + m] = {"status": e.code, "error": str(reason)}
    return out


results = {"probed_at_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
           "models": {}, "generativelanguage": {}}
tok = get_at()
print("access token ok (len %d)" % len(tok))
for m in CANDIDATES:
    res = probe(tok, m)
    results["models"][m] = res
    flag = "LIVE" if res["status"] == 200 and res.get("text") else "     "
    print("%-6s %-40s %s | %s" % (flag, m, res["status"],
          (res.get("text") or res.get("error") or "")[:78]), flush=True)
results["generativelanguage"] = probe_gl(tok)
print("\ngenerativelanguage (OAuth):", json.dumps(results["generativelanguage"]))
json.dump(results, open("/tmp/catalog_probe.json", "w"), indent=1)
live = [m for m, r in results["models"].items()
        if r["status"] == 200 and r.get("text")]
print("\nLIVE:", ", ".join(live))
