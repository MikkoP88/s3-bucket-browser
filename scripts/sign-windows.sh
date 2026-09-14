#!/usr/bin/env bash
# Code-signs Windows PE files in place with osslsigncode (Authenticode,
# SHA-256, RFC3161 timestamp). Invoked by the release workflow only when
# the WINDOWS_CERT_B64 secret is configured — a missing or rejected
# certificate fails the job instead of silently shipping unsigned bytes.
#
#   env:  S3B_CERT_PFX   path to the PFX bundle
#         S3B_CERT_PASS  passphrase ("" for none)
#         S3B_TIMESTAMP_URL  optional; defaults to DigiCert's RFC3161
#   args: PE files (.exe) to sign in place
set -euo pipefail

pfx="${S3B_CERT_PFX:?S3B_CERT_PFX not set}"
pass="${S3B_CERT_PASS:-}"
ts="${S3B_TIMESTAMP_URL:-http://timestamp.digicert.com}"

command -v osslsigncode >/dev/null || { echo "sign-windows: osslsigncode not installed" >&2; exit 1; }
[ -f "$pfx" ] || { echo "sign-windows: certificate file missing: $pfx" >&2; exit 1; }
[ "$#" -ge 1 ] || { echo "usage: sign-windows.sh FILE [FILE...]" >&2; exit 1; }

for f in "$@"; do
  [ -f "$f" ] || { echo "sign-windows: no such file: $f" >&2; exit 1; }
  tmp="$f.signed"
  osslsigncode sign -pkcs12 "$pfx" -pass "$pass" \
    -n "S3 Bucket Browser" -i "https://github.com/MikkoP88/s3-bucket-browser" \
    -h sha256 -t "$ts" -in "$f" -out "$tmp"
  mv "$tmp" "$f"
  echo "sign-windows: signed $f"
done
