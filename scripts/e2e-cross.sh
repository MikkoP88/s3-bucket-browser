#!/usr/bin/env bash
# Cross-source structure-fidelity e2e: every ordered (source, destination)
# pairing of local / S3 / SFTP / FTP / WebDAV — plus real Hetzner Object
# Storage when credentials are provided — must reproduce the IDENTICAL
# source tree at the destination: same relative paths, same sizes, and
# (per round-trip cell) the same bytes.
#
# Servers (see the e2e-cross CI job, or locally:
#   docker run -d -p 9000:9000 -e MINIO_ROOT_USER=minioadmin \
#     -e MINIO_ROOT_PASSWORD=minioadmin quay.io/minio/minio:latest server /data
#   docker run -d -p 2222:22 atmoz/sftp:latest e2e:e2epass:1001:100:upload
#   docker run -d -p 2121:21 -p 21000-21002:21000-21002 \
#     -e FTP_USER=e2e -e FTP_PASS=e2epass -e PASV_ADDRESS=127.0.0.1 \
#     -e PASV_MIN_PORT=21000 -e PASV_MAX_PORT=21002 \
#     -e REVERSE_LOOKUP_ENABLE=NO fauria/vsftpd
#   docker run -d -p 7070:80 rclone/rclone:latest \
#     serve webdav --addr :80 --user e2e --pass e2epass /srv)
#
# Hetzner (env-gated — never in CI by default):
#   S3B_HETZNER_ACCESS_KEY / S3B_HETZNER_SECRET_KEY
#   S3B_HETZNER_ENDPOINT   (default https://fsn1.yourobjectstorage.com)
#   S3B_HETZNER_BUCKET     (default s3b-e2e-cross-<random>; purged on exit)
#
# Also covers the version-preservation contract: cp --versions must
# recreate the full timeline (versions AND delete markers) at a versioned
# destination, a plain cp must collapse to the latest version, and
# mv --versions must purge the source timeline.
#
# The test never touches the user's profile store: S3B_CONFIG is pointed at a
# throwaway directory for the whole run.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
S3B_CONFIG="$WORK/config"
export S3B_CONFIG

SFTP_PORT="${S3B_SFTP_PORT:-2222}"
FTP_PORT="${S3B_FTP_PORT:-2121}"
WEBDAV_PORT="${S3B_WEBDAV_PORT:-7070}"
MINIO_PORT="${S3B_MINIO_PORT:-9000}"

BUCKET="s3b-e2e-cross-$RANDOM$RANDOM"
V1="s3b-e2e-cross-v1-$RANDOM$RANDOM"
V2="s3b-e2e-cross-v2-$RANDOM$RANDOM"
# Run-unique scratch namespace: the sftp/ftp/webdav containers persist
# between local runs, so fixed dir names would collide with a previous
# (possibly failed) run's leftovers — e.g. a rename from the mv step
# lingering in the tree-diff. S3 sides are already isolated by the random
# bucket names; this covers the directory-shaped engines.
RUN="r$RANDOM$RANDOM"

if [ -z "${S3B_BIN:-}" ]; then
  BIN="$WORK/s3b"
  case "$(go env GOOS)" in windows) BIN="$BIN.exe";; esac
  # Headless CLI build: pure Go, no GTK/webkit headers (hermetic CI).
  (cd "$ROOT" && go build -tags s3b_headless -o "$BIN" ./cmd/s3b)
else
  BIN="$S3B_BIN"
fi

step() { printf '\n== %s ==\n' "$1"; }
pass()  { printf 'PASS  %s\n' "$1"; }
fail()  { printf 'FAIL  %s\n' "$1" >&2; exit 1; }
expect_fail() {
  if "$@" >/dev/null 2>&1; then fail "expected failure: $*"; else pass "failed as designed: $*"; fi
}

# wait_tcp HOST PORT — block until a TCP connect succeeds (server up).
wait_tcp() {
  local i
  for i in $(seq 1 60); do
    if (exec 3<>"/dev/tcp/$1/$2") 2>/dev/null; then exec 3>&- 3<&- || true; return 0; fi
    sleep 1
  done
  return 1
}

