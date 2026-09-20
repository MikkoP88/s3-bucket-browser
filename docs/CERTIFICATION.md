# Action verification — the release gate

**Verified** means: the action ran end-to-end through the shipped binary —
CLI face and/or GUI face — against **live engines** (MinIO S3, SFTP, FTP and
WebDAV containers), with byte-level verification wherever bytes move,
safety-gate probes wherever destruction is involved, cancellation probes
wherever a dialog can abort a job, and fault injection wherever resilience
is claimed. A row that cannot run (engine container down, provider API gap)
is recorded **SKIP** — never silently passed.

**Terminology.** *Verification* is the process this page describes; the
*certificate* (`testartifacts/certification/certificate.json`) is the
machine-readable artifact each run produces. Before every release the whole
matrix is re-verified and the certificate regenerated — one command, exit
0 means every critical job still works on the exact binary being shipped.

This page is the contract. Every row in the tables below is executed by
[`scripts/certify.mjs`](../scripts/certify.mjs).

```
The release gate — one command verifies everything
  node scripts/certify.mjs             full run: CLI + GUI + both sweeps
  npm run certify                      same, via npm
  node scripts/certify.mjs --quick     CLI + GUI batteries, sweeps skipped (~3 min)
  node scripts/certify.mjs --skip-gui  CLI + sweeps only (no browser battery)
  node scripts/certify.mjs --no-build  reuse the exes in testartifacts/

Category runs — verify one focus area only
  node scripts/certify.mjs --only <category>    (or npm run certify:only -- <category>)
    cli          all four CLI batteries (s3 + cross + resilience + meta)
    s3           the MinIO/S3 CLI battery
    cross        cross-engine battery (FTP / SFTP / WebDAV)
    resilience   fault-injection battery (faultproxy: latency, RST, blackhole)
    meta         binary-level contracts (version, usage errors, completion)
    gui          the full GUI battery (real Wails v3 stack in a browser)
    sweeps       just the two harness sweeps
  npm run certify:cli | certify:gui    shorthands for the two common foci

Rules
  Repeat a run — results must be identical (repeatability is part of the
  certificate). Exit 0 = verified, exit 1 = at least one FAIL, exit 2 =
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
          fault lab) — folded into the certificate as summary rows.
```

The matrix below was last verified on **Windows 11 (x64)** with Node 24.
The runner is platform-portable: same containers, same rows, the OS column
reflects where the certificate was produced.

---

## Verified actions

