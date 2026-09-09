#!/usr/bin/env bash
# Syntax-check every frontend JS module (node --check, ESM mode).
set -u
fail=0
for f in frontend/js/*.js; do
  cp "$f" /tmp/s3b-js-check.mjs
  if node --check /tmp/s3b-js-check.mjs 2>/tmp/s3b-js-err; then
    echo "OK   $f"
  else
    echo "FAIL $f"
    cat /tmp/s3b-js-err
    fail=1
  fi
done
exit $fail
