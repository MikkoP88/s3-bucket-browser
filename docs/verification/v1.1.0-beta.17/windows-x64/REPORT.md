# Verification report — s3b v1.1.0-beta.17

**RELEASE GATE: PASS — 92 PASS · 2 SKIP · 0 FAIL** — full matrix in 872s.

**Version stamp:** `1.1.0-beta.17` — binary-verified by the run itself:
CLI-M-01 requires the binary to print exactly this stamp; GUI-01 round-trips
it through the Wails bridge. The stamp matches what the release workflow
builds (`.github/workflows/release.yml`).

**Verified commit:** `1708f16` — Verification: round 3 extreme rows (85 -> 94) + source-ID collision fix
**OS:** Windows_NT 10.0.26100 (x64)
**Node:** v24.12.0
**Run:** id `vermuad9tx6`, bucket `verify-muad9tx6`, started 2026-09-20T22:21:48.127Z
**Command:** `node scripts/verify.mjs --release v1.1.0-beta.17`

Every row below ran end-to-end through binaries built by this run from the
verified commit — CLI face and GUI face against live engines (MinIO S3, SFTP,
FTP, WebDAV), with byte-level verification, safety-gate probes, cancellation
probes and fault injection. The gate itself is documented in
[docs/VERIFICATION.md](../../../VERIFICATION.md).

## Certificate

