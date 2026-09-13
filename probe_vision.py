#!/usr/bin/env python3
"""probe_vision.py — uji apakah model di cloudcode-pa bisa baca gambar (vision)."""
import base64, json, struct, time, zlib, urllib.parse, urllib.request, urllib.error

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


def make_png(path):
    """Bikin PNG 200x200: latar MERAH, kotak BIRU di kiri-atas, angka putih '7' blok."""
    W = H = 200
    rows = []
    for y in range(H):
        row = bytearray()
        for x in range(W):
            if 20 <= x < 80 and 20 <= y < 80:
                row += bytes((0, 0, 255))          # kotak biru
            else:
                row += bytes((220, 30, 30))        # latar merah
        rows.append(b"\x00" + bytes(row))

    def chunk(tag, data):
        c = struct.pack(">I", len(data)) + tag + data
        return c + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)

    raw = b"".join(rows)
    png = (b"\x89PNG\r\n\x1a\n"
           + chunk(b"IHDR", struct.pack(">IIBBBBB", W, H, 8, 2, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(raw, 9))
           + chunk(b"IEND", b""))
    with open(path, "wb") as f:
        f.write(png)
    return png


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


def probe_vision(tok, model, img_b64, prompt, tries=4, wait=13):
    body = {"model": model, "project": "cloudshell-gca",
            "request": {"contents": [{"role": "user", "parts": [
                {"text": prompt},
                {"inlineData": {"mimeType": "image/png", "data": img_b64}}]}],
                "systemInstruction": {"parts": [{"text": "Hari ini %s." % time.strftime("%Y-%m-%d")}]}}}
    for i in range(tries):
        r = urllib.request.Request(CC + ":streamGenerateContent", data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "Authorization": AUTHZ + tok})
        try:
            resp = urllib.request.urlopen(r, timeout=120)
            return resp.status, extract(resp.read().decode())
        except urllib.error.HTTPError as e:
            eb = e.read().decode()
            if e.code == 429 and i < tries - 1:
                time.sleep(wait); continue
            try:
                msg = json.loads(eb)["error"]["message"]
            except Exception:
                msg = eb[:150]
            return e.code, "ERR " + msg.replace("\n", " ")
    return 0, "gagal"


img = make_png("/tmp/tes_vision.png")
b64 = base64.b64encode(img).decode()
print("gambar tes: /tmp/tes_vision.png (%d bytes) — latar MERAH, kotak BIRU kiri-atas\n" % len(img))

tok = get_at()
Q = "Lihat gambar ini. Sebutkan: (1) warna latar, (2) warna kotak, (3) posisi kotaknya. Singkat."
for m in ["gemini-2.5-flash", "gemini-3-flash-preview", "gemini-3.1-flash-lite"]:
    st, txt = probe_vision(tok, m, b64, Q)
    print("%-26s HTTP=%s | %s" % (m, st, txt.strip()[:230]))
    print()
