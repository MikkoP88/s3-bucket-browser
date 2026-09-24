# Action verification — the release gate

**Verified** means: the action ran end-to-end through the shipped binary —
CLI face and/or GUI face — against **live engines** (MinIO S3, SFTP, FTP and
WebDAV containers), with byte-level verification wherever bytes move,
safety-gate probes wherever destruction is involved, cancellation probes
wherever a dialog can abort a job, and fault injection wherever resilience
is claimed. A row that cannot run (engine container down, provider API gap)
is recorded **SKIP** — never silently passed.

**Terminology.** *Verification* is the process this page describes; the
*verification report* (`testartifacts/verification/verification.json`) is
the machine-readable artifact each run produces — and, in `--release` mode,
committed release evidence under
[`docs/verification/`](verification/). Before every release the
whole matrix is re-verified and the report regenerated — one command, exit
0 means every critical job still works on the exact binary being shipped.

This page is the contract. Every row in the tables below is executed by
[`scripts/verify.mjs`](../scripts/verify.mjs).

```
The release gate — one command verifies everything
  node scripts/verify.mjs             full run: CLI + GUI + both sweeps
  npm run verify                      same, via npm
  node scripts/verify.mjs --quick     CLI + GUI batteries, sweeps skipped (~3 min)
  node scripts/verify.mjs --skip-gui  CLI + sweeps only (no browser battery)
  node scripts/verify.mjs --no-build  reuse the exes in testartifacts/
  node scripts/verify.mjs --release <tag>  release evidence: tag-stamped full
                                      run → docs/verification/<tag>/<os>-<arch>/

Category runs — verify one focus area only
  node scripts/verify.mjs --only <category>    (or npm run verify:only -- <category>)
    cli          all four CLI batteries (s3 + cross + resilience + meta)
    s3           the MinIO/S3 CLI battery
    cross        cross-engine battery (FTP / SFTP / WebDAV)
    resilience   fault-injection battery (faultproxy: latency, RST, blackhole)
    meta         binary-level contracts (version, usage errors, completion)
    gui          the full GUI battery (real Wails v3 stack in a browser)
    sweeps       just the two harness sweeps
  npm run verify:cli | verify:gui      shorthands for the two common foci

Rules
  Repeat a run — results must be identical (repeatability is part of the
  report). Exit 0 = verified, exit 1 = at least one FAIL, exit 2 =
  bad invocation (e.g. unknown --only category). Categories map to whole
  batteries, never row slices: rows depend on state built by earlier rows
  in their battery, so a battery is the smallest safe unit. The two sweep
  rows retry once on a transient failure and disclose it in the PASS
  detail — a deterministic regression fails both attempts.

Prerequisites (scripts/e2e-cross.sh header starts all of them)
  MinIO   :9000   minioadmin / minioadmin     (required)
  SFTP    :2222   e2e / e2epass   atmoz/sftp        (joins when port answers)
  FTP     :2121   e2e / e2epass   fauria/vsftpd     (joins when port answers)
  WebDAV  :7070   e2e / e2epass   rclone serve webdav (joins when port answers)

Faces
  CLI   — s3b built with -tags s3b_headless, driven as a process; exit
          codes and output are asserted, never eyeballed.
  GUI   — s3b built with -tags server: the real Wails v3 stack (runtime,
          bindings, backend) in a real browser via Playwright. Results are
          verified BACK through the CLI, so a GUI green means bytes moved.
  SWEEP — the two full harnesses re-run as subprocesses:
          scripts/gui-visual.mjs (visual/dialog/viewport contracts)
          scripts/gui-v3live.mjs (live checks: transfers, versions,
          fault lab) — folded into the verification report as summary rows.
```

The matrix below was last verified on **Windows 11 (x64)** with Node 24.
The runner is platform-portable: same containers, same rows, the OS column
reflects where the report was produced.

---

## Verified actions