| ID | Face | Action | Source | Scenario | Result |
|---|---|---|---|---|---|
| CLI-S3-01 | CLI | Add + test S3 source | S3 (MinIO) | source add with endpoint/keys; source test dials; list shows it; mirrors as profile | PASS |
| CLI-S3-02 | CLI | Create versioned bucket | S3 (MinIO) | mb + bucket versioning on (the delete-marker scenarios need it) | PASS |
| CLI-S3-03 | CLI | Create folder marker | S3 (MinIO) | mkdir docs/ → zero-byte marker lists as a folder | PASS |
| CLI-S3-04 | CLI | Single upload | S3 (MinIO) | cp one file → stat reports size | PASS |
| CLI-S3-05 | CLI | Multi upload (recursive) | S3 (MinIO) | cp -r fixture tree (10 files incl. unicode + empty) → recursive ls count matches | PASS |
| CLI-S3-06 | CLI | List / tree / du / stat | S3 (MinIO) | dir-view ls, tree shows folders, du counts objects+bytes, stat bucket shows region | PASS |
| CLI-S3-07 | CLI | Single download | S3 (MinIO) | cp object → local; bytes identical | PASS |
| CLI-S3-08 | CLI | Multi download (recursive) | S3 (MinIO) | cp -r prefix → local dir; full tree diff byte-identical | PASS |
| CLI-S3-09 | CLI | Server-side copy S3→S3 | S3 (MinIO) | cp s3://→s3:// lands a copyable object | PASS |
| CLI-S3-10 | CLI | Single object deletion | S3 (MinIO) | rm one object → gone from ls; versioned → delete marker in timeline | PASS |
| CLI-S3-11 | CLI | Recursive deletion + safety gates | S3 (MinIO) | 55-object prefix: rm -r without --force rejected (>50 gate); --dry-run counts (marker included); --force deletes them all | PASS |
| CLI-S3-12 | CLI | rb safety gate | S3 (MinIO) | rb on a non-empty bucket rejected without --force | PASS |
| CLI-S3-13 | CLI | Rename (mv) | S3 (MinIO) | mv object → new key; old gone, new stats | PASS |
| CLI-S3-14 | CLI | sync (repair / no-op / new / --delete) | S3 (MinIO) | sync repairs the CLI-S3-10 deletion, reports 0 when in sync, 1 after a local add, deletes the extra remote with --delete | PASS |
| CLI-S3-15 | CLI | Presign + fetch | S3 (MinIO) | presign → plain HTTP GET returns identical bytes | PASS |
| CLI-S3-16 | CLI | Version timeline: restore + undo | S3 (MinIO) | overwrite → 2 versions; restore v1 as latest; rm → marker; undo revives v1 | PASS |
| CLI-S3-17 | CLI | Purge + permanent destroy | S3 (MinIO) | versions stat; purge noncurrent (>50 gate demands --force, then --force purges); versions rm --all empties the timeline (L3) | PASS |
| CLI-S3-18 | CLI | Storage-class conversion | S3 (MinIO) | sc single → REDUCED_REDUNDANCY visible + find --class; recursive dry-run gate | PASS |
| CLI-S3-19 | CLI | Deep find | S3 (MinIO) | --name glob/substring, --smaller, --limit, summary line | PASS |
| CLI-S3-20 | CLI | Doctor diagnosis | S3 (MinIO) | doctor s3://bucket runs the check ladder | PASS |
| CLI-S3-21 | CLI | Bucket admin: info / tags / policy | S3 (MinIO) | info shows versioning; tags put/get; policy put/get round-trip (cors/encryption tolerate provider gaps) | PASS |
| CLI-S3-22 | CLI | Object lock: retention + legal hold | S3 (MinIO) | mb --object-lock; retention set/show/clear; legalhold on/off (GOVERNANCE only — cleanup stays possible) | PASS |
| CLI-S3-23 | CLI | Activity log | S3 (MinIO) | log shows the operations this run performed | PASS |
| CLI-S3-24 | CLI | Bucket config deletes never destroy the bucket | S3 (MinIO) | website/encryption/lifecycle/cors/pab delete on a disposable bucket — after EACH op the bucket must still stat; pab delete must refuse cleanly on providers without PAB support | PASS |
| CLI-S3-25 | CLI | Lifecycle rules round-trip | S3 (MinIO) | put flat-schema rules (expiration + transition); get echoes them; delete clears (put tolerates provider gaps as SKIP) | **SKIP** — lifecycle put rejected by this provider (recorded gap) |
| CLI-S3-26 | CLI | Versioned migration (cp/mv --versions) | S3 (MinIO) | 2 versions at source; cp --versions s3→s3 copies the full timeline; mv --versions moves it; unversioned destination refuses (gate) | PASS |
| CLI-S3-27 | CLI | cp flag contracts: --dry-run / --no-clobber | S3 (MinIO) | --dry-run prints the plan but lands nothing (stat 404); --no-clobber skips an overwrite (documented skip semantics: exit 0, object bytes untouched); --force overwrites | PASS |
| CLI-S3-28 | CLI | find size + time filters | S3 (MinIO) | controlled prefix (1 big + 1 small): --larger/--smaller counts; --newer 1h finds both; --older 1h finds none | PASS |
| CLI-S3-29 | CLI | Multipart large-object round-trip | S3 (MinIO) | 32 MiB random object (7+ multipart parts at the 5 MiB part size) uploads and downloads back sha256-identical — integrity is byte-level, not size-level | PASS |
| CLI-S3-30 | CLI | Pagination across the 1000-key page boundary | S3 (MinIO) | 1006 objects (incl. one empty) — recursive ls must return every one across the S3 1000-key page boundary, then a gated mass delete clears them all | PASS |
| CLI-S3-31 | CLI | Hostile key names round-trip | S3 (MinIO) | spaces, unicode, %2F-literal, + = &, leading dot, 150-char names, 12-deep nesting, quotes — exact-name listing, per-key stat, byte round-trip, and a presigned fetch of the %2F hazard | PASS |
| CLI-S3-32 | CLI | Concurrency: parallel workload + same-key race | S3 (MinIO) | 5 CLI processes at once (3 uploads, 1 download, 1 listing) all succeed byte-exact; two simultaneous writes to ONE key serialize into clean versions — never a torn object | PASS |
| CLI-S3-33 | CLI | SSE-S3 server-side encryption (--sse AES256) | S3 (MinIO) | cp --sse AES256 uploads with the SSE header; the object round-trips byte-identical (SKIP records a provider gap when the engine rejects SSE) | **SKIP** — provider gap — SSE rejected: error: C:\Projects\s3-bucket-browser\testartifacts\verification\fixtures\data\root-1.txt: operation error S3: PutObject, https response error StatusCode: 501, RequestID: 18D726F696C6F305, HostID: dd9025bab4ad464b049177c95eb6ebf374d3b3fd1af9251148b658df7ac2e3e8, api error NotImplemented: Server side encryption specified but KMS is not configured (KMS not configured for a server side encrypted objects) |
| CLI-S3-34 | CLI | Presign expiry: granted TTL + fails closed | S3 (MinIO) | a 2-second grant: the URL itself must carry exactly X-Amz-Expires=2 and serve while live; after expiry the SAME URL must be refused (providers with a clock-skew grace that keeps serving record the gap as SKIP) | PASS |
| CLI-S3-35 | CLI | Object lock (WORM): retention + legal hold enforced | S3 (MinIO) | a lock-enabled bucket (mb --object-lock, irreversible): GOVERNANCE retention and legal hold each must defeat a version-purge attempt (rm --versions) while in force — the version survives and the refusal is reported; clearing each lock re-arms the delete; the emptied bucket is then removable | PASS |
| CLI-S3-36 | CLI | Data-protection toggles: versioning suspend/resume + public access block | S3 (MinIO) | a scratch bucket: suspend versioning → info reads it back suspended and the suspended write lands as the null version while the existing timeline survives untouched; resume → new writes version again; PAB put --all arms all four blocks and a bare put disarms them — or the provider gap is recorded (this MinIO build rejects the whole PAB API; the aws CLI agrees) | PASS |
| CLI-X-01 | CLI | Add + test remote sources | SFTP/FTP/WebDAV | sftp:// and webdav:// URL shorthand + ftp flags; source test dials each | PASS |
| CLI-X-02 | CLI | Multi-file copy local→FTP | FTP | cp -r fixture tree (10 files: unicode, empty file, nested dirs) → xf://vermuad9tx6/tree | PASS |
| CLI-X-03 | CLI | Multi-file copy FTP→S3 (cross-engine) | FTP → S3 | cp -r the FTP tree into the bucket; count + full byte round-trip back to disk | PASS |
| CLI-X-04 | CLI | Remote browse + delete gates | FTP | ls/du/stat/tree on the remote; rm -r dry-run counts; --force deletes; prefix gone | PASS |
| CLI-X-05 | CLI | SFTP round-trip | SFTP | upload tree → download tree → byte-identical diff | PASS |
| CLI-X-06 | CLI | WebDAV round-trip | WebDAV | upload tree → download tree → byte-identical diff | PASS |
| CLI-X-07 | CLI | Cross-engine move (mv) | SFTP → FTP | mv -r sftp tree → ftp; source gone; destination byte-identical | PASS |
| CLI-X-08 | CLI | Export + import sources | all | encrypted export (--password); import into a FRESH config lists the same sources | PASS |
| CLI-X-09 | CLI | Source lifecycle: profile test + remove | S3 (MinIO) | add a temp source; profile test dials it (OK + bucket count); source remove drops it from BOTH source list and profile mirror | PASS |
| CLI-X-10 | CLI | Wrong-password import fails closed | all | an encrypted export imported with the WRONG password: decrypt must fail BEFORE any source is upserted (no partial import, no half-populated store); the correct password still imports cleanly afterwards | PASS |
| CLI-X-11 | CLI | Hostile filenames cross-engine | SFTP/FTP/WebDAV/S3 | names that stress protocol and shell escaping — spaces, # & % + ^ $ !, apostrophes, brackets, semicolons, CJK, a 120-char name — must round-trip through EVERY live engine and S3 with names and bytes intact | PASS |
| CLI-RES-01 | CLI | Slow link: latency +600ms/chunk | S3 via faultproxy | listing through a delayed proxy completes with correct output, measurably slower | PASS |
| CLI-RES-02 | CLI | Dead link: RST mid-session | S3 via faultproxy | connection reset → non-zero exit, error classified, no partial success | PASS |
| CLI-RES-03 | CLI | Blackhole: endpoint never answers | S3 via faultproxy | watchdog timeout within the --timeout budget (no default 5-minute hang) | PASS |
| CLI-RES-04 | CLI | Hard kill mid-upload: no partial object | S3 via faultproxy | a 64 MiB upload killed mid-flight (process termination) must land NO object — no corrupt or half-written key ever becomes visible; the clean retry is byte-identical | PASS |
| CLI-RES-05 | CLI | Transient 5xx storm: absorbed by retries / exhausted honestly | S3 via faultproxy | a canned-503 flap: a 2-failure storm must be retried away INSIDE the SDK budget (the put lands byte-identical); a 50-failure storm must exhaust it — non-zero exit, a surfaced error, and NO object half-landed | PASS |
| CLI-M-01 | CLI | Version identity | — | version prints the build version, exit 0 | PASS |
| CLI-M-02 | CLI | Usage-error contract | — | unknown command → exit 2, stderr reads "usage error:" and points at --help | PASS |
| CLI-M-03 | CLI | Shell completion | — | completion bash emits a working completion script; other shells answer too | PASS |
| CLI-M-04 | CLI | Secrets never printed | S3 (MinIO) | a source with a distinctive secret key: every output surface — source list (text + json), source test, the 403 transfer path, profile list, activity log — must run but never echo the secret, even on failure paths | PASS |
| CLI-M-05 | CLI | Secrets encrypted at rest | config store + OS keyring | a distinctive secret added to the store, then a RAW byte-scan of every file under the whole config directory (recursive): the secret may live only inside the OS keyring — never on disk in any file the app wrote this run | PASS |
| CLI-M-06 | CLI | Legacy store migration: profiles seed as sources, plaintext secrets leave the file | S3 (MinIO) | a hand-written LEGACY profiles.json (two profiles, inline secrets, one carrying a session token, no sources array): ONE CLI load seeds both as data sources (M8), the keyring takes every secret and the token (M5 — none remain in the file); the token-less migrated source still dials MinIO through the keyring-held secret, while the token profile must fail with an invalid-token refusal — the synthetic token being IN the signature is itself proof it was read back from the keyring; both profiles are then removed so no keyring residue survives the run | PASS |
| GUI-01 | GUI | Live stack boots | Wails v3 server | server /health ok; page loads; GetVersion binding round-trips the build version | PASS |
| GUI-02 | GUI | Add S3 source (GUI) | S3 (MinIO) | onboarding → editor → Test ✅ → Save → bucket root lists | PASS |
| GUI-03 | GUI | Add FTP source (GUI) | FTP | sidebar + → FTP fields → Test ✅ → Save → root lists | PASS |
| GUI-04 | GUI | Multi-file copy FTP→S3 (GUI) | FTP → S3 | browse FTP seed dir; ctrl-click 2 files; Ctrl+C; open S3 verify-gui/; Ctrl+V; both rows land | PASS |
| GUI-05 | GUI | GUI transfer byte verification | FTP → S3 | download the GUI-pasted objects via the CLI; bytes match the FTP originals | PASS |
| GUI-06 | GUI | Single object deletion (GUI) | S3 (MinIO) | CLI-seeded object; row selected; Del → Delete Window (marker default) → confirm; row gone; CLI timeline shows the marker | PASS |
| GUI-07 | GUI | New folder (GUI) | S3 (MinIO) | empty-area context menu → New folder → prompt; CLI ls shows the marker | PASS |
| GUI-08 | GUI | Rename (F2, GUI) | S3 (MinIO) | CLI-seeded object; F2 → new name; CLI stat sees the new key | PASS |
| GUI-10 | GUI | DnD upload local→S3 (GUI) | S3 (MinIO) | dual pane: drag uni-åäö.txt from the local side onto the bucket folder; Start; CLI sees the object; bytes identical | PASS |
| GUI-11 | GUI | DnD download S3→local (GUI) | S3 (MinIO) | drag an S3 row onto the local pane; Start; file lands on disk; bytes identical | PASS |
| GUI-12 | GUI | Versions dialog: restore as latest | S3 (MinIO) | 2 CLI-seeded versions; context menu → Versions shows the timeline; Restore as latest on the older one; CLI downloads the restored bytes | PASS |
| GUI-13 | GUI | Overwrite conflict dialog | S3 (MinIO) | DnD a file onto an existing name → conflict dialog offers Start; confirming creates the next version | PASS |
| GUI-14 | GUI | Delete Window CANCEL keeps the object | S3 (MinIO) | Del on a row opens the Delete Window; Cancel/Esc leaves the object untouched on BOTH faces (the cancellation contract) | PASS |
| GUI-15 | GUI | Doctor over the bridge | S3 (MinIO) | Help → Doctor → pick the source → run all checks in the popout; summary reports pass; task completes | PASS |
| GUI-16 | GUI | Admin dialog (bucket info) | S3 (MinIO) | bucket guard in the tree opens the Admin panel; versioning reported; tabs render; close | PASS |
| GUI-17 | GUI | Profile file round-trip (bindings) | Wails v3 server | SaveProfileFileAs → state open; Close → onboarding; wrong password rejected through the bridge; correct password restores the sources | PASS |
| GUI-18 | GUI | Workbench surfaces: transfer manager + dual pane + filter | S3 (MinIO) | View → Transfers opens the manager popout; dual pane toggle; filter box narrows the grid to matching rows and clearing restores them | PASS |
| GUI-19 | GUI | New file dialog: cancel + create | S3 (MinIO) | Shift+F4 prompt (defaults new-file/txt); Cancel creates NOTHING (CLI 404); then create verify-newfile.txt for real; CLI stats it | PASS |
| GUI-20 | GUI | Conflict matrix: per-file skip + rename | S3 (MinIO) | both dragged files conflict; a.txt→skip keeps v1 untouched (still 1 version, original bytes); b.txt→rename keeps the original AND lands the new bytes beside it | PASS |
| GUI-21 | GUI | Cancel mid-transfer: no corrupt object; retry clean | S3 (MinIO) | 8 MiB upload throttled to 256 kB/s; Cancel while running → job canceled and the object ABSENT (no partial lands); the unthrottled retry is sha-identical | PASS |
| GUI-22 | GUI | L2 identity gate: Empty-bucket window | S3 (MinIO) | the destructive Empty-bucket window demands the bucket's OWN name: a wrong word keeps the destructive button disabled; Cancel preserves every object | PASS |
| GUI-23 | GUI | Editor auto-upload round-trip (bridge) | S3 (MinIO) | EditObject stages the object, hands it to the OS (an inert .cmd probe file — the handoff is real but the "editor" is a no-op) and watches: bytes written to the staged file upload automatically; StopEdit ends the session | PASS |
| GUI-24 | GUI | Pre-sign URL dialog (GUI) | S3 (MinIO) | context menu → Pre-sign URL: the dialog exposes a READ-ONLY signed URL that a bare HTTP client outside the app can actually use to fetch the exact object bytes | PASS |
| GUI-25 | GUI | Pane compare: all six categories exact | S3 (MinIO) + local | a hand-built pair of dirs covering EVERY compare class — identical, only-left, only-right, different-size, newer-left, newer-right — the Compare button must count each category exactly 1 | PASS |
| GUI-26 | GUI | Ctrl+C stages HIDDEN — nothing lands until Paste | S3 (MinIO) | the two-part copy contract: select remote rows, Ctrl+C → the Explorer mirror runs as a HIDDEN staging job (a scratch download for paste-out, invisible in every list) while the bucket itself stays bit-for-bit unchanged until a Paste happens | PASS |
| GUI-27 | GUI | Download-side conflict matrix | S3 (MinIO) → local | drag three remote rows onto a local folder holding two of the SAME names: the per-file decision matrix (skip + rename) must keep the skipped local file untouched, land a renamed twin with the remote bytes, and download the clean third file with no prompt | PASS |
| GUI-28 | GUI | Selection mechanics: plain anchor, shift-range, ctrl-toggle, invert, select-all | S3 (MinIO) | a six-file folder: a plain click anchors row 1 (1 of 6 on the status bar); a shift-click on row 3 selects exactly the range (3 of 6); ctrl-click drops the middle member (2 of 6); Ctrl+I flips to the exact complement (4 of 6); Ctrl+A finally selects everything (6 of 6) — every count read from the visible selection bar. The plain click MUST come first: clicking a row that is already selected keeps the selection (drag-friendly semantics), so select-all has to end the ladder | PASS |
| GUI-29 | GUI | Multi-delete ladder: markers keep history; Shift+Del destroys permanently | S3 (MinIO) | four objects selected at once: the Delete Window counts all four AND offers all three delete types; the plain marker delete hides every object while each full timeline survives — CLI re-reads the data version AND its delete marker per object; a second batch through Shift+Del (the permanent preset) destroys versions entirely — nothing left in any timeline | PASS |
| GUI-30 | GUI | Versions dialog: A/B compare diff + per-version Destroy | S3 (MinIO) | three CLI-seeded text versions: the timeline lists all three; picking the OLDEST as A and the NEWEST as B and comparing shows BOTH sides' changed lines in the diff; destroying the oldest through its confirmation window drops the timeline to exactly two — re-read by CLI | PASS |
| GUI-31 | GUI | Versioned copy: the full timeline S3→S3 (the migrator) | S3 (MinIO) | an object with THREE versions pasted into a fresh versioned bucket: the version-choice dialog appears (the destination is versioned), the GUI carries the ENTIRE timeline across in a background job — the destination lists all three versions and the current bytes round-trip | PASS |
| GUI-32 | GUI | Admin panel mutations: versioning toggle + tags | S3 (MinIO) | a scratch bucket driven ENTIRELY through the Admin panel: Overview suspends versioning (CLI reads it back suspended), re-enables it (CLI reads enabled); Tags saves a pair (CLI reads it back) then deletes all — every GUI mutation verified out-of-band | PASS |
| GUI-33 | GUI | L2 execution: Empty bucket destroys EVERY version, keeps the bucket | S3 (MinIO) | a scratch bucket seeded with nested objects, extra old versions and delete markers: typing the bucket's own name arms the Empty-bucket window and EXECUTES — afterwards nothing lists anywhere (recursive listing empty, version statistics all zero, sampled timelines empty), yet the bucket itself survives and still takes writes | PASS |
| GUI-34 | GUI | Cancel mid-batch: finished files stay, the canceled one never lands | S3 (MinIO) | five 2 MiB files drag-dropped as ONE batch job under a 128 kB/s app throttle (files process sequentially): the job is canceled only once its first file has fully landed — exactly the finished file(s) exist remotely afterwards (byte-identical, CLI-verified), while the in-flight and still-queued ones are ABSENT with no partial left behind (S3 materializes nothing until an upload completes) | PASS |
| GUI-09 | GUI | Page-error gate | Wails v3 server | zero uncaught page errors across the whole GUI battery | PASS |
| SWEEP-VIS-01 | SWEEP | Full visual sweep | shim world | node scripts/gui-visual.mjs — every dialog/popout/menu/viewport contract | PASS |
| SWEEP-LIVE-01 | SWEEP | Full live walk | real engines | node scripts/gui-v3live.mjs — real bindings, transfers, versions, fault lab | PASS |