# ---- canonical tree listing -------------------------------------------------
# canon URI OUT [ls flags...] — the tree at URI as a sorted "relpath<TAB>size"
# list of FILES only. URI forms:
#   s3://bucket/prefix   (extra flags: e.g. --profile hetzner)
#   NAME://dir           (any saved non-S3 source)
#   /local/dir           (walked with find)
# Folder markers and directory rows are engine bookkeeping, not content —
# dropped. Empty FILES survive; only relative paths remain so any two sides
# of a transfer diff directly.
canon() {
  local uri="$1" out="$2"; shift 2
  case "$uri" in
    s3://*)
      local rest="${uri#s3://}" prefix=""
      case "$rest" in */*) prefix="${rest#*/}";; esac
      prefix="${prefix%/}"
      "$BIN" ls "$uri" --recursive --json "$@" | canon_awk "$prefix" false
      ;;
    *://*)
      local dir="${uri#*://}"
      dir="${dir#/}"; dir="${dir%/}"
      "$BIN" ls "$uri" --recursive --json "$@" | canon_awk "$dir" true
      ;;
    *)
      # plain local directory — %P is the path relative to the walk root
      (cd "$uri" && find . -type f -printf '%P\t%s\n' | LC_ALL=C sort)
      ;;
  esac > "$out"
}

# canon_awk PREFIX ANCHORED — pairs "key"/"isDir"/"size" lines of the
# indented ls --json stream into relative file rows. ANCHORED=true strips
# the remote engines' leading "/" (S3 keys are prefix-relative already).
canon_awk() {
  LC_ALL=C awk -v pfx="$1" -v anchored="$2" '
    /"key":/ { key=$0; sub(/.*"key": *"/, "", key); sub(/",?$/, "", key) }
    /"isDir":/ { d=$0; sub(/.*"isDir": */, "", d); sub(/,.*/, "", d); isdir=(d == "true") }
    /"size":/ {
      s=$0; sub(/.*"size": */, "", s); sub(/[},].*/, "", s)
      if (key != "" && !isdir && key !~ /\/$/) {
        # strip the anchored leading "/" FIRST — remote keys are rooted at
        # "/" ("/tree/docs/a.md") while pfx is prefix-relative ("tree")
        if (anchored == "true") sub(/^\//, "", key)
        if (pfx != "" && index(key, pfx "/") == 1) key = substr(key, length(pfx) + 2)
        else if (pfx != "" && key != pfx) key = "MISROOTED:" key
        print key "\t" s
      }
      key=""; isdir=0
    }' | LC_ALL=C sort
}

# tree_diff NAME SRC DST [S3PROF_SRC] [S3PROF_DST] — copy-independent
# fidelity assertion: canon both sides (optional per-side --profile value,
# plain "lab"/"xh"/"") and diff. Profiles apply ONLY to s3:// operands;
# the remote and local canon branches ignore them.
tree_diff() {
  local name="$1" src="$2" dst="$3" psrc="${4:-}" pdst="${5:-}"
  local fsrc="" fdst=""
  [ -n "$psrc" ] && fsrc="--profile $psrc"
  [ -n "$pdst" ] && fdst="--profile $pdst"
  # shellcheck disable=SC2086
  canon "$src" "$WORK/canon-a" $fsrc
  # shellcheck disable=SC2086
  canon "$dst" "$WORK/canon-b" $fdst
  if ! diff -u "$WORK/canon-a" "$WORK/canon-b" > "$WORK/canon-diff"; then
    cat "$WORK/canon-diff" >&2
    fail "tree mismatch: $name"
  fi
}

# ---- servers + sources ------------------------------------------------------

wait_tcp 127.0.0.1 "$MINIO_PORT"   || fail "no MinIO on port $MINIO_PORT"
wait_tcp 127.0.0.1 "$SFTP_PORT"    || fail "no SFTP server on port $SFTP_PORT"
wait_tcp 127.0.0.1 "$FTP_PORT"     || fail "no FTP server on port $FTP_PORT"
wait_tcp 127.0.0.1 "$WEBDAV_PORT"  || fail "no WebDAV server on port $WEBDAV_PORT"
pass "MinIO, SFTP, FTP and WebDAV reachable"

