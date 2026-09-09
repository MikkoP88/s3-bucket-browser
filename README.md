# S3 Bucket Browser

**A Windows-Explorer-style desktop app + CLI for managing Amazon S3 and S3-compatible storage — buckets, objects, versions, and everything in between.**

> **Status: M0 — planning.** The full product plan, architecture, feature catalogue, CLI spec and milestones live in [PLAN.md](PLAN.md). Implementation starts at milestone M1.

## Why another S3 browser?

Because none of the existing ones do it all:

| | S3 Bucket Browser | S3 Browser (CS) | Cyberduck | MSP360 | AWS Console |
|---|---|---|---|---|---|
| Windows / macOS / Linux | planned | / – | / – | / – | browser |
| Open source (MIT) | yes | no | GPL | no | – |
| Explorer-style multi-select, drag & drop | yes (core goal) | partial | partial | partial | no |
| Versioning management (restore, purge, force-empty versioned buckets) | first-class | partial | partial | partial | clunky |
| GUI **and** CLI in one binary | yes | no | separate | no | – |
| Provider-quirk awareness (R2, MinIO, B2, Wasabi, …) | yes | minimal | profiles | minimal | AWS only |
| Connection doctor with fix suggestions | yes | no | no | no | no |
| Telemetry | **none** | – | – | – | – |

See [PLAN.md §3](PLAN.md#3-competitive-landscape--gap-analysis) for the full landscape and gap analysis.

## Design principles

1. Looks and behaves like **Windows File Explorer** (multi-select, drag & drop, context menus, keyboard-first).
2. **One binary, two faces**: run `s3b` with no arguments for the GUI, with arguments for the CLI — same engine, full parity.
3. **Safe by default, force when asked**: destructive operations follow a typed-confirmation safety ladder.
4. **Minimal dependencies**: Go + OS webview + official AWS SDK; zero npm runtime dependencies. MIT-licensed.
5. Speaks **every S3 dialect** — AWS, MinIO, Ceph, Cloudflare R2, Backblaze B2, Wasabi, DigitalOcean Spaces, IBM COS, Hetzner — and knows each provider's quirks.

## Relationship to s3-bucket-tester

This project builds on [s3-bucket-tester](https://github.com/MikkoP88/s3-bucket-tester) (MIT): its provider capability matrix, S3 diagnostics (DNS/TCP/TLS/auth/policy checks), SigV4 signing knowledge and error-remediation catalog are ported into this codebase with attribution. The upstream repository is treated as read-only source.

## License

[MIT](LICENSE) © 2026 Mikko Pesonen (MikkoP88) and S3 Bucket Browser Contributors.
