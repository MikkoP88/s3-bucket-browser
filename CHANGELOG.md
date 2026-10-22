# Changelog

All notable changes to S3 Bucket Browser are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- **The tree’s read-only glyph becomes the pen-slash icon and the badge cluster after a data source’s name tightens.** A read-only source’s row in the sidebar tree wears the Fluent read-only icon (pen with a slash, `ic_fluent_read_only_24_regular` from svgrepo) in place of the lock emoji — an inline SVG colored through `currentColor`, so it follows the light and dark themes like every other tree glyph, with the same Read-only tooltip. And the markers that follow a data source’s name — the read-only glyph, the connectivity status ball, the versioning and object-lock guard icons — now sit closer together (leaner left margins, slimmer guard hover-chip padding), so the row reads as one badge cluster instead of a spread of icons. No behavior moved: the guards still open the admin panel, the ball still speaks connectivity, and the read-only law is unchanged.

### Added

- **The inspector round: the selection’s panel grows a comprehensive face you reshape with the mouse, Properties takes the Windows file-properties grammar, data sources learn read-only mode end to end, the bucket seat becomes a picker dialog, and a share grows a lifetime and a registry of its own.** The inspector re-renders: every fact the app holds about the selection renders in one panel — the view summary when nothing is selected, the row’s own facts on a single selection, aggregates on many — under a header that carries the quick verbs in two arranged groups (the solid primary row: open, preview, edit, download; the quiet secondary: copy path, copy name, Properties), the panel’s width is yours to drag (a grab handle on its edge, double-click resets, the width remembered across sessions), and the selectable-information law runs through the app — inspector facts, Properties values, dialog text, all of it selectable and copyable. Properties re-organizes into the Windows grammar: a top card (icon, name, subtitle) over a category tab strip — an S3 object carries General, Details and Security, every seat its own set — with every value selectable text. Data sources grow read-only mode: a Read-only checkbox seated right after Skip TLS verify in the Add/Edit Data Source editor (both the S3 and the remote leg, the tooltip speaking the law — every write verb refused before it dials, safe browsing, auditing and restores), the source’s tree row wears the lock badge beside its name, a banner states the mode in the views that browse it, and every write, edit, modify and admin-panel action on that source is refused with the honest toast while its controls stay deactivated across the whole app — context menus, toolbars, dialogs, the admin panel, the dual pane’s secondary pane included — the read verbs (browse, download, copy out, properties, preview) staying live; the CLI gains `--read-only` at source-add time for both S3 mounts and remote sources, and the source listing prints the mode. The bucket seat in the source editor stops being a dropdown: Bucket opens a picker dialog listing the provider’s buckets with search, the test-first flow unchanged. Pre-signed sharing grows a lifetime and a registry: the URL dialog offers the validity choice (minutes to seven days), Share registers the share and flips the button to Stop share in the danger idiom, the View menu carries a Shares window listing every active share with its stop verb, and the window states the honest limit — a pre-signed URL cannot be revoked remotely; stopping removes it from the list and clears the local clipboard while the URL itself works until it expires. The round’s own overflow find is cured in the same reach: dual-pane with the docked inspector at the default 50/50 split starved the pane toolbars’ tail buttons — they slid under the inspector column, unreachable by mouse; below 480px of column the toolbar chrome now tightens itself (leaner button padding, closer separators — no verb folds, the context menu never stands in) and every control stays inside its column. The round’s harness pays five laws back: the dead filter needle (a leftover `view.filter` ate the first listing after a view clear — cleared at all three seats: the input, the grid, the view state), the node-serialization law (a predicate that returns a DOM node and reads it outside the page is forever-false — the whole predicate rides one evalPage), the inherited-state law (a failed earlier step leaves the panel open, so later steps normalize their own preconditions before they gesture), the glue law’s re-offense (context-menu item text glues label and accelerator — family prefixes, never bare anchors), and the scoped transfer oracle (the paste belt’s own copy is live on a guarded seat by design, so the no-transfer oracle snapshots the call log past it).

- **The hundred-million round: the listing stack’s deliberate window grows into a seamless sliding window — the “show more” footer and every continuation control of its class are gone, any single content view holds toward 100 million objects without a stall — and the terminal’s fold family is cured twice in the same reach: the mouse wheel that was dead whenever a row was selected, and the fold that survived navigation into hiding a child view’s first rows.** The grid’s window now seats itself: a first hold of 100,000 rows streams in while the first page still streams (FIRST_STEP), every further window loads itself in 25,000-row steps as the viewport approaches the loaded edge (demand-driven, a 1,500-row prefetch), and when the held model passes 200,000 the far edge trims rows above the viewport into tombstones — the scroll height stays stable through row-index math, a start-after checkpoint remembers the trim, the climb back seats inert placeholders and refills from the checkpoint below, and a hard one-million-row scroll cap bounds the virtual surface; the DOM stays a ~30-row pool, the heap bounded, the quick filter answers over held rows, and no continuation control exists anywhere. Validated empirically against a local MinIO seeded to 150,000 objects (41 rig pins, zero page errors): first rows in 3587ms while the first page still streamed; the whole 150,000-row universe known inside a 136-second window at the provider’s warm pace; a 28–36 row DOM pool and 27–37 MB heap at 110,000 held; the quick filter answering in 1838ms with one hit over 150,000 held rows; the trim walk above=40,000 held=110,000; the climb re-requesting exactly the missing head with 15,000 tombstones at 95,000 held; the mid-scroll gap-fill at scrollTop 700,000 frozen; and the re-dive reloading from the checkpoint (universe 150,000, above 0, held 110,000). GUI-111 is rewritten as the sliding-window law (2,500 seeded objects, every seam asserted on row identities and model counts — never wall-clock — with the no-continuation-control invariant pinned). The terminal find: the TUI Table’s wheel was dead whenever a row was selected — always, since Sel starts at 0 — because the wheel case clamped the fold to the selection and Draw re-clamped it every frame; the old unit test dodged the disease with Sel=-1. The cure splits the clamps: clampBounds holds Off inside the row universe (wheel, Draw, SetRows — viewport gestures own the fold) while clampTo additionally reveals Sel (MoveSel, End, mouse press — selection gestures own it), and the wheel rigs are rewritten to pin the law with a standing selection. The fix’s own battery run surfaced the navigation half: a fold carried at the wheel floor into a short descended listing hid the child’s first row above the fold — navReload now opens every changed view (dial, descent, ascent, :cd) from its top while same-view reloads (F5, r, :refresh, post-verb re-lists, the root refusal) keep the fold, pinned by TestBrowserNavFoldReset with every leg carried at an Off that bounds-clamping alone would keep. And the round’s closing sweep paid one more product find: the tree’s versioning-guard icon could vanish for a whole session on an unlucky boot — bucketGuardC discarded the versioning error, so a transient cold-dial failure answered “never configured” as a success, the frontend cached the lie (successes are truth, only failures retry), and no retry could ever run; the guard now propagates every real failure so the frontend’s retry law works as designed, and answers the zero state only for the provider’s honest not-implemented family through the new exported adminops.UnsupportedByProvider classifier — pinned by three rigs over a real httptest S3 wire (a 500 propagates, a 501 NotImplemented collapses into the honest zero state, and the Enabled+lock XML still reads the full guard) plus the classifier’s own table, with the sweep green on the cured binary at 142/142 and no page errors. The round paid four harness laws back: the fold-drift law (frame records lag renders and the click reads the live fold, so a reveal-frame click orphans at ri=-1 and Enter descends the stale row-0 selection — the cure wheels to the floor where Off clamps and the fold cannot move under the click), the JSON backslash collapse (any escape byte in a patch script is composed from fromCharCode and the written bytes verified after — a JSON double backslash collapses one level by parse time), the account-settle law (a CLI-created bucket is settled in the account listing out-of-band before the terminal waits on it), and the stall-triage law (gui-v3live’s step catch now dumps the TREE STATE — tguard count, the tree’s own text, the __s3bTree node map — beside the backend log dump, so a missing-icon failure names its own state instead of a theory). Gates: js-check OK, the i18n catalog at 538 keys across fifteen locales (the footer’s keys retired with the footer), provenance 9/9, node --check on every patched JS file, the Go suite green under -race in docker with the root wails package and cmd/s3b green natively, vet clean, staticcheck native on Windows reads clean, the full battery green at 210 PASS · 10 SKIP · 0 FAIL (220 rows; the recorded MinIO provider gaps), and the gui-visual sweep at 1544/1544. Docs: the CHANGELOG carries the round bullet, VERIFICATION grows the round row. No CLI command surface moved.

- **The operator round: five user-named defects cured end to end, the admin panel re-skinned in the Settings idiom, the lifecycle card grown comprehensive with a JSON round trip, the mid-file pause fixed at the engine, and the listing stack proven interactive at ninety-thousand-plus held objects on live MinIO.** Dual-pane clipboard law: the pane’s row menus now answer Copy/Cut with the PANE’s own selection — never the main grid’s stale multi-row gather — and a pane bound to any source (S3, remote, local) speaks its own Copy; pasting into the main view from any pane binding routes the cross-source transfer through the bucket picker (GUI-110 live; the pane-menu-copy offline twin pins the count law: OsClipboardSetFiles carries exactly the pane’s one row while the main grid still holds two armed rows). The admin panel takes the Settings view’s styling and category grammar end to end with a rail search over the group ladder. The lifecycle card v2 covers the whole rule surface — tag-based filters, min/max size, expire-after-days XOR expire-on-date, per-transition rows with days XOR date and every storage class, noncurrent-version transitions, keep-newest count, abort-MPU days, delete-marker expiry — plus Copy JSON / Paste JSON: a rule set moves as text and pastes back to the same card (the paste decode hardens against missing arrays; the nil-And crash is gone, pinned by the new Go rigs). The loading-animation family: every long action now seats the animated liveness bar (the cases that rendered a frozen spinner are cured; the gui-visual pins assert the animation class is present and moving). The transfer pause: a pause mid-file now parks the job AT its in-flight offset — the wire request drains, progress freezes, resume continues from the held offset (GUI-93: parked mid-file with the offset held, 5-second stillness as the freeze oracle, resumed to done, all three files sha-identical). And the listing stack: pages stream to the grid without accumulating on the Go side, the grid holds a deliberate window (100k step, 1M ceiling) with a grow-the-window footer, the DOM stays virtualized and the heap bounded, and the quick filter answers over held rows — validated empirically against a local MinIO seeded with 150,000 objects through tools/seedbig (150,000 rows walked by the CLI in 20.4 min — pages served at the provider’s own cold 8-20s / warm ~1s rate, so the full-walk number names the provider, not the app; the real GUI, seated through the source editor over the same bucket, stayed interactive the whole way: first rows in 1789ms while the first page still streamed, 89,000 rows arriving in a 150-second window at the warm provider pace, a 28-row DOM pool and 24 MB heap at 90,000 held rows, and the quick filter answering in 265ms over those 90,000 held rows — the stall complaint’s direct oracle). Gates: js-check OK, the i18n catalog at 539 keys across fifteen locales, provenance 9/9, node --check on every patched JS file, the Go suite green under -race in docker with the wails pair green natively, vet clean, staticcheck run native on Windows reads clean, the full battery green at 210 PASS · 10 SKIP · 0 FAIL (220 rows), and the gui-visual sweep at 1539/1539. Docs: VERIFICATION grows the round row.

- **The sixth coverage audit walks the admin panel’s two unwritten tabs live, seats the terminal on SFTP, and finds — in the lifecycle read-back — the null that every empty JSON answer carried, cured once at the print site for every verb at once.** GUI-108 walks the Security tab’s public access block end to end: a fresh bucket seated through the tree’s context menu, the panel opened onto its Security tab, and where the provider speaks the API the full round trip runs — the four block-public switches ticked and saved with the CLI reading all four back true, unticked and saved with them reading back false, the panel the writer and the CLI the oracle in both directions; on this MinIO the pane answers that the provider does not speak the API and the row’s gap decider — an out-of-band CLI put that itself refuses 501 — names the wire a recorded gap, not a GUI bug, so the row lands as the honest SKIP beside CORS GUI-35 and Website GUI-36 (the healthy skip count grows to ten). GUI-109 walks the Lifecycle tab under the same law: a rule authored in the panel’s card (id, prefix filter, transition days and class, expiration, noncurrent and abort-MPU days, delete markers) saves through the panel with the CLI reading every field back, and Delete all clears it with the CLI answering the honest empty array — the live pin of the round’s one product find, the --json law; on this MinIO the panel’s Save never lands and the decider’s out-of-band CLI put refuses 400, so the row names the recorded gap the same way. The engines answer nil slices and maps for their “nothing set” states (no lifecycle rules, no CORS, an empty listing) and Go marshals a nil slice as null, so every list verb’s empty answer was a jq-breaking null; the uploads round had cured its one verb at the call site, and the audit found the family — the cure now sits once in printJSON (a nil slice prints [], a nil map {}), pinned by TestPrintJSONNeverNull’s four cases with GUI-109’s delete-all poll the live pin on a provider that speaks the API. TUI-14 seats the terminal’s SFTP face: a source added over the ssh shorthand onto the chrooted fixture, the boot pinned on the row’s own seed (a full battery consumes the lab’s shared files), the rail dial to sftp-tui, the descent into the row’s own folder, Alt+Enter properties honest to the engine (name, modified, where — never a faked ETag), F2 rename — the terminal’s first write verb on a non-S3 remote — d downloading the renamed file byte-exact into a typed directory, and the CLI twin dialing the same source to copy the renamed file back while the old name refuses; the row’s waits paid two harness laws back. First, the rename’s completion oracle: the runVerb flash “renamed to…” draws BEFORE the re-list, so a wait on the new name alone satisfies on the flash frame riding the OLD listing — the predicate now demands the re-listed frame itself, the new name seated AND the old name gone in the same frame. Second, the Table’s 450ms double-press window strikes again (TUI-12’s law): lastClk survives re-lists keyed on ROW INDEX, the seed select and the renamed row share the single-file listing’s one row index, and the props/rename legs between the two presses are fast enough on localhost SFTP to sit INSIDE the window — the select press double-activated the renamed row into Properties and the d fell into the modal, starving the download wait on a full-screen takeover; both select presses now sleep past the window. Dead bridges retire with the audit: App.CreateBucket (the frontend creates through CreateBucketInSource; the view-source engine the old method rode is useless from the sources area) and App.DeclineLicense (the gate has no decline verb) — and the retirement pays the wails law back in words: deadcode never flags an App method because the runtime binds them through reflection, so the manual frontend-caller survey is the only dead-bridge oracle the project has. gui-visual’s admin fixtures spoke drifted contracts — the cors mock used allowed*-prefixed keys and the lifecycle mock status/days/storageClass, shapes the real tabs never read, leaving their inputs empty in every screenshot — both corrected to the true adminops contracts, and the admin step grows the offline twins of both live rows (the write-bind proof the recorded gaps cannot carry live): the PAB save writing all four switches through a fixture that ships a MIXED state (a stuck render can never pass), the lifecycle save writing the bound rule card, and delete all speaking the delete verb for the bucket. And the sweep’s own forced-colors step paid a timing law: the drop leg slept a blind 120ms before comparing the theme paint, and the drop’s asynchronous style recalc flipped the coin on 18 October — the leg polls the settle now, the same law its entrance leg always preached. Gates: js-check OK, the i18n catalog untouched at 538 keys across fifteen locales, provenance 9/9 (no watermark disturbed through any patch), node --check on every patched JS file, the Go suite green under -race in docker (26 packages; the two wails binaries stay CI-only) with the root wails package and cmd/s3b green natively, vet clean, staticcheck run native on Windows reads clean, and the full battery green at 208 PASS · 10 SKIP · 0 FAIL (218 rows; the recorded MinIO provider gaps — all four admin write tabs) with the gui-visual sweep at 1507/1507. Docs: VERIFICATION grows the round row. No CLI command surface moved — the --json fix changes what the empty answers print, not what the verbs are.

- **The fifth coverage audit seats the two TLS wire families live in the GUI and finds, in the FTPS paste, the one caller that broke the app’s own per-source lock law — the transfer planner — cured three ways in the same round.** GUI-106 seats an FTPS source live end to end: a payload seeds through the CLI face onto the vsftpd explicit-TLS fixture, the source editor dials ftps with the Test verb proving the DRAFT connection over TLS before anything is saved, Save seats the tree row and the saved source lists the seeded row, the GUI copies that row into S3 (select, Ctrl+C, paste into verify-gui/) with the S3 pull-back comparing exact bytes, and RemoveSource forgets exactly the GUI store entry while the engine data survives through a CLI twin dial. GUI-107 is the HTTPS twin over rclone serve webdav: the editor dials webdavs, the Test proves the draft, Save seats and lists, the copy into S3 comes back byte-exact INCLUDING the line endings — the CRLF law a WebDAV round-trip must hold. The round’s one product find is the crash that paste paid for: jlaffaye’s FTP client carries zero synchronization on a ServerConn (a command is a raw write plus a read), two concurrent commands tear the shared textproto reader, and the app’s law that every engine call touching a source serializes on that source’s lock was broken by exactly one caller — planXfer, whose planning Stat and Walk ran unlocked, racing the tree’s own listing on the same control connection until bufio.ReadSlice panicked on a bare goroutine and took the whole process down. The cure lands three ways: the planner now resolves every read side first (resolution dials but runs no commands), collects the lock ids of every remote source and the remote destination, and holds ONE sorted lockSrcs acquisition across the whole plan — CheckConflicts’ destination probes ride the same law — and because an unknown unknown deserves a net even when a caller slips past, remotefs’s bound runner (a bare goroutine, where an unrecovered panic kills the process) now recovers into ErrCmdPanic, classified as connection-death by both health paths (the FTP engine redials, the api healing cache retires the channel) — pinned by parking rigs that hold the source lock from inside the engine at every command boundary, and panic rigs that prove the error, the redial classification and the healthy passthrough. TUI-13 closes the terminal’s keyboard grammar: F7 opens the New folder prompt and the typed name lands as a real directory on disk, Space marks rows through the keyboard (the ✓ gutter), and d answers the MARKED SET with the download prompt — the marked-set path over any engine, both files byte-exact in the typed directory, the descent into the new folder proving it navigable (copy and paste are the S3-in-account family TUI-10 already walks). Dead wood: Box.HandleKey and Box.HandleMouse retire (no key or mouse ever reached a bare Box — the widgets that wanted them implement their own), the deadcode probe reads its steady three-finding state (provenance.Decode, FindText, C256 — test seams and the watermark pair’s decode half, kept with cause), and one honest field comment in the TUI color model. Gates: js-check OK, the i18n catalog untouched at 538 keys across fifteen locales, provenance 9/9 (no watermark disturbed through any patch), node --check on every patched JS file, the Go suite green under -race in docker (26 packages; the two wails binaries stay CI-only) with the root wails package and cmd/s3b green natively, vet clean, staticcheck run native on Windows reads clean, and the full battery green at 207 PASS · 8 SKIP · 0 FAIL (218 rows; the recorded MinIO provider gaps) with the gui-visual sweep at 1503/1503. Docs: VERIFICATION grows the round row. No CLI command surface moved.

- **The fourth coverage audit closes the battery’s last unwalked verbs and empties the dead-wood list in the same sweep — four new live rows walk real wires end to end while twenty-three lines of dead Go and their gui-visual mirror retire.** TUI-12 seats the terminal face on a second real wire: FTP end to end, the rail dial walked as a true round trip with the descent, Alt+Enter properties honest to its engine (name, modified, where — never a faked ETag) and a byte-exact download. The row’s first full run paid the round’s one find, and it was harness-side: the TUI boots on the alphabetically-first source (SortedSources then dial(0)), ftp-tui sorts before the local lab, and a boot wait for the lab’s listing starves against a crumb that honestly names the new source — the row now pins the sorted-first boot law itself and dials away to the lab and back, because a re-dial of the already-seated rail row is the rig-pinned no-op that redraws nothing and would starve any wait after it. GUI-103 walks the context menu’s Pre-sign verb over real S3: one object answers a shareable URL the row fetches back over HTTP for the exact bytes, two objects answer the N-row dialog with every URL fetched and byte-pinned, and the single face never says object(s). GUI-104 walks the Find window over live data the whole way: the toolbar opens the popout, the scope dial names the source, the name query runs on Enter, the results row carries the hit, and its double-click seats the tree at the parent with the row selected and the crumb naming the descent. GUI-105 walks Retry failed over a real kill: a faultproxy throttled to 150 ms and then reset mid-flight slays a local-to-S3 upload, the dead job carries its error and its failed count, the listing proves no partial object ever landed, the wire restored, the transfers popout’s one-click retry resubmits, and the second job completes with the bytes round-tripped back and sha-pinned. The dead half retires what no caller speaks: the FocusPopout bridge method with its DesktopShell field and gui.go wiring (the frontend re-seats through OpenPopout’s own singleton law; the focus verb never had a caller) and the versions.go SourcePrefixVersionSummary helper with its gui-visual mirror. gui-visual grows two pins of its own: the credential draft’s x discards through DiscardCredentialDrafts, and a double-click opens a workstation file through OpenLocal. Gates: js-check OK, the i18n catalog untouched at 538 keys across fifteen locales, provenance 9/9 (no watermark disturbed through any patch), node --check on every patched JS file, the Go suite green under -race in docker (26 packages; the two wails binaries stay CI-only) with the root wails package and cmd/s3b green natively, vet clean, staticcheck run native on Windows reads clean, and the full battery green at 204 PASS · 8 SKIP · 0 FAIL (215 rows; the recorded MinIO provider gaps) with the gui-visual sweep at 1503/1503. Docs: VERIFICATION grows the round row. No CLI command surface moved.

- **The orphaned parts of dead transfers become a first-class lifecycle —
seen, diagnosed, and released** — incomplete multipart uploads were the
one storage-hygiene surface the app had no leg for: a large transfer
that dies mid-flight (a killed process, a canceled job, a dropped link)
leaves its in-progress multipart upload on the provider holding every
part already streamed — invisible to every object listing, billed and
kept until aborted or expired — and s3b could neither see nor cure
them (the prevention face, the lifecycle rule’s abortMpuDays, existed
alone). The operational face lands as `s3b bucket uploads s3://bucket`:
a listing (oldest first) with each upload’s key, short id, age and
initiated timestamp, `--older-than 90m` / `--id ID` filters, `--json`
for the machines (an array, never null), and `--abort --force`
releasing the parts — a bare `--abort` refuses as a usage error the
same way `rb --force` gates, and aborting never touches completed
objects. The Doctor grows its Storage hygiene group: a tenth check
counts the bucket’s incomplete uploads, warns with the oldest’s age
in whole human units (a shared HumanAge helper — “3d 4h”, never
“26h0m0s”) and prescribes the cure command, as a warn rather than a
fail so the run stays exit 0; unsupported providers map to the honest
skip sentence. The engine underneath speaks the raw wire
(ListMultipartUploads walked through its paginator, AbortMultipartUpload
by bucket+key+uploadId). The battery walks the whole story live as
CLI-S3-53, riding the hard kill of CLI-RES-04: a 24 MiB cp slain
mid-flight through the faultproxy, then stat proving no object ever
became visible, the listing naming the orphan, --older-than 7d refusing
to match a seconds-old upload, the bare --abort refused with nothing
touched, an unknown --id failing honestly, the doctor warning by name
at exit 0, the abort by id releasing exactly the seeded orphan, and the
final sweep leaving the bucket’s upload list empty — the residue of
earlier kills swept with it.

