#!/usr/bin/env python3
"""tes_tanggal.py — buktikan: model jawab benar kalau tanggal disuntik ke systemInstruction."""
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


def extract(raw):
    out, data = [], json.loads(raw)
    for ch in (data if isinstance(data, list) else [data]):
        resp = ch.get("response") or ch
        for c in (resp.get("candidates") or []):
            for p in ((c.get("content") or {}).get("parts") or []):
                if p.get("text"):
                    out.append(p["text"])
    return "".join(out)


def ask(tok, model, prompt, sysinst=None, tries=6):
    req_obj = {"contents": [{"role": "user", "parts": [{"text": prompt}]}]}
    if sysinst:
        req_obj["systemInstruction"] = {"parts": [{"text": sysinst}]}
    body = {"model": model, "project": "cloudshell-gca", "request": req_obj}
    for i in range(tries):
        r = urllib.request.Request(CC + ":streamGenerateContent", data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
        try:
            resp = urllib.request.urlopen(r, timeout=120)
            return resp.status, extract(resp.read().decode())
        except urllib.error.HTTPError as e:
            b = e.read().decode()
            if e.code == 429 and i < tries - 1:
                time.sleep(12); continue
            return e.code, "ERR " + b[:100].replace("\n", " ")
    return 0, "gagal"


tok = get_at()
now = time.strftime("%Y-%m-%d")
Q = "Berapa tahun saat ini? Jawab satu angka saja."

print("tanggal sistem:", now, "\n")

st, txt = ask(tok, "gemini-2.5-flash", Q)
print("TANPA system prompt  ->", st, "|", txt.strip()[:100])

si = "Kamu asisten. Hari ini tanggal %s. Pakai tanggal ini kalau ditanya soal waktu." % now
st, txt = ask(tok, "gemini-2.5-flash", Q, sysinst=si)
print("DENGAN system prompt ->", st, "|", txt.strip()[:100])

Q2 = "Berapa tahun saat ini? Dan sebut satu peristiwa besar tahun 2026."
st, txt = ask(tok, "gemini-2.5-flash", Q2, sysinst=si)
print("Q lanjutan (2026)    ->", st, "|", txt.strip()[:260])
