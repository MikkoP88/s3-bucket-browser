# Changelog

All notable changes to S3 Bucket Browser are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Remote-native file operations (M9): the grid, empty-area and sidebar
  tree context menus on remote sources now offer New folder, Rename
  (F2) and count-then-act Delete (Del) — a new `remotefs.Walk` powers
  the delete preview (files/folders/bytes), the typed confirm states
  plainly that remote filesystems have no trash or versions, and the
  tree stays in sync with the grid after every remote mutation
  (`RemoteMkdir`/`RemoteRename`/`RemoteStat`/`RemoteDeletePreview`/
  `RemoteRemove` on the API). Engine operations serialize per source
  (the FTP engine allows exactly one data connection), with locks
  acquired in sorted-ID order so multi-source operations can never
  deadlock.
- Per-source browsing (M9): the sidebar tree's top level is now the
  configured data sources — the default S3 source expands into its
  buckets exactly as before, while sftp/scp/ftp/ftps/local sources
  expand into their remote directories and browse in the main grid with
  the same row shape, sorting, breadcrumb and Up navigation as S3
  (engine connections are cached per source and dropped on any source
  edit). Non-default S3 sources open a read-only bucket listing with an
  honest hint (their object operations route through the default profile
  until multi-source transfers land). `Test` now dials remote sources
  for real — GUI editor and `s3b source test` alike connect, list the
  root, and report honestly instead of "engine ships next".
- Remote-filesystem engines (M9): new `pkg/core/remotefs` package defines
  one `FS` contract (List/Stat/Open/Create/MkdirAll/Remove/Rename/Close)
  over the data-source schema and ships engines for SFTP/SCP (pkg/sftp +
  x/crypto/ssh — password incl. keyboard-interactive, plus OpenSSH default
  identity keys), FTP/FTPS (jlaffaye/ftp — implicit TLS on port 990,
  explicit AUTH TLS otherwise, anonymous default), and local directories.
  All engines speak anchored slash paths ("/" = the source root) with
  `CleanPath` making root escape impossible by construction, emit the same
  `listing.Entry` rows as the S3 pipeline so the grid renders unchanged,
  and are validated against an in-process SSH+SFTP server (real handshake,
  real filesystem) plus a full local contract suite. Engine teardown closes
  the SSH transport before the sftp client so slow servers cannot hang the
  drain goroutines.
- Data sources (M8): connection profiles generalize into data sources of
  any type — S3 today, with the sftp/scp/ftp/ftps/local schemas already
  fixed so Profile files and the API are forward-compatible for the
  remote-filesystem engines. Legacy S3 profiles migrate one-way into
  sources on first load and S3 sources keep mirroring into the profile
  store, so browsing and the CLI resolve them by name exactly as before.
  The GUI grows a unified type-aware source editor (per-type field sets,
  local-folder browser, honest "engines ship next" Test for non-S3
  types), a Data sources manager dialog, and source-typed status.
- Password-encrypted Profile files (`*.s3bprofile`): a portable bundle of
  data sources to hand a colleague or move between machines. The
  container is AES-256-GCM encrypted with a scrypt-derived key
  (N=32768), versioned (`s3bpf1|` magic), and carries per-file random
  salt + nonce; a wrong password and a corrupted file are deliberately
  indistinguishable. While a file is open it is the single source of
  truth — edits stay in memory (dirty indicator in the status bar) until
  Save/Save As re-encrypts, and nothing leaks into the local store.
  Full File-menu lifecycle (New/Open/Save (Ctrl+S)/Save As/Close with
  unsaved-changes guards) and native pickers included.
- Source-scoped keyring accounts (`sources/<id>/…`): source secrets get
  the same OS-keyring treatment as profile secrets, with an independent
  lifecycle so removing a source cleans up after itself.
