# GUI usage guide

Everything you need to drive the s3b desktop app day to day. The same
content lives in the app (Help → **User guide**, F1) and in the
supported-sources overview (Help → **Supported data sources**); this
page is the long-form version with screenshots. The terminal face of
the same binary has its own [CLI quickstart](cli-quickstart.md) and
[CLI reference](cli.md).

```
Table of contents
  1. Getting started
  2. Supported data sources
  3. Browsing
  4. File transfers — and migrating between sources
  5. Versions & the safety ladder
  6. Bucket administration
  7. Security features
  8. Keyboard map
  9. CLI quick reference
```

---

## 1. Getting started

Install the [release artifact](https://github.com/MikkoP88/s3-bucket-browser/releases) for your platform (or
[build from source](build.md)) and start it: `s3b`
with no arguments opens the desktop app, `s3b <args>` is the CLI —
one binary, same engine.

![First launch — onboarding](screenshots/onboarding.png)

### Add a data source

Click the **+** next to *DATA SOURCES* in the sidebar (or the button on
the empty state). Every connection — an S3 endpoint, an SFTP server, an
FTP site, a WebDAV share, a local folder — is a *data source*; click one
in the tree to browse it in the main view. Sources are color-coded, and
each carries a live connectivity ball (green = reachable).

![Data sources in the sidebar](screenshots/sources-tree.png)

### Import existing credentials

Don't retype what you already have. **Import S3 Credential** (File menu
or the welcome screen) reads:

- **AWS INI** — `~/.aws/credentials` + `~/.aws/config`; profiles with an
  `endpoint_url` become MinIO / R2 / Wasabi / … sources, plain profiles
  connect to Amazon S3
- **rclone** and **JSON** config files, `.env` files
- **Encrypted `.s3bprofile`** containers (your saved workspaces)
- **KMS / secrets services** — HashiCorp Vault, AWS Secrets Manager,
  Azure Key Vault, GCP Secret Manager — including fully custom HTTP
  endpoints (URL / JSON path / headers)

Every candidate is listed with a live **bucket-count test** before you
commit. Picks accumulate: add from several files and services in one
go, remove any row (or clear the list), then import the checked ones
— the imported bucket's content opens immediately.

![Credential import with live test](screenshots/import-credentials.png)

### Save your workspace

Data sources live in the session until saved — the status bar counts
unsaved sources. **Ctrl+S** / File → *Save As* writes an encrypted
`.s3bprofile` (scrypt + AES-256-GCM, password of your choice) you can
reopen, keep or share. On first launch the welcome screen disappears
for good as soon as one source exists.

### Where secrets live

Keys and passwords go to the **OS keyring** — Windows Credential
Manager, macOS Keychain, Linux SecretService — with a `0600`-permission
file fallback on headless hosts. Secrets are masked in all output and
never written to logs. See [security.md](security.md) for the full
model, including the opt-in **Secure Storage** mode that encrypts the
whole source store as one AES-256-GCM envelope.

### If the app won't start

A launch with no window and no error is almost never a broken install —
it's leftover state from a hard-killed session. When the app is killed
outright (power loss, crash, force quit), its WebView2 helper processes
can survive and keep holding the browser-profile lockfile, and every
later launch used to block on that lock silently. The app now clears
those orphaned helpers itself before opening a window, and if the
window still hasn't appeared after 60 seconds it says so — a message
box (Windows) or terminal line (Linux/macOS) instead of an invisible
hang, with the details in the event log (**Ctrl+L**). Starting it
again after that message normally just works.

---

## 2. Supported data sources

One UI, one CLI, one transfer engine — the source type changes, nothing
else does.

| Kind | Protocols | Notes |
|---|---|---|
| S3-compatible | `s3://` | Amazon S3, MinIO/AIStor, Wasabi, Cloudflare R2, Backblaze B2, DigitalOcean Spaces, IBM COS, Hetzner Storage Boxes, Ceph RGW, Dell ECS, NetApp StorageGRID — provider quirks are detected and surfaced before a request fails |
| Remote filesystems | `sftp://` `scp://` | SSH; host, port 22 default, password auth, anchored root path |
| | `ftp://` `ftps://` | plain and TLS; ports 21/990 defaults |
| | `webdav://` `webdavs://` | RFC 4918 over HTTP(S); Apache, nginx, rclone serve webdav, Nextcloud, IIS |
| Local filesystem | — | the secondary pane (F9) browses local drives; any source type can be bound to it |

