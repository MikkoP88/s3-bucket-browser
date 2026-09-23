#!/usr/bin/env bash
# Signs and notarizes the macOS release artifacts. Invoked by the release
# workflow; mirrors sign-windows.sh's contract — a missing certificate
# degrades to an ad-hoc signature (launchable once the user clears the
# download quarantine, see README "First launch on macOS"), while a
# configured certificate must succeed: a rejected import, failed signing
# or failed notarization fails the release instead of shipping an app
# Gatekeeper calls "damaged".
#
#   env:  MACOS_CERT_B64    base64-encoded Developer ID Application P12
#                            (optional; unset → ad-hoc mode)
#         MACOS_CERT_PASS   PFX passphrase ("" for none)
#         APPLE_ID          Apple ID for notarization (cert mode only)
#         APPLE_PASSWORD    app-specific password (cert mode only)
#         APPLE_TEAM_ID     team ID (cert mode only)
#   args: app <path.app>  sign (+verify) the app bundle
#         dmg <path.dmg>  sign + notarize + staple the dmg (cert mode)
set -euo pipefail

mode="${1:?usage: sign-macos.sh app <path.app> | dmg <path.dmg>}"
target="${2:?sign-macos.sh: missing target path}"
[ -f "$target" ] || [ -d "$target" ] || { echo "sign-macos: no such target: $target" >&2; exit 1; }
command -v codesign >/dev/null || { echo "sign-macos: codesign not available (run on macOS)" >&2; exit 1; }

KEYCHAIN="s3b-build-$$.keychain-db"
cleanup() {
  security delete-keychain "$KEYCHAIN" 2>/dev/null || true
  rm -f s3b-cert.p12
}
trap cleanup EXIT

# import_cert brings the Developer ID Application certificate into a
# throwaway keychain and echoes the signing identity. Call sites guard on
# MACOS_CERT_B64 first; any failure here (missing creds, rejected import,
# no identity) must abort the release — so callers must NOT run this
# inside an `if` condition, where set -e is suspended: capture it as a
# plain assignment.
import_cert() {
  command -v security >/dev/null || { echo "sign-macos: security not available" >&2; return 1; }
  for v in APPLE_ID APPLE_PASSWORD APPLE_TEAM_ID; do
    [ -n "${!v:-}" ] || { echo "sign-macos: MACOS_CERT_B64 is set but $v is not — notarization cannot run (see README)" >&2; return 1; }
  done
  printf '%s' "$MACOS_CERT_B64" | base64 -d > s3b-cert.p12
  security create-keychain -p "${MACOS_CERT_PASS:-}" "$KEYCHAIN"
  security unlock-keychain -p "${MACOS_CERT_PASS:-}" "$KEYCHAIN"
  security import s3b-cert.p12 -k "$KEYCHAIN" -P "${MACOS_CERT_PASS:-}" -T /usr/bin/codesign
  security set-key-partition-list -S apple-tool:,apple: -k "${MACOS_CERT_PASS:-}" "$KEYCHAIN" >/dev/null
  # awk reads the whole listing (no head/SIGPIPE under pipefail) and
  # extracts the first Developer ID Application identity.
  identity=$(security find-identity -v -p codesigning "$KEYCHAIN" |
    awk '/Developer ID Application/ && !found { sub(/.*"/, ""); sub(/".*/, ""); found = 1; print }')
  [ -n "$identity" ] || { echo "sign-macos: no Developer ID Application identity in the imported certificate" >&2; return 1; }
  printf '%s\n' "$identity"
}

case "$mode" in
app)
  if [ -n "${MACOS_CERT_B64:-}" ]; then
    identity="$(import_cert)"
    echo "sign-macos: signing bundle with Developer ID: $identity"
    codesign --force --options runtime --timestamp --sign "$identity" "$target"
  else
    # Ad-hoc: a valid signature Gatekeeper can at least verify — without
    # one, Apple Silicon kills the binary outright (arm64 requires a
    # signature), and a damaged signature shows as corruption.
    echo "sign-macos: MACOS_CERT_B64 not set — ad-hoc signing (unnotarized; README documents the quarantine step)"
    codesign --force --sign - "$target"
  fi
  codesign --verify --strict --verbose=2 "$target"
  # Gatekeeper verdict is informational: approved only in notarized mode.
  spctl -a -t exec --verbose "$target" 2>&1 || true
  ;;
dmg)
  if [ -n "${MACOS_CERT_B64:-}" ]; then
    identity="$(import_cert)"
    echo "sign-macos: signing + notarizing dmg"
    codesign --force --timestamp --sign "$identity" "$target"
    xcrun notarytool submit "$target" --apple-id "$APPLE_ID" --password "$APPLE_PASSWORD" --team-id "$APPLE_TEAM_ID" --wait
    xcrun stapler staple "$target"
    xcrun stapler validate "$target"
    echo "sign-macos: dmg notarized + stapled"
  else
    echo "sign-macos: MACOS_CERT_B64 not set — shipping unsigned dmg (unnotarized; README documents the quarantine step)"
  fi
  ;;
*)
  echo "sign-macos: unknown mode: $mode" >&2; exit 1 ;;
esac
