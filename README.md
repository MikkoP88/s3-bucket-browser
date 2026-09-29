# S3 Bucket Browser

<p align="center"><img src="build/icon.svg" width="120" alt="S3 Bucket Browser" /></p>

**A unified desktop browser and CLI with Windows File Explorer and a WinSCP inspired interface for managing S3‑compatible cloud storages and remote file servers, offering comprehensive supporting S3 buckets/objects, SFTP/SCP, FTP/FTPS, WebDAV, and local folders, with robust versioning, bucket administration, and security, and cross‑storage migration capabilities.**

> **Status: v1.0 released; 1.1.0 in beta (current pre-release: 1.1.0-beta.21).** The project is in its Beta phase: core functionality is operational, but some features may exhibit partial functionality. 1.1 adds a unified data-source hierarchy, OS clipboard/drag interop, credential import, and a unified versioned-delete flow — see the [CHANGELOG](CHANGELOG.md).

## Key Features of S3 Bucket Browser(s3b)

### ✔ Cross‑platform
- **Windows, Linux, macOS** — native desktop app  
- **Browser‑driven mode** — optional windowless UI accessible from any OS

### ✔ Source‑available
- **PolyForm Internal Use** license  
- Transparent codebase, no proprietary lock‑in

### ✔ True Windows‑Explorer semantics
- Multi‑select: **Ctrl/Shift**, **Ctrl+A**, **Ctrl+I**, marquee selection  
- Type‑to‑jump, drag & drop everywhere  
- Context menus, breadcrumbs, folder tree  
- Sortable details grid with pickable, resizable, reorderable columns  
- Dual‑pane local browser  
- Keyboard‑first operation (**F1** shows the full map)  
- Guarded exit: never drops running transfers or unsaved profile work

### ✔ Every source in one app
- **S3‑compatible:** AWS, MinIO, Wasabi, Cloudflare R2, Backblaze B2, DigitalOcean Spaces, IBM COS, Hetzner, Ceph, Dell ECS, StorageGRID  
- **Remote servers:** SFTP/SCP, FTP/FTPS, WebDAV/WebDAVs  
- **Local folders**  
- All color‑coded in one sidebar  
- Same UI and same CLI (`NAME://` URIs)

### ✔ Migration across storage types
- Any‑to‑any transfers: drag rows between sources, panes, or the tree  
- CLI: `s3b cp s3://bucket/ sftp://dst/ -r`  
- Same‑source S3 copies run **server‑side**  
- Cross‑source copies stream through one transfer manager  
- Conflict pre‑checks, throttling, resumable operations

### ✔ S3 versioning done right
- Per‑object timelines with restore‑as‑latest  
- Text diffs for versioned text objects  
- One‑click undo delete for markers  
- Three‑way Delete Window: marker / keep‑current / permanent  
- Version‑ and marker‑count badges  
- Folder‑level version overviews  
- Bulk purge of noncurrent versions  
- Force‑empty versioned buckets

### ✔ Import your credentials
- AWS shared files (`~/.aws/credentials`, `~/.aws/config`)  
  - `endpoint_url` → auto‑mapped to MinIO/R2/Wasabi/etc.  
- rclone, JSON, `.env`, encrypted `.s3bprofile` containers  
- KMS/secrets services: Vault, AWS Secrets Manager, Azure Key Vault, GCP  
- Fully custom HTTP endpoints  
- Live bucket‑count test before saving

### ✔ Savable encrypted profiles
- **Ctrl+S** saves the entire workspace  
- All sources + connections stored in one encrypted `.s3bprofile`  
- **scrypt + AES‑256‑GCM**  
- Reopen, keep, or share securely

### ✔ Security
- Secrets stored in OS keyring:  
  - Windows Credential Manager  
  - macOS Keychain  
  - Linux SecretService  
- Secrets masked everywhere, never logged  
- Optional **Secure Storage** mode:  
  - Seals the entire store in an AES‑256‑GCM envelope  
  - Hardens temp workspaces  
- **Zero telemetry**  
- See `docs/security.md`

### ✔ Fast at scale
- Streaming page‑by‑page listings (Go holds one page at a time)  
- Virtualized rendering  
- Cancelable deep search  
- Responsive even on **million‑object buckets**

*One binary, two faces: run `s3b` with no arguments for the GUI, with arguments for the CLI — same engine, full parity.*

## Why another S3 browser?

Because none of the existing ones do it all:

