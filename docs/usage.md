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
The same binary also builds windowless — the app served over HTTP
and driven from a browser tab on any machine — see the
[server mode guide](server.md).

![First launch — onboarding](screenshots/onboarding.png)

### Add a data source

Click the **+** next to *DATA SOURCES* in the sidebar (or the button on
the empty state). Every connection — an S3 bucket, an SFTP server, an
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

A second launch is also not a broken start — the app keeps one window
at a time on Windows: launching it again (double-click, taskbar, a
second shortcut) brings the instance that is already running to the
front, restoring it if it was minimised with its popout windows
lifted behind it, and the new process quietly exits. Two GUIs would
fight over the same browser profile, which is exactly the
invisible-hang shape above. Set S3B_MULTI_INSTANCE=1 before launching
to opt out and run two copies side by side (separate profiles, test
rigs).

Windows Server needs one thing client Windows has by default: the app’s
window is an Edge WebView2 view, and Server SKUs (and stripped-down
Windows installs) ship without the WebView2 Evergreen runtime. A launch on
such a machine used to die before any window existed — nothing happened
at all, no error anywhere. The app now checks for the runtime before it
tries to open a window and refuses loudly instead: a message box carrying
the fix (`winget install Microsoft.EdgeWebView2Runtime`, or the standalone
download from Microsoft’s WebView2 page). Install it once and the app
starts normally.

If that message box says another copy of the app is already running but
could not be raised, the previous instance is wedged — end the s3b
process in Task Manager (or reboot) and start again. And if Windows
itself warns about the build’s publisher or certificate, that is the
self-signed release identity: a one-time, per-machine import makes it
trusted — see [signing.md](signing.md), “Trusting the self-signed
certificate on a fleet machine”.

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

Every data source is exactly one root: an S3 source is one bucket, an
SFTP/FTP source is one server directory (or the whole host — still one
root), a WebDAV source is one endpoint path, a local source is one
folder. Nothing sits above a source's root — there are no
account-level sources. `s3b source add s3://my-bucket` (or the
`--bucket` option) is the bucket-scoped form the source editor
requires and credential imports build — one source per bucket, named
after it. Workspaces from older versions may still hold account-wide
S3 sources: each splits automatically — one bucket-scoped source per
visible bucket, named after the bucket — the first time the account is
reachable, while an unreachable account keeps its legacy source and is
retried on every sources refresh and reconnect (the editor's *Split
into one source per bucket…* button does it on demand, `s3b source
split NAME` on the CLI). **New bucket…** on the sidebar or an S3
source's context menu creates a bucket and seats its data source in
one step, and deleting a bucket removes its data source with it. On
the CLI every source is reachable as a URI (`team-files://docs/`,
`vault://media/…`) with the same flags everywhere.

All of them browse, upload, download, rename and delete like any other
source — and join the same transfer matrix (drag & drop, copy/paste,
directory compare) with S3 and the secondary pane.

---

## 3. Browsing

![Main window](screenshots/main-view.png)

- **Sidebar tree** — sources → folders; an S3 source IS the bucket. Every source wears
  its type as a bold text label (S3, SFTP, WebDAV, …) painted in its
  accent color — the same badge rides the breadcrumb roots and the
  source pickers. Click to navigate; right-click a node for
  Properties, the Admin panel, transfers and more. Folders expand
  lazily; buckets with versioning carry a 🔄 icon,
  object-lock buckets a 🔒 icon — hover one and its meaning surfaces
  as a soft tinted chip in the row's own color, never an underlined
  glyph (an underline strikes through an icon's feet and reads as an
  artifact). Drag the sidebar's right edge to
  resize it (double-click resets).
- **Data sources filter** — the funnel left of the **+** in the sidebar
  header narrows every data source at once. A plain word matches
  anywhere in a row's name (case-insensitive substring); `*` and `?`
  are wildcards (`prod-*`, `?ightly`); space- or comma-separated
  patterns OR together. Matches surface through the whole hierarchy —
  folders inside sources, not just source names — with
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
- **Toasts** — confirmations, completions and errors land as brief
  toasts stacked in the window's bottom corner, raised clear of the
  content bar and the secondary pane's info bar so they never smother
  the counts under them, and each carries a ✕ to dismiss it on the
  spot instead of waiting out its timer.
