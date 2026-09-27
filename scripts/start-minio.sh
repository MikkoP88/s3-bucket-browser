#!/usr/bin/env bash
# Start a local MinIO server for the e2e suites (used by CI; handy locally
# too). quay.io has refused ANONYMOUS pulls outright since 24 Sep 2026 and
# the image registries have dropped minio entirely (Docker Hub deleted
# minio/minio, bitnami froze at 2019-era tags, dl.min.io binaries 410), so
# this pulls the server BINARY from minio/minio's GitHub releases — the
# same infrastructure that hosts this repository's CI, anonymous, pinned,
# sha256-verified. No account, no secrets, no docker.
#
# The release is pinned deliberately: newer minio/minio tags can ship with
# NO binary assets, so "latest" would be a trap. Override with MINIO_RELEASE.
# S3B_MINIO_OS_ARCH (default linux-amd64) and S3B_MINIO_DIR (default /tmp)
# tune the download for non-CI use, e.g. windows-amd64 on a local Git Bash.
set -euo pipefail

MINIO_RELEASE="${MINIO_RELEASE:-RELEASE.2025-09-07T16-13-09Z}"
OS_ARCH="${S3B_MINIO_OS_ARCH:-linux-amd64}"
DIR="${S3B_MINIO_DIR:-/tmp}"
PORT="${MINIO_PORT:-9000}"
BASE="https://github.com/minio/minio/releases/download/${MINIO_RELEASE}"
# Only the windows assets carry an .exe suffix.
EXT=""
case "${OS_ARCH}" in windows-*) EXT=".exe" ;; esac
ASSET="minio.${OS_ARCH}.${MINIO_RELEASE}${EXT}"
BIN="${DIR}/minio"

mkdir -p "${DIR}"
curl -fsSL -o "${BIN}" "${BASE}/${ASSET}"
# The .sha256sum asset names the release file, not our local path — rewrite
# the filename so sha256sum -c verifies what we actually downloaded.
curl -fsSL "${BASE}/${ASSET}.sha256sum" \
  | awk -v b="${BIN}" '{print $1"  "b}' | sha256sum -c -
chmod +x "${BIN}"
mkdir -p "${DIR}/s3b-minio-data"
MINIO_ROOT_USER="${MINIO_ROOT_USER:-minioadmin}" \
MINIO_ROOT_PASSWORD="${MINIO_ROOT_PASSWORD:-minioadmin}" \
  nohup "${BIN}" server "${DIR}/s3b-minio-data" --address ":${PORT}" \
  >"${DIR}/minio.log" 2>&1 &

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bash "${script_dir}/wait-http.sh" "http://localhost:${PORT}/minio/health/live"
