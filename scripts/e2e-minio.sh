#!/usr/bin/env bash
# End-to-end test of the s3b CLI against a running MinIO on localhost:9000.
#
# Prerequisite: MinIO listening on localhost:9000 with minioadmin/minioadmin
# credentials (see the e2e-minio CI job, or:
#   docker run -d -p 9000:9000 -e MINIO_ROOT_USER=minioadmin \
#     -e MINIO_ROOT_PASSWORD=minioadmin minio/minio:latest server /data)
#
# The test never touches the user's profile store: S3B_CONFIG is pointed at a
# throwaway directory for the whole run.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
S3B_CONFIG="$WORK/config"
export S3B_CONFIG
BUCKET="s3b-e2e-$RANDOM$RANDOM"
trap 'rm -rf "$WORK"' EXIT

if [ -z "${S3B_BIN:-}" ]; then
  BIN="$WORK/s3b"
  case "$(go env GOOS)" in windows) BIN="$BIN.exe";; esac
  (cd "$ROOT" && go build -o "$BIN" ./cmd/s3b)
else
  BIN="$S3B_BIN"
fi

step() { printf '\n== %s ==\n' "$1"; }
pass()  { printf 'PASS  %s\n' "$1"; }
fail()  { printf 'FAIL  %s\n' "$1" >&2; exit 1; }
# expect_fail CMD... — the command must exit non-zero (safety gates).
expect_fail() {
  if "$@" >/dev/null 2>&1; then fail "expected rejection: $*"; else pass "rejected as designed: $*"; fi
}

step "profile add + connectivity test"
"$BIN" profile add lab --endpoint http://localhost:9000 \
  --access-key minioadmin --secret-key minioadmin --default >/dev/null
"$BIN" profile list | grep -q 'lab' || fail "profile not listed"
"$BIN" profile test lab | grep -q 'OK' || fail "profile test failed"
pass "profile created and connectivity OK"

step "bucket + folder creation"
"$BIN" mb "s3://$BUCKET" | grep -q 'created bucket' || fail "mb"
"$BIN" mkdir "s3://$BUCKET/docs/" | grep -q 'created folder' || fail "mkdir"
pass "bucket and folder marker created"

step "recursive upload (301 files)"
mkdir -p "$WORK/data/docs" "$WORK/data/logs"
for i in $(seq 1 100); do printf 'document %d\n' "$i" > "$WORK/data/docs/doc-$i.txt"; done
for i in $(seq 1 100); do printf 'log line %d\n' "$i" > "$WORK/data/logs/log-$i.txt"; done
for i in $(seq 1 100); do printf 'root file %d\n' "$i" > "$WORK/data/root-$i.txt"; done
printf 'hello s3b e2e\n' > "$WORK/data/readme.md"
"$BIN" cp -r "$WORK/data" "s3://$BUCKET/data/" --json | grep -q '"items": 301' \
  || fail "recursive upload count"
pass "301 items uploaded"

step "listing: ls / tree / du / stat"
count=$("$BIN" ls "s3://$BUCKET/data/" --recursive --json | grep -c '"key"')
[ "$count" = "301" ] || fail "recursive ls count = $count, want 301"
# Capture-then-grep: piping straight into `grep -q` would SIGPIPE the
# producer under `set -o pipefail` (grep exits on first match).
tree_out="$("$BIN" tree "s3://$BUCKET")"
printf '%s\n' "$tree_out" | grep -q 'docs/' || fail "tree missing docs/: $tree_out"
"$BIN" du "s3://$BUCKET" | grep -Eq '30[12] object' || fail "du object count"
"$BIN" stat "s3://$BUCKET" --json | grep -q '"region"' || fail "stat bucket"
"$BIN" stat "s3://$BUCKET/data/readme.md" | grep -q 'size:' || fail "stat object"
pass "list/tree/du/stat correct"

step "download + compare checksum"
"$BIN" cp "s3://$BUCKET/data/readme.md" "$WORK/out/readme.md" >/dev/null
cmp "$WORK/data/readme.md" "$WORK/out/readme.md" || fail "downloaded file differs"
pass "download round-trip identical"

step "server-side copy s3 -> s3"
"$BIN" cp "s3://$BUCKET/data/readme.md" "s3://$BUCKET/copy/readme-v2.md" >/dev/null
"$BIN" ls "s3://$BUCKET/copy/" --json | grep -q 'readme-v2.md' || fail "s3 copy missing"
pass "server-side copy works"

step "sync"
"$BIN" sync "$WORK/data" "s3://$BUCKET/data/" --json | grep -q '"transferred": 0' \
  || fail "sync should transfer nothing when in sync"
printf 'new file\n' > "$WORK/data/new.txt"
"$BIN" sync "$WORK/data" "s3://$BUCKET/data/" --json | grep -q '"transferred": 1' \
  || fail "sync should upload the new file"
rm "$WORK/data/new.txt"
"$BIN" sync "$WORK/data" "s3://$BUCKET/data/" --delete --json | grep -q '"deleted": 1' \
  || fail "sync --delete should remove the extra remote object"
pass "sync upload/no-op/delete all correct"

step "presign + fetch"
url="$("$BIN" presign "s3://$BUCKET/data/readme.md" --expires 5m)"
curl -s "$url" | cmp - "$WORK/data/readme.md" || fail "presigned URL fetch mismatch"
pass "presigned URL serves the object"

step "safety gates"
expect_fail "$BIN" rm "s3://$BUCKET/data/" -r            # 301 objects, no --force
dry_out="$("$BIN" rm "s3://$BUCKET/data/" -r --dry-run)"
printf '%s\n' "$dry_out" | grep -q 'total: 301' || fail "rm dry-run count: $dry_out"
"$BIN" rm "s3://$BUCKET/data/readme.md" >/dev/null
"$BIN" rm "s3://$BUCKET/data/" -r --force | grep -q 'deleted 300 object' || fail "rm -r --force"
expect_fail "$BIN" rb "s3://$BUCKET"                      # still has docs/ + copy/
pass "L1/L2 safety gates enforced"

step "doctor"
"$BIN" doctor "s3://$BUCKET" | grep -q 'DNS Resolution Check' || fail "doctor output"
pass "doctor runs clean"

step "bucket removal + cleanup"
"$BIN" rb "s3://$BUCKET" --force | grep -q 'removed bucket' || fail "rb --force"
if "$BIN" ls --json | grep -q "$BUCKET"; then fail "bucket still visible"; fi
"$BIN" profile remove lab | grep -q 'removed profile' || fail "profile cleanup"
pass "bucket and profile removed"

printf '\nALL E2E CHECKS PASSED\n'