- **Grid** — Windows-Explorer selection: click, Ctrl+click, Shift+click,
  Ctrl+A (all), Ctrl+I (invert), marquee drag-select, type-to-jump. The
  funnel row under the header filters per column; right-click the header
  to pick which columns show; Ctrl+F focuses the quick filter. The Type
  column names common file types ("PNG image", "Text document") in the
  UI language — anything else shows as an extension file ("DAT file") —
  and is on by default. Settings → View holds the one Columns
  partition for every view — main view, secondary pane, Search
  window — under a chooser: the full catalog as an ordered
  check-list where a tick shows a column, the list order is the
  column order (drag a row — or press Alt+Up / Alt+Down on a focused
  row — to move a column), each row names
  its data type (Text, Number, Date & time) and Name stays locked
  on; the out-of-box set is Name, Type, Size and Date modified —
  the same set, in the same order, that the Search window starts
  at.
  Opt-in columns: Date created (bucket views, WebDAV
  creationdate, Windows local birth times), Mode (local and SFTP
  permission bits), Storage class and ETag — via the header menu or
  Settings. One catalog seats every view: the main view, the
  secondary pane and the Search window carry the same column set
  through their headers, their header pickers and that Settings
  partition, and a column whose data an engine does not carry
  (Storage class outside S3, Mode outside local and SFTP, Date
  created where no birth time exists) shows empty cells — the
  column never hides itself. Folder rows follow the same
  presence-driven contract: a folder shows its date, class and ETag
  where its source reports them — a bucket's creation date in the
  buckets view, a folder marker's own metadata on S3, a directory's
  real timestamps and mode on local, SFTP and WebDAV — and Size fills
  in lazily for folders too: a listing cannot know a folder's content
  size without walking it, so the cell starts empty and fills from a
  background usage walk a moment after the listing settles (the same
  walk the content bar and Properties ride), painted in place — no
  re-sort, no lost selection — on every view: bucket lists, S3
  folders, remote engines, the workstation, the secondary pane and
  Search results alike; a folder the engine cannot answer stays an
  honest blank rather than a wrong number.
  Columns resize by dragging a header
  edge — the edge follows the pointer exactly, and growth beyond
  what the pane has becomes the horizontal scrollbar rather than
  squeezing the other columns (double-click resets; Arrow keys
  resize a focused edge) —
  and reorder by dragging the header itself; widths and order persist
  per pane, and the header picker's Reset columns brings the
  out-of-box set, order and widths back in one click.
- **Content bottom bar** — the strip along the bottom of the content
  view answers *how much is really here*, with the active data
  source's type badge and name in bold on the bar's right end (the
  same identity format every source presentation wears): with nothing selected
  it is the real
  recursive size of everything listed; select rows and it is the
  recursive size of the selection — a folder always counts its whole
  interior, WinSCP-style. It works on every source type the view
  hosts — bucket lists, S3 folders (versioned, suspended or plain)
  and every remote engine (SFTP, SCP, FTP(S), WebDAV), the
  workstation walking its real folder trees too (only the drive-roots
  view keeps the listing-level floor — no wholesale drive walks) —
  always
  for the one source you are browsing, never an aggregate across data
  sources. On buckets with versioning the history is priced in next
  to the live content (noncurrent version bytes and delete markers —
  the real storage bill). Walks run in the background
  ("calculating…" meanwhile), are cached per folder so revisiting is
  instant, and a failed walk falls back to listing-level sums marked
  *partial — at least this much* with the error on hover.
- **Global bar** — the slim bar under the menubar carries the
  controls that act on the app as a whole, not on one view:
  **Dual-pane** (F9), **Compare** and **Synchronize** (both need
  both panes by definition), the light/dark theme toggle and **?**
  (the keyboard
  map, F1). Each pane's own toolbar keeps only what acts on that
  pane — back, forward, refresh, Home, upload, download, new
  folder, new file, Search — the same set in the same order on both
  sides, so the main view and the secondary pane stay
  button-for-button twins of each other below the global bar.
  **Home** on the main side returns to the open data source's start
  view — its contents root, a remote's root (a legacy account-wide
  source's bucket list) — and to
  the workstation home folder when a local folder owns the main
  view; Home on the secondary pane is the Dual-pane picker's
  **Home view** — the workstation home folder. The seam between them
  drags to rebalance the pair — a session-only share, so resizing the
  window scales both panes together, every fresh launch opens them
  equally wide, and a double-click on the seam restores the even split.
