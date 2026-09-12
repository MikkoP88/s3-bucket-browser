# Changelog

All notable changes to S3 Bucket Browser are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **Live GUI harness** (`npm run gui-live`, dev-only): a real-backend live
  walk of the GUI. `tools/gui-live` serves the production `frontend/` over
  local HTTP with the actual `pkg/api` app behind a reflection-dispatched
  bridge (`POST /__live/call`) and backend events streamed over SSE into
  the frontend's `EventsOn`, so Playwright drives the real UI against real
  S3 (two small seams were added for this: `SetEventSink` for the event
  bus and `SetProfileDialogs` for scripted pickers). `scripts/gui-live.mjs`
  runs ~61 checks — onboarding, source add/edit/Test, tree navigation,
  uploads via drag & drop (plain, overwrite, rename), versioning (list,
  A/B pick, text diff), downloads to the dual-pane local side, cross-pane
  transfers, the transfers manager, restart persistence through an
  encrypted profile file, and cleanup — asserting DOM state, on-disk
  bytes, version counts and the transfers log. `scripts/js-check.sh`
  syntax-checks the harness; the full run needs credentials and a real
  bucket, so it lives on the dev machine, not in CI. It found three real
  bugs, fixed below.
- **WebDAV engine.** Two new source types, `webdav` (HTTP, default port
  80) and `webdavs` (HTTPS, 443), speak RFC 4918 with a stdlib-only
  HTTP client — PROPFIND for listings/metadata, GET/PUT for content,
  MKCOL/MOVE/DELETE for structure — with HTTP Basic auth and an anchored
  root path, so any compliant server (Apache, nginx, rclone serve webdav,
  Nextcloud, IIS) works with no third-party runtime dependencies
  (`golang.org/x/net` moves to a direct dependency for its WebDAV test
  server only). Browsing, the transfer matrix, directory compare and the
  remote CLI commands (`ls`, `tree`, `du`, `stat`, `mkdir`, `cp`, `mv`,
  `rm`) treat WebDAV sources like any other remote engine; `s3b source
  add` accepts `--type webdav|webdavs` or the `webdav://user:pass@host:
  port/root` URL shorthand. A contract test suite pins the full
  filesystem guarantees against a live x/net WebDAV server (root and
  prefixed mounts, auth accept/reject), and the e2e-remote matrix runs
  against real `rclone serve webdav` instances (root and `/dav`-prefixed)
  including cross-engine transfers both ways.
- **S3 sources in the dual-pane side view.** The side pane's source
  dropdown no longer skips S3 sources: binding it to one browses that
  source's buckets and prefixes (streamed, one-page memory like the main
  view; Up/Home/path prompt follow the buckets-view level), and every
  combination of the transfer matrix works to and from it — local ↔ S3
  source, S3 source ↔ S3 source (server-side when both sides are the same
  source), S3 source ↔ remote engine, S3 source ↔ the main view's default
  source — via drag & drop (bucket rows take drops as "into the bucket
  root"), copy/cut/paste, and the pane's context menus (Upload files /
  folder, Download all). Engine-native operations that address the default
  client only (Rename, Delete, Properties, New folder) are offered when the
  pane is bound to the default S3 source and hidden otherwise; bucket rows
  are navigation-only. Directory compare (Compare Any) understands an
  S3-source side, so keep-in-sync decorations work against any pane
  binding.
- **Body-level drop targets.** The empty area below the rows of both the
  main grid and the side pane is now a drop target (highlighted): dropping
  there transfers into the current directory instead of being a dead zone.
  OS-level file drops (Explorer → app) are hit-tested against the panes:
  a drop over the side pane uploads/transfers into the pane's current
  folder (any binding), otherwise the main view takes it as before.
- New `s3ClientFor` resolution in the backend: named S3 sources (by id or
  name) for streamed listings (`ListSourceObjectsStream`), bucket lists
  (`ListSourceBuckets`), directory compare sides, and transfer
  destinations — non-S3 sources are rejected with a clear error.
