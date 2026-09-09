#!/usr/bin/env bash
# Wait until a URL returns HTTP 200 (default: MinIO health on :9000).
url="${1:-http://localhost:9000/minio/health/live}"
for _ in $(seq 1 30); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$url" 2>/dev/null)
  if [ "$code" = "200" ]; then
    echo "ready: $url"
    exit 0
  fi
  sleep 2
done
echo "timeout waiting for $url" >&2
exit 1
