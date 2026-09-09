# S3 Bucket Browser — Master Plan

| | |
|---|---|
| **Project** | s3-bucket-browser — visual desktop app + CLI for managing S3 buckets and objects |
| **Repo** | https://github.com/MikkoP88/s3-bucket-browser |
| **Binary name** | `s3b` (GUI + CLI in one binary) |
| **License** | MIT |
| **Plan version** | 1.2 (2026-09-09) |
| **Derived from** | [s3-bucket-tester](https://github.com/MikkoP88/s3-bucket-tester) (MIT) — read-only source base; provider knowledge, signing, diagnostics and error catalog are ported, not copied blindly |
| **Status** | M1–M4 shipped (core CLI+GUI, dual-pane WinSCP parity, bucket administration, versioning). Remaining: deep search, storage-class conversion, object lock, hardening & packaging (M5), v1.0 launch (M6) |

---

## 1. Vision

**One small, fast, native-feeling application that lets anyone — from a beginner to an S3 storage administrator — do *everything* S3 offers, with the same ease as using Windows File Explorer.**

- It looks and behaves like Windows File Explorer: tree, list, columns, multi-select, drag & drop, context menus, keyboard-first navigation.
- Every action available in the GUI is available from the CLI, and vice versa. One binary: run it with no arguments to get the GUI, run it with arguments to get the CLI.
- It speaks *every* S3 dialect: AWS and all S3-compatible providers (MinIO, Ceph, Cloudflare R2, Backblaze B2, Wasabi, DigitalOcean Spaces, IBM COS, Hetzner, …) — and it knows each provider's quirks and warns before you hit them.
- It is MIT-licensed, dependency-minimal, telemetry-free, and cross-platform (Windows, macOS, Linux; amd64 + arm64).

### Success criteria

1. A first-time user can add credentials and upload a folder by dragging it into the window in under 60 seconds, without reading docs.
2. An administrator can empty a *versioned* bucket (all versions + delete markers) safely, with counts and a typed confirmation, faster than any competing tool.
3. `s3b ls/cp/rm/sync` in scripts feel as trustworthy as `aws s3` but with saner defaults and JSON output.
4. Browsing a bucket with 1,000,000 objects stays responsive (virtualized, streaming, cancelable).
5. Zero telemetry, zero accounts, zero cloud dependency. The app talks only to the endpoints you configure.

---

## 2. Target users

| Persona | Needs |
|---|---|
| **Beginner bucket user** | Add account, browse, upload/download via drag & drop, generate a share link. Must never be surprised by jargon. |
| **Power user / developer** | Multi-account profiles, prefix-flat browsing, sync, storage classes, metadata editing, presigned URLs, CLI for scripting. |
| **S3 storage administrator** | Bucket lifecycle (create → configure → audit → destroy): versioning, policies, ACLs, CORS, lifecycle rules, encryption, object lock, public-access block, connection diagnostics, force operations. |
| **Automation/DevOps** | CLI parity, stable JSON output, documented exit codes, shell completions. |

---

## 3. Competitive landscape & gap analysis

> Research method note (2026-09-09): built-in web-search quota was exhausted; competitor facts below were verified where possible by direct fetches (s3browser.com, cyberduck.io/s3) and GitHub API queries, combined with curated product knowledge. A refresh pass with live search is scheduled for when quota resets (2026-09-25) — tracked as a follow-up; conclusions are not expected to change materially.

| Tool | Platforms | License | S3 admin depth | Versioning UX | CLI | Main weakness |
|---|---|---|---|---|---|---|
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

GitHub landscape check (live, 2026-09): repositories matching "s3 browser gui / s3 file manager desktop" are either dead (2009-era `objc-s3`), tiny web download pages, or single-purpose Qt/web tools (`s3-bucket-diver`, React admin UIs). **No active, MIT, cross-platform, professional S3 desktop GUI+CLI exists.** That is the gap this project fills.

### The seven gaps we exploit

1. **Windows-Explorer-grade UX.** Nobody in this market has true Explorer semantics: marquee + ctrl/shift selection, keyboard-first operation, details view with sortable columns, breadcrumbs, context menus, clipboard (copy/cut/paste of objects between buckets).
2. **Versioning done right.** Per-object version timeline (like "Previous Versions" in file Properties), restore-as-latest, permanent delete of a specific version, bulk purge of versions + delete markers, one-click emptying of versioned buckets. The single most requested, worst-served S3 pain.
3. **One binary, GUI + CLI parity.** Cyberduck's `duck` and MSP360's CLI are afterthoughts or paid; we treat CLI as a first-class citizen with the same core engine.
4. **Provider-quirk intelligence.** Ported capability matrix from s3-bucket-tester: warn that Cloudflare R2 rejects path-style, MinIO ACLs are synthetic, B2 has no policy/ACL APIs, AWS path-style is deprecated — *before* the request fails.
5. **Connection doctor.** DNS → TCP → TLS → auth → policy/ACL checks with plain-language remediation (ported from s3-bucket-tester). No competitor diagnoses anything.
6. **Performance at scale.** Virtualized rendering + streaming ListObjectsV2 pagination + cancelable deep search. Most GUIs choke far below 100k objects.
7. **MIT + minimal deps + no telemetry.** Every rival is GPL, freeware, paid, or AGPL.

---

## 4. Product principles

1. **Explorer first.** If Windows Explorer does X, we do X — until S3 semantics genuinely differ (then we explain in one sentence).
2. **Safe by default, force when asked.** Destructive operations follow the safety ladder (§9). "Force" is always explicit, never a default.
3. **Beginner-readable, admin-capable.** Every property panel shows plain language plus the raw JSON/XML underneath.
4. **Provider-aware.** The capability matrix gates UI and generates warnings; unsupported actions are disabled, not hidden-failing.
5. **One engine, two faces.** GUI and CLI share `pkg/core`; no feature exists in only one face (except pure visual ones).
6. **Minimal dependencies.** Every new dependency requires justification in §7's budget. Prefer stdlib.
7. **Zero trust required.** No telemetry, no update pings, no accounts. Secrets stay in the OS keychain.

---

## 5. What we reuse from s3-bucket-tester (read-only source)

The base repository is treated as a read-only upstream: we port code into this repo with attribution, never push changes there.

| Base component (path) | Verdict | Destination |
|---|---|---|
| `pkg/config/config.go` — `ProviderCapabilitiesMap` (12 providers), `Providers` endpoint templates, `DetectProvider`, `generateProviderWarnings` | **Port nearly wholesale** | `pkg/provider/` — gates admin UI, drives warnings & doctor |
| `pkg/checker/policy.go` — policy/ACL fetch + parse, public-access detection (`AllUsers`/`AuthenticatedUsers`), `VerboseLogger`, XML/JSON beautifiers | **Port + adapt** | `pkg/core/policy/` — Security panel & `policy check` |
| `pkg/checker/auth.go`, `policy.go` — hand-rolled SigV4/SigV2 signers (two divergent copies) | **Consolidate one signer** | `pkg/core/sign/` — used by doctor's raw-request diagnostics & provider probing (day-to-day ops use the SDK) |
| `pkg/checker/{dns,tcp,tls}.go` — connectivity checkers | **Port** | `pkg/doctor/` — Connection Doctor |
| `pkg/remediation/suggestions.go` — S3 error taxonomy → cause/suggestion catalog (SignatureDoesNotMatch, NoSuchBucket, SlowDown, RequestTimeTooSkewed…) | **Port + extend** | `pkg/core/errhelp/` — user-facing error tooltips & CLI hints |
| `pkg/output/result.go` — JSON DTO shapes, exit-code contract (0/1/2/3) | **Adopt as convention** | `pkg/api/` DTOs, CLI exit codes |
| `pkg/checker/policy_test.go` — httptest mock-server tests | **Port** | Tests come along free |
| Hand-rolled flag parser (`pkg/config/flags.go`) | **Drop** | Replaced by cobra |
| Duplicated `ParseHostname`/`ParsePort`, dual signers, 3 broken test files, secrets echoed unmasked in JSON | **Drop / fix** | Single copy; secrets always masked |

---

## 6. Technology decisions

| Concern | Choice | Why |
|---|---|---|
| Language | **Go 1.22+** | Same language as the base repo (ports are cheap); single static binary; best-in-class cross-compilation; fast startup for CLI use. |
| GUI framework | **Wails v2** (OS webview: WebView2 / WKWebView / webkit2gtk) | Native webview — no bundled Chromium (~10–15 MB binaries, not 200 MB); Go backend reuses the core engine directly; full CSS/DOM control to replicate Explorer UX precisely; MIT. |
| Frontend | **Vanilla TypeScript-free JS (ES modules) + CSS** — no React/Vue/npm runtime deps | Minimal dependencies is a hard requirement; a file-manager UI is a solved problem with DOM + CSS Grid. Wails' vanilla template needs no bundler. |
| CLI | **cobra** (+ pflag) | Standard, MIT/Apache-2.0, generates help + shell completions; subcommand tree in §10. |
| S3 protocol | **aws-sdk-go-v2** (`config`, `credentials`, `service/s3`, `feature/s3/manager`) | Official, MIT, complete API surface (versioning, multipart, policies, CORS, lifecycle, object lock, presign); credential chain (env, shared config, SSO, IMDS); arbitrary endpoints + `UsePathStyle` for S3-compatible stores. Hand-rolling this surface would be reckless. |
| Secrets | **zalando/go-keyring** (Windows Credential Manager / macOS Keychain / libsecret), build-tag optional with encrypted-file fallback | Secrets never in plaintext config; never in logs or JSON output (fixes a base-repo flaw). |
| Terminal color | **fatih/color** (carried over from base) | Already proven there; MIT; Windows-safe. |
| Everything else | **Go stdlib** | HTTP, TLS, crypto, JSON/XML, context, embed. |

### Alternatives rejected

| Option | Why rejected |
|---|---|
| Electron | 150–250 MB footprint, Chromium + Node runtime — antithetical to "minimal dependencies". |
| Tauri | Excellent, but Rust backend would discard the Go base-repo assets and skill reuse. |
| Fyne / Gio | Pure-Go and appealing, but canvas-drawn widgets make pixel-faithful Explorer UX (sortable virtual tables, tree+list split, context menus, accessibility) far more expensive than DOM. |
| walk (Win32) | Windows-only, unmaintained. |
| Hand-rolled S3 client (extend base's signer) | Multipart, versioning APIs, checksums, retries, presigning — too much protocol surface to hand-maintain. The hand-rolled signer survives only for doctor-level raw diagnostics. |
| rclone as engine | Powerful sync, but AGPL-partials/GOPATH-weight and it would own the UX; we implement sync ourselves (~2k lines) and may later add an rclone config importer. |

### Dependency budget (all MIT or MIT-compatible; audited each release)

| Module | License | Role |
|---|---|---|
| `github.com/wailsapp/wails/v2` | MIT | GUI runtime |
| `github.com/aws/aws-sdk-go-v2/*` (config, credentials, service/s3, feature/s3/manager) | MIT/Apache-2.0 | S3 protocol |
| `github.com/spf13/cobra`, `spf13/pflag` | Apache-2.0 | CLI |
| `github.com/zalando/go-keyring` (+ ` Daniels/gnome-keyring` transitive) | MIT | OS keychain |
| `github.com/fatih/color` | MIT | Terminal colors |

npm runtime dependencies: **0**. (Dev-only: Wails CLI; optional esbuild for minification — never shipped.) Total transitive Go modules target: **≤ 40**. `go mod tidy` diff is reviewed in every release checklist; Dependabot weekly.

---

## 7. Architecture

### Layering

```
┌────────────────────────────────────────────────────────┐
│  frontend/            Wails webview (HTML/CSS/JS)      │  Explorer UI
├────────────────────────────────────────────────────────┤
│  pkg/api              DTOs + Wails-bound services      │  bridge (JSON events)
├────────────────────────────────────────────────────────┤
│  pkg/core             THE ENGINE (pure Go, no UI)      │
│   ├─ profile   accounts, endpoints, credential chain   │
│   ├─ s3client  thin wrapper over aws-sdk-go-v2         │
│   ├─ listing   streaming paginated lister, filters     │
│   ├─ transfer  upload/download queue, multipart, sync  │
│   ├─ bucketops bucket CRUD + config panels' ops        │
│   ├─ version   version list/restore/purge engine       │
│   ├─ policy    policy/ACL get-put + analyzer (ported)  │
│   ├─ errhelp   error → remediation catalog (ported)    │
│   └─ sign      consolidated SigV4/V2 (ported)          │
├────────────────────────────────────────────────────────┤
│  pkg/provider         capability matrix (ported)       │
│  pkg/doctor           dns/tcp/tls/auth/policy checks   │
├────────────────────────────────────────────────────────┤
│  cmd/s3b              entry: no args → GUI, args → CLI │
│   └─ cli              cobra command tree               │
└────────────────────────────────────────────────────────┘
```

Rules:
- `pkg/core` never imports Wails, cobra, or DOM — it is fully unit-testable and powers both faces.
- GUI ↔ core exclusively through `pkg/api` DTOs (JSON-tagged, mirroring base repo's `output/result.go` conventions) and Wails events (`transfer:progress`, `listing:page`, `doctor:result`).
- Progress/cancellation via `context.Context` everywhere.

### Repository layout

```
s3-bucket-browser/
├── PLAN.md  README.md  LICENSE  Makefile  go.mod
├── cmd/s3b/main.go            # dispatch: os.Args → GUI or CLI
├── internal/cli/…             # cobra commands (one file per group)
├── pkg/{api,core,provider,doctor}/…
├── frontend/                  # vanilla JS: index.html, app.js, ui/, styles/
├── build/                     # Wails platform packaging configs
├── scripts/                   # dev utilities
├── .github/workflows/ci.yml   # fmt+vet+lint+test+build matrix
└── docs/                      # screenshots, CLI reference, comparison page
```

---

## 8. Feature catalogue

Legend: **[M1]**…**[M6]** = milestone delivering the feature (§12). Everything below ships in v1.0 unless marked *later*.

### 8.1 Accounts & connections
- Profile manager: multiple named profiles; per-profile endpoint, region, addressing (virtual-host/path-style), signature version, provider preset — **[M1]**
- Credential sources: access-key pair, session token, env vars, shared AWS config/SSO chain, per-profile keyring secrets — **[M1]**
- **Connection Doctor**: ported DNS/TCP/TLS/auth/policy checks with pass/fail report + remediation text; GUI panel and `s3b doctor` — **[M3]** (CLI subset M1)
- Profile color-coding in the UI to prevent wrong-account operations — **[M2]**
- Import from `~/.aws/credentials` and shared config — **[M2]**

### 8.2 Browsing (Explorer core)
- Bucket list → virtual folder tree (delimiter-based), breadcrumbs, back/forward/up navigation history — **[M2]**
- Details view (Name, Size, Type, Storage Class, Last Modified, Version count, Tags badge) with per-column sort & remembered widths; Large-icons and List (compact) modes — **[M2]**
- Multi-select: click, Ctrl+click, Shift+click, Ctrl+A, marquee (drag rectangle) — **[M2]**
- Instant client-side filter (type-ahead in the "search box") + cancelable server-side deep prefix search — **[M2]** (deep search **[M4]**)
- Flat "show all objects" mode (no folder grouping) — **[M2]**
- Type-to-jump, status bar (counts, selection size), empty-state guidance — **[M2]**
- Total size / object count calculator per prefix (`du`) with progress — **[M3]**
- Favorites/recents for buckets and prefixes — **[M5]**

### 8.3 Transfer
- Upload: files & whole folders via drag & drop or file picker; multipart (>8 MB) with configurable part size & concurrency; pause/resume/cancel; per-file and aggregate progress; retry with exponential backoff — **[M2]** (resume **[M4]**)
- Upload options: storage class, SSE-S3/SSE-KMS/SSE-C, metadata & content-type, checksum verification (CRC32/SHA-256) — **[M2/M3]**
- Download: single/batch to a chosen folder; conflict policy (overwrite / skip / rename); queue manager dialog (like Windows copy dialog) with speed + ETA — **[M2]**
- Server-side copy/move between buckets & prefixes (CopyObject + delete; multipart copy for large) — **[M2]**
- Sync engine: `sync src dst [--delete] [--dry-run] [--size-only]` comparing size+mtime(etag) metadata; mirrored in GUI — **[M4]**
- Bandwidth throttle (global & per-transfer) — **[M5]**
- Transfer history log (JSONL, local) — **[M3]**

### 8.4 Object operations
- New "folder" (prefix marker), rename (F2 — server-side copy + delete, preserving metadata & versions note), delete (Del), permanent delete (Shift+Del on versioned objects) — **[M2/M4]**
- Properties dialog: size, etag, storage class, SSE, metadata editor, tags editor, ACL viewer, versions tab — **[M3]**
- Clipboard: Ctrl+C/Ctrl+X/Ctrl+V across buckets/profiles — **[M2]**
- Presigned URLs (GET/PUT) with expiry picker; copy to clipboard — **[M3]**
- Storage-class conversion (in-place via copy with new class) single & batch — **[M4]**
- Preview panel: images, text/code (with size cap), CSV as table, audio/video via `<audio>/<video>`, PDF via webview; "download to temp then open with OS app" for everything else — **[M3]**
- Batch rename? *later* (v1.x); zipped download of multi-selection *later* (needs zip streaming — tracked as feature request)

### 8.5 Bucket administration
- Create bucket (region picker, provider-aware), delete bucket, empty bucket (§9 ladder) — **[M2/M3]**
- Versioning: enable/suspend — **[M3]**
- Bucket policy: get/put/delete, JSON editor with syntax validation, **public-access analyzer** (ported AllUsers/AuthenticatedUsers detection) with warning banner — **[M3]**
- ACL viewer (provider-aware: MinIO synthetic, B2 none) — **[M3]**
- CORS rules editor — **[M3]**
- Lifecycle rules editor (transition/expiration, noncurrent versions, cleanup delete markers) — **[M3]**
- Default encryption (SSE-S3/SSE-KMS) — **[M3]**
- Public Access Block settings panel — **[M3]**
- Object Lock & retention (governance/compliance) — **[M4]**
- Static website hosting config + endpoint URL display — **[M3]**
- Bucket tagging; Requester Pays toggle; Transfer Acceleration toggle (AWS) — **[M4]**
- Replication rules viewer (*editor later*) — **[M4]**
- Notifications, CloudWatch metrics dashboards — *later* (explicit non-goal for v1.0, revisit post-1.0)

### 8.6 Versioning management (headline feature)
- Per-object **Versions tab** (Properties): timeline of versions + delete markers, newest-first; columns: version id, type, size, modified, storage class, is-latest — **[M4]**
- Actions per version: download, **restore-as-latest** (server-side copy), permanently delete — **[M4]**
- Bucket **Version Dashboard**: totals (versions, delete markers, noncurrent bytes estimate), largest noncurrent objects, purge tools — **[M4]**
- Bulk tools: *Delete all noncurrent versions*, *Delete all delete markers*, *Purge everything (force)* — §9 ladder level 2 — **[M4]**
- Delete marker UX: strikethrough entries, "undo delete" = remove delete marker — **[M4]**

### 8.7 Force & destructive operations (see §9 safety model)
- `rb --force`: empty (incl. all versions if versioned) then delete bucket — **[M3]**
- `rm --recursive --versions --force`: bulk delete incl. versions — **[M4]**
- Upload overwrite modes: overwrite (default), `--no-clobber`, `--if-match` conditional — **[M2/M4]**
- `--continue-on-error` batch mode with per-item report — **[M3]**
- `--insecure` TLS-skip for self-signed labs (from base, clearly warned) — **[M1]**
- Force addressing overrides (`--path-style`, `--virtual-hosted`) — **[M1]**

### 8.8 CLI (full tree in §10) — **[M1 core, grows with each milestone]**
- Same binary, same engine; `--json` machine output everywhere; exit codes 0/1/2/3 (base repo contract); shell completions; `NO_COLOR`/`--no-color`.

### 8.9 UX shell
- Light/dark theme (follows OS), compact/comfortable density, font size — **[M2]**
- Keyboard shortcut sheet (F1), tooltips with remediation hints on errors — **[M2/M3]**
- i18n scaffolding (en first; strings externalized from day one) — **[M2]**; translations *later*
- Accessibility: focus order, ARIA on the grid, high-contrast check — **[M5]**
- Settings persisted locally; portable mode (config next to binary) — **[M5]**

---

## 9. Force & destructive operations — safety ladder

| Level | Operations | Confirmation required |
|---|---|---|
| **L0** normal | Delete selection (<50 items), overwrite upload | Single dialog with item count; Enter defaults to safe action |
| **L1** large | Delete ≥50 items, empty prefix | Dialog lists exact counts + total size; must check "I understand" |
| **L2** bucket-wide | Empty bucket, delete bucket with contents, purge all versions, `rb --force` | **Type the bucket name**; shows version/delete-marker counts first |
| **L3** unrecoverable | Delete versions/delete markers permanently | L2 + explicit "permanent" checkbox; CLI requires both `--versions --force` |

Rules:
- Count-then-act: every bulk destructive op first *counts* (objects, versions, markers, bytes) and shows the numbers — counting is cancelable.
- Batched 1000 keys/delete request with rate handling (SlowDown → backoff), progress dialog, cancelable mid-run.
- All L1+ operations append to a local audit log (JSONL: who/what/when/counts) — answerable after the fact.
- CLI mirrors the ladder: `--force` gates L2/L3; without it the command prints what it *would* do and exits 2.
- Versioned buckets: plain "delete" always means *create delete marker* (with inline hint); permanence is never implicit.

---

## 10. CLI specification

```
s3b                              # launch GUI
s3b gui
s3b profile add|list|use|remove|test|export|import
s3b ls       s3://bucket[/prefix] [-l|--long] [-r|--recursive] [--json] [--flat]
s3b tree     s3://bucket[/prefix]
s3b du       s3://bucket[/prefix]           # sizes & counts
s3b stat     s3://bucket/key                # object metadata incl. versions summary
s3b mb       s3://bucket [--region R]
s3b rb       s3://bucket [--force]          # --force = empty first (L2)
s3b mkdir    s3://bucket/prefix/
s3b cp|mv    <src…> <dst>  [-r] [--storage-class C] [--sse …] [--checksum] [--no-clobber]
             # local<->s3 and s3<->s3; mv = cp + rm
s3b rm       s3://… [-r] [--versions] [--dry-run] [--force]
s3b sync     <src> <dst> [--delete] [--dry-run] [--size-only]
s3b version  list|download|restore|delete|purge   s3://bucket/key[@version]
s3b presign  s3://bucket/key [--get|--put] [--expires 1h]
s3b policy   get|put|rm|check   s3://bucket   # check = public-access analyzer
s3b acl      get|put            s3://bucket[/key]
s3b cors     get|put|rm         s3://bucket
s3b lifecycle get|put|rm        s3://bucket
s3b bucket   info|versioning enable|suspend|encryption|lock|tagging|website|location|block-public-access …
s3b doctor   [--profile P] [--json]           # ported checks + remediation
s3b completions bash|zsh|fish|powershell
```

Global flags: `--profile`, `--endpoint-url`, `--region`, `--path-style/--virtual-hosted`, `--auth sigv4|sigv2`, `--insecure`, `--timeout`, `--json`, `--no-color`, `--verbose`, `--config path`.

Exit codes (inherited contract from base repo): `0` success · `1` operation failure · `2` usage/config error (or blocked-by-safety) · `3` unexpected internal error.

JSON mode: single object per result row or `{"items":[…]}` envelope; stable field names (DTOs shared with GUI); secrets always masked.

---

## 11. Windows-Explorer UX specification (GUI)

### Layout
```
┌──────────────────────────────────────────────────────────────────┐
│ Toolbar: [Upload] [Download] [Sync ▾] [New Bucket] | view ▾ search│
│ Breadcrumb: profile ▸ bucket ▸ prefix ▸ …      [history ◀ ▶ ▲]   │
├───────────────┬──────────────────────────────────────────────────┤
│ Tree          │  Details grid (virtualized rows)                 │
│  profile ▾    │  Name │ Size │ Type │ Class │ Modified │ Versions│
│   bucket ▸    │  … rows …                                        │
│   bucket ▾    │                                                  │
│  Favorites    │                                                  │
├───────────────┴──────────────────────────────────────────────────┤
│ Status: 1,204 objects (3 selected, 128.4 MB) · transfer ▂▄▆ 2/5  │
└──────────────────────────────────────────────────────────────────┘
```

### Selection model (Explorer-faithful)
- Click select (single), Ctrl+click toggle, Shift+click range, Ctrl+A all, marquee drag-select; right-click on unselected item selects it first.
- Selection persists across view-mode switches; deselect on navigation with back-forward restore.

### Keyboard map
`Enter` open/preview · `F2` rename · `Del` delete · `Shift+Del` permanent (versioned) · `Ctrl+C/X/V` copy/cut/paste · `Ctrl+A` select all · `Ctrl+F` filter · `F3` deep search · `F5` refresh · `Alt+←/→` back/forward · `Alt+↑` / `Backspace` up · typing jumps to matching row · `Ctrl+Shift+N` new folder · `Ctrl+1/2/3` view modes · `Space` quick preview · `Esc` cancel · `F1` shortcut sheet.

### Drag & drop matrix
| Direction | Behavior |
|---|---|
| OS → app (file/folder) | Upload to the currently open prefix (hover a folder row = that folder); modifier none |
| app → OS | Download to drop target where the OS webview allows; fallback: Save-As dialog (drag-out is webview-limited; documented honestly) |
| Internal (rows → folder/bucket) | Default: **move** within same bucket (server-side copy+delete), **copy** across buckets/profiles; `Ctrl` forces copy, `Shift` forces move — Explorer convention |

### Context menus
Row: Open/Preview, Download, Copy/Share presigned URL, Cut/Copy/Paste, Rename, Delete, Properties (Versions tab), Change storage class, Restore previous version.
Bucket: Open, Empty bucket…, Delete bucket…, Properties (admin panels), Policy/ACL/CORS/Lifecycle/Encryption…, Version dashboard, Requester pays, Transfer acceleration.
Tree/background: New bucket, New folder, Paste, Refresh, Deep search here.

### Dialogs
Transfer manager (aggregated + per-file progress, pause/resume/cancel, speed/ETA) · Conflict policy picker · L0–L3 confirmations (§9) · Properties multi-tab (General/Versions/Metadata/Tags/Permissions) · Profile editor · Doctor report.

---

## 12. Milestones & acceptance criteria

| M | Scope (from §8) | Acceptance criteria |
|---|---|---|
| **M0** ✅ | Plan, skeleton, CI, docs structure | Repo exists, CI green, `go build ./...` passes |
| **M1** ✅ Core headless | `pkg/core` foundations: profile, s3client, listing, transfer, sign port, errhelp port, doctor port; CLI: profile, ls, tree, du, stat, mb, rb, cp, rm, presign, doctor | E2E against MinIO in CI: create profile, make bucket, upload 1k files, list, sync-dry-run, delete; JSON stable; exit codes honored |
| **M2** ✅ GUI foundations | Wails shell, Explorer layout, selection model, navigation, upload/download + drag&drop, transfer manager, clipboard ops, themes | Beginner flow test: drag a folder into window → uploads with progress; navigate with keyboard only; 100k-object bucket browses at 60 fps scroll |
| **M2.5** ✅ WinSCP parity | Dual-pane local browser (F9), synchronized browsing, cross-pane drag & drop, directory compare, open-in-external-editor with auto re-upload, transfer bandwidth throttle | Local+remote panes stay in lockstep; compare highlights newer/older/size-diff/only-here; edited files re-upload on save; throttle caps effective throughput |
| **M3** ✅ Administration | Bucket panels: versioning toggle, policy, CORS, lifecycle, encryption, website, tagging, public-access-block; doctor UI; du; transfer log | Admin can configure a bucket end-to-end (versioning→policy→CORS→lifecycle) and doctor explains a broken profile. Shipped as tabbed GUI panel + `s3b bucket …` CLI with full JSON; e2e-verified on MinIO (provider gaps surface as plain "not supported by this provider" errors) |
| **M4** ✅ Versioning & force | Versions timeline + restore-as-latest, undo-delete for markers, bulk purge (noncurrent/markers/all), version stats dashboard, version-aware `rb --force`, `rm --versions` | The classic benchmark: a versioned bucket emptied via GUI and `rb --force` with typed confirm (whole version history + markers purged, e2e-verified on MinIO); restore an old version in ≤3 clicks. *Deferred to M5: deep search, storage-class conversion, object lock* |
| **M5** Hardening & packaging | 1M-object performance pass, keyring, favorites, accessibility, i18n scaffolding, portable mode, installers (NSIS/MSI, dmg, AppImage+deb/rpm or tarballs), completions, bandwidth throttle ✅, deep search, storage-class conversion, object lock | 1M objects listed/searched within memory budget (<300 MB RSS) and responsive UI; signed installers for 3 OS; `s3b` on PATH with completions |
| **M6** v1.0 launch | Polish, docs site, CLI reference, comparison page vs §3 table, release notes | Public 1.0 announcement-ready; fresh-machine install test passes |

---

## 13. Performance engineering

- Listing: streaming `ListObjectsV2` paginator; pages pushed to UI as events; UI renders only visible rows (virtualization); memory cap via bounded page cache (LRU).
- 1M-object benchmark in CI (generated keyset against MinIO): navigate, filter, select-all, delete-batch dry-run.
- Transfers: worker pool (default 4×CPU), multipart 8–64 MB parts, adaptive retry (`SlowDown`/5xx backoff), checksum verify post-transfer.
- Startup: GUI cold start < 400 ms target (no splash, no update check).
- Profiling pprof endpoints behind `--dev` flag only.

## 14. Security

- Secrets in OS keyring (fallback: `AES-GCM` file with passphrase, never plaintext).
- Secret masking in **all** output incl. JSON and logs (`AKIA…ABCD` style).
- No telemetry, no outbound request except to user-configured endpoints; documented + testable via CI network-matrix.
- Presigned URL generation fully local (SDK presigner).
- Audit log for L1+ operations (§9).
- `--insecure` TLS bypass: loud warning in UI/CLI, never persisted as profile default.

## 15. Testing strategy

- **Unit** (table-driven): capability matrix, errhelp catalog, listing/pagination logic, sync differ, safety-ladder gating.
- **Protocol**: ported `httptest` mock-server suite (policy/ACL/signing) extended per operation.
- **Integration**: GitHub Actions service container running MinIO; full CLI E2E per milestone; AWS-only features (lifecycle nuances, object lock) tested against an AWS account in a manually-triggered workflow with ephemeral credentials.
- **GUI**: pure-logic JS unit tests for selection/sort models; manual test checklist per milestone; Playwright-against-webview investigated in M5.
- **Benchmarks**: listing & sync benchmarks with `go test -bench`, tracked in CI artifacts.

## 16. CI/CD & release

- CI (`.github/workflows/ci.yml`): gofmt, `go vet`, golangci-lint, `go test -race ./...`, build matrix (win/mac/linux × amd64/arm64).
- Release on `v*` tags: `go build` artifacts + checksums + Wails-packaged installers attached to a GitHub Release; SBOM (`syft`) + `go mod graph` dependency report per release (keeps the minimal-deps promise auditable).
- Branch model: `main` protected; feature branches; conventional commits.

## 17. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Wails v3 migration churn | Stay on stable v2; GUI bridge isolated in `pkg/api`; migration is bounded work |
| Webview drag-out to OS limited | Save-As fallback day one; document; revisit per-platform native hooks |
| Provider API drift | Capability matrix gates UI; SDK upgrade pinning + integration suite on MinIO + manual AWS suite |
| 1M-object memory blowups | Bounded caches, streaming everywhere, CI benchmark gate |
| Keyring absent on headless Linux | Encrypted-file fallback + `--no-keyring` mode |
| Scope creep (§8 is large) | Milestones are hard gates; §8 features tagged *later* are explicit non-goals for 1.0 |
| Solo-maintainer bus factor | Clean architecture, docs, conventional commits from day one |

## 18. Documentation & community

- `README.md` (feature overview, install, quickstart), `docs/cli.md` (generated from cobra), `docs/comparison.md` (the §3 table, kept honest), `docs/security.md`, `CONTRIBUTING.md` (M3+), `CHANGELOG.md`.
- Credit: provider knowledge, diagnostics, signing and error-remediation catalog ported from [s3-bucket-tester](https://github.com/MikkoP88/s3-bucket-tester) (MIT).

## 19. Explicit non-goals for v1.0

Drive mounting (leave to TntDrive/Mountain Duck/rclone mount) · non-S3 protocols (SFTP/FTP/WebDAV) · cloud KMS/IAM console features · team/collaboration features · mobile · CloudFront management · notification configuration UI · embedded web server mode.

---

*Appendix — research log (2026-09-09): s3browser.com (live: "CS Browser" 13.5.7 freeware/Pro, Windows-only, Netsdk FZE) · cyberduck.io/s3 (live: GPL, SigV4/V2 profiles, versioning/lifecycle/SSE, Mountain Duck separate paid) · GitHub API searches "s3 browser gui", "s3 file manager desktop", repo checks (nehbit/buckets → unavailable). Web-search quota exhausted at research time; refresh scheduled 2026-09-25.*