An S3 source is either **account-wide** (all buckets the key can list)
or **bucket-scoped** — `s3b source add s3://my-bucket` or the `--bucket`
option pins it to one bucket, which is what credential imports create.
On the CLI every source is reachable as a URI (`hetzner://bucket/prefix`,
`vault://media/…`) with the same flags everywhere.

All of them browse, upload, download, rename and delete like any other
source — and join the same transfer matrix (drag & drop, copy/paste,
directory compare) with S3 and the secondary pane.

---

## 3. Browsing

![Main window](screenshots/main-view.png)

- **Sidebar tree** — sources → buckets → folders. Click to navigate;
  right-click a node for Properties, the Admin panel, transfers and
  more. Folders expand lazily; buckets with versioning carry a 🔄 icon,
  object-lock buckets a 🔒 icon. Drag the sidebar's right edge to
  resize it (double-click resets).
- **Data sources filter** — the funnel left of the **+** in the sidebar
  header narrows every data source at once. A plain word matches
  anywhere in a row's name (case-insensitive substring); `*` and `?`
  are wildcards (`prod-*`, `?ightly`); space- or comma-separated
  patterns OR together. Matches surface through the whole hierarchy —
  buckets and folders inside sources, not just source names — with
  pass-through ancestors dimmed, and the pattern narrows the Favorites
  list to matching buckets too. Everything the tree has loaded filters
  as you type; emptying the box restores the tree, Enter applies at
  once, and Escape closes the panel but keeps the pattern. The
  sidebar's bottom bar counts the tree at all times — *n* items with
  no filter, *n* shown while one narrows it — taking over the plain
  item count the window status bar used to carry (that bar now speaks
  only for a selection). When a pattern matches nothing loaded, the
  no-match note offers **Search all folders**: the walk loads the
  unloaded depths of every source in the background (the bottom bar
  names the source being scanned, and its Stop button cancels and
  opts back out) so deep matches appear without opening anything.
  The funnel icon and the bottom bar hide themselves when there are
  no data sources.
- **Connection status** — the ball beside a source's name says whether
  it is reachable: green connected, red connection problem, busy while
  a probe runs (hover for the exact state; right-click the source for
  Test / Reconnect). The source behind the view you are looking at is
  probed once a minute, so a connection that drops mid-session shows
  itself without any refresh: a slim strip appears above the content —
  *not connected — showing previous content* — while the rows you were
  browsing stay on screen. **Reconnect** (on the strip, on a failed
  view's error panel, or in the source's right-click menu)
  re-establishes the connection and reloads the view; a failed load
  names its cause ("Source not connected", "took too long to load")
  instead of a generic error; and when the connection comes back on
  its own, the strip clears, the view refreshes itself and a toast
  says so. The strip's × hides it until recovery.
- **Grid** — Windows-Explorer selection: click, Ctrl+click, Shift+click,
  Ctrl+A (all), Ctrl+I (invert), marquee drag-select, type-to-jump. The
  funnel row under the header filters per column; right-click the header
  to pick which columns show; Ctrl+F focuses the quick filter. The Type
  column names common file types ("PNG image", "Text document") in the
  UI language — anything else shows as an extension file ("DAT file") —
  and is on by default. Every column shows or hides in Settings → View
  (Name is always on); the out-of-box set is Name, Type, Size and Date
  modified. Opt-in columns: Date created (bucket views, WebDAV
  creationdate, Windows local birth times), Mode (local and SFTP
  permission bits), Storage class and ETag — via the header menu or
  Settings. Columns resize by dragging a header edge — the edge
  follows the pointer exactly, and growth beyond what the pane has
  becomes the horizontal scrollbar rather than squeezing the other
  columns (double-click resets; Arrow keys resize a focused edge) —
  and reorder by dragging the header itself; widths and order persist
  per pane, and the header picker's Reset columns brings the
  out-of-box set, order and widths back in one click.
