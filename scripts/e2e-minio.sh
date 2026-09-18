#!/usr/bin/env bash
# End-to-end test of the s3b CLI against a running MinIO on localhost:9000.
#
# Prerequisite: MinIO listening on localhost:9000 with minioadmin/minioadmin
# credentials (see the e2e-minio CI job, or:
#   docker run -d -p 9000:9000 -e MINIO_ROOT_USER=minioadmin \
#     -e MINIO_ROOT_PASSWORD=minioadmin quay.io/minio/minio:latest server /data)
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
  # Headless CLI build: the suite exercises the CLI face only, and the
  # pure-Go build needs no GTK/webkit headers (hermetic CI).
  (cd "$ROOT" && go build -tags s3b_headless -o "$BIN" ./cmd/s3b)
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

step "source add + connectivity test"
"$BIN" source add lab --type s3 --endpoint http://localhost:9000 \
  --access-key minioadmin --secret-key minioadmin >/dev/null
"$BIN" source list | grep -q 'lab' || fail "source not listed"
# s3 sources mirror as legacy profiles — --profile keeps resolving them
"$BIN" profile list | grep -q 'lab' || fail "s3 source not mirrored as profile"
"$BIN" profile test lab | grep -q 'OK' || fail "profile test failed"
pass "source created, mirrored as profile, connectivity OK"

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
expect_fail "$BIN" rm "s3://$BUCKET/data/" -r            # 302 objects, no --force
dry_out="$("$BIN" rm "s3://$BUCKET/data/" -r --dry-run)"
# 301 files + the data/ folder marker itself — recursive delete must remove
# the marker too (MinIO omits it from listings under its own prefix, and a
# delete that misses it leaves a ghost folder behind)
printf '%s\n' "$dry_out" | grep -q 'total: 302' || fail "rm dry-run count: $dry_out"
printf '%s\n' "$dry_out" | grep -q 'would delete s3://'"$BUCKET"'/data/$' || fail "rm dry-run must include the folder marker: $dry_out"
"$BIN" rm "s3://$BUCKET/data/readme.md" >/dev/null
"$BIN" rm "s3://$BUCKET/data/" -r --force | grep -q 'deleted 301 object' || fail "rm -r --force"
expect_fail "$BIN" rb "s3://$BUCKET"                      # still has docs/ + copy/
pass "L1/L2 safety gates enforced"

step "doctor"
"$BIN" doctor "s3://$BUCKET" | grep -q 'DNS Resolution Check' || fail "doctor output"
pass "doctor runs clean"

step "versioning (M4)"
"$BIN" bucket versioning "s3://$BUCKET" on | grep -q 'versioning enabled' || fail "versioning on"
printf 'v1\n' > "$WORK/ver1.txt"
printf 'v2\n' > "$WORK/ver2.txt"
"$BIN" cp "$WORK/ver1.txt" "s3://$BUCKET/ver.txt" >/dev/null
"$BIN" cp "$WORK/ver2.txt" "s3://$BUCKET/ver.txt" >/dev/null
n="$("$BIN" versions ls "s3://$BUCKET/ver.txt" --json | grep -c '"versionId"')"
[ "$n" = "2" ] || fail "versions ls count = $n, want 2"
# rows are newest-first: the second versionId is v1
vid="$("$BIN" versions ls "s3://$BUCKET/ver.txt" --json | grep -o '"versionId": "[^"]*"' | sed -n 2p | cut -d'"' -f4)"
[ -n "$vid" ] || fail "could not parse version id"
"$BIN" versions restore "s3://$BUCKET/ver.txt" --version-id "$vid" | grep -q 'restored' || fail "versions restore"
"$BIN" cp "s3://$BUCKET/ver.txt" "$WORK/restored.txt" >/dev/null
cmp "$WORK/ver1.txt" "$WORK/restored.txt" || fail "restore served wrong content"
pass "timeline, restore-as-latest correct"

step "versioning: delete marker + undo delete"
"$BIN" rm "s3://$BUCKET/ver.txt" >/dev/null  # versioned delete → marker
"$BIN" versions ls "s3://$BUCKET/ver.txt" --json | grep -q '"isDeleteMarker": true' \
  || fail "rm created no delete marker"
mid="$("$BIN" versions ls "s3://$BUCKET/ver.txt" --json | grep -B2 '"isDeleteMarker": true' | grep -o '"versionId": "[^"]*"' | cut -d'"' -f4)"
[ -n "$mid" ] || fail "could not parse delete-marker id"
"$BIN" versions undo "s3://$BUCKET/ver.txt" --version-id "$mid" | grep -q 'is back' || fail "versions undo"
"$BIN" cp "s3://$BUCKET/ver.txt" "$WORK/back.txt" >/dev/null
cmp "$WORK/ver1.txt" "$WORK/back.txt" || fail "undo restored wrong content"
pass "undo delete brings the object back"