- **Path bar** — the breadcrumb shows where you are; click it (or the
  edit icon) and it becomes an editable line holding the view's typed
  source address, `scheme://Name/folder/` —
  `s3://team-files/docs/`, the scheme speaking the source's type
  (visible only while editing; the workstation side stays a bare
  native path). The same line minus its scheme is the
  `Name/contents` form the Copy path action puts on the clipboard,
  and a bare source name means that source's home view (the bucket
  list, or a scoped source's root). The line is a
  universal address bar: the seeded typed form and the plain
  `Name/contents` form paste straight back — the scheme names the
  source's type, the first segment its name — and a bare local path (`C:\Projects`,
  `\\server\share`, `~`, a `file:///C:/Users/...` or
  `file:///home/...` URL), an `s3://bucket/key` URI or a connection
  URI (`ftp://user:pass@host:21/root`, `sftp://`, `webdav://`) opens
  where it points. A local address opens the workstation folder right
  in the main view: the breadcrumb walks the native path (a `C:` root
  crumb, or `\servershare` for a UNC root), rows carry sizes and
  timestamps, and the folder's context menus, properties, Find scope
  and size bar all speak local — Copy / Cut put real files on the OS
  clipboard, Paste and dropped files transfer in from any view, Delete
  and upload work as anywhere else, while rename, New folder and New
  file rest (no local API — Explorer's job, the toasts say so) and
  drags between two local spots stay Explorer's too. An `s3://` URI
  names a source outright when its first segment is one (else it
  resolves the source that owns the bucket); a connection URI no
  source matches stands its source up on the fly and opens its root. Back /
  forward
  history works like Explorer — including its finer rule: any climb
  to an ancestor (a breadcrumb segment, the data-source tree, the
  parent row, a typed path above the current one) leaves the deeper
  view one Forward away instead of cutting it, and re-listing the
  very same place moves no history at all. The parent-directory
  row ("..") is the
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
  form's side inset. The window rides wider than the app's shared
  wide tier so the four-column table breathes, and the Source column
  carries the type badge plus a source/bucket pairing whole. **Name**
  and **Sources** sit side by side, always in view: Name is a
  substring or glob (Enter runs the search from any field),
  Sources is a flat list with no grouping — *All data sources*
  (the default: every data source from its root — an S3 source is one
  bucket, a remote source one host root) and each configured source
  by type and name
  (`S3 · team-files` — the type badge's text form; the pane's source
  picker labels its options the same way), an S3
  source searched in its one bucket (a legacy account-wide source,
  its every bucket), a remote source
  (local, SFTP, FTP, WebDAV) from its root. Every search honors
  each source's own boundary — a bucket-scoped S3 source is
  searched in its one bucket alone (buckets that belong to other
  sources over the same endpoint never leak in under its name), a
  legacy account-wide source keeps its every-bucket walk, and a folder
  preset scopes the walk itself to that folder — and two Search
  windows running at once never cross-paint: every page a window
  paints belongs to its own run's token. Everything else folds
  out under the *More filters* disclosure chip, whose label counts
  the filters you have set: **Kind** (any/files/folders),
  **Limit**, **Extension** (comma list, dot optional), **Path
  contains**, **Larger/smaller than** and **Older/newer than** —
  every filter works the same across S3 and remote sources
  (storage class never could, so it left the window). A **Clear**
  button resets the whole form. Results stream in as grid rows
  with type-matched icons (folder, image, video, archive,
  document …); clicking a column header sorts ascending, again
  descending. The columns are the main view's own — the one
  catalog seats both views — and carry its whole mechanics:
  drag a boundary to resize (arrow keys resize from the keyboard,
  a double-click hands a column its standard width back), drag a
  header sideways to reorder, right-click one for the add/remove
  check-list (name always stays; *Reset columns* restores the
  out-of-box set) — the layout persists per window across runs,
  and the same choice lives in Settings → View's Columns
  partition: pick the Search window in the chooser to show, hide
  and order its columns there too. A
  run spanning several origins adds a **Source** column parked at
  the far edge — it resizes like any column, but never reorders
  and never lists in the check-list — and a single-source run
  drops it; a
  double-click (or Enter)
  jumps to the object and selects it. Only the results scroll —
  the form stays put — and a running search can be stopped from
  the window.
  The secondary pane's own **Search** button opens the same window
  auto-selected on the pane's current location: one locked scope in the
  dropdown — the opened data source by NAME plus its path (a bucket, a
  remote folder, a workstation directory; at the source's own root the
  label is its typed name alone, no dangling slash), no *All data
  sources* and nothing else to pick, the Sources input itself disabled
  so the scope cannot be changed — and its dropdown arrow drops with
  it, so the dimmed input cannot read as a control that opens — and
  a pick lands on the pane and
  selects the
  row. A source named after its bucket (the credential import's own
  naming) reads once in the Source column and the scope labels — never
  doubled.

![Search](screenshots/search.png)

- **Edit in place** — right-click a file → *Edit* opens it in the
  app you pick (the OS "Open with" chooser) or the system default;
  every save uploads automatically, and on versioned buckets each
  save becomes a new version, so nothing is ever lost. Uploads are
  conditional: if the object changed on the server while you edited,
  the push refuses instead of overwriting it — the row turns ⟳
  *changed on server*, and you decide: *Push anyway*, *Reload from
  server* (discarding your pending edits), or stop and discard.
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

- **Dual pane** — F9 or the **Dual-pane** button on the global bar
  opens an optional secondary pane beside the main view: a full
  twin of the content area — its own toolbar (back, forward,
  refresh, Home, upload, download, new folder, new file, search),
  interactive breadcrumb, quick filter,
  back/forward history, parent row and status line. Each app run
  opens it on the workstation's home folder (like the old Panes
  panel, never an empty stop); right-click any data source, bucket
  or folder in the tree and choose **Open on secondary pane** to land
  there directly, and if the remembered source ever disappears the
  pane's source picker stands up to choose another.
  The pane's path bar carries the main one's every function: click its
  empty area and the breadcrumb becomes an editable line holding the
  pane's typed source address (`scheme://Name/contents`, or the bare
  native path `C:\Users\...` when the workstation side is bound) — copy it out,
  paste any address the main bar takes
  and the pane goes there: another source's `Name/contents` path
  points the
  pane at it, a bare local path or `file:///` URL rebinds the pane to
  the workstation, an `s3://` URI or a connection URI binds it to
  that source (standing one up when none matches), Enter goes, Esc
  cancels.
  Any source type binds to it — S3, SFTP, FTP, WebDAV, local drives —
  and inside the run it remembers where you left it, reopening
  there (closing the app forgets: the next run starts over at the
  home folder). Drag between
  panes transfers, and **Compare** — on the global bar, since it
  needs both sides — color-codes newer / older / size-diff /
  only-here between the two sides. The **Dual-pane** button does
  what it says: with the pane open it closes the pane; with the pane
  closed it opens — unless the pane was left on a view that is not
  the default home, in which case a small picker appears under the
  button (the pane stays hidden) offering where the reopened pane
  should point — **Home view** (a house glyph) starts over at the
  workstation's home
  folder with the history cut clean, and **Return to last view**
  reopens straight on the remembered spot (through history, so Back
  undoes the jump). Escape or a click outside dismisses the picker
  and the pane stays hidden — F9 plain-toggles the pane either way,
  and the pane's × stays a closer. When the columns run narrow each
  toolbar folds on its own measured width — word labels go first
  (tooltips carry the full names), and the buttons themselves fold
  away only when that toolbar's own column is truly cramped, so an
  ordinary window with the split open keeps every button in glyph
  form on both sides.