step "sources: local, s3 (MinIO), sftp, ftp, webdav"
mkdir -p "$WORK/lroot"
"$BIN" source add lab --type s3 --endpoint "http://localhost:$MINIO_PORT" \
  --access-key minioadmin --secret-key minioadmin >/dev/null
"$BIN" source test lab | grep 'OK' >/dev/null || fail "lab source test"
# saved LOCAL source: exercises the local engine as a first-class source
"$BIN" source add xl --type local --root "$WORK/lroot" >/dev/null
"$BIN" source test xl | grep 'OK' >/dev/null || fail "xl source test"
# sftp:// and webdav:// URL shorthand, ftp via flags (mirrors e2e-remote)
"$BIN" source add xt "sftp://e2e:e2epass@127.0.0.1:${SFTP_PORT}/upload" >/dev/null
"$BIN" source add xf --type ftp --host 127.0.0.1 --port "$FTP_PORT" \
  --username e2e --password e2epass >/dev/null
"$BIN" source add xw "webdav://e2e:e2epass@127.0.0.1:${WEBDAV_PORT}/" >/dev/null
for s in xt xf xw; do "$BIN" source test "$s" | grep 'OK' >/dev/null || fail "$s source test"; done
"$BIN" mb "s3://$BUCKET" >/dev/null || fail "mb"
pass "five sources online (lab/xl/xt/xf/xw)"

step "seed tree"
# Multi-shape tree: root files, nested dirs, a space, unicode, an empty
# file — every axis a transfer can silently flatten.
SEED="$WORK/tree"
mkdir -p "$SEED/docs/nested" "$SEED/logs"
printf 'cross e2e readme\n'                     > "$SEED/readme.md"
printf 'root file one\n'                        > "$SEED/root-1.txt"
printf 'docs alpha\n'                           > "$SEED/docs/a.md"
printf 'space in the name\n'                    > "$SEED/docs/b with space.txt"
printf 'unicode åäö in file and content åäö\n'  > "$SEED/docs/uni-åäö.txt"
printf 'deep file\n'                            > "$SEED/docs/nested/deep-file.txt"
: > "$SEED/docs/empty.txt"
printf 'log one\n'                              > "$SEED/logs/l1.log"
printf 'log two\n'                              > "$SEED/logs/l2.log"
canon "$SEED" "$WORK/canon-seed"
n="$(wc -l < "$WORK/canon-seed")"
[ "$n" = "9" ] || fail "seed canon has $n rows, want 9"
# seed every remote side (raw local path as the cp source — the everyday
# form); the saved local source carries the same tree.
"$BIN" cp -r "$SEED" "s3://$BUCKET/tree" >/dev/null || fail "seed s3"
"$BIN" cp -r "$SEED" "xt://$RUN/tree"  >/dev/null || fail "seed sftp"
"$BIN" cp -r "$SEED" "xf://$RUN/tree"  >/dev/null || fail "seed ftp"
"$BIN" cp -r "$SEED" "xw://$RUN/tree"  >/dev/null || fail "seed webdav"
"$BIN" cp -r "$SEED" "xl://$RUN/tree"  >/dev/null || fail "seed local source"
# every seed must itself match the local canon — a broken seed must fail
# here, not deep inside the matrix
tree_diff "seed s3"     "$SEED" "s3://$BUCKET/tree"
tree_diff "seed sftp"   "$SEED" "xt://$RUN/tree"
tree_diff "seed ftp"    "$SEED" "xf://$RUN/tree"
tree_diff "seed webdav" "$SEED" "xw://$RUN/tree"
tree_diff "seed local"  "$SEED" "xl://$RUN/tree"
pass "seed tree (9 files, nested/space/unicode/empty) on every source"

