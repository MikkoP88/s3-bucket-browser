#!/usr/bin/env bash
# End-to-end test of the s3b remote-source engines against real servers:
# OpenSSH/SFTP on localhost:2222 and vsftpd/FTP on localhost:2121 (see the
# e2e-remote CI job, or locally:
#   docker run -d -p 2222:22 atmoz/sftp:latest e2e:e2epass:1001:100:upload
#   docker run -d -p 2121:21 -p 21000-21002:21000-21002 \
#     -e FTP_USER=e2e -e FTP_PASS=e2epass -e PASV_ADDRESS=127.0.0.1 \
#     -e PASV_MIN_PORT=21000 -e PASV_MAX_PORT=21002 fauria/vsftpd)
#
# The M9 source surface (add/test/list) and the M10.5 remote commands
# (ls/tree/du/stat/mkdir/cp/mv/rm through NAME:// URIs) run against the
# real servers: SSH and FTP handshakes, auth, PASV data connections, and
# the transfer matrix — local<->remote, same-engine spool copies, and
# cross-engine sftp<->ftp, including special-character filenames.
#
# The test never touches the user's profile store: S3B_CONFIG is pointed at a
# throwaway directory for the whole run.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
S3B_CONFIG="$WORK/config"
export S3B_CONFIG
trap 'rm -rf "$WORK"' EXIT

SFTP_PORT="${S3B_SFTP_PORT:-2222}"
FTP_PORT="${S3B_FTP_PORT:-2121}"

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
# expect_fail CMD... — the command must exit non-zero.
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

# Servers must be reachable before any assertion makes sense.
wait_tcp 127.0.0.1 "$SFTP_PORT" || fail "no SFTP server on port $SFTP_PORT"
wait_tcp 127.0.0.1 "$FTP_PORT" || fail "no FTP server on port $FTP_PORT"
pass "SFTP and FTP servers reachable"

step "sftp source: add + real connectivity test"
"$BIN" source add sftpbox --type sftp --host 127.0.0.1 --port "$SFTP_PORT" \
  --username e2e --password e2epass >/dev/null
"$BIN" source test sftpbox | grep -q 'OK' || fail "sftp source test"
"$BIN" source list | grep -q 'sftpbox' || fail "sftp source not listed"
pass "SFTP handshake + auth against real OpenSSH"

step "sftp source: wrong password is an honest failure"
"$BIN" source add sftpbad --type sftp --host 127.0.0.1 --port "$SFTP_PORT" \
  --username e2e --password wrongpass >/dev/null
expect_fail "$BIN" source test sftpbad
"$BIN" source remove sftpbad >/dev/null
pass "wrong credentials rejected"

step "ftp source: add + real connectivity test"
"$BIN" source add ftpbox --type ftp --host 127.0.0.1 --port "$FTP_PORT" \
  --username e2e --password e2epass >/dev/null
"$BIN" source test ftpbox | grep -q 'OK' || fail "ftp source test"
pass "FTP handshake + auth + PASV data connection against real vsftpd"

step "ftp source: wrong password is an honest failure"
"$BIN" source add ftpbad --type ftp --host 127.0.0.1 --port "$FTP_PORT" \
  --username e2e --password nope >/dev/null
expect_fail "$BIN" source test ftpbad
"$BIN" source remove ftpbad >/dev/null
pass "wrong credentials rejected"

step "local source: add + test over a real directory"
mkdir -p "$WORK/localroot/docs"
printf 'hello\n' > "$WORK/localroot/readme.md"
"$BIN" source add disk --type local --root "$WORK/localroot" >/dev/null
"$BIN" source test disk | grep -q 'OK' || fail "local source test"
pass "local engine lists a real directory"

step "unknown host fails fast"
"$BIN" source add dead --type sftp --host 127.0.0.1 --port 1 \
  --username u --password x >/dev/null
expect_fail "$BIN" source test dead
pass "unreachable host rejected"

step "remote command matrix: writable sources (URL shorthand live)"
# atmoz/sftp gives e2e a writable /upload inside the chroot; the sftp
# source is added with the sftp:// URL shorthand to prove it against a
# real server. The ftp source roots at the vsftpd login directory.
"$BIN" source add sftpw "sftp://e2e:e2epass@127.0.0.1:${SFTP_PORT}/upload" >/dev/null
"$BIN" source add ftpw --type ftp --host 127.0.0.1 --port "$FTP_PORT" \
  --username e2e --password e2epass >/dev/null
"$BIN" source test sftpw | grep -q 'OK' || fail "sftpw (URL shorthand) test"
"$BIN" source test ftpw | grep -q 'OK' || fail "ftpw test"
pass "writable sources online (sftp:// URL shorthand included)"

