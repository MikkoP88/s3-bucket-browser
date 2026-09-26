# Contributing to S3 Bucket Browser

Thanks for helping! This file covers the short path from clone to a merged
PR. The architecture is documented in code comments (start at
`cmd/s3b/main.go` and `pkg/api/app.go`); the safety model in
[docs/security.md](docs/security.md); release history in
[CHANGELOG.md](CHANGELOG.md).

## Build & run

```bash
# `production` strips Wails v3's devtools — the GUI is the default build
go build -tags production -o s3b ./cmd/s3b && ./s3b   # GUI (no args) + CLI (any arg)
go build -tags production,gtk3 -o s3b ./cmd/s3b  # Linux: GTK3/webkit2gtk 4.1 (Ubuntu 24.04)
go build -tags s3b_headless -o s3b ./cmd/s3b   # pure-Go CLI, no GTK deps (Linux CI)
go run ./tools/gendocs                    # regenerate docs/cli.md
node scripts/gen-icons.mjs             # re-derive build/iconset + frontend assets from build/icon.svg
go run ./tools/appicon                # re-assemble build/icon.ico + build/AppIcon.icns
```

Go 1.26+. The frontend is vanilla JS/CSS embedded via `go:embed`
(no npm install or bundler). Brand assets: `build/icon.svg` is the master
mark; regenerate and commit the PNG set, .ico and .icns together when it
changes.

On macOS, install Go and Xcode or Command Line Tools, then run `make mac`
and `open "dist/S3 Bucket Browser.app"`. `make mac-universal` builds both
architectures. See [the Mac build guide](docs/macos-build.md) for setup and
verification. `make build` produces the standalone host binary and applies
the Mac compiler flags too. GUI builds need cgo and Apple's SDK; there is
no Xcode project. `make build-all` is a collection of platform recipes,
not a portable cross-compiler: Linux GUI compilation requires a Linux
GTK/WebKit toolchain and macOS requires Apple's toolchain.

Linux GUI builds need GTK3/WebKitGTK 4.1 development packages
(`sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev`).

## Before you push

```bash
gofmt -l .
make test                       # host GUI flags + race detector
CGO_ENABLED=0 go test -tags s3b_headless ./... # CLI/core without GUI libraries
./scripts/js-check.sh              # frontend logic tests
```

CI (`.github/workflows/ci.yml`) runs the same plus `-race` on Linux, the
build matrix, an NSIS-compile check and a docs-freshness check. Optional,
needs Docker: `./scripts/e2e-minio.sh` runs the full end-to-end suite
against a local MinIO — it never touches your real profile store.

## Cutting a release

Releases are tag-driven: pushing a `v*` tag runs
[`.github/workflows/release.yml`](.github/workflows/release.yml), which
builds the linux/windows artifacts and the SBOM from the tagged
commit, signs the Windows binaries, and attaches everything to a GitHub
release.

The action-verification gate (the full matrix in
[docs/VERIFICATION.md](docs/VERIFICATION.md)) needs the live engine
containers and a browser, so it runs on the release machine as a **pre-tag
phase** — and the workflow refuses to publish a tag without its committed
report (**no report, no release**):

```bash
git checkout main && git pull              # the commit CI will build
node scripts/verify.mjs --release v1.2.3   # full gate, tag-stamped build;
                                           # writes docs/verification/v1.2.3/<os>-<arch>/
git add docs/verification                  # commit the evidence…
git commit -m "test: verification report for v1.2.3" && git push
git tag v1.2.3 && git push origin v1.2.3   # …then tag (the tag must contain the report)
```

`--release` stamps `main.version` exactly as CI does (the tag without the
leading `v`), runs every row from a fresh build — it refuses
`--only`/`--quick`/`--no-build`/`--skip-gui` — and writes the evidence:
`REPORT.md` (build + OS + the certificate) and `verification.json`, plus the
index at [`docs/verification/`](docs/verification/). The release job then
checks the report for the tag (full matrix, zero FAIL) and links it at the
top of the release notes.

The provenance guard runs alongside every suite: `node
scripts/provenance.mjs check` decodes the covert creator/license
watermarks from the nine marked files (see the Hidden provenance
markers row in [docs/VERIFICATION.md](docs/VERIFICATION.md)) and fails
if one was stripped or drifted — `go test ./internal/provenance/` is
the same guard on the Go side.

Also bump `CHANGELOG.md` for the new version (and `docs/security.md` if the
safety model moved).

## Ground rules

1. **Hermetic tests.** Unit and protocol tests use `httptest` mock servers
   (see `pkg/core/listing/streaming_test.go`,
   `pkg/core/policy/policy_test.go`) and run with `S3B_NO_KEYRING=1`. No
   test may require network access, real cloud credentials, or a keyring.
   MinIO-based e2e is the only exception and lives behind the optional
   script above.
2. **Minimal dependencies (hard rule).** Runtime deps are permissive-only
   (MIT/Apache-2.0/ISC/BSD) and audited each release
   (see [docs/security.md](docs/security.md)). Zero npm runtime
   dependencies. If a PR needs a new module, justify it in the PR text —
   stdlib-first is the default answer.
3. **One engine, two faces.** Feature logic goes in `pkg/core` (pure Go);
   `pkg/api` binds it to the GUI, `internal/cli` to the CLI. No feature
   exists in only one face except pure visuals.
4. **Safety ladder is sacred.** Any destructive operation must count
   before acting, expose `--dry-run`, and gate on the ladder in
   [docs/security.md](docs/security.md). PRs
   that loosen a gate need a very good reason in writing.
5. **Secrets stay masked.** Never log, echo or JSON-print a secret.
6. **Generated docs are generated.** After changing any command's flags or
   help text, run `go run ./tools/gendocs` — CI fails on a stale
   `docs/cli.md`.

## Conventions

- **Commits:** Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`,
  `refactor:`, `chore:`), imperative subject line, wrapped body.
- **Branches:** feature branches off `main`; keep PRs focused — one
  feature or one fix.
- **Go:** gofmt-clean, table-driven tests next to the code, errors
  wrapped with `%w` where callers may inspect them, `context.Context`
  first parameter for anything that does I/O.
- **Frontend:** ES modules, no framework, no transpile step; keep
  frontend logic in testable modules (see `scripts/js-check.sh`).
- **i18n:** user-visible frontend strings go through the i18n table
  (15 languages are built in; more via PR — `scripts/i18n-check.mjs`
  enforces key parity, placeholders and native names per language).

## Reporting bugs

Open an issue with: `s3b version`, OS, the provider (AWS/MinIO/R2/…), the
exact command or GUI action, and output with `--verbose` (secrets are
masked automatically). For connectivity mysteries, include
`s3b doctor <uri> --json` output.

## Relationship to s3-bucket-tester

[Upstream](https://github.com/MikkoP88/s3-bucket-tester) is a read-only
source: provider knowledge, diagnostics and the error-remediation catalog
were ported from it with attribution. Changes to those components happen
here, never upstream.

## Security issues

Do not open public issues for exploitable findings — see
[docs/security.md](docs/security.md#reporting-a-vulnerability).