- **Content bottom bar** — the strip along the bottom of the content
  view answers *how much is really here*, with the active data
  source's name in bold on the bar's right end: with nothing selected
  it is the real
  recursive size of everything listed; select rows and it is the
  recursive size of the selection — a folder always counts its whole
  interior, WinSCP-style. It works on every source type the view
  hosts — bucket lists, S3 folders (versioned, suspended or plain)
  and every remote engine (SFTP, SCP, FTP(S), WebDAV, local) — always
  for the one source you are browsing, never an aggregate across data
  sources. On buckets with versioning the history is priced in next
  to the live content (noncurrent version bytes and delete markers —
  the real storage bill). Walks run in the background
  ("calculating…" meanwhile), are cached per folder so revisiting is
  instant, and a failed walk falls back to listing-level sums marked
  *partial — at least this much* with the error on hover.
- **Path bar** — the breadcrumb shows where you are; click it (or the
  edit icon) and type a path like `s3://bucket/folder/` — or
  `source://path` for any source — to jump directly. Back / forward
  history works like Explorer. The parent-directory row ("..") is the
  first row of the listing itself, WinSCP Explorer-style — it scrolls
  with the content, one click climbs a level from any content view,
  and it floats over the empty-folder note so even an empty folder
  shows its parent. Where nothing sits above it — a source's top
  level, including a bucket-scoped source's root (its home IS the
  bucket's contents; the account bucket list is unreachable) — there
  is no row at all, and it steps aside while a loading,
  error or first-run panel owns the area. The feature is opt-in as a
  whole (Settings → View, off by default — the View menu carries the
  same toggle): while hidden there is no row anywhere and the climb
  keys rest; while shown, Alt+↑ and Backspace climb from the
  keyboard (the toolbar's Up button is retired), and
  back / forward wear arrow icons. The row wears the same folder glyph
  as every folder row — a backward-arrow badge riding its corner
  marks the climb — beside the `..` caption, seated in the Name
  column exactly where a folder's own name sits — resizing or
  reordering the columns never breaks the seat.
- **Favorites** — star buckets and folders for one-click jumps.
- **Search** — Ctrl+Shift+F opens the Search window (also the toolbar
  button, View → Search, or *Search in this folder…* from a context
  menu — the menu presets the scope): WinSCP's Find window,
  modernized — a filter form pinned on top, and below it a results
  area wearing the app's own content-area chrome: the same column
  header, hairline rows and bottom bar as the main view, framed as a
  content panel by the app's standard hairline border, seated at the
  form's side inset. **Name**
  and **Sources** sit side by side, always in view: Name is a
  substring or glob (Enter runs the search from any field),
  Sources is a flat list with no grouping — *All data sources*
  (the default: every bucket of every S3 source plus every remote
  source from its root) and each configured source by name, an S3
  source searched across every bucket it holds, a remote source
  (local, SFTP, FTP, WebDAV) from its root. Everything else folds
  out under the *More filters* disclosure chip, whose label counts
  the filters you have set: **Kind** (any/files/folders),
  **Limit**, **Extension** (comma list, dot optional), **Path
  contains**, **Larger/smaller than** and **Older/newer than** —
  every filter works the same across S3 and remote sources
  (storage class never could, so it left the window). A **Clear**
  button resets the whole form. Results stream in as grid rows
  with type-matched icons (folder, image, video, archive,
  document …); clicking a column header sorts ascending, again
  descending; a run spanning several origins adds a **Source**
  column at the end of the row, a single-source run drops it; a
  double-click (or Enter)
  jumps to the object and selects it. Only the results scroll —
  the form stays put — and a running search can be stopped from
  the window.

![Search](screenshots/search.png)

- **New file** — the WinSCP flow: Shift+F4, the 📄+ toolbar button or
  *New file…* in the context menu opens a small dialog for a file name
  and type (a dozen common extensions, or none). The empty object is
  created first, then handed to the editor you pick — the OS
  "Open with" chooser by default, respecting the Settings → Editing
  choice. Cancelling the picker, or having no app, still leaves the
  created empty file behind; nothing is lost. The dialog previews the
  exact object name live, and the name composes the way you expect
  (`notes` + `md` → `notes.md`; an extension already in the name is
  not doubled). Works on S3 and on remote sources (creation only);
  shows as a 📄 *Creating* task in Running tasks.

- **Dual pane** — F9 or the **Dual-pane** toolbar button opens an
  optional secondary pane beside the main view: a full twin of the
  content area — its own toolbar (upload, download, new folder, new
  file, compare, search), interactive breadcrumb, quick filter,
  back/forward history, parent row and status line. It opens on the
  workstation's home folder (like the old Panes panel, never an empty
  stop) or wherever it last stood; right-click any data source, bucket
  or folder in the tree and choose **Open on secondary pane** to land
  there directly, and if the remembered source ever disappears the
  pane's source picker stands up to choose another.
  The pane's path bar carries the main one's every function: click its
  empty area and the breadcrumb becomes an editable line holding the
  pane's canonical path — copy it out, paste any source's `Name://`
  path to point the pane there (a bare directory navigates the local
  side, and a pasted path binds an unbound pane directly), Enter
  goes, Esc cancels.
  Any source type binds to it — S3, SFTP, FTP, WebDAV, local drives —
  and it remembers where you left it, reopening there. Drag between
  panes transfers, and **Compare** color-codes newer / older /
  size-diff / only-here between the two sides. With the pane open,
  clicking the **Dual-pane** button again does not close it: a small
  picker appears under the button offering where the pane should
  point — **Home view** starts over at the workstation's home folder
  with the history cut clean, and **Return to last view** jumps back
  to the spot the pane stood on when it was opened (through history,
  so Back undoes the jump; the option rests disabled when nothing was
  remembered). Escape or a click outside dismisses the picker and the
  pane stays open throughout — the pane's × or F9 remains the honest
  close. When the columns run narrow each toolbar folds its
  word labels down to icons on its own, so neither side overflows.

