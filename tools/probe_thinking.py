#!/usr/bin/env python3
"""probe_thinking.py — uji dukungan penalaran mendalam (thinking) di cloudcode-pa.

Cek 3 hal terpisah:
  A. Apakah request bisa minta mode thinking (generationConfig.thinkingConfig)
  B. Apakah response berisi bagian thought / reasoning (bukan cuma jawaban akhir)
  C. Apakah thoughtSignature muncul (penanda model reasoning betulan)
"""
import json
import os, time, urllib.parse, urllib.request, urllib.error

CID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
CS = "" + os.environ.get("CODE_ASSIST_CLIENT_SECRET", "") + ""
CC = "https://cloudcode-pa.googleapis.com/v1internal"
AUTHZ = "Bea" + "rer "

SOAL = ("Sebuah toko menaikkan harga 20% lalu menurunkan harga baru itu 20%. "
        "Berapa persen perubahan akhir dibanding harga semula? Jelaskan penalaranmu.")


def get_at():
    t = json.load(open("/root/gemini2API/data/google_oauth.json"))
    r = urllib.request.Request("https://oauth2.googleapis.com/token",
        data=urllib.parse.urlencode({"grant_type": "refresh_token",
            "refresh_token": t["refresh_token"], "client_id": CID, "client_secret": CS}).encode())
    return json.loads(urllib.request.urlopen(r, timeout=30).read())["access_token"]


def walk(raw):
    """Ambil text + tandai thought/Signature dari seluruh elemen stream."""
    parts, thoughts, sigs = [], [], 0
    data = json.loads(raw)
    for ch in (data if isinstance(data, list) else [data]):
        resp = ch.get("response") or ch
        for c in (resp.get("candidates") or []):
            for p in ((c.get("content") or {}).get("parts") or []):
                if p.get("thoughtSignature"):
                    sigs += 1
                if p.get("text"):
                    (thoughts if p.get("thought") else parts).append(p["text"])
                if p.get("inlineData") or p.get("functionCall"):
                    parts.append("[non-teks]")
    return "".join(parts), "".join(thoughts), sigs


def ask(tok, model, thinking=None, tries=5, wait=14):
    req = {"contents": [{"role": "user", "parts": [{"text": SOAL}]}],
           "systemInstruction": {"parts": [{"text": "Hari ini %s." % time.strftime("%Y-%m-%d")}]}}
    cfg = {}
    if thinking == "on":
        cfg["thinkingConfig"] = {"includeThoughts": True, "thinkingBudget": 8192}
    elif thinking == "off":
        cfg["thinkingConfig"] = {"thinkingBudget": 0}
    if cfg:
        req["generationConfig"] = cfg
    body = {"model": model, "project": "cloudshell-gca", "request": req}
    for i in range(tries):
        r = urllib.request.Request(CC + ":streamGenerateContent", data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
        try:
            t0 = time.time()
            resp = urllib.request.urlopen(r, timeout=180)
            raw = resp.read().decode()
            return resp.status, walk(raw), time.time() - t0, None
        except urllib.error.HTTPError as e:
            eb = e.read().decode()
            if e.code == 429 and i < tries - 1:
                time.sleep(wait); continue
            try:
                msg = json.loads(eb)["error"]["message"]
            except Exception:
                msg = eb[:160]
            return e.code, ("", "", 0), 0, msg.replace("\n", " ")
    return 0, ("", "", 0), 0, "gagal"


tok = get_at()
MODELS = ["gemini-3-flash-preview", "gemini-2.5-flash", "gemini-2.5-pro",
          "gemini-3-pro-preview", "gemini-3.1-pro-preview", "gemini-3.1-flash-lite"]

for m in MODELS:
    print("=" * 78)
    print("MODEL:", m)
    for mode in ("default", "on", "off"):
        st, (ans, th, sig), el, err = ask(tok, m, None if mode == "default" else mode)
        if st != 200:
            print("  thinking=%-8s HTTP=%s ERR %s" % (mode, st, (err or "")[:120]))
        else:
            print("  thinking=%-8s HTTP=200 %5.1fs | jawaban %4d char | THOUGHT %4d char | thoughtSignature x%d"
                  % (mode, el, len(ans), len(th), sig))
            if th:
                print("     cuplikan thought: %s" % th[:200].replace("\n", " "))
        time.sleep(3)
