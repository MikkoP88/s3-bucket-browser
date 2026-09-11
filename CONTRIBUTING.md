# Contributing to S3 Bucket Browser

Thanks for helping! This file covers the short path from clone to a merged
PR. The product plan lives in [PLAN.md](PLAN.md) — architecture (§7),
safety model (§9) and non-goals (§19) explain most "why did they do it this
way" questions.

## Build & run

```bash
# Wails needs its tags for a working GUI (wails.io/docs/guides/manual-builds)
go build -tags desktop,production -o s3b ./cmd/s3b && ./s3b   # GUI (no args) + CLI (any arg)
go build -tags s3b_headless -o s3b ./cmd/s3b   # pure-Go CLI, no GTK deps (Linux CI)
go run ./tools/gendocs                    # regenerate docs/cli.md
```

Go 1.25+. The frontend is vanilla JS/CSS (no npm install, no bundler —
embedded via `go:embed`). Windows and macOS build out of the box; Linux
GUI builds need webkit2gtk (`sudo apt install libgtk-3-0 libwebkit2gtk-4.1-dev`).

## Before you push

```bash
gofmt -l . && go vet ./...
CGO_ENABLED=0 go test ./...       # hermetic: no network, no keyring, no S3
./scripts/js-check.sh              # frontend logic tests
```

CI (`.github/workflows/ci.yml`) runs the same plus `-race` on Linux, the
build matrix, an NSIS-compile check and a docs-freshness check. Optional,
needs Docker: `./scripts/e2e-minio.sh` runs the full end-to-end suite
against a local MinIO — it never touches your real profile store.

## Ground rules

1. **Hermetic tests.** Unit and protocol tests use `httptest` mock servers
   (see `pkg/core/listing/streaming_test.go`,
   `pkg/core/policy/policy_test.go`) and run with `S3B_NO_KEYRING=1`. No
   test may require network access, real cloud credentials, or a keyring.
   MinIO-based e2e is the only exception and lives behind the optional
   script above.
2. **Minimal dependencies (hard rule).** Runtime deps are MIT/Apache-2.0
   only and limited to the budget in PLAN.md §6. Zero npm runtime
   dependencies. If a PR needs a new module, justify it in the PR text —
   stdlib-first is the default answer.
3. **One engine, two faces.** Feature logic goes in `pkg/core` (pure Go);
   `pkg/api` binds it to the GUI, `internal/cli` to the CLI. No feature
   exists in only one face except pure visuals.
4. **Safety ladder is sacred.** Any destructive operation must count
   before acting, expose `--dry-run`, and gate on the ladder in §9. PRs
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