![Dual pane with directory compare](screenshots/dual-pane-compare.png)

- **Floating windows** — the views you keep an eye on — File transfers,
  Running tasks, Search, the User guide, the keyboard map, the sources overview,
  the license information, the connection Doctor — open as draggable
  non-modal popouts: no
  grey-out mask, the app underneath stays fully usable while a transfer
  crawls or you read the guide. They resize from the corner grip,
  stack like real
  windows (any press raises one, Escape closes the topmost), each
  remembers where you left it — position **and** size survive every
  reopen, and only closing the app forgets them — and reopening a view
  just focuses its floating window instead of stacking a duplicate.
  Clicking **any** app window — the main one or a popout — brings the
  whole group forward above other applications (the clicked window on
  top, the rest keeping their stacking), so the app never ends up
  scattered window by window behind other programs.
  File transfers and Running tasks are the exceptions: their width is
  a fixed 490 px footprint that never moves (native windows are not
  user-resizable at all; the grip adjusts the height only), and what
  they remember between reopens is placement, not size.
  Native popout
  windows open centered on the display the app window is on
  (multi-display aware — they follow the app onto whatever monitor it
  lives on); Settings → View → *Popout windows open centered on* pins
  them to the app window's own center instead. A native popout carries
  only the OS window chrome — the in-page header stays hidden, so there
  is no second title bar or close icon.
- **Light/dark theme** and 15 built-in languages (English default,
  auto-detect optional) — switchable in Settings.
- **Settings commit on an explicit Save** — every control in the
  Settings dialog stages into a draft until you press Save (disabled
  until something changes; Save applies everything and closes the
  dialog). Closing with unsaved changes asks before discarding, and
  Reset to defaults stages the defaults without applying anything. The
  Security page is the one live control — enabling it rewrites the
  store on the spot.
