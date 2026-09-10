#!/bin/bash
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

if [ -f .env ]; then
  set -a
  source .env
  set +a
fi

PORT="${PORT:-7860}"

if [ "$1" = "--daemon" ] || [ "$1" = "-d" ]; then
  if pgrep -f "gemini2api-backend" > /dev/null; then
    PID=$(pgrep -f "gemini2api-backend" | tr '\n' ' ')
    echo "[!] gemini2API sudah berjalan (PID: $PID)."
    exit 0
  fi
  setsid -f "$DIR/bin/gemini2api-backend" </dev/null > "$DIR/logs/output.log" 2>&1
  sleep 1
  PID=$(pgrep -f "gemini2api-backend")
  if [ -n "$PID" ]; then
    echo "[+] gemini2API berhasil dijalankan di background (PID: $PID)"
    echo "[+] WebUI & API: http://127.0.0.1:$PORT"
    echo "[+] Log: $DIR/logs/output.log"
  else
    echo "[-] Gagal menjalankan gemini2API di background. Cek log: $DIR/logs/output.log"
    exit 1
  fi
else
  echo "[*] Menjalankan gemini2API di port $PORT..."
  echo "[*] WebUI & API: http://127.0.0.1:$PORT"
  echo "[*] Tekan Ctrl+C untuk menghentikan."
  exec "$DIR/bin/gemini2api-backend"
fi