## Notes

- **CLI-S3-25 SKIP** — lifecycle put rejected by this provider (recorded gap)
- **CLI-S3-33 SKIP** — provider gap — SSE rejected: error: C:\Projects\s3-bucket-browser\testartifacts\verification\fixtures\data\root-1.txt: operation error S3: PutObject, https response error StatusCode: 501, RequestID: 18D726F696C6F305, HostID: dd9025bab4ad464b049177c95eb6ebf374d3b3fd1af9251148b658df7ac2e3e8, api error NotImplemented: Server side encryption specified but KMS is not configured (KMS not configured for a server side encrypted objects)
- CLI-S3-01 PASS — added, tested, listed, mirrored
- CLI-S3-02 PASS — verify-muad9tx6 created, versioning on
- CLI-S3-03 PASS — docs/ marker created and listed
- CLI-S3-04 PASS — uploaded + stat ok
- CLI-S3-05 PASS — 10 files uploaded, 11 listed
- CLI-S3-06 PASS — ls/tree/du/stat consistent
- CLI-S3-07 PASS — byte-identical
- CLI-S3-08 PASS — 10 files byte-identical
- CLI-S3-09 PASS — server-side copy listed
- CLI-S3-10 PASS — removed + marker recorded
- CLI-S3-11 PASS — gate held; dry-run 56, force-deleted 55
- CLI-S3-12 PASS — rejected as designed
- CLI-S3-13 PASS — moved + stat ok
- CLI-S3-14 PASS — repair/no-op/add/delete all correct
- CLI-S3-15 PASS — URL fetched, bytes identical
- CLI-S3-16 PASS — restore/undo byte-correct
- CLI-S3-17 PASS — purged and destroyed
- CLI-S3-18 PASS — single + recursive conversion verified
- CLI-S3-19 PASS — filters + limits correct
- CLI-S3-20 PASS — check ladder ran
- CLI-S3-21 PASS — info/tags/policy verified
- CLI-S3-22 PASS — retention/hold round-tripped, cleaned up
- CLI-S3-23 PASS — 2 lines recorded
- CLI-S3-24 PASS — bucket survived all five config deletes
- CLI-S3-26 PASS — timeline copied + moved intact; unversioned dest refused
- CLI-S3-27 PASS — dry-run inert; no-clobber skipped (bytes untouched); --force overwrites
- CLI-S3-28 PASS — size/time filters correct
- CLI-S3-29 PASS — 32 MiB multipart round-trip sha256-identical in 2.4s
- CLI-S3-30 PASS — 1006 objects listed exactly across the 1000-key boundary; mass delete clean
- CLI-S3-31 PASS — 9 hostile keys exact-listed; legal set byte-identical; %2F presigned clean
- CLI-S3-32 PASS — 5 parallel ops byte-exact; race → 2 clean versions, latest intact
- CLI-S3-34 PASS — 2s grant: served, then refused (HTTP 403) after expiry
- CLI-S3-35 PASS — retention + hold each blocked the purge; unlock re-armed it; bucket removed
- CLI-S3-36 PASS — suspend kept history (null-version semantics); resume re-versions; PAB rejected by this MinIO build (recorded provider gap — aws CLI agrees)
- CLI-X-01 PASS — sftp+ftp+webdav tested
- CLI-X-02 PASS — 10 files on the FTP source
- CLI-X-03 PASS — 10 files cross-engine, byte-identical
- CLI-X-04 PASS — browse + gated delete verified
- CLI-X-05 PASS — round-trip byte-identical
- CLI-X-06 PASS — round-trip byte-identical
- CLI-X-07 PASS — moved + verified
- CLI-X-08 PASS — encrypted export/import round-trip
- CLI-X-09 PASS — tested, removed, gone from both lists
- CLI-X-10 PASS — rejected before any upsert; clean import after
- CLI-X-11 PASS — 6 hostile names intact through s3+sftp+ftp+dav
- CLI-RES-01 PASS — correct listing in 2.1s under latency
- CLI-RES-02 PASS — clean classified failure
- CLI-RES-03 PASS — failed cleanly in 8.7s
- CLI-RES-04 PASS — kill left no visible object; retry sha-identical
- CLI-RES-05 PASS — absorbed 2×503 (bytes intact), then failed honestly after 3
- CLI-M-01 PASS — 1.1.0-beta.17
- CLI-M-02 PASS — exit 2 + labeled
- CLI-M-03 PASS — completion scripts emitted: bash, zsh, fish, powershell
- CLI-M-04 PASS — secret absent from all 6 surfaces (6 produced output)
- CLI-M-05 PASS — token absent from all 1 file(s) under the config dir (keyring holds it)
- CLI-M-06 PASS — legacy-vermuad9tx6 dials through the keyring-held secret; legacytok-vermuad9tx6's token reached the signature (refused as synthetic); no keyring residue
- GUI-01 PASS — v3 stack up, 1.1.0-beta.17
- GUI-02 PASS — tested ✅ and listed
- GUI-03 PASS — tested ✅ and listed
- GUI-04 PASS — 2 files FTP→S3 through the GUI
- GUI-05 PASS — both files byte-identical
- GUI-06 PASS — marker deletion verified both faces
- GUI-07 PASS — folder created + CLI-verified
- GUI-08 PASS — renamed + CLI-verified
- GUI-10 PASS — unicode filename uploaded, byte-identical
- GUI-11 PASS — downloaded via DnD, byte-identical
- GUI-12 PASS — restored as latest, CLI-verified
- GUI-13 PASS — conflict → Start → new version
- GUI-14 PASS — cancelled; object intact on both faces
- GUI-15 PASS — ladder ran, summary pass
- GUI-16 PASS — admin panel, 11 tab(s)
- GUI-17 PASS — save/close/reject/reopen all good
- GUI-18 PASS — popout + dual pane + filter all live
- GUI-19 PASS — cancel inert; create landed
- GUI-20 PASS — skip kept v1 untouched; rename landed "b (1).txt" with the new bytes
- GUI-21 PASS — canceled job left no object; retry sha-identical
- GUI-22 PASS — wrong word disabled; cancel preserved everything
- GUI-23 PASS — edit auto-uploaded and round-tripped (verify-gui/edit/verify-edit.cmd)
- GUI-24 PASS — read-only signed URL, verified by an out-of-app client
- GUI-25 PASS — all six categories counted exactly
- GUI-26 PASS — staging hidden + bytes staged for Explorer; bucket unchanged
- GUI-27 PASS — skip kept local a.txt; rename landed "b (1).txt"
- GUI-28 PASS — plain anchor 1; shift-range 3; ctrl-toggle 2; invert 4; select-all 6/6 — counted on the status bar
- GUI-29 PASS — marker multi-delete kept every timeline; Shift+Del destroyed the second batch entirely
- GUI-30 PASS — A/B diff showed both sides; destroying the oldest left exactly two versions
- GUI-31 PASS — version-choice dialog + background job carried the whole 3-version timeline across buckets
- GUI-32 PASS — versioning suspend/enable + tags put/delete landed server-side, CLI-verified
- GUI-33 PASS — executed: every version and marker destroyed; the bucket survives and takes writes
- GUI-34 PASS — canceled mid-batch: 4 file(s) never landed, the 1 finished one(s) are byte-identical
- GUI-09 PASS — clean console
- SWEEP-VIS-01 PASS — 609/609 checks
- SWEEP-LIVE-01 PASS — 142 checks, no page errors

## Reproduce

```bash
node scripts/verify.mjs --release v1.1.0-beta.17
```

Prerequisites (live engine containers) and the full row matrix:
[docs/VERIFICATION.md](../../../VERIFICATION.md). `verification.json` next to
this file is the machine-readable copy of the same run.