step "any→any matrix: cp -r + tree-diff"
# srcs[i] / dsts[i] are the SAME source at position i; cell (i,j) copies
# srcs[i]'s tree to a fresh dir on dsts[j]. dsts[] entries are URI bases
# (name:// or s3://bucket/) so the cell target is dsts[j]+out-N. s3prof[i]
# is the --profile value bare s3:// operands need once a second S3
# profile exists.
srcs=( "xl://$RUN/tree" "s3://$BUCKET/tree" "xt://$RUN/tree" "xf://$RUN/tree" "xw://$RUN/tree" )
dsts=( "xl://"         "s3://$BUCKET/"    "xt://"        "xf://"        "xw://" )
s3prof=( "" "lab" "" "" "" )
labels=( local s3 sftp ftp webdav )
cell=0
for i in 0 1 2 3 4; do
  for j in 0 1 2 3 4; do
    cell=$((cell + 1))
    dst="${dsts[$j]}$RUN/out-$cell"
    "$BIN" cp -r "${srcs[$i]}" "$dst" ${s3prof[$i]:+"--profile" "${s3prof[$i]}"} >/dev/null \
      || fail "cp ${labels[$i]} -> ${labels[$j]}"
    tree_diff "cell ${labels[$i]} -> ${labels[$j]}" "${srcs[$i]}" "$dst" "${s3prof[$i]}" "${s3prof[$j]}"
  done
done
pass "25 any→any cells reproduce the exact tree (paths + sizes)"

step "byte fidelity round-trips"
for i in 1 2 3 4; do
  rt="$WORK/rt-${labels[$i]}"
  "$BIN" cp -r "${srcs[$i]}" "$rt" ${s3prof[$i]:+"--profile" "${s3prof[$i]}"} >/dev/null \
    || fail "download ${labels[$i]}"
  diff -r "$SEED" "$rt" >/dev/null || fail "round-trip bytes differ: ${labels[$i]}"
done
pass "s3/sftp/ftp/webdav → local round-trips byte-identical"

step "mv semantics"
# remote → remote (cross-engine): dest matches, source gone
"$BIN" cp -r "$SEED" "xt://$RUN/mv-a" >/dev/null
"$BIN" mv -r "xt://$RUN/mv-a" "xf://$RUN/mv-b" >/dev/null || fail "mv sftp -> ftp"
tree_diff "mv sftp -> ftp" "$SEED" "xf://$RUN/mv-b"
expect_fail "$BIN" stat "xt://$RUN/mv-a"
# s3 → saved local source (uses the local→s3 matrix cell's output as the
# source so the canonical s3://BUCKET/tree stays available for hetzner;
# cell 2 = local→s3)
"$BIN" mv -r "s3://$BUCKET/$RUN/out-2" "xl://$RUN/mv-from-s3" >/dev/null || fail "mv s3 -> local source"
tree_diff "mv s3 -> local" "$SEED" "xl://$RUN/mv-from-s3"
expect_fail "$BIN" stat "s3://$BUCKET/$RUN/out-2/readme.md"
# same-engine rename
"$BIN" mv "xf://$RUN/mv-b/readme.md" "xf://$RUN/mv-b/renamed.md" >/dev/null || fail "rename in ftp"
"$BIN" stat "xf://$RUN/mv-b/renamed.md" | grep -q 'renamed.md' || fail "renamed stat"
expect_fail "$BIN" stat "xf://$RUN/mv-b/readme.md"
pass "mv moves trees (not copies), rename within an engine"