step "versioning: purge + permanent destroy (L3)"
"$BIN" versions stat "s3://$BUCKET" | grep -q 'total versions:' || fail "versions stat"
printf 'a\n' > "$WORK/a.txt"
printf 'b\n' > "$WORK/b.txt"
"$BIN" cp "$WORK/a.txt" "s3://$BUCKET/vp.txt" >/dev/null
"$BIN" cp "$WORK/b.txt" "s3://$BUCKET/vp.txt" >/dev/null
"$BIN" versions purge "s3://$BUCKET" --mode noncurrent --dry-run | grep -q 'would purge' \
  || fail "versions purge dry-run"
"$BIN" versions purge "s3://$BUCKET" --mode noncurrent | grep -q 'deleted' \
  || fail "versions purge noncurrent"
"$BIN" versions rm "s3://$BUCKET/ver.txt" --all | grep -q 'deleted' || fail "versions rm --all"
# grep -c exits 1 on zero matches — guard with || true or set -e kills the script.
n="$("$BIN" versions ls "s3://$BUCKET/ver.txt" --json | grep -c '"versionId"' || true)"
[ "$n" = "0" ] || fail "timeline not destroyed, $n left"
pass "purge and permanent destroy work"

step "bucket admin (M3)"
"$BIN" bucket info "s3://$BUCKET" | grep -q 'versioning:' || fail "bucket info"
"$BIN" bucket tags put "s3://$BUCKET" team=e2e env=ci | grep -q 'tag(s) saved' || fail "tags put"
"$BIN" bucket tags get "s3://$BUCKET" | grep -q 'team=e2e' || fail "tags get"
# Recent MinIO releases dropped the bucket-CORS API: accept a graceful
# "not supported" as a pass (the engine maps provider gaps to plain errors).
cors_out="$(printf '[{"origins":["https://example.com"],"methods":["GET"],"maxAge":3600}]' \
  | "$BIN" bucket cors put "s3://$BUCKET" - 2>&1 || true)"
case "$cors_out" in
  *"CORS saved"*)
    "$BIN" bucket cors get "s3://$BUCKET" | grep -q 'example.com' || fail "cors get" ;;
  *"not supported"*)
    printf 'SKIP  bucket CORS API not supported by this MinIO\n' ;;
  *)
    fail "cors put: $cors_out" ;;
