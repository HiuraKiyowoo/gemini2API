#!/usr/bin/env python3
"""daftar_akun_oauth.py — daftarkan akun OAuth Google ke data/accounts.json
supaya gateway bisa memakainya lewat pool (token handle = 'oauth:<file>').

Akun cookie tetap didukung; ini hanya menambah entri sumber OAuth.
"""
import json, os, sys

ACCOUNTS = "/root/gemini2API/data/accounts.json"
TOKEN_FILE = "/root/gemini2API/data/google_oauth.json"

if not os.path.exists(TOKEN_FILE):
    print("[!] %s belum ada" % TOKEN_FILE); sys.exit(1)

tok = json.load(open(TOKEN_FILE))
if not tok.get("refresh_token"):
    print("[!] file OAuth tidak punya refresh_token"); sys.exit(1)

accs = json.load(open(ACCOUNTS)) if os.path.exists(ACCOUNTS) and os.path.getsize(ACCOUNTS) else []

# hindari duplikat
handle = "oauth:google_oauth.json"
accs = [a for a in accs if a.get("token") != handle and a.get("source") != "oauth_google"]

accs.append({
    "email": "google-oauth@local",
    "password": "",
    # token handle wajib berawalan 'oauth:' — itulah penanda akun Code Assist
    "token": "oauth:google-oauth@local",
    "cookies": "",
    "username": "",
    "source": "oauth_google",
    "auth_type": "code_assist",
    "oauth_file": "google_oauth.json",
    "env_name": "",
    "activation_pending": False,
    "status_code": "valid",
    "last_error": "",
    "consecutive_failures": 0,
    "rate_limit_strikes": 0,
})

with open(ACCOUNTS, "w") as f:
    json.dump(accs, f, indent=2)
os.chmod(ACCOUNTS, 0o600)
print("akun OAuth didaftarkan:", handle)
print("total akun:", len(accs))
