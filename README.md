# S3 Bucket Browser

**A Windows-Explorer-style desktop app + CLI for S3-compatible cloud storage and remote file servers — S3 buckets and objects, SFTP/SCP, FTP/FTPS, WebDAV and local folders — with first-class versioning, bucket administration and security.**

> **Status: v1.0 released; 1.1.0 in beta (current pre-release: 1.1.0-beta.14).** The project is in its Beta phase: core functionality is operational, but some features may exhibit partial functionality. 1.1 adds a unified data-source hierarchy, OS clipboard/drag interop, credential import, and a unified versioned-delete flow — see the [CHANGELOG](CHANGELOG.md).

![Main window](docs/screenshots/main-view.png)

*One binary, two faces: run `s3b` with no arguments for the GUI, with arguments for the CLI — same engine, full parity.*

## Why s3b

| | |
|---|---|
| **Easy to use** | True Windows-Explorer semantics: multi-select (Ctrl/Shift, Ctrl+A, Ctrl+I, marquee, type-to-jump), drag & drop everywhere, context menus, breadcrumbs, folder tree, sortable details grid with pickable columns, dual-pane local browser, keyboard-first operation (F1 shows the full map). A guarded exit never silently drops running transfers or unsaved profile work. |
| **Every source, one app** | S3-compatible (AWS, MinIO, Wasabi, Cloudflare R2, Backblaze B2, DigitalOcean Spaces, IBM COS, Hetzner, Ceph, Dell ECS, StorageGRID), SFTP/SCP, FTP/FTPS, WebDAV/WebDAVs and local folders — color-coded in one sidebar, browsable with the same UI and `NAME://` URIs on the CLI. |
| **Migration across sources** | Any-to-any transfers: drag rows between sources, panes or the tree, or `s3b cp s3://bucket/ vault://dst/ -r` on the terminal. Same-source S3 copies run server-side; cross-source copies stream through the same transfer manager with conflict pre-checks and throttling. |
| **S3 versioning done right** | Per-object timelines with restore-as-latest and text diffs, one-click undo delete for markers, a three-way marker / keep-current / permanent Delete Window, version- and marker-count badges, folder-level version overviews, bulk purge of noncurrent versions, force-emptying of versioned buckets. |
| **Import your credentials** | AWS shared files (`~/.aws/credentials` + `~/.aws/config`, `endpoint_url` entries become MinIO/R2/Wasabi/… sources), rclone, JSON, `.env`, encrypted `.s3bprofile` containers — or a KMS/secrets service (Vault, AWS Secrets Manager, Azure Key Vault, GCP), including fully custom HTTP endpoints. Live bucket-count test before you commit. |
| **Savable encrypted profiles** | Ctrl+S writes the whole workspace — every source and connection — into one password-encrypted `.s3bprofile` (scrypt + AES-256-GCM) you can reopen, keep or share. |
| **Security features** | Secrets in the OS keyring (Windows Credential Manager / macOS Keychain / Linux SecretService), masked everywhere, never logged; opt-in **Secure Storage** mode that seals the whole store as an AES-256-GCM envelope and hardens temp workspaces; **zero telemetry**. See [docs/security.md](docs/security.md). |
| **Fast at scale** | Streaming page-by-page listings (the Go side holds one page at a time), virtualized rendering, cancelable deep search — responsive on million-object buckets. |

More screenshots:

| | |
|---|---|
| ![Data sources](docs/screenshots/sources-tree.png) | ![Dual pane compare](docs/screenshots/dual-pane-compare.png) |
| ![Versions](docs/screenshots/versions.png) | ![Delete window](docs/screenshots/delete-window.png) |
| ![Admin panel](docs/screenshots/admin-panel.png) | ![Dark theme](docs/screenshots/dark-theme.png) |

The full tour with screenshots lives in the **[usage guide](docs/usage.md)**; the app carries the same guide (Help → User guide, F1).

## Supported operating systems & requirements

One binary per platform carries the GUI and the CLI together (`s3b` with no
arguments opens the desktop app; with arguments it is the CLI).

| Platform | Builds | Webview / runtime needed |
|---|---|---|
| **Windows 10/11** | amd64, arm64 | Microsoft Edge WebView2 (preinstalled on current Windows 10/11; a machine without it needs the free Evergreen runtime from Microsoft first) |
| **macOS 12 Monterey or later** (verified up to macOS 26 Tahoe) | amd64 (Intel), arm64 (Apple Silicon) | System WebKit — nothing to install |
| **Linux desktop** | amd64 | GTK3 + WebKitGTK 4.1 (what Ubuntu 24.04+, Mint and current Fedora ship) |
| **Linux servers / arm64** | amd64, arm64 | none — the `-tags s3b_headless` build is a pure-Go CLI with no GUI libraries |
| **Any OS, browser-driven** | `-tags server` | none on the host — the same stack runs windowless and serves the UI over HTTP |