- `s3b source` CLI family: add/list/use/remove/test/export/import for
  data sources of any type. `source export FILE` / `source import FILE`
  re-encrypt the full source set into a portable `*.s3bprofile`
  (`--password`, `$S3B_PASSWORD`, or a masked interactive prompt). The
  legacy `s3b profile add/use/remove` are deprecated in its favor, and
  every profile-first write path — the old GUI profile editor, AWS
  credential import, the legacy CLI — now keeps its s3 source mirror in
  sync, so nothing written the old way is invisible to the new sources
  UI and the CLI store runs the one-way migration just like the GUI.

- Publisher metadata everywhere Windows and macOS surface it: the
  binaries now carry a proper VERSIONINFO resource (CompanyName,
  ProductName, FileDescription, versions, LegalCopyright, …) generated at
  build time by a new dependency-free tool (`tools/versioninfo`) that
  emits the COFF `.syso` for each target arch — validated end-to-end by
  reading the fields back with Windows itself. The NSIS installer adds
  DisplayIcon and URLInfoAbout to its ARP entry, the macOS Info.plist
  gains NSHumanReadableCopyright, and the About dialog shows the
  publisher. CI and release pipelines generate + clean the .syso around
  each Windows build (a stale cross-arch .syso breaks the other build).
- Sidebar tree context-menu parity: right-clicking a bucket node (Open,
  Favorites, Upload files/folder here, Paste, Find, Admin, Doctor,
  Properties, Delete bucket) or a folder node (Open, Upload here,
  Download, Copy/Cut/Paste-into, Rename, Delete, Find, Properties with
  recursive object counts) now matches the details grid. Upload, paste,
  download and delete accept an explicit bucket/prefix, so tree actions
  work on nodes outside the current view.
- Empty-area context menus: right-click on the grid background (Paste,
  Upload files/folder, New folder, Download all…, Find, Refresh,
  Properties — folder stats from the live view; New bucket / Import AWS /
  Refresh in the buckets view), on the local pane (Select all, Refresh,
  Open terminal here… — new cross-platform `OpenTerminal` binding), and on
  the sidebar background (Add profile, Import AWS credentials, Collapse
  all, Refresh). All popup menus now share one anchored menu helper.
- Top menu bar (File / Edit / View / Help): dropdown menus with shortcut
  hints, separators and greyed-out unavailable actions; minimal About
  dialog (version, MIT license, project URL).
- Central command-state system: toolbar buttons now grey out when their
  action is unavailable (no selection, no profile, nothing to paste, …).
- Doctor v2 backend: every check carries start/finish timestamps, checks
  can be run individually by name (`DoctorChecks`, `RunDoctorCheck`),
  policy and ACL are timed separately; hermetic unit tests for the
  registry.
- Doctor v2 dialog: check list rendered before running, "Run all" plus
  per-check re-run, status pills, start→finish times and durations,
  expandable detail (advice, error, raw info JSON).
- Structured in-app log events (`log:line`): transfers, doctor runs,
  batch operations (delete/copy/move/rename/mkdir/storage-class) and
  listing failures emit timestamped, scoped, leveled log lines for the
  upcoming log drawer; no secrets are ever logged.
- Optional bottom log drawer (View menu / Ctrl+L / status-bar "Log"):
  timestamp + level + scope + message per line, level filter, copy,
  clear, auto-scroll toggle, 2000-line ring buffer; hidden by default
  and remembered across sessions.
- Auto refresh: View ▸ Auto refresh with Off / 5 s / 10 s / 30 s / 60 s
  intervals plus a "refresh on focus" option; ticks are skipped while a
  modal or context menu is open, transfers are running, or the window is
  hidden; the interval shows in the status bar and persists. The menu
  bar gained one-level submenus and checkmark items to support this.

### Changed