![Dual pane with directory compare](screenshots/dual-pane-compare.png)

- **Synchronize** — the ⟳ **Synchronize** button on the global bar
  (View → Synchronize; it needs the dual pane open) plans a sync
  between any two sides the panes seat — a local folder, an S3 bucket
  folder, a remote source directory — and runs it: missing and
  size-differing files copy each way — mtimes never matter, and the
  run rides *skip* semantics, so whatever landed at the target in the
  meantime stays put. The plan dialog shows each direction as its own
  capped section (file, size) — upload/download wording for the
  local↔S3 pair, destination-worded "Copy to …" for every other
  pairing — a direction picker narrows the run to one side only, and
  *remove files that are not at the source* — off by default — adds
  the delete legs, which run one direction at a time through the same
  confirmation windows as every delete. The one refusal is the
  degenerate pair: both sides naming the same location. The same
  predicate the CLI's `s3b sync` rides computes the plan, and that
  CLI now takes the same operand grammar `cp` speaks (`s3b sync
  vault://media ./mirror`), so the faces agree on the whole matrix.

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
  auto-detect optional) — switchable in Settings and, for the
  theme, straight from the global bar's toggle.
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
  copies names, normalized `Name/bucket/key` paths (the form the
  path bars paste straight back; local rows copy their bare native
  path) or URLs to the OS clipboard.
