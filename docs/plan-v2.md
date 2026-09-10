# s3b v2 — Comprehensive Improvement Plan

Grounded in the v1.0.0 codebase. Milestones are independently shippable; each
lands green (gofmt/vet/tests/e2e, CI on all 6 targets) before the next starts.
Numbering continues from v1's M0–M6.

## Architecture decision: the storage abstraction (drives M8–M10)

Today every path goes through `pkg/core/s3client` + `bucketops`; the frontend
nav model is `{kind:'objects', bucket, prefix}` and the sidebar assumes one
active S3 profile. Multi-source support needs a narrow filesystem interface
so S3, local FS, SFTP/SCP and FTP(FPTS) all look the same to UI and transfers:

```
pkg/vfs
  Entry     { Name, IsDir, Size, ModTime, ETag?, StorageClass? }
  FS        interface {
    Source() SourceRef                 // id, label, scheme, capabilities
    Roots(ctx) ([]Entry, error)        // buckets (S3) / "/" (local,sftp,ftp)
    List(ctx, path, token) (entries []Entry, next token, err)   // streaming
    Stat(ctx, path) (Entry, error)
    Mkdir(ctx, path) error
    Delete(ctx, paths []string, recurse bool) (Result, error)
    Copy(ctx, src []string, dstFS FS, dstDir string, move bool) (Result, error)
    Read(ctx, path) (io.ReadCloser, error)     // download streams
    Write(ctx, path, r io.Reader) error        // upload streams
  }
```

- S3 implementation wraps the existing s3client/bucketops (no behavior change;
  all existing tests keep running against it).
