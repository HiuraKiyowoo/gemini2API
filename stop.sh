#!/bin/bash
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PIDS=$(pgrep -f "gemini2api-backend")

if [ -z "$PIDS" ]; then
  echo "[-] gemini2API tidak sedang berjalan."
else
  echo "[*] Menghentikan gemini2API (PID: $(echo $PIDS | tr '\n' ' '))..."
  kill $PIDS
  sleep 1
  if pgrep -f "gemini2api-backend" > /dev/null; then
    kill -9 $PIDS 2>/dev/null
  fi
  echo "[+] gemini2API berhasil dihentikan."
fi