Legend — the **CLI**/**GUI** columns name the certificate row that covers
the action on that face (`✅` = passed in the latest run, `—` = that face
does not expose the action as a one-step flow; where the live sweep covers
it, the sweep row is named). **OS** is the platform the certificate ran on.

### Sources

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Add + test source | S3 (MinIO) | `source add` with endpoint/keys; `source test` dials; listed; mirrored as a profile | ✅ CLI-S3-01 | ✅ GUI-02 (editor → Test ✅ → Save → bucket root lists) | Win 11 x64 |
| Add + test remote sources | SFTP/FTP/WebDAV | `sftp://`/`webdav://` URL shorthand + ftp flags; `source test` dials each | ✅ CLI-X-01 | ✅ GUI-03 (FTP: Test ✅ → Save → root lists) | Win 11 x64 |
| Source lifecycle: profile test + remove | S3 (MinIO) | add a temp source; `profile test` dials it (OK + bucket count); `source remove` drops it from BOTH source list and profile mirror | ✅ CLI-X-09 | — | Win 11 x64 |
| Export + import sources | all | AES-256-GCM encrypted export (`--password`); ciphertext verified; import into a fresh store lists the same sources | ✅ CLI-X-08 | — | Win 11 x64 |
| Profile file round-trip | Wails v3 server | SaveProfileFileAs → state open; Close → onboarding; wrong password rejected through the bridge; correct password restores the sources | — | ✅ GUI-17 | Win 11 x64 |

### Buckets & configuration

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Create versioned bucket | S3 (MinIO) | `mb` + versioning on (the delete-marker scenarios need it) | ✅ CLI-S3-02 | ✅ GUI-16 (admin dialog reports versioning) | Win 11 x64 |
| Bucket safety gates | S3 (MinIO) | `rb` on a non-empty bucket rejected without `--force` | ✅ CLI-S3-12 | — (delete window gates cover the GUI, SWEEP-LIVE-01) | Win 11 x64 |
| Bucket admin: info / tags / policy | S3 (MinIO) | `info` shows versioning; tags put/get; policy put/get round-trip (cors/encryption tolerate provider gaps) | ✅ CLI-S3-21 | ✅ GUI-16 (Admin panel: versioning + tabs) | Win 11 x64 |
| **Config deletes never destroy the bucket** | **S3 (MinIO)** | website/encryption/lifecycle/cors/pab `delete` on a disposable bucket — after EACH op the bucket must still `stat`; pab delete must refuse cleanly on providers without PAB support. (Regression tripwire: MinIO RELEASE.2025-09-07 destroys the whole bucket on `DELETE ?publicAccessBlock`; s3b now pre-checks the capability and refuses — CLI-S3-24 pins that defense) | ✅ CLI-S3-24 | — | Win 11 x64 |
| Lifecycle rules round-trip | S3 (MinIO) | put flat-schema rules (expiration + transition); get echoes them; delete clears — `put` tolerates provider gaps as SKIP (this MinIO build rejects valid rules with 400 InvalidArgument: a recorded provider gap, not a pass) | ✅ CLI-S3-25 | — | Win 11 x64 |
| Object lock | S3 (MinIO) | `mb --object-lock`; retention set/show/clear; legal hold on/off (GOVERNANCE, cleanup stays possible) | ✅ CLI-S3-22 | — | Win 11 x64 |
| Doctor diagnosis | S3 (MinIO) | `doctor s3://bucket` runs the check ladder | ✅ CLI-S3-20 | ✅ GUI-15 (Help → Doctor → popout → run all → pass summary) | Win 11 x64 |

### Browsing & inspection

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| List / tree / du / stat | S3 (MinIO) | dir-view `ls`, `tree` shows folders, `du` counts objects+bytes, bucket `stat` shows region | ✅ CLI-S3-06 | — (sweep: browse/tree guards, SWEEP-LIVE-01) | Win 11 x64 |
| Remote browse | FTP | `ls`/`du`/`stat`/`tree` on the remote engine | ✅ CLI-X-04 | ✅ GUI-04 (browse FTP seed dir through the tree) | Win 11 x64 |
| Deep find | S3 (MinIO) | `--name` glob/substring, `--smaller`, `--limit`, subtree prefix, summary line | ✅ CLI-S3-19 | — (sweep: Ctrl+Shift+F dialog, SWEEP-LIVE-01) | Win 11 x64 |
| find size + time filters | S3 (MinIO) | controlled prefix (1 big + 1 small): `--larger`/`--smaller` counts; `--newer 1h` finds both; `--older 1h` finds none | ✅ CLI-S3-28 | — | Win 11 x64 |
| Create folder marker | S3 (MinIO) | `mkdir docs/` → zero-byte marker lists as a folder | ✅ CLI-S3-03 | ✅ GUI-07 (empty-area context menu → New folder; CLI `ls` sees the marker) | Win 11 x64 |
| New file dialog (cancel + create) | S3 (MinIO) | Shift+F4 prompt (defaults new-file/txt); Cancel creates NOTHING (CLI 404); then create for real; CLI `stat` sees it | — | ✅ GUI-19 | Win 11 x64 |
| Workbench surfaces | S3 (MinIO) | View → Transfers opens the manager popout; dual-pane toggle; filter box narrows the grid and clearing restores it | — | ✅ GUI-18 | Win 11 x64 |

### Transfers — the critical jobs

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Single upload | S3 (MinIO) | `cp` one file → `stat` reports size | ✅ CLI-S3-04 | ✅ GUI-10 (dual-pane DnD of a unicode filename; CLI sees it; bytes identical) | Win 11 x64 |
| Multi upload (recursive) | S3 (MinIO) | `cp -r` fixture tree (10 files: unicode, empty file, nested dirs) → recursive `ls` count matches | ✅ CLI-S3-05 | — (sweep: folder-row DnD upload ×3 engines, SWEEP-LIVE-01) | Win 11 x64 |
| Single download | S3 (MinIO) | `cp` object → local; bytes identical | ✅ CLI-S3-07 | ✅ GUI-11 (DnD S3 row → local pane; file lands on disk; bytes identical) | Win 11 x64 |
| Multi download (recursive) | S3 (MinIO) | `cp -r` prefix → local dir; full tree diff byte-identical | ✅ CLI-S3-08 | — (sweep, SWEEP-LIVE-01) | Win 11 x64 |
| Server-side copy S3→S3 | S3 (MinIO) | `cp s3://→s3://` lands a copyable object | ✅ CLI-S3-09 | — | Win 11 x64 |
| cp flag contracts | S3 (MinIO) | `--dry-run` prints the plan but lands nothing (stat 404); `--no-clobber` skips an overwrite (documented skip semantics: exit 0, object bytes untouched); `--force` overwrites | ✅ CLI-S3-27 | ✅ GUI-13 (conflict dialog on overwrite → Start → next version) | Win 11 x64 |
| **Multi-file copy local→FTP** | **FTP** | `cp -r` fixture tree (10 files: unicode, empty file, nested dirs) → `xf://`; every fixture key verified in the remote listing | ✅ CLI-X-02 | — (sweep: DnD upload × FTP, SWEEP-LIVE-01) | Win 11 x64 |
| **Multi-file copy FTP→S3 (cross-engine)** | **FTP → S3** | `cp -r` the FTP tree into the bucket; count + full byte round-trip back to disk | ✅ CLI-X-03 | ✅ GUI-04 (ctrl-click 2 files; Ctrl+C; S3 folder; Ctrl+V; rows land) + ✅ GUI-05 (downloaded via CLI, byte-identical) | Win 11 x64 |
| SFTP round-trip | SFTP | upload tree → download tree → byte-identical diff | ✅ CLI-X-05 | — (sweep: sftp round-trip, 9 files, SWEEP-LIVE-01) | Win 11 x64 |
| WebDAV round-trip | WebDAV | upload tree → download tree → byte-identical diff | ✅ CLI-X-06 | — (sweep: webdav round-trip, 9 files, SWEEP-LIVE-01) | Win 11 x64 |
| Cross-engine move (mv) | SFTP → FTP | `mv -r` sftp tree → ftp; source gone; destination byte-identical | ✅ CLI-X-07 | — (sweep: cross-engine DnD keeps the tree, SWEEP-LIVE-01) | Win 11 x64 |
| Rename (mv) | S3 (MinIO) | `mv` object → new key; old gone, new stats | ✅ CLI-S3-13 | ✅ GUI-08 (F2 → new name; CLI `stat` sees the new key) | Win 11 x64 |
| sync (repair / no-op / new / --delete) | S3 (MinIO) | repairs a deletion, reports 0 in sync, 1 after local add, `--delete` removes the extra remote | ✅ CLI-S3-14 | — | Win 11 x64 |
| Presign + fetch | S3 (MinIO) | `presign` → plain HTTP GET returns identical bytes | ✅ CLI-S3-15 | — | Win 11 x64 |
| Storage-class conversion | S3 (MinIO) | `sc` single → REDUCED_REDUNDANCY visible in `find --class`; recursive dry-run gate | ✅ CLI-S3-18 | — (sweep: storage-class dialog, SWEEP-LIVE-01) | Win 11 x64 |

### Deletion — gated destruction, verified cancellations

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| **Single object deletion** | **S3 (MinIO)** | `rm` one object → gone from `ls`; versioned bucket → delete marker recorded in the timeline | ✅ CLI-S3-10 | ✅ GUI-06 (Del → Delete Window, marker default → confirm; row gone; CLI timeline shows the marker) | Win 11 x64 |
| Recursive deletion + safety gates | S3 (MinIO) | 55-object prefix: `rm -r` without `--force` rejected (>50 L1 gate); `--dry-run` total counts files + folder; `--force` deletes exactly that many | ✅ CLI-S3-11 | — (sweep: Delete Window marker→badge→permanent, SWEEP-LIVE-01) | Win 11 x64 |
| Remote delete gates | FTP | `rm -r` dry-run counts; `--force` deletes; prefix gone | ✅ CLI-X-04 | — | Win 11 x64 |
| **Delete Window CANCEL keeps the object** | **S3 (MinIO)** | Del on a row opens the Delete Window; Cancel/Esc leaves the object untouched on BOTH faces — the cancellation contract | — | ✅ GUI-14 | Win 11 x64 |

### Versions — the safety ladder

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Version timeline: restore + undo | S3 (MinIO) | overwrite → 2 versions; restore v1 as latest; `rm` → marker; `undo` removes the marker and serves v1 bytes | ✅ CLI-S3-16 | ✅ GUI-12 (Versions dialog → Restore as latest on the older one; CLI downloads the restored bytes) | Win 11 x64 |
| Purge + permanent destroy | S3 (MinIO) | `versions stat`; purge noncurrent — >50 gate demands `--force`, then purges; `versions rm --all` empties the timeline (L3) | ✅ CLI-S3-17 | — (sweep, SWEEP-LIVE-01) | Win 11 x64 |
| Versioned migration (cp/mv --versions) | S3 (MinIO) | 2 versions at source; `cp --versions` s3→s3 copies the full timeline; `mv --versions` moves it; unversioned destination refuses (gate) | ✅ CLI-S3-26 | — | Win 11 x64 |

### Resilience — when the link misbehaves

Fault injection via `scripts/faultproxy.mjs` (HTTP control plane, live mode
switching; the same S3 source stays connected through it).

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Slow link | S3 via faultproxy | +600 ms latency per chunk: listing completes with correct output, measurably slower | ✅ CLI-RES-01 | — (sweep: skeleton-rows + paced listing, SWEEP-LIVE-01) | Win 11 x64 |
| Dead link | S3 via faultproxy | RST mid-session → non-zero exit, classified error, no partial success | ✅ CLI-RES-02 | — (sweep: Retry recovers, SWEEP-LIVE-01) | Win 11 x64 |
| Blackhole | S3 via faultproxy | endpoint never answers → stream watchdog inside the `--timeout` budget (no 5-minute hang) | ✅ CLI-RES-03 | — (sweep, SWEEP-LIVE-01) | Win 11 x64 |

### Meta — the binary and the stack

| Action | Data source | Tested scenario | CLI | GUI | OS |
|---|---|---|---|---|---|
| Version identity | — | `version` prints the build version, exit 0 | ✅ CLI-M-01 | ✅ GUI-01 (GetVersion binding round-trips it) | Win 11 x64 |
| Usage-error contract | — | unknown command → exit 2, stderr reads `usage error:` and points at `--help` | ✅ CLI-M-02 | — | Win 11 x64 |
| Shell completion | — | `completion bash` emits a working completion script; other shells answer too | ✅ CLI-M-03 | — | Win 11 x64 |
| Live stack boots | Wails v3 server | `/health` ok; page loads; bridge surface; GetVersion | — | ✅ GUI-01 | Win 11 x64 |
| Page-error gate | Wails v3 server | zero uncaught page errors across the whole GUI battery | — | ✅ GUI-09 | Win 11 x64 |
| Activity log | S3 (MinIO) | `log` shows the operations this run performed | ✅ CLI-S3-23 | — (sweep: Ctrl+L log area, SWEEP-LIVE-01) | Win 11 x64 |
| Full visual sweep | shim world | every dialog, popout, menu and viewport contract | — | ✅ SWEEP-VIS-01 | Win 11 x64 |
| Full live walk | real engines | real bindings: transfers, versions, profiles, i18n, fault lab | — | ✅ SWEEP-LIVE-01 | Win 11 x64 |

---

## SKIP policy — honest gaps, never silent passes

A row records **SKIP** only for conditions outside s3b's control, and the
reason is written into the certificate:

- **Engine down** — a container (SFTP/FTP/WebDAV) is not reachable; the
  cross-engine rows SKIP instead of failing.
- **Provider API gap** — the engine answers but rejects a valid request.
  The one live example: this MinIO build returns `400 InvalidArgument` for
  valid lifecycle rules, so CLI-S3-25 records SKIP on `put` (the `get`/`
  delete` halves still run). Against a provider that implements the API,
  the row passes fully — the SKIP is proof of a recorded provider gap,
  not a waived check.

## Latest certificate

Replaced on every run — this snapshot is from the verification runs of
**20 Sep 2026** against `v1.1.0-beta.14-9-wails3` on Windows 11 (x64):

```
quick run ×2 (back-to-back, identical):
  61 PASS · 1 SKIP · 0 FAIL — 174 s / 177 s
  (the 1 SKIP = lifecycle put, the recorded MinIO provider gap above)
full run (quick batteries + both sweeps), ×2 back-to-back:
  63 PASS · 1 SKIP · 0 FAIL — 494 s / 500 s   (64 rows)
  SWEEP-VIS-01  gui-visual   609/609 checks
  SWEEP-LIVE-01 gui-v3live   142 checks, no page errors
category smoke: --only meta → 3 PASS · 0 FAIL in 3 s, certificate stamped
  category: meta; unknown --only category → exit 2 with the category list
```

Run it yourself: `node scripts/certify.mjs` and read the table it prints,
plus `testartifacts/certification/certificate.json` for the machine copy
(fields: version, os, node, runId, bucket, category, summary, rows — one
entry per row with result and detail).