esac
cat > "$WORK/policy.json" <<EOF
{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":"s3:GetObject","Resource":"arn:aws:s3:::$BUCKET/*"}]}
EOF
"$BIN" bucket policy put "s3://$BUCKET" "$WORK/policy.json" | grep -q 'policy saved' || fail "policy put"
"$BIN" bucket policy get "s3://$BUCKET" | grep -q "$BUCKET" || fail "policy get"
enc_out="$("$BIN" bucket encryption put "s3://$BUCKET" --algo AES256 2>&1 || true)"
case "$enc_out" in
  *"saved"*)
    "$BIN" bucket encryption get "s3://$BUCKET" | grep -q 'AES256' || fail "encryption get" ;;
  *"not supported"*)
    printf 'SKIP  default encryption not supported by this MinIO\n' ;;
  *)
    fail "encryption put: $enc_out" ;;
esac
pass "tags/cors/policy/encryption manageable"

step "deep search (M5)"
# At this point the bucket holds exactly three current objects:
# docs/ (marker), copy/readme-v2.md and vp.txt.
n="$("$BIN" find "s3://$BUCKET" --name 'readme*' --json | grep -c '"key"')"
[ "$n" = "1" ] || fail "find --name glob count = $n, want 1"
n="$("$BIN" find "s3://$BUCKET" --name 'vp' --json | grep -c '"key"')"
[ "$n" = "1" ] || fail "find --name substring count = $n, want 1"
n="$("$BIN" find "s3://$BUCKET" --smaller 1B --json | grep -c '"key"')"
[ "$n" = "1" ] || fail "find --smaller 1B count = $n, want 1 (the docs/ marker)"
n="$("$BIN" find "s3://$BUCKET" --limit 1 --json | grep -c '"key"')"
[ "$n" = "1" ] || fail "find --limit count = $n, want 1"
find_out="$("$BIN" find "s3://$BUCKET" --name 'vp' 2>&1)"
printf '%s\n' "$find_out" | grep -q '1 match(es) among 3 scanned' \
  || fail "find summary: $find_out"
pass "name/size/limit filters + summary correct"

step "storage-class conversion (M5)"
# REDUCED_REDUNDANCY is the one alternative class every S3-compatible
# store understands; MinIO validates storage classes on copy.
sc_out="$("$BIN" sc "s3://$BUCKET/copy/readme-v2.md" REDUCED_REDUNDANCY 2>&1 || true)"
case "$sc_out" in
  *"converted"*)
    "$BIN" ls "s3://$BUCKET/copy/" --json | grep -q '"storageClass": "REDUCED_REDUNDANCY"' \
      || fail "storage class not visible after conversion"
    n="$("$BIN" find "s3://$BUCKET" --class REDUCED_REDUNDANCY --json | grep -c '"key"')"
    [ "$n" = "1" ] || fail "find --class count = $n, want 1"
    printf 'sc-a\n' > "$WORK/sc-a.txt"
    printf 'sc-b\n' > "$WORK/sc-b.txt"
    "$BIN" cp "$WORK/sc-a.txt" "s3://$BUCKET/sczone/a.txt" >/dev/null
    "$BIN" cp "$WORK/sc-b.txt" "s3://$BUCKET/sczone/b.txt" >/dev/null
    "$BIN" sc "s3://$BUCKET/sczone/" STANDARD -r --dry-run | grep -q 'would convert 2 object(s)' \
      || fail "sc dry-run"
    expect_fail "$BIN" sc "s3://$BUCKET/sczone/" STANDARD  # prefix needs -r
    "$BIN" sc "s3://$BUCKET/sczone/" STANDARD -r | grep -q 'converted 2 object(s)' \
      || fail "sc recursive"
    pass "self-copy conversion, dry-run, gate, find --class"
    ;;
  *"not supported"*|*"Invalid storage class"*)
    printf 'SKIP  storage-class conversion not supported by this MinIO\n' ;;
  *)
    fail "sc single object: $sc_out" ;;
esac

step "object lock (M5)"
# Dedicated bucket: object lock is permanent and can only be enabled at
# creation (mb --object-lock; versioning comes with it). GOVERNANCE only —
# it can be cleared, so cleanup can actually delete the object again.
LOCKED="s3b-e2e-lock-$RANDOM$RANDOM"
"$BIN" mb "s3://$LOCKED" --object-lock >/dev/null
lock_out="$("$BIN" bucket lock "s3://$LOCKED" --enable 2>&1 || true)"
case "$lock_out" in
  *"object lock enabled"*)
    "$BIN" bucket lock "s3://$LOCKED" | grep -q 'object lock: enabled' || fail "lock config show"
    printf 'locked\n' > "$WORK/locked.txt"
    "$BIN" cp "$WORK/locked.txt" "s3://$LOCKED/important.txt" >/dev/null
    "$BIN" lock retention "s3://$LOCKED/important.txt" --mode GOVERNANCE --until +1h \
      | grep -q 'retention GOVERNANCE' || fail "retention set"
    "$BIN" lock retention "s3://$LOCKED/important.txt" | grep -q 'GOVERNANCE' || fail "retention show"
    # Note: with minioadmin root creds MinIO exempts us from governance/hold
    # delete-blocking (root bypasses silently), so enforcement is not
    # assertable here — set/show/clear state round-trips are.
    "$BIN" lock retention "s3://$LOCKED/important.txt" --clear --bypass-governance \
      | grep -q 'retention cleared' || fail "retention clear"
    "$BIN" lock retention "s3://$LOCKED/important.txt" | grep -q 'no retention configured' \
      || fail "retention show after clear"
    "$BIN" lock legalhold "s3://$LOCKED/important.txt" --on | grep -q 'legal hold ON' \
      || fail "legalhold on"
    "$BIN" lock legalhold "s3://$LOCKED/important.txt" | grep -q 'legal hold: ON' \
      || fail "legalhold show"
    "$BIN" lock legalhold "s3://$LOCKED/important.txt" --off | grep -q 'legal hold OFF' \
      || fail "legalhold off"
    "$BIN" versions rm "s3://$LOCKED/important.txt" --all >/dev/null
    "$BIN" rb "s3://$LOCKED" --force | grep -q 'removed bucket' || fail "rb lock bucket"
    pass "retention + legal hold round-trips + cleanup OK"
    ;;
  *"not supported"*)
    printf 'SKIP  object lock not supported by this MinIO\n'
    "$BIN" rb "s3://$LOCKED" --force >/dev/null 2>&1 || true ;;
  *)
    fail "bucket lock enable: $lock_out" ;;
esac

step "bad/slow connections: faultproxy lab (latency/throttle/reset/blackhole)"
# scripts/faultproxy.mjs is a TCP fault-injection proxy with an HTTP control
# plane: one process serves the whole scenario list while the CLI's keep-alive
# pool stays connected to it — the "connection went bad mid-session" class,
# not a fresh dial per scenario. CLI contract under test: slow links still
# succeed with correct output, dead links fail CLEANLY and inside the
# --timeout budget (never a 5-minute default hang).
if command -v node >/dev/null 2>&1 && command -v curl >/dev/null 2>&1; then
  FPORT=19000 FCTL=19001
  # A previously failed run can leave its faultproxy alive holding the ports
  # (MSYS job-control kills do not reach node on Windows) — still sitting in
  # whatever fault mode killed that run. Clear the ports up front and use
  # the same port-based kill in the EXIT trap.
  FPID=""
  fault_stop() {
    [ -n "$FPID" ] && kill "$FPID" >/dev/null 2>&1 || true
    bash "$ROOT/scripts/kill-faultports.sh" "$FPORT" "$FCTL" >/dev/null 2>&1 || true
  }
  trap 'fault_stop; rm -rf "$WORK"' EXIT
  fault_stop
  node "$ROOT/scripts/faultproxy.mjs" --listen "$FPORT" --control "$FCTL" \
    --target 127.0.0.1:9000 >/dev/null 2>&1 &
  FPID=$!
  for _ in $(seq 1 50); do
    curl -s "http://127.0.0.1:$FCTL/state" >/dev/null 2>&1 && break
    sleep 0.1
  done
  fmode() { curl -s -X POST -H 'content-type: application/json' -d "$1" \
    "http://127.0.0.1:$FCTL/mode" >/dev/null; }
  "$BIN" source add faultlab --type s3 --endpoint "http://127.0.0.1:$FPORT" \
    --access-key minioadmin --secret-key minioadmin >/dev/null

  # direct (control): the proxy itself is transparent
  direct_out="$("$BIN" ls "s3://$BUCKET" --recursive --json --profile faultlab)"
  printf '%s\n' "$direct_out" | grep -q 'copy/readme-v2.md' || fail "fault lab control listing"
  pass "direct: listing through the proxy OK"

  # latency: every TCP chunk held 600ms — slow but correct
  fmode '{"mode":"latency","delayMs":600}'
  t0=$SECONDS
  lat_out="$("$BIN" ls "s3://$BUCKET" --recursive --json --profile faultlab)"
  printf '%s\n' "$lat_out" | grep -q 'copy/readme-v2.md' || fail "listing under latency"
  [ $((SECONDS - t0)) -ge 1 ] || fail "latency run was instant — delay not applied"
  pass "latency: correct listing, visibly slower ($((SECONDS - t0))s)"

  # throttle 1KB/s: paced, still complete
  fmode '{"mode":"throttle","bps":1024}'
  t0=$SECONDS
  thr_out="$("$BIN" ls "s3://$BUCKET" --recursive --json --profile faultlab)"
  printf '%s\n' "$thr_out" | grep -q 'copy/readme-v2.md' || fail "listing under throttle"
  [ $((SECONDS - t0)) -ge 2 ] || fail "throttled run too fast — pacing not applied"
  pass "throttle: listing trickled in but complete ($((SECONDS - t0))s)"

  # reset: RST mid-session — hard error, no hang, no partial success
  fmode '{"mode":"reset"}'
  if "$BIN" ls "s3://$BUCKET" --profile faultlab >/dev/null 2>&1; then
    fail "ls unexpectedly survived a reset connection"
  else
    pass "reset: rejected cleanly"
  fi

  # blackhole: dead endpoint — the per-request --timeout must fire (3s),
  # not the 5-minute default
  fmode '{"mode":"blackhole"}'
  t0=$SECONDS
  bh_out="$("$BIN" ls "s3://$BUCKET" --profile faultlab --timeout 3s 2>&1 || true)"
  [ $((SECONDS - t0)) -le 10 ] || fail "blackhole ls took $((SECONDS - t0))s — timeout not honored"
  case "$bh_out" in
    *'timed out'*|*timeout*|*deadline*|*context*) ;;
    *) fail "blackhole error is not a timeout: $bh_out" ;;
  esac
  pass "blackhole: timed out on schedule ($((SECONDS - t0))s), clean error"

  # recovery: with the fault cleared the same source works again
  fmode '{"mode":"direct"}'
  rec_out="$("$BIN" ls "s3://$BUCKET" --recursive --json --profile faultlab)"
  printf '%s\n' "$rec_out" | grep -q 'copy/readme-v2.md' || fail "post-fault recovery listing"
  pass "recovery: source healthy again after the fault cleared"

  "$BIN" source remove faultlab | grep -q 'removed source' || fail "faultlab cleanup"
  kill "$FPID" >/dev/null 2>&1 || true
else
  printf 'SKIP  fault lab: node/curl not available\n'
fi

step "bucket removal + cleanup"
# Versioned bucket with markers: rb --force must purge version history too (M4).
"$BIN" rb "s3://$BUCKET" --force | grep -q 'removed bucket' || fail "rb --force (versioned)"
if "$BIN" ls --json | grep -q "$BUCKET"; then fail "bucket still visible"; fi
"$BIN" source remove lab | grep -q 'removed source' || fail "source cleanup"
pass "bucket and source removed"

printf '\nALL E2E CHECKS PASSED\n'