- **Engine tuning in Settings** — Settings → Network sets the *Listing
  timeout* (cuts off listings, object stats, share-link generation and
  source tests on a silent endpoint; every page of data resets the clock
  — default 30 s), the *Compare timeout* for one deep pane-to-pane
  compare walk (default 5 min) and *S3 retry attempts* per request
  (default 3). Settings → File transfers → *Transfer engine* sets the
  multipart *Part size* and *Parts in flight* (Auto = SDK defaults) and
  the *Stall threshold* that flags a transfer row Stalled when no bytes
  move (default 10 s). Values persist in the config folder
  (`appsettings.json`), apply without a restart and clamp to safe
  ranges.

![Dark theme](screenshots/dark-theme.png)

---

## 4. File transfers — and migrating between sources

Every combination of the sources above transfers through the same
engine, which makes **migration** — S3 → SFTP, WebDAV → S3, local →
R2, MinIO → Wasabi — a drag or a `cp` away.

- **Upload** — the toolbar ▲ button and every context menu open one
  Upload flyout: **Files… (Ctrl+U)** picks files, **Folder…** a whole
  directory tree — or just drag files/folders from the OS anywhere onto
  the window.
- **Download** — toolbar ▼, Ctrl+D, Enter, or the context menu.
  Multistep transfers are multipart and resumable per file. Dragging rows
  **out of the window** is a download too: a plain drag of a files-only
  selection onto Explorer, Finder or the desktop drops real files (staged
  through the transfer engine and streamed — no modifier key); releasing
  the same gesture back over the app window is an internal move/copy.
- **Copy & move** — Ctrl+C / Ctrl+X / Ctrl+V, or drag rows onto folders,
  the tree, or the other pane. Same-source S3 copies run **server-side**
  (no traffic through you); hold Shift while dragging to force a move;
  cross-source drags spool through an encrypted temp workspace when
  Secure Storage is on.
- **Shared with File Explorer** — Ctrl+C in Explorer, Ctrl+V here: the
  copied files upload into the open folder. The other direction works
  too: Ctrl+C on remote rows quietly mirrors small selections (≤ 500
  files / 256 MB) onto the OS clipboard through a hidden staging
  download — never a row in File transfers, never an auto-opened window
  — so Ctrl+V in Explorer pastes real files; pasting inside the app
  still uses the reference copy and runs the transfer then. Copying
  real local files in the secondary pane's local view hands them to the OS
  clipboard directly. Cut never mirrors — an Explorer paste of a cut
  would move. **Last copy wins** on both sides. Machines that must not
  touch the OS clipboard can turn the whole bridge off: Settings →
  File transfers → *Explorer copy & paste* (on by default).
- **Text instead of files** — the context menu (or Edit → Copy as)
  copies names, full paths or `s3://` URIs to the OS clipboard.
- **Conflicts & speed** — every transfer states a conflict policy
  (overwrite / skip / rename) with a live pre-check that lists exactly
  which files collide, and can be throttled (256 kB/s … 1000 MB/s).
- **Transfer manager** — View → File transfers (or the status-bar
  counter) shows every job with per-file and byte-level progress, speed
  and cancel — in a floating window, so you can keep browsing while it
  runs. The window also opens **itself** the moment a transfer starts
  and closes itself when the batch ends cleanly (Settings → File
  transfers → *Transfer window auto open/close*, on by default). The
  automatic close is careful: a failed or canceled transfer keeps the
  window on screen, simultaneous transfers close it only when the last
  one finishes, and a window you opened by hand never closes on its
  own. A freshly opened window shows **what happened since it opened**;
  finished jobs from before the open sit behind a *Show history* toggle
  (with a count of hidden rows) — so a glance answers "what is running
  right now" without the noise of an all-day backlog. The toggle stays
  available whenever anything finished exists: *Hide history*
  collapses all finished rows on demand — including jobs that finished
  inside the open view — leaving just the live work. **Clear** removes
  the finished rows you can see (all of them once history is shown);
  running jobs always stay.