| Feature | S3 Bucket Browser | S3 Browser (CS) | Cyberduck | MSP360 | AWS Console |
|--------|--------------------|-----------------|-----------|--------|-------------|
| **Windows support** | ✔️ Yes | ✔️ Yes | ✔️ Yes | ✔️ Yes | ❌ No |
| **Linux support** | ✔️ Yes | ❌ No | ✔️ Yes | ❌ No | ❌ No |
| **Mac support** | ✔️ Yes | ❌ No | ✔️ Yes | ❌ No | ❌ No |
| **Browser support** | ✔️ Yes | ❌ No | ❌ No | ❌ No | ✔️ Yes |
| **Source-available** | ✔️ PolyForm | ❌ No | GPL | ❌ No | – |
| **Explorer-style multi-select, drag & drop** | ✔️ Full | ◑ Partial | ◑ Partial | ◑ Partial | ❌ No |
| **Versioning management** | ✔️ First-class | ◑ Partial | ◑ Partial | ◑ Partial | ❌ Clunky |
| **GUI + CLI in one binary** | ✔️ Yes | ❌ No | ❌ Separate | ❌ No | – |
| **Remote file servers (SFTP/FTP/WebDAV)** | ✔️ Yes | ❌ No | ✔️ Yes | ❌ No | ❌ No |
| **Provider-quirk awareness** | ✔️ Full | ◑ Minimal | ◑ Profiles | ◑ Minimal | AWS only |
| **Connection doctor** | ✔️ Yes | ❌ No | ❌ No | ❌ No | ❌ No |
| **Telemetry** | None | Unknown | ✔️ Yes | ✔️ Yes | ✔️ Yes |

See [docs/comparison.md](docs/comparison.md) for the full landscape and gap analysis.

## Supported operating systems & requirements

One binary per platform carries the GUI and the CLI together (`s3b` with no
arguments opens the desktop app; with arguments it is the CLI).

| Platform | Builds | Webview / runtime needed |
|---|---|---|
| **Windows 10/11** | amd64, arm64 | Microsoft Edge WebView2 (preinstalled on current Windows 10/11; a machine without it needs the free Evergreen runtime from Microsoft first) |
| **macOS 13+ (source build)** | arm64, amd64, universal | System WebKit; local execution verified on macOS 15.2 / arm64 |
| **Linux desktop** | amd64, arm64 | GTK3 + WebKitGTK 4.1 (what Ubuntu 24.04+, Mint and current Fedora ship) |
| **Linux servers (headless CLI)** | amd64, arm64 | none — the `-tags s3b_headless` build is a pure-Go CLI with no GUI libraries |
| **Any OS, browser-driven** | `-tags server` | none on the host — the same stack runs windowless and serves the UI over HTTP |

- Some operating system versions especially **beta** or **pre‑release** builds may still exhibit compatibility issues.  
- The application is primarily developed and tested in **Windows environments**.  
- Verification focuses on **application functionality**, not full OS‑level compatibility across all distributions.