Legend — the **CLI**/**GUI** columns name the verification row that covers
the action on that face (`✅` = passed in the latest run, `—` = that face
does not expose the action as a one-step flow; where the live sweep covers
it, the sweep row is named). **OS** is the platform the verification ran on.

### Sources

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Add + test source | S3 (MinIO) | `source add` with endpoint/keys; `source test` dials; listed; mirrored as a profile | ✅ CLI-S3-01 | ✅ GUI-02 (editor → Test ✅ → Save → bucket root lists) | Win 11 x64 |
| **Source-editor name auto-fill takes the full host** | shim world | every source type: a remote host "10.20.3.65" suggests the name "10.20.3.65" (never its first label "10"), a start directory turns it into "<host> - <dir>", "/" falls back to the bare host, the S3 endpoint fallback keeps the full hostname, the bucket and the local folder leaf still win for their types, and a hand-typed name is never overwritten | — | — (sweep: source-editor-autoname, SWEEP-VIS-01 — 662/662) | Win 11 x64 |
| **Modal stack: a sub-view's cancel goes BACK, not out** | shim world | the directory browser opened from the source editor's Browse stacks on the editor; a New-folder prompt stacks a third level; Escape or Cancel on any level returns exactly to the view below with every field value intact (host, name) — never out through the parent; the admin panel returns under its cleanup windows, and the import dialog no longer double-shows after a nested prompt | — | — (sweep: source-editor-autoname + delete-window-uniform, SWEEP-VIS-01 — 669/669) | Win 11 x64 |
| Add + test remote sources | SFTP/FTP/WebDAV | `sftp://`/`webdav://` URL shorthand + ftp flags; `source test` dials each | ✅ CLI-X-01 | ✅ GUI-03 (FTP: Test ✅ → Save → root lists) | Win 11 x64 |
| Source lifecycle: profile test + remove | S3 (MinIO) | add a temp source; `profile test` dials it (OK + bucket count); `source remove` drops it from BOTH source list and profile mirror | ✅ CLI-X-09 | — | Win 11 x64 |
| Export + import sources | all | AES-256-GCM encrypted export (`--password`); ciphertext verified; import into a fresh store lists the same sources | ✅ CLI-X-08 | — | Win 11 x64 |
| **Wrong-password import fails closed** | **all** | an encrypted export imported with the WRONG password: decrypt fails BEFORE any source is upserted (no partial import, no half-populated store); the correct password still imports cleanly afterwards | ✅ CLI-X-10 | — | Win 11 x64 |
| Profile file round-trip | Wails v3 server | SaveProfileFileAs → state open; Close → onboarding; wrong password rejected through the bridge; correct password restores the sources | — | ✅ GUI-17 | Win 11 x64 |
| **`source add` UPDATE semantics + secret masking** | **S3 (dead endpoint)** | re-adding an EXISTING source name must update in place (one row, new endpoint live immediately — never a silent duplicate) or refuse outright; the secret never appears in `source list`; the store stays removable afterwards | ✅ CLI-S3-41 | — | Win 11 x64 |
| **Re-import + name collision: the store never forks** | **all** | the same encrypted export imported TWICE into one config: zero duplicates (source-ID collision) and the imported entry dials the live endpoint; then a pre-existing owner of the incoming name + a third import: exactly one row per name, nothing else lost, everything still removable | ✅ CLI-X-12 | — | Win 11 x64 |
| **Credential file import over the bridge** | **S3 (MinIO) via INI** | an AWS-style credentials INI: ParseCredentialFile finds the profile (secret detected, never returned); TestCredentialDraft dials the live endpoint; ImportCredentials upserts it as a source (ListSources proves it by name); RemoveSource drops it — the whole import ladder runs through the GUI's own bindings | — | ✅ GUI-41 | Win 11 x64 |
| Local filesystem bindings: list / preview / remove | local disk | ListLocal reports entries with correct dir flags; LocalDeletePreview counts exactly what a delete would take (1 file, exact bytes); LocalRemove actually deletes — proven from OUTSIDE the app (Node fs), never from the app's own view | — | ✅ GUI-44 | Win 11 x64 |

### Buckets & configuration

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Create versioned bucket | S3 (MinIO) | `mb` + versioning on (the delete-marker scenarios need it) | ✅ CLI-S3-02 | ✅ GUI-16 (admin dialog reports versioning) | Win 11 x64 |
| Bucket safety gates | S3 (MinIO) | `rb` on a non-empty bucket rejected without `--force` | ✅ CLI-S3-12 | — (delete window gates cover the GUI, SWEEP-LIVE-01) | Win 11 x64 |
| Bucket admin: info / tags / policy | S3 (MinIO) | `info` shows versioning; tags put/get; policy put/get round-trip (cors/encryption tolerate provider gaps) | ✅ CLI-S3-21 | ✅ GUI-16 (Admin panel: versioning + tabs) | Win 11 x64 |
| **Admin panel mutations: versioning toggle + tags** | **S3 (MinIO)** | scratch bucket driven entirely through the Admin panel: Overview suspends versioning (CLI reads it back suspended), re-enables it; Tags saves a pair (CLI reads it back) then deletes all — every GUI mutation verified out-of-band | — | ✅ GUI-32 | Win 11 x64 |
| **Config deletes never destroy the bucket** | **S3 (MinIO)** | website/encryption/lifecycle/cors/pab `delete` on a disposable bucket — after EACH op the bucket must still `stat`; pab delete must refuse cleanly on providers without PAB support. (Regression tripwire: MinIO RELEASE.2025-09-07 destroys the whole bucket on `DELETE ?publicAccessBlock`; s3b now pre-checks the capability and refuses — CLI-S3-24 pins that defense) | ✅ CLI-S3-24 | — | Win 11 x64 |
| Lifecycle rules round-trip | S3 (MinIO) | put flat-schema rules (expiration + transition); get echoes them; delete clears — `put` tolerates provider gaps as SKIP (this MinIO build rejects valid rules with 400 InvalidArgument: a recorded provider gap, not a pass) | ✅ CLI-S3-25 | — | Win 11 x64 |
| Object lock | S3 (MinIO) | `mb --object-lock`; retention set/show/clear; legal hold on/off (GOVERNANCE, cleanup stays possible) | ✅ CLI-S3-22 | — | Win 11 x64 |
| **Object lock ENFORCED against deletes (WORM)** | **S3 (MinIO)** | a lock-enabled bucket: GOVERNANCE retention and legal hold each defeat a version-purge attempt (`rm --versions`) while in force — the version survives and the refusal is reported; clearing each lock re-arms the delete; the emptied bucket is then removable | ✅ CLI-S3-35 | — | Win 11 x64 |
| **Data-protection toggles: versioning suspend/resume + public access block** | **S3 (MinIO)** | scratch bucket: suspend versioning → `info` reads it back suspended and a suspended write lands as the null version while the existing timeline survives; resume → writes version again; PAB `put --all` arms all four blocks, bare `put` disarms them — or the provider gap is recorded (this MinIO build rejects the whole PAB API; the aws CLI agrees) | ✅ CLI-S3-36 | — | Win 11 x64 |
| Doctor diagnosis | S3 (MinIO) | `doctor s3://bucket` runs the check ladder | ✅ CLI-S3-20 | ✅ GUI-15 (Help → Doctor → popout → run all → pass summary) | Win 11 x64 |
| Bucket CORS round-trip | S3 (MinIO) | `bucket cors put FILE` (full rule document), `get --json` echoes every field, `delete` clears — provider gaps (this MinIO build refuses the CORS API) record SKIP, never a pass | ✅ CLI-S3-37 | — | Win 11 x64 |
| Bucket website round-trip | S3 (MinIO) | `bucket website put --index/--error`, `get --json` echoes, redirect-host/proto re-put round-trips, `delete` clears — provider gaps (PutBucketWebsite 400 here) record SKIP | ✅ CLI-S3-38 | — | Win 11 x64 |
| Bucket default encryption | S3 (MinIO) | `bucket encryption put --algo AES256`, `get` echoes, `delete` clears — provider gaps (no KMS here) record SKIP | ✅ CLI-S3-39 | — | Win 11 x64 |
| **Object-lock enable on a plain bucket: refused honestly** | **S3 (MinIO)** | `bucket lock <plain-bucket> --enable` must be refused (AWS semantics: lock is decided at creation) with a reason — if a provider allows it, that is a recorded gap; either way the bucket stays healthy and writable | ✅ CLI-S3-40 | — | Win 11 x64 |
| **Admin panel: CORS tab round-trip** | **S3 (MinIO)** | scratch bucket's Admin panel → CORS tab: rule card filled (origins/methods/headers/expose/maxAge), Save → the rule is read back OUT-OF-BAND via the CLI (`bucket cors get --json`); Delete all clears it the same way — a GUI mutation is only believed when the CLI sees it | — | ✅ GUI-35 | Win 11 x64 |
| **Admin panel: Website tab round-trip** | **S3 (MinIO)** | Admin panel → Website tab: index/error keys saved → CLI `bucket website get` reads them back; Disable clears them — again verified out-of-band, provider gaps recorded not papered over | — | ✅ GUI-36 | Win 11 x64 |

### Browsing & inspection

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| List / tree / du / stat | S3 (MinIO) | dir-view `ls`, `tree` shows folders, `du` counts objects+bytes, bucket `stat` shows region | ✅ CLI-S3-06 | — (sweep: browse/tree guards, SWEEP-LIVE-01) | Win 11 x64 |
| Remote browse | FTP | `ls`/`du`/`stat`/`tree` on the remote engine | ✅ CLI-X-04 | ✅ GUI-04 (browse FTP seed dir through the tree) | Win 11 x64 |
| **FTP sources survive idle control-connection drops** | **FTP** | vsftpd drops idle control connections after ~5 minutes and the drop can be silent — the next operation meets a dead socket; the engine redials once on the stored source settings and answers (listing verified after the drop, write verified on the fresh connection); server 4xx/5xx replies about the filesystem are never mistaken for a dead connection | ✅ Go test: pkg/core/remotefs/ftp_test.go TestFTPRedialAfterControlDrop | ✅ GUI-50 (the battery's FTP leg runs ~7 min after its last FTP touch — every run exercises the redial for real) | Win 11 x64 |
| Deep find | S3 (MinIO) | `--name` glob/substring, `--smaller`, `--limit`, subtree prefix, summary line | ✅ CLI-S3-19 | — (sweep: Ctrl+Shift+F dialog, SWEEP-LIVE-01) | Win 11 x64 |
| find size + time filters | S3 (MinIO) | controlled prefix (1 big + 1 small): `--larger`/`--smaller` counts; `--newer 1h` finds both; `--older 1h` finds none | ✅ CLI-S3-28 | — | Win 11 x64 |
| **Pagination across the 1000-key page boundary** | **S3 (MinIO)** | 1006 objects (incl. one empty): recursive `ls` returns every one across the S3 1000-key page boundary; the last object stays findable; a gated mass delete clears them all | ✅ CLI-S3-30 | — | Win 11 x64 |
| **Hostile key names round-trip** | **S3 (MinIO)** | spaces, unicode, `%2F`-literal, `+ = &`, leading dot, 150-char names, 12-deep nesting, quotes/apostrophes (remote-only): exact-name listing, per-key `stat`, byte round-trip of the legal set, and a presigned fetch of the `%2F` hazard (must never decode into a slash) | ✅ CLI-S3-31 | — | Win 11 x64 |
| Create folder marker | S3 (MinIO) | `mkdir docs/` → zero-byte marker lists as a folder | ✅ CLI-S3-03 | ✅ GUI-07 (empty-area context menu → New folder; CLI `ls` sees the marker) | Win 11 x64 |
| New file dialog (cancel + create) | S3 (MinIO) | Shift+F4 prompt (defaults new-file/txt); Cancel creates NOTHING (CLI 404); then create for real; CLI `stat` sees it | — | ✅ GUI-19 | Win 11 x64 |
| **Editor auto-upload round-trip (WinSCP flow)** | **S3 (MinIO)** | EditObject through the bridge: stages the object, hands it to the OS (an inert `.cmd` probe — the handoff is real, the "editor" is a no-op), watches the file; bytes written to the staged path upload automatically (CLI byte-verified); StopEdit ends the session | — | ✅ GUI-23 | Win 11 x64 |
| Workbench surfaces | S3 (MinIO) | View → Transfers opens the manager popout; dual-pane toggle; filter box narrows the grid and clearing restores it | — | ✅ GUI-18 | Win 11 x64 |
| **Selection mechanics: plain anchor, shift-range, ctrl-toggle, invert, select-all** | **S3 (MinIO)** | six-file folder: plain click anchors (1 of 6), shift-click selects the exact range (3 of 6), ctrl-click drops the middle (2 of 6), Ctrl+I flips to the exact complement (4 of 6), Ctrl+A selects all (6 of 6) — every count from the visible selection bar | — | ✅ GUI-28 | Win 11 x64 |
| **`--json` machine contract** | **S3 (MinIO)** | every read command (ls/tree/du/stat/versions ls/bucket info) under `--json`: exit 0, zero ANSI escapes, non-empty, and the whole output parses as one JSON document (array or object) whose rows pass semantic checks (counts, keys, version IDs) — scripts may rely on the shape | ✅ CLI-S3-42 | — | Win 11 x64 |
| Deep search: token + CancelSearch contract | S3 (MinIO) | DeepSearch returns a live token and registers a search task; CancelSearch stops it cleanly; canceling an UNKNOWN token also resolves — a bogus cancel may never throw or wedge the registry | — | ✅ GUI-40 | Win 11 x64 |
| **Copy As: exact text shapes for name / path / S3 URI** | **S3 (MinIO)** | a clipboard spy on the bridge binding captures exactly what Copy name / Copy path / Copy S3 URI write: bare name, `bucket/key`, `s3://bucket/key` — single row exact, and a multi-selection joins with newlines in row order | — | ✅ GUI-42 | Win 11 x64 |
| **Copy URL: the real address, resolved from the viewing source** | **S3 (MinIO) + FTP** | the bridge clipboard spy again: Copy URL on an S3 row writes exactly `http://localhost:9000/bucket/key` — the source's OWN endpoint and addressing style, no signature query — and a bare out-of-band GET on that address is answered 403: the address is real, the grant is not; on an FTP file row it writes `ftp://user@host:port/server-path` — username and non-default port from the source's own connection fields, the anchored server path, never the password | — | ✅ GUI-49 + GUI-50 | Win 11 x64 |

### Transfers — the critical jobs

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Single upload | S3 (MinIO) | `cp` one file → `stat` reports size | ✅ CLI-S3-04 | ✅ GUI-10 (dual-pane DnD of a unicode filename; CLI sees it; bytes identical) | Win 11 x64 |
| Multi upload (recursive) | S3 (MinIO) | `cp -r` fixture tree (10 files: unicode, empty file, nested dirs) → recursive `ls` count matches | ✅ CLI-S3-05 | — (sweep: folder-row DnD upload ×3 engines, SWEEP-LIVE-01) | Win 11 x64 |
| **S3 folder markers never ride transfers as files** | **S3 (MinIO)** | transfer planning (copy/move) treats zero-byte folder markers as folders, never payload: marker keys are excluded from the plan, so a tree copy recreates the folder structure without uploading marker objects as files | ✅ Go test: pkg/api/xfer_markers_test.go | — (sweep: folder-row DnD ×3 engines, SWEEP-LIVE-01) | Win 11 x64 |
| Single download | S3 (MinIO) | `cp` object → local; bytes identical | ✅ CLI-S3-07 | ✅ GUI-11 (DnD S3 row → local pane; file lands on disk; bytes identical) | Win 11 x64 |
| Multi download (recursive) | S3 (MinIO) | `cp -r` prefix → local dir; full tree diff byte-identical | ✅ CLI-S3-08 | — (sweep, SWEEP-LIVE-01) | Win 11 x64 |
| Server-side copy S3→S3 | S3 (MinIO) | `cp s3://→s3://` lands a copyable object | ✅ CLI-S3-09 | — | Win 11 x64 |
| cp flag contracts | S3 (MinIO) | `--dry-run` prints the plan but lands nothing (stat 404); `--no-clobber` skips an overwrite (documented skip semantics: exit 0, object bytes untouched); `--force` overwrites | ✅ CLI-S3-27 | ✅ GUI-13 (conflict dialog on overwrite → Start → next version) | Win 11 x64 |
| **Multi-file copy local→FTP** | **FTP** | `cp -r` fixture tree (10 files: unicode, empty file, nested dirs) → `xf://`; every fixture key verified in the remote listing | ✅ CLI-X-02 | — (sweep: DnD upload × FTP, SWEEP-LIVE-01) | Win 11 x64 |
| **Multi-file copy FTP→S3 (cross-engine)** | **FTP → S3** | `cp -r` the FTP tree into the bucket; count + full byte round-trip back to disk | ✅ CLI-X-03 | ✅ GUI-04 (ctrl-click 2 files; Ctrl+C; S3 folder; Ctrl+V; rows land) + ✅ GUI-05 (downloaded via CLI, byte-identical) | Win 11 x64 |
| SFTP round-trip | SFTP | upload tree → download tree → byte-identical diff | ✅ CLI-X-05 | — (sweep: sftp round-trip, 9 files, SWEEP-LIVE-01) | Win 11 x64 |
| WebDAV round-trip | WebDAV | upload tree → download tree → byte-identical diff | ✅ CLI-X-06 | — (sweep: webdav round-trip, 9 files, SWEEP-LIVE-01) | Win 11 x64 |
| Cross-engine move (mv) | SFTP → FTP | `mv -r` sftp tree → ftp; source gone; destination byte-identical | ✅ CLI-X-07 | — (sweep: cross-engine DnD keeps the tree, SWEEP-LIVE-01) | Win 11 x64 |
| **Hostile filenames cross-engine** | **SFTP/FTP/WebDAV/S3** | names that stress protocol and shell escaping — spaces, `# & % + ^ $ !`, apostrophes, brackets, semicolons, CJK, a 120-char name — round-trip through EVERY live engine and S3 with names and bytes intact (JSON-escaped listing match + full tree diff) | ✅ CLI-X-11 | — | Win 11 x64 |
| Rename (mv) | S3 (MinIO) | `mv` object → new key; old gone, new stats | ✅ CLI-S3-13 | ✅ GUI-08 (F2 → new name; CLI `stat` sees the new key) | Win 11 x64 |
| sync (repair / no-op / new / --delete) | S3 (MinIO) | repairs a deletion, reports 0 in sync, 1 after local add, `--delete` removes the extra remote | ✅ CLI-S3-14 | — | Win 11 x64 |
| Presign + fetch | S3 (MinIO) | `presign` → plain HTTP GET returns identical bytes | ✅ CLI-S3-15 | — | Win 11 x64 |
| **Presign expiry: granted TTL + fails closed** | **S3 (MinIO)** | a 2-second grant: the URL carries exactly `X-Amz-Expires=2` and serves while live; after expiry the SAME URL is refused (providers with a clock-skew grace that keep serving record the gap as SKIP) | ✅ CLI-S3-34 | — | Win 11 x64 |
| **Pre-sign URL dialog (GUI)** | **S3 (MinIO)** | context menu → Pre-sign URL: the dialog exposes a READ-ONLY signed URL that a bare HTTP client outside the app fetches for the exact object bytes | — | ✅ GUI-24 | Win 11 x64 |
| Storage-class conversion | S3 (MinIO) | `sc` single → REDUCED_REDUNDANCY visible in `find --class`; recursive dry-run gate | ✅ CLI-S3-18 | — (sweep: storage-class dialog, SWEEP-LIVE-01) | Win 11 x64 |
| **Multipart large-object round-trip** | **S3 (MinIO)** | 32 MiB random object (7+ multipart parts at the 5 MiB part size): upload → stat → download is sha256-identical — integrity is byte-level, not size-level | ✅ CLI-S3-29 | — | Win 11 x64 |
| **SSE-S3 server-side encryption** | **S3 (MinIO)** | `cp --sse AES256` uploads with the SSE header and round-trips byte-identical (an engine without KMS rejects SSE — recorded as a provider-gap SKIP, never a silent pass) | ✅ CLI-S3-33 | — | Win 11 x64 |
| **Concurrency: parallel workload + same-key race** | **S3 (MinIO)** | 5 CLI processes at once (3 uploads, 1 download, 1 listing) all succeed byte-exact; two simultaneous writes to ONE key serialize into clean versions — never a torn object | ✅ CLI-S3-32 | — | Win 11 x64 |
| **Conflict matrix: per-file skip + rename** | **S3 (MinIO)** | both dragged files conflict; a.txt→skip keeps v1 untouched (still 1 version, original bytes); b.txt→rename keeps the original AND lands the new bytes beside it | — | ✅ GUI-20 | Win 11 x64 |
| **Cancel mid-transfer: no corrupt object; retry clean** | **S3 (MinIO)** | 8 MiB upload throttled to 256 kB/s; Cancel while running → job canceled and the object ABSENT (no partial lands); the unthrottled retry is sha-identical | — | ✅ GUI-21 | Win 11 x64 |
| **Cancel mid-batch: finished files stay, the canceled one never lands** | **S3 (MinIO)** | five 2 MiB files drag-dropped as ONE batch job under a 128 kB/s throttle: canceled once the first file fully landed — exactly the finished file(s) exist remotely (byte-identical, CLI-verified) while in-flight and queued ones are absent with no partial left behind | — | ✅ GUI-34 | Win 11 x64 |
| **Pane compare: all six categories exact** | **S3 (MinIO) + local** | a hand-built pair of dirs covering EVERY compare class — identical, only-left, only-right, different-size, newer-left, newer-right — the Compare button counts each category exactly 1 | — | ✅ GUI-25 | Win 11 x64 |
| **Ctrl+C stages HIDDEN — nothing lands until Paste** | **S3 (MinIO)** | the two-part copy contract: Ctrl+C on remote rows runs the Explorer mirror as a HIDDEN staging job (a scratch download for paste-out, invisible in every list) whose staged bytes are byte-verified on disk, while the bucket stays bit-for-bit unchanged until a Paste | — | ✅ GUI-26 | Win 11 x64 |
| **Download-side conflict matrix** | **S3 (MinIO) → local** | three remote rows dragged onto a local folder holding two of the same names: skip keeps the local file untouched, rename lands a twin with the remote bytes, the clean third file downloads without a prompt | — | ✅ GUI-27 | Win 11 x64 |
| **cp boundary sizes: 0 / 1 byte / exactly 5 MiB** | **S3 (MinIO)** | the multipart boundary is a cliff, not a slope: a 0-byte object, a 1-byte object and an EXACTLY 5 MiB object each upload and `stat` reports the exact byte count (`(0 bytes)`, `(1 bytes)`, `(5242880 bytes)`); the 5 MiB object downloads sha256-identical | ✅ CLI-S3-43 | — | Win 11 x64 |
| Transfer-manager hygiene: ClearFinishedTransfers | S3 (MinIO) | a bridge-driven Upload runs to completion; ClearFinishedTransfers([id]) prunes the finished job from the manager (an empty list is a no-op by contract — null means all); the uploaded object is proven present by the CLI | — | ✅ GUI-39 | Win 11 x64 |

### Deletion — gated destruction, verified cancellations

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| **Single object deletion** | **S3 (MinIO)** | `rm` one object → gone from `ls`; versioned bucket → delete marker recorded in the timeline | ✅ CLI-S3-10 | ✅ GUI-06 (Del → Delete Window, marker default → confirm; row gone; CLI timeline shows the marker) | Win 11 x64 |
| Recursive deletion + safety gates | S3 (MinIO) | 55-object prefix: `rm -r` without `--force` rejected (>50 L1 gate); `--dry-run` total counts files + folder; `--force` deletes exactly that many | ✅ CLI-S3-11 | — (sweep: Delete Window marker→badge→permanent, SWEEP-LIVE-01) | Win 11 x64 |
| Remote delete gates | FTP | `rm -r` dry-run counts; `--force` deletes; prefix gone | ✅ CLI-X-04 | — | Win 11 x64 |
| **Delete Window CANCEL keeps the object** | **S3 (MinIO)** | Del on a row opens the Delete Window; Cancel/Esc leaves the object untouched on BOTH faces — the cancellation contract | — | ✅ GUI-14 | Win 11 x64 |
| **L2 identity gate: Empty-bucket window** | **S3 (MinIO)** | the destructive Empty-bucket window demands the bucket's OWN name: a wrong word keeps the destructive button disabled; Cancel preserves every object | — | ✅ GUI-22 | Win 11 x64 |
| **Multi-delete ladder: markers keep history; Shift+Del destroys permanently** | **S3 (MinIO)** | four objects selected at once: the Delete Window counts all four and offers all three delete types; plain marker delete hides every object while each full timeline survives (CLI re-reads data version + delete marker per object); a second batch through Shift+Del destroys versions entirely | — | ✅ GUI-29 | Win 11 x64 |
| **Delete-marker windows: merged multi view, single-object view, Undo delete restores** | **S3 (MinIO)** | two marker-carrying objects selected together open the merged Delete markers window — both keys listed, bulk checkboxes, Remove selected un-deletes both through its confirm (CLI re-reads both timelines); one object alone opens the fitted single view — no bulk checkboxes, Close footer, per-row Remove — and the window closes itself after the last undo, the data timeline intact | — | ✅ GUI-51 | Win 11 x64 |
| **L2 execution: Empty bucket destroys EVERY version, keeps the bucket** | **S3 (MinIO)** | scratch bucket seeded with nested objects, extra old versions and delete markers: typing the bucket's own name arms the Empty-bucket window and executes — nothing lists anywhere afterwards (recursive listing empty, version statistics zero, sampled timelines empty) yet the bucket survives and still takes writes | — | ✅ GUI-33 | Win 11 x64 |
| **Delete Window keepcurrent mode: history destroyed, current version survives** | **S3 (MinIO)** | three CLI-seeded versions behind a Delete Window switched to "keep current": the typed-word gate arms, execution leaves exactly ONE version (the current bytes, no delete marker) — the timeline is gone, the file is not | — | ✅ GUI-37 | Win 11 x64 |

### Versions — the safety ladder

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Version timeline: restore + undo | S3 (MinIO) | overwrite → 2 versions; restore v1 as latest; `rm` → marker; `undo` removes the marker and serves v1 bytes | ✅ CLI-S3-16 | ✅ GUI-12 (Versions dialog → Restore as latest on the older one; CLI downloads the restored bytes) | Win 11 x64 |
| Purge + permanent destroy | S3 (MinIO) | `versions stat`; purge noncurrent — >50 gate demands `--force`, then purges; `versions rm --all` empties the timeline (L3) | ✅ CLI-S3-17 | — (sweep, SWEEP-LIVE-01) | Win 11 x64 |
| Versioned migration (cp/mv --versions) | S3 (MinIO) | 2 versions at source; `cp --versions` s3→s3 copies the full timeline; `mv --versions` moves it; unversioned destination refuses (gate) | ✅ CLI-S3-26 | — | Win 11 x64 |
| **Versions dialog: A/B compare diff + per-version Destroy** | **S3 (MinIO)** | three CLI-seeded text versions: timeline lists all three; oldest as A vs newest as B shows both sides' changed lines in the diff; destroying the oldest through its confirmation window drops the timeline to exactly two, re-read by CLI | — | ✅ GUI-30 | Win 11 x64 |
| **Versioned copy: the full timeline S3→S3 (the migrator)** | **S3 (MinIO)** | an object with three versions pasted into a fresh versioned bucket: version-choice dialog appears, the GUI carries the ENTIRE timeline across in a background job; destination lists all three versions and the current bytes round-trip | — | ✅ GUI-31 | Win 11 x64 |
| **Versions dialog: Undo delete removes the marker only** | **S3 (MinIO)** | an object with data → marker → newer data: the Versions dialog's Undo delete on the marker row removes ONLY the marker — the timeline drops to the two data versions and the served bytes are the pre-marker version, CLI-verified | — | ✅ GUI-38 | Win 11 x64 |

### Resilience — when the link misbehaves

Fault injection via `scripts/faultproxy.mjs` (HTTP control plane, live mode
switching; the same S3 source stays connected through it).

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Slow link | S3 via faultproxy | +600 ms latency per chunk: listing completes with correct output, measurably slower | ✅ CLI-RES-01 | — (sweep: skeleton-rows + paced listing, SWEEP-LIVE-01) | Win 11 x64 |
| Dead link | S3 via faultproxy | RST mid-session → non-zero exit, classified error, no partial success | ✅ CLI-RES-02 | — (sweep: Retry recovers, SWEEP-LIVE-01) | Win 11 x64 |
| Blackhole | S3 via faultproxy | endpoint never answers → stream watchdog inside the `--timeout` budget (no 5-minute hang) | ✅ CLI-RES-03 | — (sweep, SWEEP-LIVE-01) | Win 11 x64 |
| **Hard kill mid-upload: no partial object** | **S3 via faultproxy** | a 64 MiB upload killed mid-flight (process termination) lands NO object — no corrupt or half-written key ever becomes visible; the clean retry is sha-identical | ✅ CLI-RES-04 | — (sweep: GUI cancel mid-transfer, GUI-21) | Win 11 x64 |
| **Transient 5xx storm: absorbed / exhausted** | **S3 via faultproxy** | a new faultproxy `flap` mode (canned `HTTP 503` × N): a 2-failure storm is retried away INSIDE the SDK budget (the put lands byte-identical, sha-verified); a 50-failure storm exhausts it — non-zero exit, a surfaced error, and NO half-landed object | ✅ CLI-RES-05 | — | Win 11 x64 |
| **Blackholed transfer: `--timeout` bounds the hang, no partial lands** | **S3 via faultproxy** | an endpoint that never answers would hang a transfer for the default 5 minutes: `--timeout 8s` cuts it to a fast, clean non-zero failure well inside the budget, the half-sent object does NOT exist — and the healthy-path retry lands afterwards (a timeout may cost the attempt, never the store's integrity) | ✅ CLI-RES-06 | — | Win 11 x64 |

### Meta — the binary and the stack

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Version identity | — | `version` prints the build version, exit 0 | ✅ CLI-M-01 | ✅ GUI-01 (GetVersion binding round-trips it) | Win 11 x64 |
| Usage-error contract | — | unknown command → exit 2, stderr reads `usage error:` and points at `--help` | ✅ CLI-M-02 | — | Win 11 x64 |
| Shell completion | — | `completion bash` emits a working completion script; other shells answer too | ✅ CLI-M-03 | — | Win 11 x64 |
| **Secrets never printed** | **S3 (MinIO)** | a source with a distinctive secret key: every output surface — `source list` (text + json), `source test`, the 403 transfer path, `profile list`, activity log — must run but never echo the secret, even on failure paths | ✅ CLI-M-04 | — | Win 11 x64 |
| **Secrets encrypted at rest** | **config store + OS keyring** | a distinctive secret added to the store, then a RAW byte-scan of every file under the whole config directory (recursive): the secret may live only inside the OS keyring — never on disk (SKIP under `S3B_NO_KEYRING`, the documented headless plaintext mode) | ✅ CLI-M-05 | — | Win 11 x64 |
| **Legacy store migration: profiles seed as sources, plaintext secrets leave the file** | **S3 (MinIO)** | hand-written LEGACY `profiles.json` (two profiles, inline secrets, one with a session token, no sources array): one CLI load seeds both as data sources (M8); keyring takes every secret and the token, none remain in the file (M5); the token-less source dials through the keyring-held secret, the token profile fails with an invalid-token refusal (the synthetic token being in the signature proves it came from the keyring); both profiles removed afterwards | ✅ CLI-M-06 | — | Win 11 x64 |
| `--help` contract: every command self-documents | — | all 22 top-level commands answer `--help` with a usage line; the root `--help` lists every one of them | ✅ CLI-M-07 | — | Win 11 x64 |
| Live stack boots | Wails v3 server | `/health` ok; page loads; bridge surface; GetVersion | — | ✅ GUI-01 | Win 11 x64 |
| Page-error gate | Wails v3 server | zero uncaught page errors across the whole GUI battery | — | ✅ GUI-09 | Win 11 x64 |
| Activity log | S3 (MinIO) | `log` shows the operations this run performed | ✅ CLI-S3-23 | — (sweep: Ctrl+L log area, SWEEP-LIVE-01) | Win 11 x64 |
| Full visual sweep | shim world | every dialog, popout, menu and viewport contract | — | ✅ SWEEP-VIS-01 | Win 11 x64 |
| Full live walk | real engines | real bindings: transfers, versions, profiles, i18n, fault lab | — | ✅ SWEEP-LIVE-01 | Win 11 x64 |
| **Hidden provenance markers: covert watermarks + hidden binary readout** | **all sources** | the creator/license identity (PolyForm Internal Use 1.0.0, see LICENSE/NOTICE) hides as zero-width-encoded watermarks on comment lines of nine core files — invisible in editors and diffs, surviving code copies after visible attribution is stripped; two guards decode all nine and fail on loss or drift, so refactors cannot strip them silently; a hidden `s3b provenance` command (absent from --help) prints the same identity plus build stamp from any built binary | ✅ Go test: internal/provenance TestProvenanceWatermarks + `node scripts/provenance.mjs check` | — (readout is CLI-only; the watermarks also ship inside the embedded frontend) | Win 11 x64 |
| **Help → License window: empty popout fixed, About partition added, license name linked** | **Wails v3 server** | the native window payload itself (?popout=license) renders the three-partition window — About (version from GetVersion, publisher, license + project links), License (the name itself linked to the published PolyForm text, canonical URL shown), Third-party components — where the desktop app previously showed a dead "Unknown popout" page (the dispatcher had no license case; the sweeps only exercise the DOM fallback); external links route through the OpenExternal binding whose scheme allow-list refuses file:// and every non-http(s) URL, so a webview link can never reach the shell | ✅ targeted rig check 20/20 (Playwright on the exact popout URL: partitions, default tab, both link targets, guard refusal without an actual open, guide regression, zero page errors) + ✅ GUI-52 added to the battery | — (guard proven by refusal, never by opening anything) | Win 11 x64 |
| **First-launch license gate: the setup phase every fresh install walks** | **Wails v3 server + CLI** | a fresh config boots into the full-screen accept-license gate — boot is held behind it (status bar unstamped), Escape cannot dismiss it, Decline swaps to a blocked state whose only exit is Exit, and a reload walks the gate again with no record written; Accept unlocks the app and writes license.json (timestamp, OS user, face) which the CLI face reads back — the same record both faces share; an accepted install boots straight in until `s3b license decline` re-arms the gate, and the terminal round-trip (status → accept --yes → status --json → decline) proves the store from the CLI side | ✅ GUI-53 + ✅ CLI-M-08 (+ targeted rig check 16/16 before the matrix: gate shape, Escape, blocked state, cross-face record, re-arm) | — | Win 11 x64 |
| **Exit gates: busy work survives ExitApp and the close request** | **S3 (MinIO)** | with a throttled transfer CONFIRMED running: ShouldClose answers true with a live-work reason; ExitApp only emits exit:confirm — the server stays healthy, the job keeps running (never confirmed away from the harness side); CancelTransfer cancels it and NO partial object lands | — | ✅ GUI-43 | Win 11 x64 |
| Settings round-trips: log-file prefs + tuning | Wails v3 server | SetLogSettings(off) reads back off and the original restores exactly; SetTuning echoes every requested value (floors respected) and the originals restore — settings are reversible state, never one-way doors | — | ✅ GUI-45 | Win 11 x64 |
| Secure storage toggle round-trip | OS keyring (Windows) | GetSecureStorage reports availability; where the keyring is usable the toggle flips and restores with every read consistent — where it is not, that is recorded, never papered over | — | ✅ GUI-46 | Win 11 x64 |
| Language switch: Finnish UI, then back | Wails v3 server | `s3b-lang=fi` + reload: the grid's own chrome switches (Finnish present, English absent); `en` restores — i18n actually drives the shipped UI | — | ✅ GUI-47 | Win 11 x64 |
| Task registry: running view + ClearFinishedTasks | S3 (MinIO) | a deep search registers as a running task (kind search); once finished, ClearFinishedTasks(null) prunes EVERY finished row (null = all; an empty list is a no-op by contract) — the registry mirrors live work and forgets dead work on command | — | ✅ GUI-48 | Win 11 x64 |
| Context menu closes on every outside click | S3 (MinIO) | the open context menu dismisses on a plain click outside it — grid, tree and the hidden-objects world (markers, empty states) never leave an orphaned menu behind | — | — (sweep: ctxmenu-click-outside, SWEEP-VIS-01) | Win 11 x64 |
| Sidebar-originated deletes refresh the sidebar | S3 (MinIO) + remote | deleting a folder through the sidebar tree refreshes the sidebar itself — the folder's subtree disappears from the tree — while the main view's breadcrumb stays where it was | — | — (sweep: tree-delete-syncs-sidebar, SWEEP-VIS-01) | Win 11 x64 |
| Monitoring windows: 490×300 footprint, auto-height, row detail panels | Wails v3 server | the monitor popouts (transfers, searches) open at their 490×300 footprint — also their resize floor — grow with content instead of scrolling it away, expand per-row detail panels in place, collapse long lists behind "+N more", and carry the header strip that identifies the window | — | — (sweep: popout-auto-height + popout-row-details + popout-window-views, SWEEP-VIS-01) | Win 11 x64 |

---

## SKIP policy — honest gaps, never silent passes

A row records **SKIP** only for conditions outside s3b's control, and the
reason is written into the verification report:

- **Engine down** — a container (SFTP/FTP/WebDAV) is not reachable; the
  cross-engine rows SKIP instead of failing.
- **Provider API gap** — the engine answers but rejects a valid request.
  The one live example: this MinIO build returns `400 InvalidArgument` for
  valid lifecycle rules, so CLI-S3-25 records SKIP on `put` (the `get`/`
  delete` halves still run). Against a provider that implements the API,
  the row passes fully — the SKIP is proof of a recorded provider gap,
  not a waived check.

## The release pipeline — no report, no release

Verification needs the live engine containers and a browser, so it runs on
the release machine, not in CI. The two halves meet in the middle:

1. **Pre-tag (release machine):** `node scripts/verify.mjs --release <tag>`
   stamps the tag into the binaries exactly as the release workflow does
   (`main.version=<tag without v>`), runs the full matrix from a fresh
   build, and writes the committed report —
   `docs/verification/<tag>/<os>-<arch>/` (`REPORT.md` with build + OS +
   the certificate, `verification.json` as the machine copy) plus the index
   at [`docs/verification/README.md`](verification/README.md). Commit the
   report, then push the tag: the tag must contain its own report.
2. **Publish (CI):** the release job in
   [`.github/workflows/release.yml`](../.github/workflows/release.yml)
   checks out the tagged tree and refuses to publish unless
   `docs/verification/<tag>/verification.json` exists, is a full-matrix
   run, and has zero FAIL rows — then links the report at the top of the
   release notes.

`--release` refuses `--only`/`--quick`/`--no-build`/`--skip-gui` — release
evidence is always the whole matrix from a fresh build, and its version
stamp is binary-verified (CLI-M-01 requires the binary to print exactly the
stamp). The step-by-step checklist lives in CONTRIBUTING.md, "Cutting a
release".

## Latest verification report

Replaced on every run — this snapshot is from the release gate of
**24 Sep 2026** (`node scripts/verify.mjs --release v1.1.0-beta.18`,
tag-stamped, binary-checked, on the tree of `81fd1b6`) on Windows
Server 2025 (x64), after the license work grew the matrix from 121 to
124 rows: the rebuilt Help → License window — About | License |
Third-party partitions, license name and project URL as external
links (GUI-52) — the first-launch accept-license phase every fresh
install now walks (GUI-53: the gate holds boot, Escape cannot dismiss
it, Decline blocks with Exit the only way out, a reload re-arms, and
acceptance is recorded cross-face in `license.json`), and the license
CLI face round-trip (CLI-M-08). The visual sweep's license check was
partitioned across the window's three tabs — the NOTICE summary lives
on its own — which is the 656 → 658 growth. The same sitting found and
fixed a latent release-pipeline bug: the workflow's verification gate
now locates the evidence by glob (`verification.json` under the tag)
instead of assuming it at the tag root, where it would have missed the
`windows-x64/` directory every real report lives in. Evidence committed
for the tag at
[`docs/verification/v1.1.0-beta.18/windows-x64/REPORT.md`](verification/v1.1.0-beta.18/windows-x64/REPORT.md)
(the previous release evidence, beta.17's 94-row matrix, stays at
[`docs/verification/v1.1.0-beta.17/windows-x64/REPORT.md`](verification/v1.1.0-beta.17/windows-x64/REPORT.md)):

```
release gate (--release v1.1.0-beta.18; fresh build; 124 rows incl. the two sweep rows):
  117 PASS · 7 SKIP · 0 FAIL — 1342 s
  (the 7 SKIPs are the recorded MinIO provider gaps: lifecycle put,
   SSE-S3, CORS put, website put and encryption put on the CLI, plus
   the CORS and website admin tabs behind the same refused APIs)
  SWEEP-VIS-01  gui-visual   658/658 checks
  SWEEP-LIVE-01 gui-v3live   142 checks, no page errors
```

Run it yourself: `node scripts/verify.mjs` and read the table it prints,
plus `testartifacts/verification/verification.json` for the machine copy
(fields: version, os, node, runId, bucket, category, summary, rows — one
entry per row with result and detail).