- **Running tasks** — View → Running tasks (or the status-bar ⚙
  indicator) is the everything-monitor. The ⚙ indicator always shows
  a live count of active tasks (e.g. "⚙ 2 tasks — search 3/10") and
  clicking it opens the Running tasks window. The list covers transfer
  jobs, searches, bulk deletes, version purges, bucket emptying,
  storage-class conversions, folder and file creation, bucket deletes,
  pane compares, doctor runs — every action
  the app is taking, each with progress and a **Cancel** button. Kill a
  task that hangs or runs too long; destructive tasks count before they act, so canceling
  during the counting phase destroys nothing. Bulk operations run as
  tracked tasks with no fixed time limit — they finish, or you stop
  them. The window keeps the same session history as File transfers —
  pre-open finished rows hide behind *Show history*, *Hide history*
  collapses everything finished on demand, and **Clear** retires what
  is visible.

![Transfer manager](screenshots/transfers.png)

Closing the window or quitting while transfers or other tasks still
run — or with unsaved profile work — asks first; nothing is dropped
silently.

---

## 5. Versions & the safety ladder

The feature set most S3 tools treat as an afterthought.

- **Versioning** — enable it per bucket (Admin panel or
  `s3b bucket versioning s3://b on`). Versioned buckets show 🔄 in the
  tree; an object's context menu → **Versions** opens its timeline:
  restore a previous version as latest, view text diffs between
  versions, or destroy specific versions.

![Object version timeline](screenshots/versions.png)

- **Undo delete** — deleting on a versioned bucket leaves a *delete
  marker*; **Versions → undo delete** brings the object back in one
  click (bulk-select works in the marker window). Rows with markers in
  their history carry a ⛔ badge; a folder whose every file is
  delete-marked shows *all deleted*. The **Show delete marker icons**
  toggle (Settings → View, default off) hides delete-marker versions
  from every version view at once — the grid badges, the Versions and
  Content Versions windows and the Delete Marker window itself, which
  then lists nothing and offers the toggle inline (the context menu
  keeps opening it, so undo-delete stays reachable). Bucket admin
  version stats and pre-delete counts always show the true numbers.
- **One Delete Window for every destructive op** — every delete counts
  first and acts second, and every destructive flow — selection deletes,
  bucket delete, destroying a single version, purging noncurrent
  versions or markers, force-emptying a bucket — confirms in the same
  window: target, counted stats, an amber consequence line and the
  typed partition when the ladder asks for one. Selection deletes on
  versioned buckets choose between three types:
  1. **Add a delete marker** (default) — everything stays restorable
  2. **Delete all except current version** — keep the latest, clear
     the history
  3. **Delete permanently** — purge every version and marker
  The destructive types state their consequence in an amber line and ask
  for a typed `delete` when *Require typing "delete"* is on; Shift+Del
  jumps straight to the permanent path. Settings → Deleting →
  *Always use the delete window* (default on) routes everything through
  the window; with it off, plain deletes fall back to the classic
  compact confirm — the typed ladder applies either way.

![The Delete Window](screenshots/delete-window.png)

- **Object Lock** — enable at bucket creation only (permanent);
  per-version retention (GOVERNANCE / COMPLIANCE) and legal hold, with
  the same confirm gates as the CLI.
- **Bulk tools** — folder-level *Content Versions* overview, per-child
  version counts, bulk purge of noncurrent versions
  (`s3b versions purge`), and force-emptying a versioned bucket
  (markers included) via `rb --force`.

The ladder, formally:

| Level | Operations | Gate |
|---|---|---|
| L0 | delete selection, overwrite upload, purge ≤50 | count + confirm |
| L1 | ≥50 items, prefix delete, bulk class conversion | `--force` (CLI) / typed confirm (GUI¹) |
| L2 | empty/remove bucket, purge >50 | type the **bucket name** / `purge` |
| L3 | destroy versions & markers | explicit destructive choice + typed `delete`¹ |

¹ Every typed-`delete` gate follows Settings → Deleting → **Require
typing "delete"** (default off — the counted delete window / classic
confirm is the base guard; the setting adds the typed word everywhere,
window and classic alike). Typing the **bucket name** — and the `purge`
escalation word for >50-item purges — is an identity check, not a
delete guard, and always applies.

---

## 6. Bucket administration

Right-click a bucket → **Admin panel**: one tabbed dialog for
versioning, policy, ACL, CORS, lifecycle rules, default encryption,
public-access block, static website and tags — plus a versions overview
and the object-lock tab. Everything the CLI's `s3b bucket …` tree
offers, same confirm gates.