**macOS: local source builds.** The release workflow does not currently
produce macOS artifacts. See the tested local build
recipe in [Build from source → macOS](#macos-13-intel-or-apple-silicon)
— and the [local verification notes](docs/macos-build.md).

- **To run**: the portable editions need no install and no admin rights —
  releases ship an NSIS installer (machine-wide, elevated) and portable
  zip (Windows), and tarballs plus portable tarballs (Linux). Secrets use the OS keychain (Windows
  Credential Manager, macOS Keychain, Linux SecretService); keyring-less
  hosts fall back to a `0600` file.
- **To build from source**: Go **1.26+**, git and the platform compiler/libraries — the frontend is
  vanilla JS/CSS embedded via `go:embed` (no npm install, no bundler).
  Linux GUI builds additionally need `libgtk-3-dev` and
  `libwebkit2gtk-4.1-dev`. Per-OS recipes:
  [Build from source](#build-from-source);
  developer workflows live in [CONTRIBUTING.md](CONTRIBUTING.md).

## Install

Prebuilt artifacts are attached to every [`v*` release](../../releases): a Windows NSIS installer (`s3b-setup-x.y.z.exe`, registers an App Paths entry so Win+R `s3b` works without touching PATH), and standalone zips/tarballs for Windows and Linux (amd64 and arm64) — all checksummed in `SHA256SUMS`, with a dependency report and SBOM (SPDX-JSON) per release (see [docs/security.md](docs/security.md)). Or build from source as shown above; releases stamp the version into `s3b version`.

## Documentation

- **[Usage guide](docs/usage.md)** — the full walkthrough with screenshots (same content as the in-app guide)
- **[Mac build](docs/macos-build.md)** — local toolchain, build and launch
- **[CLI reference](docs/cli.md)** — every command, generated from the cobra tree
- **[Security model](docs/security.md)** — keyring, Secure Storage, safety ladder, supply chain
- **[Verification reports](docs/verification/)** — per-release evidence: the build, the OS, and the full action-verification matrix behind every tag
- **[Competitive comparison](docs/comparison.md)** — the S3-browser landscape, fact-checked
- **[CHANGELOG](CHANGELOG.md)** · **[CONTRIBUTING](CONTRIBUTING.md)** · **[Portable edition](README-portable.md)**

## Screenshots of S3 Bucket Browser(s3b)

![Main window](docs/screenshots/main-view.png)

More screenshots:

| | |
|---|---|
| ![Data sources](docs/screenshots/sources-tree.png) | ![Dual pane compare](docs/screenshots/dual-pane-compare.png) |
| ![Versions](docs/screenshots/versions.png) | ![Delete window](docs/screenshots/delete-window.png) |
| ![Admin panel](docs/screenshots/admin-panel.png) | ![Dark theme](docs/screenshots/dark-theme.png) |

The full tour with screenshots lives in the **[usage guide](docs/usage.md)**; the app carries the same guide (Help → User guide, F1).

## Build from source

Everything below builds from a plain checkout: **Go 1.26+, git and the platform compiler/libraries** —
the frontend is vanilla JS/CSS embedded via `go:embed` (no npm install, no
bundler) and the brand assets (`build/`) are committed. The `production`
tag strips Wails v3's devtools; stamp the version any build reports with
`-ldflags "-X main.version=$(git describe --tags --always)"`.

### Linux (desktop GUI)

```bash
# Ubuntu 24.04+/Debian/Mint — GTK3 + WebKitGTK 4.1, the webkit Wails v3's
# gtk3 tag builds against (its GTK4 default needs webkitgtk-6.0)
sudo apt-get install -y --no-install-recommends libgtk-3-dev libwebkit2gtk-4.1-dev
# current Fedora: sudo dnf install gtk3-devel webkit2gtk4.1-devel
go build -tags production,gtk3 -o s3b ./cmd/s3b && ./s3b
```

### Windows 10/11 (amd64 or arm64)

Plain dev build — works out of the box, no icon or version stamp:

```bash
go build -tags production -o s3b.exe ./cmd/s3b
```

Release-style build — GUI subsystem (native, console-flash-free launch;
the CLI still prints normally — it re-attaches the parent terminal on
demand) plus the version stamp and the brand icon embedded as the exe's
RT_GROUP_ICON resource, which is what the taskbar, Alt-Tab and the NSIS
installer show:

```bash
VER="$(git describe --tags --always)"
go run ./tools/versioninfo -version "${VER#v}" -arch amd64 -icon build/icon.ico
go build -tags production -ldflags "-s -w -X main.version=${VER#v} -H windowsgui" -o s3b.exe ./cmd/s3b
rm -f cmd/s3b/*.syso   # stale syso poisons the next build of the other arch
```

(arm64: `-arch arm64`. Strip the tag's leading `v` — the UI prefixes its
own, or the About box shows "vv1.2.3".)

### macOS 13+ (Intel or Apple Silicon)

Build on the Mac in Terminal. Install Go and Apple's compiler tools:

```bash
brew install go
# Only if neither Xcode nor Command Line Tools is installed:
xcode-select --install
```

After Xcode finishes installing, open it once to complete its setup. Verify
`go version` (1.26+) and `xcrun --find clang`, then from this checkout:

```bash
make mac
open "dist/S3 Bucket Browser.app"
"dist/S3 Bucket Browser.app/Contents/MacOS/s3b" version
```

`make mac` builds for this Mac, packages the icon and Info.plist, and applies
and verifies a local ad-hoc signature. No Node, Wails CLI, paid developer
account or Xcode project is needed. Xcode supplies clang and the macOS SDK;
Go drives the build. `make build` builds just `bin/s3b` (GUI and CLI).

For both architectures in one bundle, run `make mac-universal`. Intel
execution still needs testing on an Intel Mac; compiling a slice does not
verify its runtime compatibility. The configured deployment target is
macOS 13.0, not a claim that every older OS version has been tested.

See **[Mac build instructions](docs/macos-build.md)** for setup,
troubleshooting, verification results and the distinction between local
signing and distributing a notarized application. macOS release artifacts
remain disabled; this change provides a local source-build workflow.

### Linux servers / any OS (headless CLI)

Pure-Go CLI with no GUI libraries — for servers, arm64 boards and
keyring-less hosts:

```bash
go build -tags s3b_headless -o s3b ./cmd/s3b
```

### Any OS (browser-driven, windowless)

The same stack without a window: the app serves its UI over HTTP —
run it and open the URL it logs.

```bash
go build -tags server -o s3b ./cmd/s3b && ./s3b
```

## Relationship to s3-bucket-tester

This project builds on [s3-bucket-tester](https://github.com/MikkoP88/s3-bucket-tester) (MIT): its provider capability matrix, S3 diagnostics (DNS/TCP/TLS/auth/policy checks), SigV4 signing knowledge and error-remediation catalog are ported into this codebase with attribution. The upstream repository is treated as read-only source.

## License

[PolyForm Internal Use License 1.0.0](LICENSE) © 2026 Mikko Pesonen (MikkoP88) — source-available, with an Additional Use Grant. In short:

- **Allowed**: any person or company using, modifying and running the tool for their own operations — explicitly including buckets that back the company's own apps, websites and SaaS services offered to end customers.
- **Not allowed**: using the tool to operate storage that is offered or provisioned to others as a bucket/storage service (a storage provider's tenant/customer buckets), and redistributing the software.
- Versions up to and including v1.0.0 were released under MIT and remain MIT for their recipients. Third-party dependencies keep their own permissive licenses (MIT/Apache-2.0), unaffected by this change.
