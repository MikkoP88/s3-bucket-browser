# Usage guide

Everything you need to drive s3b day to day — in the GUI and on the
terminal. The same content lives in the app (Help → **User guide**, F1)
and in the supported-sources overview (Help → **Supported data
sources**); this page is the long-form version with screenshots.

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

Install the [release artifact](../../releases) for your platform (or
[build from source](../README.md#quickstart-gui)) and start it: `s3b`
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
commit; import it and the bucket's content opens immediately.

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
| Local filesystem | — | the dual-pane side (F9) browses local drives; any source type can be bound to it |

An S3 source is either **account-wide** (all buckets the key can list)
or **bucket-scoped** — `s3b source add s3://my-bucket` or the `--bucket`
option pins it to one bucket, which is what credential imports create.
On the CLI every source is reachable as a URI (`hetzner://bucket/prefix`,
`vault://media/…`) with the same flags everywhere.

All of them browse, upload, download, rename and delete like any other
source — and join the same transfer matrix (drag & drop, copy/paste,
directory compare) with S3 and the local pane.

---

## 3. Browsing

![Main window](screenshots/main-view.png)

- **Sidebar tree** — sources → buckets → folders. Click to navigate;
  right-click a node for Properties, the Admin panel, transfers and
  more. Folders expand lazily; buckets with versioning carry a 🔄 icon,
  object-lock buckets a 🔒 icon.
- **Grid** — Windows-Explorer selection: click, Ctrl+click, Shift+click,
  Ctrl+A (all), Ctrl+I (invert), marquee drag-select, type-to-jump. The
  funnel row under the header filters per column; right-click the header
  to pick which columns show; Ctrl+F focuses the quick filter.
- **Path bar** — the breadcrumb shows where you are; click it (or the
  edit icon) and type a path like `s3://bucket/folder/` — or
  `source://path` for any source — to jump directly. Back / forward /
  up history works like Explorer.
- **Favorites** — star buckets and folders for one-click jumps.
- **Deep search** — Ctrl+Shift+F filters every object under the open
  bucket/folder by name glob, size, age or storage class; results
  stream in, are cancelable, and clicking one jumps to the object.

![Deep search](screenshots/deep-search.png)

- **Dual pane** — F9 opens a local-filesystem pane (or another source)
  beside the main view, WinSCP-style: drag between panes, synchronized
  browsing, and **Compare** color-codes newer / older / size-diff /
  only-here between the two sides.

![Dual pane with directory compare](screenshots/dual-pane-compare.png)

- **Floating windows** — the views you keep an eye on — File transfers,
  Running tasks, the User guide, the keyboard map, the sources overview,
  the connection Doctor — open as draggable non-modal popouts: no
  grey-out mask, the app underneath stays fully usable while a transfer
  crawls or you read the guide. They resize from the corner grip,
  stack like real
  windows (any press raises one, Escape closes the topmost), each
  remembers where you left it — position **and** size survive every
  reopen, and only closing the app forgets them — and reopening a view
  just focuses its floating window instead of stacking a duplicate.
  Native popout
  windows open centered on the display the app window is on
  (multi-display aware — they follow the app onto whatever monitor it
  lives on); Settings → View → *Popout windows open centered on* pins
  them to the app window's own center instead. A native popout carries
  only the OS window chrome — the in-page header stays hidden, so there
  is no second title bar or close icon.
- **Light/dark theme** and 15 built-in languages (English default,
  auto-detect optional) — switchable in Settings.

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
  copied files upload into the open folder. In the other direction a
  copy inside the app is mirrored onto the OS clipboard through a
  staging download (small selections only), so Ctrl+V in Explorer works
  too. **Last copy wins** on both sides. Machines that must not touch
  the OS clipboard can turn the whole bridge off: Settings → File transfers
  → *Explorer copy & paste* (on by default).
- **Text instead of files** — the context menu (or Edit → Copy as)
  copies names, full paths or `s3://` URIs to the OS clipboard.
- **Conflicts & speed** — every transfer states a conflict policy
  (overwrite / skip / rename) with a live pre-check that lists exactly
  which files collide, and can be throttled (256 kB/s … 10 MB/s).
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
  jobs, deep searches, bulk deletes, version purges, bucket emptying,
  storage-class conversions — every action the app is taking, each
  with progress and a **Cancel** button. Kill a task that hangs or
  runs too long; destructive tasks count before they act, so canceling
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

- **Doctor** — Help → Doctor (or `s3b doctor s3://bucket`) runs a guided
  diagnosis: DNS → TCP → TLS → auth → permissions, with plain-language
  remediation and one-click re-runs of individual checks — in a floating
  window that doesn't block the app while it runs.
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
| Ctrl+Shift+F | deep search |
| Ctrl+U / Ctrl+D | upload files / download selection |
| Ctrl+C / Ctrl+X / Ctrl+V | copy / cut / paste (two-way Explorer clipboard sharing) |
| Ctrl+A / Ctrl+I | select all / invert selection |
| Del / Shift+Del | delete window / permanent path |
| F9 | dual pane |
| Ctrl+L | event log |
| Ctrl+S | save workspace profile |
| F1 | help (in-app guide + key map) |

---

## 9. CLI quick reference

Same binary, same engine, same sources — `s3b` with arguments is the
CLI. The complete tree is in [cli.md](cli.md); the shape of it:

```bash
s3b profile add lab --endpoint http://localhost:9000 \
    --access-key minioadmin --secret-key minioadmin --default
s3b source add vault sftp://deploy@backups.example.com

s3b ls            s3b ls vault://media      # any source, same flags
s3b cp ./site s3://b/site/ -r               # upload (or download, or copy)
s3b cp s3://src-b/ sftp://host/dst/ -r      # cross-source migration
s3b sync ./site s3://b/site/ --delete
s3b find s3://b --name 'backup*' --older 90d
s3b doctor s3://my-bucket

s3b versions ls s3://b/docs/report.pdf      # timeline, newest first
s3b versions undo s3://b/docs/report.pdf --version-id MARKER
s3b versions purge s3://b --mode noncurrent --dry-run
s3b rb s3://old-bucket --force              # empties versioned buckets too
```

Every command takes `--json`, `--profile` and `--verbose`; shell
completions: `s3b completion bash|zsh|fish|powershell`.