step "version preservation (cp --versions, plain cp collapse, mv --versions)"
"$BIN" mb "s3://$V1" >/dev/null; "$BIN" bucket versioning "s3://$V1" on >/dev/null
"$BIN" mb "s3://$V2" >/dev/null; "$BIN" bucket versioning "s3://$V2" on >/dev/null
# hist.txt: 3 versions; hist2.txt: 2 versions + a delete marker
for i in 1 2 3; do printf 'hist v%d\n' "$i" > "$WORK/h.txt"; "$BIN" cp "$WORK/h.txt" "s3://$V1/hist.txt" >/dev/null; done
for i in 1 2;     do printf 'hist2 v%d\n' "$i" > "$WORK/h.txt"; "$BIN" cp "$WORK/h.txt" "s3://$V1/hist2.txt" >/dev/null; done
"$BIN" rm "s3://$V1/hist2.txt" >/dev/null   # versioned delete → marker
# vcount counts NON-marker versions (delete markers carry a versionId of
# their own, so a raw versionId grep would count them as data). The JSON
# rows print versionId before isDeleteMarker — awk pairs the lines.
# mcount counts delete markers.
vcount() {
  "$BIN" versions ls "$1" --json ${2:+"--profile" "$2"} 2>/dev/null | LC_ALL=C awk '
    /"versionId":/ { v=$0; sub(/.*"versionId": *"/, "", v); sub(/".*/, "", v); pend=(v != "") }
    /"isDeleteMarker":/ { d=$0; sub(/.*"isDeleteMarker": */, "", d); sub(/[},].*/, "", d);
      if (pend && d != "true") n++; pend=0 }
    END { print n+0 }'
}
mcount() { "$BIN" versions ls "$1" --json ${2:+"--profile" "$2"} 2>/dev/null | grep -c '"isDeleteMarker": true' || true; }
[ "$(vcount "s3://$V1/hist.txt")"  = "3" ] || fail "V1 hist.txt timeline"
[ "$(vcount "s3://$V1/hist2.txt")" = "2" ] || fail "V1 hist2.txt timeline"
[ "$(mcount "s3://$V1/hist2.txt")" = "1" ] || fail "V1 delete marker"
# --versions recreates the timeline (hist2 stays deleted at the destination)
"$BIN" cp --versions "s3://$V1/" "s3://$V2/v/" -r >/dev/null || fail "cp --versions"
[ "$(vcount "s3://$V2/v/hist.txt")"  = "3" ] || fail "V2 timeline not preserved"
[ "$(vcount "s3://$V2/v/hist2.txt")" = "2" ] || fail "V2 hist2 timeline"
[ "$(mcount "s3://$V2/v/hist2.txt")" = "1" ] || fail "delete marker not recreated"
# plain cp collapses: latest versions only, deleted key skipped entirely
"$BIN" cp -r "s3://$V1/" "s3://$V2/plain/" >/dev/null || fail "plain cp"
[ "$(vcount "s3://$V2/plain/hist.txt")" = "1" ] || fail "plain cp did not collapse"
if "$BIN" versions ls "s3://$V2/plain/hist2.txt" --json | grep -q '"versionId"'; then
  fail "plain cp resurrected a deleted key"
fi
# content check: the collapsed latest must be v3
"$BIN" cp "s3://$V2/plain/hist.txt" "$WORK/latest.txt" >/dev/null
printf 'hist v3\n' > "$WORK/want.txt"
cmp -s "$WORK/latest.txt" "$WORK/want.txt" || fail "latest version is not v3"
# mv --versions moves the timeline and purges the source
"$BIN" mv --versions "s3://$V2/v/hist.txt" "s3://$V2/gone.txt" >/dev/null || fail "mv --versions"
[ "$(vcount "s3://$V2/gone.txt")" = "3" ] || fail "moved timeline"
[ "$(vcount "s3://$V2/v/hist.txt")" = "0" ] || fail "source timeline not purged"
pass "timelines preserved (incl. delete markers), plain cp collapses, mv purges"

step "version preservation across providers (Hetzner, env-gated)"
if [ -n "${S3B_HETZNER_ACCESS_KEY:-}" ] && [ -n "${S3B_HETZNER_SECRET_KEY:-}" ]; then
  HENDPOINT="${S3B_HETZNER_ENDPOINT:-https://fsn1.yourobjectstorage.com}"
  HBUCKET="${S3B_HETZNER_BUCKET:-s3b-e2e-cross-$RANDOM$RANDOM}"
  "$BIN" source add xh --type s3 --endpoint "$HENDPOINT" \
    --access-key "$S3B_HETZNER_ACCESS_KEY" --secret-key "$S3B_HETZNER_SECRET_KEY" >/dev/null
  "$BIN" source test xh | grep 'OK' >/dev/null || fail "hetzner source test"
  "$BIN" mb "s3://$HBUCKET" --profile xh >/dev/null || fail "mb hetzner"
  "$BIN" bucket versioning "s3://$HBUCKET" on --profile xh >/dev/null || fail "hetzner versioning"
  "$BIN" cp -r "$SEED" "xh://$HBUCKET/$RUN/tree" >/dev/null || fail "seed hetzner"
  tree_diff "seed hetzner" "$SEED" "s3://$HBUCKET/$RUN/tree" "" "xh"
  # hetzner row + column of the matrix (bare s3:// operands pin --profile;
  # xh:// operands dial their own client). hetzner→local is the download
  # round-trip below; local→hetzner is the seed above.
  for j in 1 2 3 4; do
    cell=$((cell + 1))
    "$BIN" cp -r "xh://$HBUCKET/$RUN/tree" "${dsts[$j]}$RUN/h-$cell" ${s3prof[$j]:+"--profile" "${s3prof[$j]}"} >/dev/null \
      || fail "cp hetzner -> ${labels[$j]}"
    tree_diff "cell hetzner -> ${labels[$j]}" "s3://$HBUCKET/$RUN/tree" "${dsts[$j]}$RUN/h-$cell" "xh" "${s3prof[$j]}"
  done
  for i in 0 1 2 3 4; do
    cell=$((cell + 1))
    "$BIN" cp -r "${srcs[$i]}" "xh://$HBUCKET/$RUN/out-$cell" ${s3prof[$i]:+"--profile" "${s3prof[$i]}"} >/dev/null \
      || fail "cp ${labels[$i]} -> hetzner"
    tree_diff "cell ${labels[$i]} -> hetzner" "${srcs[$i]}" "s3://$HBUCKET/$RUN/out-$cell" "${s3prof[$i]}" "xh"
  done
  "$BIN" cp -r "xh://$HBUCKET/$RUN/tree" "$WORK/rt-hetz" >/dev/null || fail "download hetzner"
  diff -r "$SEED" "$WORK/rt-hetz" >/dev/null || fail "hetzner round-trip bytes"
  # streamed --versions out to hetzner and back: the copy-back proves the
  # remote timeline kept every version (countable on the MinIO side)
  "$BIN" cp --versions "s3://$V1/" "xh://$HBUCKET/$RUN/v/" -r --profile lab >/dev/null || fail "cp --versions -> hetzner"
  "$BIN" cp --versions "xh://$HBUCKET/$RUN/v/" "s3://$V1/back/" -r --profile lab >/dev/null || fail "cp --versions <- hetzner"
  [ "$(vcount "s3://$V1/back/hist.txt" lab)"  = "3" ] || fail "hetzner round-trip lost hist.txt versions"
  [ "$(vcount "s3://$V1/back/hist2.txt" lab)" = "2" ] || fail "hetzner round-trip lost hist2.txt versions"
  [ "$(mcount "s3://$V1/back/hist2.txt" lab)" = "1" ] || fail "hetzner round-trip lost the delete marker"
  "$BIN" rb "s3://$HBUCKET" --force --profile xh >/dev/null || fail "hetzner bucket purge"
  if "$BIN" ls --json --profile xh | grep -q "$HBUCKET"; then fail "hetzner bucket still visible"; fi
  "$BIN" source remove xh >/dev/null
  pass "hetzner: matrix row+column, byte round-trip, streamed --versions round-trip"
else
  printf 'SKIP  hetzner cells (set S3B_HETZNER_ACCESS_KEY + S3B_HETZNER_SECRET_KEY)\n'
fi

step "cleanup"
"$BIN" rb "s3://$BUCKET" --force --profile lab >/dev/null || fail "rb $BUCKET"
"$BIN" rb "s3://$V1" --force --profile lab >/dev/null || fail "rb V1"
"$BIN" rb "s3://$V2" --force --profile lab >/dev/null || fail "rb V2"
for s in lab xl xt xf xw; do "$BIN" source remove "$s" >/dev/null; done
if "$BIN" source list --json | grep -q '"name"'; then fail "sources left behind"; fi
pass "buckets purged (versioned included), sources removed"

printf '\nALL CROSS-SOURCE E2E CHECKS PASSED\n'