- Relicensed from MIT to the **PolyForm Internal Use License 1.0.0** with
  an Additional Use Grant (LICENSE): any person or company may use,
  modify and run the tool freely for their own operations — explicitly
  including buckets that back their own apps, websites and SaaS services
  offered to end customers — while the tool may not be used to operate
  storage offered or provisioned to others as a bucket/storage service
  (a storage provider's tenant/customer buckets), and the software may
  not be redistributed. Versions up to and including v1.0.0 remain MIT
  for their recipients. Third-party dependency packages keep their own
  permissive licenses (MIT/Apache-2.0), unaffected by this change; the
  per-release dependency report and SBOM continue to carry their notices.
  The Windows VERSIONINFO resource, the macOS Info.plist and the GUI
  About dialog now state the new license.

- Checkbox column in both grids (remote + local pane): a per-row checkbox
  toggles selection without resetting the rest, and the header checkbox
  selects all / none (indeterminate when partial). It feeds the existing
  selection model, so every command (download, copy, drag & drop, …)
  works on checked rows unchanged.

- One Upload command: the separate "Upload folder" toolbar button is gone.
  A single "Upload ▾" button opens a small menu (Files / Folder); Ctrl+U
  still opens the file picker directly, and File ▸ Upload files/folder
  mirror both pickers. The backend walks directories either way, and
  drag & drop was never affected.

### Fixed

- Generated source IDs re-roll on nanosecond-clock collisions (observed
  on Windows), which could silently replace a just-added source with the
  next one.
- Windows: launching the GUI no longer opens an empty console window behind
  the app. The binary keeps its console subsystem (the CLI needs it), but
  GUI mode now detaches the console at startup (`FreeConsole`), so
  double-click / Start-menu launches are popup-free; CLI behavior and
  launching from a terminal are unchanged.

## [1.0.0] — 2026-09-09

First stable release. Windows-Explorer-style S3 GUI + CLI in one binary,
MIT-licensed, zero telemetry.

### Added — core browsing & transfer

- Explorer-style GUI: toolbar, back/forward/up history, breadcrumb, folder
  tree sidebar, sortable details grid, status bar, light/dark theme.
- Full multi-select (click / Ctrl / Shift / Ctrl+A, marquee, type-to-jump)
  and a complete keyboard map (F1 in-app).
- Drag & drop: OS files/folders onto the window to upload; rows onto
  folders/tree to move (same bucket) or copy (cross bucket).
- Dual-pane local browser (F9) with cross-pane drag & drop, synchronized
  browsing and one-click directory compare (newer/older/size-diff/only-here).
- Transfer manager: multipart upload/download with per-file and byte-level
  progress, speed, cancel, optional bandwidth throttle.
- Conflict policies (overwrite / skip / rename) on upload and download;
  external-editor integration with automatic re-upload on save.
- CLI with full parity: `ls`, `tree`, `du`, `stat`, `mb`, `rb`, `mkdir`,
  `cp`, `mv`, `rm`, `sync`, `presign`, `find`, `sc`, `doctor`, `profile`,
  shell completions (bash/zsh/fish/powershell), `--json` everywhere,
  exit-code contract 0/1/2/3.

### Added — versioning (headline feature)

- Per-object version timeline, restore-as-latest, one-click undo delete for
  delete markers, permanent destroy of specific versions (L3), bulk purge
  of noncurrent versions, version statistics; `rb --force` purges full
  version history of versioned buckets.

### Added — bucket administration

- Tabbed admin panel + `s3b bucket …`: versioning, policy, CORS,
  lifecycle, default encryption, public-access block, static website,
  tags, object-lock configuration.
- Storage-class conversion (server-side self-copy), per-object retention
  (GOVERNANCE/COMPLIANCE) and legal hold, `mb --object-lock`.

### Added — safety, connection & scale

- Safety ladder (PLAN.md §9): count-then-act on every destructive
  operation, `--force` gates, `--dry-run` previews, typed bucket name in
  the GUI for bucket-wide actions.
- Profiles with OS-keyring secret storage (Windows Credential Manager /
  macOS Keychain / Linux SecretService), `0600` file fallback, masked
  secrets in all output, `~/.aws/credentials` import, connectivity test.
- Connection doctor: DNS → TCP → TLS → auth → policy/ACL with
  plain-language remediation; provider capability matrix (R2, MinIO, B2,
  Wasabi, …) warns before unsupported requests fail.
- Streaming listings (the Go side holds one page at a time) and cancelable
  streaming deep search by name, size, age or storage class.
- Favorites, i18n (English + Finnish built in), accessibility pass on the
  grid and dialogs, portable mode (`s3b-portable` marker file).

### Added — packaging & release

- Release pipeline on `v*` tags: Windows NSIS installer (App Paths entry —
  Win+R `s3b` works without touching PATH), standalone zips/tarballs,
  macOS dmg, `SHA256SUMS`, dependency report and SBOM (SPDX-JSON, syft) per
  release.
- Generated single-file CLI reference (`docs/cli.md`, `go run
  ./tools/gendocs`) with a CI freshness check; competitive comparison and
  security-model pages; `CHANGELOG.md` and `CONTRIBUTING.md`.
- Memory-bound listing test: a hermetic walk of 250k objects through an
  in-process mock S3 grows the live heap by <1 MB (retaining everything
  costs 40 MB) plus a 100k-object listing benchmark — the O(page) streaming
  guarantee (PLAN.md §13) is now pinned by CI.

### Fixed

- **CI was silently building Wails' stub frontend, not the GUI.** The build
  matrix passed tags unquoted (`tags: desktop,production`); YAML flow
  mappings split at the comma, so CI built with `-tags desktop` only. The
  `production` tag is what selects Wails' real desktop frontend — without
  it, `internal/app/app_default_unix.go` compiles a stub that imports no
  GUI code at all, so every platform "built" without cgo, GTK or Cocoa and
  the artifacts were CLI-only binaries with a build-tag error for a GUI.
  Matrix tags are now quoted; CI builds the same binaries the release
  pipeline does.
- **Release pipeline: linux/darwin GUI builds.** Real production builds
  surfaced three platform requirements, now fixed in CI and release
  workflows: linux needs Wails' `webkit2_41` tag on Ubuntu 24.04 (which
  ships webkit2gtk 4.1 only); darwin cross-arch builds (amd64 on arm64
  runners) need `CGO_ENABLED=1` (Go disables cgo when cross-compiling);
  and the macOS 26 SDK needs `-framework UniformTypeIdentifiers` linked
  explicitly (WailsContext.m uses UTType, which the SDK no longer
  auto-links).