- Local implementation shares code with the dual-pane local listing API.
- SFTP/SCP (one engine, scp:// is served by SFTP) and FTP/FTPS engines
  implement the same interface.
- Transfers (pkg/core/transfer) gain an `fsCopy` driver that streams
  reader→writer between *any two* FS implementations — that single driver
  then covers local↔S3, S3↔S3, source↔source, panel↔panel.

## M7 — Shell & UX professionalism (no architecture change)

1. **Kill the console popup.** `go-webview2` logs "[WebView2] Environment
   created successfully" to stdout; because the exe is built without
   `-H windowsgui` (CLI needs a console), GUI launch shows a console window.
   Fix in `gui.Run`: on Windows call `kernel32.FreeConsole()` before
   `wails.Run` (GUI mode = no args ⇒ detach console; CLI mode untouched).
2. **Top menu bar** (native-feel, in-webview menubar component): File / Edit /
   View / Settings / Help. File: New Profile…, Open Profile…, Save Profile As…,
   Import AWS credentials, Exit. Edit: mirrors grid context actions (Cut/
   Copy/Paste, Select all, Rename, Delete). View: theme, panels (F9), log area,
   auto-refresh interval, filter focus, refresh. Settings: Data Sources…,
   conflict policy defaults, bandwidth throttle, language. Help: Keyboard map
   (F1), Doctor, About (version, publisher, license, third-party licenses).
   Every item carries the same shortcut as the toolbar and greys out when its
   action is unavailable (see 7).
3. **Optional log area** — collapsible bottom drawer (View menu + status-bar
   toggle). Log lines: timestamp, level (info/warn/error), scope (op), message;
   buttons: copy, clear, autoscroll toggle, level filter. Backend emits
   structured `log:line` events through the existing event bus; transfers,
   doctor, admin ops and source errors log there. Hidden by default.
4. **Auto refresh** — View submenu: Off / 5s / 10s / 30s / 60s + "refresh on
   focus". Ticker cancels during navigation (listSeq guard already exists);
   never triggers while a modal is open or transfers run against the open
   prefix.
5. **One Upload command** — single "Upload" button/picker that accepts files
   *and* folders (backend walks both). Toolbar gains space; Ctrl+U unchanged;
   upload-folder dialog removed.
6. **Checkbox column** — first grid column (both grids): per-row checkbox
   toggles selection independent of focus; header checkbox = select all/none/
   indeterminate. Checkbox state feeds the existing selection Set.
7. **Grey-out audit** — central `updateCommandState()` recomputes enabled/
   disabled for toolbar, menu bar and context menus from (location kind,
   selection size/composition, clipboard, transfers, profile presence).
   Context-menu items already have a disabled flag; extend everywhere.
8. **Empty-area context menus** — right-click on grid background (objects
   view: Paste, New folder, Upload, Download all…, Find, Refresh, Properties;
   buckets view: New bucket, Paste bucket-level?, Refresh), local pane body
   (Paste, New folder, Open terminal here…, Refresh), sidebar background
   (Add Data Source…, New Profile…, Import AWS, Collapse all, Refresh).
9. **Sidebar context-menu parity** — tree nodes get the same menu as grid rows
   (Open, Upload here, Download, Copy/Cut/Paste, Rename, Delete, Properties,
   Admin, Doctor, Find, Favorites). Sidebar root shows the *configured Data
   Source name* (v1 shows bucket names at top level — see M8).
10. **Doctor v2** — dialog lists each check with status pill, run timestamp,
    duration, and expandable raw detail (Advice + Info JSON) per test; buttons
    "Run all" and per-test re-run. Backend: `RunDoctorCheck(bucket, name)`
    runs a single check; Report carries StartedAt/FinishedAt.
11. **Publisher metadata** — Windows: VERSIONINFO resource (CompanyName
    Publisher "MikkoP88", ProductName, FileVersion, LegalCopyright) embedded
    via a checked-in `.syso` generated by `tools/versioninfo` (build-tool dep
    only — not linked into the app); NSIS publisher fields + ARP registry
    keys; macOS Info.plist already carries version, add publisher/credit.
    About dialog shows publisher + license.

## M8 — Data Sources & encrypted Profiles (model rework)

1. **Rename** Profile → **Data Source** everywhere (UI copy, API names where
   backward-compatible, CLI `s3b source …` with `profile …` as deprecated
   alias). A Data Source = one connection (S3 endpoint OR sftp host OR ftp
   host OR local root).
2. **Profile (new meaning)** = a named collection of Data Sources, saved to a
   single **password-encrypted file** (`*.s3bprofile`): AES-256-GCM, key
   derived with scrypt (golang.org/x/crypto — already a dependency), random
   salt+nonce per file, format `s3bpf1|salt|nonce|ciphertext`. File → New/
   Open/Save/Save As/Close in the File menu. Password prompt via existing
   dialog. Wrong password = auth error (GCM tag), no oracle.
3. **Session semantics** — with no Profile open, Data Sources live only in
   memory (existing per-session behavior): closing the app keeps nothing
   unless saved into a Profile file. The legacy auto-saved `profiles.json`
   remains for CLI use (`S3B_PROFILE_FILE` to point elsewhere); GUI "Open
   Profile" is the explicit path.
4. **Sidebar hierarchy** — top level = Data Source labels (source-local,
   colored dot); expanding lists its roots (buckets / remote dirs / local
   roots). Breadcrumb root becomes the source label.
5. Backend: `pkg/core/profile` gains `Source` records (`Type: s3|sftp|ftp|ftps
   |local`, per-type fields); `pkg/api` exposes Source CRUD + Profile
   open/save/close with password.

## M9 — New protocol sources (dependency-gated)

Candidates vetted for "well-maintained + valid license + minimal deps":

| Protocol | Library | License | Verdict |
|----------|---------|---------|---------|
| SFTP     | github.com/pkg/sftp + golang.org/x/crypto/ssh | BSD-2 / BSD-3 | **add** — canonical, maintained |
| SCP      | same engine (scp:// URLs via SFTP) | — | **add** (transport, not a separate dep) |
| FTP/FTPS | github.com/jlaffaye/ftp | MIT | **add** — maintained, zero transitive deps; explicit TLS = FTPS |
| SMB      | github.com/hirochachacha/go-smb2 | MIT | **defer** — pure-Go but release cadence too slow to meet the bar; documented, revisit |
| NFS      | none (no maintained pure-Go client) | — | **out** — documented rationale |

1. Engines behind the vfs interface (M8 refactor), each with a fakeserver-free
   unit suite for path mapping + an integration tier that is **skipped unless
   Docker is present**.
2. `scripts/e2e-docker-sources.sh` + CI job: `atmoz/sftp` (SFTP+SCP),
   `stilliard/pure-ftpd` (FTP/FTPS), plus existing MinIO (S3) → full matrix:
   local↔sftp, sftp↔s3, ftp↔sftp, recursion, special chars, resume/cancel.
3. CLI: `s3b source add sftp://user@host:22/path`, `s3b ls source://…`,
   transfers accept any pair of source URIs.

## M10 — Transfers, panels, versioning UX, parity, portable

1. **DnD/copy-paste matrix** — after the vfs driver lands, every drag payload
   carries `{sourceID, paths}`; every drop target accepts any payload; copy =
   default, Shift = move, Ctrl on same-source = move (current S3 rules
   generalized). Manual test script + automated e2e for each cell of the
   matrix (local, S3, SFTP, FTP).
2. **Panels v2** — the dual-pane becomes N-pane (2): each pane can bind any
   Data Source (or local). Compare works pane↔pane (same driver as dir
   compare); "Compare versions" on a versioned object opens a pane-pair with
   two selected versions diffable (size/ETag/time; content diff for text via
   a small viewer).
3. **Multi-run commands** — Properties/Storage class/lock/download/presign
   accept the full selection (some already do); add a selection summary bar
   (count, size, folders/objects) and batch progress with per-item status.
4. **Versioning/lock visuals** — object rows gain badges (versioned, locked,
   noncurrent, legal hold); Previous Versions dialog becomes a timeline strip
   with restore/destroy per version and a diff column vs current.
5. **CLI parity** — every M7–M10 feature mirrored: `s3b log` (tail events),
   `s3b source`/`s3b profile` (encrypted), `s3b autorefresh` flag on `ls
   --watch`, URIs for all sources, batch operations unchanged.
6. **Portable builds** — release job adds `s3b-<ver>-<os>-<arch>-portable.(
   zip|tar.gz)` = binary + `LICENSE` + `NOTICE` + `s3b-portable` marker +
   README-portable.md; the marker already redirects config beside the exe.

## Dependency & license policy (applies throughout)

- Direct deps stay: aws-sdk-go-v2 (Apache-2.0), wails (MIT-3rd-party-licensed
  bundle), cobra (Apache-2.0), go-keyring (MIT), fatih/color (MIT),
  zalando/go-keyring, samber/lo… — all maintained; no replacements needed.
- New: pkg/sftp, jlaffaye/ftp, x/crypto promotion to direct — all permissive.
- `go mod tidy` after each milestone; `godepgraph`-style review of the
  transitive set; anything unmaintained (>2y without fix releases, archived)
  gets replaced or dropped. wails' indirect set is pinned by wails itself.
- **License compliance**: NOTICE/THIRD-PARTY.md generated per release from the
  SBOM (already SPDX) — license text + version pin per module, plus the
  project's own MIT header. LICENSE audit in M7 (gofmt for the file list),
  surfaced in About + docs/security.md.

## Verification per milestone

`gofmt -l .` empty, `go vet ./...`, `go test -race ./...`, JS syntax check,
`scripts/e2e-minio.sh`, new docker source e2e (M9+), manual GUI smoke on the
rebuilt exe, CI green on all six targets, CHANGELOG entry, commit (no
attribution), push.
