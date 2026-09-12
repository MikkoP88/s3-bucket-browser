#!/usr/bin/env bash
# gen-notice.sh — emit a NOTICE file for distribution bundles (plan-v2 M10
# item 6): the project's own license line, every direct dependency with its
# SPDX id, and the full pinned module graph. Versions are read live from
# go.mod at release time; the release SBOM (docs/security.md) stays the
# authoritative license inventory for the transitive set.
#
# Usage: bash scripts/gen-notice.sh VERSION > NOTICE
set -eu

ver="${1:?usage: gen-notice.sh VERSION > NOTICE}"
main_mod="github.com/MikkoP88/s3-bucket-browser"

# license_for maps a module path to its SPDX id. Every direct dependency
# must have a mapping — an unmapped one fails the release build on purpose
# so the id gets looked at, not silently published as unknown.
license_for() {
  case "$1" in
    github.com/aws/*)           echo "Apache-2.0" ;;
    github.com/spf13/cobra)     echo "Apache-2.0" ;;
    github.com/fatih/color)     echo "MIT" ;;
    github.com/jlaffaye/ftp)    echo "ISC" ;;
    github.com/wailsapp/*)      echo "MIT" ;;
    github.com/zalando/go-keyring) echo "MIT" ;;
    github.com/pkg/sftp)        echo "BSD-2-Clause" ;;
    golang.org/x/*)             echo "BSD-3-Clause" ;;
    *)                          return 1 ;;
  esac
}

echo "S3 Bucket Browser (s3b) ${ver}"
echo "Copyright (c) 2026 Mikko Pesonen (MikkoP88)."
echo "Licensed under the PolyForm Internal Use License 1.0.0 — see LICENSE."
echo
echo "This product includes third-party software. Direct dependencies:"
echo

for dep in $(go list -m -f '{{if and (not .Indirect) (ne .Path "'"${main_mod}"'")}}{{.Path}}@{{.Version}}{{end}}' all); do
  mod="${dep%@*}"
  dv="${dep##*@}"
  if ! lic="$(license_for "${mod}")"; then
    echo "gen-notice.sh: no SPDX id mapped for direct dependency ${mod} — add it before releasing" >&2
    exit 1
  fi
  printf '  %-45s %-12s %s\n' "${mod}" "${dv}" "${lic}"
done

echo
echo "Complete pinned module graph as module@version (per-module license"
echo "declarations are inventoried in the release SBOM, spdx-json):"
echo
go list -m all | tail -n +2 | sed 's/^/  /'
