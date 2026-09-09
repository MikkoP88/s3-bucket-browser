# S3 Bucket Browser

**A Windows-Explorer-style desktop app + CLI for managing Amazon S3 and S3-compatible storage — buckets, objects, versions, and everything in between.**

> **Status: M5 — hardening & packaging.** Deep search, storage-class conversion and object lock join the GUI and CLI; secrets live in the OS keyring; listings stream for huge buckets. Remaining roadmap: v1.0 launch polish (see the [roadmap](PLAN.md#12-milestones)).

## Quickstart (GUI)

```bash
go build -o s3b ./cmd/s3b && ./s3b        # no arguments -> desktop app
```

- **Deep search** (Ctrl+Shift+F): filter every object under a bucket/folder by name glob, size, age or storage class — results stream in and are cancelable; click a result to jump straight to the object.
- **Storage class & object lock**: convert objects between classes (server-side self-copy) from the context menu; per-object retention (GOVERNANCE/COMPLIANCE) and legal hold, plus the bucket-level Lock tab — with the same confirm gates as the CLI.
- **Streaming listing**: huge folders stream page-by-page into the grid (Go side holds one page at a time), so million-object buckets stay responsive.
- **Favorites**: star buckets and folders for one-click jumps from the sidebar.
- **Secrets in the OS keyring** (Windows Credential Manager / macOS Keychain / Linux SecretService), with automatic fallback to the `0600` config file on headless hosts.
- **Explorer layout**: toolbar, back/forward/up history, breadcrumb, folder tree sidebar, sortable details grid, status bar.
- **Dual-pane local browser** (F9): a full local-filesystem pane beside the S3 pane, WinSCP-style — cross-pane drag & drop uploads/downloads, synchronized browsing, and one-click **directory compare** (color-coded newer/older/size-diff/only-here).
- **Open in external editor**: edit remote files in your editor of choice; s3b watches for saves and re-uploads automatically (✎ indicator in the status bar).
- **Versions**: per-object timeline ("Previous Versions"), restore-as-latest, one-click **undo delete** for delete markers, permanent destroy and bulk purge of noncurrent versions — in the GUI dialog and via Shift+Del.
- **Bucket administration**: versioning toggle, bucket policy, CORS, lifecycle rules, default encryption, public-access block, static website and tags — all in one tabbed admin panel (also `s3b bucket …` on the CLI).
- **Multi-select everything**: click / Ctrl+click / Shift+click / Ctrl+A, marquee drag-select, type-to-jump, full keyboard map (F1 in-app).
- **Drag & drop**: drop files or folders from the OS onto the window to upload into the open folder; drag rows onto folders or the tree to move (same bucket) or copy (cross bucket).
- **Transfer manager**: per-file and byte-level progress, speed, cancel — powered by multipart upload/download, with an optional **bandwidth throttle** (512 kB/s … 10 MB/s).
- **Safety ladder**: deletes count first and act second; large selections demand typed confirmation, bucket removal demands typing the bucket name; removing a versioned bucket with `--force` purges the whole version history, markers included.
- **Profiles**: color-coded connections, `~/.aws/credentials` import, built-in connectivity test, connection doctor.
- **Light/dark theme**, conflict policies (overwrite / skip / rename) on upload and download, pre-signed URLs, server-side copy/move, rename, new folder.
- **i18n ready** (English + Finnish built in), accessibility pass on the grid and dialogs (ARIA roles, focus trap), portable mode (drop a `s3b-portable` marker file next to the binary to keep config beside it).

Headless Linux servers can build a pure-Go CLI without GTK dependencies:

```bash
go build -tags s3b_headless -o s3b ./cmd/s3b
```

## Quickstart (CLI)

```bash
go build -o s3b ./cmd/s3b

# Connect to any S3 provider (AWS, MinIO, Wasabi, R2, ...) — credentials
# also fall back to $S3B_ACCESS_KEY / $S3B_SECRET_KEY
s3b profile add lab --endpoint http://localhost:9000 \
    --access-key minioadmin --secret-key minioadmin --default
s3b profile test lab          # lightweight connectivity check
s3b doctor s3://my-bucket     # deep diagnosis: DNS → TCP → TLS → auth → policy/ACL

s3b ls                        # buckets          s3b ls s3://b/photos/    # folder view
s3b tree s3://b               # ASCII tree       s3b du s3://b/photos/    # size + count
s3b stat s3://b/photos/a.jpg  # object metadata
s3b mb s3://new-bucket        s3b mkdir s3://b/folder/

s3b cp report.pdf s3://b/docs/            # upload
s3b cp -r ./site s3://b/site/             # recursive upload
s3b cp s3://b/docs/report.pdf ./out/      # download
s3b cp s3://b/a.jpg s3://b/copy/a.jpg    # server-side copy
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

# Deep search (M5): streams matches, cancelable
s3b find s3://b --name 'backup*'          # glob over the full key
s3b find s3://b/photos/ --larger 10MB --older 90d
s3b find s3://b --class GLACIER --limit 100

# Storage-class conversion (M5): server-side self-copy
s3b sc s3://b/photos/a.jpg GLACIER        # single object
s3b sc s3://b/photos/ GLACIER -r --dry-run  # whole prefix; >50 needs --force

# Object lock (M5): enable at bucket creation — permanent from then on
s3b mb s3://b --object-lock                   # the only moment lock can be enabled
s3b bucket lock s3://b --enable --mode GOVERNANCE --days 30   # default retention rule
s3b lock retention s3://b/report.pdf --mode GOVERNANCE --until +7d
s3b lock retention s3://b/report.pdf --clear --bypass-governance
s3b lock legalhold s3://b/report.pdf --on
```

Every command takes `--json` for machine-readable output, `--profile` to pick a connection, and `--verbose` for per-item detail. Shell completions: `s3b completion bash|zsh|fish|powershell`. Exit codes: `0` OK, `1` operation failure, `2` usage/config error, `3` unexpected.

**Safety ladder** (PLAN.md §9): destructive operations count first and act second. Prefix deletes over 50 objects require `--force`, removing non-empty buckets requires `--force`, and the GUI will additionally require typed confirmation. Enabling object lock is permanent; COMPLIANCE retention cannot be shortened or removed.

## Install

Prebuilt artifacts are attached to every [`v*` release](../../releases): a Windows NSIS installer (`s3b-setup-x.y.z.exe`, registers an App Paths entry so Win+R `s3b` works without touching PATH), standalone zips/tarballs for Windows/Linux, and macOS dmg images — all checksummed in `SHA256SUMS`. Or build from source as shown above; releases stamp the version into `s3b version`.

## Why another S3 browser?

Because none of the existing ones do it all:

| | S3 Bucket Browser | S3 Browser (CS) | Cyberduck | MSP360 | AWS Console |
|---|---|---|---|---|---|
| Windows / macOS / Linux | yes (M2) | / – | / – | / – | browser |
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