The macOS floor is real, not aspirational: the release binaries carry a
pinned 12.0 deployment target (the oldest macOS the Go 1.26 runtime itself
runs on) and the release pipeline verifies it (`vtool`) before shipping.
Releases up to and including **v1.1.0-beta.13** were built without that
pin and accidentally required macOS 26 — if one of those told you "This
version cannot be used with this version of macOS", take v1.1.0-beta.14
or later.

- **To run**: the portable editions need no install and no admin rights —
  releases ship an NSIS installer (machine-wide, elevated) and portable
  zip (Windows), a DMG (macOS), and tarballs plus portable tarballs
  (Linux). Secrets use the OS keychain (Windows
  Credential Manager, macOS Keychain, Linux SecretService); keyring-less
  hosts fall back to a `0600` file.
- **To build from source**: Go **1.26+** and git only — the frontend is
  vanilla JS/CSS embedded via `go:embed` (no npm install, no bundler).
  Linux GUI builds additionally need `libgtk-3-dev` and
  `libwebkit2gtk-4.1-dev`; see [CONTRIBUTING.md](CONTRIBUTING.md).

## Quickstart (GUI)

```bash
# `production` strips Wails v3's devtools — the GUI is the default build
go build -tags production -o s3b ./cmd/s3b && ./s3b   # no arguments -> desktop app
# Ubuntu 24.04+ ships webkit2gtk 4.1, not the GTK4 webkit Wails v3 defaults
# to — build the GTK3 frontend there:
# go build -tags production,gtk3 -o s3b ./cmd/s3b
# Windows: link with the GUI subsystem for a native, console-flash-free
# launch (the CLI still prints normally — it re-attaches the parent
# terminal on demand; release builds use exactly this):
# go build -tags production -ldflags "-H windowsgui" -o s3b.exe ./cmd/s3b
```

- **Data sources**: color-coded connections in one sidebar hierarchy — S3 sources (account-wide, or bucket-scoped via `--bucket` / `s3b source add s3://bucket`), SFTP/SCP, FTP/FTPS servers, WebDAV/WebDAVs shares and local folders, all browsable with the same Explorer UI. **Import credentials** from files or KMS/secrets services — with a live bucket-count test; the very first import, straight from the welcome screen, opens the imported bucket's content.
- **Explorer layout**: toolbar, back/forward/up history, breadcrumb (type `source://bucket/prefix` to jump), folder tree sidebar, sortable details grid with per-column visibility, status bar, favorites.
- **Multi-select everything**: click / Ctrl+click / Shift+click / Ctrl+A / Ctrl+I (invert), marquee drag-select, type-to-jump, full keyboard map (F1 in-app).
- **Drag & drop + OS interop**: drop files or folders from the OS to upload; drag rows onto folders or the tree to move (same bucket) or copy (cross bucket); drag rows **out of the window** to Explorer, Finder or the desktop — a plain, unmodified drag hands the selection to the OS as real files (staged and streamed by the transfer engine), and a release back over the app is an internal move/copy (in the browser/server build, rows drag out as downloadable URLs instead); Ctrl+C mirrors the selection to the real OS clipboard and Ctrl+V uploads files copied in Explorer/Finder; **Copy name / Copy path / Copy S3 URI** put plain text on the clipboard.
- **Upload**: one **Upload ▸** flyout everywhere — **Files… (Ctrl+U)** for the native multi-select dialog, **Folder…** for a whole directory tree.
- **Transfer manager**: per-file and byte-level progress, speed, cancel — multipart and resumable, with an optional bandwidth throttle (256 kB/s … 10 MB/s) and conflict policies (overwrite / skip / rename) backed by a live pre-check that lists exactly which files collide.
- **Versions**: opt-in version-count badges (⟲ n, ⛔) open the object's timeline or the folder-level **Content Versions** overview; restore-as-latest, one-click **undo delete** for markers, permanent destroy and bulk purge — in the dialog or via Shift+Del.
- **Unified Delete Window**: deletes count first and act second — one dialog on every source; on versioned buckets it offers **add a delete marker** (default, everything restorable), **delete all except current version**, or **delete permanently**, with amber consequence lines and an optional typed `delete` gate.
- **Bucket administration**: versioning, policy, CORS, lifecycle, default encryption, public-access block, static website and tags in one tabbed admin panel (also `s3b bucket …`).
- **Deep search** (Ctrl+Shift+F): filter every object under a bucket/folder by name glob, size, age or storage class — streaming, cancelable, click-to-jump.
- **Storage class & object lock**: server-side class conversion from the context menu; per-object retention (GOVERNANCE/COMPLIANCE), legal hold and the bucket-level Lock tab.
- **Connection doctor**: DNS → TCP → TLS → auth → policy/ACL checks with plain-language remediation (`s3b doctor`).
- **Open in external editor**: edit remote files in your editor of choice; s3b watches for saves and re-uploads automatically.
- **Secure Storage** (Settings → Security): the shared-host hardening that encrypts the whole store, protects temp workspaces and auto-clears pre-signed URLs from the clipboard — see [docs/security.md](docs/security.md).
- **Light/dark theme**, 15 languages built in (English default, optional auto-detect), accessibility pass (ARIA roles, focus trap), portable mode (`s3b-portable` marker keeps config beside the binary).