- **Conflicts & safety** — every transfer states a conflict policy
  (overwrite / skip / rename) with a live pre-check that lists exactly
  which files collide, and can be throttled (256 KB/s … 1000 MB/s, the same shelf Settings → Transfers edits). A
  transfer can never feed itself: a destination that sits inside the
  source's own subtree (a folder pasted into itself, a bucket into its
  own prefix) is refused by the engine before a byte moves — a move
  beneath itself would delete the fresh copies with the originals.
- **Transfer manager** — View → File transfers (or the status-bar
  counter) shows every job with per-file and byte-level progress, speed
  and cancel — in a floating window, so you can keep browsing while it
  runs. Multi-item jobs expand (the "+N more" link) into a per-item
  list that updates live — every top-level item’s state (waiting,
  transferring, done, failed, skipped) with its size, or its file
  count for a folder — held through to completion, so a finished
  batch still tells you which item was which. The window also opens **itself** the moment a transfer starts
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
  running jobs always stay. A row that settled with failures offers
  **Retry failed** — one click resubmits exactly the failed items
  (a canceled job's unfinished ones) as a fresh job under skip
  semantics: what landed stays untouched, only what failed goes
  again, and the original row keeps its verdict. **Show destination** on a settled row opens where the bytes landed — the bucket folder, the remote directory or the local folder — in the main window, wherever the transfers window floats.
- **Running tasks** — View → Running tasks (or the status-bar ⚙
  indicator) is the everything-monitor. The ⚙ indicator always shows
  a live count of active tasks (e.g. "⚙ 2 tasks — search 3/10") and
  clicking it opens the Running tasks window. The list covers transfer
  jobs, searches, bulk deletes, version purges, bucket emptying,
  storage-class conversions, folder and file creation, bucket deletes,
  pane compares, doctor runs — every action
  the app is taking, each with progress and a **Cancel** button;
  merged transfer rows expand into the same live per-item list as
  File transfers. Kill a
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

A bucket is born with its data source in the same gesture: **New
bucket…** on the sidebar's background menu (right-click the data
sources area) or on an S3 source's context menu picks the
connection, creates the bucket, and seats its data source in the
tree — the newborn source opens on its empty contents immediately.
**Delete bucket** on a source removes the bucket *and* its data
source together, and the view re-homes to a surviving source: a
source whose bucket is gone never lingers.

![New bucket — the newborn source's empty landing](screenshots/new-bucket.png)

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
s3b source split old-account                # legacy account → one source per bucket

s3b ls            s3b ls vault://media      # any source, same flags
s3b cp ./site s3://b/site/ -r               # upload (or download, or copy)
s3b cp s3://src-b/ sftp://host/dst/ -r      # cross-source migration
s3b sync ./site s3://b/site/ --delete
s3b sync vault://media ./mirror             # any two sides, same rule
s3b find s3://b --name 'backup*' --older 90d
s3b find s3://b --ext pdf,csv --path docs --larger 1MB
s3b doctor s3://my-bucket

s3b versions ls s3://b/docs/report.pdf      # timeline, newest first
s3b versions undo s3://b/docs/report.pdf --version-id MARKER
s3b versions purge s3://b --mode noncurrent --dry-run
s3b rb s3://old-bucket --dry-run            # the removal plan, nothing deleted
s3b rb s3://old-bucket --force              # empties versioned buckets too
```

Every command takes `--json`, `--profile` and `--verbose`; shell
completions: `s3b completion bash|zsh|fish|powershell`.