step "matrix: mkdir + cp local->remote + inspect (ls/tree/du/stat)"
mkdir -p "$WORK/seed/docs"
printf 'alpha contents\n' > "$WORK/seed/a.txt"
printf 'beta body\n' > "$WORK/seed/docs/b with space.txt"
printf 'unicode åäö\n' > "$WORK/seed/docs/uni-å.txt"

"$BIN" mkdir sftpw://e2e-matrix >/dev/null
"$BIN" cp "$WORK/seed/a.txt" sftpw://e2e-matrix/ >/dev/null
"$BIN" cp "$WORK/seed" sftpw://e2e-matrix/seed -r >/dev/null
"$BIN" ls sftpw://e2e-matrix | grep -q 'a.txt' || fail "ls: pushed file"
"$BIN" ls sftpw://e2e-matrix/seed -r | grep -q 'b with space.txt' || fail "ls -r: special chars"
"$BIN" tree sftpw://e2e-matrix | grep -q 'docs' || fail "tree"
"$BIN" du sftpw://e2e-matrix | grep -q '4 object(s)' || fail "du count"
"$BIN" stat sftpw://e2e-matrix/seed/docs/uni-å.txt | grep -q 'File' || fail "stat"
"$BIN" ls sftpw://e2e-matrix --json | grep -q '"name": "a.txt"' || fail "ls --json"
pass "mkdir/cp/ls/tree/du/stat over SFTP with special-char names"

step "matrix: cp remote->local round trip verifies bytes"
"$BIN" cp sftpw://e2e-matrix/seed "$WORK/out" -r >/dev/null
cmp -s "$WORK/seed/docs/b with space.txt" "$WORK/out/docs/b with space.txt" \
  || fail "round trip: special-char file differs"
cmp -s "$WORK/seed/docs/uni-å.txt" "$WORK/out/docs/uni-å.txt" \
  || fail "round trip: unicode file differs"
pass "bytes survive the local->remote->local round trip"

step "matrix: same-engine copy (temp spool) and mv"
"$BIN" cp sftpw://e2e-matrix/seed sftpw://e2e-matrix/mirror -r >/dev/null
"$BIN" ls sftpw://e2e-matrix/mirror -r | grep -q 'uni-å.txt' || fail "same-engine copy"
"$BIN" mv sftpw://e2e-matrix/a.txt sftpw://e2e-matrix/renamed.txt >/dev/null
"$BIN" stat sftpw://e2e-matrix/renamed.txt | grep -q 'renamed' || fail "mv stat"
expect_fail "$BIN" stat sftpw://e2e-matrix/a.txt
pass "same-engine copy + mv over one SFTP connection"

step "matrix: cross-engine transfer sftp <-> ftp"
# cp -r SRC DST lands the *contents* of SRC in DST (rsync-style), so
# seed's tree arrives directly under ftpw://matrix.
"$BIN" cp sftpw://e2e-matrix/seed ftpw://matrix -r >/dev/null
"$BIN" ls ftpw://matrix -r | grep -q 'docs/b with space.txt' || fail "ftp: special chars"
"$BIN" cp ftpw://matrix sftpw://e2e-matrix/back -r >/dev/null
"$BIN" ls sftpw://e2e-matrix/back -r | grep -q 'uni-å.txt' || fail "ftp->sftp copy"
pass "cross-engine transfers stream between engines"

step "matrix: rm guards and tree removal"
expect_fail "$BIN" rm sftpw://e2e-matrix/seed
"$BIN" rm sftpw://e2e-matrix/renamed.txt >/dev/null
expect_fail "$BIN" stat sftpw://e2e-matrix/renamed.txt
"$BIN" rm sftpw://e2e-matrix -r >/dev/null
expect_fail "$BIN" stat sftpw://e2e-matrix
"$BIN" rm ftpw://matrix -r >/dev/null
# (vsftpd's LIST of a missing dir succeeds with an empty listing, so
# absence is asserted through stat's not-exist mapping, not ls.)
expect_fail "$BIN" stat ftpw://matrix
pass "rm refuses folders without -r, removes trees on both engines"

step "cleanup"
"$BIN" source remove sftpbox >/dev/null
"$BIN" source remove ftpbox >/dev/null
"$BIN" source remove disk >/dev/null
"$BIN" source remove dead >/dev/null
"$BIN" source remove sftpw >/dev/null
"$BIN" source remove ftpw >/dev/null
if "$BIN" source list --json | grep -q '"name"'; then fail "sources left behind"; fi
pass "all sources removed"

printf '\nALL REMOTE E2E CHECKS PASSED\n'