- **Every live data source earns its row, and the terminal's S3 write law
is found and fixed** — the third coverage audit walked every verb,
source kind and data scenario on the app's surface against the battery
and closed all thirteen honest gaps with real-data rows: FTPS over
explicit TLS (a committed self-signed identity under scripts/certs/
e2e-tls, CN=localhost, valid to 2036 — a vsftpd fixture rather than
rclone, because the jlaffaye FTP client sends PBSZ 0 + PROT P whenever
TLS is on and rclone's goftp server 550s it), WebDAVS over HTTPS (the
rclone server pinned to GODEBUG=http2server=0 — the stock http2 server
speaks a dialect webdav clients choke on — with a CRLF-bearing payload
proving the line-ending law a WebDAV round-trip must hold), an SCP
round-trip, a local source as a typed NAME:// view over a workstation
directory, the guarded edit round-trip on a remote engine, ls --watch
and sync --watch live tails, the provenance verb, the GUI rows seating
PreviewData over s3, remote and local files, the typed editor family
(EditFile/PushEditFile/StopEditFile) live over SFTP, and log --follow
tailing the GUI app's own event stream from the CLI face — plus the
TUI's first write rows, and Ctrl+L seating the log drawer in the visual
battery. The TUI row paid the round's one product find, a whole family
of it: transfer.JoinKey answers PREFIX form (trailing slash included),
which is right for descent and :cd and wrong for object keys — every
S3 write site in the terminal face rode it, so an upload at dir/name
landed as dir/name/, a folder marker with the file's bytes hidden
behind a synthetic directory, and mkdir, rename, delete, copy and paste
all carried the same tail. A local objKey helper trims the slash at all
six sites now, pinned by TestTuiObjectKeyLaw. The verdict runs also
paid three harness finds worth their laws: a single mouse press on a
row whose INDEX an earlier click pressed activates inside the 450ms
double-click window (lastClk survives reloads), the transfers panel
only draws over real transfer rows so its toggle verb must be seeded,
and the virtualized grid renders only the visible fold — an assert for
a row below it narrows the listing with the live filter instead. The
dead-wood sweep that closed the round retired six unused TUI symbols
(the stBright style, the padRight column helper, five dead glyph
constants, the parser's Pending probe), the dragprobe's unused
GetAsyncKeyState binding, and renamed the CLI edit sentinel to the
errFoo convention — staticcheck on the Windows face reads clean.

- **One Doctor window, pickers that never get lost, and logs two windows
can share** — three hardening laws land on the multi-window app. The
Doctor was the last view that could stack: its window id was scoped by
bucket, so asking about a second bucket opened a second window beside
the first. It now seats one window whatever bucket it serves — the
same law File transfers and Running tasks already ride: asking again
focuses the one window, and when the ask is about a different bucket
the window re-seats onto it (on the native face the window reloads
onto the new query; on the DOM face the title, body and footer swap
in place), with the check list booting afresh because a re-seated
window is a new scope. The reopen path itself moved: the frontend used
to ask the backend whether the window was alive and fall back to a DOM
box on a stale flag — now one OpenPopout call serves both roads, the
backend being the singleton authority (a live window focuses and
keeps its title honest, a dead id is re-created natively, so a window
that died without its close event heals into a real window instead of
an in-page box beside nothing), and the first-open verification polls a
few times before ever downgrading the session — a cold first window on
a fresh per-instance browser profile no longer costs the app its
native popouts. The OS pickers learned where they belong: every native
file/folder dialog (upload files, upload folder, choose download
folder, profile open and save-as, credential import, the source
editor's folder browse) is now owned by the app's main window — an
owned dialog opens centered on its owner and stays above it, never
lost on some other monitor's corner or buried under the app — and
while a picker is up a modality ring disables every sibling window so
a click meant for the dialog can never land in a popout behind it,
with the window-group raise standing down the whole time a picker is
up. And the shared log sinks learned about each other: events.jsonl
and transfers.log are written by every window of the app, and Go's
O_APPEND is seek-then-write on Windows, not atomic — two windows could
interleave half-lines into a tail no reader parses. Both sinks now
serialize rotation and append through a zero-byte cross-process lock
beside the file (the same kernel-released lockfile the profile
containers ride), and the law is best effort: a sibling window holding
the lock past a 150 ms budget costs that one line, never a blocked
app — the professional answer to overlapping artifacts is structural
serialization, not a popup asking what to do about a log line.
- **The app learns to run beside itself: full multi-instance support,
and one OS file dialog per window** — a second window of the app is
now a first-class thing: File → New window (Ctrl+N),
`s3b --new-instance`, or S3B_MULTI_INSTANCE=1 opens another full
window beside the first, and every opted-out window seats its
WebView2 browser state on its own per-instance profile under the
config dir (a %TEMP% fallback when the config dir is not writable),
with the seats of dead windows swept at boot by a PID-liveness probe
that fails safe — an unprobeable process counts as alive, because a
wrongly swept profile costs a window its state while a wrongly kept
one costs nothing. The default stays the honest default: launching
s3b again without the verb still just raises the running app (the
single-instance guard and its focus relay untouched), and side-by-side
windows no longer stack invisibly on the first window’s exact
remembered rect — the OS cascades them. The store is shared, so the
round hardens the seams two windows actually meet at: every
per-instance scratch seat (clipboard staging, editor staging,
transfer spool) moves from one shared folder into <base>/<pid>
subfolders the boot sweep keeps clean the same PID-honest way, and a
profile file open in one window is refused in another — its path
carries a container lock (a Windows byte-range lock, a unix flock,
taken before the in-process mutex in every verb so the open and
save-as doors can never deadlock each other), released on close,
re-pointed on save-as, reclaimed by the kernel if a holder dies, and
honest about the same window reopening its own file. The second half
of the round is the picker law: one native OS file/folder dialog per
window, always — every picker verb (upload files, upload folder,
choose download folder, open and save profile file, import credential
files, the source editor’s folder browse) rides one gate (a
CAS’d busy flag in pkg/api), so a second picker in the same
window is refused until the first closes and no two OS explorers can
ever overlap into a pick from the wrong one; the frontend wraps every
bare picker call in one helper that toasts the refusal instead of
dying silently, and test seams sit before the gate so rigs never
queue behind it. The battery walks it live: GUI-99 spawns real
desktop instances side by side (two opted-out windows alive on their
own per-PID webview profiles, a default launch booting beside them
because opted-out windows never held the single-instance slot, and a
second default launch focusing the running instance and exiting 0 —
the guard still guards), and TUI-09 pins the --new-instance flag
routing the headless build into the terminal face. The Go suite
grows the rigs with it: the lockfile’s exclude, release and
leftover-file laws; the dialog gate’s refuse-second, reopen,
error-passthrough and seam-bypass laws; the instance-spawn contract;
the profile-file container-lock family across windows (held while
open, same-window reopen, re-path, wrong-password clean, save-as
destination lock, external holders refused); the per-instance
workspace sweep with live siblings preserved; and the webview
profile plan’s precedence and sweep. No CLI command surface
moved.
- **The terminal grows its own face: `s3b tui` — the whole
browser in any modern terminal, no WebView, no new
dependencies** — the CLI binary now carries an interactive
terminal UI beside its verbs: a hand-rolled toolkit under
internal/tui (a pure-ANSI cell grid with diff flushing, the
alternate screen, SGR-1006 mouse, bracketed paste and the
kitty key protocol — zero added dependencies, only the
x/term, x/sys and color packages the CLI already rides) draws
the GUI's grammar in text: a source rail with type badges
beside the listing (double-click dials a source, Tab walks
between rail and grid), a crumb speaking the seat, a
folders-first humanized listing, `/` live filter, `:` command
bar (cd s3://bucket/prefix, NAME://dir or relative paths,
mkdir, rm, sources, xfers, help, q), F2 rename, F7 new folder,
Del delete behind a confirm whose Cancel is honest, u and d
upload and download with the F8 live transfers panel, c and v
copy and paste, Space and Ctrl+Click marks with their count
dialog, Alt+Enter properties (size, modified, storage class,
ETag, mode, where), F1 the key map, and Ctrl+C twice, Ctrl+Q
or :q to quit. It runs anywhere the CLI runs — the same
source store and pkg/core engine, over SSH, in every modern
terminal, no WebView2 — and the headless CLI build runs it on
bare launch (s3b with no arguments gets the browser, not a
usage screen); the face also stays drivable over pipes
(S3B_TUI_SIZE fixes the geometry, S3B_TUI_FRAMES dumps every
rendered frame), which is how the battery walks it black-box.
The verification the face was built under: a Go rig suite in
internal/tui (the parser's escape grammar, the width laws,
every widget's key and mouse semantics, and live-browser
walks over seeded local sources — navigation, filter, sort,
rename, delete, transfers, marks, modals, the rail switch and
the S3 prefix arithmetic), and eight new battery rows
TUI-01…TUI-08 driving the real binary over pipes: boot
geometry and the version-bearing header, Enter/Backspace
navigation with the honest root refusal, the live filter and
mouse header sorting both directions, :mkdir and F2 rename
round-trips with Del's honest Cancel, upload and download
byte-exact round-trips under the F8 panel, Ctrl+Click SGR
marks with the count dialog, Alt+Enter properties and the F1
map closing on Esc, and the S3 face end-to-end over MinIO —
the rail double-click dialing the account-wide source into its
bucket list, the bucket seat, the prefix descent, the
ETag-bearing properties and a download verified byte for
byte. The command census grows to 25 with tui. The round's
verification paid two more product finds, both in the S3
face: the rail dial never seated its source (crumb reads
b.sel, dial set only the rail's own selection, so the dialed
source's buckets listed under the booted source's name —
navigate even carried a manual seat patch at its own call
site, honest at one door and wrong at every other) — the seat
now follows the dial; and every S3 descent doubled the prefix
slash (transfer.JoinKey already answers prefix form, and the
browser's own slash on top sent prefix// to MinIO, which
answers XMinioInvalidObjectName — the descent, the :cd
s3:// URI leg and the relative leg all ride JoinKey now, and
a bare s3://bucket lists its root). The full run then paid one harness find the solo slice could not see: the S3 descent pressed Enter on row 0, which is the seed's seat only in a lonely bucket — the full battery's earlier rows leave their own folders in the same bucket, hostile-names sorting first — so the row now selects the prefix by its own coordinates before Enter. Gates: js-check OK, the
i18n catalog untouched at 537 keys across fifteen locales,
provenance 9/9 (no watermark disturbed), node --check on every
patched file, the Go suite green under -race in docker
(internal/tui joins the suite — the two wails binaries stay
CI-only), the full battery green at 184 PASS · 8 SKIP · 0
FAIL (the recorded MinIO provider gaps), and the gui-visual
battery green at 1497/1497 (no frontend surface moved). Docs:
usage.md grows the terminal section with its key map, cli.md
regenerates through the twenty-five-command census, and the
README names the face beside the GUI.
- **The audit's last two unwitnessed actions earn their rows, the
  advertised accelerator that never answered is wired, the dead wood
  goes** — every verb, button and accelerator on the app's surface
  was walked against the battery's coverage once more, and exactly
  two critical actions had no witness: the Properties family and
  Clear All…. GUI-97 seats the Properties verbs live: Alt+Enter on a
  selected object — the accelerator every grid menu labels and no
  keydown ladder answered until this round wired it beside F2 —
  opens the metadata modal whose ETag and SSE row answer the
  stat --json oracle, the tree source's Properties… opens the
  bucket-grade modal whose region and versioning pill answer the
  bucket info --json oracle beside its admin summary rows, and a
  multi-selection aggregates items, total size and common prefix
  without per-item round trips. GUI-98 seats Clear All… — the
  workspace reset behind its danger confirm: Cancel is honest (the
  view, the open pane and the session seats all stand), Clear
  closes the windows, hides the pane, empties the filter, drops the
  session keys (the pane seats, the popout geometries and the
  remembered view — s3b-popout-center survives by design), leaves
  the deep view for the source home and keeps every source and
  setting. The gui-visual battery grows the wiring pin (the
  keyboard opens the properties modal on the selected row). The
  dead-wood half of the audit: an unused mock field and a dead
  assignment retired from the test rigs, and every probe — go vet,
  staticcheck run native (the linux docker run is blind to
  windows-gated files and cries false positives on cross-file
  windows-only symbols), the JS dead-symbol probe, the i18n
  catalog's dead-key rule and go mod tidy — reads clean.
- **Destructive decisions stop reading silence as permission** —
  every existence probe that gates an overwrite now answers honestly
  and fails closed: `transfer.ObjectExists` returns (found, error)
  where `errhelp.IsNoSuchKey` — the typed NoSuchKey code or a bare
  404, the shape a server that skips the XML body returns — is the
  only answer a caller may read as “nothing lives there”, and a
  403, a 500 or a dead socket stays an error. Every caller that
  used to treat a failed probe as free space now refuses: an
  upload or copy/move under the skip policy fails the file with
  “could not check whether … is taken”, the rename policy
  refuses to fall back to a key it cannot vouch for, a rename
  whose destination cannot be probed leaves the original standing
  (with its error line in the activity log), the CLI’s three
  `--no-clobber` legs say so instead of skipping blindly, and the
  compare walk’s local leg fails whole rather than hand sync a
  partial map it would render as phantom remote-only rows — the
  deletes built on those rows would destroy good data. The one
  probe that stays deliberately advisory is the transfer
  dialog’s live collision pre-check (a gray-out hint, not a
  gate), now documented as such. Secure mode’s file-log promise
  joins the honesty: when the settings file cannot be written the
  store still seals, but the log carries the error instead of the
  info line claiming “file logging off” the disk refused to
  make.
- **The activity log grows its missing voices** — every
  bucket-administration verb now lands a line in the log the app
  already keeps: the fourteen setters the Admin panel speaks each say
  exactly what changed (versioning enabled, bucket policy updated,
  CORS rules updated (2), default encryption set to aws:kms (key …),
  tags updated (3)…) scoped *admin* and sourced to the bucket —
  info for what changed, warn for what was removed (an empty CORS or
  lifecycle rule set rides the delete wire-verb and says so), error
  when the wire refuses — while client-resolution failures stay
  silent the way CreateBucket set the law (the caller already holds
  the error, and there is no resolved bucket to source the line to).
  The transfer row's newest verbs log too: pausing and resuming a job
  each say so under the job's own source tag, so the log drawer's
  per-source filter keeps working mid-park. And the vocabulary
  underneath is made honest: LogScopes — the Settings dialog's
  file-log scope selector options, where an unlisted scope is a value
  cleanLogFilter silently drops on save — had drifted to miss four
  scopes the app emits (edit, license, mkfile, security); all
  twenty-two are now listed, sorted, and pinned by a test that walks
  the emitted set.
- **Doctor learns the clock** — a Clock Skew check joins the pipeline
  between TLS and auth: it HEADs the endpoint and compares the local
  clock against the server's Date header, so the RequestTimeTooSkewed
  rejection SigV4 hands a drifted machine is named before it ever fires
  — within 5 minutes passes, past 5 warns, past 15 fails carrying the
  NTP resync commands, a probe that cannot complete skips honestly
  (reachability stays owned by the TCP/TLS checks), and a server that
  sends no usable Date header warns instead of guessing. Both faces
  ride it free — the GUI renders check rows from the registry and the
  CLI report loop already speaks any check — and a latent mapping bug
  rides along: the RequestTimeTooSkewed advice could never match its
  own error code (a normalized-form mismatch nobody caught because the
  test never asserted the cause), and now it does and the test does.
- **Transfers learn to pause** — every running job in the transfer
  manager (uploads, downloads, copies, moves — the synchronize
  auto-legs too, they ride the same walker) grows a **Pause**
  button beside cancel: the park lands between files, the in-flight
  file always settles first (a stream is never abandoned mid-write),
  the row wears a *Paused* chip while it holds, and **Resume** walks
  on at the very file that was next — the park is a decision, never
  a stall (the stall clock restarts on wake, and a paused row is
  never flagged Stalled). Cancel reaches a parked job, settled or
  unknown rows accept neither verb, and the Running tasks window's
  merged rows speak the same paused chip. Behind it one typed law:
  `PauseTransfer`/`ResumeTransfer` on the between-files gate every
  walker already passes.
- **The editor's destructive decisions grow a diff view** — *Push
  anyway* and *Reload from server* were label-shaped consents: one
  force-fed a stale session past the guard on the strength of its own
  label, the other discarded pending edits on a bare text confirm, and
  in neither case did the deciding eye see what was actually about to
  be lost. Both ways out now seat a diff first: the row button opens a
  modal carrying both sides of the session — the staged edit on this
  machine and the server's current bytes — rendered as one unified
  line diff, red for what leaves and green for what lands (the force
  gate paints server→staged, the reload gate staged→server), with the
  honest shapes beside it: binary content (a NUL in the first 8 KiB)
  shows the two sizes instead of a corrupted view, identical contents
  say only the server clock moved, samples are capped at 256 KiB with
  the cut noted, and a file the teammate deleted is named — while a
  diff that cannot be fetched toasts and never opens the gate (the
  row's ways out stay reachable by retry). One typed api serves both
  kinds and both legs (`EditDiff` on the EditTarget grammar — S3 GET
  and engine Open under the source lock, a 60-second budget so a
  wedged wire cannot hang the click), the renderer trims common head
  and tail and bounds its alignment work (past 640k cells the changed
  middle renders as one whole-block change — no false alignment), and
  the old reload confirm is gone entirely: the discard warning rides
  the diff's own framing. GUI-only round by design; the CLI twin keeps
  its honest one-line refusal (the terminal already names what moved
  and keeps the staging copy — its consent surface is content-shaped).
- **Synchronize grows the standing mirror on both faces** — the
  one-shot pair becomes a kept pair. `s3b sync --watch` re-runs the
  sync until Ctrl+C (`--interval`, 30s default): every pass dials
  both sides fresh (an engine's idle wire may have died between
  passes), a pass that moves nothing prints nothing, transient errors
  after the first pass are printed and retried on the next tick, and
  `--watch --json` / `--watch --dry-run` refuse as the usage errors
  they are. The GUI's Synchronize dialog gains *Keep synchronized* —
  off by default, its own seconds field — where the Run click is the
  standing consent: the arm pass and every later tick ride the same
  headless legs (the transfer matrix under skip semantics, the delete
  legs previewed and landed without windows, a bulk delete above the
  safety threshold skipped and named once per count, never forced past
  an L2 gate), any hard failure stops the loop with one honest note,
  and the reopened dialog seats **Stop auto-sync**, which disarms in
  place and reverts the button for one-click re-arm. Session-only by
  design: a background mutator never outlives the session that armed
  it. Pinned by the internal/cli watch rigs (the standing mirror with
  mid-flight adds and removes, the clean stop, after-stop quiescence,
  both refusals) and the battery's sync-auto step (the arm pass's
  headless matrix and delete legs, the cadence toast, the tick's
  re-plan, the stop surface and its quiescence).
- **The editor grows its CLI face: `s3b edit`** — one guarded
  round trip in the terminal, speaking `cp`’s operand grammar
  (`./file`, `s3://bucket/key`, `NAME://path`): the file pulls to a
  staging copy, `$VISUAL`/`$EDITOR` opens it (platform default
  otherwise), and the saved result pushes back under the lost-update
  cure — S3 legs carry the pulled object’s ETag (If-Match; a missing
  ETag degrades the guard off), engine legs compare size and
  modification time as they were at pull (mtime only when both sides
  report a clock), so a teammate’s overwrite mid-edit refuses with
  the edit kept in the staging copy and both ways out named (`--force`
  to overwrite, or edit again from the fresh pull). An editor that
  saves nothing pushes nothing; an operand that does not exist yet
  opens an empty buffer and is created on exit; local paths edit in
  place (the file itself is the store); folder-shaped operands refuse
  as usage errors before any wire moves. Pinned by the internal/cli
  rigs (engine round trip, both guard legs, force, the S3 If-Match law
  against a stateful fake — the 412 refused, the forced landing, the
  fresh create — local in place, the refusals, the editor-command
  resolution).
- **The editor grows the whole matrix — any file the panes seat** —
  Edit existed only for the view source's S3 objects; now every seat a
  file gets one. Remote source files (FTP/SFTP/WebDAV/local-root
  engines) ride a typed EditFile/StopEditFile/PushEditFile family that
  pulls through the engine into a per-source staging namespace (never
  colliding with a bucket's tree), pushes every save back under the
  push budget, and guards against a teammate's mid-edit overwrite in
  the engines' own grammar — size plus modification time, the ETag-less
  twin of the S3 If-Match law — with the same two ways out (Push anyway
  writes past and rebases; Reload from server discards and re-pulls);
  a named source's S3 objects edit through their own client in the
  dual pane; workstation files open in place (the file itself is the
  store — no session, no push); F4 joins the key map (Shift+F4 keeps
  New file); New file on a remote source hands the created file to the
  editor (the "creation only" clause dies); the editor manager's
  engine rows speak the crumb grammar (source:path) and route the
  typed calls; a landed save refreshes the views seating the source
  (remote:changed, the s3:changed twin). Pinned Go-side by the
  lab-engine rigs (watcher auto-save, guard refuse/force/rebase, stop
  legs, refocus, typed parity, refusals) and GUI-side by the battery's
  edit-any step.
- **Synchronize grows the whole matrix — any two sides, both faces** —
  the one-wall rule ("one local folder, one S3 prefix") falls on both
  faces at once. The GUI dialog now takes any pair the dual pane can
  seat — a local folder, an S3 prefix, a remote source directory —
  through the same shared predicate (`pkg/core/syncplan`), with
  destination-worded text where upload/download would lie ("Copy to
  backup-box:/", "{dest} only" radios, a two-way summary that never
  says upload), transfer legs that ride the transfer matrix
  (`TransferCross` under forced *skip* semantics, typed items and
  dests naming the from-side's source) while the local↔S3 pair keeps
  its historical Upload/DownloadRefs fast paths and wording, and
  delete legs that route by kind through the same gated confirmation
  windows — the S3 leg of a named per-bucket source taking the
  source-preview route like every other delete on that surface; the
  only refusal left is the honest one, both sides naming the same
  location. The CLI's `s3b sync` takes the same operand grammar
  `cp` speaks — each operand a local directory, an
  `s3://bucket/prefix/` URI, or a `NAME://` path into any saved
  data source (engine↔engine legs ride the temp spool, local↔local
  mirrors plainly), pinned by the sync matrix rigs. Localized in all
  fifteen languages.
- **Synchronize — the CLI's sync grown its GUI face** — the ⟳
  **Synchronize** button (global bar, View → Synchronize; needs the
  dual pane open) plans a sync between a local folder and an S3
  prefix and runs it: missing and size-differing files copy each way
  under *skip* semantics (mtimes never matter — clocks lie; whatever
  landed in the meantime stays), a direction picker narrows the run
  to uploads or downloads only, and the CLI `--delete`'s shape rides
  an explicit opt-in — *remove files that are not at the source* —
  whose legs route, one direction at a time, through the same
  confirmation windows as every delete, each with its own preview
  and typed confirmation. The plan comes from the same shared
  predicate the CLI rides (`pkg/core/syncplan`), so the two faces can
  never drift, and any other pane pairing gets the honest pointer at
  Compare + copy instead of a broken plan. Localized in all fifteen
  languages.
- **The editor's lost-update guard — pushes are conditional** — a file
  open for edit now records the object's ETag when it is pulled, and
  every push that follows — the auto-save and *Stop & upload* — rides
  `If-Match`: if the object changed on the server while you edited (a
  teammate, another tool), the push refuses with *Precondition failed*
  instead of silently overwriting the newer version. The session turns
  sticky-stale (⟳ *changed on server* — the condition cannot heal on a
  timer, so the watcher stops pushing until you decide), the editor
  pill escalates like a failing push, and the editor manager offers
  the two ways out: *Push anyway* (the informed consent that writes
  past the guard and rebases the session on the new version, so the
  next save guards against the fresh baseline) or *Reload from server*
  (asks first — it discards the edits pending on this machine — then
  pulls fresh). A server whose HEAD cannot be read degrades honestly
  to today's unconditional push, and the push body rides a seekable
  length-pinned section reader so the SDK's header checksum and the
  transport's own length accounting keep torn staged writes from
  landing short. Localized in all fifteen languages.
- **Show destination — one click from a transfer row to where the
  bytes land** — every job now carries its destination typed (the
  bucket and prefix it uploaded into, the remote directory it copied
  to, the local folder it downloaded to), and a settled transfer
  manager row grows a *Show destination* button beside *Retry failed*
  that navigates the app there — the S3 bucket folder, the remote
  source directory or the local pane folder — instead of re-walking
  the tree by hand. In a floating transfers window the pick relays
  through the backend event bus and the main window navigates (the
  SearchGoto shape), so the affordance works wherever the window
  floats; retried jobs re-enter the same entry points and stamp
  their own destination, so the fresh row carries the button too.

- **Server mode guide** — the windowless build (`-tags server`) now
  has a guide of its own (docs/server.md): the two environment
  variables that steer the listener, the `/health` probe, remote
  access with the firewall leg, and — the part that had never been
  written down straight — the trust model: no authentication, no TLS,
  and "local disk" meaning the host's disk, with SSH-tunnel,
  reverse-proxy and trusted-LAN as the three safe exposures, plus
  secrets on a keyring-less host (`S3B_NO_KEYRING=1`), systemd and
  Task Scheduler service recipes, the desktop/server difference table
  (popouts, drag-out's loopback-URL reach, clipboard focus) and a
  troubleshooting table. README, build.md and the usage guide carry
  the pointers.

- **Retry failed transfers — one click, only what failed** — a settled
  job with failed items now carries its own way back: the transfer
  manager row grows a *Retry failed* button that resubmits exactly
  the failed items (a canceled job's unfinished ones) as a fresh
  running job under skip semantics — what already landed stays
  untouched and counts as skipped, never re-copied; a failed file
  (which the integrity round guarantees committed nothing) gets
  another chance with nothing duplicated. No re-dropping, no
  re-picking, no overwrite roulette over a mixed folder: the retry
  replays none of the original per-file decisions, so a rename that
  already landed can never mint a `file (2)` twin. Jobs past the
  per-item row cap (50 items) have no failure map and say so
  honestly instead of retrying blind; internal staging jobs are
  refused; still-running jobs are refused; a clean job answers
  "nothing failed to retry". Every retry goes through the same
  entry points, so its own failures can be retried in turn, and the
  original row keeps its verdict — retry never rewrites history.
- **Compare grows its whole face** — the strip under the toolbar is
  the compare’s home now: a flight shows itself (“Comparing X ↔ Y …”
  with both seats named in the tree’s own grammar — `bucket/folder`,
  `source:dir`, a bare local dir — and the button parked disabled so
  a second click cannot stack a flight on a flight), the verdict
  lands on the same strip as a summary (“✓ 2 identical · ← 1 only on
  left · → 1 only on right · 1 newer on right · 1 size diff”, a
  bucket that is zero stays unsaid) beside the details dialog that
  still stacks with every count, a refused wire says so on the strip
  with a Retry arm that re-runs the same pair, and the X clears the
  banner and every decoration from BOTH grids at once. A verdict
  about seats nobody sits in anymore retires by itself — moving the
  main view or the pane past a verdict, or a flight landing after
  its seats moved, clears it, so phantom arrows never stain rows the
  compare no longer speaks about.
- **The File menu seats the workspace verbs** — *Data Sources…* opens
  the new source manager: every configured source in one list with
  its type and target, Edit reopening the editor on the row, Remove
  beside it, Add starting a fresh one. *Import S3 Credential…* keeps
  its seat, and *Clear All…* resets the workspace to a freshly
  opened state behind a Cancel/Clear confirm: every modal and
  floating window closes, the dual pane closes and unbinds, selection,
  clipboard and filter rest, the history empties (Back and Forward
  disarm) and the view re-homes to the first source — while saved
  settings, sources and preferences survive (a reopened app keeps
  them) and running transfers keep running (they are the wire’s
  business, not the workspace’s). Upload left the File menu for the
  view verbs that always owned it (toolbar, context menus, empty
  states), and New / Open / Close profile file ride the sidebar’s
  background context menu beside the source verbs (Ctrl+S and Open
  keep their File-menu seats; Save takes its own save-as flow when
  there is nothing to overwrite).

- **The CLI’s discovery face: find over any saved source, TAB completion
  for the whole vocabulary** — `find` takes a source URI beside
  s3://bucket[/prefix] (NAME://dir over any saved non-S3 source), walking
  the source’s own engine depth-first through the same filter pipeline
  (--name, --ext, --path, --larger/--smaller, --older/--newer, --kind,
  --limit): hits speak their path relative to the searched root the way
  the S3 leg always has, the JSON face carries the source’s name, the
  stderr summary names the URI it walked — and `--class` over a source
  refuses up front as a usage error (remote trees carry no storage class;
  the GUI search refuses the same mix with the same words). Every operand
  learns to complete: source names on `source test` and `source remove`,
  profile names on the persistent `--profile` flag and `profile test`, the
  browsing grammar (s3:// plus every saved NAME://) on ls, tree, du, stat,
  mkdir, rm and find, the s3://-only grammar on the versions, lock, admin,
  presign, doctor and sc verbs (a put verb’s FILE position goes back to
  the shell), cp/mv/sync/edit offering URI candidates on both seats while
  the shell’s own file completion stays alive beside them, and the static
  enums — storage classes on sc’s second seat, retention modes on
  `lock retention --mode`, the exact source-type list `source add --type`
  accepts. Every completer is offline (it reads the profile store and the
  verbs’ own enums, never dials a wire) and silent with an error
  directive on a store it cannot read — the command itself will speak —
  and cobra’s own speech (help screens, the completion protocol) rides
  the swappable writer so capture-based tests see the completion stream
  too.

- **In-app image viewer** — double-click an image in any view that
  seats files — S3 objects, remote sources, workstation files, the
  dual pane — and it opens in a lightbox over everything without
  touching disk: the view asks the bridge for a bounded slice
  (`PreviewData` caps at 16 MiB, sniffs the content type, speaks the
  true size and whether the read was cut) and renders it as a data
  URL. Arrow keys walk the folder’s images in grid order, Esc closes,
  a click on the image toggles fitted and actual size, and the context
  menus grow a Preview item beside Open. A row past the bound is
  refused aloud — the exact size and the cap — without reading a byte
  over the wire, a failed read toasts while the seat stays honest
  with its name, and a folder activation never seats the viewer.
- **The glance learns text — and the terminal gets `cat`.** The
  in-app viewer now seats readable files, not just images: a readme,
  config, CSV or log double-clicked (or Preview’d from the menu)
  opens in the same lightbox as a monospace scroll with Wrap and
  Copy verbs — still no download, nothing on disk. The walk becomes
  the previewable union (images and text one sequence), the desk is
  chosen by the wire’s sniff with the extension vouching the way
  the SVG law always has (octet-stream on a readme still seats
  text; an image wearing a text name reroutes to the image desk),
  and text shows its first 512 KB while Copy speaks the whole
  read. The CLI gains the twin: `s3b cat s3://bucket/key` (or
  `NAME://path`, or a plain workstation path) streams one file’s
  bytes to stdout — it pipes cleanly into grep or jq — with the
  same operand routing `edit` uses and folders refused up front.
- **Favorites grow from buckets to locations.** A favorite is now a
  typed place — an S3 bucket root or one of its folders, a remote
  directory, or a workstation folder — pinned from any context menu
  that seats one (bucket rows and folder rows in the grid and the
  tree, remote directories, workstation folders) under the label
  grammar the app already speaks: a bucket shows its name, a folder
  the bucket/prefix walk, a remote directory `source:path`, a
  workstation folder its own name, the full address on the tooltip.
  The M5 law stored bare bucket names and bound them to whichever S3
  source was active at click time — with two S3 sources a favorite
  silently followed the last one viewed; the typed pin carries its
  own source and survives source switches, legacy stores upgrade in
  place on the first render that can resolve an S3 source, and a pin
  retires itself when its source disappears — but never against a
  provisional world: while an account-wide legacy source can still
  split into the bucket-scoped sources it names, neither the upgrade
  nor the retirement runs (the boot pass that lands before the split
  would otherwise destroy exactly the pins the split is about to
  legitimate, a race the verification battery caught pinning a folder
  of a bucket the split had not minted yet). Clicking a pin navigates
  its exact location, the pin’s own context menu opens or unpins
  without navigating, the sidebar funnel narrows pins by label the
  way it always narrowed buckets, and favorites are preferences —
  they survive reloads and Clear All.
- **The app reopens where you left it.** The main view remembers its
  seat — a bucket, one of its folders, a remote directory, a
  workstation folder — on every navigation, and the next launch
  re-seats it while its source still resolves; a seat whose source
  disappeared falls back to the first source’s home, and that fall
  re-records it (a freshly opened app remembers exactly what it
  opened on). The restore seats synchronously when the boot pass can
  already see the stored source and completes through a bounded late
  step when it cannot — the legacy split runs concurrently with boot,
  so the very source the seat names may be seconds from minting, and
  the restore defers to a pending split without ever crossing a
  settled world (the provisional-world law that governs the favorites
  upgrade) and without delaying the initial seat, whose timely
  landing is what the split’s own re-home catches. Clear All wipes
  the key with the other session seats and its re-home re-records
  the first source; popout windows never write (they render one
  floating view, not the workspace); a malformed or absent store is
  simply ignored.

- **The gallery: the listing grows a tile face** — the toolbar's new
  🖼 button seats every folder and file as a tile (a glyph by type, its
  name beneath), and every image tile earns a live thumbnail through
  the same preview grammar the viewer owns: a bounded PreviewData
  sliver (192 KB, `maxBytes` threading through the Go side's caller
  cap — the Range request, the remote engine lock and the local read
  all honor it now), never a download and never a byte on disk beyond
  the bridge. The tiles share the rows model, selection and verbs
  outright — click, ctrl/shift grammar, Ctrl+A, Escape, the status
  bar, the toolbar actions, the context menu, double-click activating
  folders and seating the viewer — so the tile face is a view of the
  listing, not a second one; the arrow keys walk visual order (Up and
  Down stepping by the laid-out column count), Enter activates, Home
  and End hold the ends. Thumbnails load lazily through an
  IntersectionObserver (a pool of four reads, a 240-entry cache swept
  on overflow), a refused sniff or a failed read keeps the glyph
  silently and retries on the next seating, an SVG's text/xml sniff is
  vouched by its extension the way the viewer always has, and a
  generation token keeps a view that moved on from paying for tiles
  nobody seats anymore. The secondary pane carries its own face — the
  same button family the pane toolbar owes the main view, its own
  toggle and preference (`s3b-gallery-pane`), speaking the binding
  grammar so a pane bound to another source thumbnails through it.
  The preference persists beside the column layouts and survives
  Clear All. Underneath, the model hook widened: streamed pages
  (appendRows) fire the same on.model contract apply() always did,
  because a streamed page is a model change too.
- **The inspector: the selection grows its own panel** — F8, the
  ℹ toolbar button (the pane’s toolbar mirrors it, both toggling
  the same panel; the View menu speaks it too) seats a docked
  details column on the right edge that renders the selection the
  instant it changes: a single row’s own facts — kind, size, last
  modified, created, storage class, the source, the key or path —
  read from the row itself, never a round trip (the deep facts —
  ETag, SSE, retention, usage — stay a Properties… click away
  through the same modal Alt+Enter always opened), a
  multi-selection aggregated the way the Properties dialog
  aggregates (items, total size, modified range), a view summary
  when nothing is selected (the location named the way the
  breadcrumb names it, item and size counts, the source with its
  type), and quick verbs that follow the seat — Open for folders,
  Preview and Edit for files, Download…, Copy path,
  Properties… — each the exact verb the seat’s own menus
  speak. Whichever grid spoke last owns the panel: a selection in
  the dual pane hands it the seat (the pane’s button and verbs
  speaking the binding, its summary naming the pane’s own
  location), a closed pane hands the seat back to the main view,
  and the preference (`s3b-inspector`) survives Clear All the way
  every face preference does.
- **The gallery finishes its grammar: tiles that drag, drop and walk** —
  every tile is a drag source the way its row always was — the same MIME
  and the same payload (source, bucket, dir), the OS drag-out URL riding
  the precomputed `DownloadURL` seam in browser mode, and a thumbnail
  never hijacking the gesture (the picture is not draggable, the tile is)
  — and folder tiles are drop targets speaking the one grammar the rows
  speak: the grid’s new `acceptedTypes()` names the payload types both
  faces receive, a dashed highlight marks the hovered folder, and the drop
  parses through the same dispatcher — a tile dropped on a folder tile
  lands the exact transfer the row-on-row drop lands (the in-view
  server-side copy behind the version choice; a pane-bound folder tile
  streams through the cross engine the tree targets ride, because the
  pane binding addresses its source by ID). The keyboard completes too:
  PageUp and PageDown stride by twenty the way the rows stride, and each
  face owns its keys through a listener on its own wrap — the grammar
  that gives the PANE gallery the keyboard it never had (arrows, Enter,
  Home, End and the page strides walk the pane’s tiles while the main
  view’s selection stays put, and the pane body’s Backspace climb and
  grid walk stop propagation so the document chain never double-walks a
  focused pane), while modified keys (the Alt+arrows history walk) stay
  the global chain’s — a bare arrow walks tiles, a modified one walks
  history.
- **The listing answers as you type, on every face** — type-ahead
  grows from a rows-only privilege to the law of every face: the
  same buffer (`grid.typeFeed`) folds each typed letter — 600ms,
  case-folded, prefix — and names the first row that carries it, the
  rows committing the jump as they always did and both tile faces
  committing it their own way (selection, focus, anchor, the
  face’s sync, a nearest scroll) through the walker’s printable
  branch, reached by the face’s own wrap listener or the global
  chain, whichever is seated. A miss moves nothing but the buffer
  still holds the keys for the next letter — the rows’ own law,
  now every face’s. And the buffer speaks: a small chip pinned
  into the corner of the listing being typed into says what it
  holds while it holds it and leaves when it expires, following
  whichever grid and whichever face owns the keystroke — the pane
  gallery answers a typed letter while the main view’s selection
  stands still, the pane rows answer through their own body, and
  the chip sits over the right listing each time.
- **The walk speaks: screen readers name the seat** — every
  walkable surface now carries `aria-activedescendant` naming the
  row or tile the keyboard walk, the click, the type-ahead jump or
  the drag last seated: the rows body (ids minted on the stable
  pool position, so recycling never mints a new name), both gallery
  wraps (the tile position, maintained by the same sync that paints
  selection), and the search results list (the hit position, kept
  honest through both the in-place click patch and the
  re-rendering walk) — a screen reader speaks the listing the way
  the status bar reads it, with no per-row DOM focus moved or
  stolen. The tree joins the semantics its role always promised:
  each flat row a `treeitem` carrying its own `aria-level` and an
  `aria-expanded` that claims expansion only where the twist
  itself does (an unloaded folder stays silent the way its hidden
  twist is).
- **The menus answer the keyboard** — the Menu key (Shift+F10 its
  laptop spelling) opens the verb menu of the walked row or tile on
  every face the pointer can right-click (the rows, both gallery
  faces, the pane bodies): the focus row is seated into the
  selection exactly the way the row’s own contextmenu listener
  seats it, and the pool row or tile rides the same `on.context`
  seam as the anchor, so the menu opens beside the seat with no
  pointer there. Every open menu is now a keyboard surface of its
  own, mouse or Menu key alike: focus lands on the first enabled
  item, the arrows walk the level with wrap, Home and End hold the
  ends, Enter activates, Escape closes and hands focus back to the
  opener (a picked verb restores it before the verb runs, so a
  modal captures the seat as its own previous focus), and the
  cascade flyouts open under ArrowRight and fold under ArrowLeft
  with the parent item claiming `aria-expanded` while its flyout
  stands. The menus speak their semantics for screen readers
  (`menu`, `menuitem`, `separator`, `aria-haspopup`,
  `aria-disabled`). And the round healed a long-standing trap the
  toolbar leg caught: Enter on a focused control outside the
  listing seats (a toolbar or menubar button, a modal foot
  button, a crumb link) no longer falls through to the grid walk
  under a `preventDefault` that swallowed the click — controls
  keep their keys.
- **The verbs that had no row earn their witnesses, the codebase sheds its dead wood, the docs tell the 1.2 story at last** — the round opens with an audit: every button, menu verb and CLI flag walked against the battery's coverage, and the honest gaps were the Test verbs and the terminal. GUI-96 pins the Test family over the live bridge — the add-source editor's Test footer dials the typed draft through TestS3Draft (good credentials answer ✅ connected — bucket accessible; a wrong secret answers ❌ with the dial's error) and Cancel leaves ListSources without the probed source, the test-without-save law, because a probe is not a promise and only Save writes — while the tree row's Test connection… probes the stored source through TestSource and toasts its ✅ verdict. The gui-visual battery grows four wiring pins of the same family: the local view's Open terminal here carrying the open folder over OpenTerminal, an sftp source's Test connection dialing its root through RemoteList, a bucket-scoped s3 source riding TestSource beside its toast, and the account-wide row's verb riding TestProfile — pinned in the offline-fallback world, the only world where an unsplit account still lives (the topology the round mapped: editor drafts ride TestS3Draft, import candidates ride TestCredentialDraft, and a tree row rides TestProfile or TestSource by its scope). The round's own full batteries paid one more find, twice: GUI-95's no-residue assert caught a provider acknowledging the doctor's probe delete while the key kept listing live half an hour later — the second run's doctor even verified its cleanup with a HEAD that honestly answered not-found, the shape of a probe whose PUT flushed after its DELETE — an orphan two hundred and sixty replays could not reproduce outside the battery. So the write round-trip no longer trusts the ack: after the delete it verifies with the reader's own oracle — a listing — across a settling window wide enough for a late flush to surface, re-issues the delete while the store still lists the probe, and warns naming the key when a leftover cannot be ruled out, confirmed riding the check's JSON with the lying-delete and retry-cure rigs pinning both roads; and the census itself learned the same patience and then honesty, re-checking a first sighting across a settling window and classifying what persists by its version trail — a probe resurrected markerless after a verified-clean delete is the store undoing its own acknowledgment, skipped with its reason like the other recorded provider gaps, while a markered residue still fails. The dead-wood sweep follows the same audit's other half: conflictChoices drops the kind and target no caller passed, the size bar's usageOf drops the loc it never read, pkg/setup's ParseScope goes with its rigs, go vet and the deadcode probe read clean, the struct-field probe's every survivor is a binary or XML layout contract, and go mod tidy moves nothing. The docs catch up with the forty-nine rounds above the last beta: the README's status paragraph tells the whole 1.2 arc and six new feature sections seat it — editing under a lost-update guard, transfers that pause and resume under a speed limiter, Synchronize, the dashboard admin panel, the nine-check doctor, the tunable face — beside the installer's scope question, the s3b setup verbs and a screenshot table that grows the doctor, the editor, the Synchronize dialog and the viewer; cli.md is regenerated through the twenty-four-command census; the quickstart and the comparison ride; and seventeen screenshots are retaken from the round's own green runs, three of them faces the docs never had — the editor, the viewer, the Synchronize dialog. Gates: js-check OK, the i18n catalog untouched at 537 keys across fifteen locales, provenance 9/9 (the main.js watermark undisturbed through every node patch), the Go suite green under -race in docker (the two wails binaries stay CI-only), the full battery green at 174 PASS · 8 SKIP (the recorded MinIO provider gaps, the doctor-probe resurrection among them) · 0 FAIL, and the gui-visual battery green at 1496/1496. Docs: the CHANGELOG carries this bullet above the dashboard round's, and VERIFICATION's round row grows the Test-verb rows, the wiring pins and the real count.
- **The admin panel becomes a dashboard of doors, the doctor asks before it writes** — the Overview grew a switch it should never have carried and a tab wore a name it never earned: the overview is for showing, the pages are for changing, and the round enforces both. The Overview tab is now five door-cards — versioning, public-access block, default encryption, object lock, each tinted by its tone, and a Contents card that fills asynchronously from the bucket's version statistics (current objects, their versions, their delete markers) — every card a real button whose tooltip names its door and whose click seats that page, with a chevron speaking the same; beneath them a chips row — Policy with its statement count, CORS, Lifecycle, Website, Tags — lit when configured, and every chip a door too, while the bucket identity keeps its Copy s3:// URI row. The versioning toggle moved to the Versions page, where the versions it produces are listed, seated beside the cleanup tools and the explainer of what enabling means; the Overview carries no switch anymore. The tab mislabeled Lock is renamed Object lock, in the rail, in its key, and in the docs' grammar. Every async seat now speaks its state: the admin panel's reload, the Versions statistics and the doctor picker's check list paint a real loading block — a spinner and its label under role=status — while they fetch, and the shared foot builders wire every modal and popout footer verb to wear a spinner while its call is in flight, so no wait is silent. The File menu's Import S3 Credential becomes S3 Credential, and the dialog becomes a manager in the Data Source idea: the S3 data sources you already have seat at the top — Your S3 data sources (N), each row naming its endpoint and bucket scope — where Remove asks before it takes a source down (an honest Cancel keeps the source and never crosses the bridge) and an empty state speaks the truth when none exist yet, with the import face — file, service, manual — riding below unchanged. The doctor's write round-trip is opt-in, because it is the one check that writes: a consent checkbox in the window, unticked by default, and a --write flag on the terminal — a run without consent reports the Write Round-trip Check as SKIP with its reason (it is opt-in because it writes and deletes a tiny probe object) and the summary counts its skips, while a consented run pays the round-trip as before, the allowWrite argument riding RunDoctor and RunDoctorCheck through the api, the window and the CLI. The battery grows fifteen oks: the doctor step pins the unticked default, the skip pill with its reason and the consented rerun passing the consent through; the admin step pins the five door-cards, the tooltip grammar, the chips with their configured tone, the asynchronous Contents fill, both door kinds seating their page, the switch living on Versions and the Overview carrying none; the manager pins ride the import step — the seated sources, the Remove confirm, the honest Cancel — and a curated admin-overview shot joins the artifacts for the docs (its subject check the fifteenth ok). CLI-S3-20 walks the opt-in on the terminal side (the default skip with its reason, the --write pass), GUI-32 follows the toggle to the Versions page, GUI-73 follows the Object lock rename. Gates: js-check OK, the i18n catalog at 537 keys across fifteen locales (doctor.allowWrite), provenance 9/9 (the main.js watermark undisturbed through every node patch), the Go suite green under -race in docker (the doctor rigs grow the allowed and disallowed roads on Run and RunCheck — the two wails binaries stay CI-only), the full battery green at 169 PASS · 7 SKIP (the recorded MinIO provider gaps) · 0 FAIL, and the gui-visual battery green at 1492/1492. Docs: the CHANGELOG carries the bullet above the installer round's, VERIFICATION the row (GUI-130), and usage.md the manager sentences, the dashboard-of-doors paragraph, the doctor's opt-in clause and the --write example.
- **The installer asks who it is for, the terminal grows a setup face** — the Windows setup wizard lived one law only: every install was for all users, administrator approval and Program Files every time, while the per-user operator had no seat at all. The wizard now asks: a scope page (real radio buttons — *All users of this computer* against *Current user only (recommended)*, each with its one-line consequence) ahead of the components and directory pages, and the whole ride follows the answer — a per-user install lands in %LOCALAPPDATA%\S3 Bucket Browser with its shortcuts in the user's own Programs menu and both registry keys (the Apps & Features uninstall entry and the App Paths registration that lets Win+R find s3b without touching PATH) in HKCU, needing no administrator for install or uninstall; an all-users install probes HKLM first and, from an unelevated context, re-launches itself elevated once via UAC instead of failing. The uninstaller follows the recorded selection — the uninstall key carries an InstallScope value and the uninstaller resolves its hive from it and from the recorded install location, so uninstalling a per-user install never asks for elevation while an all-users one re-asks — and both faces now write the 64-bit registry view (SetRegView 64), because the 32-bit NSIS compiler quietly wrote WOW6432Node, invisible to every 64-bit reader: the round's own control experiment caught the old script's keys landing in the 32-bit view where the CLI could never see them. Silent deploys keep their contract: /S /CURRENTUSER or /S /ALLUSERS picks a scope explicitly, a silent run with no flag still defaults to all users (the behavior of every version before the choice existed, so existing automation rides unchanged), /D= is honored rather than stomped (NSIS applies it before .onInit runs — only an empty $INSTDIR is defaulted), a prior install is upgraded in place (same scope, same directory) unless a flag overrides, and the payload lands as s3b.exe through File /oname= whatever the staged artifact was named. The terminal twin is s3b setup on every supported OS — setup install --user (the default; %LOCALAPPDATA%\s3b on Windows, ~/.local/bin on unix) or --system (Program Files\s3b needing an elevated terminal, /usr/local/bin needing sudo), setup uninstall removing exactly the scope the install made (the flag-less call removes whichever scope is installed, user first), and setup status naming both seats, which one PATH resolves to, and any foreign registration (the wizard's) without following it — all with --json. The new pkg/setup owns the law: stage-then-rename replacement (a running binary is refused with its remedy, never truncated mid-flight), a dedicated-dir-only removal rule (an emptied %LOCALAPPDATA%\s3b goes, the shared ~/.local/bin never does), an honest ErrNotInstalled verdict that outranks the elevation check (a user without rights asking about an empty scope learns there is nothing to remove, not a demand for elevation), and S3B_SETUP_USER_DIR relocating the user seat for portable machines and CI while skipping shell registration for it. The battery grows the CLI-M-09 row (the redirected install→status→uninstall round-trip with the portable note, the JSON seat, the both-flags usage refusal, the honest empty-uninstall verdict, and the un-redirected status provably read-only on the real seat) and the command census grows to 24 with setup (provenance stays hidden by its own law — deliberately absent from --help so the census never asserts it). The round's full run also caught two battery rows the October 8 hardenings had left behind: CLI-S3-47's operand legs still expected the pre-matrix refusal while s3-to-s3 and local-to-local have been lawful sync pairs since the matrix round (the legs now pin the one refusal that survived — the degenerate same-location pair — beside a dry-run matrix plan that moves nothing), and GUI-84's push leg still rode the editor's pre-guard side-door clobber while the lost-update cure made the explicit save keep the If-Match guard and survive stale (the row now pins the refusal, the stale survival, the Push anyway consent that lands the last write, and the clean settle). Gates: js-check OK, provenance 9/9 (the main.js watermark undisturbed through every node patch), the Go suite green under -race in docker (pkg/setup's fourteen rigs: seat creation and registration, the portable redirect, in-place replacement without .new litter, the same-binary noop, the unelevated system refusal, the partial-registration verdict, the seat/dir/shared-dir/foreign-file/foreign-registration uninstall laws, the honest not-installed, and the probe's PATH resolution — the two wails binaries stay CI-only), the installer validated live on the box (a per-user silent install with a custom /D=, the no-flag upgrade ride staying per-user, both silent uninstalls leaving nothing behind, the all-users leg through one elevated run, and setup status naming the wizard's install as another integration), and the full battery green at 169 PASS · 7 SKIP (the recorded MinIO provider gaps) · 0 FAIL, and the gui-visual battery green at 1477/1477 (no frontend surface moved). Docs: the CHANGELOG carries the bullet above the admin round's, VERIFICATION the row (GUI-129), and usage.md the install-scope section with the wizard's silent flags and the s3b setup verbs.
- **The admin panel finds its categories, the doctor speaks its verdict** — the admin dialog kept all eleven of its tabs but seated them as one flat strip, so a hardening question meant scanning an ungrouped ladder, and the doctor answered with a bare checklist a reader had to walk line by line to learn the run’s shape. The admin panel now renders a rail: the same eleven tabs (the registry untouched) grouped into General, Security & access and Data & content under a vertical tablist of real buttons with a roving tabindex — arrows, Home and End walk and select, the bucket name and its region head the rail, every pane opens under its own title and one-line description, and a completeness guard seats any tab a future registry adds into a trailing group instead of dropping it. The Overview tab became a dashboard: four stat cards (versioning, public-access block, default encryption, object lock) tinted ok/warn/danger at their border, the bucket identity rows with a Copy s3:// URI button beside the versioning toggle that always lived there; the Policy tab gained a Format JSON button plus a save-time parse that refuses to write a policy that does not decode. The doctor grew two checks and a face: the credential identity check (ListBuckets names the account owner and how many buckets the keys can see — a 403 warns that the access may be scoped to one bucket rather than failing, and the check skips honestly when no S3 client exists) and the write round-trip (a tiny .s3b-doctor-probe marker object put and deleted through the same client — read-only keys warn with the refusal, a probe that cannot be deleted warns that the key remains in the bucket, and the happy road cleans up after itself), both wired into Run and RunCheck and both listed by CheckNames, now nine. The report itself grew a meta block — region, the masked access key (first four and last four characters, all stars for short keys, never the full credential even in JSON), path-style, the total in milliseconds and the UTC run time — which the CLI’s doctor prints beside the endpoint (region:, key:, total: N ms (ran at T)). The window groups the checks under Connectivity, Bucket access and Identity & writes headers with unknown names trailing under a catch-all, opens every run under a verdict banner that speaks the shape first (N failing checks, N warnings, the all-clear, or N skipped) in the app’s ok/warn/danger tones, seats the region, the masked key and the total in its header rows, and carries a Copy report button that puts the full report JSON on the clipboard and flips to Copied ✓. The battery grows twenty-one oks across its two touched steps: the doctor step pins the meta names, the three group headers, the warn verdict, the copy flip and the identity row expanding to its owner info, and the admin step pins the three category labels, the tablist semantics (eleven real tab buttons, exactly one aria-selected), the four stat cards, the Overview head, the arrow walk from Overview to Versions, and a per-tab head-name ok riding all eleven panes of the existing walk. Gates: js-check OK with the i18n catalog untouched at 536 keys (the dialogs stay English-hardcoded by their own precedent), provenance 9/9 (the main.js watermark undisturbed through every node patch), the Go suite green under -race in docker (the nine-check registry pin, the identity pass and 403-warn roads, the write round-trip’s happy, read-only, undeleted and needs-bucket legs, maskKey’s table, the report-meta rig with the JSON leak guard, and the live-fake cleanup pin — the two wails binaries stay CI-only), and the full battery green at 1477/1477. Docs: the CHANGELOG carries the bullet above the credentials round’s, VERIFICATION the row (GUI-128), and usage.md the admin rail sentences plus the doctor’s widened check list and verdict/copy prose.
- **Credentials by hand, and a paste that finds the pane a home** — the import dialog grew its third origin beside files and services: *Enter manually…* opens its own modal over the import dialog (a type select, one flat form of friendly field names, password-type inputs with autocomplete off and the mono face on secret fields), and Add hands one flat map to the backend’s new AddCredentialDraft — kind s3 (name, endpoint, region, access key, secret key, session token, an optional pinned bucket) or any remote protocol (name, host, port, username, password, start directory) — where the parser’s alias grammar builds the same stashed candidate a file would: required fields refuse honestly (an S3 entry without its secret, a remote without its host or user, an unknown kind), the name defaults from what the entry names (the endpoint’s host, s3-<region>, the remote host), the secret crosses the bridge only inside the call and lands in the stash behind HasSecret, and Test / Import / Discard ride unchanged — the re-import law included (a manual entry naming an existing bucket-scoped source updates it in place, never a duplicate root). The secondary pane learned the same hospitality on the paste side: a paste aimed at a pane sitting at an empty location no longer refuses or rides off with the document chain’s main-view preference — the pane’s empty-area menus route through panePasteScoped, so a local binding with no directory open (the This PC roots view) lands the paste in the workstation home, the same place a fresh pane navigates to, and an S3 binding sitting on its bucket list asks which bucket should take it: a real picker over the source’s buckets where Cancel is honest (no transfer, no lingering modal) and a pick navigates the pane to the chosen bucket and transfers into it; the document Ctrl+V keeps its main-view precedence but falls back to the pane’s own home when the main view has no destination either (a legacy account-wide source’s bucket list). The battery grows two steps: import-creds-manual (seven oks — the File-menu road, the Enter manually… button beside the file and service roads, the modal opened over the import dialog, the typed flat map pinned on the bridge call, the candidate row carrying its manual-entry origin, the Import riding the same stash law, and the no-new-tree-root pin that keeps the tree-scale round’s 5011 whole) and pane-empty-paste (five oks — the trusted-copy seed through the OS mirror, the directory-less local pane pasting into the workstation home through the pane’s own empty-area menu, the bucket picker listing the source’s buckets, an honest cancel with no transfer and no lingering modal, and the pick landing the transfer in logs-2026 with the pane itself navigated there). Gates: js-check OK, provenance 9/9 (the main.js watermark undisturbed through every node patch), the Go suite green under -race in docker (AddCredentialDraft rigs: the manual happy path, the refusal grammar, the remote and name-defaulting legs — the two wails binaries stay CI-only), and the full battery green at 1456/1456. Docs: the CHANGELOG carries the bullet above the tree-window round’s, VERIFICATION the row (GUI-127), and usage.md the manual-entry bullet plus the pane’s paste-acceptance sentence.
- **The tree face survives its own five thousand** — the rows, search and gallery faces had all learned the window law while the sidebar still lived the old one: one expansion of a date-partitioned bucket seated every folder row at once, so a big source meant thousands of treeitem rows in the sidebar DOM. The tree now renders a window over a flattened order (roots sorted, depth-first through open nodes — the one list the walk, the window and the footer truth now share, computed once per render): TREE_WINDOW rows to start (four hundred), grown a page at a time (TREE_PAGE) over three append-only roads that never rebuild what is already seated — the scroll seam, an IntersectionObserver over the last seated row (isIntersecting-guarded: the observer speaks on every registration too, the law the gallery round paid to learn); the walk itself, where End, an arrow past the seated edge or a type-ahead jump calls ensure() so the keyboard never names a row no reader could see; and a truthful strip under the tree (.t-window, a real button OUTSIDE the tree — a real button is not a treeitem), speaking how many rows are shown of how many exist with a Show more verb, hidden entirely while everything fits so small worlds render byte-for-byte exactly as before. Navigation IS a focus move: the current row always seats, so a render grows the window to the current row’s page boundary when it lands beyond the seat (the law the round’s own slice caught — expanding a five-thousand-folder source while the view sits on a source that sorts after it seats the whole order to reach the current row, honest and correct, and the step now navigates first so the window law shows itself). The side footer keeps speaking the true count (5011 items while the window paints its four hundred), and the filter law is unchanged: under a pattern the order narrows through the same visibility gate the rows always rode and the window hides when everything fits. treeShown/treeMore speak fifteen languages (the checker green at 536 keys). The battery grows the tree-scale step with ten oks in one run: five thousand folders seeded into archive-cold, the click-tree road seating the window at exactly four hundred rows with the order at 5011, the strip speaking Showing 400 of 5011 with its verb, the footer speaking the true 5011 items, the scroll seam growing the seat a page, the verb growing it another page with the truth re-spoken, End growing the seat to the last row and naming it through aria-activedescendant (pinned against the order’s own last node, never a hardcoded label) with Home walking back to the first, the filter narrowing the five thousand to its hundred needles with the window hidden and the dim-ancestor law intact, and the leave-clean tail returning eleven source rows with the pattern cleared, the panel closed and the bucket re-emptied. Gates: js-check OK, provenance 9/9 (the main.js watermark undisturbed through every node patch), the Go suite untouched by design — a frontend-only round, the 23 packages unmoved — and the full battery green at 1444/1444. Docs: the CHANGELOG carries the bullet above the gallery round’s, VERIFICATION the row (GUI-126), and usage.md’s sidebar-tree bullet the window-law sentence.
- **The gallery face survives its own five thousand** — the rows face had
  gotten its pool pinned at five thousand while the tile face over the
  same model still seated one button per row: the thumbnails were lazy
  but the DOM was not, so a big bucket meant thousands of tile nodes
  (and their listeners) in one face, and every streamed page rebuilt
  the whole grid of them. The gallery now renders a window of the
  model — three hundred tiles to start, grown a page at a time over
  three append-only roads: the scroll seam (an observer over the last
  seated tile that grows the window before the eye arrives, honest
  only to a true crossing — the observer’s own initial
  non-intersecting report is not one, a bug the new battery step
  caught in the product’s first cut), the walk itself (End, a
  type-ahead jump or an arrow past the edge grows the seat it names,
  so the keyboard never names a tile no reader can see), and a
  truthful strip under the tiles — a real button seated OUTSIDE the
  listbox, the same law the search face learned — speaking how many
  tiles are shown of how many exist with a Show more verb, hidden
  entirely while everything fits so small worlds render byte-for-byte
  exactly as before. Selection keeps speaking the model (select-all
  says five thousand while the window paints its three hundred — the
  row pool’s own law), sorting still orders the whole model, and the
  strip paints in the results area’s idiom across fifteen languages
  (galleryShown/galleryMore, the checker green at 534 keys). The
  battery grows the gallery-scale step with eight oks in one run:
  five thousand objects seeded into archive-cold, the gallery seating
  exactly the three-hundred-tile window rather than the world, the
  strip speaking Showing 300 of 5000 with its verb, select-all
  painting three hundred seats while the status bar speaks the five
  thousand, the scroll seam growing the window a page, the verb
  growing it another page with the truth re-spoken, End growing the
  seat to the five-thousandth tile and naming it through
  aria-activedescendant with Home walking back to the first, and the
  leave-clean tail returning the rows face with the tail hidden, the
  bucket re-emptied and the preference removed. Gates: js-check OK,
  provenance 9/9 (the main.js watermark undisturbed through every
  node patch), the Go suite untouched by design — a frontend-only
  round — and the full battery green at 1434/1434.
- **The search face survives its own hits** — the scale round had
  pinned the main listing’s pool boundedness at five thousand rows, but
  the search results grid still lived the old law: it rendered every
  hit the run matched (the Limit field defaults to ALL, so a broad
  pattern could seat tens of thousands of rows in the DOM at once),
  rebuilt the entire list for every streamed page, and rebuilt it again
  for every arrow key — a walk over a big result set re-rendered the
  world per keypress. The results body now renders a growing window of
  the model: five hundred rows to start, a thousand more per press of
  the truthful strip that appears under the list when the model
  outgrows the seat — the strip speaks how many rows are shown of how
  many exist and carries the Show more verb (a real button, seated
  outside the listbox because a button is not an option), it hides
  entirely while everything fits so small worlds render exactly as
  before, and the status bar keeps speaking the true count throughout.
  Sorting still orders the whole model — the window re-renders over
  the ordered list — and the walk both clamps itself to the rendered
  window and patches its seat in place: selection, aria-selected, the
  activedescendant name and the nearest-scroll all land on the seated
  row without a rebuild, the click riding the same law, so a run of
  twenty thousand hits sorts, speaks and walks as if it were twenty.
  The battery grows the search-scale step with seven oks — the true
  count spoken at twenty thousand hits, the body seating the
  five-hundred-row window rather than the world, the strip’s whole
  truth with its verb, the verb growing the seat a step, the size sort
  mirroring the model at the window’s edge (the two smallest keys
  computed from the seed law, never hardcoded), the walk striding in
  place with no rebuild per keypress (element identity pinned through
  page-side references), and the leave-clean tail re-emptying the
  world. Gates: js-check OK, provenance 9/9 (the main.js watermark
  undisturbed through every node patch), the Go suite untouched by
  design — a frontend-only round, the 23 packages unmoved — and the
  full battery green at 1426/1426.
- **The listing at five thousand rows** — the battery’s worlds had
  only ever spoken in tens of rows while production lists run to
  thousands, so every grammar the listing owns now has a step that
  seats it at scale: the new big-listing step seeds five thousand
  objects into the once-empty archive-cold bucket (its emptiness is
  load-bearing only in earlier steps, and the world is re-emptied
  before the walk ends) and pins the whole ladder in one run — the
  model seated at exactly five thousand, the row pool staying
  bounded while the scroll extent covers every row (a handful of
  rendered rows over a hundred-and-forty-thousand-pixel track), the
  deep jump to the bottom rendering the model’s final row, the size
  sort mirroring the model at both ends, the keyboard walk striding
  twelve rows with the seat still named through
  aria-activedescendant, the quick filter finding its ten needles in
  the haystack, select-all seating all five thousand rows, and the
  second click clearing them. The step taught a harness law the
  slice run caught: a select-all click can race the filter-clear
  debounce — the model was still the filtered ten when the head’s
  verb fired — so the leg waits for the model to be the full five
  thousand before it clicks. Validation-only by design: no product
  line moved, the Go suite untouched, the 23 packages unmoved, and
  the full battery green at 1419/1419.
- **Nothing lies about being a button** — the access rounds had grown
  the walk, the menus and the sort seats while four old surfaces still
  told two lies: focusables the Tab key could reach but no grammar
  owned, and button roles with no focus path at all. The row
  checkboxes were plain inputs — up to forty phantom Tab stops inside
  a role=listbox whose options must own no focusable descendants (Tab
  fell out of the listing body straight into checkbox #1); they now
  sit at tabindex -1, pointer-only by law, because the walk’s Space
  already owns keyboard selection and the head’s select-all stays the
  one checkbox in the Tab order. The count badges (⟲ / ⛔) claimed
  role=button with no focus and no keys — the verb already lives in
  the row context menu (Versions, Content Versions, Delete Marker),
  so the badges drop the role and stay what they work as: pointer
  shortcuts whose counts read as plain text under their tooltips. The
  gallery tiles were real buttons — natively tabbable, one stop per
  pooled tile in a face whose wrap owns the walk and names the seated
  tile through aria-activedescendant; the tiles now sit at -1 and
  answer the walk alone. And the modal/popout close × carried the
  same button role with no path — Escape owns the close verb
  everywhere, so the × hides from readers (aria-hidden) and stays a
  pointer glyph. The battery grows the row-tabs step with eight oks —
  the checkboxes out of the Tab order, no tabbable descendant in any
  rendered row (every focusable candidate pinned at tabIndex -1), the
  badges reading their counts as text with no role, the head
  select-all keeping its stop, Tab out of the listing body landing
  beyond the rows, the gallery tiles answering the walk with the wrap
  still the focus seat and activedescendant naming a tile, the modal
  × hidden with no role, and the walk leaving the settings keys null.
- **The headers answer the keyboard** — the last pointer-only verb in
  the listing was its oldest: sorting. The column headers sorted on
  click and nothing else — a bare div with an onclick, no role, no
  name, the sort state spoken nowhere — while the funnel beside it was
  already a button and the resize separator even owned a keyboard
  ladder. The sort verb now rides a real button in every header (the
  main and local listings through grid.js, the search results through
  srgrid.js — an S3-bound pane rides the local grid): a click still
  bubbles to the cell's own handler so the pointer grammar is
  unchanged, Enter and Space fire the same click natively, and the
  header row becomes a roving keyboard surface the way the menubar
  already is — ONE tab stop for the whole row (tabIndex 0 on the
  sorted column's seat, -1 on the rest, focus reseating wherever it
  lands), the arrows walking the seats with wraparound, Home and End
  holding the ends, every owned key stopped on the seat's own listener
  so the listing's document-level walk never moves under a seated
  header, and cycleSort capturing and re-focusing the seat across its
  own head rebuild (a keyboard sort no longer loses its seat to the
  re-render; a pointer sort never steals one). The state speaks: the
  seat's accessible name carries the live sort — "Name, sorted
  ascending, activate to reverse" — because the listing speaks a
  listbox grammar (role=option rows) where aria-sort has no home,
  while the search heads already claim columnheader and speak
  aria-sort too (none/ascending/descending, re-read at every build)
  beside the same name law. The seat paints with the inset accent
  outline the resize separators own, and under forced colors it joins
  the outline group. The battery grows the header-keys step with
  fourteen oks — the roving law (one tabbable seat), the sorted and
  unsorted names, ArrowRight walking and ArrowLeft wrapping and Home
  holding the ends, the listing walk unmoved throughout, Enter sorting
  the seated column with the seat kept across the rebuild, the
  descending press mirroring the ascending order exactly (pinned
  against the model, never hardcoded fixture names), Tab leaving the
  head straight for the listing (the one-stop economy — hidden funnels
  skip), the search popout speaking its columnheader states, Enter
  stepping the search sort down its cycle and the second press
  mirroring the first, and the default order restored — the search
  legs pinned as transition laws, not defaults, because the popout may
  carry a predecessor's sort. Gates: js-check OK, provenance 9/9 (the
  main.js watermark undisturbed through every node patch), the Go
  suite untouched by design — a frontend-only round, the 23 packages
  unmoved — and the full battery green at 1402/1402. Docs: the
  CHANGELOG carries the bullet above the text-size round's, and
  VERIFICATION the row (GUI-121).
- **The app answers text size** — the fourth leg of the access arc
  (keyboard, spoken, contrast, size): Ctrl+= zooms in, Ctrl+- out,
  Ctrl+0 resets, and Ctrl+wheel steps the same ladder — the pinch
  grammar trackpads speak. The mechanism is root-element zoom because
  it scales the layout viewport itself: every CSS pixel, scrollbar and
  scroll offset stays in one consistent space, so the pool
  virtualization, the column-drag handles and the gallery geometry
  keep their math (a standalone probe pinned the law before a line
  shipped: at 1.3 the viewport's client height reads 462 of 600, the
  row stride 37 of 29, and the bottom-of-scroll math still covers
  every row). The grammar answers from anywhere — inputs included, the
  branch sitting above the inInput return the way F1 does; floating
  windows never run the main chain, so their own listener reads the
  same table; steps are 10% per key and 5% per wheel tick, clamped
  0.7–1.6, the choice persists across boots (and floats to the
  popouts, which load the same page), and a coalesced readout toast
  speaks the landing percent instead of a burst of stale ones. The
  round healed a latent layout gap the zoom surfaced: the grids only
  re-rendered on scroll, so a window grown taller — or a zoom step
  that fits more rows — left the pool short at the bottom; the
  ResizeObserver now re-renders and watches the body's box (window
  height, zoom, pane split all land there). The F1 sheet grows the
  Ctrl+= / Ctrl+- / Ctrl+0 row, the guide's Keyboard & access grows
  its Text size entry, and usage.md's keyboard map and accessibility
  paragraph speak the grammar. The battery grows the text-zoom step
  with fifteen oks — the step ladder and store, the toast readout, the
  grammar answering from inside the filter input with focus kept, the
  ceiling clamp at 1.6, Ctrl+0 clearing the store, both wheel ticks,
  the pool covering the last row at 1.2, the keep-staged reload
  applying the saved level at the boot seam with the listing back, the
  ?popout=keys window answering through its own listener and
  resetting, and the step leaving zoom 1 with a clean store. Gates:
  js-check OK, provenance 9/9 (the main.js watermark undisturbed
  through every node patch), the Go suite untouched by design — a
  frontend-only round, the 23 packages unmoved — and the full battery
  green at 1388/1388. Docs: the CHANGELOG carries the bullet above the
  high-contrast round's, and VERIFICATION the row (GUI-120).
- **The app answers high contrast** — Windows High Contrast (forced
  colors) strips author backgrounds and box-shadows, and six rounds of
  keyboard grammar had left the app speaking ever more through paint:
  selection fills, walk-seat shades, focus paints, active-tab shadows.
  Under the mode every one of those states now speaks through the
  system palette instead: selected rows, tree nodes and tiles (the
  search results and pane rows ride the same .grid-row law) paint
  Highlight over HighlightText with the tile border matching, the
  keyboard walk seat paints a Highlight outline when it sits off the
  selection (the Ctrl+arrow moveSel branch), every menu seat — menubar
  titles and items, context menus, pickers, pane destinations, the
  settings column rows — paints an outline on :focus (any focus road;
  the mode forgives the pointer), disabled menu items dim to GrayText,
  and the active tab (the guide's seven sections, the admin panel)
  fills as a seat instead of the box-shadow underline the mode erases.
  The block sits last in app.css so it holds the final word at equal
  specificity, and the guide's Keyboard & access entry and usage.md's
  new accessibility paragraph say the claim out loud. The battery grows
  the forced-colors step with ten oks — the selected row painting
  Highlight and reading HighlightText (probes resolve the system colors
  in-page, so the pins hold whatever the emulated palette maps to), the
  walk seat's outline through Home then Ctrl+ArrowDown (the seat off
  the selection), the seated menu item's outline with the disabled item
  GrayText beside it (Escape first — Cut is disabled at an empty
  selection), the selected tile's fill and border, the selected tree
  node, the active guide tab filling Highlight, and the mode dropping
  with the theme paint returning to its exact pre-emulation value —
  and the round taught a harness law: under forced-colors emulation an
  entrance animation's first frame is also the style recalc that lands
  the forced palette, so a computed-style read racing the .14s entrance
  sees pre-adjustment values (opacity 0, transparent, black) — the leg
  waits on the popout's opacity settle instead of a blind sleep. Gates:
  js-check OK, provenance 9/9 (the main.js watermark undisturbed
  through every node patch), the Go suite untouched by design — a
  frontend-only round — and the full battery green at 1373/1373. Docs:
  the CHANGELOG carries the bullet above the help round's, and
  VERIFICATION the row (GUI-119).
- **The in-app help catches up with the keyboard grammar** — the F1
  sheet and the user guide had fallen six rounds behind the keyboard
  surfaces the app grew. The sheet now carries the rows the faces own:
  PgUp/PgDn paging the listing, the Menu key (Shift+F10) opening the
  seated row's or tile's verb menu, the tree's arrow walk, and the
  menubar's Tab-arrows-Enter grammar. The guide grows its seventh
  section — Keyboard & access — speaking the whole law in prose: every
  surface walks and answers typing (rows, tiles and the sidebar tree
  alike), the menus and the menubar are keyboard surfaces with Escape
  closing back to where you were, screen readers hear the seat through
  aria-activedescendant (treeitems carrying level and expansion, menus
  speaking their roles and checked states), and the OS "reduce motion"
  setting is honored — transitions settle instantly and nothing that
  carries meaning depends on animation. The Browsing section's sidebar
  tree entry grows its keyboard sentence — the arrows walk, Right
  expands (an unloaded folder loads), Left folds or climbs, Enter
  navigates, Space toggles, typing jumps — so the pointer is no longer
  the only documented road into the tree. Gates: js-check OK,
  provenance 9/9 (the main.js watermark undisturbed through every
  node patch), the Go suite untouched by design — a frontend-only
  round — and the full battery green at 1363/1363 (the help-guide
  step's two exact-six pins grown to seven with the new section's
  content ok; the keysheet step's grammar-rows ok). Docs: VERIFICATION
  the row (GUI-118).
- **The menubar answers the keyboard** — the bar becomes a keyboard
  surface the way desktop menubars are: the titles are a roving tab
  stop (one seat in the Tab order, the arrows striding the closed bar
  between File and Help), Enter, Space or ArrowDown opens a menu and
  the freshly opened dropdown seats focus on its first enabled item —
  the APG law the context menus already own — with ArrowDown and
  ArrowUp walking the items (the disabled skipped, wraparound, Home
  and End holding the ends), ArrowRight opening a cascade or, from a
  plain leaf, striding the bar onto the NEXT menu (the road every
  desktop menubar owns), ArrowLeft folding an open cascade back onto
  its parent or walking onto the previous menu, Enter running the
  seated verb with the seat restored to the title BEFORE the action
  runs (a modal the verb opens captures the bar as its previous
  focus), Escape closing back onto the title, and Tab closing
  silently because focus left on purpose. The menus speak their
  semantics for the first time: the bar a `role="menubar"`, each
  title a `menuitem` with `aria-haspopup` and `aria-expanded`, the
  dropdowns and cascades `menu`s, every leaf a `menuitem` or — where
  it carries a checked state — a `menuitemcheckbox` speaking
  `aria-checked` re-read at every open (the dropdown re-renders the
  way it always did), the disabled marked `aria-disabled`, and the
  focus seat painted with the `:focus-visible` grammar every
  surface owns. Every key the bar handles is stopped on its own
  listeners — the listing walk on the document chain never moves
  under the menubar, and a stray letter typed at a seated title or
  item never falls through to the listing's type-ahead. The battery
  grows the menubar-keys step with thirteen oks — the roles and the
  roving tab stop, Enter opening and seating, the arrows walking,
  End and Home, Escape back onto the title, the closed-bar stride,
  the walk skipping the disabled (Cut at an empty selection), the
  cascade opening and folding (View's Auto-refresh options), the
  plain-leaf stride onto the next menu, the checked leaf running its
  verb (Panes F9) with the state re-read on reopen, and Tab closing
  the open menu. Gates: js-check OK, provenance 9/9 (the main.js
  watermark undisturbed through every node patch), the Go suite
  untouched by design — a frontend-only round — and the full battery
  green at 1361/1361. Docs: the usage guide grows the keyboard-map
  menubar row and a menubar-keyboard paragraph beside the tree's,
  and VERIFICATION the row (GUI-117).
- **The tree answers the keyboard** — the sidebar tree becomes a
  focus surface of its own (the container carries the seat,
  `tabindex` beside its `role="tree"`) and the walk grammar is the
  tree’s own hierarchy: ArrowDown and ArrowUp stride the flat
  visible order, ArrowRight expands a collapsed folder — an
  unloaded one too, the same road a double-click and a click’s
  reveal own — and walks into an open one’s first child,
  ArrowLeft folds an open folder and climbs from a collapsed one
  onto its parent, Home and End hold the ends, Enter navigates the
  walked row exactly the way a click does, Space toggles expansion,
  and the Menu key (Shift+F10) opens the row’s verb menu with the
  row itself as the anchor (openMenu seats an element by its rect,
  so the menu lands beside the row with no pointer there). The walk
  paints its seat with the focused-row grammar the grid rows own
  and names it through the container’s `aria-activedescendant`
  (the rows already spoke treeitem, level and expansion; now the
  tree says which one is seated), and navigation itself is a focus
  move: `markCurrent` seats the walk on the current row through
  every road that lands one (a click, Enter, a reveal from the
  grid) — one choke point, the naming law the grids own.
  Type-ahead joins the tree as the last face: the same 600ms
  case-folded prefix buffer jumps the walk by label with the chip
  pinned to the tree’s corner, and a refined miss holds the seat
  with the keys kept for the next letter. Every key the walk
  handles is stopped on the tree’s own listener — the listing
  walk on the document chain never moves under a tree seat — and
  a click on a tree row leaves focus on the tree, so a focused
  tree owns its typing the way every focused surface does.

### Fixed

- **A sourceless app could stick on a loading skeleton** — with no
  data source seated, a reload (F5, or the removal of the last
  source) took loadView’s generic path: the skeleton and spinner
  came up and waited on a listing no leg would ever request, so the
  pane sat on “Loading” forever. The onboarding branch now renders
  the welcome panel directly — nothing is in flight, and the welcome
  panel IS the view.
- **The transfers and tasks windows rebuilt every row on every
  tick** — the
  full redraw made the info button’s hover background flash on each
  progress update, and a click racing the redraw could land on a
  handler about to be discarded — the verb that looked stuck was a
  dead promise on a replaced row. Rows are now patched in place: a
  structural signature (status, phase, error shape, expanded state,
  the route, the destination arm — every field that adds, removes or
  re-seats a control) decides rebuild, and everything else (bytes,
  percent, chips, counts, current file) patches the same DOM node.
  The verb buttons read the job’s live state at click time and
  refuse aloud when the job settled underneath them (a toast, not a
  swallowed promise), and the Show destination arm appears the
  moment a settled job grows a destination — the signature carries
  it, so a dest-bearing tick rebuilds the row it belongs on.
- **Root-level renames landed at “./name”** — path.Dir’s root
  artifact rode into the destination key: renaming a file at a
  bucket’s root computed “./new.txt”, a phantom “./” segment
  that stores keep as a literal prefix — an object no clean
  listing of the root shows and no path bar addresses. The
  parent now normalizes to empty at the root: the destination
  is the clean key, and the clobber guard probes exactly the
  key the copy writes.
- **WebDAV: the same verdict, closed the HTTP way** — the engine
  threads the caller's context into every request (net/http honors
  it end to end), so cancel always had teeth there, but no command
  carried a budget: a CLI path rides a context with no deadline at
  all (a silently wedged peer hung it for the life of the process),
  and a GUI listing paid the caller's whole patience budget per op,
  failed with a deadline the healing contract deliberately ignores,
  and repeated that forever — bounded per call, but never healing
  and never ending. Every metadata round-trip now rides a 15-second
  cap (PROPFIND, MKCOL per segment, MOVE, DELETE — request plus
  body drain; the HTTP engine carries the context natively, so no
  force-close is needed the way the FTP socket and the SSH
  transport needed theirs), and when the cap fires while the
  caller's own patience is unspent the verdict is the shared
  deadline sentinel, classified like a reset so the engine cache's
  redial answers with a fresh connection. Streams stay on the
  caller's context alone — an upload's or download's duration is
  not the engine's to guess, and cancel already has full teeth
  over HTTP. Covered by a server that answers the dial probe and
  goes silent on every request after it (List must return the
  sentinel inside one span, the follow-up repeats fast), plus the
  streams leg proving a body that outlives the cap still completes
  whole.

- **Editor pushes carry their own deadline — the S3 wire’s silent-death
  hole closed** — the auto-save push behind Edit (and “Stop & upload”)
  rode the app-lifetime context into a client with no transport budget
  (Timeout: -1 is deliberate — a slow link must not kill a transfer), and
  its caller is the watcher goroutine, whose only cancellation is app
  shutdown: against a peer that took the PUT body and went silent (no
  FIN, no RST — the wedge class the FTP, SFTP and WebDAV engines were
  hardened against), the response wait parked for the life of the process
  — the watcher died inside one push, the failure never reached the
  push-failed voice, and the session just sat dirty with no error
  anywhere. Every push now rides a 15-minute budget of its own
  (editPushTimeout): a wedged peer turns into an ordinary failed push —
  voiced, backed off, retried — the contract everything downstream
  already knows, and shutdown still stops an in-flight push through the
  app context. Covered by a server that takes the PUT and never answers:

- **Local filesystem: the fifth wire gets the same verdict** — a
  local syscall has neither a socket to tear nor a context the kernel
  honors, so the wedge class the four network engines were cured of
  survived here untouched: a root whose backing vanished (a dead UNC
  path, a disconnected mapped drive — no FIN, no RST, the same shape
  on a different wire) parks os.ReadDir and os.Stat inside the kernel
  until the redirector gives up, which can be effectively never, and
  no caller context can reach inside a syscall to stop it — the GUI’s
  dual-pane navigation promised and never settled, and the delete
  preview and compare walk parked with it; worst was the dial, which
  runs under the engine-cache lock and froze every source’s engine
  resolution along with its own wedged root. The cure cannot be
  interruption, so it is abandonment: every point operation now runs
  as a bounded step (LocalStep, 15 seconds) on its own goroutine while
  the caller waits only as long as the budget or its own patience
  allows — the caller’s context is honored for the first time on this
  wire — and the abandoned step finishes alone with whatever it
  produced released rather than leaked (an abandoned Open’s file
  handle closes; ownership decided by one compare-and-swap so neither
  side can touch a verdict the other owns). The verdict is its own
  sentinel, deliberately not the engines’ redial-me deadline: local
  has no connection to tear and nothing to redial, so it surfaces as
  an ordinary failure to voice, never as a death to heal. Streams keep
  their own duration (a transfer’s reads and writes, a recursive
  delete’s real work — its gate, the preview, is bounded instead),
  and the sweep covers the engine ops (list, stat, open, mkdir,
  rename, remove), the dial, the pane bindings (navigation, roots,
  open-with, terminal, delete preview and act), the bounded walkers
  behind preview, compare, usage, and upload and transfer expansion
  (the strict one aborts on an unreadable directory — a silently
  skipped file would be a silently missing upload), the conflict
  dialog’s destination probes, the download job’s skip/rename probes
  (a wedged destination volume fails the item honestly instead of
  parking the job in running forever), the transfer planner’s local
  legs (destination resolution, the cycle guard, item expansion,
  the local-source open mid-job), and the container/credential file
  reads. Covered by rigs that park the read and stat seams — the
  pane’s listing, the engine’s listing, the dial, the drop expansion
  — each returning the verdict in bounded time and working again the
  moment the wire is back.

- **Transfer integrity: a transfer lands whole or fails loud** —
  io.Copy returns nil when a stream ends in a clean EOF short of the
  size its wire promised, so a source that truncated mid-transfer
  committed silently — a file rewritten shorter while the upload
  streamed it, an FTP data connection that delivered fewer bytes
  than SIZE announced, a WebDAV body short of its Content-Length —
  and a short object or file landed as though it were whole,
  byte-level corruption with no error anywhere. Every streaming
  convergence now wraps its source in a length-verifying reader
  (VerifiedStream, with the ErrShortStream sentinel naming both
  counts — "got 40 of 100 bytes"): uploads read the file against
  its open-time stat (a file that grew past the stat is capped at
  the size the transfer planned), cross-transfers verify against
  the WIRE's own size on every leg (S3's Content-Length and the
  local handle's stat are authoritative; FTP and WebDAV report 0
  for a size they could not learn, and an unknown size streams
  unverified exactly as before), and drag-out, the version-diff
  fetch, versioned cross-client copies and the CLI's cp on every
  leg — local, S3 and remote destinations alike — ride the same wrap; S3 reads ask for checksum mode
  (ChecksumMode on every direct GetObject — the SDK validates the
  payload whenever the server sends one), and the download path —
  where the manager sizes a download from what the wire serves,
  not what the planning HEAD promised — gains a written-count
  guard against the planned total. A truncated source fails the
  file with nothing committed: no partial at the final name, no
  staging leftovers. Covered by the wrapper's contract tests
  (both directions, zero, unknown, hard-error passthrough), a
  download rig whose HEAD and GET disagree, and the field shape
  end to end — an engine whose Open lies exactly the way a
  truncated FTP stream does fails the cross-transfer with the
  verdict while the destination stays empty.
## [1.2.0-beta.3] — 2026-10-07

### Added

- **A launch that cannot work says so** — on Windows Server (and
  stripped-down Windows installs) the app used to die before any window
  existed: the Edge WebView2 Evergreen runtime those machines lack is
  what the window is made of, and its absence failed fast inside webview
  creation, where the only witness was a stderr no GUI build has — to
  the user, nothing happened at all. The app now asks the loader’s own
  registry question (the EdgeUpdate Clients registration, all three
  locations: machine-wide 32-bit and 64-bit views plus the per-user
  install) before anything window-shaped starts, and refuses loudly
  instead — a message box carrying the fix (winget install
  Microsoft.EdgeWebView2Runtime, or Microsoft’s standalone download)
  plus an event-log row; an unreadable registry fails open, because a
  broken probe may never block a launch. Every fast GUI failure gets the
  same voice through guihealth.Announce — a Run error is announced,
  not just printed to the invisible stream, and a second launch whose
  stuck predecessor cannot be raised says so too instead of exiting into
  the same silence. Alongside it, the self-signed certificate guidance
  is fixed where it was wrong: chain trust lives only in Trusted Root
  Certification Authorities (the store the “this root certificate is
  not trusted” warning actually reads), Trusted Publisher suppresses
  the publisher prompt only on top of it, and Trusted People never built
  the chain at all — scripts/trust-cert.ps1 imports both stores
  idempotently (machine-wide elevated, -CurrentUser, -Remove to undo),
  and the signing guide, security notes and usage troubleshooting now
  teach the split instead of the wrong store.

- **The app remembers its windows** — the main window opened at a
  fixed 1280×800 every launch, and every popout forgot its
  placement when the app closed: the user resized, arranged on a
  second monitor, maximized — and the next boot threw it all away.
  The geometry (the main window’s rect and maximized state, each
  popout’s rect) now persists to windows.json in the config dir
  (written atomically, like every piece of state) and is re-applied
  at launch under sanity rules: dimensions clamp to the minimums,
  and a rect is honored only when it overlaps a screen that still
  exists by enough to grab the title bar — a window remembered on a
  monitor that is no longer attached falls back to default
  placement instead of reopening stranded off-screen. A maximized
  quit restores maximized, and the remembered rect is the window’s
  NORMAL one — tracked live while it runs, since a maximized window
  cannot report its pre-maximize frame on the way out — so
  un-maximizing after a restart lands where the user left it.
  Popouts reopen where they stood, still-open ones included
  (snapshotted at quit, not only on their own close). A missing or
  corrupt memory is simply defaults: placement is cosmetic and must
  never block a boot.
- **Signing guide (`docs/signing.md`)** — the one page answering "what
  do I need to sign every build": the Windows identity decision tree
  (self-signed fleet cert today; CA OV/EV under the post-2023
  hardware-key rule, or Azure Trusted Signing) with the exact secrets
  and workflow steps each provider plugs into, the macOS Developer ID +
  notarization stack for manual distribution, Linux checksums with the
  optional GPG layer, and rotation + verification recipes for
  maintainers and end users; README's documentation index, build.md,
  security.md and CONTRIBUTING.md gain the pointer.

- **The last-resort error net** — an async failure nobody caught (a
  bare API call, a timer callback, a render pass) used to die
  silently inside the webview: production users saw nothing at all.
  Unhandled rejections and uncaught errors now surface as one error
  toast per distinct message — a retry loop cannot flood the corner
  — alongside the console record devtools expects, in the main
  window and popouts alike; the toast helper itself no longer
  crashes on a view that replaced the toast corner, and the
  visual battery pins the net end to end (a deliberately fired
  rejection and sync throw each land as exactly one toast, repeats
  deduped).

- **Live transfer progress in the CLI** — the transfer engine has
  always reported bytes as they move; the CLI face never listened,
  so a multi-gigabyte cp printed one announce line and then silence.
  cp/mv/sync uploads and downloads (plain s3:// and NAME:// source
  URIs alike) now render one rewriting line on stderr while a file
  streams — "name  1.2 GB / 4.7 GB (26%)  9.8 MB/s" — under three
  politeness gates: stderr must be a terminal (piped output stays
  byte-clean for scripts and redirections), --json must be off, and
  nothing renders until a transfer has moved 1 MB or run a second,
  so a folder of small files never flickers. The line redraws at
  10 Hz, smooths its speed readout over the window, pads over its
  own residue when the readout shrinks, cuts long names on rune
  boundaries (the file name survives the ellipsis), and erases
  itself when the transfer settles so history keeps only the
  command's own record. Seven unit tests pin the gates, the throttle,
  the format and the erase.

- **The worker panic net** — the frontend got its last-resort net
  for async failures; the backend never had one for its own goroutines.
  Wails already recovers panics in bound methods, but every worker the
  app spawns — listing streams, deep searches, the transfer-job
  runners, the drag-out stage threads, the editor watcher, the task
  and job heartbeats, the presign scrub — ran bare: one panic took
  the whole desktop process and every in-flight transfer with it, and
  whatever the goroutine was driving froze at “running” forever.
  Each worker now defers a guard that recovers the panic and converts
  it into the same shape every other failure already rides: one
  event-log line with the message and the capped trace (log drawer,
  `s3b log`, the opt-in file log), and the task or job settled as
  failed — the view answers, the spinner ends. The webview shell
  installs Wails’ PanicHandler so binding-layer and framework
  panics land in the same log instead of vanishing into the discarded
  default logger; finishJob becomes idempotent so a panic in a
  deferred cleanup cannot rewrite a row’s real outcome; and the
  CLI grows the same contract — a panic prints one
  “unexpected: internal error:” line with the trace and exits 3
  (the unexpected-error code) instead of the runtime’s raw dump and
  exit 2, so scripts branching on the exit code see the app’s own
  vocabulary. Eight unit tests pin the settle, the normalization, the
  idempotence, the trace cap and both heartbeat fallbacks.

### Changed

- **The last grid mirror folds** — the search pane's column
  machinery (the persistence loader, the Type-column text, the
  sizing floors) now imports the browser pane's exported
  implementation instead of carrying a near-copy: one loader takes
  the caller's column catalog so a persisted Source width survives,
  one type formatter answers both grids, and the resize hit-slop
  and minimum width live in one place. Seven single-module exports
  across four files went back to module-private (sameLoc,
  isAncestorLoc, srcIcon, markerDialog, typedConfirm,
  composeFileName, browseDirDialog) — the public surface is now
  exactly what other modules import.

### Fixed

- **One speed-limit shelf everywhere** — the transfer dialog's limit
  picker and Settings → Transfers edit the same remembered value, but
  offered two different option lists: a limit picked on one surface
  (256 KB/s, 512 kB/s) rendered as no-limit on the other. Both now
  share one canonical list — same steps, same localized "None" — from
  a single definition, so what one surface sets the other always
  shows.

- **Per-item detail in File transfers** — a multi-item job’s "+N
  more" disclosure now lists every top-level item with its live
  state (waiting → transferring → done / failed / skipped), a
  per-item counter — the size for single files, the file count for
  folders, with failed/skipped counts spelled out when a folder
  mixes outcomes — riding the existing transfer updates instead of
  a one-shot name fetch, so the detail survives to completion
  instead of vanishing with the in-flight line. Jobs over fifty
  items fall back to the plain name list, with the job's own
  failed/skipped totals riding the panel header so a big sweep
  still answers "did anything fail" at a glance. Same-source S3
  (server-side) copies no longer read "0 B (0%)": the row says
  *Server-side copy* while it runs. A finished job reads 100% even
  when skips settled no bytes, and done/failed/skipped file
  counters are disjoint now — a skipped file no longer
  double-counts as done. The Running tasks window’s merged
  transfer rows carry the same live per-item disclosure.

- **Drag-out staging retires after the gesture** — the files a
  native drag-out stages for the drop target (a fresh temp dir per
  gesture, downloaded through the transfer engine) were never removed
  when the gesture ended: they sat in the temp workspace until the
  NEXT launch’s wipe, so a long session of drag-outs piled up one
  full copy of everything ever dragged. Each gesture now schedules
  the staged dir’s removal ten minutes after the drag ends —
  generous enough for a slow target still importing the files,
  best-effort (files held open without FILE_SHARE_DELETE survive,
  and the launch wipe remains the backstop), running under the worker
  panic net. Cancels and self-drops are covered too: the stage knows
  its dir from the moment it is created, so an abandoned drag’s
  partial download retires on the same schedule.

- **Quitting with unuploaded edits now asks** — the editor’s
  keep-remote-up-to-date watcher pushes a save only after it stays
  stable for two polls (~2.4s) plus the upload itself, and the
  launch-time workspace wipe discards whatever was never pushed: quit
  inside that window and the last edit vanished silently. A dirty
  edit session now refuses the exit with the same confirmation every
  other unsaved change gets — “N edited file(s) not yet
  uploaded (e.g. notes.md)” — on both the window’s close
  button and the menu’s Exit; exiting anyway is then an explicit
  discard, exactly like Stop Edit without upload.

- **The editor's pull joins the task registry** — opening a file
  for editing downloaded it through the transfer engine on an
  invisible, uncancellable bound call: a multi-gigabyte object just
  spun with no row anywhere, and the only way out was waiting. The
  pull now rides the Running tasks window as a transient “edit”
  row — visible while it runs with its Cancel, gone when it lands
  (the status-bar editor indicator is the session's lasting
  surface), engine tuning applied as before, and an idempotent
  defer settles the row even if a panic unwinds through the pull;
  the exit gate's running-task check covers a quit mid-pull
  automatically. The push side honors shutdown through the app
  context — an in-flight upload is stopped by a quit the
  dirty-session gate has already confirmed. The tasks window names
  the verb properly (“Opening”, pencil glyph) in all 15 languages
  instead of the raw-kind fallback.

- **A failing editor push gets a voice** — the keep-remote-up-to-date
  watcher retried a failed upload silently every poll (1.2s), so
  the user kept editing a file whose changes had stopped reaching
  the bucket: nothing toasted, the indicator said nothing, and the
  first hint of trouble was the exit gate refusing a quit. A
  failing push now announces itself exactly once per streak (an
  error toast naming the file and the cause), every attempt writes
  an error line to the log drawer, the status-bar editor pill
  escalates — ⚠ glyph, danger color — while any session is failing,
  and the Files-open-in-editor dialog flags the session on its row;
  retries back off exponentially (2.4s doubling to a ~19s cap)
  instead of hammering a dead endpoint, and a landing upload
  clears every mark (the existing “Uploaded” toast already voices
  the recovery). The session stays dirty the whole time, so the
  exit gate's confirmation keeps guarding the un-pushed edits.

- **“Stop & upload” stops destroying the session when the upload
  fails** — the manager’s explicit save deleted the edit session
  before it pushed, so a network blip at the moment of stopping
  removed the row and the status-bar indicator, silenced the
  watcher, and dropped the exit gate’s guard while the edits had
  reached nowhere — stranded on a staged file the next boot’s
  workspace wipe deletes. “Stop & upload” is a save request, not a
  discard: the session is only ended when the push lands, and a
  failing one keeps it alive — still dirty, flagged with the
  failure mark, retried by the watcher with backoff (the manager
  row and the pill say so, the toast carries the cause, and the
  exit gate keeps guarding) — so a clean second Stop settles it.
  The dialog refreshes on failure too (the row regains its flag
  and the pill escalates), and a settled session now retires its
  staged file after the same ten-minute grace a drag-out stage
  gets (the editor process may still hold it open; the launch
  wipe backstops) instead of leaving the downloaded bytes in the
  edit workspace until the next boot.

- **A second Edit re-focuses the live session instead of re-downloading
  it** — EditObject never looked at the session registry before pulling,
  so editing an object that was already being edited re-downloaded the
  REMOTE bytes over the staged file — destroying every edit saved but
  not yet uploaded — and replaced the registry entry while the old
  watcher kept polling the same path: a leaked goroutine racing a
  second one to upload. A second Edit now re-focuses the existing
  session (the editor opens on the same staged file, pending edits and
  all; no re-download, no second watcher), a concurrent double-Edit
  whose pulls were both in flight keeps whichever session registered
  first instead of stacking two watchers on one file, and a watcher
  whose staged file is deleted ends the session in the registry too
  (a zombie entry used to pin the indicator — and, when dirty, the
  exit gate — on a file that no longer exists). A transient stat
  failure — an editor’s atomic save (write temp, rename over) can
  briefly hide the file — no longer kills the watcher: auto-upload
  silently stopping forever was the cost; the next poll retries.

- **Server-supplied paths can no longer write outside their
  destination** — every join of a listing-derived name onto a local
  directory (the GUI’s downloads, cross-engine transfers with a local
  side, the editor’s staged pull and its per-bucket workspace, the
  CLI’s NAME:// and recursive copies) used raw filepath.Join, which
  resolves “..” lexically: a hostile endpoint — or a merely exotic
  object key, S3 legally allows “..”, backslashes and names Windows
  reserves — turned a download into an arbitrary-path write, and a
  key literally named NUL silently discarded its bytes. All of those
  joins now ride SafeLocalJoin (pkg/core/transfer, one policy shared
  by both faces): traversal is neutralized in place — the object
  still downloads, under a contained name, instead of being refused
  — drive and UNC shapes and Windows-invalid characters become
  literal contained segments, reserved device names are pushed past
  their device meaning, and both slash kinds count as separators.
  Ordinary names map to themselves byte for byte (the identity every
  existing transfer rides on), pinned by a table test that also
  walks a hostile corpus, and an editor integration test downloads a
  traversal-shaped key strictly inside the workspace.

- **Local state can no longer be destroyed by an interrupted
  write** — every piece of persisted state (the profile/source store
  itself, the encrypted Profile file and its Save As, the engine
  tuning, the license record, the log settings, the seen-sources
  list, the CLI’s `source export`) was written with os.WriteFile,
  which opens the target with O_TRUNC: a full disk, a quota hit, or
  a crash mid-write destroyed a previously good file — and for the
  store that is every source and profile at once (plaintext secrets
  included on keyring-less hosts), leaving “invalid profile store”
  and no second copy. All of those writes now ride
  pkg/core/atomicfile: bytes land in a sibling temp, are flushed to
  disk, and replace the target by rename — the same critical-data
  contract the transfer engine’s staging established for downloads
  (same-volume atomic replace; a failure at any point leaves the
  previous bytes untouched; a hard kill at worst strands one temp,
  never a partial at the final name). The two streaming write paths
  that truncated just the same — the cross-engine local Create and
  the CLI’s NAME:// downloads — now stage and commit too, so a
  transfer that fails mid-stream leaves the previous local file
  byte-for-byte intact instead of trading it for a truncated copy,
  and the log’s rotation rewrites atomically instead of cutting the
  file down the middle on a crash.


- **FTP: a silently dead control channel can no longer hang the engine
  forever** — jlaffaye/ftp's commands take no context, so a peer that
  vanished without a FIN or an RST (an expired NAT mapping, a container
  that disappeared mid-session) left the reply read blocked for the
  life of the process: no error, no retry, and in the GUI a navigation
  that had already painted its breadcrumb but never settled its
  listing. Every FTP command round-trip now rides a 15-second
  force-break (the same budget the dial options and the WebDAV engine
  use): at the deadline the engine closes the socket — the blocked
  read unblocks immediately — classifies the break exactly like a 421
  or an EOF, and the existing one-redial policy answers on a fresh
  connection; a canceled transfer context now tears down an in-flight
  upload the same way. Recursive wipes and uploads keep a
  caller-scoped budget (a tree's size is not the engine's to guess)
  while every individual command inside them stays bounded. Covered
  by an in-process FTP server that wedges its first listing and must
  recover inside one deadline span, plus the force-breaker's own
  pass-through/deadline/cancel contract.

- **SFTP: the same silent-death class, closed with the same tool** —
  pkg/sftp's requests carry no per-command deadline either, so a
  peer that vanished mid-session left the reply wait blocked
  forever; the engine now rides the same force-breaker the FTP
  engine built (one deadline machinery, shared): every request
  round-trip gets the 15-second budget, the subsystem handshake and
  the home-directory query at dial get it too (both run under the
  engine-cache lock, where a wedged dial would freeze every
  source's resolution), and at the deadline the transport is closed
  — the blocked request unblocks immediately — with the verdict
  classified exactly like a reset, so the engine cache's healing
  redial answers on a fresh connection. Recursive walks and uploads
  keep a caller-scoped budget while the caller's context finally
  has teeth there too: a canceled transfer tears the transport down
  mid-copy. Covered by an in-process SSH server that wedges right
  after the version exchange and must return the deadline verdict
  inside one span, plus the classifier case that turns that verdict
  into a redial.

## [1.2.0-beta.2] — 2026-10-06

### Changed

- **Every data source is exactly one root — no account-level
  sources.** An S3 source is one bucket, a remote source one host
  root, a local source one folder, on every face. Legacy account-wide
  S3 sources split automatically the first time the account can be
  listed — one bucket-scoped source per visible bucket, named after
  the bucket — while an unreachable account keeps its legacy source
  and is retried on every sources refresh and reconnect; the source
  editor gains *Split into one source per bucket…* for the manual
  path. Cross-bucket drops and pastes are cross-source transfers now
  (same-client copies still ride server-side). **New bucket…** on the
  sidebar or an S3 source's context menu creates the bucket and its
  data source in one step, Delete bucket removes the source with it,
  and the CLI requires a bucket on `source add` and gains `s3b
  source split` (with `--dry-run` and `--json`). The single-root
  model is pinned for every source type in the visual battery (S3
  bucket roots, remote host roots, local folders).

### Fixed

- **The local-address ladder means the same thing on every
  platform.** Three contracts only another operating system could
  see: a Windows-shaped directory pasted into a mac or linux pane
  came back rewritten (local:// folded every backslash to a forward
  slash before FromSlash, and FromSlash is the identity on unix),
  the ~ expansion joined home and tail with no separator of its own
  (the tail's grammar backslash survived as a literal on unix —
  ~\Documents became /Users/you/\Documents), and a pasted C:\
  escaped the delete gate everywhere but Windows, because isFsRoot
  leaned on filepath.VolumeName, which knows volumes only on
  Windows — so removing a drive root fell through to the OS error
  instead of the fast per-item refusal. The local surface's
  canonical form keeps backslash forms native on every platform and
  folds only the slash forms, the tail after ~ rejoins the home
  dir through filepath.Join, and drive roots are recognized by
  shape (bare drive, drive plus one separator, nothing more) so
  the refusal fires before the OS is ever asked, wherever the path
  was pasted. The root-refusal test drives C:\ and / through both
  rungs on every platform.

- **The Data sources tree parks every indicator flush — no empty
  space anywhere.** The type slot was a shared column seated on the
  widest chip the list carries, so a mixed list parked a WebDAV-wide
  stretch of empty space to the left of every S3/SFTP badge, and
  bucket and folder glyphs centered in that same measured column,
  floating mid-column with a gap to their names that grew with the
  longest type chip. The shared column is gone: the slot is
  content-sized now — every chip and glyph keeps exactly the width
  its own text needs, parks flush after the expander, and the name
  follows on the row's own 4px gap, so no row carries empty space
  and each name starts right after its own indicator. Pinned by two
  reworked visual battery legs.

- **Removing the data source behind the main view re-homes the view.**
  The tree's Remove source confirmed and refreshed but left the view
  pointing at the source it had just deleted — the next operation
  that resolves the view source (a storage-class convert, an admin
  dialog…) died with `profile not found`. Delete bucket and the
  legacy split already re-homed a vanished source's view; the Remove
  source menu items (legacy account rows and bucket-scoped rows
  alike) now do the same — the view lands on the store's first
  surviving S3 source, because a remote heir would seat the view
  while the engine's view-source pointer stayed on the ghost
  (remote views never re-point it); with no S3 source left, any
  survivor hosts the view and the pointer is cleared. Delete
  bucket's re-home rides the same heir rule. Caught live by the
  re-pointed verification gate: a
  teardown removed the source whose admin panel the row had just
  walked, and the next row's first binding crashed the battery on
  the dead view.

## [1.2.0-beta.1] — 2026-10-05

### Added

- **Folder sizes fill in — every view, every source.** A listing
  cannot know a folder's content size without walking it, so folder
  rows shipped a blank Size cell and kept it (Properties was the only
  walk). The cells now fill in lazily: a moment after a listing
  settles (and again after every page of a streaming one), folders
  with an unknown size ride the same usage walks the content bar
  uses — cache-first, twenty rows at a time — and the cells paint in
  place: no re-sort, no lost selection, the order under the cursor
  never jumps. It holds on every surface — the bucket list (a bucket
  is a folder row too), S3 object views, every remote engine, the
  workstation, the secondary pane (its own bounded cache, dropped the
  moment its listing is replaced) and the Search window, where folder
  hits fill from one grouped walk per origin while the run streams
  and once more at completion. Freshness follows each engine's
  truth: S3 sizes drop on the change event, a replaced pane listing
  drops its walks, and local and remote re-walk per visit (no change
  events to trust); a folder the engine cannot answer — a partial or
  errored walk — stays an honest blank, cached so it is not hammered
  and retried on the next settle. The local content bar joins the
  real walks too (the drive-roots view keeps the floor), and the
  walk engines gain the source-scoped twins the pane and Search
  need (SourceS3Usage, SourceBucketUsage), pinned in
  pkg/api/usage_test.go.
- **Engine guards: no transfer may feed itself, and bucket removal
  answers a dry run.** A full sweep of every action that can affect
  data — deletes, edits, task cancellation, migration, Dual-pane
  transfers, settings honors, security — across every source type and
  both faces found one structural gap and closed it at the engine
  level: a copy or move whose destination sits inside its own source
  subtree (a folder pasted into itself, a bucket copied into its own
  prefix) is refused with a cycle error before a byte moves, on every
  face and every family — the cross-source transfer engine (S3,
  remote and local legs), the GUI selection paste, versioned copies,
  and the CLI's s3-to-s3, remote and --versions copy paths. The
  destructive rb gains --dry-run: the removal preview mirrors the
  removal path itself — live objects and bytes on a plain bucket, the
  whole history (versions, delete markers, their bytes) once versioning
  is configured — plus the gate line that says removal needs --force,
  and nothing is deleted. Everything else
  the sweep walked was verified already-gated (the rm force ladder,
  the Delete Window family, cancellations, the migration paths) and
  is recorded in VERIFICATION.md; the new guards are pinned by Go
  tests against the real engines — the GUI battery's backend is a JS
  mock, so a shim-side refusal would test the shim, not the engine.
- **Home buttons on both pane toolbars.** Each content toolbar now
  carries a house-glyph Home beside its navigation buttons. On the
  main side it jumps to the open data source's start view — a
  bucket-scoped source's contents root, an account-wide source's
  bucket list, a remote's root — and to the workstation home folder
  when a local folder owns the main view. On the secondary pane it
  performs the Dual-pane picker's Home view action: a fresh start on
  the workstation home folder, from any binding.
- **Local addresses open the primary pane: the workstation is a
  first-class main view.** Pasting a bare path (`C:\Projects`,
  `\servershare`, `~`) or a `file:///` URL into the main path bar
  no longer bounces the address to the secondary pane — the main
  view lists the folder itself: rows with sizes, modified and
  created times, a breadcrumb walking the native path (a `C:` root
  crumb or a `\servershare` UNC root), folder activation, the
  parent-row climb, folder and selection properties, a Find scoped
  to the open folder, and the operations the folder honestly
  supports — Copy / Cut land real files on the OS clipboard, Paste
  and OS drops transfer in from any remote or S3 view, Delete walks
  the unified confirm, upload copies files and folders in, and the
  size bar walks the folder's real tree (the drive-roots view keeps
  the listing-level floor). Rename, New folder and New file rest
  where no API exists
  (their toasts point at Explorer), drags between two local spots
  stay Explorer's job while remote-origin drags drop into the
  folder and its rows, and the menubar's Copy URL admits the local
  kind (`file:///` addresses). The secondary pane is untouched — its
  own path bar still rebinds it to the workstation exactly as
  before.

- **Column parity pinned: the one global column catalog seats
  every view end to end.** The main view, the secondary pane and
  the Search window draw the same eight columns through their
  headers, their header pickers and the Settings partition — one
  catalog, three views — and the battery now pins it at runtime:
  the search window's picker and the main view's own picker list
  the identical labels, and the Settings manager seats the same
  eight rows. A column whose data an engine doesn't carry
  (Storage class outside S3, Mode outside local and SFTP, Date
  created where no birth time exists) renders empty cells — the
  column never hides itself; the Search window's only extra is
  the automatic Source column on multi-origin runs, which never
  lists. No new behavior — the parity the settings-columns work
  delivered is now asserted so drift is caught.
- **Normalized paths and a universal paste: every data source's
  address reads `name/contents`, and every path input takes any
  address.** The path bars, breadcrumbs, properties dialogs,
  transfer labels, directory picker and search scopes all speak one
  display form — `hetzner/team-files/docs/`, a bucket-scoped source
  folding its bucket away (`website-prod/assets/`), the buckets view
  bare (`hetzner`), the remote pane `name/path` at its root
  (`backup-box/`), and the local side a bare native path
  (`C:\Users\...` — `local://` is gone from the UI, though it
  still pastes back). The form is an input too: the address ladder
  (Go `ParseAddress` and the battery shim alike) gains the bare
  `Name/contents` step — first segment names a configured source
  (name or id, case-insensitive), the rest is its contents — so a
  copied path pastes straight into any path bar and a bare source
  name opens its home view; the pane's own bare-path shortcut
  steps aside when the first segment names a configured source
  (rebinding instead of navigating the local binding), while every
  URI family keeps working (`s3://bucket/key`,
  `ftp://user:pass@host:21/root`, `file:///C:/...` and
  `file:///home/...`, drive letters, UNC, `~`) and `NAME://` stays
  accepted for old clips. Copy path carries the normalized form on
  every surface it lands — objects, buckets, remote and local rows,
  the properties Path rows, the search results' Source column — and
  the search popout URL now carries its scope's source so a native
  window's preset label reads the same form. The source type label
  becomes a chip: S3, SFTP, WebDAV and friends painted bold in the
  source's accent color over a 13% tint of the same, one
  `srcIconEl` builder serving the tree rows, breadcrumbs and every
  source picker, and the tree's type column widens to a fixed 62px
  stop with the chips left-aligned — the longest label fits without
  the old centered dead air on both sides.
- **One Columns partition for every view in Settings.** The two
  grid-column check-box groups give way to a single Columns
  partition in Settings → View: a chooser — main view, secondary
  pane, Search window — and under it the full catalog as an
  ordered check-list, where a tick shows a column, the list order
  is the column order (arrow buttons move a column) and each row
  names its data type (Text, Number, Date & time). Name stays
  locked on, the Search window gains its first Settings selector
  (Source stays automatic and never lists), and Save seats a live
  Search window on the spot — native popouts pick the layout up
  on their next open. The old mergeColOrder apply path (which
  kept a pane's current arrangement and would have discarded a
  chosen order) is retired; grid.js is untouched.

- **The Search window's columns carry the main view's mechanics.**
  The results grid resizes by boundary drag or keyboard (arrow
  keys, Shift for bigger steps, Home/End to the limits, a
  double-click for the standard width back), reorders by header
  drag with the same insert indicator, and grows an add/remove
  check-list from the header's right-click over the full shared
  catalog — Type, Mode, Created, Storage class, ETag — with name
  locked on and Reset columns last; the layout persists per
  window across runs. The Source column keeps its own rule: it
  rides only runs that span origins, parks at the far edge,
  resizes like any column but never reorders and never lists in
  the check-list. The rows themselves stay the window's own
  contract — a streaming list, single select, Enter or
  double-click to open — untouched; grid.js is reused as-is and
  a sibling module (srgrid.js) mirrors its mechanics against the
  search catalog. The header band also gets its true height back:
  the auto-height window's content-driven sizing cycle had
  squeezed the band well under the row height (headers rendered
  as clipped slivers, the rows riding over the band's overflow)
  and the band is now locked structural.

- **One window at a time: a second launch focuses the running app.**
The Windows desktop build now runs a single GUI instance: a named
mutex (per login session) marks the running app from the first line
of startup, and a second launch signals it through a named event —
the running window comes forward (restored if minimised, the popout
group lifted behind it) — then exits quietly. Two instances would
contend the same WebView2 user-data folder and the loser would hang
into the startup watchdog, the exact shape the app's preflight
defenses exist for; the kernel reclaims the lock when the process
dies (no stale lock after a crash), guard failures fail open to the
old multi-instance behavior, and S3B_MULTI_INSTANCE=1 opts out for
side-by-side copies. The CLI, the headless build and the server
build stay multi-instance.

- **Path bars take any address, and every path normalizes to
  `Source://`.** Both content panes' path lines are universal
  address bars now: paste a bare local path (`C:\Projects`,
  `\\server\share`, `~`, a `file:///` URL), an `s3://bucket/key`
  URI or a connection URI (`ftp://user:pass@host:21/root`,
  `sftp://`, `webdav://`) and Enter opens where it points — a local
  address opens the secondary pane at the directory (rebinding it
  from a remote source), an `s3://` URI resolves the source that
  owns the bucket, and a connection URI no source matches stands
  its source up on the fly and opens its root. One resolver ladder
  behind both bars — a new App.ParseAddress sharing the CLI's
  connection-URI parser — keeps GUI and CLI in agreement, and the
  pane's local side reports its directory as `local://C:\...`.
  Copy path now carries the normalized `Source://` form everywhere
  it lands (context menus, the Edit menu, properties dialogs), so a
  copied path pastes straight back into any path bar; the separate
  Copy S3 URI action is gone and Copy URL keeps real addresses.

- **Data source types read as bold text.** Every surface that
  identified a source by an icon — the sidebar tree, the breadcrumb
  roots of both content panes, the Doctor and view pickers — now
  shows the type as a bold label (S3, SFTP, FTPS, WebDAV, Local, …)
  painted in the source's accent color. The labels match the
  Add-source dialog's type list, and the secured variants no longer
  lean on composite lock badges — the name itself carries the
  distinction. With the glyph set gone the app embeds no
  third-party icon data anymore (the NOTICE drops its Bootstrap
  Icons entry; the remaining arrows and funnels are hand-drawn).

- **The secondary pane starts every app run fresh.** Where the pane
  stood is now session-scoped: closing the app forgets it, so each
  run's first pane open lands on the workstation's home folder —
  never the onboarding picker (which used to stand up when a
  location remembered by an earlier run pointed at a source that no
  longer existed) and never a stale cross-run location. Inside a
  run the memory is unchanged: × closes honestly and the next open
  returns to where the pane stood, and the view picker's **Return
  to last view** keeps its target. The pane's canvas also matches
  the primary's now — its background and bottom strip sit on the
  app's base color instead of a step-brighter panel tint, so the
  two columns read as true twins edge to edge.

- **A global bar under the menubar carries the app-scope controls.**
  The controls that act on the app as a whole — **Dual-pane** (F9),
  **Compare** (it needs both panes by definition), the light/dark
  theme toggle and **?** (F1) — now live on a slim bar of their own
  directly under the menubar, lifted out of the content panes.
  Each pane's toolbar keeps only what acts on that pane — back,
  forward, refresh, upload, download, new folder, new file, Search
  — the same set in the same order on both sides, so the main view
  and the secondary pane stay button-for-button twins below the
  global bar and each column keeps its full width for its own
  controls.

- **The secondary pane's Search scopes to the pane alone.** The
  pane's own Search button opens the Search window with exactly one
  scope: the pane's current binding — an S3 binding searches its
  bucket and prefix, a remote binding its folder, and a local
  binding walks the workstation's filesystem under the pane's
  directory (a new local mode in the search engine) — labeled by
  its path in the dropdown with no *All data sources* entry. A
  pick navigates the pane through its own history and selects the
  row, the twin of the main view's search-pick behavior.

- **Dual pane: a fresh pane opens on the workstation's home folder.**
  A pane with nothing remembered no longer stops on an onboarding
  picker — it opens straight on the local directory (the old Panes
  panel's default, never an empty stop). The picker keeps its one
  real job: it stands only when a remembered source has vanished,
  asking which source should take its place.

- **Dual pane: the Dual-pane button does what it says — close — with
  one courtesy for a remembered view.** With the pane open, clicking
  the toolbar's **Dual-pane** button (either pane's copy) closes it.
  With the pane closed it opens — plainly on the standing view —
  unless the pane was left somewhere that is not the default home
  (a remote source, a wandered folder): then a small anchored picker
  stands under the button while the pane stays hidden. **Home view**
  reopens the pane fresh at the workstation's home folder with the
  history cut clean, and **Return to last view** reopens straight on
  the remembered spot — the same door as any navigation, so when the
  remembered spot is the view the hidden pane still holds, the pick
  re-lists that very place and history moves nothing (Back rests).
  Escape, an outside click, or the button again dismisses the picker
  and the pane stays hidden. F9 and the View menu stay the plain
  toggle (they open on the standing view, never the picker), and the
  pane's × stays a closer.

- **Dual pane: the secondary pane is a full twin of the main view.** The
  old "Panes" side panel and its Sync toggle are gone; F9 or the
  **Dual-pane** toolbar button now opens an optional secondary pane on
  the right that mirrors the content area button for button — its own
  toolbar (upload, download, new folder, new file, compare, search),
  interactive breadcrumb, quick filter, back/forward/refresh history,
  parent row and status line. A fresh pane opens on the workstation's
  home folder; the tree's right-click menu gains **Open on secondary pane**
  on every data source, bucket and folder, landing the pane there
  directly. Any source type binds to it — S3, SFTP, FTP, WebDAV, local
  drives — the last location is remembered across reopens, the pane's ×
  or F9 closes it, and drag & drop between the panes feeds
  the same transfer matrix. Directory **Compare** stays;
  synchronized browsing is removed with the old panel. Under the split,
  each toolbar folds its word labels to icons (and, narrower still,
  folds the menu-covered buttons) on its own, so neither column
  overflows at any width.

- **Dual pane: the secondary pane's path bar is the main one's twin,
  function for function.** Clicking the pane navbar's empty area swaps
  the breadcrumb for the same inline editable path line the main view
  has: it holds the pane's canonical path (`Name://bucket/prefix/` for
  sources, the raw directory for the local side), copies out, and a
  pasted `Name://` path of any source points the pane there — a bare
  directory navigates the local binding, and an unbound pane binds
  straight from a pasted path. Enter navigates, Esc restores, an
  unparsable line toasts; the parser is one shared routine
  (`parseSourcePath`), so a path means the same thing in either pane.

- **Not-connected is now a first-class state.** A source that drops
  mid-session becomes visible without any refresh: the source behind
  the active view is probed once a minute, and a failed connection
  raises a slim persistent strip above the content — the rows stay on
  screen ("showing previous content") with a Reconnect action. Failed
  loads classify themselves ("Source not connected", the existing
  timeout title) and put Reconnect beside Retry; the sidebar status
  ball agrees with what the content area shows; Reconnect heals for
  real (re-save the source to drop cached engines, probe, and reload
  the view when it belongs to that source); recovery is announced and
  auto-refreshes the stale view; and silent background-refresh
  failures escalate from a one-time toast to the persistent strip
  instead of staying invisible.

- **Data sources filter: one pattern narrows every source, wildcards
  included.** The sidebar header gains a funnel next to the **+** that
  opens a filter panel for the whole Data sources tree. Plain words
  match as case-insensitive substrings, `*` and `?` act as wildcards,
  and space- or comma-separated patterns OR together. The pattern
  filters the entire hierarchy — sources, buckets and folders — with
  pass-through ancestors dimmed, an honest "no matches" note when
  nothing survives, and the Favorites list narrowed to matching
  buckets. Loaded content filters instantly as you type; emptying the
  box restores the tree, Enter applies at once, and Escape closes the
  panel while keeping the pattern. When a pattern matches nothing
  loaded, the no-match note offers **Search all folders**: the walk
  loads the unloaded depths of every source in the background (the
  sidebar's bottom bar names the source being scanned; its Stop
  button cancels and opts back out; clearing the pattern resets the
  opt-in) so deep matches surface without opening anything. The
  funnel icon hides itself entirely when no data sources exist.

- **The Data sources area has its own bottom bar.** The sidebar counts
  its tree at all times — *n* items with no filter, *n* shown while a
  pattern narrows it, the source being scanned with a Stop control
  while a deep walk runs — mirroring the content area's size bar. The
  plain item count moves here from the window status bar, which now
  speaks only for a selection (selection summaries stay, the idle span
  stays empty), and the bar hides itself along with the funnel when
  no data sources exist.

- **Content size bar: the real recursive size, every source type,
  version-aware.** The content viewer gains a WinSCP-style bottom
  bar: with nothing selected it shows the real recursive size of
  everything listed; with a selection, the recursive size of the
  selected items — folders always count their whole interior. It
  works on every source type the viewer hosts (bucket lists, S3
  folders — versioned, suspended or plain — and every remote engine),
  always for the single source being browsed, never across data
  sources. Versioned buckets additionally price the history in:
  noncurrent version bytes and delete-marker counts next to the live
  content. Walks run in the background (a busy placeholder while
  calculating, cached per folder, stale results never paint) and a
  failed walk degrades honestly to listing-level sums marked
  *partial* with the error on hover. The footer's old byte count —
  a non-recursive level sum that read like a total — is gone (its
  counts stay).

- **Grid columns: resize, reorder and two new data columns.** Drag a
  header edge to resize and the header itself to reorder — widths and
  order persist per pane. The dragged edge follows the pointer
  exactly: the first resize pins the stretch column, growth beyond
  what the pane has becomes the horizontal scrollbar instead of
  squeezing other columns, and every boundary keeps a whole grab
  strip even when it sits flush with the pane's right edge
  (double-click resets; Arrow keys resize a focused edge). The header
  picker gains **Reset columns** — one click restores the out-of-box
  set, order and widths.
  **Date created** (bucket views, WebDAV
  creationdate, Windows local birth times) and **Mode** (local and
  SFTP permission bits) join the opt-in set, and the Type column now
  names common file types ("PNG image", "Text document") in the UI
  language — on by default.

- **The sidebar splitter is a real grab strip.** Pointer capture keeps
  the drag alive past the window edge, the boundary keeps its offset
  from the grab point (N pixels dragged, N pixels moved), a stale
  saved width clamps on boot, and double-click resets.
- **The speed-limit setting covers fast links: 50–1000 MB/s tiers**
  (the 256 kB/s floor and every step beneath are unchanged).

- **Import S3 Credential accumulates picks** — files and service
  entries can be combined across multiple rounds of picking in one
  import, so a mixed fleet (a shared credentials file plus Vault
  entries, say) lands in one pass.

- **Linux arm64 is a first-class desktop platform.** CI builds and
  tests the GTK3 GUI natively on an arm64 runner, and releases ship a
  `linux-arm64` GUI tarball next to the arm64 headless-CLI portable.

### Changed

- **The CLI speaks the GUI's unified formats.** The terminal read
  sizes, dates and source identities its own way: byte counts always
  carried a decimal (217.2 MB where every GUI surface reads 217 MB,
  rounding whole at a hundred), the stat views, version timelines and
  retention echoes printed seconds or a raw RFC3339 zone where the GUI
  reads its unified minute-precision local form, and `source list`
  spelled a bare name over a raw type where every GUI surface wears
  the typed identity. All of it now flows through the one scheme —
  sizes share the GUI's rounding rule, object and retention dates read
  the GUI's local form (machine JSON keeps its RFC3339 contract), and
  `source list` and the connect test speak the `S3 · name` typed
  label.
- **One source identity everywhere.** Every surface that names a data
  source wears the same format the breadcrumbs, the sidebar and the
  pickers already used: the source's type badge beside its name. The
  Search window's Sources dropdown and the secondary pane's source
  picker are native option lists, so they carry the badge's text form
  (`S3 · hetzner`, `SFTP · backup-box` — replacing the old
  `hetzner (s3)`), typed through each window's own source list so
  preset and solo scopes speak it too (the native solo popout now
  loads its source list for exactly that); the Search results' Source
  column seats the tinted chip before each origin text; and both
  content bottom bars' source tags carry the badge beside the name. An
  unresolved name keeps its plain text — never a wrong badge.
- **Folder rows show their information like files do.** Folders were
  blank in every column but Name, Type and (sometimes) Date created;
  they now follow the same presence-driven contract as files across
  every content view — main view, secondary pane and the Search
  window. A bucket row shows its creation date in the buckets view;
  an S3 folder shows the date, storage class and ETag of its own
  folder marker (a common prefix alone stays honestly blank — S3
  reports nothing for it); local, SFTP and WebDAV folders show the
  directory's real modified/created dates and permission mode; folder
  hits in Search results carry their dates the same way. Size stays
  the one file-only column: a listing cannot know a folder's content
  size without walking it, which is the Properties dialog's job.
- **Editable path lines speak the typed address.** Opening either path
  editor (main view or secondary pane) now seeds the line with
  `scheme://Name/contents` — `s3://testijotain/`, the scheme naming
  the source's type — instead of the bare `Name/contents` form, and
  the untouched seed pastes straight back through the ladder's typed
  door (Enter re-lists the same place). The scheme is visible only
  while editing: the resting breadcrumb keeps its type-icon
  presentation, local paths stay bare (`C:\Users\...` — the native
  path is its address), and the plain `Name/contents` form still
  pastes back everywhere.
- **Typed source addresses: scheme://Name/contents pastes into any
  path bar.** Every app-added data source now carries its canonical
  address form — the scheme is the source's type, the first segment
  its name (`s3://hetzner/team-files/`, `sftp://backup-box/`) — and
  the universal address ladder takes it: the named source opens
  outright, no stand-up, in the main view or the secondary pane. Real
  connection URIs are untouched (an authority segment — user@, :port,
  ?query, #fragment — still rides the connection family, and a first
  segment naming no source of that type falls through to it). The
  editors' own form is unchanged: the editable line still seeds and
  accepts the bare Name/contents display form, and the resting
  breadcrumb keeps its type-icon presentation.
- **The Dual-pane picker's Home view wears a house.** The destination
  picker's restart option drops the 'Local' type chip for a house
  glyph in the picker's own icon idiom — a chip speaks a source
  type, the restart speaks a place.
- **Dual-pane resizing keeps its balance: the seam seats a share, not
  pixels.** Dragging the splitter still rebalances the pair (the 300px
  floor and the 60% ceiling stay), but the seat is now a percentage
  share of the pane row that lives for the session only — resizing the
  app window scales both panes together with the seam keeping its
  proportion, instead of handing every added pixel to one side. Nothing
  is persisted anymore: every fresh launch opens the panes equally wide
  (a legacy stored width is drained once at boot), and a double-click
  on the seam still restores the even 50/50 twins.
- **Settings COLUMNS orders by drag: grab a row, drop it in place.**
  The column manager's list rows are now draggable — the live insertion
  slot marks as an inset accent bar on the target row's top or bottom
  edge while the pointer travels, the dragged row fades to 45%, and the
  checkbox never starts a drag (the same guard the main grid's rows
  carry), so a tick can't ghost the row. Alt+Up / Alt+Down move the
  focused row for keyboard reach, the rows take a grab cursor, and the
  ▲/▼ buttons retire along with their col.moveUp/col.moveDown keys in
  all 15 languages; every colsHint learns the drag wording, the
  battery's reorder legs turn into synthetic DataTransfer drags over
  the neighbour row's halves (settings-honor walks to the View page
  first — a drag reads row geometry, and the book hides inactive
  pages), and usage documents the drag.

- **The content bottom bar says whose files it is counting.** The
  active data source's name moves from the app footer to the content
  area's own bottom bar — bold, on the bar's right end, where
  WinSCP's own status bar carries its session name — with the size
  text on the left. The app footer keeps jobs and selection state
  only.

- **The parent-directory row ("..") is the first row of the listing
  itself — WinSCP Explorer's parent entry.** Every content view —
  buckets, S3 folders, remote sources — carries the one-click climb
  to the directory above as a row inside the grid body, styled like
  the folder rows around it — the same folder glyph they wear — a
  backward-arrow badge on its corner — beside the `..` caption,
  seated in the Name column exactly where a
  folder's own name sits (the row mirrors the grid's live column
  template, so resizing or reordering the columns never breaks the
  seat).
  It scrolls with the content, stays present over an empty folder
  (floating above the empty-state panel), and where nothing sits
  above it — a source's top level, a bucket-scoped source's root
  included (its home IS the bucket's contents) — there is no row at
  all; it also
  steps aside while an information panel owns the area (loading
  skeleton, errors, first-run). As a plain row of the listing it
  never joins selection, counts, the size bar, sorting or filters.
  The feature is opt-in as a whole — Settings → View or the View
  menu toggle, off by default — and while it is hidden the climb
  keys rest with it. The toolbar's Up button is retired — Alt+↑ and
  Backspace climb from the keyboard while the row shows — and
  back / forward wear proper arrow icons from the app's icon family.

- **Deep search is now Search — a real window across every data
  source.** The one-bucket modal becomes a floating Search window
  (Ctrl+Shift+F, the toolbar button, View → Search) with the same
  multi-window behavior as Running tasks and Transfers: draggable,
  resizable, remembers its place, one instance that refocuses instead
  of stacking. It defaults to searching every data source — every
  bucket of every S3 source plus every remote source (local, SFTP,
  FTP, WebDAV) from root — and the **Sources** dropdown is a flat
  list with no grouping: *All data sources* plus every configured
  source by name, an S3 source searched across every bucket it
  holds, a remote engine from its root; context-menu *Search in
  this folder…* presets it. Sources is a primary control now —
  beside **Name**, always in view — and the rest — Kind
  (any/files/folders), Limit, the new Extension and Path-contains
  filters, larger/smaller, older/newer — fold out under a bordered
  *More filters* chip that counts the active filters, every one of
  them working the same across S3 and remote sources (storage
  class never could, so it left the window — the CLI keeps
  --class). The window rides its own wide tier and the results wear
  the main view's own content-area chrome — the same column header,
  hairline rows and bottom bar — framed as a content panel by the
  app's standard hairline border at the form's side inset, with
  type-matched icons, a click-to-sort header
  (ascending, then descending) and a **Source** column that appears
  only when the run spans origins, parked last on the row; only the
  results scroll — streaming pages land without ever auto-scrolling
  the view; **Clear** resets the form and results stay
  cancelable; clicking a hit navigates to the object and selects it
  (a hit clicked in a native popout window relays through the main
  window). The CLI gains the same filters: `s3b find
  --kind file|dir`, `--ext` and `--path`.

- **Settings apply on Save, not on click.** The dialog is now a draft:
  every control stages until Save (disabled until something changes),
  Save applies the whole diff and closes, a dirty close asks before
  discarding, and Reset to defaults stages the defaults instead of
  wiping live. The Security page stays live — enabling it rewrites the
  store, and the log rows re-sync from what the backend then reports.

- **Save logs to file defaults to off.** File logging joins the rest
  of the log controls as opt-in: a fresh install writes no log file, a
  v2-era store that persisted the then-default is migrated to off
  (deliberate off and custom-folder choices always survive), and Reset
  to defaults leaves logging off instead of reverting to the
  pre-v1 App-settings-folder mode.

- **CI hardened and self-sufficient.** Actions bumped to node24
  runtimes, runners pinned to ubuntu-24.04 (the -latest label moves to
  Ubuntu 26 in October 2026), e2e fixture images digest-pinned, per-job
  timeouts and concurrency cancellation added, release permissions
  dropped to least privilege. The e2e MinIO fixture now runs from a
  pinned GitHub-release binary — quay.io's refusal of anonymous pulls
  had broken both e2e jobs despite beta.21's login workaround; no
  Docker and no account are needed at all now. The settings battery
  walks every control and default end to end, and Import S3 Credential
  is verified against a real Vault.

- **The Search window reads its columns at a glance.** Both faces of
  the Search window widened: the floating window rides its own 940px
  tier (from 860) and the native window opens at a 900×640 default
  (from 760×600), so the four-column results table breathes; the
  Source column's default width grew to 180px — wide enough that the
  type badge plus a source/bucket pairing (toinentesti/testijotain)
  reads whole instead of clipping mid-word. The shared column widths
  (Name, Size, Date modified) stay exactly the main view's, so the
  two surfaces never disagree about a column's shape.
- **Type joins the Search window's out-of-box columns.** Every grid
  the app seats now opens with one layout: the Search window's
  results table starts at Name, Type, Size and Date modified — the
  same set, in the same order, the panes do. The default is the
  panes' own list itself (a spread of it, not a sibling that could
  drift), so Type — on by default in the main view since the
  beginning — needs no opt-in in search results and sheds the
  extra-column background tint it wore there. A saved column choice
  still wins, Reset columns restores the shared default, and
  Settings' Reset to defaults seats all three views at the same
  four.
- **Docs and screenshots catch up with the whole 1.1 surface.** The
  verified screenshot set is republished from the current battery (all
  thirteen docs images — every one a battery capture whose
  subject-on-screen was asserted before the shutter, the Search window
  shot now showing the widened window with Type seated second and the
  badge-plus-pairing Source column), the README grows the Search
  window's own feature block plus the engine cycle guard, folder sizes
  and the late-beta 1.1 headline list, the safety ladder in
  docs/security.md and the CLI's cp/mv help carry the engine cycle
  refusal, and the usage guide's transfer bullet states it in the GUI
  face.

### Fixed

- **The solo Search window no longer dangles a slash at whole-source
  scope.** The pane's find button locks the window to one scope, and
  when that scope was the source's own root the label read
  SFTP · backup-box/ — a trailing slash with nothing behind it. The
  root scope now reads the typed name alone.
- **Toasts ride above the bars they used to smother, and each carries
  its own ✕.** The toast stack parked at the window's bottom edge,
  over the content bar's counts and the secondary pane's info bar; it
  now sits raised clear of both, and a close button dismisses a toast
  on the spot instead of waiting out its timer.
- **Tree guard icons hover as a tinted chip, never an underline.**
  The versioning and lock glyphs in the sidebar carried a link
  underline on hover — an underline strikes through an icon's feet
  and reads as an artifact; the hover now tints a soft chip in the
  row's own color instead.
- **Pane toolbar controls no longer vanish at ordinary window widths
  under the dual-pane split.** The fold tiers for word labels and
  button hiding were calibrated for full-width toolbars, but under
  the split each pane toolbar measures its own column — half the
  window or less — so a pane's Search, New file and New folder
  buttons folded away while the window was still comfortably wide.
  The tiers are per bar now: the global bar (Dual-pane, Compare,
  theme, help — full window width) keeps its thresholds, while a
  content pane's words fold at a narrower column and its buttons
  fold only when the column itself is truly cramped. Every shortcut
  and menu path keeps working in every tier.
- **The pane Search window kept its Sources dropdown changeable.** A
  solo window carries exactly one scope, yet the dropdown stayed an
  enabled, openable input — it reads as if the source could be changed.
  The Sources select now renders disabled in every solo window (the
  pane's find button, a local view's own-folder search, native popout
  and DOM window alike), dimmed with the shared disabled-input style,
  while the full Search window keeps its changeable list.
- **The secondary pane Search window opened empty in the OS-native
  popout.** The pane's locked scope rode into the native window's query
  only when it named a bucket — a bucket-less s3 scope (a scoped source
  folded to its own name, an account-wide source at its bucket list)
  vanished from the query, and a solo window carries no All-sources
  fallback to catch it: it booted with an empty Sources dropdown and
  nothing to run. s3 scopes now always mark their family (mode=s3 rides
  source/bucket/prefix), the popout bootstrap reads it ahead of the
  legacy bucket-keyed form (old links still parse), and the battery
  walks a native solo window end to end — one locked option, a run on
  that source alone, and picks relaying with the pane flag.
- Search's Source column doubled a source named after its bucket
  (testijotain/testijotain — the credential-import flow's own naming):
  the column and the Search window's scope labels now collapse the
  bucket when it matches the source's name, so the origin reads once.
- The secondary pane's find button showed its scope as a raw source
  id (src-*) in an immovable dropdown: the Search window now opens
  auto-selected on the pane's current data-source location — one locked
  scope reading the data source's NAME (a source named after its bucket
  reads once) — and every pick lands on the pane.
- The content bottom bar lost its data-source name after a local detour
  (a workstation path pasted into the main path bar cleared the tag) and
  it stayed lost on returning to the very source that stood before: the
  repaint no longer skips when the source did not change.
- The Data sources list carried a WebDAVS-sized hole at its left: the
  source-type chips sat right-aligned in a fixed 62px type column, so a
  list of S3 / SFTP sources parked ~30px of dead air left of every
  chip. The column is now measured — the tree seats it on the widest
  chip the list actually carries (plus the chip's 2px cushion) — so
  chips keep hugging the source name at the column's right edge,
  folder and bucket glyphs keep centering, and every name keeps the
  one shared stop; the sidebar's whole stack (header, filter strip,
  favorites head, no-match note, footer) now sits on the same 8px left
  rail as the rows (battery-asserted: the column equals its widest
  chip, names share one stop).

- The tree's source-type chips hugged the type column's left edge,
  leaving a band of dead air between the type (S3, SFTP, WebDAVS, ...)
  and the source name beside it. Chips now right-align inside the fixed
  62px column — the row gap plus a 2px cushion is all the air between
  the chip and the name — while folder and bucket glyphs keep centering
  and source names keep their aligned stop (battery-asserted: every
  source row's chip sits 4-8px from its label).

- The secondary pane sometimes opened on the onboarding source
  picker instead of the workstation home: a location remembered by
  an earlier app run pointed at a source that no longer existed
  (or had not loaded yet), and the picker stood up for it.
  Remembered locations no longer cross app restarts, so a fresh
  run's first open always starts at the home folder.

- Forward now arms on every climb to an ancestor, not only after
  the Back button: clicking a breadcrumb segment above the current
  folder, a data-source tree node above it, the parent row or a
  typed path above where you stand all leave the deeper view one
  Forward away instead of cutting it — Explorer's rule — and
  re-listing the very same place (clicking the crumb or tree node
  that is already current) moves no history at all. Both history
  engines follow: the main view's and the secondary pane's own.

- The seam between the Data sources sidebar and the content area no
  longer opens a grey gutter: the resize strip now fully straddles the
  sidebar's own hairline (its two overlaps sum to its width, so it
  claims no layout slot of its own) and the content's surfaces begin
  at that hairline — the divider reads like every other seam in the
  app.

- The tree context menu's Reconnect no longer wipes every other
  source's status ball to "unknown" (a one-source status update now
  merges into the map instead of replacing it) and no longer toasts
  "Reconnected" before the probe has said so — it funnels through the
  same reconnect path as the error surfaces and reloads the current
  view when that source owns it. Status updates that change nothing
  also skip their re-render, so a landed listing no longer rebuilds
  the tree to re-green an already-green ball.

- Column-resize drags no longer misbehave at the edges: the boundary
  between two columns grabs from both sides, and a rightward drag in
  narrow panes no longer snaps the column to the 48 px floor.

- The macOS Help menu no longer carries Wails' default "Learn More"
  item, which navigated the app window itself to wails.io (thanks
  @Tapiolavi — PR #2).
- **Search honors each source's own boundary — no more cross-source
  leaks.** Two S3 sources over one endpoint made the Search window a
  sharing ground: the engine walked the ENDPOINT's bucket list for
  every source, so a bucket-scoped source returned hits from buckets
  belonging to other sources, stamped with the searching source's
  name (the Source column read pairings like toinentesti/testijotain
  the workspace never configured) — and a folder-scoped search swept
  its whole bucket, because the scope's prefix was dropped on the
  floor. Each source's own boundary now rides with the search job:
  a bucket-scoped source searches exactly its one bucket (by name or
  by the pane's raw src-* id alike), an account-wide source keeps
  its every-bucket walk, and the walk itself starts inside the
  scope's folder prefix; the Search window's early-page replay
  filters by token like the live path, so a run streaming while
  another window's search is in flight can no longer paint its
  pages — sources and all — into that window's results. The engine
  refusals are pinned by Go tests against real search runs
  (search_scope_test.go), since the GUI battery's backend mock was
  already honest.
- **A locked solo scope wears no dropdown arrow.** The secondary
  pane's Search window locks its one scope outright, and its Sources
  input rendered disabled — but still carried the native dropdown
  arrow, which reads as a control that opens. The arrow drops with
  the lock on every solo face (the DOM popout and the native popout
  window alike), while the full Search window keeps its changeable
  list, arrow and all.

## [1.1.0-beta.21] — 2026-09-27

### Fixed

- **A data source never repeats its own identity in its own paths.**
  Every path surface now renders the canonical hierarchy
  `<source>://<content>`: a bucket-scoped S3 source's name already IS
  the bucket, so the bucket never appears again after `://` (the
  reported `testijotain://testijotain/…` doubling is gone), and the
  scoped root crumb stays inside the source's contents — the account
  bucket list is unreachable from a single data source, by design.
  Legacy account-wide sources keep the bucket as their first content
  segment, and a path copied under the old doubled rendering still
  navigates (parsePath folds an exact-case repeated bucket away). One
  scope-aware composer feeds the navbar path bar, breadcrumbs,
  properties windows, transfer labels, the dual-pane crumb and the
  directory picker; remote sources render the same hierarchy with no
  host or port leak. The CLI refuses `NAME://` browsing with s3://
  guidance while cp/mv accept scoped operands as content-only
  (CLI-S3-48, GUI-85, GUI-86).

- **External links can no longer hijack the webview.** A middle-click
  or new-tab gesture on an external link now routes to the OS browser
  (with a window.open fallback), and the context menu is suppressed on
  external links — no gesture can swap the app out for an external
  page inside the webview.

- **Aborted FTP uploads no longer strand remote staging.** After a
  torn data connection the pooled control channel desyncs, and the
  discard path used to mistake a poisoned listing verdict for "no such
  file" without ever issuing DELE — leaving the staged partial
  behind. Suspect verdicts are now wrapped as unreliable (with
  redial), the miss verdict must be earned with a SIZE probe, and the
  discard retries on a fresh connection; wire-proven end to end
  (GUI-71). Remote stage names also lost their leading dot
  (`s3b-part-` prefix), so a stranded stage is visible to users, and
  transfers re-read size after staging.

- **ClearObjectRetention requests governance bypass** — MinIO rejects
  the clear with "Object is WORM protected" otherwise.

- **The License window's version row can no longer stick at `v?`.** A
  freshly opened license popout could render before the Wails
  bindings land, and the swallowed version-fetch rejection left the
  About row empty for the window's whole life. The fetch now retries
  (bounded) until the binding answers.

### Added

- **macOS builds are supported and documented.** A dedicated build
  path (`scripts/build-macos.sh`, `docs/macos-build.md`) takes a local
  macOS build from the Xcode toolchain through notarization, with CI
  wired to match; the README slims to pointers (PR #1).

- **The verification matrix grew from 138 to 169 rows** across five
  critical-action rounds: every data-affecting action (deletes,
  edits, cancels, overwrites, purges, conversions, moves) swept
  across all five source types on both faces; sync download
  `--delete` aiming only at the extra local file; count-phase purge
  cancel; remote-delete interruption cells (cancel and wire-death
  with honest partial counters, RemoveSource mid-loop, engine healing
  after wire-death, keyring hygiene around battery wipes); the
  cross-source engine under wire-death (no partial ever sits at the
  final name); the editor concurrent-change race on a versioned
  bucket (last-writer-wins, the loser recoverable); and the canonical
  path-hierarchy rows. CI now logs in to quay.io for the MinIO
  fixtures (anonymous pulls are refused upstream).

## [1.1.0-beta.20] — 2026-09-26

### Changed

- **The icon master is the author's draw.io original, verbatim.**
  beta.19's brand-mark redraw smoothed the folder's edges with
  computed curves; the author's actual draw.io master — one straight
  hinge slope per edge — replaces the approximation, and every surface
  (the exe icon, popouts, About box) re-renders from the adopted
  vector.

- **README restructured for builders.** The build guide is renamed
  "Build from source" and the CLI quickstart becomes usage-only; the
  macOS path is documented end to end — Xcode toolchain through
  notarization.

## [1.1.0-beta.19] — 2026-09-26

### Added

- **A brand mark, everywhere the app shows itself.** One vector master
  (build/icon.svg — blue folder, gray back panel and tab, darker fold
  shadows) now feeds every surface the app presents: Windows exes embed
  a 7-image icon through the versioninfo tool's new -icon flag
  (RT_GROUP_ICON lands at resource id 3, the slot the Wails window shell
  loads, so the taskbar, Alt-Tab and the window corner all match), the
  NSIS installer and uninstaller carry build/icon.ico, macOS bundles
  carry AppIcon.icns through CFBundleIconFile, Linux windows get the
  mark via Options.Icon, and the frontend serves assets/logo.svg as the
  favicon, the empty-state mark and the About-box logo; the README leads
  with it. Derived assets are deterministic: scripts/gen-icons.mjs
  rasterizes the master through headless Chromium into build/iconset,
  and tools/appicon (stdlib-only, unit-tested) assembles build/icon.ico
  (DIB32 below 64px for legacy shell surfaces, PNG above) and
  build/AppIcon.icns from that set — the committed binaries change only
  when the artwork does. The master itself was traced from the supplied
  artwork and validates at 99.3% pixel-class agreement against the
  source raster.

### Removed

- **macOS builds — withdrawn from every release and from the pipeline.**
  The dmg images the releases shipped through v1.1.0-beta.18 carried
  non-functional apps: every darwin asset is deleted from the existing
  releases (each SHA256SUMS regenerated without them) and the darwin
  job is gone from the release workflow — v1.1.0-beta.19 onward ships
  Windows and Linux only. macOS stays build-from-source (the README
  keeps the recipe, marked experimental), with no support guarantees
  while the verification fleet has no Mac.

### Changed

- **README: one Quickstart build guide for every OS.** The build
  instructions that lived inline in Quickstart (GUI) moved into a
  dedicated Quickstart (build from source) section with a real recipe
  per platform — Linux desktop GUI (gtk3/webkit2gtk 4.1), Windows
  (plain + release-style with the icon-bearing versioninfo syso),
  macOS (local recipe), the headless CLI and the windowless
  browser-driven build — each mirroring what the release workflow
  actually does; Quickstart (GUI)/(CLI), the requirements bullet and
  docs/usage.md’s build link all point at it.

### Fixed
- **Cancel on a sub-view now goes back, not out.** Cancelling the
  start-directory browser while adding a data source closed the
  whole editor with it — the dialog framework had a single shared
  slot, so a sub-dialog replaced its parent and the parent was gone
  when the child closed (Escape was worse: both dialogs' handlers
  were live, so one press wiped both; the parent's handlers also
  leaked). Modals are now a stack: only the top one answers Escape,
  Tab and the backdrop; the parent is parked with every field value
  intact and returns exactly as it was when the child closes —
  Browse → Cancel (or Escape) lands back in the editor, the
  browser's New-folder prompt stacks a third level and returns to
  the browser, the admin panel survives its cleanup windows, the
  settings sheet survives its Reset confirm, and the import dialog
  no longer re-shows itself after a nested prompt (the old
  workaround double-stacked it).


- **Add-data-source name auto-fill no longer truncates.** A remote
  host shrank to its first label — "10.20.3.65" suggested the name
  "10" — and a start directory replaced the host entirely with the
  directory's last segment. The suggestion now carries the full host,
  and once a start directory is set, "<host> - <start directory>"
  ("10.20.3.65 - /var/media/clips"); the S3 endpoint fallback likewise
  keeps the full hostname instead of its first label. The name filter
  was relaxed to match — it repurposed every space and slash into
  dashes, which would have made that format impossible — safe because
  names are display-only (IDs key the keyring and workspaces). The
  suggestion also tracks the fields as they are typed (not only on
  blur, matching the Browse buttons) and clears when the fields it
  described are emptied; a hand-typed name still always wins.

- **A rename can no longer clobber what already owns the target name.**
  F2 onto an existing name used to merge silently: the file rename
  overwrote the target object, and a folder rename copied its contents
  INTO the target folder and then deleted the source — two trees
  quietly becoming one. The occupancy guard that was supposed to stop
  this probed a double-slash key ("name//") and so found nothing, and
  remote sources (SFTP/SCP/FTP/WebDAV) had no guard at all. Now every
  rename — file or folder, S3 or remote — refuses when the target
  exists in ANY form (the file key, the folder marker, or anything
  beneath the prefix: a folder with no marker still counts as
  occupied), a folder rename lands beside its parent under the new
  name, and a same-name rename is a no-op. Remote renames Stat the
  target first — engine Rename semantics vary (SFTP overwrites files
  silently, WebDAV MOVE merges by server policy).

- **A stopped editor session stays stopped.** Discarding an edit
  (StopEdit with upload=false) ended the session, but the watcher
  goroutine could outlive it and keep pushing the discarded local
  changes to the object on its next poll — a discard that un-did
  itself about a second later. The watcher now re-checks the session
  state on every tick and exits with it: what the user throws away
  stays thrown away.

- **--no-clobber now means no clobber on every copy path.** Server-side
  S3→S3 copies and remote-engine uploads (SFTP/WebDAV/FTP → S3) ignored
  the flag outright; worse, mv deleted the source file after a SKIPPED
  upload — the copy was refused and the move still happened — and a
  directory move pruned the emptied source folder recursively even
  when skipped files were still in it. Every path now pre-checks the
  destination, reports the skip, moves nothing, and the source is only
  ever removed after a real transfer.

- **Downloads under --no-clobber no longer truncate local files — and a
  skipped move keeps its source.** S3→local and remote→local downloads
  over an existing local file wrote the new bytes over it (a local file
  has no version history to recover from); mv then deleted the source
  even though the download was skipped — the copy was refused and the
  move still happened — and a directory move pruned the emptied source
  folder recursively, taking the skipped files with it (the return
  count that gated the prune counted skips as copies; the verbose log
  also wrote to a closed file handle). Every download path now
  pre-checks the destination, reports the skip, moves nothing, and a
  source is only ever removed after a real transfer.

- **"Save and close" now pushes the edit that is on disk.** StopEdit
  with upload=true trusted the watcher's dirty flag, which updates on
  a 1.2 s poll — saving and closing inside that window silently pushed
  nothing and lost the edit. The stop path now re-checks the staged
  file against the last state the app knew: an explicit save pushes
  what is on disk, an untouched file still costs no upload (and no
  extra object version), and a save against a WORM-locked object
  lands as a new version with the retained original intact and
  unpurgeable — an editor session can never destroy locked bytes.

- **A partially-failed delete no longer exits 0.** rm, rm --versions,
  versions rm/purge and the move-pruning paths printed the per-object
  errors but still exited 0 whenever some deletions failed — the
  classic case is a WORM-retained version refusing the purge while
  its sibling versions delete, so a scripted
  `s3b rm --versions ... && next-step` would read success with the
  locked bytes still in place. Batch deletes now exit 1 with an
  "N deletion(s) failed" summary whenever anything survived.

### Added

- **Round 3 of extreme-level verification: the destructive surface
  under proof.** The matrix grew from 124 to 132 rows across S3, SFTP,
  WebDAV, FTP and the local disk, CLI and GUI: the execute-time delete
  gate (a preview can go stale — 49 objects previewed, 3 more land
  before Execute, and the fresh expansion refuses without force,
  deleting nothing), canceling a 600-object delete mid-flight (the
  out-of-scope prefix untouched, the scoped one countable, the task
  registry never wedged, the job finishable afterwards), rename
  refusals (file, folder and remote; both trees byte-intact after),
  the editor-discard zombie regression, the local delete ladder
  (filesystem roots refuse; siblings survive; mixed lists report
  honestly), RemoveSource as pure bookkeeping (the data survives the
  removal), and per-engine cross-source delete scope. The sweep also
  caught a row the modal-stack change had made stale: GUI-30 now
  proves the version timeline refreshes in place after a destroy,
  instead of reopening through the grid the restored timeline covers.


- **Round 4 of extreme-level verification: the download side and the
  remaining source types.** The matrix grew from 132 to 138 rows:
  no-clobber download guarantees on S3→local and remote→local — the
  local file, the source object/file and the source folder all survive
  a skip (CLI-S3-46, CLI-X-14); per-engine delete scope now covers
  scp:// and FTP alongside SFTP and WebDAV (CLI-X-13); the remote
  rename guard is proven through the bridge bindings on the FTP
  engine (GUI-59); the editor's explicit save pushes what is on disk
  and is refused by GOVERNANCE retention with the staged file kept as
  the recovery copy (GUI-60 — the row caught the lost-save bug); the
  keep-current purge deletes exactly N-1 versions with the newest
  surviving byte-identical (GUI-61); and a delete canceled while
  still in the count phase deletes exactly nothing, proven through a
  latency-injected source (GUI-62). The cross battery is now
  self-sufficient standalone (--only cross recreates the verifys3
  profile a full run would have seeded), and the scp:// shorthand is
  part of the permanently verified source set (CLI-X-01).

## [1.1.0-beta.18] — 2026-09-24

### Added

- **Every fresh install now walks an accept-license phase.** First
  launch boots into a full-screen gate: the license terms summarized,
  the license name linked to the published PolyForm text, and the app
  held until accepted — Escape cannot dismiss it, Decline leads to a
  blocked state whose only exit is Exit. Acceptance is recorded in the
  config store (`license.json`, 0600) with timestamp, OS user and face,
  keyed by license name+version so a license bump re-runs the phase.
  `s3b license status|accept|decline|show` drives the same record from
  the terminal — the CLI stays script-friendly, no hard gate — and both
  faces prove the shared store in the matrix (GUI-53, CLI-M-08).

- **The License window is now three partitions: About, License and
  Third-party components.** About carries the About-box identity —
  live version from the backend, publisher, license and project links;
  the license name is a link to the published PolyForm text in both the
  About and License partitions. External links open in the default
  browser through the new `OpenExternal` binding (http/https with a host
  only — `file://` and custom schemes are refused before the shell ever
  sees them), never in the app webview.

- **Round 2 of extreme-level rows: the gate grew from 76 to 85.** WORM
  enforcement (GOVERNANCE retention and legal hold each defeat a
  version-purge attempt while in force; clearing re-arms the delete),
  secrets encrypted at rest (a raw byte-scan of the whole config dir —
  the key may live only in the OS keyring), and a wrong-password source
  import that must fail BEFORE any upsert (no partial import). A new
  faultproxy `flap` mode injects canned `HTTP 503` storms: a small storm
  is absorbed inside the SDK retry budget (put lands sha-identical), a
  large one exhausts it honestly (non-zero exit, no half-landed object).
  Hostile filenames — `# & % + ^ $ !`, apostrophes, brackets, semicolons,
  CJK, 120-char names — round-trip through every live engine and S3 with
  names and bytes intact. Through the GUI face: the presign dialog's
  read-only URL verified by an out-of-app client, a pane compare covering
  all six categories exactly, the hidden Ctrl+C staging download (bytes
  staged for Explorer paste-out, bucket bit-for-bit unchanged until
  Paste), and the download-side per-file conflict matrix (skip keeps the
  local file, rename lands the twin).
- **Extreme-level rows for critical data sources.** The verification gate
  grew from 64 to 76 rows, targeting the contracts that matter when the
  data source is critical. Byte-level integrity via sha256 (not size):
  a 32 MiB multipart round-trip and every "identical" claim in the new
  rows. Scale and hostile input: 1006-object pagination across list-API
  page boundaries; keys with unicode, `%2F`, `+`/`=`/`&`, quotes and
  150-char names pushed through without a shell. Concurrency: parallel
  transfers plus a same-key race that must land clean versions with the
  latest byte-intact. Security: SSE-S3 (recorded provider gap on MinIO
  without KMS), presign TTL enforcement (a 2-second grant served before
  expiry, refused after), and a secrets-leak probe asserting the auth
  token never appears in any CLI surface. Crash safety: a hard process
  kill mid-64 MiB upload must leave NO visible object, and the retry
  must be sha-identical. Through the GUI face: the per-file conflict
  matrix (skip keeps exactly 1 version with original bytes; rename lands
  the new bytes beside the untouched original), cancel-mid-transfer on a
  throttled job (no partial object lands; retry clean), the L2
  typed-identity Empty-bucket gate, and the WinSCP-style editor
  auto-upload round-trip verified byte-level back through the CLI.
- **Verification is now a release-pipeline phase: no report, no release.**
  `node scripts/verify.mjs --release <tag>` stamps the tag into the
  binaries exactly as the release workflow does (`main.version=<tag>`),
  runs the full matrix from a fresh build — refusing
  `--only`/`--quick`/`--no-build`/`--skip-gui` — and commits the evidence
  to `docs/verification/<tag>/<os>-<arch>/` (`REPORT.md` with the build,
  OS and full certificate, plus the machine-readable `verification.json`)
  behind a generated index, linked from the README. The release job then
  checks out the tagged tree, refuses to publish unless the tag carries a
  green full-matrix report, and links the report at the top of the release
  notes. Step-by-step: CONTRIBUTING.md, "Cutting a release".
- **Release verification: one command proves every critical job.**
  `scripts/verify.mjs` (or `npm run verify`) is the pre-release gate:
  an 85-row verification matrix across three faces, run against the live
  engine containers (MinIO S3, SFTP, FTP, WebDAV) before any version
  ships. The CLI battery asserts exit codes and output — sources and
  their lifecycle, versioned buckets, transfers (including the
  `--dry-run`/`--no-clobber`/`--force` contracts and versioned
  migration `cp/mv --versions`), gated deletion, the version-safety
  ladder, bucket admin (config deletes that must never harm the bucket,
  lifecycle round-trip), object lock, presign, sync, doctor, storage
  class, find with size/time filters, completion). The cross-engine
  battery covers FTP/SFTP/WebDAV round-trips, cross-engine moves and
  encrypted source export/import; fault injection verifies latency, RST
  and blackhole behavior through the fault proxy. The GUI battery
  drives the real Wails v3 server stack in a real browser — DnD
  uploads/downloads, the versions dialog, overwrite-conflict and
  Delete-Window cancellation contracts, doctor, the admin dialog,
  profile-file round-trip, the transfer manager, dual pane and filter —
  and verifies every result *back through the CLI*, so a GUI green
  means bytes actually moved. A full run folds both complete harnesses
  in as sweep rows (gui-visual 609 checks, gui-v3live 142 checks).
  Category runs (`--only cli|s3|cross|resilience|meta|gui|sweeps`,
  or `npm run verify:cli` / `verify:gui`) verify one focus area;
  rows whose engines are down — or that hit a recorded provider API
  gap — are SKIPped with the reason, never passed. Exit 0 = verified;
  the machine-readable verification report lands in
  `testartifacts/verification/verification.json`. `docs/VERIFICATION.md`
  is the human-readable matrix: action × data source × tested scenario
  × CLI/GUI face × OS.
- **Context menus can no longer overflow the window.** Menu placement now
  clamps by the menu's *real* rendered width instead of a hardcoded 220px
  guess: wide menus (long localized labels + keyboard hints) opened near
  the right edge used to spill past it; both axes now clamp with a 0px
  floor, so a menu fired from the bottom-right corner of a small window
  stays fully on-screen.
- **Regression probes for the flash-free and clamp contracts.** The
  visual harness now asserts: the saved theme resolves *pre-paint* (a
  `setAttribute` hook flags any theme decision that lands after the body
  parses — the FOUC regression signal); a popout parked at the right edge
  is re-clamped when the app window shrinks under it (≥60px reachable,
  title bar always grabbable); a context menu opened in the extreme
  corner stays inside the viewport; and the modal × close button is a
  padded hit target, not a 1-character sliver. The CLI side gained
  usage-error cases (missing flag argument, invalid flag value) and a
  stderr label test: invocation mistakes must read `usage error:` and
  point at `--help` — never `unexpected:`.

- **Small windows are a first-class scenario.** A new visual-harness sweep
  resizes the app window to 1024×640 and 820×560 and reopens every flagship
  surface — Settings, the keyboard shortcuts, File transfers — proving
  dialogs clamp to the viewport (the 880px tier included), popouts re-clamp
  their remembered placement, and no button or scroll ever strands
  off-screen.

- **Running tasks now really monitors everything.** Four long operations
  that ran silently are registered tasks with progress and a **Cancel**
  button: deleting a bucket (with all its contents — no fixed time
  limit), batch deletes on remote sources (SFTP/FTP/WebDAV — checked
  between paths), pane-to-pane compares (the ↔ walk over both sides,
  reporting the row count), and doctor runs (a full diagnosis dials the
  bucket a dozen times; single-check reruns are tracked too). Canceled
  mid-flight, the doctor stamps the run instead of only its checks, and
  a compare dies with the walk. New verbs — ↔ *Comparing*, ⚖
  *Diagnosing* — are localized in all 15 languages.
- **New file — the WinSCP flow for objects.** Shift+F4, the 📄+ toolbar
  button or *New file…* in the context menu opens a name + type dialog
  (a dozen common extensions, or none) with a live preview of the exact
  object name. The empty object is created first — the name is composed
  server-side and an extension already in the name is never doubled —
  and the created key is returned straight to the editor handoff (the
  OS "Open with" chooser by default, respecting Settings → Editing).
  Cancelling the picker, or having no app at all, still leaves the
  created empty file behind; nothing is lost. Works on S3 buckets and
  on remote sources (creation only), and is tracked as a 📄 Creating
  task in the Running-tasks window like every other operation.
- **Clicking any app window brings the whole window group forward.** The
  main window and the native popouts now behave as one stack: focus any
  of them — click, Alt-Tab, a View menu reopen — and every app window is
  raised above other applications, the clicked one on top and the rest
  keeping their relative stacking (remembered across raises, so repeated
  clicks never shuffle the popouts). Siblings are lifted with
  `SetWindowPos(SWP_NOACTIVATE)`, so nothing steals the focus from the
  window you clicked; minimized windows stay minimized and rejoin on
  their next focus. In-page popouts (server/browser mode) are unaffected.
- **Help → Doctor now opens a source picker**: a small window listing every
  S3 data source — bucket-scoped sources carry their bucket, account-wide
  ones show "all buckets of this key" — so you choose exactly what to
  analyze before the diagnosis window opens. Picking a source switches the
  engine's view source to it, then runs the same Doctor as always. The
  right-click **Doctor…** on a source/bucket node is untouched and still
  goes straight to the window; the picker is only the Help menu's front
  door. Picker title/hint/no-sources strings are localized in all 15
  languages.
- **Help → License window + a single source of license identity.** The
  app's license identity (product, holder, year, license name/version,
  text and repo URLs) now lives in one place — `frontend/js/license.js` —
  and both the About box and the new Help → License popout read from it.
  The License window summarizes the PolyForm Internal Use 1.0.0 terms,
  links the canonical text, and points third-party attribution at the
  NOTICE file and release SBOM (summary, not a copy — nothing to drift).
  The About box's license line was also corrected to the exact LICENSE
  wording ("PolyForm Internal Use License 1.0.0 — Copyright (c) 2026
  MikkoP88").
- **User guide accuracy + coverage pass.** The File transfers tab's
  conflicts entry no longer claims "every transfer asks for a conflict
  policy" — it now matches the real behavior (live destination pre-check;
  clean destinations start immediately, only real collisions open the
  per-file dialog). New entries cover the two-way Explorer clipboard,
  drag-out download, transfer-window auto open/close and history toggle,
  the Running tasks monitor, floating popout windows, in-place file
  editing, the Settings dialog (theme, languages, engine tuning, Secure
  Storage) and the Delete Window (the old one-line "safety ladder" note).
  The Doctor entry documents the picker. The keyboard map gains the
  missing Ctrl+Shift+F (deep find) row.
- **Engine tuning is now in Settings — the six budgets the engine ran on
  hardcoded defaults for are first-class, honored-live settings.** A new
  **Settings → Network** page carries **Listing timeout** (how long a
  listing, stat, presign or source test may wait on a silent source
  before it is cut off — every page of data resets the clock; default
  30 s), **Compare timeout** (the budget for one deep pane-to-pane
  compare walk, default 5 min) and **S3 retry attempts** (SDK retries per
  request, default 3). Under **Settings → File transfers** a new
  **Transfer engine** group carries **Multipart part size** and **Parts
  in flight** (both Auto = SDK defaults, 5 MiB / 5) and the **Stall
  threshold** (how long a transfer may sit without byte progress before
  the row flags Stalled, default 10 s). Everything persists Go-side in
  `appsettings.json` (config folder, 0600, torn file → defaults), is
  read per call so changes apply without a restart (the S3 client cache
  is dropped on change), clamps into documented ranges, round-trips
  through new GetTuning/SetTuning bindings, logs a `settings` line on
  every change, resets through *Reset to defaults*, and is honored at
  every consumer: the listing-stream watchdog, the quick-op contexts
  (listings, stat, presign, source tests), the compare walk, the SDK
  retryer, every transfer construction site (uploads, downloads, editor
  open/save, cross-engine transfers) and the per-job stall capture
  (running jobs keep the threshold they started with; new jobs pick up
  the change). Verified live: the walk sets the listing timeout to 10 s
  through the real Settings UI, watches it survive a server restart, and
  the fault-lab blackhole step then times out at exactly that 10 s
  watchdog — duration spelled out in the error — before defaults are
  restored.

### Changed

- **Copy is now a true two-part action: nothing happens until Paste.**
  Ctrl+C on remote/S3 rows used to start a real staging download into a
  temp clipboard directory before anything was pasted; it now stages
  references only — no visible transfer, nothing downloads — and the
  actual transfer or server-side copy runs at Ctrl+V in the destination.
  Explorer owns its paste (there is no second step we control there), so
  that one direction is served at copy time: small selections
  (≤ 500 files / 256 MB) are mirrored onto the OS clipboard through a
  staging download flagged hidden, and a cut never mirrors — an Explorer
  paste of a cut would move. Copying real local files in the dual-pane
  local view still hands them to the OS clipboard directly, and the
  Explorer → app direction is unchanged: Ctrl+C in File Explorer, Ctrl+V
  in the app uploads the files. The two-part sweep also covers the
  internal staging the drag-out download needs: those scratch transfers
  are now flagged hidden and are invisible everywhere — never a row in
  File transfers, never in Running tasks, never on the status-bar
  badges, never an auto-opened window. Only real transfers show: data
  source ↔ data source, and local machine ↔ app (drag, copy & paste).
- **File transfers rows stack their route vertically.** The From and To
  locations now render one above the other (each leg truncating its own
  path) instead of sharing a line behind a "→" arrow — long paths no
  longer squeeze each other out of view, and the arrow is gone.
- **Running tasks rows always name their action type.** Every task row —
  and the status-bar tasks badge — now leads with a localized verb
  (Deleting, Purging, Emptying, Converting, Searching, Listing, and the
  transfer verbs Uploading/Downloading/Copying/Moving); the raw kind in
  brackets survives only as an unknown-kind fallback. The six new task
  verbs are localized in all 15 languages.
- **Windows no longer change size with their content.** Every standard
  dialog and popout now sits on one of three fixed width tiers — 560 px
  for base dialogs, 720 px for the wide views (deep search, versions,
  the keyboard map), 880 px for Settings, Admin and conflict resolution,
  each clamped to the viewport — so switching a Settings category,
  flattening the tree with the settings search, or walking Admin
  sub-pages keeps the window pixel-stable while the content scrolls
  inside instead of re-widthing it mid-interaction. The growing lists
  got the same treatment: the versions diff, version batches, the Delete
  window's breakdown and imported credentials render in fixed-height
  scroll lists rather than stretching the window taller as rows stream
  in, and the Admin guide spans its wider tier. A dead-styling sweep
  across the frontend turned up no dead CSS classes — only three
  redundant JS `export` keywords (removed).
- **The right-click menu of an S3 data source no longer carries "Open
  buckets view".** Clicking the source already opens its content (buckets
  for account-wide sources, the bucket's objects for bucket-scoped ones),
  so the extra navigation entry was redundant noise; every source type
  behaves the same way now.
- **Running tasks rows now carry the whole picture: phase, current item,
  live speed, ETA and every finish stamp.** Every long-running task —
  copy, move, delete, permanent/keep-current purge, purge-all,
  empty-bucket, convert, search and listing — now feeds live progress
  from its producer, and the row renders all of it: a "Counting" chip
  over an indeterminate shimmer bar while the full object list is
  walked, "d / t" counts with the percentage once the total is known,
  a current-item line naming the key in flight, live speed ("@ 4.2/s")
  with a remaining-time estimate, and on finish the elapsed lifetime
  ("in 10s") in place of the ETA. Failures are classified — a timeout
  gets a critical "Timed out" chip — and merged transfer rows pass
  their byte-speed, phase, current file and stalled flag through
  untouched. The status-bar badge shows the first running task's live
  count — "purge 10/40 (25%)" — without opening the window.

- **File transfers rows now carry the whole picture: action + name, route,
  current item, speed, ETA and every state.** Every job row is titled by
  what it does and what it works on — "↑ Uploading video-final.mp4 +2",
  verb plus the primary source item plus how many more ride the job — and
  when two visible jobs answer to the same title the second gets a
  "(2)" suffix so they can never be confused. Under the title a route
  line spells out From: and To: (local path or s3://bucket/prefix on one
  side, the destination on the other), and while several files ride one
  job a current-file line always names what is moving right now — "File
  2 / 3 · video-take-7.mp4 · 22.0 MB / 75.0 MB (29%)" — so the row can
  never sit on a stale name for minutes. The meta line keeps counts and
  bytes and now shows the live speed and a remaining-time estimate while
  running, and the lifetime average ("in 43s") once finished. State is
  chipped instead of implied: the status itself, a "Cleaning up" phase
  chip while a move deletes its sources, a "Stalled" warning chip when a
  known-size file stops making byte progress for over 10 seconds, a
  critical "Timed out" chip when the failure was a timeout, and an
  explicit "Server-side copy" chip (with an indeterminate shimmer bar)
  for in-flight S3-to-S3 copies that legitimately carry no byte counts.
  The status-bar badge follows suit — "⇅ Uploading video-final.mp4 —
  31% @ 8.0 MB/s" — and the Running tasks window lists transfer jobs
  under their name with the "+N" item count.

### Fixed

- **Help → License opened an empty window in the desktop app.** The
  native-popout dispatcher had no `license` case, so the OS window the
  menu opened rendered a dead "Unknown popout" page instead of the
  license view (the DOM fallback the tests exercise worked, which is
  why it slipped through). The dispatcher routes `license` again, and
  battery row GUI-52 loads the popout URL itself — the exact payload a
  native window loads — so the gap cannot reopen.

- **macOS release builds no longer die at Gatekeeper as “damaged”.**
  The darwin dmg shipped completely unsigned: Sequoia shows
  “‘S3 Bucket Browser.app’ is damaged and can’t be opened” for any
  downloaded copy it cannot assess, and on Apple Silicon an unsigned
  binary is killed outright. The release pipeline now signs the .app
  bundle before packaging — ad-hoc when no certificate is configured
  (launchable after the one-time `xattr -dr com.apple.quarantine` step
  README documents; Developer ID + hardened runtime + notarization +
  stapling when the `MACOS_CERT_B64` / `APPLE_*` secrets are set) — and
  a configured certificate that fails any stage fails the release
  (`scripts/sign-macos.sh`).

- **`bucket pab delete` can no longer destroy the bucket.** Found by the
  verification probes and confirmed at the wire level: some S3-compatible
  servers (observed on MinIO RELEASE.2025-09-07) mis-handle
  `DELETE /bucket?publicAccessBlock` and delete the entire bucket — with
  all its objects — while reporting success. s3b sent the documented
  request; the server is broken. DeletePAB now pre-checks the PAB
  capability with a GetPublicAccessBlock call and refuses with the
  standard "public access block is not supported by this
  provider/endpoint" classification when the server cannot serve it, so a
  broken server code path is never handed the bucket. Verification row
  CLI-S3-24 pins this defense: after every bucket-config delete the
  bucket must still stat.
- **No more theme flash on launch.** The palettes live on the
  `data-theme` attribute, which the app set only after `main.js` booted —
  every start painted one frame with unset colors (white flash for dark
  users, worst on cold start). A tiny pre-paint script in `<head>` now
  resolves the saved choice — or the system preference — before the
  first paint, in sync with `initTheme()`.
- **Dialog close buttons are a real hit target.** The × on every modal
  and floating window was a bare 16px glyph — the smallest clickable
  thing in the app. It now carries padding (negative margins keep the
  layout pixel-identical) and lights up on hover.
- **CLI typos are usage errors, not crashes.** Cobra's own invocation
  failures — unknown command, unknown flag, wrong argument count — were
  reported as `unexpected:` with exit code 3 (the app-failure class).
  They now read `usage error:` with a `run 's3b --help'` hint and exit 2,
  matching every other usage classification in the tree.
- **Version-preserving paste of a folder no longer flattens it.** The
  versioned copy path (the default whenever the destination bucket has
  versioning enabled) mapped every source key of a pasted folder straight
  under the destination prefix — pasting `docs/` into `target/` produced
  `target/a.txt` instead of `target/docs/a.txt`, silently re-rooting the
  whole subtree. The destination mapping (`vcopyDstKey`) now mirrors the
  plain copy path exactly: exact objects land under their base name,
  folder selections re-root their subtree under the folder's own name
  (nested picks keep their leaf), and the folder's own marker timeline is
  recreated in trailing-slash form so recreated versions don't surface as
  a plain file beside the folder. Pinned by a six-case unit table
  covering the folder, bucket-root, nested, marker, exact and
  same-place cases.
- **A newly created folder now appears in the sidebar tree.** The S3
  side of the Data sources tree never re-listed after the initial lazy
  load, so a folder made with New folder (or deleted/renamed anywhere)
  only ever showed up in the grid until the node was collapsed and
  re-expanded — the remote side had this fix all along. Every completed
  bucket listing now feeds the view's tree node (`updateObjectsDir`,
  the S3 twin of the remote path's `updateRemoteDir`), with
  bucket-scoped sources resolving the source node itself as the bucket
  root, exactly like reveal() does. Pinned by a live-walk step that
  creates a folder and asserts it in the grid AND the tree.
- **Folder creation is a tracked task.** New folder (S3 and remote
  alike) registered no Running-tasks row, so the operation was
  invisible in the everything-monitor and uncancellable. CreateFolder,
  SourceCreateFolder and RemoteMkdir now register a `mkdir` task —
  labeled `s3://bucket/prefix/name/` or `source:/dir` — with the same
  finish semantics as every other kind, and the monitor leads the row
  with a proper "+ Creating" verb (localized in all 15 languages)
  instead of the raw-kind fallback.
- **File transfers and Running tasks hold their width.** The two
  auto-height windows drifted sideways: the height-fit loop re-reported
  the window's own width on every resize (each pass picked up a
  pixel-rounding delta and fed it back in), the session memory then
  pinned whatever width it had drifted to, and a user drag could
  stretch the window on top of both. The fit now resizes height only —
  "keep the current width" is an explicit resize argument the backend
  honors — the 490 px footprint is pinned in CSS, only placement is
  remembered between reopens, and the native windows are
  non-user-resizable outright (the app's own height fits still resize
  them). The in-page fallback's grip is height-only to match.
- **A dialog no longer widens the moment it shows an error.** Raw backend
  errors are one long line of unbreakable tokens (URLs, host ids, request
  ids), and every dialog is a content-sized box: the first error used to
  shove the Add/Edit data source window — and every dialog with a status
  line — out to its maximum width and hold it there. All eleven dialog
  status strips now share a proper `.dlg-status` style that wraps anywhere
  (the inline `min-height` styles they carried before matched no CSS rule
  at all), the source editor is width-pinned like the Delete window so its
  form never reflows when a Test result or error appears, and the same
  wrap treatment went to the remaining raw-error surfaces: the main
  panel's error subtitle, toasts, the directory browser's status line and
  the credential importer's per-source test result. While there: clicking
  a modal's backdrop now actually closes it (the listener sat on the
  dialog while testing for the backdrop as the event target — an event
  that can never bubble that way, so the handler had never fired), the
  source editor's Test button disables itself while a dial is in flight
  so two results can't race onto the status line, and a dead
  self-assignment left over in the grid's sort cycle is gone. The
  backdrop handler also removes itself on close — it sits on the modal
  root every dialog shares, so before the fix each opened dialog left
  another stale close behind and one backdrop press fired them all,
  each wiping whatever modal was open at the time. Pinned by a live
  walk that grew two steps (error-width lock, backdrop close) and had
  its engine waits made virtualization-proof — the grid renders only
  the scrolled window plus overscan, so a freshly made folder can exist
  yet sit outside the DOM; the walk now filters the view onto its
  target first and keeps the side pane scrolled while it waits, and a
  flight recorder dumps the last UI events and backend log lines into
  any step that fails.

- **A slow or dead source can no longer pass an empty panel off as an
  empty folder.** Every navigation into data that has not arrived yet
  now shows the truth: an in-flight state — spinner, "Loading…" and
  animated placeholder rows — for the whole time the listing is on the
  wire, on S3 buckets, S3 object views, remote-engine folders and the
  local pane alike. When the data lands, rows replace the skeleton in
  one frame; when a folder really is empty, it says so with a proper
  translated subtitle instead of a raw literal. A load that fails
  renders a classified error state — timeouts ("The data source took
  too long to respond", detected from the error shape) read
  differently from other failures ("Could not load this view"), the
  raw message sits under the title, and a **↻ Retry** button repeats
  the exact navigation in one click. The same treatment covers the
  Versions window (loading text while fetching, retry on failure).
  Races are guarded on both panels: a response landing after a newer
  navigation started is dropped, so a slow directory can never clobber
  the one the user actually opened; silent background refreshes keep
  the current rows on transient errors but the first failure of a
  streak says so — the rows on screen may be out of date — instead of
  letting stale rows masquerade as live ones. UI strings in all 15
  languages.

- **CLI `--timeout` is now one budget for the whole call, not one per
  retry attempt.** The deadline used to live on
  http.Client.Timeout, whose "Client.Timeout exceeded" error the AWS
  SDK's retryer classifies as retryable — so each of the 3 attempts
  paid the full budget again, plus backoff. Against a dead endpoint
  `--timeout 3s` took ~12s, and the 5-minute default meant a
  ~15-minute hang on a simple `ls`. The budget now rides each
  request's context and a budget-expired error is marked terminal for
  the retryer (fast transient failures — connection reset, 503 — keep
  their retries), covers body streaming as well as headers, and never
  extends a caller deadline that is already sooner. GUI paths run
  deadline-less by design and are bounded by their own contexts; a
  new 30s no-progress watchdog bounds server-side listing streams —
  a stream that produces nothing for 30s (a dead endpoint) is cut
  off and reported as a timeout, while a slow-but-alive source
  resets the timer with every page it delivers.

- **Tasks no longer jump from 0% to 100%.** Task progress updated the
  done-count silently — update events only fired on registry lifecycle
  changes — so a task that ran for 15+ seconds sat at 0% the whole way
  and snapped straight to done. progress() now emits (throttled to
  100 ms), a per-task heartbeat re-emits every 250 ms regardless, and
  speed (EMA) and ETA tick in real time. The long producers were also
  restructured count-then-act: folder copies and moves plan their full
  src→dst pair list first and then copy pair by pair with live counts
  (a mid-folder error no longer aborts the folder — the rest are
  attempted and the errors collected into the result), bulk deletes
  and version purges feed per-key progress from new engine callbacks
  (DeleteKeysProg, PurgeProg, EmptyBucketVersionsProg), bucket
  emptying reports per-batch deletions, searches and listings report
  live match and entry counts, and a move's source-delete pass is
  chipped "Cleaning up" so it reads as work, not a hang.

- **Transfers no longer jump from 0% to 100%.** Progress events were
  only emitted at file boundaries, so a single file large enough to take
  15+ seconds produced exactly two updates — 0 bytes at the start and
  "done" at the end — with the bar frozen at 0% the entire time in
  between. The engine now emits on every progress callback (throttled to
  100 ms) and a per-job heartbeat re-emits every 250 ms regardless, so
  the percentage, the EMA-smoothed speed and the ETA all tick in real
  time even when the underlying stream reports nothing. Timeouts are
  additionally classified (timeout, deadline exceeded, context deadline)
  and surface as an explicit error kind on the job.

- **Settings rebuilt as a Visual Studio 2026-style two-pane dialog with
  search.** The single scrolling list of sections is gone: a category
  navigation (Appearance, View, Refresh, Editing, Deleting, Logging,
  Security, File transfers) now sits on the left with the matching page
  on the right, and a search box above it filters every setting across
  all categories at once — matches are shown grouped under their
  category headers, the nav carries per-category match counts, empty
  categories dim, and an honest "no settings match" note appears when
  nothing hits. Searching also matches descriptions and category names,
  and clearing it (or picking a category) restores the paged view. The
  per-setting rows keep the label + description + control family and
  apply immediately, exactly as before.

- **File transfers and Running tasks now size like the Windows
  file-transfer window: 490x300 default and minimum, height tracking
  the content up to 740px.** Both monitoring windows previously opened
  at a fixed 720px wide and, natively, a fixed height — regardless of
  how little or how much they contained. They now open at the
  file-transfer footprint and their height follows the content between
  300 and 740px, expanding and shrinking as jobs come and go; beyond
  740px the window stays put and the content scrolls inside. A manual
  height resize takes over for the window's life and can never exceed
  the space the content fills (the 300px minimum still applies);
  reopening the window starts the tracking fresh. In the desktop app
  the native popout windows carry real resize bounds (490x300 minimum,
  740 maximum height) and drive their own height through a new
  ResizePopout binding; in-page they get the same contract through CSS
  and the resize grip.

### Added

- **A fault-injection lab in the live test suites — bad, slow and dead
  connections are now tested, not assumed.** `scripts/faultproxy.mjs`
  is a TCP proxy in front of the real MinIO whose fault mode can be
  switched live over a control port (so the SDK's keep-alive pool
  cannot dodge it): **direct** passthrough, **latency** (every TCP
  chunk held N ms), **throttle** (bytes paced at N B/s), **reset**
  (connections killed with data pending → RST) and **blackhole**
  (accepted, never answered — a dead endpoint). The CLI suite runs
  copy/list round-trips through every mode and asserts the timeout
  budget actually holds (`--timeout 3s` must fail in ~3s with a
  clean, deadline-shaped error, not 12s of silent retries); the GUI
  live walk points a whole source at the proxy and asserts the UX
  contract end to end — skeleton rows while chunks crawl in, complete
  rows landing anyway, a classified error with a working Retry after
  mid-stream RSTs, and the stream-watchdog timeout state (plus
  recovery) against a black hole. Stale proxies from failed runs are
  cleared by port (Windows job-control kills don't reach node), and
  the visual harness gained a shimmed loading-states step covering
  skeleton, error+retry, the navigate-away race and the side pane.

- **Theme can follow the OS (Auto (system))** — the third theme choice
  alongside Light and Dark, after the VS 2026 "use system setting". The
  app now tracks the system light/dark switch live while Auto is
  selected; never-made-a-choice boots behave (and display) as Auto.
- **Settings → View → "Remember popout window positions"** (default on):
  turning it off stops popout geometry being read or written — every
  popout opens at its default spot instead of where it was last left.
- **Settings → View → "Sync local pane with remote"**: the synced-browsing
  knob previously reachable only through the small checkbox in the local
  pane header is now a first-class setting; both panels browse the same
  folders side by side with the base pair captured on enable.

### Fixed

- **Hidden (delete-marked) rows no longer flash on auto refresh.** With
  Settings → View → *Show hidden (delete-marked) objects* enabled, every
  silent refresh swapped the grid to the fresh listing — which by
  definition cannot contain delete-marked children — so the ghost rows
  vanished at that paint and only reappeared a moment later, once the
  version-summary pass re-appended them: a vanish/reappear flash on every
  tick, while normal rows (which never leave the grid) stayed steady. The
  folder's ghost rows are now carried across the listing swap so the swap
  paints with them in place, and the summary that follows reconciles
  instead of rebuilding — retiring ghosts that were restored or purged out
  of history and adding new delete markers, with no flash in either
  direction. A side effect worth noting: a selection on a ghost row now
  survives auto refresh too (the swap no longer drops the row out from
  under it). The visual harness pins the exact flash window — a delayed
  version summary holds the reply past the silent swap and asserts the
  ghost stays rendered, renders exactly once after the summary lands, and
  is retired by the reconcile once purged.
- **Multi-selection delete on versioned buckets: the destructive modes
  could never run.** The delete window's "Delete all but the current
  version" and "Delete permanently" modes derive their `force` flag from
  the delete preview's *object* count — but the backend gates them on the
  *version* count, and hundreds of versions can hide behind a handful of
  current objects (directories especially). Every such delete above the
  50-version threshold failed with "N version(s) selected — typed
  confirmation (force) required" whether the Require-typing setting was
  on (the typed word was demanded, given, and then ignored) or off (no
  path to force existed at all). The confirmed delete window — explicit
  destructive-mode choice, amber consequence note, typed word whenever
  the setting is on, and never auto-confirmable — now IS the force
  contract for those two modes, matching the purge/empty-bucket flows;
  the plain marker mode keeps mirroring the preview's object count,
  which the backend re-counts identically. The visual harness pins all
  four paths (marker, keep-current, permanent, Shift+Del directory) to
  their exact force flags.

### Changed

- **Show/Hide history moved to the bottom bar, styled like Clear.** In
  both the File transfers and the Running tasks windows the history
  toggle left the top of the list for the left end of the window's
  bottom bar — the same button styling as the Clear button sitting
  opposite it on the right (the compact small-text button that used to
  float above the list is gone), with the hidden-row count beside it.

## [1.1.0-beta.17] — 2026-09-21

### Added

- **Round 3 of extreme-level rows: the gate grew from 85 to 94.** Data-protection toggles — versioning suspend/resume (a suspended write lands as the null version while the timeline survives) plus public access block arm/disarm, with the MinIO PAB provider gap recorded (the aws CLI agrees) (CLI-S3-36); legacy store migration — hand-written `profiles.json` seeds both profiles as data sources, the keyring takes every secret and the session token (none remain in the file), and the token profile's invalid-token refusal carries the synthetic token in the signature, proving it came from the keyring (CLI-M-06). Through the GUI face: the selection-mechanics ladder — plain anchor, shift-range, ctrl-toggle, Ctrl+I complement, Ctrl+A, every count from the visible selection bar (GUI-28); the multi-delete ladder — marker delete hides all four selected objects while every full timeline survives, then Shift+Del destroys versions entirely (GUI-29); the versions dialog's A/B compare diff plus per-version Destroy re-read by the CLI (GUI-30); versioned copy carrying the ENTIRE timeline S3→S3 in a background job (GUI-31); Admin-panel versioning and tag mutations verified out-of-band (GUI-32); L2 execution — Empty bucket destroys EVERY version and keeps the bucket (GUI-33); and cancel mid-batch — finished files stay byte-identical, the canceled one never lands (GUI-34).

### Fixed

- **One-pass data-source migration can no longer collide on the same nanosecond ID.** Windows' coarse clock could mint two migrated sources with the same ID, and `sources/<id>/secret`/`token` share one keyring slot — the second silently overwrote the first's credentials. Source IDs now carry a crypto-random suffix, with collision re-rolls (`pkg/core/profile/source.go`).

## [1.1.0-beta.14] — 2026-09-18

### Added

- **Transfers window opens itself — and closes itself, carefully.**
  With Settings → File transfers → *Transfer window auto open/close*
  (on by default) the File transfers window floats the moment a
  transfer starts, so progress is always visible without a click. The
  automatic close is guarded four ways: only a window the app opened
  itself closes (any manual open — View menu, status bar — locks it
  open), a failed or canceled job keeps it on screen for the
  post-mortem (per-file failures count as not-successful too), and with
  simultaneous transfers only the **last** running job's completion
  closes it. A clean, all-done batch is the only thing that does.
- **Status-bar ⚙ indicator always shows the active-task count.** The
  ⚙ badge beside the transfers counter now always displays how many
  tasks are currently running (e.g. "⚙ 2 tasks — search 3/10"), not
  just when there are two or more. Clicking it opens — or focuses —
  the Running tasks window.
- **Quit guard warns about running tasks too.** The confirm dialog
  that already fires for running transfers and unsaved profile work
  now also fires when non-transfer tasks are running (deep searches,
  bulk deletes, version purges, bucket emptying, storage-class
  conversions, copies). It states the count and an example task;
  transient directory listings do not trigger it.
- **Popouts remember where you left them — for the session.** A
  floating window reopens at its last dragged position and last
  resized size, every time, until the app closes: native popout
  windows keep an in-process rect snapshot (nothing on disk), in-page
  popouts keep a session store that the next launch wipes. The
  *Popout windows open centered on* setting is a preference, not
  geometry, and survives.
- **Running tasks — one window watches (and kills) every action.**
  View → Running tasks opens a floating monitor modeled on the File
  transfers manager, but listing *everything* the app is doing: transfer
  jobs, deep searches, bulk deletes and version purges, bucket
  emptying, storage-class conversions and server-side copies — each
  with status, progress counts and a **Cancel** button. Anything that
  hangs or runs too long can be stopped; destructive tasks are
  count-then-act, so canceling during the counting phase destroys
  nothing. To make that meaningful, the bulk engine operations (delete
  selection and its version variants, `PurgeVersions`,
  empty-bucket, storage-class conversion, selection copy) now run as
  tracked, cancellable tasks instead of invisible 30-second-bounded
  calls — a batch that used to fail on a big prefix now runs to
  completion or is killed from the window. Deep searches appear under
  their token (the search window's own cancel and the task list agree
  on one ID); listing streams show only while running. New bindings
  `RunningTasks`/`CancelTask`/`ClearFinishedTasks`; UI strings in all
  15 languages.
- **Popout windows open on the app's display (multi-monitor support).**
  Native popout windows now open centered on the display that carries
  the app's main window — inside that display's work area, so the
  taskbar/dock never eats them and the popout follows the app onto
  whatever monitor it lives on (previously every popout centered on
  the app window's own rect). The original behavior stays available:
  Settings → View → *Popout windows open centered on* picks "The
  display the app is on" (default) or "The app window" — translated in
  all 15 languages. In-page popouts (harness, browser and server
  builds) are bounded by the app window either way and are unaffected.
- **Drag out of the window as real files — plain drag, nothing to hold.**
  Dragging a files-only selection (S3 objects or a remote/local listing)
  out of the app window onto Explorer, Finder or the desktop now hands
  the selection to the OS as a native drag that drops real files. The
  webview's own drag data cannot do this (WebView2 does not implement
  the Chromium DownloadURL drag-out the browser build rides), so the
  gesture is cancelled at `dragstart` and handed to Go: the selection is
  staged through the transfer engine into a temp dir and a native OLE
  drag floats delay-rendered file data — the cursor moves at once and a
  drop resolves once the bytes exist, with the same size/count envelope
  the OS clipboard mirror uses (500 files / 256 MB). Released back over
  the app's own window the gesture is routed through the internal
  move/copy funnel (Shift/Ctrl state honored) instead of re-importing
  the staged files. Selections that contain folders keep the in-app DOM
  drag (expand-vs-zip policy for dragged directories is deliberately
  deferred). Browser/server builds keep the loopback-URL drag-out.
- **Popout windows — monitoring views no longer block the app.** The
  Transfer manager, the User guide, Supported data sources, the F1
  keyboard-shortcut sheet and the connection Doctor now open as
  *floating non-modal windows* instead of dialogs: no grey-out mask,
  no focus trap — the view underneath stays fully interactive, so you
  can watch a transfer crawl or read the guide while keep working.
  Windows drag by their header, resize from the bottom-right grip,
  stack and raise like real windows (any press lifts a window above
  its siblings; Escape closes only the topmost one, and only while no
  dialog is open — confirmations still overlay everything), and each
  remembers where you left it (`s3b-popout-<id>`; wiped by Settings
  reset together with the rest). One instance per view: reopening
  focuses the floating window instead of stacking a duplicate. The
  in-app DOM popouts are bounded by the app window and re-clamp
  themselves when the window shrinks so they can never be stranded
  off-screen (see the Wails v3 entry under Changed — on desktop the
  same views can now also float as real OS windows).
- **File-log source filter.** Settings → Logging can now filter what is
  written to the log file by *source* — the third dimension the in-app
  log drawer already filters by (bucket and data-source names). The
  selector's options grow the same way the drawer's do: every source
  that appears on a log line is registered (`logsources.json`) and
  offered, together with the configured data sources, so a fresh source
  is selectable before it ever logs. Like the level and area filters it
  gates only the file — the on-screen log is untouched — and while a
  filter is set, lines with no source stay out, exactly like the
  drawer's rule. The line's source now also lands in `events.jsonl`
  (and thus in `s3b log` output), where it was previously dropped.
- **Transfers and tasks windows show what happened *since you looked*.**
  A freshly opened File transfers or Running tasks window keeps rows
  that finished before the open behind a *Show history* toggle (with a
  count of the hidden rows) — the window answers "what is running
  right now" instead of an all-day backlog, and one click brings the
  whole story back. The toggle is always there while anything finished
  exists: *Hide history* collapses every finished row on demand — the
  ones that finished inside the open view included — leaving just the
  live work. Translated in all 15 languages.

### Changed

- **"Clear finished" is now "Clear" — and clears only what you see.**
  Both windows' button lost the qualifier and gained scope: it removes
  the finished rows currently visible (every finished row once
  history is shown), while rows hidden as history — pre-open or
  collapsed with *Hide history* — and any
  running work are untouched. The backend bindings take an explicit
  id list (`ClearFinishedTransfers`/`ClearFinishedTasks`); a null
  list keeps the classic clear-all behavior for other callers.
- **The Transfers feature is now called "File transfers".** The View menu
  item, the manager window's title and the Settings section all carry the
  clearer name — translated in all 15 languages; the in-app guide (F1),
  the usage docs and the visual harness follow. A rename of the label
  only: same manager, same engine, same shortcuts.
- **Wails v2 → v3 (v3.0.0-beta.23) — same frontend, native popout
  windows.** The unmodified frontend now runs on the v3 runtime in the
  desktop webview or over plain HTTP: a small bridge restores the v2
  surface on top of v3's runtime, and `pkg/api` stays framework-free
  behind a desktop-shell seam (events, clipboard, native dialogs, quit,
  popout windows) installed once by the GUI. Monitoring views can now
  float as *real OS popout windows* (v3 multi-window): a native window
  per view, centered on the main window, per-id singleton with close
  bookkeeping — server/browser builds keep the in-app DOM popout, and
  the GUI workspace becomes session-only (ListSources serves the
  in-memory registry or an open `.s3bprofile` container). A `-tags
  server` build runs the exact production stack windowless over HTTP
  for browser-driven validation. Build tags follow v3: no desktop tag
  (the GUI is the default build; `production` strips devtools), `gtk3`
  for Ubuntu 24.04's webkit2gtk 4.1 — CI, release workflow and docs
  all updated.
- **The marker toggle now hides delete-marker versions everywhere.**
  Settings → View → *Show delete marker icons* (default off) already
  folded markers out of the grid's ⛔ badges and the Versions and
  Content Versions windows — but the Delete Marker window ignored it
  and always listed them. It now honors the toggle like every other
  marker surface: while off it fetches and lists nothing, showing a
  notice with an inline *Show delete markers* button that makes the
  same flip the View menu does (the grid re-badges live). The
  context-menu entry stays available — the window remains the
  undo-delete surface — and bucket admin version stats and pre-delete
  safety counts keep reporting true numbers either way.
- **Every destructive operation now confirms in the Delete Window.**
  Four flows bypassed Settings → Deleting → *Always use the delete
  window* and confirmed through bare prompts: bucket delete, the
  Versions window's per-version **Destroy**, and the admin panel's
  purge (noncurrent / delete markers) and force-empty tools. All of
  them ride the same `deleteWindow` base template as selection deletes
  now — one clean layout (target, counted stats, amber consequence
  line, typed partition) with bespoke stats per flow: bucket delete
  previews object / version / marker counts, version destroy shows the
  one version's size · class · date · id, purge its exact count,
  force-empty the full version statistics. The safety ladder is
  unchanged and uniform across window and classic modes: typing the
  bucket's own name (bucket delete, force-empty) or `purge` (>50) is an
  identity check that always applies, the typed `delete` partition
  follows *Require typing "delete"*, and none of these flows ever
  auto-confirm. With the window off they fall back to the classic
  typed/plain confirm carrying the same stakes text.
  `runDeleteWindow` moved to dialogs.js so every destructive flow
  shares one orchestrator; the usage-guide ladder documents the gates.

### Fixed

- **macOS builds demanded macOS 26 — "This version cannot be used with
  this version of macOS."** The darwin releases were linked on GitHub's
  macOS 26 runners without a pinned deployment target, and Xcode 26's
  clang defaults that target to the SDK version (26.0) — so both the
  Intel and Apple Silicon binaries carried `minos 26.0` and every Mac
  below macOS 26 refused to launch them (the `.app`'s Info.plist even
  claimed 10.13; the binary's Mach-O load command is what the OS
  enforces). The release and CI workflows now pin the deployment target
  to **macOS 12.0** — the oldest macOS the Go 1.26 runtime itself runs
  on, and exactly what the README has always promised — via
  `-mmacosx-version-min=12.0` in `CGO_CFLAGS`/`CGO_LDFLAGS` plus
  `MACOSX_DEPLOYMENT_TARGET` (the env var alone is not honored at link
  time), the Info.plist's `LSMinimumSystemVersion` is stamped from the
  same constant instead of a stale hardcoded 10.13, and a release gate
  runs `vtool` on every darwin artifact and fails the build if the
  binary's minimum is not 12.0 — so a future runner-image default change
  can never silently ship an OS-gated binary again. `make build-all`
  pins the same target for local darwin builds. Verified live on the
  macOS 26 runner: both arches now carry `LC_BUILD_VERSION minos 12.0
  sdk 26.5`, built clean; supported range is macOS 12 Monterey through
  26 Tahoe on Intel and Apple Silicon. Affected: every release up to and
  including v1.1.0-beta.13.
- **Drag out of the window killed the webview — nothing ever dropped.**
  The OLE `DoDragDrop` modal loop must run on the app's UI thread, the
  one that owns the windows and pumps their messages; the drag-out
  binding ran it on a plain worker goroutine, and inside the loop
  WebView2 hit an invalid-state call (`resyncWebviewRasterizationScale`)
  that took the whole GUI down mid-gesture — the drag cursor moved, the
  webview died, no drop could ever arrive. The gesture now marshals onto
  the main thread through a new desktop-shell hook
  (`InvokeMain` → Wails' `InvokeSyncWithError`), with a dedicated STA
  thread as the fallback when no desktop shell is installed
  (server/headless builds and unit tests). Proven live by a purpose-built
  rig (`scripts/drag-live.mjs` + `tools/dragprobe`, a real Win32 drop
  target driven by synthetic input against the real app): the gesture
  floats on the UI thread, negotiates DragEnter/DragOver with a copy
  effect on a real external target, returns `DRAGDROP_S_DROP` on
  release, and its delay-rendered CF_HDROP payload — pulled at
  DragLeave, byte-identical to the GetData a real target's Drop makes —
  hashes to exactly the staged objects. The rig also pinned a machine
  property worth recording: on this Windows build ole32 never dispatches
  the final `IDropTarget::Drop` for any injected (SendInput) release —
  verified against WebView2's own registered target — so that last
  dispatch is validated by the S_DROP return and the payload pull, and
  needs a hardware drag to observe directly.
- **Native popout windows showed a duplicate header and close icon.**
  A view floating as a real OS window (File transfers, Running tasks,
  Doctor, the guide, …) carried the OS title bar *and* the in-page
  header strip with a second ×. When a view renders as a native
  popout the in-page header is now hidden — one title, one close —
  and the view's content starts at the top edge.
- **Drag out of the window dropped nothing.** The OLE drag's two
  success codes (`DRAGDROP_S_DROP` / `DRAGDROP_S_CANCEL`) were
  swapped, so every mouse release read as a cancel and no drop ever
  delivered — the drag cursor moved, the files never arrived. The
  drop target's pre-release data probes could also block on staging
  while the button was still held. Both are fixed, the HRESULT
  values are pinned by a regression test, and dragging rows out
  drops real files again.
- **Finished jobs no longer read 0%.** Jobs with no byte totals
  (server-side copies) and tasks with no unit totals (a bulk delete
  whose counting phase was skipped) showed "Done — 0%": progress now
  falls back through the file/unit counts to a finished 100%, and
  error-status transfer jobs sort and badge as failures again.
- **Status-bar badge clicks reliably open their window.** A native
  popout that failed to materialize — or died without a close event —
  left the badge click doing nothing. Opens are now verified through
  a new `PopoutOpen` binding: a stale bookkeeping flag heals by
  focusing or reopening, and a genuinely broken native path falls
  back to the in-app popout for the session (with a notice) instead
  of failing silently.
- **"Require typing 'delete'" is now honored by every typed-`delete`
  guard.** Two flows demanded the typed word even with the setting
  disabled: the Versions window's per-version **Destroy** always opened
  the typed prompt, and with the delete window turned off the classic
  confirm ladder typed the word for L2-path and no-undo (remote/local)
  deletes. Both now read the same `s3b-del-typeconfirm` switch the
  delete window's typed partition reads (exported as `delTypedOn` from
  dialogs.js — one switch, every typed-`delete` site); with the setting
  off they fall back to the plain danger confirm carrying the same
  stakes text. Guards that type a *different* word are separate
  escalation mechanisms and keep applying regardless: the bucket's own
  name (bucket delete, force-empty, enabling object lock), `purge`
  (bulk noncurrent purge >50 versions) and `convert` (bulk storage-class
  conversion). Settings hint reworded in all 15 languages ("…to every
  delete confirmation", not just the window), and the usage-guide ladder
  now footnotes which gates the setting governs.
- **Dragging files from Windows File Explorer into the app did nothing.**
  Three independent defects, any one of them fatal: (1) the Wails
  `DisableWebViewDrop` option set WebView2 `AllowExternalDrop=false`,
  which rejects external drags outright, so the DOM drop event the
  Windows file-drop bridge depends on never fired; (2) the frontend
  subscribed with plain `EventsOn` instead of `runtime.OnFileDrop`, so
  the runtime's bridging listeners were never attached and the Go side
  never learned about a drop; (3) the handler expected a single
  `{x, y, paths}` object while Wails emits three positional arguments —
  `paths` was always empty and the handler returned silently. The
  visual harness masked (3) because its shim emitted exactly the wrong
  shape the app was written against. All three fixed: the webview drop
  stays enabled (the runtime's listeners `preventDefault` external file
  drags, so the webview never navigates to a dropped file), the drop
  registers through the official `OnFileDrop` API with the app's own
  hit-testing kept, external drags now highlight the grid/side-pane
  drop targets, and both harnesses exercise the real positional
  contract.
- **No orphan root crumb in the path bar.** With zero sources
  configured or none selected the breadcrumb drew the root bucket icon
  for a source that isn't there; the path bar is now empty in the
  onboarding state and whenever the session's view source matches no
  existing source, and the root crumb returns as soon as a source
  opens.
- **Log drawer and Settings controls read as one family.** The log
  drawer's Filter input had been stretched across the whole header row
  by a later cascade rule; it is back to the compact chip that sits
  with the toolbar's filter chips. Settings → Logging's multi-select
  pickers had kept the drawer's compact-chip metrics — they now take
  the settings-select metrics (13px text, select padding, 200–280px
  band) and a settings row's Browse button sizes to its content like
  every other button. Pinned by the visual harness as same-kind peer
  checks, not eyeballed screenshots.
- **Dead-code sweep.** A module-wide scan for unreferenced symbols
  found the codebase clean apart from two leftovers: an unused OLE
  constant in the drag-out plumbing and one i18n key (`modified`)
  defined in all 15 dictionaries but never referenced (the grid's
  date column uses `col.date`). Both removed; i18n parity now 258
  keys. Stale build binaries at the repo root (pre-release `s3b`,
  `s3b.exe`, `s3b.exe~`) cleaned from the working tree.

## [1.1.0-beta.13] — 2026-09-17

Copy/paste reliability release: copying files in Windows File Explorer and
pasting into the app now actually works. The app clipboard never expired,
so the first in-app Ctrl+C shadowed every later Explorer copy for the rest
of the session, and every Paste affordance was greyed out without an app
payload. Precedence is now **last copy wins**, arbitrated by the OS
clipboard sequence number: an Explorer copy outranks a stale app payload,
the app payload keeps precedence while the OS clipboard is untouched by
anything else, and a staging mirror aborts instead of clobbering a
clipboard the user changed mid-download. Validated by the 375-check visual
walk (three new steps: precedence, mirror abort, Settings toggle) and a
new Explorer-clipboard section of the live walk that rides the real OS
clipboard end to end (`Set-Clipboard -Path` ≡ Ctrl+C in File Explorer →
Ctrl+V upload, in-app copy mirror round-trip, last-copy-wins over the
stale app clipboard, and the Settings toggle off/on live — debris
permanently purged afterwards so the marker-count assertions stay
deterministic).

Startup reliability: a hard-killed session (power loss, crash, force
quit) could leave an orphaned WebView2 process tree holding the
browser-profile lockfile, after which every launch hung invisibly — a
live process in Task Manager, no window, no error, one more wedged
process per retry. The GUI now sweeps orphaned webview trees before
creating its own window and arms a startup watchdog so a hang that
still happens fails loudly instead of forever.

### Fixed

- **GUI startup could hang forever with no window.** If a previous
  session was hard-killed, its `msedgewebview2.exe` children could
  survive it and keep holding the WebView2 user-data-folder lockfile;
  every later launch then blocked inside WebView2 environment creation.
  On Windows the app now terminates orphaned webview trees (dead parent
  PID) before starting the window. Healthy trees with a live host are
  never touched, and PID reuse can only make the sweep skip, never
  overreach.

- **Explorer → app paste was a silent no-op.** `paste()` only consulted
  the OS clipboard when the app clipboard was empty, and the app
  clipboard never emptied — one in-app copy shadowed Explorer for the
  whole session. Paste now resolves the freshest payload first; a
  winning Explorer paste supersedes (clears) the app clipboard.
- **Paste affordances ignored the OS clipboard.** The toolbar command
  state and all thirteen context-menu Paste gates keyed off the app
  clipboard alone; with only Explorer files waiting they stayed greyed.
  They now light up whenever either clipboard can paste.
- **Silent no-op without a destination** — pasting with nothing open now
  says so ("Open a bucket or folder first"), and an empty clipboard says
  "Nothing to paste" instead of failing quietly.
- **Late staging mirror could clobber the user's clipboard.** A copy's
  staging download captured the clipboard sequence up front; if anything
  wrote the clipboard while it ran (the user copied elsewhere), the late
  mirror aborts — their clipboard wins.
- **`OpenClipboard` contention**: the native CF_HDROP read now retries
  briefly (10 × 20 ms) instead of failing when another process holds the
  clipboard.

### Added

- **Settings → Transfers → Explorer copy & paste** (on by default):
  disables the entire OS-clipboard bridge on locked-down machines — no
  reads, no mirror writes; in-app copy/paste semantics are untouched.
  Re-enabling keeps last-copy-wins semantics (no reload needed).
- New `OsClipboardState` binding exposing the clipboard sequence number
  and file availability to the frontend (the arbitration signal).
- **Startup watchdog (all platforms):** if the window hasn't appeared
  60 s after launch, the app gives up loudly instead of hanging
  silently — a message box on Windows, a stderr line on Linux/macOS,
  an `error` entry in the event log, then exit. New `pkg/guihealth`
  package, covered by unit tests with fake process tables plus an
  end-to-end test that spawns a real orphan named
  `msedgewebview2.exe` and watches the sweep reap it.

## [1.1.0-beta.12] — 2026-09-16

Security and presentation release: the opt-in Secure Storage mode for
multi-user hosts, a full dependency refresh, and a documentation
overhaul — a new usage guide, a rewritten README with a tracked
screenshot set, and comparison tables re-verified against live sources.
Validated by the 367-check visual walk, which now boots with a realistic
version string and a saved profile file in the status bar, waits for the
UI to settle before every capture (no dialog caught half-faded or toast
mid-show), and verifies each curated shot's subject is actually on
screen.

### Added

- **Secure Storage — opt-in hardening for multi-user hosts**
  (Settings → Security, documented in `docs/security.md`): the whole
  profile/data-source store becomes one AES-256-GCM envelope (`s3bsf1`)
  keyed by a random 32-byte master key held in the OS keyring, the temp
  workspaces for external editing, cross-source transfers and clipboard
  staging move from the shared system temp dir into the 0700 config dir
  and are wiped on every launch (crash leftovers never survive a
  restart), file logging turns itself off while the mode is on, and
  pre-signed URLs copied to the clipboard are auto-cleared after 60
  seconds. The toggle is global and off by default because it adds a
  keyring dependency and a small per-write cost; the store file itself
  carries the mode (self-describing envelope), so the CLI and the GUI can
  never disagree, and an encrypted store on a host without a keyring
  fails loudly with remediation instead of silently degrading to
  plaintext.
- **Usage guide** (`docs/usage.md`) — the long-form walkthrough with
  screenshots, aligned with the in-app guide (Help → User guide, F1) and
  linked from the README.
- **Tracked screenshot set** (`docs/screenshots/`) — 13 curated 1440×900
  captures straight from the visual harness (main view, sources, import,
  versions, delete window, dual-pane compare, admin panel, doctor,
  transfers, secure storage, deep search, dark theme, onboarding), all
  showing professional fixture names and verified subjects.

### Security

- **Editor workspace directory was world-readable on multi-user systems**
  (`os.TempDir()/s3b-edit` created `0755`): now `0700` with `0600` object
  files in every mode, not just under Secure Storage.

### Changed

- **Data-source icons redrawn from professional icon sets** — every type
  now uses a real-world metaphor embedded as inline SVG (license-free,
  dependency-free), all in one consistent style: Bootstrap Icons (MIT)
  filled glyphs — a bucket for S3, a PC display for local, the network
  drive for FTP/SFTP, the globe for WebDAV, a server rack for the
  unknown fallback. The secured variants are composites: the base glyph
  shrinks to 75% and a corner badge marks the transport (terminal = SSH
  for SFTP/SCP, padlock = TLS for FTPS/WebDAVS), knocked out of the base
  with an SVG mask so it stays readable on any background, theme, hover
  state, or source tint. Icons still render in `currentColor`, so the
  source's accent color keeps painting them. Attributed in `NOTICE` and
  `docs/security.md`.
- **Dependency refresh** — Wails v2.16.0, the aws-sdk-go-v2 family,
  `golang.org/x/{crypto,net,sys,term}`, cobra/pflag, go-keyring and the
  rest all updated to current; `go mod tidy` clean, full test suite and
  race detector green on the new graph (`NOTICE` is regenerated per
  release from `go.mod`, so license attribution follows automatically).
- **README rewritten and fact-checked** — a "Why s3b" highlights table
  (ease of use, one app for every source type including WebDAV, any-to-
  any migration, versioning, credential import, encrypted profiles,
  security, scale), hero screenshot and gallery, a documentation index,
  and long-standing gaps fixed: WebDAV/WebDAVs is now listed as a
  supported source everywhere it was missing, the transfer-throttle
  range matches the UI (256 kB/s … 10 MB/s), and `docs/comparison.md`
  was re-verified against live vendor pages and the GitHub API on
  2026-09-16 (S3 Browser 13.5.7, MSP360 Explorer pricing, the MinIO
  console repository's disappearance, new entrants brows3/BucketDock).

## [1.1.0-beta.11] — 2026-09-16

Data-source identity pass: a redrawn icon set, the accent color you
picked actually painting it, and the same icon leading the path bar.
Validated by the 351-check visual walk (three new checks: every source
row carries its SVG glyph, glyphs paint in the source's accent color,
the breadcrumb root carries the same colored glyph).

### Changed

- **Improved data-source icons — now in color.** All eight type glyphs
  redrawn (the S3 bucket gains a rim rib, SFTP/SCP a proper terminal
  window with title bar, FTP a folder flanked by opposing transfer
  arrows, tighter padlock badges on FTPS/WebDAVS, cleaner globe, drive
  and server glyphs) — still original inline SVGs, no external icon
  set. The accent color chosen in the source editor now actually
  paints them: sidebar rows carry their source's glyph in its own
  color, making the color-coded connections the README always promised.
- **Same icon on the path bar.** The breadcrumb's root crumb leads
  with the identical type glyph in the identical color as the sidebar
  row (both surfaces build on one shared helper, so they cannot drift
  apart), replacing the generic key/folder emoji. Works on every
  source kind — S3, SFTP/SCP, FTP/FTPS, WebDAV(S), local.

## [1.1.0-beta.10] — 2026-09-16

Marker-visibility patch: the Content Versions window honors its own
default. Covered by a strengthened A/B check in the 348-check visual
walk (markers-off now asserts the deleted child is absent, markers-on
that it is present).

### Fixed

- **Content Versions listed delete-marked children by default.** The
  window already gated the marker counts and the Markers… buttons
  behind the "show delete markers" setting, but the ghost rows for
  marker-deleted children (⛔ icon, "deleted" tag) rendered
  unconditionally — exactly the rows that setting promises to keep
  out of sight. Marker-deleted children now appear only with the
  marker setting on, the same toggle that reveals the grid's ⛔
  badges; live children are unaffected.

## [1.1.0-beta.9] — 2026-09-16

Provider-dialect patch: the Object lock windows and commands work on
Ceph-based providers. Unit-tested against both error dialects plus the
generic 404 fallback; `go test -race` green across all packages.

### Fixed

- **Object lock window on Ceph (Hetzner Object Storage, and other RGW
  fronts).** Reading lock state for an object that simply has no
  retention or legal hold errored out — AWS answers that ordinary
  empty state with `NoSuchObjectLockConfiguration`, but Ceph RGW with
  `ObjectLockConfigurationNotFoundError`, and only the AWS code was
  tolerated. Both dialects (and any other 404 variant that is not a
  plainly missing object/version/bucket) now read as "nothing set".
  The same normalization covers the bucket-level lock configuration:
  `s3b bucket lock` (status) and the admin Lock tab previously errored
  on every never-locked bucket — AWS's own bucket-level answer is the
  Ceph code — instead of showing the zero configuration they promised.

## [1.1.0-beta.8] — 2026-09-16

Consistency and trust cut: windows stop resizing as you flip their
controls, Upload collapses into the single Explorer-style command it
always should have been, context menus stop offering actions the
target cannot take, and the version windows default to the calm
view. Validated by a 348-check visual walk (thirteen new checks
covering the upload flyout, menu-gating negatives, dialog height
stability, marker visibility A/B and the first-import auto-open)
plus an 89-check live GUI walk against real MinIO and `go test -race`
green across all packages.

### Changed

- **Dialogs keep their size.** The Delete Window no longer grows and
  shrinks as you switch delete type: the consequence line's slot is
  pinned at open to the tallest possible note (measured per locale),
  so the radio switch swaps text without moving the buttons. The
  keyboard-map guide pins its height across all six tabs, and the
  bucket-admin panel keeps a stable body height while cycling tabs.
- **One Upload command.** The toolbar Upload button opens the native
  file picker directly, and every context menu carries a single
  **Upload ▸** flyout with exactly two entries — **Files… (Ctrl+U)**
  and **Folder…** — Explorer-style, flipping leftwards when it would
  otherwise leave the screen. The old pair of separate menu rows is
  gone from every menu on every source.
- **Menus show only what the target supports.** A deep sweep across
  every target and source type: **Versions**, **Content versions**
  and **Delete marker** need a versioning-enabled bucket and a
  single selection (suspended or unversioned buckets lose them),
  **Object lock** appears only on lock-enabled S3 buckets for file
  selections, **Find in this folder** is offered only on a single
  folder, and files never grow folder-only entries.
- **Content Versions: one line, not four cards.** The folder-level
  overview drops its stat cards for a single count line in the same
  format as the Delete marker window ("9 version(s) · Delete
  markers: 2"); the per-child Open / Versions / Markers controls
  stay.
- **Calm version windows.** The Versions timeline and Content
  Versions hide delete-marker rows by default — the same "show
  delete-marker icons" toggle that reveals the ⛔ badges on the grid
  reveals them here too; the separate Content-Versions marker
  setting is gone.
- **First import opens the bucket.** Importing the very first data
  source — including straight from the welcome screen — opens the
  imported bucket's content immediately, and the "No data source
  yet" welcome never lingers once any source exists.

### Removed

- **"latest" pill in the Delete marker window.** The window exists
  to undo deletes; the tag restated what the window already implies
  and added visual noise.

## [1.1.0-beta.7] — 2026-09-16

Desktop-UX alignment cut: the small Explorer/WinSCP-grade
affordances the menus were missing — copy-as-text, a header column
picker, View-menu quick toggles, invert selection — plus a guarded
exit that refuses to lose running transfers or unsaved profile
work. Validated by a 335-check visual walk (five new steps covering
every new surface) and the live GUI walk against real MinIO, where
copy-as is verified through the real OS-clipboard bridge; `go test
-race` green across all packages.

### Added

- **Copy name / Copy path / Copy S3 URI.** Every row context menu
  (S3 objects, buckets, remote engines, local pane, side S3 pane)
  and a new Edit ▸ Copy as submenu put plain text on the OS
  clipboard: the bare name, the full path (`bucket/key`, the
  remote source path, or the absolute local path), or an
  `s3://bucket/key` URI — multi-row selections copy one per line.
  Ctrl+C keeps mirroring the selection as real OS file objects for
  Explorer/Finder interop.
- **Header column picker.** Right-clicking a grid header (main
  grid or side panel) opens a checklist of the column catalog —
  the same control Settings exposes, one click closer. The name
  column stays locked on, and choices persist per pane.
- **View-menu quick toggles.** Show version icons, show
  delete-marker icons and reveal hidden (delete-marked) objects
  flip straight from the View menu — checkmarked, no Settings
  round-trip.
- **Invert selection (Ctrl+I).** Complements the selection over
  the visible rows, Explorer-style; also in the Edit menu and on
  the F1 keyboard map.
- **Guarded exit.** Closing the window (X) or File ▸ Exit while
  transfer jobs are running, the profile file has unsaved changes,
  or session sources are not saved asks first, stating the concrete
  reason; **Exit anyway** quits past it. A clean state closes
  without asking.

## [1.1.0-beta.6] — 2026-09-15

Refinement cut: the delete/versioning surfaces from beta.5 are
re-tuned against best-practice destructive-action design — calmer
defaults, a clearer consequence display, and version tooling that is
now opt-in. The Delete Window keeps a fixed width with an
always-Delete footer, the typed gate moves to Settings, badges and
windows get quieter and more precise, and Directory Versions grows
into Content Versions with per-object controls. Validated by a
315-check visual walk (five new DOM-level polish locks) plus the
live GUI walk against real MinIO and `go test -race` green across
all packages.

### Changed

- **Typed delete gate is opt-in.** The Delete Window no longer
  demands typing `delete` for the destructive types by default:
  destructiveness is carried by the explicitly chosen delete type
  plus the pre-counted summary, and one deliberate click runs it.
  A Settings toggle ("typed confirmation for destructive deletes")
  brings the gate back for every delete; enabling it shows the
  typed partition again.
- **Delete Window polish.** The window keeps a fixed width no
  matter what is selected (single object, deep multi-folder
  selection), and the footer confirm button always reads
  **Delete**. The amber consequence line now appears only for the
  destructive modes — the safe default explains itself in its
  radio hint, so it no longer repeats in warning color.
- **Badges are opt-in and precise.** The ⟲ version and ⛔ marker
  badges default to off; two Settings toggles ("show version
  icons", "show delete-marker icons") bring them back. The object
  marker badge dropped its count — a single object carries at most
  one marker, so it shows the plain icon; folder badges keep their
  aggregated counts.
- **Delete marker, singular.** The marker window for a single
  object is titled "Delete marker", and its context-menu entry
  appears only on versioned sources where the selected object
  actually has a marker. Inside the window, rows are checkbox
  multi-selectable (bulk **Remove selected**), each row keeps its
  one-click Remove (undo delete), the current version earns a
  "latest" tag pill, and **Remove all** is no longer red —
  removing a marker restores the object, so red overstated the
  risk. Both bulk actions still confirm before running.
- **Directory Versions → Content Versions.** The folder-level
  overview is renamed and rebuilt: stat cards (current objects,
  versions, markers, bytes — marker info on by default, toggleable
  in Settings) sit above per-child rows that each carry direct
  controls (Open / Versions… / Markers…). The refresh button is
  retired — every action reloads exactly what it changed.

### Added

- **Best-practice toggles in Settings.** Show version icons
  (default off), show delete-marker icons (default off), Content
  Versions marker info (default on) join the existing delete
  confirmation knobs, so every version-UI surface is now
  user-controllable.

## [1.1.0-beta.5] — 2026-09-15

Beta cut: deletes and versioning get one unified stage — a pre-counting
Delete Window on every source type (S3, SFTP/FTP/WebDAV, local pane)
with a third delete type (all except current version), a typed
"delete" gate that is always enforced for the destructive modes, and
version/marker count badges that open per-object and per-folder
version overviews. Grids gain user-configurable columns, hidden
(delete-marked) objects become a toggleable ghost view, and Settings
grows per-column checkboxes for both panes. Everything was validated
against a real backend: a 305-check visual walk plus an 85-check live
GUI walk (real browser, real MinIO, real transfers — exercising all
three delete types, the marker window and the typed gate), and
`go test -race` green across all packages.

### Added

- **Unified Delete Window (all sources).** Deleting from S3, an
  SFTP/FTP/WebDAV source or the local pane now goes through one dialog
  that pre-counts exactly what will be removed (objects/folders/bytes)
  and names the target path. On versioned buckets it offers three
  types: **add a delete marker** (default — everything stays
  restorable), **delete all except current version** (new — keeps the
  latest version of each object, clears every older version and marker
  beneath the selection), and **delete permanently** (destroys every
  version AND marker). The two destructive types always require typing
  `delete` before the button unlocks — no setting can waive that. The
  window is on by default; Settings can turn it off (the classic
  confirm ladder returns), add the typed gate to every delete, or
  auto-confirm single-item marker deletes. Shift+Del still jumps
  straight to the permanent path.
- **Version- and marker-count badges.** Every row in a versioned
  bucket carries its numbers: ⟲ n (version count — per object, or
  aggregated for folders) and ⛔ n (delete markers). Clicking ⟲ opens
  the object's Versions dialog — or the new **Directory Versions**
  window for folders: current/noncurrent/marker/bytes totals plus the
  full per-object list, ghosted where nothing live remains. Clicking ⛔
  opens the **Delete Marker** window with per-marker **Remove**
  (undo delete) and a Remove-all sweep.
- **Configurable grid columns.** The column catalog grew (Type, ETag)
  and Settings now has per-column visibility checkboxes for both the
  main grid and the side-panel grid (Name stays pinned). Objects whose
  latest version is a delete marker are hidden from listings by
  default; "Show hidden (delete-marked) objects" lists them as ghost
  rows, and the marker icons can be turned off separately.

### Fixed

- **Directory Versions could stick on "Loading…"** — the window now
  always renders the structured stats instead of a spinner that never
  resolves.

### Changed

- **"Previous versions…" → "Versions…"** everywhere — the dialog shows
  the full timeline including the current version, not just older
  copies.
- **Every Upload button opens the Files… / Folder… menu** — toolbar,
  both panes' context menus and empty states now all route through the
  same native pickers (the Windows-safe dialogs restored in beta.4).

## [1.1.0-beta.4] — 2026-09-15

Beta cut: versioning takes center stage — a marker-vs-permanent choice
on every versioned delete, per-row delete-marker badges in the grid, and
version-preserving `cp`/`mv --versions` — plus the fix for the beta.3
Windows upload regression and a new cross-source e2e matrix covering
every local/s3/sftp/ftp/webdav pairing. Everything was validated against
a real backend: an 85-check live GUI walk (real browser, real MinIO,
real transfers — now exercising both delete modes and the marker
badges), the cross-source e2e suite, and `go test -race` green across
all packages.

### Added

- **Delete mode choice (versioned buckets).** Deleting from a versioned
  bucket now asks first: **Delete (add a delete marker)** — the safe
  default, everything stays restorable in version history — or
  **Delete permanently**, which destroys every version AND marker of the
  selection (the typed "permanent" confirmation is the force gate;
  folder selections purge everything beneath them). Shift+Del still jumps
  straight to the permanent path.
- **Delete-marker badges.** In versioned buckets the grid badges
  delete-marker state per row: files show how many markers their history
  carries (⛔ n), folders aggregate everything beneath them and read
  "all deleted" when nothing live remains. Costs one extra
  ListObjectVersions pass per folder view (skipped beyond 500 rows).
- **Version-preserving transfers.** `cp --versions` (S3→S3) recreates the
  source's full version timeline at the destination, delete markers
  included — the destination bucket must be versioned. `mv --versions`
  then purges the sources (L3; `--force` gates >50 versions). The GUI's
  S3→S3 conflict dialog gained the same choice.
- **Cross-source e2e suite.** `scripts/e2e-cross.sh` (+ CI job `e2e-cross`):
  every local/s3/sftp/ftp/webdav pairing must reproduce the exact source
  tree at the destination (tree-diff per cell + byte round-trips), plus
  the version-preservation contract above. Hetzner cells stay
  env-gated and skip when no credentials are configured.

### Fixed

- **GUI upload on Windows.** The beta.3 "one dialog picks files AND
  folders" experiment regressed hard on real Win10/11 desktops: with
  `FOS_PICKFOLDERS` set, the common-file dialog greys the file rows out,
  so only directories could be selected — and uploads went nowhere. The
  proven v1.0.0 pickers are restored and now sit under one **Upload**
  menu everywhere (toolbar, context menus, empty states):
  **Files… (Ctrl+U)** opens the native multi-select file dialog,
  **Folder…** the directory dialog. The backend walks directories on
  either path, and drag & drop / paste keep working unchanged.
- **Auto refresh is opt-in.** The GUI no longer polls on a timer by
  default: auto refresh starts OFF (View menu / Settings to enable) and
  is fully disabled while no data source is configured — no background
  traffic the user never asked for.
- **Streamed listings could miss early pages.** The grid subscribed to
  `list:page` events after issuing the list call — a fast first page
  could beat the subscription and vanish. The app now subscribes before
  calling and replays anything the backend flushed in between
  (`subscribeStream`, also applied to deep-search results).
- **Grid double-fired selection events** on select-all, set-rows and
  clear-selection — one Ctrl+A or Escape requested drag URLs twice.

### Changed

- **Data-source icons.** The sidebar now draws a purpose-made, license-
  free SVG glyph per source type (original artwork, no external icon
  set): a storage bucket for S3, the SSH terminal prompt for
  SFTP/SCP, a folder with transfer arrows for FTP, a locked folder for
  FTPS, a globe for WebDAV (padlock-badged for WebDAVs) and a disk
  drive for local sources. Glyphs follow the theme color.
- **CI: vsftpd containers run with `REVERSE_LOOKUP_ENABLE=NO`.** The
  image default (reverse DNS of the client IP) stalled every fresh FTP
  control connection ~15 s before the greeting, starving the e2e suites.

## [1.1.0-beta.3] — 2026-09-14

Beta cut: transfer conflict preview, per-bucket S3 sources, Windows code
signing and a UI polish pass — plus two live-testing discoveries fixed:
ghost folders after recursive deletes on MinIO-style stores, and clean
destinations always opening a dialog. Everything was validated against a
real backend: a 62-check live GUI walk (real browser, real MinIO, real
transfers) and the full CLI e2e suite, with `go test -race` green across
all packages.

### Added

- **Transfer conflict preview (GUI).** Every upload, download and copy
  pre-checks the destination before moving anything. A clean destination
  now transfers with **no dialog at all**; when files collide you get a
  per-file list showing both sides' size and modification time, a
  checkbox per row, select/unselect all, bulk actions and a per-row
  action (overwrite / skip / rename) with a live summary. The old
  whole-transfer policy prompt survives only as the fallback when the
  pre-check itself fails.
- **Per-bucket S3 sources.** Every S3 source is scoped to one bucket:
  `s3b source add` gains `--bucket` plus an `s3://bucket` shorthand
  (without a name, the bucket is the name), `source list` shows each
  source's bucket or "(all — account-wide)", and the GUI tree shows the
  bucket's content directly under the source node — the same shape as
  every other source type. Legacy account-wide sources keep the
  bucket-list level. This replaces the removed `source use`
  default-source concept.
- **Windows code signing.** Release builds sign every Windows artifact —
  both architecture binaries, the portable zips and the NSIS installer —
  with a SHA-256 Authenticode signature plus an RFC 3161 timestamp. The
  current certificate is self-signed; its public key ships at
  `scripts/certs/s3b-signing.cer` so fleets can pin it (details and
  SmartScreen guidance in [docs/security.md](docs/security.md)).
- **Transfer manager polish.** Per-job percentage, an animated progress
  bar and a proper empty state.
- **Log area live filtering.** Level, scope and source selectors plus
  free-text search — all applied retroactively over the whole line
  buffer, not just future lines.

### Changed

- UI polish pass: uniform micro-transitions on interactive elements,
  the theme accent color on checkboxes and radios, hover elevation on
  primary/danger buttons, and row hover on grid header filters.
- Code hygiene: dead code removed across Go and the frontend (including
  the `source use` command, unused i18n keys, CSS rules and JS exports);
  the tree is staticcheck-clean; `scripts/i18n-check.mjs` now also
  detects unused translation keys.
- Test harnesses grew with the features: the live GUI walk covers 62
  checks end-to-end, the visual harness 258.

### Fixed

- **No more ghost folders after recursive delete.** Stores disagree on
  whether the folder marker `dir/` appears in a listing under `dir/`
  (AWS lists it, MinIO omits it), so recursive deletes built from the
  listing left an unremovable ghost folder row on MinIO-style stores.
  `rm -r` and the GUI folder delete (including their dry-runs) now
  always include the marker; copies and storage-class conversion
  deliberately never do — converting an implicit folder's marker is
  pointless and can fail outright.
- **Clean-destination transfers no longer prompt.** An empty conflict
  pre-check result serialized as JSON `null`, which the frontend read
  as "pre-check failed" — so every clean upload/download opened the
  classic whole-transfer dialog. Empty now round-trips as a proper
  empty list and the transfer simply starts.

## [1.1.0-beta.2] — 2026-09-14

Beta cut: OS interop, the unified data-source hierarchy, and credential
import — the app now behaves like a native file manager for every source
type. Full validation pass on CLI and GUI; the release pipeline ships
macOS dmg for both Apple Silicon (arm64) and Intel (amd64).

### Added

- **OS ⇄ App clipboard and drag interop.** `Ctrl+C` on remote/S3 rows
  mirrors the selection onto the real OS clipboard (files are staged
  through a temp-dir transfer, then handed to the OS as file paths — cut
  never mirrors, so Explorer never sees a paste as a move). `Ctrl+V` in a
  bucket accepts paths copied in Explorer and uploads them. Dragging rows
  OUT of the app exports them as downloadable loopback URLs
  (`DownloadURL` + `text/uri-list`), so files and folders can be dropped
  straight into Explorer, e-mail clients and browsers; dropping OS files
  IN keeps working everywhere (grid, tree, side pane, dual-pane).
- **Unified data-source hierarchy.** Every source type — S3, SFTP/SCP,
  FTP/FTPS, WebDAV/WebDAVs, local — renders the same tree: source name →
  content. The old S3-only "source → bucket level → buckets" extra hop is
  gone, the Default Data Source concept is gone, and each source node
  carries a live connectivity ball (green/red, probed after every source
  refresh). Locations are source-scoped with ONE canonical path format,
  `Source name://bucket/prefix/` (remotes: `Source name:///dir/`),
  editable in an inline path bar: click the navbar, type or paste any
  path, Enter navigates. The sidebar is resizable (drag the splitter,
  double-click resets, width persisted).
- **Every S3 feature works on every S3 source.** Opening a source pins it
  as the "view source" (mirrored in the status bar); rename, new folder,
  delete (with version-aware gates), properties, versioning/restore,
  pre-sign, storage class, object lock, admin panel, uploads, downloads,
  deep search and the dual pane all address the source you are browsing
  through new source-pinned `Source*` APIs — no "current profile"
  coupling anywhere in the UI.
- **Add-source dialog with auto-filled name.** The name auto-fills from
  the endpoint host for S3 (`hel1.your-objectstorage.com` → `hel1`), the
  folder name for local sources, and the start-directory leaf (or the
  host's first label when the start dir is empty) for remote engines —
  and stays editable: once you type a name it is never overwritten.
  Start-directory/root fields gain Browse… pickers (server-side browser
  for remotes, OS folder picker for local), and Test dials the FORM
  values, saved or not.
- **One upload command.** A single `Ctrl+U` / toolbar / menu action opens
  ONE OS dialog that picks files AND folders together; everything routes
  through the same conflict-policy and transfer pipeline.
- **Import credentials.** A new dialog (File → Import credentials…)
  discovers credentials from local files — AWS INI `credentials`/
  `config`, shared JSON — or fetches them from secrets services:
  Vault, AWS Secrets Manager, GCP Secret Manager, Azure Key Vault, and a
  fully custom HTTP endpoint (URL, JSON path for the secret payload,
  arbitrary auth headers). Every candidate gets a live Test (bucket
  count) before Import; secret values never cross into candidate
  metadata, and importing creates a ready data source whose buckets are
  immediately browsable.
- **Versioning / Object Lock as pills.** Bucket properties render
  Versioning, Object Lock (and MFA-delete) as Enabled/Disabled pills
  instead of raw strings.

### Testing

- **Visual harness extended to 223 checks** (from 189): the walk now also
  drives the add-source auto-name rules (S3 endpoint → host label, local
  folder → leaf, typed-name precedence), both import-credentials flows
  (file parse and custom-HTTP KMS with header/JSON-path params), view-
  source pinning and status-bar mirroring, per-source status balls,
  canonical-path navigation, sidebar resizing, the OS clipboard mirror
  and Explorer-path paste, drag-out URL precompute, toolbar
  back/forward/up with their Alt-key shortcuts, F5, breadcrumb-segment
  clicks, favorites (add/jump/remove with persistence), theme toggle,
  auto-refresh intervals (including the blocked-while-jobs-running rule),
  marquee rubber-band selection, the external-editor manager, `Ctrl+D`
  download routing and the `Ctrl+F`/`Ctrl+U`/`F9` shortcuts.
- **Live import-credentials e2e** (`pkg/api`, env-gated by `S3B_E2E_*`):
  both flows — AWS INI file parse and a custom-HTTP secrets endpoint —
  run against a real S3-compatible provider end to end: parse/fetch →
  live bucket-count test → import → buckets listed through the new
  source, with assertions that credential values never leak into
  candidate metadata. Verified against Hetzner Object Storage; skips
  cleanly when the environment is unset.

## [1.1.0-beta.1] — 2026-09-13

### Fixed (post-cut GUI audit)

- **Properties dialogs were broken for every data source.** `fmtDate` was
  used in eight places in `main.js` but never imported from `util.js`, so
  object, folder, bucket and multi-selection Properties all failed with
  `ReferenceError: fmtDate is not defined` (surfaced as an error toast).
  Found by the extended visual harness, which now opens every properties
  dialog and asserts its rows render.
- **Admin panel ACL tab never rendered.** The tab spread a ternary result
  (`...(cond ? element : null)`) into `replaceChildren`, which throws for
  both outcomes — with grants (element is not iterable) and without
  (null is not iterable) — leaving the previous tab's content on screen.
  Now a plain conditional child.
- **Visual harness coverage pass.** The walk now covers all 11 admin tabs,
  About, the F1 key sheet, pre-sign/storage-class/object-lock dialogs,
  folder/object/bucket properties, rename/new-folder prompts, delete
  gates, a dark-theme main-view shot, chrome (menubar/toolbar/statusbar)
  alignment assertions, and a 1024×640 small-viewport admin-modal fit
  check; every dialog step runs a geometry audit (modal inside viewport,
  no clipped buttons, tabs inside the strip, no page-level horizontal
  scroll). `npm run gui` runs the same walk in a visible browser window.

First beta cut of 1.1.0: feature-complete, full validation pass on CLI
and GUI, positioned ahead of the stable release.

### Added

- **Help menu: User guide + Supported data sources.** Two new dialogs
  document the app in-app: a six-section usage guide (getting started,
  browsing, transfers, versions & safety, administration, tips — tabbed,
  same chrome as the admin panel) and a supported-sources sheet covering
  S3 and every known S3-compatible provider with capability notes, the
  remote engines (SFTP/SCP, FTP/FTPS, WebDAV/WebDAVs), the local
  filesystem pane, and CLI URI parity. Menu labels localized in all 15
  languages; guide prose stays English like the admin panel. The visual
  harness walks both dialogs (screenshots + content assertions).
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

- **S3 credentials import actually supports S3-compatible providers.** The
  import parsed only `aws_access_key_id`/`aws_secret_access_key`, so a
  MinIO/R2/Wasabi profile in `~/.aws/credentials` landed as an AWS source
  pointing at the wrong cloud. It now also parses `endpoint_url`, `region`
  and `aws_session_token`, merges the optional `~/.aws/config`
  (`[profile x]` sections; sso-session/services blocks are ignored), and
  sets endpoint/region on the imported sources. A missing credentials file
  returns a friendly "create it with 'aws configure'" error instead of a
  bare path error. UI strings renamed from "Import ~/.aws/credentials" to
  "Import S3 credentials" in all 15 languages.
- Admin panel dialog now reserves enough width for the full tab strip
  (and wraps it on narrow windows) instead of scrolling tabs out of view.

- License audit corrections: `gen-notice.sh` mislabeled two direct
  dependencies' SPDX ids in release NOTICE files — `jlaffaye/ftp` is ISC
  (was "MIT") and `pkg/sftp` is BSD-2-Clause (was "BSD-3-Clause"),
  verified against the modules' own LICENSE files. `docs/security.md`
  still claimed a "MIT/Apache-2.0 only" dependency set and
  `docs/comparison.md` still said "no SBOM yet" — both updated to match
  the post-M9 dependency reality (SBOM + SHA256SUMS ship per release).

### Changed

- Onboarding/empty-state copy simplified: "Add a data source to connect
  to Amazon S3 or any S3-compatible storage." (the MinIO/R2/Wasabi
  enumeration is gone), in all 15 languages.

- **Default UI language is now English.** A fresh start no longer follows
  the browser/OS language (Finnish on fi systems); Settings → Language →
  "Auto (browser)" explicitly opts back into browser detection, and an
  already-saved language choice keeps winning as before.

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

- Safety ladder: count-then-act on every destructive
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
  guarantee is now pinned by CI.

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
