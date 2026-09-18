#!/usr/bin/env bash
# Kill whatever still listens on the faultproxy ports (a stale proxy from a
# failed run would keep serving its last fault mode). Cross-platform: tries
# taskkill on Windows netstat PIDs, falls back to pkill on POSIX.
for port in "$@"; do
  if command -v netstat >/dev/null 2>&1; then
    netstat -ano 2>/dev/null | awk -v ep=":$port\$" '$2 ~ ep && /LISTENING/ {print $5}' | sort -u |
    while read -r pid; do
      [ -n "$pid" ] || continue
      taskkill //F //PID "$pid" >/dev/null 2>&1 || kill "$pid" >/dev/null 2>&1 || true
      echo "killed PID $pid on :$port"
    done
  fi
done
command -v pkill >/dev/null 2>&1 && pkill -f faultproxy.mjs >/dev/null 2>&1
exit 0
