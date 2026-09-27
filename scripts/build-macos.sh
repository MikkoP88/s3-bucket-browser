#!/usr/bin/env bash
# Local macOS build: no Wails CLI, Node, signing account or Xcode project.
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { echo "build-macos: $*" >&2; exit 1; }
[[ "$(uname -s)" == Darwin ]] || fail "run this script on macOS"
command -v go >/dev/null || fail "Go is missing; install it with: brew install go"
xcrun --find clang >/dev/null || fail "install Xcode Command Line Tools: xcode-select --install"
xcrun --show-sdk-path >/dev/null || fail "macOS SDK is missing; finish the Xcode installation"

arch="${1:-$(go env GOHOSTARCH)}"
case "$arch" in
  arm64|amd64) arches=("$arch") ;;
  universal) arches=(arm64 amd64) ;;
  *) fail "usage: bash scripts/build-macos.sh [arm64|amd64|universal]" ;;
esac

version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
version="${version#v}"
# Apple bundle versions must be numeric; retain the full version in the CLI.
bundle_version="$(printf '%s' "$version" | sed -nE 's/^([0-9]+\.[0-9]+\.[0-9]+).*/\1/p')"
bundle_version="${bundle_version:-0.0.0}"
app="dist/S3 Bucket Browser.app"
mkdir -p dist
stage="$(mktemp -d dist/.mac-build.XXXXXX)"
trap 'rm -rf "$stage"' EXIT
bundle="$stage/S3 Bucket Browser.app"
mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources"

export GOOS=darwin CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=13.0
export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=13.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=13.0 -framework UniformTypeIdentifiers"
for slice in "${arches[@]}"; do
  echo "Building macOS/$slice ($version) with $(go version)"
  GOARCH="$slice" go build -tags production -ldflags "-s -w -X main.version=$version" \
    -o "$stage/s3b-$slice" ./cmd/s3b
  xcrun vtool -show-build "$stage/s3b-$slice" | grep -Eq 'minos 13\.0$' || \
    fail "unexpected deployment target for $slice (expected macOS 13.0)"
done
if [[ "$arch" == universal ]]; then
  xcrun lipo -create "$stage/s3b-arm64" "$stage/s3b-amd64" -output "$bundle/Contents/MacOS/s3b"
else
  cp "$stage/s3b-$arch" "$bundle/Contents/MacOS/s3b"
fi
cp build/AppIcon.icns "$bundle/Contents/Resources/AppIcon.icns"
sed -e "s/__VERSION__/$bundle_version/g" -e 's/__MINOS__/13.0/g' \
  scripts/installer/Info.plist > "$bundle/Contents/Info.plist"
plutil -lint "$bundle/Contents/Info.plist"
# Local builds always use ad-hoc signing; release credentials are separate.
codesign --force --sign - "$bundle"
codesign --verify --strict --verbose=2 "$bundle"
xcrun vtool -show-build "$bundle/Contents/MacOS/s3b"

# Replace only this generated output after the new bundle is complete.
if [[ -e "$app" ]]; then mv "$app" "$stage/previous.app"; fi
mv "$bundle" "$app"
echo "Built $app"
echo 'Launch: open "dist/S3 Bucket Browser.app"'