Headless Linux servers can build a pure-Go CLI without GTK dependencies:

```bash
go build -tags s3b_headless -o s3b ./cmd/s3b
```

## Quickstart (CLI)

```bash
go build -o s3b ./cmd/s3b   # CLI-only works in any build; add -tags
                             # production if you want the GUI too

# Connect to any S3 provider (AWS, MinIO, Wasabi, R2, ...) — credentials
# also fall back to $S3B_ACCESS_KEY / $S3B_SECRET_KEY
s3b profile add lab --endpoint http://localhost:9000 \
    --access-key minioadmin --secret-key minioadmin --default
s3b profile test lab          # lightweight connectivity check
s3b doctor s3://my-bucket     # deep diagnosis: DNS → TCP → TLS → auth → policy/ACL

# Non-S3 sources live in the same store and use NAME:// URIs everywhere
s3b source add vault sftp://deploy@backups.example.com
s3b ls vault://media           # same engine, same flags as s3://

s3b ls                        # buckets          s3b ls s3://b/photos/    # folder view
s3b tree s3://b               # ASCII tree       s3b du s3://b/photos/    # size + count
s3b stat s3://b/photos/a.jpg  # object metadata
s3b mb s3://new-bucket        s3b mkdir s3://b/folder/

s3b cp report.pdf s3://b/docs/            # upload
s3b cp -r ./site s3://b/site/             # recursive upload
s3b cp s3://b/docs/report.pdf ./out/      # download
s3b cp s3://b/a.jpg s3://b/copy/a.jpg    # server-side copy
s3b cp -r s3://b/site/ vault://site/      # migrate S3 -> SFTP
s3b mv s3://b/old.txt s3://b/new.txt
s3b sync ./site s3://b/site/ --delete     # two-way safe sync
s3b presign s3://b/docs/report.pdf --expires 1h

s3b rm s3://b/tmp/file.txt                # single object
s3b rm -r --dry-run s3://b/tmp/           # preview a prefix delete
s3b rm -r --force s3://b/tmp/             # >50 objects requires --force
s3b rm -r --versions --force s3://b/tmp/  # destroy all versions too (L3)
s3b rb s3://old-bucket --force            # empty + remove (L2; purges
                                          #   version history if versioned)

# Object versions (versioned buckets)
s3b bucket versioning s3://b on           # enable versioning
s3b versions ls s3://b/docs/report.pdf    # timeline, newest first
s3b versions restore s3://b/docs/report.pdf --version-id ID
s3b versions undo s3://b/docs/report.pdf --version-id MARKER  # un-delete
s3b versions stat s3://b                  # current/noncurrent/marker stats
s3b versions purge s3://b --mode noncurrent --dry-run
s3b versions rm s3://b/docs/report.pdf --all               # permanent (L3)

# Bucket administration
s3b bucket info s3://b                    # region, versioning, encryption, PAB
s3b bucket versioning s3://b off
s3b bucket policy put s3://b policy.json  # also: cors | lifecycle |
s3b bucket tags put s3://b team=infra     #      encryption | pab | website

# Deep search: streams matches, cancelable
s3b find s3://b --name 'backup*'          # glob over the full key
s3b find s3://b/photos/ --larger 10MB --older 90d
s3b find s3://b --class GLACIER --limit 100

# Storage-class conversion: server-side self-copy
s3b sc s3://b/photos/a.jpg GLACIER        # single object
s3b sc s3://b/photos/ GLACIER -r --dry-run  # whole prefix; >50 needs --force

# Object lock: enable at bucket creation — permanent from then on
s3b mb s3://b --object-lock                   # the only moment lock can be enabled
s3b bucket lock s3://b --enable --mode GOVERNANCE --days 30   # default retention rule
s3b lock retention s3://b/report.pdf --mode GOVERNANCE --until +7d
s3b lock retention s3://b/report.pdf --clear --bypass-governance
s3b lock legalhold s3://b/report.pdf --on
```