![Admin panel](screenshots/admin-panel.png)

- **Doctor** — Help → Doctor opens a picker of your S3 sources — pick one
  and the guided diagnosis runs: DNS → TCP → TLS → auth → permissions,
  with plain-language remediation and one-click re-runs of individual
  checks — in a floating window that doesn't block the app while it runs.
  Right-clicking a bucket or data source → **Doctor…** skips the picker
  and diagnoses that target directly (same window, same checks).
- **License** — Help → License shows the app's license identity (PolyForm
  Internal Use 1.0.0) and a third-party attribution summary pointing at
  the NOTICE file and release SBOM; the About box carries the same
  one-line identity from a single shared source.
- **Presign & storage class** — the context menu creates time-limited
  pre-signed URLs (computed locally) and converts objects between
  storage classes via server-side self-copy.
- **Properties** — full metadata for buckets, folders, objects and
  sources: provider, region, versioning, lock, encryption, policy
  state.

![Connection doctor](screenshots/doctor.png)

---

## 7. Security features

The short version of [security.md](security.md):

- **Secrets in the OS keyring** — never in files when a keyring exists;
  `0600` file fallback on headless hosts; masked in all output.
- **Encrypted profiles** — saved `.s3bprofile` workspaces are
  scrypt + AES-256-GCM containers.
- **Secure Storage** (Settings → Security) — opt-in mode that encrypts
  the whole source store as one AES-256-GCM envelope keyed from the OS
  keyring, moves temp workspaces into the protected config folder
  (wiped at every launch), turns file logging off, and auto-clears
  pre-signed URLs from the clipboard after 60 s. Built for terminal
  servers, jump boxes and USB-stick installs.

![Secure Storage in Settings](screenshots/settings.png)

- **Portable mode** — drop an empty `s3b-portable` marker file next to
  the binary and all settings stay beside it — perfect for USB sticks
  ([README-portable](../README-portable.md)).
- **No telemetry, ever** — no metrics, no crash reports, no update
  pings; the binary talks only to the endpoints you configure.

---

## 8. Keyboard map

The full map ships in the app (Help → Keyboard shortcuts). The ones
you'll use daily:

| Keys | Action |
|---|---|
| Ctrl+F | focus quick filter |
| Ctrl+Shift+F | search window |
| Ctrl+U / Ctrl+D | upload files / download selection |
| Ctrl+C / Ctrl+X / Ctrl+V | copy / cut / paste (in-app paste runs the real transfer; copy also mirrors small selections onto Explorer's clipboard) |
| Ctrl+A / Ctrl+I | select all / invert selection |
| Del / Shift+Del | delete window / permanent path |
| F9 | dual pane |
| Ctrl+L | event log |
| Ctrl+S | save workspace profile |
| F1 | help (in-app guide + key map) |

---

## 9. CLI quick reference

Same binary, same engine, same sources — `s3b` with arguments is the
CLI. The guided tour is the [CLI quickstart](cli-quickstart.md); the
complete tree is in [cli.md](cli.md). The shape of it:

```bash
s3b profile add lab --endpoint http://localhost:9000 \
    --access-key minioadmin --secret-key minioadmin --default
s3b source add vault sftp://deploy@backups.example.com

s3b ls            s3b ls vault://media      # any source, same flags
s3b cp ./site s3://b/site/ -r               # upload (or download, or copy)
s3b cp s3://src-b/ sftp://host/dst/ -r      # cross-source migration
s3b sync ./site s3://b/site/ --delete
s3b find s3://b --name 'backup*' --older 90d
s3b find s3://b --ext pdf,csv --path docs --larger 1MB
s3b doctor s3://my-bucket

s3b versions ls s3://b/docs/report.pdf      # timeline, newest first
s3b versions undo s3://b/docs/report.pdf --version-id MARKER
s3b versions purge s3://b --mode noncurrent --dry-run
s3b rb s3://old-bucket --force              # empties versioned buckets too
```

Every command takes `--json`, `--profile` and `--verbose`; shell
completions: `s3b completion bash|zsh|fish|powershell`.
