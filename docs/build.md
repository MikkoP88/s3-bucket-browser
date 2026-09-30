# Build from source

One checkout, five build flavors: the same tree builds the desktop GUI
for Windows, Linux and macOS, the pure-Go headless CLI for servers, and
a windowless browser-driven build that serves its UI over HTTP.

Every flavor needs **Go 1.26+**, git and the platform
compiler/libraries. The frontend is vanilla JS/CSS embedded via
`go:embed` — no npm install, no bundler — and the brand assets
(`build/`) are committed.

> **Build the `./cmd/s3b` package.** The repository root is a library
> package, not the application — `go build .` at the root does not
> produce a runnable binary. Every recipe below targets `./cmd/s3b`.

| Flavor | Build tags | Runs on | Extra requirements |
|---|---|---|---|
| Desktop GUI — Windows | `production` | Windows 10/11 (amd64, arm64) | Microsoft Edge WebView2 (preinstalled on current Windows 10/11; otherwise the free Evergreen runtime) |
| Desktop GUI — Linux | `production,gtk3` | Linux desktops (amd64, arm64) | GTK3 + WebKitGTK 4.1 development packages |
| Desktop GUI — macOS | `production` | macOS 13+ (arm64, amd64, universal) | Xcode or Command Line Tools — see the [Mac guide](macos-build.md) |
| Headless CLI | `s3b_headless` | any OS — servers, containers, arm64 boards | none (pure Go, no GUI libraries) |
| Browser-driven | `server` | any OS | none on the host — serves the UI over HTTP |

The `production` tag strips Wails v3's devtools; without it you get a
dev build with the same features. Stamp the version any build reports
with `-ldflags "-X main.version=…"` — see
[Version stamping](#version-stamping).

## Linux (desktop GUI)

```bash
# Ubuntu 24.04+/Debian/Mint — GTK3 + WebKitGTK 4.1, the webkit Wails v3's
# gtk3 tag builds against (its GTK4 default needs webkitgtk-6.0)
sudo apt-get install -y --no-install-recommends libgtk-3-dev libwebkit2gtk-4.1-dev
# current Fedora: sudo dnf install gtk3-devel webkit2gtk4.1-devel
go build -tags production,gtk3 -o s3b ./cmd/s3b && ./s3b
```

## Windows 10/11 (amd64 or arm64)

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

## macOS 13+ (Intel or Apple Silicon)

Build on the Mac in Terminal. Install Go and Apple's compiler tools:

```bash
brew install go
# Only if neither Xcode nor Command Line Tools is installed:
xcode-select --install
```

After Xcode finishes installing, open it once to complete its setup.
Verify `go version` (1.26+) and `xcrun --find clang`, then from this
checkout:

```bash
make mac
open "dist/S3 Bucket Browser.app"
"dist/S3 Bucket Browser.app/Contents/MacOS/s3b" version
```

`make mac` builds for this Mac, packages the icon and Info.plist, and
applies and verifies a local ad-hoc signature. No Node, Wails CLI, paid
developer account or Xcode project is needed — Xcode supplies clang and
the macOS SDK, Go drives the build. `make build` builds just
`bin/s3b` (GUI and CLI); `make mac-universal` puts both architectures
in one bundle. Compiling an Intel slice does not verify its runtime
compatibility — Intel execution still needs testing on an Intel Mac.
The configured deployment target is macOS 13.0, not a claim that every
older OS version has been tested.

The [Mac build guide](macos-build.md) covers setup, troubleshooting,
verification results and the distinction between local signing and
distributing a notarized application. macOS release artifacts remain
disabled; that guide is the local source-build workflow.

## Linux servers / any OS (headless CLI)

Pure-Go CLI with no GUI libraries — for servers, arm64 boards and
keyring-less hosts:

```bash
go build -tags s3b_headless -o s3b ./cmd/s3b
```

## Any OS (browser-driven, windowless)

The same stack without a window: the app serves its UI over HTTP — run
it and open the URL it logs.

```bash
go build -tags server -o s3b ./cmd/s3b && ./s3b
```

## Version stamping

`s3b version`, the About box and diagnostics all report
`main.version`, stamped at link time; an unstamped build reports
`0.2.0-dev`:

```bash
go build -tags production -ldflags "-X main.version=1.1.0" -o s3b ./cmd/s3b
```

CI stamps the git tag without its leading `v`; strip it yourself or
the UI doubles it ("vv1.2.3"). On Windows,
[`tools/versioninfo`](../tools/versioninfo) additionally embeds the
VERSIONINFO resource (file/product version, publisher, copyright) and
the icon set as the exe's RT_GROUP_ICON — run it into `cmd/s3b/` right
before the build and delete the `.syso` afterwards (a stale cross-arch
syso breaks the next build of the other architecture).

## Where to go next

- [CONTRIBUTING.md](../CONTRIBUTING.md) — developer workflows, test
  gates, release cutting
- [macos-build.md](macos-build.md) — the Mac deep-dive: toolchain,
  verification, signing and notarization
- [security.md](security.md) — supply chain: dependencies, SBOM,
  reproducible artifacts
