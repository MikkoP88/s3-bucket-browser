# S3 Bucket Browser vs. the field

> Research method note (2026-09-09): competitor facts were verified where
> possible by direct fetches (s3browser.com, cyberduck.io/s3) and GitHub API
> queries, combined with curated product knowledge. A refresh pass with live
> search is planned (tracked in PLAN.md §3); conclusions are not expected to
> change materially. Corrections to any row are welcome — this page is meant
> to stay honest, not to win.

| Tool | Platforms | License | S3 admin depth | Versioning UX | CLI | Main weakness |
|---|---|---|---|---|---|---|
| **S3 Bucket Browser (`s3b`)** | Win, macOS, Linux | PolyForm Internal Use (source-available) | Deep (policy, CORS, lifecycle, encryption, PAB, website, tags, object lock) | First-class (timeline, restore, undo delete, purge, force-empty versioned buckets) | Yes — same binary, same engine | 1.0: no code signing yet (SBOM + SHA256SUMS ship per release); no CloudFront/KMS consoles; single maintainer |
| **S3 Browser / "CS Browser" 13.x** (s3browser.com) | Windows only | Freeware; Pro paid | Deep (policy, ACL, CORS, lifecycle, CloudFront) | Yes, incl. delete versions | No | Windows-only, closed-source, dated UI |
| **Cyberduck** | Win, macOS | GPL-3.0 (copyleft) | Medium (versioning, lifecycle, logging, storage class, SSE) | Partial | `duck` (separate) | GPL, Java footprint, no Linux desktop, generic multi-protocol (S3 not first-class) |
| **MSP360 (CloudBerry) Explorer** | Win, macOS | Freemium (1 account free) | Medium-deep | Yes | No (separate paid) | Paywalls for sync/encryption/multi-account |
| **WinSCP** | Windows only | GPL | Low (transfer-focused) | No | Scripting | S3 is second-class; no bucket admin |
| **FileZilla Pro** | Win, macOS, Linux | Paid, closed | Medium | Partial | CLI (paid) | Not open source |
| **Transmit** | macOS only | Paid, closed | Medium | Partial | Yes | macOS only, paid |
| **Buckets** (Electron, MIT) | Win, macOS, Linux | MIT | Medium | Partial | No | Development stalled (repo now unavailable) |
| **Rclone (+ RcloneBrowser)** | CLI (+ web UI) | MIT | Config-level only | No | Excellent | GUI frontends unmaintained; not an admin console |
| **MinIO Console** | Web (bundled with server) | AGPL | Deep (MinIO-flavored) | Yes | N/A | Tied to MinIO deployments, AGPL, web-only |
| **AWS Console** | Web | — | Full | Clunky | — (use AWS CLI) | Slow on big buckets, noisy, poor multi-account, dangerous clicks |
| **AWS CLI** | CLI | Apache-2.0 | Full (raw) | Yes (jq gymnastics) | Excellent | No GUI; version purges are a known pain |

GitHub landscape check (2026-09): repositories matching "s3 browser gui / s3
file manager desktop" are either dead, tiny web download pages, or
single-purpose tools. **No active, source-available, cross-platform,
professional S3 desktop GUI+CLI other than this one.** That is the gap
this project fills.

## The seven gaps we exploit

1. **Windows-Explorer-grade UX.** True Explorer semantics: marquee +
   Ctrl/Shift selection, Ctrl+A, type-to-jump, keyboard-first operation,
   details view with sortable columns, breadcrumbs, folder tree, context
   menus — plus a WinSCP-style dual-pane local browser with directory
   compare.
2. **Versioning done right.** Per-object version timeline ("Previous
   Versions"), restore-as-latest, one-click undo delete for delete markers,
   permanent destroy of specific versions, bulk purge of noncurrent
   versions, and force-emptying of versioned buckets (markers included).
   The single most requested, worst-served S3 pain.
3. **One binary, GUI + CLI parity.** Cyberduck's `duck` and MSP360's CLI
   are afterthoughts or paid; here the CLI is a first-class citizen with
   the same core engine, JSON output everywhere and shell completions.
4. **Provider-quirk intelligence.** A capability matrix (ported from
   [s3-bucket-tester](https://github.com/MikkoP88/s3-bucket-tester)) warns
   that Cloudflare R2 rejects path-style, MinIO ACLs are synthetic, B2 has
   no policy/ACL APIs — before the request fails.
5. **Connection doctor.** DNS → TCP → TLS → auth → policy/ACL checks with
   plain-language remediation (`s3b doctor`). No competitor diagnoses
   anything.
6. **Performance at scale.** Streaming ListObjectsV2 pagination (the Go
   side holds one page at a time), virtualized rendering, cancelable deep
   search. Most GUIs choke far below 100k objects.
7. **Source-available + minimal deps + no telemetry.** Every rival is
   GPL, freeware, paid, AGPL or stalled. Zero npm runtime dependencies;
   the dependency budget is audited each release (PLAN.md §6).

## What s3b deliberately does not do (v1.0)

Drive mounting (TntDrive/Mountain Duck/rclone mount territory) · non-S3
protocols (SFTP/FTP/WebDAV) · cloud KMS/IAM console features ·
team/collaboration features · mobile · CloudFront management · notification
configuration UI · embedded web server mode. See PLAN.md §19.