- **GUI builds required Wails' build tags all along.** A plain
  `go build` produced a binary whose GUI face only showed Wails' "will not
  build without the correct build tags" error (the CLI face worked, which
  is why e2e never caught it). All documented build commands, CI and
  release pipelines now pass `-tags desktop,production`.
- **GUI: backend bindings were unreachable — the app booted to an empty
  shell.** Wails exposes bound Go methods under `window.go[<full import
  path>].App`, but the frontend assumed a nested `window.go.pkg.api.App`
  path (and `window.go.main.App` for the local pane). The binding is now
  resolved by probing `window.go` once (api.js), so the GUI works on every
  Wails version/layout. Found by live validation against a real bucket.
- **GUI: the details grid never rendered any rows.** The virtualized row
  pool was built and configured but never attached to the DOM (missing
  `canvas.appendChild`), so listings showed an empty body while the status
  bar still counted items. Selection, filtering and keyboard navigation
  worked on the model, which masked the bug in smoke tests.
- Storage-class conversion to the same class is now idempotent (S3
  rejects no-op self-copies; they are treated as success).
- Object-lock buckets must be created with `mb --object-lock`; the CLI
  and GUI create a clear error instead of a raw provider failure.
- Clearing/shortening GOVERNANCE retention sends
  `x-amz-bypass-governance-retention` when `--bypass-governance` is set.

### Known issues

- **Cancelling a native file/folder dialog closes the whole app on
  Windows.** Pressing ESC or "Cancel" in the OS "Choose download folder" /
  upload picker terminates the app: the cancelled Windows file dialog
  posts `WM_QUIT`, and Wails v2's Windows message loop treats it as an
  exit signal. Workaround: pick a folder instead of cancelling, or close
  the dialog with its X button. In-app (HTML) dialogs cancel normally.

[1.0.0]: https://github.com/MikkoP88/s3-bucket-browser/releases/tag/v1.0.0