- Context menus on data-source root nodes in the sidebar tree: Open
  (buckets for S3, root directory for remote/local sources), Refresh,
  Reconnect (drops cached connections/engines and re-lists the node),
  Test connection (S3 probe or a root listing through the live engine),
  Set default (non-default S3 sources), Edit source and Remove source —
  parity with the grid's source management, no detour through the Data
  sources dialog. Remote/local source roots are now also drag-and-drop
  targets (drop = transfer into the source's root directory), and the
  empty sidebar carries a persistent "+" button next to the DATA SOURCES
  header to add the first source.

### Fixed

- License audit corrections: `gen-notice.sh` mislabeled two direct
  dependencies' SPDX ids in release NOTICE files — `jlaffaye/ftp` is ISC
  (was "MIT") and `pkg/sftp` is BSD-2-Clause (was "BSD-3-Clause"),
  verified against the modules' own LICENSE files. `docs/security.md`
  still claimed a "MIT/Apache-2.0 only" dependency set and
  `docs/comparison.md` still said "no SBOM yet" — both updated to match
  the post-M9 dependency reality (SBOM + SHA256SUMS ship per release).

### Changed

- Drag payloads now carry their origin unambiguously (remote sources:
  `source` without bucket; S3 side pane: `source` + `bucket`; main grid:
  `bucket` only = default source), so the same-bucket/same-source "move is
  default" modifier rules and the onto-itself guard apply across the whole
  matrix, and S3→S3 keeps the synchronous server-side copy path only when
  both sides are the default source.

- **Strict session-only data sources (GUI).** The GUI workspace is now
  either an open encrypted Profile file or a session-only in-memory
  registry — the GUI never reads or writes the CLI's `profiles.json`
  anymore (the CLI store is untouched and keeps working). Sources added
  without an open Profile file live in memory only and vanish on close:
  the status bar shows "● N unsaved sources" with the escape hatch spelled
  out. Ctrl+S / File → Save with no file open runs Save As directly, so
  session sources become an encrypted `.s3bprofile` wherever the user
  picks — the "cannot save without creating a New Profile file first"
  dead end is gone. New/Open Profile file guard against shadowing unsaved
  session sources; Close offers a discard confirmation. `~/.aws/credentials`
  import lands in the workspace too. The legacy GUI profile-mirror API
  (ListProfiles/SaveProfile/RemoveProfile/SetDefaultProfile) was removed
  along with the store fallback in `client()` — S3 browsing resolves from
  the open file or session, nowhere else.

### Added

- Localized UI in 15 languages: English, Finnish, Swedish, German, French,
  Spanish, Portuguese, Italian, Dutch, Polish, Russian, Turkish, Chinese
  (Simplified), Japanese and Korean — all selectable from Settings → Language
  or the Settings menu, shown under their native names, auto-detected from the
  browser locale when set to Auto. `scripts/i18n-check.mjs` (now part of
  `scripts/js-check.sh`) deep-validates every dictionary on each run: full key
  parity with en, non-empty values, matching `{placeholder}` tokens and
  LANG_NAMES coverage in both directions.
- Settings menu in the top bar (between View and Help) with a full Settings
  dialog: theme, language (Auto plus every supported language — switching
  reloads the window), panels and log-area visibility, auto-refresh interval
  and refresh-on-focus, plus transfer defaults — a conflict policy that can
  skip the per-transfer dialog entirely (overwrite / skip / rename) and the
  remembered speed limit. Every row applies immediately and persists across
  restarts; the same keys the shell already read, so nothing migrates.
- Native single-process launch on Windows: release binaries (and the README
  quickstart) link with `-H windowsgui` — the app starts with no console
  flash and no lingering console window, as one native process. The CLI in
  the same binary re-attaches the parent terminal on demand: cmd.exe and
  PowerShell get output through `AttachConsole` + `CONOUT$`, while
  Git-Bash/mintty pipes and redirections (`> file`, `| grep`) keep using
  their inherited handles untouched.
- `source add` URL shorthand: `s3b source add [NAME] sftp://user:pass@host:port/root`
  (scp:// ftp:// ftps:// too) sets type, host, port, credentials and root
  from one URL — percent-encoded special characters in passwords are
  decoded, the name defaults to the hostname, and combining a URL with
  `--type/--host/--port/--username/--password/--root` is a usage error.
- FTP engine unit suite: a minimal but real in-process FTP server (RFC 959
  control protocol, RFC 3659 MLST/MLSD facts, EPSV/PASV data connections)
  serves the local filesystem and anchors the client at a temp root,
  mirroring the SFTP suite. The full FS contract — List/Stat/Open/Create/
  MkdirAll/Rename/Remove, root-escape guards, auth and dial errors — runs
  twice: against an MLSD-capable server and against a vsftpd-style one
  (no MLST: unix `ls` listings, no single-entry stat).
- The remote e2e script now drives the whole M10.5 command matrix against
  the live Docker servers: mkdir/cp/ls/tree/du/stat through `NAME://`
  URIs (the sftp:// URL shorthand included), byte-verified
  local→remote→local round trips with space/unicode names, same-engine
  spool copies, mv, cross-engine sftp↔ftp transfers, and rm tree guards.
- Portable release builds (M10.6): every release now ships portable
  editions next to the installers — `s3b-<ver>-linux-amd64-portable.tar.gz`,
  `s3b-<ver>-linux-arm64-cli-portable.tar.gz` and
  `s3b-<ver>-windows-{amd64,arm64}-portable.zip`. Each archive is the
  binary plus `LICENSE`, a generated `NOTICE` (direct dependencies with
  SPDX ids read live from go.mod, full pinned module graph, SBOM pointer),
  the `s3b-portable` marker that keeps all state in a `config` folder
  beside the binary, and a `README-portable.md` explaining usage, what
  travels and what deliberately does not (OS-keychain secrets stay on the
  host machine). macOS keeps the dmg as its only form — portable mode
  still works there if a user drops a marker next to the binary.
- CLI parity (M10.5): every saved non-S3 source is now reachable from the
  shell as `NAME://dir` URIs — `ls`, `tree`, `du`, `stat`, `mkdir`, `rm`,
  `cp` and `mv` all accept them beside `s3://` paths (e.g.
  `s3b ls lab://docs`, `s3b cp lab://a.txt s3://bucket/`, `s3b cp
  s3://bucket/pics/ lab://archive/ -r`). The copy engine composes every
  operand mix — remote/S3/local on either side — with recursive tree
  copies, per-file progress, dry-run, and a temp-file spool when a source
  would otherwise copy onto itself; `--force` still gates large deletes.
  `ls --watch` re-lists any bucket or source directory on `--interval`
  (default 2s) and prints only `+` added, `~` changed and `-` removed
  entries until Ctrl+C. A new `s3b log` command tails the app activity
  log the GUI drawer shows: activity is now persisted as JSON lines in
  `events.jsonl` beside the profiles (capped at 1 MiB, rotating to the
  newest half), and `s3b log [-n N] [--level info|warn|error]
  [--scope prefix] [-f]` filters or follows it. Source-only commands no
  longer require an S3 profile to exist.
- Versioning & object-lock visuals (M10.4): the navbar now shows guard chips
  for the browsed bucket — versioning state (on / suspended) and object-lock
  mode with retention days — fetched through a new cheap `GetBucketGuard`
  endpoint (two tolerant calls, cached per bucket per session; clicking a
  chip opens the bucket admin panel). The Previous Versions dialog gains a
  vs-current column: every old version shows its size delta against the
  current one (or an "identical" tag when the ETags match) plus a one-click
  "vs current" diff, next to the existing A/B compare. Single-object
  Properties now include the object's retention mode/until date and legal
  hold state when the bucket has a lock config.
- Multi-run commands (M10.3): Properties, Pre-sign URL and Object lock now
  accept the full selection. A multi-selection Properties summarizes
  composition (folders/files), total size, modification range and the
  deepest common prefix without per-item round trips; multi presign signs
  every selected object locally and lists one URL per row with copy-per-row
  and copy-all; the lock dialog's retention/legal-hold actions run on every
  selected object through a shared batch runner with live per-item status
  (pending/running/ok/failed + error), a Stop button and a final tally.
  The status bar grows into a selection summary bar — count, folders,
  files and total size of the current selection. Download and Storage
  class were already selection-wide.
- Panels v2 (M10): the dual-pane's side pane is no longer local-only — a
  source dropdown binds it to the workstation filesystem or to any remote
  source (sftp/scp/ftp/ftps/local-dir; S3 sources keep browsing in the main
  view), remembered across restarts. A remote-bound pane is a full peer in
  the transfer matrix: its clipboard records the remote origin, its drags
  carry `{source, dir, keys, entries}`, its row/empty-area menus offer
  Download, Copy/Cut/Paste-into-folder, Rename, Delete, New folder, Upload
  and engine-side Properties, and Enter on a file downloads it. Directory
  compare is now pane↔pane through one `CompareAny(left, right)` driver —
  local ↔ S3, local ↔ remote and remote ↔ remote — with the same
  recursive size/mtime verdicts, 2 s clock tolerance and grid decorations
  (`CompareDir` is a thin wrapper over it); the summary dialog labels the
  sides by their real refs. Versioning gains a compare view: pick any two
  versions A/B in the Versions dialog to see a metadata table (size, mtime,
  ETag, storage class — identical ETags answer instantly) plus a unified
  line diff of the contents (LCS with common prefix/suffix trimming,
  context collapsing and hard caps; `VersionDiffText` streams each version
  with a 512 KB limit, skips delete markers and binary content, and says
  so). Synchronized browsing stays local-binding-only (it maps local dirs
  to S3 prefixes) and disables itself on other bindings.
- Cross-source transfers (M10 backend): `TransferCross` streams copies
  between any two sides — S3 (the default profile or a named S3 source),
  the remote-filesystem sources (sftp/scp/ftp/ftps/local-dir) and the
  local pane — into an S3 bucket/prefix, a remote directory or a local
  folder. One synchronous planner expands mixed item lists (directories
  via remotefs.Walk / listing.Walk / WalkDir, empty folders collected on
  the way; S3 folder markers are recreated as real directories, never as
  marker objects) and the background job streams reader→writer under the
  per-source engine locks (acquired in sorted-ID order, so multi-source
  jobs can never deadlock) with the conflict policies
  (overwrite/skip/rename), byte-level progress and the bandwidth
  throttle. Fast paths: same-profile S3→S3 is a server-side copy,
  local→S3 reuses the multipart uploader, and a same-engine remote copy
  drains through a temp file first (FTP engines allow exactly one data
  connection). move is copy-then-delete per item and a source item is
  deleted only when every file under it verifiably transferred — a
  skipped file is not a success, so the source keeps it. New transfer
  package exports: `UploadReader` (streaming upload) and
  `NewProgressReader` (progress + throttle wrapper for engine streams),
  exercised end-to-end by hermetic tests over the local engine.
- Cross-source transfers (M10 GUI): the full matrix is wired into the
  UI — the clipboard records its origin (S3 bucket, remote source or
  local-pane paths) so Copy/Cut/Paste (Ctrl+C/X/V, Edit menu, row and
  tree menus) work from and to any side; drag & drop carries the origin
  in its payload, so rows drop onto S3 folders, remote folders (grid and
  sidebar tree) and the local pane with Explorer modifier rules (copy by
  default, Shift = move, same-source = move with Ctrl to keep a copy);
  remote views gain Download (Ctrl+D), Upload (Ctrl+U, OS file drop,
  empty-area and tree menus) and Paste; the local pane gains a row menu
  (Open/Copy/Cut/Properties) and Paste in its empty-area menu; opening a
  remote file downloads it. Jobs surface in the existing transfer
  manager (⇄ icon) and the open views refresh when a job finishes. Two
  stale "ships next" hints were made honest along the way.
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
- **GUI visual harness** (`npm run gui-visual`, dev-only): a Playwright
  walk of every GUI surface — boot/onboarding, buckets/objects/remote
  views, all five menubar dropdowns, every context menu, upload menu,
  settings/sources/doctor/deep-search/versions+compare dialogs, transfers,
  dual-pane with local/S3/remote bindings, directory compare, log drawer —
  against a fake backend injected ahead of the Wails bindings, with
  synthetic HTML5 drag & drop exercising the full transfer matrix and
  asserting the exact backend payloads. Screenshots plus a JSON report land
  in `testartifacts/gui/`; failures dump the last backend calls and the UI
  state (breadcrumb, tree, rows, selection) for triage. Runs headless on
  Edge/Chrome/Chromium with per-run `s3b-*` localStorage isolation, and a
  `gui-visual` CI job runs it on every push; `scripts/js-check.sh`
  syntax-checks the harness itself.

### Fixed

- Settings dialog controls were squeezed to a fixed 200px track:
  `.set-ctl` used `min-width` without `width: auto`, so selects and
  inputs rendered at the minimum instead of their natural size and
  longer labels/units wrapped inside the control. The control now sizes
  to content (found in the manual GUI pass).
- Finished downloads never refreshed the dual-pane local view: the
  `transfer:update` handler refreshed only jobs whose id started with
  "transfer" (cross-source transfers), so a completed download job left
  the local pane stale until the next manual refresh. The refresh now
  keys off the job's `op` — cross-source transfers refresh both sides,
  downloads refresh the local pane; uploads still arrive via `s3:changed`
  (found by the live GUI harness).
- Flat-file downloads nested under their object prefix: dragging
  `zz-live/live-b.txt` onto the side pane produced
  `downloads/zz-live/live-b.txt` instead of `downloads/live-b.txt`, for
  every download whose items came from the grid. `DownloadItem` gains an
  optional `Local` override for the path under the destination folder and
  `DownloadRefs` sets the basename for dragged files — folder references
  keep their structure (found by the live GUI harness).
- The source editor's Test button dialed the saved source, not the form:
  with an existing source loaded, Test probed the stored
  endpoint/credentials even with unsaved edits in the dialog (and saved
  sources only — a brand-new unsaved source could not be tested at all).
  A new `TestS3Draft` binding tests exactly what is in the form, before
  you save (found by the live GUI harness).
- `s3b versions undo` (CLI and GUI) trusted any `--version-id`: S3 honors a
  delete of any version id as an idempotent success, so a typo'd id printed
  "object is back" while the real delete marker stayed current and the object
  stayed hidden — and the id of a *real* version destroyed that version
  permanently under the same success message. Undo now verifies the id is a
  delete marker of that key's timeline before deleting and fails with an
  explicit error otherwise (found in live validation against a versioned
  Hetzner bucket).
- The side pane's breadcrumb showed the internal source id (e.g.
  `src-abc123:/`) instead of the source's name; the path-prompt titles had
  the same problem. Both now show the name the user configured.
- The "[WebView2] Environment created successfully" popup behind the GUI is
  gone: the go-webview2 startup line (an unconditional std-log print) is
  discarded in GUI mode — application events still go to the shared event
  log shown by `s3b log` and the in-app log area.
- FTP engine against vsftpd and other no-MLST servers: single-entry stat
  (Stat, MkdirAll's segment verification, Remove) fell over the client
  library's synthetic 502 when the server implements neither MLST nor
  MLSD — vsftpd, the common Linux FTP server, does not. Stat now falls
  back to listing the parent and matching the basename, works everywhere
  LIST does, and reports a missing path as a proper not-exist error.
- FTP engine: `Open` issued RETR before SIZE; the transfer-complete
  reply still pending on the control channel desynced the SIZE that
  followed, so downloads silently reported size 0. SIZE now precedes RETR.
- FTP engine: `MkdirAll` sent unanchored paths, creating the tree at the
  server root instead of inside the source root. Every segment is now
  anchored like every other engine operation.
- FTP engine: 550 replies surfaced as raw protocol errors, so neither
  `os.IsNotExist` nor `errors.Is(err, fs.ErrNotExist)` recognized a
  missing path; they now map to a PathError over `fs.ErrNotExist`,
  matching the local and SFTP engines.
- CLI: copying a single file onto a folder-style remote destination
  (`cp file.txt name://dir/` or a URI naming an existing folder) nested
  the file as `dir/file.txt/file.txt` — the destination resolver
  appended the filename while still flagging folder mode, so the copy
  loop appended it a second time. Folder destinations now always land
  `dir/file.txt`.
- `source add` help no longer claims the remote-filesystem engines "ship
  next" — they shipped; the shorthand URL form is documented instead.
- Generated source IDs re-roll on nanosecond-clock collisions (observed
  on Windows), which could silently replace a just-added source with the
  next one.
- Windows: launching the GUI no longer opens an empty console window behind
  the app. The binary keeps its console subsystem (the CLI needs it), but
  GUI mode now detaches the console at startup (`FreeConsole`), so
  double-click / Start-menu launches are popup-free; CLI behavior and
  launching from a terminal are unchanged.
- CI: the e2e-minio job pulls MinIO from `quay.io/minio/minio` — the
  `minio/minio` Docker Hub repository is gone (pulls fail with "repository
  does not exist"), which broke the job outright.

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
