#!/usr/bin/env bash
# Syntax-check every frontend JS module (node --check, ESM mode),
# then deep-validate the i18n dictionaries (key parity + placeholders).
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
node scripts/i18n-check.mjs || fail=1
# the visual harness is Node too (playwright-core drives it; the full run
# needs a browser and lives in CI's gui-visual job — here we syntax-check it)
node --check scripts/gui-visual.mjs && echo "OK   scripts/gui-visual.mjs" || fail=1
# same for the live harness (its full run needs credentials + a real bucket)
node --check scripts/gui-live.mjs && echo "OK   scripts/gui-live.mjs" || fail=1
exit $fail