Every command takes `--json` for machine-readable output, `--profile` to pick a connection, and `--verbose` for per-item detail. Shell completions: `s3b completion bash|zsh|fish|powershell`. Exit codes: `0` OK, `1` operation failure, `2` usage/config error, `3` unexpected.

**Safety ladder**: destructive operations count first and act second. Prefix deletes over 50 objects require `--force`, removing non-empty buckets requires `--force`, and the GUI routes every delete through the pre-counting Delete Window — with an optional typed `delete` gate in Settings for extra friction. Enabling object lock is permanent; COMPLIANCE retention cannot be shortened or removed.

## Install

Prebuilt artifacts are attached to every [`v*` release](../../releases): a Windows NSIS installer (`s3b-setup-x.y.z.exe`, registers an App Paths entry so Win+R `s3b` works without touching PATH), standalone zips/tarballs for Windows/Linux, and macOS dmg images — all checksummed in `SHA256SUMS`, with a dependency report and SBOM (SPDX-JSON) per release (see [docs/security.md](docs/security.md)). Or build from source as shown above; releases stamp the version into `s3b version`.

## Documentation

- **[Usage guide](docs/usage.md)** — the full walkthrough with screenshots (same content as the in-app guide)
- **[CLI reference](docs/cli.md)** — every command, generated from the cobra tree
- **[Security model](docs/security.md)** — keyring, Secure Storage, safety ladder, supply chain
- **[Verification reports](docs/verification/)** — per-release evidence: the build, the OS, and the full action-verification matrix behind every tag
- **[Competitive comparison](docs/comparison.md)** — the S3-browser landscape, fact-checked
- **[CHANGELOG](CHANGELOG.md)** · **[CONTRIBUTING](CONTRIBUTING.md)** · **[Portable edition](README-portable.md)**

## Why another S3 browser?

Because none of the existing ones do it all:

| | S3 Bucket Browser | S3 Browser (CS) | Cyberduck | MSP360 | AWS Console |
|---|---|---|---|---|---|
| Windows / macOS / Linux | yes | / – | / – | / – | browser |
| Source-available (PolyForm Internal Use) | yes | no | GPL | no | – |
| Explorer-style multi-select, drag & drop | yes (core goal) | partial | partial | partial | no |
| Versioning management (restore, purge, force-empty versioned buckets) | first-class | partial | partial | partial | clunky |
| GUI **and** CLI in one binary | yes | no | separate | no | – |
| Remote file servers (SFTP/FTP/WebDAV) in the same UI and CLI | yes | no | yes | no | – |
| Provider-quirk awareness (R2, MinIO, B2, Wasabi, …) | yes | minimal | profiles | minimal | AWS only |
| Connection doctor with fix suggestions | yes | no | no | no | no |
| Telemetry | **none** | – | – | – | – |

See [docs/comparison.md](docs/comparison.md) for the full landscape and gap analysis.

## Design principles

1. Looks and behaves like **Windows File Explorer** (multi-select, drag & drop, context menus, keyboard-first).
2. **One binary, two faces**: run `s3b` with no arguments for the GUI, with arguments for the CLI — same engine, full parity.
3. **Safe by default, force when asked**: destructive operations count first, state what will happen, and ask once.
4. **Minimal dependencies**: Go + OS webview + official AWS SDK; zero npm runtime dependencies. Source-available under the [PolyForm Internal Use License](LICENSE).
5. Speaks **every S3 dialect** — AWS, MinIO, Ceph, Cloudflare R2, Backblaze B2, Wasabi, DigitalOcean Spaces, IBM COS, Hetzner — and knows each provider's quirks.

## Relationship to s3-bucket-tester

This project builds on [s3-bucket-tester](https://github.com/MikkoP88/s3-bucket-tester) (MIT): its provider capability matrix, S3 diagnostics (DNS/TCP/TLS/auth/policy checks), SigV4 signing knowledge and error-remediation catalog are ported into this codebase with attribution. The upstream repository is treated as read-only source.

## License

[PolyForm Internal Use License 1.0.0](LICENSE) © 2026 Mikko Pesonen (MikkoP88) — source-available, with an Additional Use Grant. In short:

- **Allowed**: any person or company using, modifying and running the tool for their own operations — explicitly including buckets that back the company's own apps, websites and SaaS services offered to end customers.
- **Not allowed**: using the tool to operate storage that is offered or provisioned to others as a bucket/storage service (a storage provider's tenant/customer buckets), and redistributing the software.
- Versions up to and including v1.0.0 were released under MIT and remain MIT for their recipients. Third-party dependencies keep their own permissive licenses (MIT/Apache-2.0), unaffected by this change.
