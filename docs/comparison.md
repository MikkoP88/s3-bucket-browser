# S3 Bucket Browser vs. the field

> Research method note (verified 2026-09-16): competitor facts below were
> re-checked by live fetches — vendor pages (s3browser.com, msp360.com,
> cyberduck.io) and the GitHub API — on the date shown. Facts that could
> not be re-verified from a primary source are marked unverified.
> Corrections to any row are welcome — this page is meant to stay honest,
> not to win.

| Tool | Platforms | License | S3 admin depth | Versioning UX | CLI | Main weakness |
|---|---|---|---|---|---|---|
| **S3 Bucket Browser (`s3b`)** | Win, macOS, Linux | PolyForm Internal Use (source-available) | Deep (policy, CORS, lifecycle, encryption, PAB, website, tags, object lock); SFTP/SCP, FTP/FTPS, WebDAV/WebDAVs and local sources ride the same UI and CLI | First-class (timeline, restore, undo delete, three-way marker / keep-current / permanent Delete Window, version- and marker-count badges, purge, force-empty versioned buckets) | Yes — same binary, same engine | Beta: Windows artifacts are Authenticode-signed with a self-signed "s3b Project" certificate (public key at scripts/certs/s3b-signing.cer; a CA cert drops in via repo secrets); SBOM + SHA256SUMS ship per release; no CloudFront/KMS consoles; single maintainer |
| **S3 Browser** 13.x — 13.5.7 current (s3browser.com) | Windows only | Freeware (personal accounts) + Pro paid | Deep (policy, ACL, CORS, lifecycle, CloudFront) | Yes, incl. delete versions | No | Windows-only, closed-source, dated UI |
| **Cyberduck** | Win, macOS | GPL-3.0 (copyleft) | Medium (versioning, lifecycle, logging, storage class, SSE) | Partial | `duck` (separate) | GPL, Java footprint, no Linux desktop, generic multi-protocol (S3 not first-class) |
| **MSP360 (CloudBerry) Explorer** — freeware + PRO tiers, PRO $59.99 (verified 2026-09-16) | Win, macOS | Freeware + PRO (paid) | Medium-deep | Yes | No | Paywalls for sync/encryption/multi-account; backup-vendor side project |
| **WinSCP** | Windows only | GPL | Low (transfer-focused) | No | Scripting | S3 is second-class; no bucket admin |
| **FileZilla Pro** | Win, macOS, Linux | Paid, closed | Medium | Partial | CLI (paid) | Not open source |
| **Transmit** | macOS only | Paid, closed | Medium | Partial | Yes | macOS only, paid |
| **Buckets** (Electron, MIT) | Win, macOS, Linux | MIT | Medium | Partial | No | Development stalled (repo now unavailable) |
| **Rclone (+ RcloneBrowser)** | CLI (+ web UI) | MIT | Config-level only | No | Excellent | GUI frontends unmaintained; not an admin console |
| **MinIO Console** | Web (bundled with MinIO server releases) | AGPL | Deep (MinIO-flavored) | Yes | N/A | Tied to MinIO deployments, AGPL, web-only — and the standalone console repository is gone from GitHub (404 as of 2026-09-16); older server releases still bundle it |
| **AWS Console** | Web | — | Full | Clunky | — (use AWS CLI) | Slow on big buckets, noisy, poor multi-account, dangerous clicks |
| **AWS CLI** | CLI | Apache-2.0 | Full (raw) | Yes (jq gymnastics) | Excellent | No GUI; version purges are a known pain |

GitHub landscape check (2026-09-16 refresh): the top repositories matching
"s3 browser / s3 explorer desktop" are still web download pages, single-page
bucket viewers (awslabs/aws-js-s3-explorer, qoomon/aws-s3-bucket-browser) or
security/bug-bounty tooling — not desktop file managers. The newest entrants
worth naming: **brows3** (rgcsekaraa/brows3, ~190★, TypeScript, active 2025+,
open-source desktop S3 client) and **BucketDock** (bucketdock, ~40★,
macOS-only native S3 browser). Both are early-stage and single-surface (no
CLI parity, no bucket administration, no S3 versioning workflow).
**No active, source-available, cross-platform, professional S3 desktop
GUI+CLI with versioning administration other than this one.** That is the
gap this project fills.

## The seven gaps we exploit

1. **Windows-Explorer-grade UX.** True Explorer semantics: marquee +
   Ctrl/Shift selection, Ctrl+A, Ctrl+I invert, type-to-jump,
   keyboard-first operation, details view with sortable and
   user-pickable columns, breadcrumbs, folder tree, context menus
   (copy-as name/path/URI included) — plus a WinSCP-style dual-pane
   local browser with directory compare, and a guarded exit that
   never silently drops running transfers or unsaved profile work.
2. **Versioning done right.** Per-object version timeline one click
   away (the row's version-count badge), folder-level Directory
   Versions overviews, restore-as-latest, one-click undo delete for
   delete markers (per-marker window included), a three-way marker /
   keep-current / permanent choice in a unified Delete Window,
   per-row version- and marker-count badges, permanent destroy of
   specific versions, bulk purge of noncurrent versions, and
   force-emptying of versioned buckets (markers included). The single
   most requested, worst-served S3 pain.
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
   the dependency budget is audited each release.

## What s3b deliberately does not do

Drive mounting (TntDrive/Mountain Duck/rclone mount territory) · SMB and
other legacy network-filesystem sources · cloud KMS/IAM console features ·
team/collaboration features · mobile · CloudFront management · notification
configuration UI · embedded web server mode.
