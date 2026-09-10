#!/usr/bin/env bash
# End-to-end test of the s3b remote-source engines against real servers:
# OpenSSH/SFTP on localhost:2222 and vsftpd/FTP on localhost:2121 (see the
# e2e-remote CI job, or locally:
#   docker run -d -p 2222:22 atmoz/sftp:latest e2e:e2epass:1001:100
#   docker run -d -p 2121:21 -p 21000-21002:21000-21002 \
#     -e FTP_USER=e2e -e FTP_PASS=e2epass -e PASV_ADDRESS=127.0.0.1 \
#     -e PASV_MIN_PORT=21000 -e PASV_MAX_PORT=21002 fauria/vsftpd)
#
# The M9 CLI surface is source add/test/list: `source test` performs a real
# dial + root listing, so this suite exercises the actual SSH and FTP
# handshakes, auth success and failure, and the local engine. Remote
# ls/cp commands extend this script when they land (CLI parity slice).
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

step "cleanup"
"$BIN" source remove sftpbox >/dev/null
"$BIN" source remove ftpbox >/dev/null
"$BIN" source remove disk >/dev/null
"$BIN" source remove dead >/dev/null
if "$BIN" source list --json | grep -q '"name"'; then fail "sources left behind"; fi
pass "all sources removed"

printf '\nALL REMOTE E2E CHECKS PASSED\n'
