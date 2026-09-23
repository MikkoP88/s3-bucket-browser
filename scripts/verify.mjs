#!/usr/bin/env node
// verify.mjs — the ACTION VERIFICATION run: every row of docs/VERIFICATION.md
// is executed here, for real, against real engines.
//
// "Verified" means: the action ran end-to-end through the shipped binary —
// CLI face and/or GUI face — against live data (MinIO S3, SFTP, FTP, WebDAV
// containers per scripts/e2e-cross.sh) with byte-level verification where
// bytes move, safety-gate probes where destruction is involved, and fault
// injection where resilience is claimed. A row that cannot run (engine
// container down, provider gap) is recorded SKIP — never silently passed.
//
// Three faces are verified:
//   CLI   — s3b-verify-cli.exe (s3b_headless build) driven as a process; exit
//           codes and output are asserted, never eyeballed.
//   GUI   — s3b-server.exe (the Wails `server` stack: real bindings, real
//           backend, real browser) driven by Playwright; results verified
//           BACK through the CLI so a GUI green means bytes on disk.
//   SWEEP — the two full harnesses (gui-visual.mjs, gui-v3live.mjs) re-run
//           as subprocesses and folded into the verification report as summary rows.
//
// Usage:  node scripts/verify.mjs [--quick] [--skip-gui] [--no-build] [--headed]
//                                         [--only <category>]
//           (or: npm run verify / verify:quick / verify:only -- <category>)
//           default        THE release gate: every row, CLI + GUI + both sweeps
//           --quick        skip the two full sweeps (CLI+GUI batteries still run)
//           --skip-gui     CLI + sweeps only (no browser battery)
//           --only <cat>   run one category only (release-focus verification):
//                            cli          all four CLI batteries
//                            s3           the MinIO/S3 CLI battery
//                            cross        cross-engine CLI battery (FTP/SFTP/WebDAV)
//                            resilience   fault-injection battery
//                            meta         binary/invocation contract battery
//                            gui          the whole GUI battery (both parts)
//                            sweeps       the two full harnesses
//           --no-build     reuse existing testartifacts exes
//           --release <tag>  release evidence: stamp the tag into the binaries
//                            exactly as the release workflow does, run the FULL
//                            matrix from a fresh build, and write the committed
//                            report docs/verification/<tag>/<os>-<arch>/
//                            (REPORT.md + verification.json) + the index there.
//                            Incompatible with --only/--quick/--no-build/--skip-gui.
//         Repeat the run to verify repeatability: results must be identical.
// Artifacts: testartifacts/verification/ (verification.json, fixtures/,
//           gui shots, server log) — wiped fresh every run.
//
// Prerequisites: MinIO on :9000 (minioadmin/minioadmin). SFTP :2222,
// FTP :2121, WebDAV :7070 (e2e/e2epass) join when their port answers.

import { spawn, execFile } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { rm, mkdir, writeFile, readFile, readdir } from 'node:fs/promises';
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const ART = path.join(ROOT, 'testartifacts', 'verification');
const FIX = path.join(ART, 'fixtures');
const CFG = path.join(ART, 'cli-config');      // S3B_CONFIG for the CLI face
const GUICFG = path.join(ART, 'gui-config');   // S3B_CONFIG for the GUI face
const SHOTS = path.join(ART, 'shots');
const CLI_EXE = path.join(ROOT, 'testartifacts', 's3b-verify-cli.exe');
const SRV_EXE = path.join(ROOT, 'testartifacts', 's3b-server.exe');

const arg = (k) => process.argv.includes(`--${k}`);
const QUICK = arg('quick');
const SKIP_GUI = arg('skip-gui');
const NO_BUILD = arg('no-build');
const HEADED = arg('headed');
// Category runs (--only): each maps to whole batteries, which are internally
// ordered so every row has the state it needs (a mid-battery slice could
// depend on rows that did not run — batteries are the safe unit).
const CATEGORIES = ['all', 'cli', 's3', 'cross', 'resilience', 'meta', 'gui', 'sweeps'];
const ONLY = (() => {
  const i = process.argv.indexOf('--only');
  const v = i >= 0 ? process.argv[i + 1] : 'all';
  if (!CATEGORIES.includes(v)) {
    console.error(`unknown --only "${v}" — categories: ${CATEGORIES.join(' | ')}`);
    process.exit(2);
  }
  return v;
})();
const want = (c) => ONLY === 'all' || ONLY === c || (ONLY === 'cli' && ['s3', 'cross', 'resilience', 'meta'].includes(c));

// Release-evidence mode: `--release v1.2.3` stamps the tag into the binaries
// exactly as the release workflow does (main.version=<tag without v> — see
// .github/workflows/release.yml), refuses anything but the full matrix from a
// fresh build, and writes the committed report under docs/verification/.
// CLI-M-01/GUI-01 then assert the binary prints the stamp, so the report's
// version is binary-verified, not declared.
const RELEASE = (() => {
  const i = process.argv.indexOf('--release');
  if (i < 0) return null;
  const tag = process.argv[i + 1];
  if (!tag || !/^v\d+\.\d+\.\d+(-[\w.]+)?$/.test(tag)) {
    console.error(`--release expects a release tag like v1.2.3 or v1.1.0-beta.15 (got "${tag ?? ''}")`);
    process.exit(2);
  }
  const clash = [QUICK && '--quick', SKIP_GUI && '--skip-gui', NO_BUILD && '--no-build', ONLY !== 'all' && '--only'].filter(Boolean);
  if (clash.length) {
    console.error(`--release is release evidence: the full matrix from a fresh build — incompatible with ${clash.join(', ')}`);
    process.exit(2);
  }
  return tag;
})();
const VERSION = RELEASE ? RELEASE.replace(/^v/, '') : 'v1.1.0-beta.14-9-wails3';
// Stable directory id for the report, e.g. windows-x64 / macos-arm64.
const OSID = `${({ win32: 'windows', darwin: 'macos' })[os.platform()] || os.platform()}-${os.arch()}`;

// ---------- live engines ----------
const ENDPOINT = 'http://localhost:9000';
const KEY = 'minioadmin';
const SECRET = 'minioadmin';
const REGION = 'us-east-1';
const E2E_USER = process.env.S3B_E2E_USER || 'e2e';
const E2E_PASS = process.env.S3B_E2E_PASS || 'e2epass';
const SFTP_PORT = +(process.env.S3B_SFTP_PORT || 2222);
const FTP_PORT = +(process.env.S3B_FTP_PORT || 2121);
const WEBDAV_PORT = +(process.env.S3B_WEBDAV_PORT || 7070);

const RUNID = `ver${Date.now().toString(36)}`;
const BUCKET = `verify-${Date.now().toString(36)}`;      // lowercase — S3-safe
const SRCNAME = 'verify-minio';                           // GUI S3 source (bucket-scoped)
const FTPNAME = 'verify-ftp';                             // GUI FTP source
const GUI_PORT = 39874;                                 // v3live uses 39872; keep distinct

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// gitOut runs a git command in the repo; '' when git is absent or fails —
// the commit fields are evidence, not load-bearing for the gate.
const gitOut = (args) => new Promise((resolve) => {
  execFile('git', args, { cwd: ROOT, windowsHide: true, timeout: 10000 }, (err, stdout) => resolve(err ? '' : String(stdout).trim()));
});

// ---------- verification rows ----------
const rows = [];
let cur = null;
function verify(meta, fn) {
  cur = { result: 'PASS', detail: '', ...meta };
  rows.push(cur);
  process.stdout.write(`[${cur.face}] ${cur.id} ${cur.action} (${cur.ds}) … `);
  return Promise.resolve()
    .then(fn)
    .then((detail) => { cur.detail = detail || ''; finish(); })
    .catch((err) => { cur.result = 'FAIL'; cur.detail = String(err?.message || err).split('\n')[0].slice(0, 200); finish(); });
}
function finish() {
  process.stdout.write(`${cur.result}${cur.detail ? ` — ${cur.detail}` : ''}\n`);
  if (cur.result === 'FAIL') failures.push(`${cur.id} ${cur.action}: ${cur.detail}`);
}
const failures = [];
function skip(detail) { cur.result = 'SKIP'; cur.detail = detail; return detail; }
function need(cond, what) { if (!cond) throw new Error(what); }

// ---------- CLI process driver ----------
// nokeyring: run with S3B_NO_KEYRING=1 so secrets stay inside the config
// file. REQUIRED for isolated-config rows that add/remove/import sources
// sharing a name with the shared faces — profile keyring slots are keyed
// by NAME ONLY (pkg/core/profile/keyring.go), so a plain add in a scratch
// config would silently overwrite/delete the live face's global slot.
function cli(args, { timeout = 120000, cfg = CFG, nokeyring = false } = {}) {
  return new Promise((resolve) => {
    execFile(CLI_EXE, args, {
      cwd: ROOT,
      windowsHide: true,
      timeout,
      maxBuffer: 64 * 1024 * 1024,
      env: { ...process.env, S3B_CONFIG: cfg, NO_COLOR: '1', TERM: 'dumb', ...(nokeyring ? { S3B_NO_KEYRING: '1' } : {}) },
    }, (err, stdout, stderr) => {
      resolve({ code: err ? (err.code ?? 1) : 0, out: String(stdout || ''), err: String(stderr || '') });
    });
  });
}
const countLines = (out, needle) => out.split('\n').filter((l) => l.includes(needle)).length;

// ---------- byte-level verification ----------
async function treeOf(dir, base = dir) {
  const out = [];
  for (const e of await readdir(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) out.push(...await treeOf(p, base));
    else {
      const b = await readFile(p);
      out.push([path.relative(base, p).split(path.sep).join('/'), b.length, b.toString('utf8')]);
    }
  }
  return out.sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0));
}
const sameTree = (a, b) => JSON.stringify(a) === JSON.stringify(b);

// ---------- binary verification ----------
// sha256file is the integrity oracle for the extreme-level rows: transfers
// are only "correct" when the sha of what came back equals the sha of what
// went in — byte-level, not size-level.
const sha256file = async (p) => {
  const h = createHash('sha256');
  h.update(await readFile(p));
  return h.digest('hex');
};

// ---------- port probe ----------
const portOpen = (port) => new Promise((res) => {
  const s = net.connect({ host: '127.0.0.1', port, timeout: 1500 });
  s.once('connect', () => { s.destroy(); res(true); });
  s.once('error', () => res(false));
  s.once('timeout', () => { s.destroy(); res(false); });
});

// ---------- fixtures ----------
const FIXTREE = {
  'readme.md': 'verification readme\n',
  'root-1.txt': 'root file one\n',
  'root-2.txt': 'root file two\n',
  'docs/doc-01.txt': 'document one\n',
  'docs/doc-02.txt': 'document two\n',
  'docs/doc-03.txt': 'document three\n',
  'logs/log-01.log': 'log line one\n',
  'logs/log-02.log': 'log line two\n',
  'empty.txt': '',
  'uni-åäö.txt': 'unicode åäö content\n',
};
const NFILES = Object.keys(FIXTREE).length; // 10
async function writeFixtures() {
  await rm(FIX, { recursive: true, force: true });
  for (const [rel, content] of Object.entries(FIXTREE)) {
    const p = path.join(FIX, 'data', rel);
    await mkdir(path.dirname(p), { recursive: true });
    await writeFile(p, content);
  }
}

// ============================================================
// CLI battery — S3 (MinIO)
// ============================================================
async function cliS3() {
  const B = `s3://${BUCKET}`;
  await verify({ id: 'CLI-S3-01', area: 'sources', action: 'Add + test S3 source', ds: 'S3 (MinIO)', scenario: 'source add with endpoint/keys; source test dials; list shows it; mirrors as profile', face: 'CLI' }, async () => {
    let r = await cli(['source', 'add', 'verifys3', '--type', 's3', '--endpoint', ENDPOINT, '--access-key', KEY, '--secret-key', SECRET]);
    need(r.code === 0, `source add: ${r.err}`);
    r = await cli(['source', 'test', 'verifys3']);
    need(r.code === 0 && /✅|OK/.test(r.out + r.err), `source test: ${r.out}${r.err}`);
    r = await cli(['source', 'list']);
    need(r.out.includes('verifys3'), 'source list missing verifys3');
    r = await cli(['profile', 'list']);
    need(r.out.includes('verifys3'), 'profile mirror missing');
    return 'added, tested, listed, mirrored';
  });

  await verify({ id: 'CLI-S3-02', area: 'buckets', action: 'Create versioned bucket', ds: 'S3 (MinIO)', scenario: 'mb + bucket versioning on (the delete-marker scenarios need it)', face: 'CLI' }, async () => {
    let r = await cli(['mb', B]);
    need(r.code === 0 && /created bucket/.test(r.out), `mb: ${r.out}${r.err}`);
    r = await cli(['bucket', 'versioning', B, 'on']);
    need(r.code === 0 && /versioning enabled/.test(r.out), `versioning: ${r.out}${r.err}`);
    return `${BUCKET} created, versioning on`;
  });

  await verify({ id: 'CLI-S3-03', area: 'objects', action: 'Create folder marker', ds: 'S3 (MinIO)', scenario: 'mkdir docs/ → zero-byte marker lists as a folder', face: 'CLI' }, async () => {
    const r = await cli(['mkdir', `${B}/docs/`]);
    need(r.code === 0 && /created folder/.test(r.out), `mkdir: ${r.out}${r.err}`);
    const l = await cli(['ls', B]);
    need(/docs/.test(l.out), 'folder not listed');
    return 'docs/ marker created and listed';
  });

  await verify({ id: 'CLI-S3-04', area: 'transfers', action: 'Single upload', ds: 'S3 (MinIO)', scenario: 'cp one file → stat reports size', face: 'CLI' }, async () => {
    let r = await cli(['cp', path.join(FIX, 'data', 'readme.md'), `${B}/readme.md`]);
    need(r.code === 0 && /copied 1 item/.test(r.out), `cp: ${r.out}${r.err}`);
    r = await cli(['stat', `${B}/readme.md`]);
    need(/size:/.test(r.out), `stat: ${r.out}`);
    return 'uploaded + stat ok';
  });

  await verify({ id: 'CLI-S3-05', area: 'transfers', action: 'Multi upload (recursive)', ds: 'S3 (MinIO)', scenario: `cp -r fixture tree (${NFILES} files incl. unicode + empty) → recursive ls count matches`, face: 'CLI' }, async () => {
    let r = await cli(['cp', '-r', path.join(FIX, 'data'), `${B}/data/`, '--json']);
    need(r.code === 0 && r.out.includes(`"items": ${NFILES}`), `cp -r: ${r.out}${r.err}`);
    r = await cli(['ls', `${B}/`, '--recursive', '--json']);
    const n = countLines(r.out, '"key"');
    need(n === NFILES + 1, `recursive ls count ${n}, want ${NFILES + 1} (incl. readme)`);
    return `${NFILES} files uploaded, ${n} listed`;
  });

  await verify({ id: 'CLI-S3-06', area: 'objects', action: 'List / tree / du / stat', ds: 'S3 (MinIO)', scenario: 'dir-view ls, tree shows folders, du counts objects+bytes, stat bucket shows region', face: 'CLI' }, async () => {
    let r = await cli(['ls', `${B}/data/docs/`]);
    need(/doc-01/.test(r.out), `ls docs: ${r.out}`);
    r = await cli(['tree', B]);
    need(/docs\//.test(r.out) && /logs\//.test(r.out), `tree: ${r.out}`);
    r = await cli(['du', B]);
    // 11 files + the docs/ marker: du counts raw objects, recursive ls hides markers
    need(r.out.includes(`${NFILES + 2} object`), `du: ${r.out}`);
    r = await cli(['stat', B, '--json']);
    need(r.out.includes('"region"'), `stat bucket: ${r.out}`);
    return 'ls/tree/du/stat consistent';
  });

  await verify({ id: 'CLI-S3-07', area: 'transfers', action: 'Single download', ds: 'S3 (MinIO)', scenario: 'cp object → local; bytes identical', face: 'CLI' }, async () => {
    const out = path.join(ART, 'down', 'readme.md');
    await mkdir(path.dirname(out), { recursive: true });
    const r = await cli(['cp', `${B}/readme.md`, out]);
    need(r.code === 0, `cp down: ${r.err}`);
    const a = await readFile(path.join(FIX, 'data', 'readme.md'));
    const b = await readFile(out);
    need(a.equals(b), 'downloaded bytes differ');
    return 'byte-identical';
  });

  await verify({ id: 'CLI-S3-08', area: 'transfers', action: 'Multi download (recursive)', ds: 'S3 (MinIO)', scenario: 'cp -r prefix → local dir; full tree diff byte-identical', face: 'CLI' }, async () => {
    const out = path.join(ART, 'down-tree');
    await rm(out, { recursive: true, force: true });
    const r = await cli(['cp', '-r', `${B}/data/`, out]);
    need(r.code === 0, `cp -r down: ${r.err}`);
    const want = await treeOf(path.join(FIX, 'data'));
    const got = await treeOf(out);
    need(sameTree(want, got), `tree diff:\n${JSON.stringify(got).slice(0, 300)}`);
    return `${got.length} files byte-identical`;
  });

  await verify({ id: 'CLI-S3-09', area: 'transfers', action: 'Server-side copy S3→S3', ds: 'S3 (MinIO)', scenario: 'cp s3://→s3:// lands a copyable object', face: 'CLI' }, async () => {
    let r = await cli(['cp', `${B}/readme.md`, `${B}/copy/readme-v2.md`]);
    need(r.code === 0, `s3s3: ${r.err}`);
    r = await cli(['ls', `${B}/copy/`, '--json']);
    need(r.out.includes('readme-v2.md'), 'copy not listed');
    return 'server-side copy listed';
  });

  await verify({ id: 'CLI-S3-10', area: 'objects', action: 'Single object deletion', ds: 'S3 (MinIO)', scenario: 'rm one object → gone from ls; versioned → delete marker in timeline', face: 'CLI' }, async () => {
    let r = await cli(['rm', `${B}/data/logs/log-01.log`]);
    need(r.code === 0 && /deleted/.test(r.out), `rm: ${r.out}${r.err}`);
    r = await cli(['ls', `${B}/data/logs/`, '--json']);
    need(!r.out.includes('log-01'), 'object still listed');
    r = await cli(['versions', 'ls', `${B}/data/logs/log-01.log`, '--json']);
    need(r.out.includes('"isDeleteMarker": true'), 'no delete marker in timeline');
    return 'removed + marker recorded';
  });

  await verify({ id: 'CLI-S3-11', area: 'objects', action: 'Recursive deletion + safety gates', ds: 'S3 (MinIO)', scenario: '55-object prefix: rm -r without --force rejected (>50 gate); --dry-run counts (marker included); --force deletes them all', face: 'CLI' }, async () => {
    // seed a 55-object prefix — the L1 gate demands --force above 50
    const gate = path.join(ART, 'gate');
    await rm(gate, { recursive: true, force: true });
    await mkdir(gate, { recursive: true });
    for (let i = 0; i < 55; i++) await writeFile(path.join(gate, `g-${String(i).padStart(2, '0')}.txt`), `gate ${i}\n`);
    let r = await cli(['cp', '-r', gate, `${B}/gate/`, '--json']);
    need(r.code === 0 && r.out.includes('"items": 55'), `gate seed: ${r.out}${r.err}`);
    r = await cli(['rm', `${B}/gate/`, '-r']);
    need(r.code !== 0, 'rm -r over the 50-object gate must fail without --force');
    r = await cli(['rm', `${B}/gate/`, '-r', '--dry-run']);
    const m = /total: (\d+)/.exec(r.out);
    need(m, `dry-run missing total: ${r.out}`);
    const total = +m[1];
    need(total === 55 || total === 56, `dry-run total ${total}, want 55 files (+ marker)`);
    r = await cli(['rm', `${B}/gate/`, '-r', '--force']);
    const del = /deleted (\d+) object/.exec(r.out);
    need(r.code === 0 && del && +del[1] === total, `rm -r: ${r.out}${r.err}`);
    r = await cli(['ls', `${B}/`, '--recursive', '--json']);
    need(!r.out.includes('gate/'), 'gate/ still listed');
    return `gate held; dry-run ${total}, force-deleted 55`;
  });

  await verify({ id: 'CLI-S3-12', area: 'buckets', action: 'rb safety gate', ds: 'S3 (MinIO)', scenario: 'rb on a non-empty bucket rejected without --force', face: 'CLI' }, async () => {
    const r = await cli(['rb', B]);
    need(r.code !== 0, 'rb non-empty must fail');
    return 'rejected as designed';
  });

  await verify({ id: 'CLI-S3-13', area: 'objects', action: 'Rename (mv)', ds: 'S3 (MinIO)', scenario: 'mv object → new key; old gone, new stats', face: 'CLI' }, async () => {
    let r = await cli(['mv', `${B}/copy/readme-v2.md`, `${B}/copy/readme-v3.md`]);
    need(r.code === 0 && /moved 1 item/.test(r.out), `mv: ${r.out}${r.err}`);
    r = await cli(['stat', `${B}/copy/readme-v3.md`]);
    need(/size:/.test(r.out), 'renamed key missing');
    return 'moved + stat ok';
  });

  await verify({ id: 'CLI-S3-14', area: 'transfers', action: 'sync (repair / no-op / new / --delete)', ds: 'S3 (MinIO)', scenario: 'sync repairs the CLI-S3-10 deletion, reports 0 when in sync, 1 after a local add, deletes the extra remote with --delete', face: 'CLI' }, async () => {
    const data = path.join(FIX, 'data');
    let r = await cli(['sync', data, `${B}/data/`, '--json']);
    need(r.out.includes('"transferred": 1'), `sync repair (log-01): ${r.out}`);
    r = await cli(['sync', data, `${B}/data/`, '--json']);
    need(r.out.includes('"transferred": 0'), `sync no-op: ${r.out}`);
    const p = path.join(data, 'new.txt');
    await writeFile(p, 'new file\n');
    r = await cli(['sync', data, `${B}/data/`, '--json']);
    need(r.out.includes('"transferred": 1'), `sync add: ${r.out}`);
    await rm(p, { force: true });
    r = await cli(['sync', data, `${B}/data/`, '--delete', '--json']);
    need(r.out.includes('"deleted": 1'), `sync delete: ${r.out}`);
    return 'repair/no-op/add/delete all correct';
  });

  await verify({ id: 'CLI-S3-15', area: 'objects', action: 'Presign + fetch', ds: 'S3 (MinIO)', scenario: 'presign → plain HTTP GET returns identical bytes', face: 'CLI' }, async () => {
    const r = await cli(['presign', `${B}/readme.md`, '--expires', '5m']);
    need(r.code === 0 && r.out.startsWith('http'), `presign: ${r.out}${r.err}`);
    const res = await fetch(r.out.trim());
    need(res.ok, `presigned GET ${res.status}`);
    const body = Buffer.from(await res.arrayBuffer());
    const want = await readFile(path.join(FIX, 'data', 'readme.md'));
    need(body.equals(want), 'presigned bytes differ');
    return 'URL fetched, bytes identical';
  });

  await verify({ id: 'CLI-S3-16', area: 'versions', action: 'Version timeline: restore + undo', ds: 'S3 (MinIO)', scenario: 'overwrite → 2 versions; restore v1 as latest; rm → marker; undo revives v1', face: 'CLI' }, async () => {
    const K = `${B}/ver.txt`;
    await cli(['cp', path.join(FIX, 'data', 'root-1.txt'), K]);
    await cli(['cp', path.join(FIX, 'data', 'root-2.txt'), K]);
    let r = await cli(['versions', 'ls', K, '--json']);
    const vers = JSON.parse(r.out);
    need(vers.length === 2, `want 2 versions, got ${vers.length}`);
    const vid = vers[vers.length - 1].versionId; // newest-first → last is v1
    r = await cli(['versions', 'restore', K, '--version-id', vid]);
    need(/restored/.test(r.out), `restore: ${r.out}`);
    await cli(['rm', K]);
    r = await cli(['versions', 'ls', K, '--json']);
    const mid = JSON.parse(r.out).find((v) => v.isDeleteMarker)?.versionId;
    need(mid, 'no delete marker in timeline');
    r = await cli(['versions', 'undo', K, '--version-id', mid]);
    need(/is back/.test(r.out), `undo: ${r.out}`);
    const out = path.join(ART, 'back.txt');
    await cli(['cp', K, out]);
    const a = await readFile(path.join(FIX, 'data', 'root-1.txt'));
    need((await readFile(out)).equals(a), 'undo served wrong content');
    return 'restore/undo byte-correct';
  });

  await verify({ id: 'CLI-S3-17', area: 'versions', action: 'Purge + permanent destroy', ds: 'S3 (MinIO)', scenario: 'versions stat; purge noncurrent (>50 gate demands --force, then --force purges); versions rm --all empties the timeline (L3)', face: 'CLI' }, async () => {
    let r = await cli(['versions', 'stat', B]);
    need(/total versions:/.test(r.out), `stat: ${r.out}`);
    r = await cli(['versions', 'purge', B, '--mode', 'noncurrent', '--dry-run']);
    need(/would purge/.test(r.out), `purge dry: ${r.out}`);
    // 55+ noncurrent versions survive from CLI-S3-11 → the L1 gate must demand --force
    r = await cli(['versions', 'purge', B, '--mode', 'noncurrent']);
    need(r.code !== 0 && /--force/.test(r.out + r.err), `purge gate: ${JSON.stringify(r.out)} ${JSON.stringify(r.err)}`);
    r = await cli(['versions', 'purge', B, '--mode', 'noncurrent', '--force']);
    need(r.code === 0 && /deleted/.test(r.out), `purge --force: ${r.out}${r.err}`);
    r = await cli(['versions', 'rm', `${B}/ver.txt`, '--all']);
    need(/deleted/.test(r.out), `versions rm: ${r.out}`);
    r = await cli(['versions', 'ls', `${B}/ver.txt`, '--json']);
    need(countLines(r.out, '"versionId"') === 0, 'timeline not destroyed');
    return 'purged and destroyed';
  });

  await verify({ id: 'CLI-S3-18', area: 'objects', action: 'Storage-class conversion', ds: 'S3 (MinIO)', scenario: 'sc single → REDUCED_REDUNDANCY visible + find --class; recursive dry-run gate', face: 'CLI' }, async () => {
    let r = await cli(['sc', `${B}/copy/readme-v3.md`, 'REDUCED_REDUNDANCY']);
    if (/not supported|Invalid storage class/.test(r.out + r.err)) return skip('not supported by this MinIO');
    need(/converted/.test(r.out), `sc: ${r.out}${r.err}`);
    r = await cli(['find', B, '--class', 'REDUCED_REDUNDANCY', '--json']);
    need(countLines(r.out, '"key"') === 1, 'find --class count');
    await cli(['cp', path.join(FIX, 'data', 'root-1.txt'), `${B}/sczone/a.txt`]);
    await cli(['cp', path.join(FIX, 'data', 'root-2.txt'), `${B}/sczone/b.txt`]);
    r = await cli(['sc', `${B}/sczone/`, 'STANDARD']);
    need(r.code !== 0, 'prefix sc without -r must fail');
    r = await cli(['sc', `${B}/sczone/`, 'STANDARD', '-r']);
    need(/converted 2 object/.test(r.out), `sc -r: ${r.out}`);
    return 'single + recursive conversion verified';
  });

  await verify({ id: 'CLI-S3-19', area: 'search', action: 'Deep find', ds: 'S3 (MinIO)', scenario: '--name glob/substring, --smaller, --limit, summary line', face: 'CLI' }, async () => {
    let r = await cli(['find', B, '--name', 'readme*', '--json']);
    need(countLines(r.out, '"key"') === 3, `glob: ${r.out}`); // readme.md + copy/readme-v3.md + data/readme.md
    r = await cli(['find', B, '--name', 'doc-02', '--json']);
    need(countLines(r.out, '"key"') === 1, 'substring');
    r = await cli(['find', B, '--smaller', '1B', '--json']);
    need(countLines(r.out, '"key"') >= 1, 'smaller finds markers');
    r = await cli(['find', B, '--limit', '1', '--json']);
    need(countLines(r.out, '"key"') === 1, 'limit');
    return 'filters + limits correct';
  });

  await verify({ id: 'CLI-S3-20', area: 'admin', action: 'Doctor diagnosis', ds: 'S3 (MinIO)', scenario: 'doctor s3://bucket runs the check ladder', face: 'CLI' }, async () => {
    const r = await cli(['doctor', B]);
    need(r.code === 0 && /DNS Resolution Check/.test(r.out), `doctor: ${r.out}${r.err}`);
    return 'check ladder ran';
  });

  await verify({ id: 'CLI-S3-21', area: 'admin', action: 'Bucket admin: info / tags / policy', ds: 'S3 (MinIO)', scenario: 'info shows versioning; tags put/get; policy put/get round-trip (cors/encryption tolerate provider gaps)', face: 'CLI' }, async () => {
    let r = await cli(['bucket', 'info', B]);
    need(/versioning:/.test(r.out), `info: ${r.out}`);
    r = await cli(['bucket', 'tags', 'put', B, 'team=verify', 'env=ci']);
    need(/tag\(s\) saved/.test(r.out), `tags put: ${r.out}`);
    r = await cli(['bucket', 'tags', 'get', B]);
    need(r.out.includes('team=verify'), 'tags get');
    const pol = path.join(ART, 'policy.json');
    await mkdir(ART, { recursive: true });
    await writeFile(pol, JSON.stringify({ Version: '2012-10-17', Statement: [{ Effect: 'Allow', Principal: { AWS: ['*'] }, Action: 's3:GetObject', Resource: `arn:aws:s3:::${BUCKET}/*` }] }));
    r = await cli(['bucket', 'policy', 'put', B, pol]);
    if (!/not supported/.test(r.out + r.err)) {
      need(/policy saved/.test(r.out), `policy put: ${r.out}${r.err}`);
      r = await cli(['bucket', 'policy', 'get', B]);
      need(r.out.includes(BUCKET), 'policy get');
      // restore the private default: this bucket is shared with the GUI
      // battery, whose Copy-URL row proves the anonymous 403 of a real,
      // ungranted address — a leftover public-read policy would serve
      // bytes to that probe (the full-run GUI-49 flake of 23 Sep 2026)
      r = await cli(['bucket', 'policy', 'delete', B]);
      need(/policy removed/.test(r.out), `policy delete: ${r.out}${r.err}`);
      r = await cli(['bucket', 'policy', 'get', B]);
      need(/no policy set/.test(r.out), `policy survived the delete: ${r.out.slice(0, 120)}`);
    }
    return 'info/tags/policy verified (private default restored)';
  });

  await verify({ id: 'CLI-S3-22', area: 'admin', action: 'Object lock: retention + legal hold', ds: 'S3 (MinIO)', scenario: 'mb --object-lock; retention set/show/clear; legalhold on/off (GOVERNANCE only — cleanup stays possible)', face: 'CLI' }, async () => {
    const LB = `s3://${BUCKET}-lock`;
    let r = await cli(['mb', LB, '--object-lock']);
    need(r.code === 0, `mb lock: ${r.err}`);
    r = await cli(['bucket', 'lock', LB, '--enable']);
    if (/not supported/.test(r.out + r.err)) { await cli(['rb', LB, '--force']); return skip('not supported by this MinIO'); }
    need(/object lock enabled/.test(r.out), `lock: ${r.out}${r.err}`);
    await cli(['cp', path.join(FIX, 'data', 'readme.md'), `${LB}/important.txt`]);
    r = await cli(['lock', 'retention', `${LB}/important.txt`, '--mode', 'GOVERNANCE', '--until', '+1h']);
    need(/retention GOVERNANCE/.test(r.out), `retention: ${r.out}${r.err}`);
    r = await cli(['lock', 'retention', `${LB}/important.txt`]);
    need(/GOVERNANCE/.test(r.out), 'retention show');
    r = await cli(['lock', 'retention', `${LB}/important.txt`, '--clear', '--bypass-governance']);
    need(/retention cleared/.test(r.out), 'retention clear');
    r = await cli(['lock', 'legalhold', `${LB}/important.txt`, '--on']);
    need(/legal hold ON/.test(r.out), 'legalhold on');
    r = await cli(['lock', 'legalhold', `${LB}/important.txt`, '--off']);
    need(/legal hold OFF/.test(r.out), 'legalhold off');
    await cli(['versions', 'rm', `${LB}/important.txt`, '--all']);
    await cli(['rb', LB, '--force']);
    return 'retention/hold round-tripped, cleaned up';
  });

  await verify({ id: 'CLI-S3-23', area: 'admin', action: 'Activity log', ds: 'S3 (MinIO)', scenario: 'log shows the operations this run performed', face: 'CLI' }, async () => {
    const r = await cli(['log']);
    need(r.code === 0 && r.out.trim().length > 0, `log: ${r.out}${r.err}`);
    return `${r.out.split('\n').length} lines recorded`;
  });

  // Found by this suite: MinIO RELEASE.2025-09-07 deletes the WHOLE BUCKET
  // when it receives DeletePublicAccessBlock (s3b sends the documented
  // DELETE /bucket?publicAccessBlock). DeletePAB now refuses on providers
  // that cannot serve GetPublicAccessBlock — and this row is the permanent
  // tripwire: NO bucket-config delete may ever destroy the bucket itself.
  await verify({ id: 'CLI-S3-24', area: 'admin', action: 'Bucket config deletes never destroy the bucket', ds: 'S3 (MinIO)', scenario: 'website/encryption/lifecycle/cors/pab delete on a disposable bucket — after EACH op the bucket must still stat; pab delete must refuse cleanly on providers without PAB support', face: 'CLI' }, async () => {
    const TB = `s3://${BUCKET}-cfg`;
    let r = await cli(['mb', TB]);
    need(r.code === 0, `mb cfg: ${r.err}`);
    const alive = async (after) => {
      const s = await cli(['stat', TB]);
      need(s.code === 0, `BUCKET DESTROYED by "${after}" — stat: ${s.out.split('\n')[0]}`);
    };
    const attempt = async (label, args) => {
      r = await cli(args);
      // A provider gap (not supported / MalformedXML / NotImplemented / 400
      // InvalidArgument) is a legitimate clean refusal; anything else that
      // exits 0 must leave the bucket alive (checked right after).
      need(r.code === 0 || /not supported|not implemented|malformed|invalid/i.test(r.out + r.err),
        `${label}: unexpected failure ${r.out.split('\n')[0]}${r.err.split('\n')[0]}`);
      await alive(label);
    };
    await attempt('website delete', ['bucket', 'website', 'delete', TB]);
    await attempt('encryption delete', ['bucket', 'encryption', 'delete', TB]);
    await attempt('lifecycle delete', ['bucket', 'lifecycle', 'delete', TB]);
    await attempt('cors delete', ['bucket', 'cors', 'delete', TB]);
    await attempt('pab delete', ['bucket', 'pab', 'delete', TB]);
    await cli(['rb', TB, '--force']);
    return 'bucket survived all five config deletes';
  });

  await verify({ id: 'CLI-S3-25', area: 'admin', action: 'Lifecycle rules round-trip', ds: 'S3 (MinIO)', scenario: 'put flat-schema rules (expiration + transition); get echoes them; delete clears (put tolerates provider gaps as SKIP)', face: 'CLI' }, async () => {
    const TB = `s3://${BUCKET}-lc`;
    const f = path.join(ART, 'lifecycle.json');
    await writeFile(f, JSON.stringify([
      { id: 'purge-tmp', enabled: true, prefix: 'tmp/', expirationDays: 1 },
      { id: 'to-glacier', enabled: false, prefix: 'cold/', transitionDays: 30, transitionClass: 'GLACIER' },
    ]));
    let r = await cli(['mb', TB]);
    need(r.code === 0, `mb lc: ${r.err}`);
    r = await cli(['bucket', 'lifecycle', 'put', TB, f]);
    if (r.code !== 0 && /not supported|not implemented|invalid|malformed|StatusCode: 400/i.test(r.out + r.err)) {
      await cli(['rb', TB, '--force']);
      return skip('lifecycle put rejected by this provider (recorded gap)');
    }
    need(r.code === 0, `lifecycle put: ${r.out}${r.err}`);
    r = await cli(['bucket', 'lifecycle', 'get', TB, '--json']);
    const rules = JSON.parse(r.out);
    need(Array.isArray(rules) && rules.some((x) => x.id === 'purge-tmp' || x.ID === 'purge-tmp'), `get echo: ${r.out}`);
    r = await cli(['bucket', 'lifecycle', 'delete', TB]);
    need(r.code === 0, `lifecycle delete: ${r.out}${r.err}`);
    r = await cli(['bucket', 'lifecycle', 'get', TB]);
    need(/no lifecycle rules/i.test(r.out), `get after delete: ${r.out}`);
    await cli(['rb', TB, '--force']);
    return 'put/get/delete round-trip';
  });

  await verify({ id: 'CLI-S3-26', area: 'versions', action: 'Versioned migration (cp/mv --versions)', ds: 'S3 (MinIO)', scenario: '2 versions at source; cp --versions s3→s3 copies the full timeline; mv --versions moves it; unversioned destination refuses (gate)', face: 'CLI' }, async () => {
    const SRC = `${B}/vmig/a.txt`, DST = `${B}/vmig2/a.txt`, MOVED = `${B}/vmig3/a.txt`;
    await cli(['cp', path.join(FIX, 'data', 'root-1.txt'), SRC]);
    await cli(['cp', path.join(FIX, 'data', 'root-2.txt'), SRC, '--force']);
    let r = await cli(['versions', 'ls', SRC, '--json']);
    need(countLines(r.out, '"versionId"') === 2, `seed versions: ${r.out}`);
    r = await cli(['cp', SRC, DST, '--versions']);
    need(r.code === 0, `cp --versions: ${r.out}${r.err}`);
    r = await cli(['versions', 'ls', DST, '--json']);
    need(countLines(r.out, '"versionId"') === 2, `dest timeline: ${r.out}`);
    const out = path.join(ART, 'vmig-latest.txt');
    await cli(['cp', DST, out]);
    need((await readFile(out)).equals(await readFile(path.join(FIX, 'data', 'root-2.txt'))), 'latest bytes wrong after migration');
    r = await cli(['mv', DST, MOVED, '--versions']);
    need(r.code === 0, `mv --versions: ${r.out}${r.err}`);
    r = await cli(['versions', 'ls', MOVED, '--json']);
    need(countLines(r.out, '"versionId"') === 2, `moved timeline: ${r.out}`);
    // the gate: an unversioned destination silently collapses the timeline —
    // the binary must refuse instead
    const NB = `s3://${BUCKET}-plain`;
    await cli(['mb', NB]);
    r = await cli(['cp', MOVED, `${NB}/a.txt`, '--versions']);
    need(r.code !== 0 && /versioning/i.test(r.out + r.err), `unversioned-dest gate: exit ${r.code} ${r.out}${r.err}`);
    await cli(['versions', 'rm', MOVED, '--all']);
    await cli(['rb', NB, '--force']);
    return 'timeline copied + moved intact; unversioned dest refused';
  });

  await verify({ id: 'CLI-S3-27', area: 'transfers', action: 'cp flag contracts: --dry-run / --no-clobber', ds: 'S3 (MinIO)', scenario: '--dry-run prints the plan but lands nothing (stat 404); --no-clobber skips an overwrite (documented skip semantics: exit 0, object bytes untouched); --force overwrites', face: 'CLI' }, async () => {
    const K = `${B}/flags/x.txt`;
    let r = await cli(['cp', path.join(FIX, 'data', 'root-1.txt'), K, '--dry-run']);
    need(r.code === 0 && r.out.includes('x.txt'), `dry-run plan: ${r.out}${r.err}`);
    r = await cli(['stat', K]);
    need(r.code !== 0, 'dry-run must not upload');
    r = await cli(['cp', path.join(FIX, 'data', 'root-1.txt'), K]);
    need(r.code === 0, `first cp: ${r.err}`);
    r = await cli(['cp', path.join(FIX, 'data', 'root-2.txt'), K, '--no-clobber']);
    need(r.code === 0, `no-clobber must skip cleanly: exit ${r.code} ${r.out}${r.err}`);
    const out = path.join(ART, 'nc.txt');
    await cli(['cp', K, out]);
    need((await readFile(out)).equals(await readFile(path.join(FIX, 'data', 'root-1.txt'))), 'no-clobber leaked a write');
    r = await cli(['cp', path.join(FIX, 'data', 'root-2.txt'), K, '--force']);
    need(r.code === 0, `explicit overwrite: ${r.err}`);
    return 'dry-run inert; no-clobber skipped (bytes untouched); --force overwrites';
  });

  await verify({ id: 'CLI-S3-28', area: 'search', action: 'find size + time filters', ds: 'S3 (MinIO)', scenario: 'controlled prefix (1 big + 1 small): --larger/--smaller counts; --newer 1h finds both; --older 1h finds none', face: 'CLI' }, async () => {
    const big = path.join(ART, 'big.txt');
    await writeFile(big, 'x'.repeat(3000));
    await cli(['cp', big, `${B}/find/big.txt`]);
    await cli(['cp', path.join(FIX, 'data', 'root-1.txt'), `${B}/find/small.txt`]);
    let r = await cli(['find', `${B}/find/`, '--larger', '2000', '--json']);
    need(countLines(r.out, '"key"') === 1, `larger: ${r.out}`);
    r = await cli(['find', `${B}/find/`, '--smaller', '100', '--json']);
    need(countLines(r.out, '"key"') === 1, `smaller: ${r.out}`);
    r = await cli(['find', `${B}/find/`, '--newer', '1h', '--json']);
    need(countLines(r.out, '"key"') === 2, `newer: ${r.out}`);
    r = await cli(['find', `${B}/find/`, '--older', '1h', '--json']);
    need(countLines(r.out, '"key"') === 0, `older: ${r.out}`);
    return 'size/time filters correct';
  });

  // ---- extreme-level rows: the critical-data-source contracts ----

  await verify({ id: 'CLI-S3-29', area: 'transfers', action: 'Multipart large-object round-trip', ds: 'S3 (MinIO)', scenario: '32 MiB random object (7+ multipart parts at the 5 MiB part size) uploads and downloads back sha256-identical — integrity is byte-level, not size-level', face: 'CLI' }, async () => {
    const BYTES = 32 * 1024 * 1024;
    const src = path.join(ART, 'mp-32m.bin');
    await writeFile(src, randomBytes(BYTES));
    const sha = await sha256file(src);
    const t0 = Date.now();
    let r = await cli(['cp', src, `${B}/verify-big/mp-32m.bin`], { timeout: 240000 });
    need(r.code === 0, `multipart cp: ${r.out}${r.err}`);
    r = await cli(['stat', `${B}/verify-big/mp-32m.bin`]);
    need(r.out.includes(String(BYTES)), `stat size after multipart: ${r.out}${r.err}`);
    const dl = path.join(ART, 'mp-32m.dl.bin');
    await cli(['cp', `${B}/verify-big/mp-32m.bin`, dl], { timeout: 240000 });
    need(await sha256file(dl) === sha, '32 MiB multipart round-trip sha mismatch');
    return `32 MiB multipart round-trip sha256-identical in ${((Date.now() - t0) / 1000).toFixed(1)}s`;
  });

  await verify({ id: 'CLI-S3-30', area: 'objects', action: 'Pagination across the 1000-key page boundary', ds: 'S3 (MinIO)', scenario: '1006 objects (incl. one empty) — recursive ls must return every one across the S3 1000-key page boundary, then a gated mass delete clears them all', face: 'CLI' }, async () => {
    const P = path.join(FIX, 'pages');
    await rm(P, { recursive: true, force: true });
    await mkdir(P, { recursive: true });
    const N = 1005;
    for (let i = 0; i < N; i++) await writeFile(path.join(P, `p-${String(i).padStart(4, '0')}.txt`), `page ${i}\n`);
    await writeFile(path.join(P, 'p-empty.txt'), ''); // an empty file inside the mass
    const total = N + 1;
    let r = await cli(['cp', '-r', P, `${B}/verify-pages/`], { timeout: 600000 });
    need(r.code === 0, `cp -r ${total} files: ${r.out}${r.err}`);
    r = await cli(['ls', `${B}/verify-pages/`, '--recursive', '--json'], { timeout: 120000, });
    const got = countLines(r.out, '"key"');
    need(got === total, `pagination: listed ${got} of ${total} — the page boundary at 1000 lost ${total - got}`);
    r = await cli(['find', `${B}/verify-pages/`, '--name', 'p-1004.txt', '--json']);
    need(r.code === 0 && countLines(r.out, '"key"') === 1, `last object findable: ${r.out}`);
    r = await cli(['rm', '-r', `${B}/verify-pages/`, '--force'], { timeout: 600000 });
    need(r.code === 0, `mass rm of ${total}: ${r.out}${r.err}`);
    r = await cli(['ls', `${B}/verify-pages/`, '--recursive', '--json']);
    need(countLines(r.out, '"key"') === 0, 'prefix not empty after mass delete');
    return `${total} objects listed exactly across the 1000-key boundary; mass delete clean`;
  });

  await verify({ id: 'CLI-S3-31', area: 'objects', action: 'Hostile key names round-trip', ds: 'S3 (MinIO)', scenario: 'spaces, unicode, %2F-literal, + = &, leading dot, 150-char names, 12-deep nesting, quotes — exact-name listing, per-key stat, byte round-trip, and a presigned fetch of the %2F hazard', face: 'CLI' }, async () => {
    const ND = path.join(FIX, 'names');
    await rm(ND, { recursive: true, force: true });
    const legal = {
      'space name.txt': 'spaces are fine\n',
      'uni-Ωθ-ключ-日本語.txt': 'unicode keys\n',
      'pct %2F edge 100%.txt': 'percent hazard\n',
      'plus+eq=a&b.txt': 'query hazards\n',
      '.dotfile': 'leading dot\n',
      [`${'L'.repeat(150)}.txt`]: 'long name\n',
    };
    for (const [n, c] of Object.entries(legal)) {
      await mkdir(path.dirname(path.join(ND, n)), { recursive: true });
      await writeFile(path.join(ND, n), c);
    }
    let deep = ND;
    for (let i = 1; i <= 12; i++) deep = path.join(deep, `d${i}`);
    await mkdir(deep, { recursive: true });
    await writeFile(path.join(deep, 'leaf.txt'), 'deep leaf\n');
    let r = await cli(['cp', '-r', ND, `${B}/verify-names/`], { timeout: 120000 });
    need(r.code === 0, `cp -r hostile names: ${r.out}${r.err}`);
    // remote-only keys Windows cannot host locally — the URI path must carry
    // them verbatim (execFile passes args without a shell, so no quoting loss)
    const nasty = ['say "hi".txt', "it's an apostrophe.txt"];
    for (const n of nasty) {
      r = await cli(['cp', path.join(FIX, 'data', 'root-1.txt'), `${B}/verify-names/${n}`]);
      need(r.code === 0, `cp nasty ${n}: ${r.out}${r.err}`);
      r = await cli(['stat', `${B}/verify-names/${n}`]);
      need(/size:/.test(r.out), `stat nasty ${n}: ${r.out}${r.err}`);
    }
    const l = await cli(['ls', `${B}/verify-names/`, '--recursive']);
    for (const n of [...Object.keys(legal), ...nasty]) need(l.out.includes(n), `ls lost exact name: ${n}`);
    need(l.out.includes('leaf.txt'), 'deep leaf not listed');
    // byte round-trip of the Windows-legal set
    for (const [n, c] of Object.entries(legal)) {
      const out = path.join(ART, 'names-dl', n);
      await mkdir(path.dirname(out), { recursive: true });
      r = await cli(['cp', `${B}/verify-names/${n}`, out]);
      need(r.code === 0 && (await readFile(out)).equals(Buffer.from(c)), `round-trip bytes differ: ${n}`);
    }
    // the %2F-literal must presign+fetch WITHOUT being decoded into a slash
    r = await cli(['presign', `${B}/verify-names/pct %2F edge 100%.txt`, '--json']);
    // JSON.parse, never a regex over the raw text: the JSON escapes "&"
    // as \u0026 and a regex hands fetch a mangled query — the URL then
    // carries no signature and the fetch silently degrades to an
    // ANONYMOUS request (200 while a public policy leaked from S3-21,
    // 403 once the private default is restored: the oracle would never
    // have tested the signature at all)
    let pres = null;
    try { pres = JSON.parse(r.out); } catch { pres = null; }
    need(pres && pres.url, `presign hostile: ${r.out}${r.err}`);
    const res = await fetch(pres.url);
    // one body read for both the failure message (the error XML names
    // the reason — SignatureDoesNotMatch vs AccessDenied vs expired)
    // and the byte check
    const body = await res.text();
    need(res.ok, `presign fetch of %2F name: HTTP ${res.status}: ${body.replace(/\s+/g, ' ').slice(0, 240)}`);
    need(body === 'percent hazard\n', 'presigned %2F name delivered wrong bytes');
    await cli(['rm', '-r', `${B}/verify-names/`, '--force']);
    return `${Object.keys(legal).length + nasty.length + 1} hostile keys exact-listed; legal set byte-identical; %2F presigned clean`;
  });

  await verify({ id: 'CLI-S3-32', area: 'transfers', action: 'Concurrency: parallel workload + same-key race', ds: 'S3 (MinIO)', scenario: '5 CLI processes at once (3 uploads, 1 download, 1 listing) all succeed byte-exact; two simultaneous writes to ONE key serialize into clean versions — never a torn object', face: 'CLI' }, async () => {
    const par = path.join(ART, 'par');
    await rm(par, { recursive: true, force: true });
    await mkdir(par, { recursive: true });
    const srcs = [];
    for (let i = 1; i <= 3; i++) {
      const p = path.join(par, `par-${i}.bin`);
      await writeFile(p, randomBytes(4 * 1024 * 1024));
      srcs.push(p);
    }
    const dl = path.join(par, 'readme-dl.txt');
    const rs = await Promise.all([
      cli(['cp', srcs[0], `${B}/par/f1.bin`]),
      cli(['cp', srcs[1], `${B}/par/f2.bin`]),
      cli(['cp', srcs[2], `${B}/par/f3.bin`]),
      cli(['cp', `${B}/readme.md`, dl]),
      cli(['ls', B, '--recursive', '--json']),
    ]);
    rs.forEach((r, i) => need(r.code === 0, `parallel job ${i + 1}: exit ${r.code} ${r.err}`));
    for (let i = 0; i < 3; i++) {
      const out = path.join(par, `f${i + 1}.dl.bin`);
      await cli(['cp', `${B}/par/f${i + 1}.bin`, out]);
      need(await sha256file(out) === await sha256file(srcs[i]), `parallel upload ${i + 1} bytes differ`);
    }
    need((await readFile(dl)).equals(await readFile(path.join(FIX, 'data', 'readme.md'))), 'parallel download bytes differ');
    // same-key race: distinct sizes so a torn write cannot masquerade as either
    const KA = path.join(par, 'race-a.bin'), KB = path.join(par, 'race-b.bin');
    await writeFile(KA, Buffer.alloc(100_000, 0x41));
    await writeFile(KB, Buffer.alloc(200_000, 0x42));
    const K = `${B}/race/race.bin`;
    const [ra, rb] = await Promise.all([cli(['cp', KA, K]), cli(['cp', KB, K])]);
    need(ra.code === 0 && rb.code === 0, `race exits: ${ra.code}/${rb.code} ${ra.err}${rb.err}`);
    const v = await cli(['versions', 'ls', K, '--json']);
    const nv = countLines(v.out, '"versionId"');
    need(nv >= 2, `same-key race collapsed to ${nv} version(s): ${v.out}`);
    const latest = path.join(par, 'race-latest.bin');
    await cli(['cp', K, latest]);
    const lab = await readFile(latest);
    need(lab.equals(Buffer.alloc(100_000, 0x41)) || lab.equals(Buffer.alloc(200_000, 0x42)), 'same-key race left a torn write as latest');
    await cli(['rm', K, '--force']);
    return `5 parallel ops byte-exact; race → ${nv} clean versions, latest intact`;
  });

  await verify({ id: 'CLI-S3-33', area: 'transfers', action: 'SSE-S3 server-side encryption (--sse AES256)', ds: 'S3 (MinIO)', scenario: 'cp --sse AES256 uploads with the SSE header; the object round-trips byte-identical (SKIP records a provider gap when the engine rejects SSE)', face: 'CLI' }, async () => {
    let r = await cli(['cp', path.join(FIX, 'data', 'root-1.txt'), `${B}/verify-sse/enc.txt`, '--sse', 'AES256']);
    if (r.code !== 0) return skip(`provider gap — SSE rejected: ${(r.out + r.err).split('\n')[0]}`);
    r = await cli(['stat', `${B}/verify-sse/enc.txt`]);
    need(/size:/.test(r.out), `stat sse object: ${r.out}${r.err}`);
    const out = path.join(ART, 'sse-dl.txt');
    await cli(['cp', `${B}/verify-sse/enc.txt`, out]);
    need((await readFile(out)).equals(await readFile(path.join(FIX, 'data', 'root-1.txt'))), 'SSE round-trip bytes differ');
    return 'SSE-S3 AES256 accepted; round-trip byte-identical';
  });

  await verify({ id: 'CLI-S3-34', area: 'security', action: 'Presign expiry: granted TTL + fails closed', ds: 'S3 (MinIO)', scenario: 'a 2-second grant: the URL itself must carry exactly X-Amz-Expires=2 and serve while live; after expiry the SAME URL must be refused (providers with a clock-skew grace that keeps serving record the gap as SKIP)', face: 'CLI' }, async () => {
    let r = await cli(['presign', `${B}/readme.md`, '--expires', '2s', '--json']);
    need(r.code === 0, `presign: ${r.out}${r.err}`);
    let url;
    try { url = JSON.parse(r.out).url; } catch { url = (r.out.match(/"url":\s*"([^"]+)"/) || [])[1]; }
    need(url, `presign json: ${r.out}`);
    url = url.replace(/\\u0026/g, '&');
    need(/[?&]X-Amz-Expires=2(&|$)/.test(url), `grant does not carry the requested TTL: ${url}`);
    const early = await fetch(url);
    need(early.ok, `pre-expiry fetch: HTTP ${early.status}`);
    need((await early.text()) === 'verification readme\n', 'pre-expiry fetch delivered wrong bytes');
    await sleep(3500);
    const late = await fetch(url);
    if (late.ok) return skip(`provider gap — engine serves the expired grant (HTTP ${late.status}, clock-skew grace)`);
    const body = await late.text();
    need(!body.includes('verification readme'), 'expired URL delivered the object');
    return `2s grant: served, then refused (HTTP ${late.status}) after expiry`;
  });

  await verify({ id: 'CLI-S3-35', area: 'security', action: 'Object lock (WORM): retention + legal hold enforced', ds: 'S3 (MinIO)', scenario: 'a lock-enabled bucket (mb --object-lock, irreversible): GOVERNANCE retention and legal hold each must defeat a version-purge attempt (rm --versions) while in force — the version survives and the refusal is reported; clearing each lock re-arms the delete; the emptied bucket is then removable', face: 'CLI' }, async () => {
    const W = `${BUCKET}-worm`;
    let r = await cli(['mb', `s3://${W}`, '--object-lock']);
    need(r.code === 0, `mb --object-lock: ${r.out}${r.err}`);
    const K = `s3://${W}/worm-${RUNID}.bin`;
    const src = path.join(ART, 'worm.bin');
    await writeFile(src, `worm payload ${RUNID}\n`);
    r = await cli(['cp', src, K]);
    need(r.code === 0, `cp: ${r.out}${r.err}`);
    const verCount = async () => countLines((await cli(['versions', 'ls', K, '--json'])).out, '"versionId"');
    need(await verCount() === 1, 'seed version missing');
    // GOVERNANCE retention in force → the purge attempt must be refused
    // server-side. NOTE: rm's exit code stays 0 (the batch API reports
    // per-item refusals, the CLI prints them) — the CONTRACT is behavioral:
    // the version survives the attempt and the refusal is on the record.
    r = await cli(['lock', 'retention', K, '--mode', 'GOVERNANCE', '--until', '+1h']);
    need(r.code === 0, `set retention: ${r.out}${r.err}`);
    r = await cli(['lock', 'retention', K]);
    need(/GOVERNANCE/.test(r.out), `retention show: ${r.out}${r.err}`);
    r = await cli(['rm', '--versions', K]);
    need(/error|denied|fail/i.test(r.out + r.err), `purge of a retained object was silent: ${r.out}${r.err}`);
    need(await verCount() === 1, 'GOVERNANCE retention did not survive the purge attempt');
    // legal hold: the same wall, independently
    r = await cli(['lock', 'legalhold', K, '--on']);
    need(r.code === 0, `legalhold on: ${r.out}${r.err}`);
    r = await cli(['lock', 'retention', K, '--clear', '--bypass-governance']);
    need(r.code === 0, `clear retention: ${r.out}${r.err}`);
    r = await cli(['rm', '--versions', K]);
    need(/error|denied|fail/i.test(r.out + r.err), `purge under legal hold was silent: ${r.out}${r.err}`);
    need(await verCount() === 1, 'legal hold did not survive the purge attempt');
    // hold off → the delete finally goes through, and the bucket follows
    r = await cli(['lock', 'legalhold', K, '--off']);
    need(r.code === 0, `legalhold off: ${r.out}${r.err}`);
    r = await cli(['rm', '--versions', K]);
    need(await verCount() === 0, `unlocked object survived the purge: ${r.out}${r.err}`);
    r = await cli(['rb', `s3://${W}`, '--force']);
    need(r.code === 0, `rb: ${r.out}${r.err}`);
    return 'retention + hold each blocked the purge; unlock re-armed it; bucket removed';
  });

  await verify({ id: 'CLI-S3-36', area: 'admin', action: 'Data-protection toggles: versioning suspend/resume + public access block', ds: 'S3 (MinIO)', scenario: 'a scratch bucket: suspend versioning → info reads it back suspended and the suspended write lands as the null version while the existing timeline survives untouched; resume → new writes version again; PAB put --all arms all four blocks and a bare put disarms them — or the provider gap is recorded (this MinIO build rejects the whole PAB API; the aws CLI agrees)', face: 'CLI' }, async () => {
    const PB = `${BUCKET}-prot`;
    let r = await cli(['mb', `s3://${PB}`]);
    need(r.code === 0, `mb: ${r.out}${r.err}`);
    r = await cli(['bucket', 'versioning', `s3://${PB}`, 'on']);
    need(/versioning enabled/.test(r.out), `versioning on: ${r.out}${r.err}`);
    const K = `s3://${PB}/prot.txt`;
    const A = path.join(ART, 'prot-a.txt');
    const C = path.join(ART, 'prot-c.txt');
    const D = path.join(ART, 'prot-d.txt');
    await writeFile(A, 'prot-v1\n');
    await writeFile(C, 'prot-null-replace\n');
    await writeFile(D, 'prot-resumed\n');
    await cli(['cp', A, K]);
    await cli(['cp', C, K, '--force']); // a second versioned write → timeline of 2
    const nVers = async () => countLines((await cli(['versions', 'ls', K, '--json'])).out, '"versionId"');
    need(await nVers() === 2, 'seeded timeline is not 2 versions');
    // suspend: the protection state flips server-side and reads back
    r = await cli(['bucket', 'versioning', `s3://${PB}`, 'off']);
    need(/versioning suspended/.test(r.out), `versioning off: ${r.out}${r.err}`);
    r = await cli(['bucket', 'info', `s3://${PB}`]);
    need(/suspended/i.test(r.out), `info after suspend: ${r.out}`);
    // a suspended write becomes the null version — the old timeline must
    // survive it untouched (suspend never destroys history)
    await cli(['cp', C, K, '--force']);
    need(await nVers() === 3, 'a suspended write disturbed the existing timeline');
    const dl = path.join(ART, 'prot-dl.txt');
    await cli(['cp', K, dl]);
    need((await readFile(dl)).toString() === 'prot-null-replace\n', 'suspended write is not the current bytes');
    // resume: new writes create versions again
    r = await cli(['bucket', 'versioning', `s3://${PB}`, 'on']);
    need(/versioning enabled/.test(r.out), `versioning resume: ${r.out}${r.err}`);
    await cli(['cp', D, K, '--force']);
    need(await nVers() === 4, 'a resumed write did not version');
    const PABKEYS = ['blockPublicAcls', 'ignorePublicAcls', 'blockPublicPolicy', 'restrictPublicBuckets'];
    r = await cli(['bucket', 'pab', 'put', `s3://${PB}`, '--all']);
    // Provider gap (MinIO RELEASE.2025-09-07): PAB put answers MalformedXML
    // and PAB get NotImplemented — the official aws CLI gets the same two
    // refusals, so this is the server, not the client. Record it and keep
    // the row green on the versioning semantics that did verify.
    if (r.code !== 0 && /malformedxml|notimplemented|not supported/i.test(r.out + r.err)) {
      await cli(['rb', `s3://${PB}`, '--force']);
      return 'suspend kept history (null-version semantics); resume re-versions; PAB rejected by this MinIO build (recorded provider gap — aws CLI agrees)';
    }
    need(r.code === 0, `pab put --all: ${r.out}${r.err}`);
    let p = JSON.parse((await cli(['bucket', 'pab', 'get', `s3://${PB}`, '--json'])).out || '{}');
    need(PABKEYS.every((k) => p[k] === true), `pab get after --all: ${JSON.stringify(p)}`);
    r = await cli(['bucket', 'pab', 'put', `s3://${PB}`]);
    need(r.code === 0, `pab put (none): ${r.out}${r.err}`);
    p = JSON.parse((await cli(['bucket', 'pab', 'get', `s3://${PB}`, '--json'])).out || '{}');
    need(PABKEYS.every((k) => !p[k]), `pab get after none: ${JSON.stringify(p)}`);
    await cli(['rb', `s3://${PB}`, '--force']);
    return 'suspend kept history (null-version semantics); resume re-versions; PAB on/off read back';
  });

  await verify({ id: 'CLI-S3-37', area: 'admin', action: 'Bucket CORS: put FILE / get --json / delete', ds: 'S3 (MinIO)', scenario: 'a JSON rules file round-trips: put saves, get --json echoes every field (origin, method, header, max-age), delete removes it all and get then reports none — each mutation re-read out-of-band before the next', face: 'CLI' }, async () => {
    const TB = `s3://${BUCKET}-cors`;
    let r = await cli(['mb', TB]);
    need(r.code === 0, `mb cors: ${r.err}`);
    const f = path.join(ART, 'cors-rules.json');
    await writeFile(f, JSON.stringify([{ origins: ['https://verify.example.com'], methods: ['GET', 'PUT'], headers: ['content-type'], expose: ['ETag'], maxAge: 1800 }]));
    r = await cli(['bucket', 'cors', 'put', TB, f]);
    // provider gap: some builds refuse bucket CORS mutation outright
    if (r.code !== 0 && /not supported|not implemented|malformed|invalid/i.test(r.out + r.err)) {
      await cli(['rb', TB, '--force']);
      return skip(`provider refused CORS put (recorded gap): ${(r.out + r.err).trim().slice(0, 120)}`);
    }
    need(r.code === 0, `cors put: ${r.out}${r.err}`);
    const g = await cli(['bucket', 'cors', 'get', TB, '--json']);
    need(g.code === 0, `cors get: ${g.out}${g.err}`);
    for (const needle of ['verify.example.com', 'GET', 'content-type', 'ETag', '1800']) {
      need(g.out.includes(needle), `cors get lost "${needle}": ${g.out}`);
    }
    r = await cli(['bucket', 'cors', 'delete', TB]);
    need(r.code === 0, `cors delete: ${r.out}${r.err}`);
    const gone = await cli(['bucket', 'cors', 'get', TB, '--json']);
    need(!gone.out.includes('verify.example.com'), `cors rule survived delete: ${gone.out}`);
    await cli(['rb', TB, '--force']);
    return 'cors put/get/delete round-trip, fields echoed';
  });

  await verify({ id: 'CLI-S3-38', area: 'admin', action: 'Bucket website: put flags / get / delete', ds: 'S3 (MinIO)', scenario: 'website hosting configured through flags (--index, --error, --redirect-host/proto): get echoes the exact documents, the redirect override replaces them, delete clears the config', face: 'CLI' }, async () => {
    const TB = `s3://${BUCKET}-web`;
    let r = await cli(['mb', TB]);
    need(r.code === 0, `mb web: ${r.err}`);
    r = await cli(['bucket', 'website', 'put', TB, '--index', 'verify-index.html', '--error', 'verify-404.html']);
    if (r.code !== 0 && /not supported|not implemented|malformed|invalid/i.test(r.out + r.err)) {
      await cli(['rb', TB, '--force']);
      return skip(`provider refused website put (recorded gap): ${(r.out + r.err).trim().slice(0, 120)}`);
    }
    need(r.code === 0, `website put: ${r.out}${r.err}`);
    let g = await cli(['bucket', 'website', 'get', TB, '--json']);
    need(g.code === 0, `website get: ${g.out}${g.err}`);
    need(g.out.includes('verify-index.html') && g.out.includes('verify-404.html'), `website get lost the documents: ${g.out}`);
    r = await cli(['bucket', 'website', 'put', TB, '--redirect-host', 'verify.example.net', '--redirect-proto', 'http']);
    need(r.code === 0, `website put redirect: ${r.out}${r.err}`);
    g = await cli(['bucket', 'website', 'get', TB, '--json']);
    need(g.out.includes('verify.example.net'), `redirect host not echoed: ${g.out}`);
    r = await cli(['bucket', 'website', 'delete', TB]);
    need(r.code === 0, `website delete: ${r.out}${r.err}`);
    g = await cli(['bucket', 'website', 'get', TB, '--json']);
    need(!g.out.includes('verify.example.net'), `website config survived delete: ${g.out}`);
    await cli(['rb', TB, '--force']);
    return 'website documents + redirect override round-trip';
  });

  await verify({ id: 'CLI-S3-39', area: 'admin', action: 'Bucket encryption: default SSE put / get / delete', ds: 'S3 (MinIO)', scenario: 'default server-side encryption set to AES256: get echoes the algorithm, delete returns the bucket to provider-default and get says so — the at-rest contract visible from outside', face: 'CLI' }, async () => {
    const TB = `s3://${BUCKET}-enc`;
    let r = await cli(['mb', TB]);
    need(r.code === 0, `mb enc: ${r.err}`);
    r = await cli(['bucket', 'encryption', 'put', TB, '--algo', 'AES256']);
    if (r.code !== 0 && /not supported|not implemented|malformed|invalid/i.test(r.out + r.err)) {
      await cli(['rb', TB, '--force']);
      return skip(`provider refused default encryption put (recorded gap): ${(r.out + r.err).trim().slice(0, 120)}`);
    }
    need(r.code === 0, `encryption put: ${r.out}${r.err}`);
    let g = await cli(['bucket', 'encryption', 'get', TB]);
    need(g.code === 0, `encryption get: ${g.out}${g.err}`);
    need(/AES256/i.test(g.out), `encryption get lost AES256: ${g.out}`);
    r = await cli(['bucket', 'encryption', 'delete', TB]);
    need(r.code === 0, `encryption delete: ${r.out}${r.err}`);
    g = await cli(['bucket', 'encryption', 'get', TB]);
    need(!/AES256/.test(g.out) || /none|default/i.test(g.out), `encryption survived delete: ${g.out}`);
    await cli(['rb', TB, '--force']);
    return 'default SSE on/off readable from outside';
  });

  await verify({ id: 'CLI-S3-40', area: 'admin', action: 'Object-lock enable on a plain bucket: refused honestly', ds: 'S3 (MinIO)', scenario: 'a bucket created WITHOUT --object-lock: enabling lock must be refused (non-zero, a stated reason) — and the refusal must leave the bucket itself healthy (still stats, still takes writes, still removable)', face: 'CLI' }, async () => {
    const TB = `s3://${BUCKET}-nl`;
    let r = await cli(['mb', TB]);
    need(r.code === 0, `mb nl: ${r.err}`);
    r = await cli(['bucket', 'lock', TB, '--enable']);
    if (r.code === 0 && /object lock enabled/i.test(r.out)) {
      // recorded gap, not a pass: AWS refuses this outright; a provider that
      // allows it changes the retention safety story and must stay visible
      await cli(['rb', TB, '--force']);
      return skip('provider ALLOWED enabling object lock on a plain bucket (recorded gap — AWS semantics refuse this)');
    }
    need(r.code !== 0, 'lock --enable on a non-lock bucket exited 0');
    need((r.out + r.err).trim().length > 0, 'the refusal printed no reason');
    const info = await cli(['bucket', 'info', TB]);
    need(info.code === 0, `bucket unhealthy after refusal: ${info.out}${info.err}`);
    const src = path.join(ART, 'nl-probe.txt');
    await writeFile(src, `nl probe ${RUNID}\n`);
    r = await cli(['cp', src, `${TB}/probe.txt`]);
    need(r.code === 0, `write after refusal: ${r.out}${r.err}`);
    const st = await cli(['stat', `${TB}/probe.txt`]);
    need(st.code === 0 && /probe\.txt/.test(st.out), `stat after refusal: ${st.out}${st.err}`);
    await cli(['rb', TB, '--force']);
    return 'refused with a reason; bucket still healthy and writable';
  });

  await verify({ id: 'CLI-S3-41', area: 'sources', action: 'source add UPDATE semantics + secret masking', ds: 'S3 (dead endpoint)', scenario: 're-adding an EXISTING name must never fork the store: either the entry updates in place (one row, new endpoint live, old endpoint gone) or the add is refused as a duplicate — and the secret never appears in any listing, on either path', face: 'CLI' }, async () => {
    const dir = path.join(ART, 'upd-config');
    await rm(dir, { recursive: true, force: true });
    await mkdir(dir, { recursive: true });
    let r = await cli(['source', 'add', 'upd1', '--type', 's3', '--endpoint', 'http://127.0.0.1:1', '--access-key', 'updkey', '--secret-key', 'upd1-s3cret-zz'], { cfg: dir });
    need(r.code === 0, `add: ${r.err}`);
    let l = await cli(['source', 'list'], { cfg: dir });
    need(l.code === 0 && countLines(l.out, 'upd1') === 1, `first add left ${countLines(l.out, 'upd1')} row(s)`);
    r = await cli(['source', 'add', 'upd1', '--type', 's3', '--endpoint', 'http://127.0.0.2:2', '--access-key', 'updkey', '--secret-key', 'upd1-s3cret-zz'], { cfg: dir });
    l = await cli(['source', 'list'], { cfg: dir });
    need(l.code === 0, `list after re-add: ${l.err}`);
    need(!l.out.includes('upd1-s3cret-zz'), 'the secret leaked into a listing');
    if (r.code === 0) {
      // update-in-place contract: exactly one row, the NEW endpoint live
      need(countLines(l.out, 'upd1') === 1, `re-add forked the store (${countLines(l.out, 'upd1')} rows)`);
      need(/127\.0\.0\.2:2/.test(l.out) && !/127\.0\.0\.1:1/.test(l.out), `re-add did not move the endpoint: ${l.out}`);
      const t = await cli(['source', 'test', 'upd1'], { cfg: dir, timeout: 45000 });
      need(t.code !== 0, 'source test dialed a DEAD endpoint — the updated entry is not the live one');
    } else {
      // refused-duplicate contract: the message says so and the store is untouched
      need(/exist|duplicate|taken|already/i.test(r.out + r.err), `refusal not labeled as a name conflict: ${r.out}${r.err}`);
      need(/127\.0\.0\.1:1/.test(l.out) && !/127\.0\.0\.2:2/.test(l.out), `refused add still changed the store: ${l.out}`);
      const t = await cli(['source', 'test', 'upd1'], { cfg: dir, timeout: 45000 });
      need(t.code !== 0, 'source test dialed something live on a dead endpoint');
    }
    await cli(['source', 'remove', 'upd1'], { cfg: dir });
    await cli(['source', 'remove', 'upd1-2'], { cfg: dir });
    return r.code === 0
      ? 're-add updated in place: one row, new endpoint, secret masked'
      : 're-add refused as duplicate: store untouched, secret masked';
  });

  await verify({ id: 'CLI-S3-42', area: 'meta', action: '--json machine contract', ds: 'S3 (MinIO)', scenario: 'every JSON-emitting read (ls, tree, du, stat, versions ls, bucket info) on live data: the whole stdout parses as machine JSON — a single JSON document or newline-delimited objects — with zero ANSI escapes anywhere, and the parsed values carry the real semantics (row counts, keys, version count) — the contract automation is built on', face: 'CLI' }, async () => {
    const P = `s3://${BUCKET}/verify-json`;
    await cli(['mkdir', `${P}/`]);
    await cli(['cp', path.join(FIX, 'data', 'root-1.txt'), `${P}/j1.txt`]);
    await cli(['cp', path.join(FIX, 'data', 'root-2.txt'), `${P}/j1.txt`, '--force']);
    await cli(['cp', path.join(FIX, 'data', 'empty.txt'), `${P}/j2.txt`]);
    const runs = [
      ['ls', ['ls', `${P}/`, '--json'], (rows) => rows.length === 2],
      ['tree', ['tree', `${P}/`, '--json'], (rows) => rows.length >= 1],
      ['du', ['du', `${P}/`, '--json'], (rows) => rows.length >= 1],
      ['stat', ['stat', `${P}/j2.txt`, '--json'], (rows) => rows.some((x) => String(x.key || '').endsWith('j2.txt'))],
      ['versions ls', ['versions', 'ls', `${P}/j1.txt`, '--json'], (rows) => rows.length === 2],
      ['bucket info', ['bucket', 'info', `s3://${BUCKET}`, '--json'], (rows) => rows.length >= 1],
    ];
    for (const [name, args, check] of runs) {
      const r = await cli(args);
      need(r.code === 0, `${name} --json: ${r.out}${r.err}`);
      need(!/\x1b\[/.test(r.out + r.err), `${name} --json leaked ANSI escapes`);
      const trimmed = r.out.trim();
      need(trimmed.length > 0, `${name} --json printed nothing`);
      let rows;
      const doc = JSON.parse(trimmed); // a whole-document parse is the contract
      rows = Array.isArray(doc) ? doc : [doc];
      need(check(rows), `${name} --json semantics wrong: ${JSON.stringify(rows).slice(0, 160)}`);
    }
    return `${runs.length} JSON read paths parse as machine JSON with correct semantics, no ANSI`;
  });

  await verify({ id: 'CLI-S3-43', area: 'transfers', action: 'cp boundary sizes: 0 / 1 byte / exactly 5 MiB', ds: 'S3 (MinIO)', scenario: 'the multipart boundary is 5 MiB: a 0-byte file, a 1-byte file and an EXACTLY-5242880-byte file must each land with stat reporting the exact size — and the 5 MiB boundary file round-trips sha-identical (an off-by-one part boundary corrupts exactly here)', face: 'CLI' }, async () => {
    const P = `s3://${BUCKET}/verify-size`;
    await cli(['mkdir', `${P}/`]);
    const files = [
      ['b0.txt', Buffer.alloc(0)],
      ['b1.txt', Buffer.from('x')],
      ['b5m.bin', randomBytes(5 * 1024 * 1024)],
    ];
    const sizes = { 'b0.txt': 0, 'b1.txt': 1, 'b5m.bin': 5242880 };
    for (const [n, buf] of files) {
      const src = path.join(ART, n);
      await writeFile(src, buf);
      const r = await cli(['cp', src, `${P}/${n}`]);
      need(r.code === 0, `cp ${n}: ${r.out}${r.err}`);
      const st = await cli(['stat', `${P}/${n}`]);
      need(st.code === 0 && new RegExp(`\\(${sizes[n]} bytes\\)`).test(st.out), `stat ${n} did not report exactly ${sizes[n]} bytes: ${st.out}`);
    }
    const sha = await sha256file(path.join(ART, 'b5m.bin'));
    const back = path.join(ART, 'b5m-back.bin');
    await rm(back, { force: true });
    const dl = await cli(['cp', `${P}/b5m.bin`, back]);
    need(dl.code === 0, `download b5m: ${dl.out}${dl.err}`);
    need(await sha256file(back) === sha, 'the exact-5 MiB boundary file round-tripped with different bytes');
    return '0/1/5242880-byte files exact; boundary file sha-identical';
  });
}

// ============================================================
// CLI battery — cross-engine (SFTP / FTP / WebDAV)
// ============================================================
async function cliCross() {
  const haveSftp = await portOpen(SFTP_PORT);
  const haveFtp = await portOpen(FTP_PORT);
  const haveDav = await portOpen(WEBDAV_PORT);
  if (!haveSftp && !haveFtp && !haveDav) {
    await verify({ id: 'CLI-X-00', area: 'sources', action: 'Remote engines', ds: 'SFTP/FTP/WebDAV', scenario: 'containers not running', face: 'CLI' },
      () => skip('no engine containers reachable (2222/2121/7070)'));
    return;
  }

  await verify({ id: 'CLI-X-01', area: 'sources', action: 'Add + test remote sources', ds: 'SFTP/FTP/WebDAV', scenario: 'sftp:// and webdav:// URL shorthand + ftp flags; source test dials each', face: 'CLI' }, async () => {
    if (haveSftp) {
      const r = await cli(['source', 'add', 'xt', `sftp://${E2E_USER}:${E2E_PASS}@127.0.0.1:${SFTP_PORT}/upload`]);
      need(r.code === 0, `sftp add: ${r.err}`);
      const t = await cli(['source', 'test', 'xt']);
      need(t.code === 0, `sftp test: ${t.out}${t.err}`);
    }
    if (haveFtp) {
      const r = await cli(['source', 'add', 'xf', '--type', 'ftp', '--host', '127.0.0.1', '--port', String(FTP_PORT), '--username', E2E_USER, '--password', E2E_PASS]);
      need(r.code === 0, `ftp add: ${r.err}`);
      const t = await cli(['source', 'test', 'xf']);
      need(t.code === 0, `ftp test: ${t.out}${t.err}`);
    }
    if (haveDav) {
      const r = await cli(['source', 'add', 'xw', `webdav://${E2E_USER}:${E2E_PASS}@127.0.0.1:${WEBDAV_PORT}/`]);
      need(r.code === 0, `webdav add: ${r.err}`);
      const t = await cli(['source', 'test', 'xw']);
      need(t.code === 0, `webdav test: ${t.out}${t.err}`);
    }
    return [haveSftp && 'sftp', haveFtp && 'ftp', haveDav && 'webdav'].filter(Boolean).join('+') + ' tested';
  });

  // The user-facing flagship: MULTI-FILE COPY with an FTP data source.
  if (haveFtp) {
    await verify({ id: 'CLI-X-02', area: 'transfers', action: 'Multi-file copy local→FTP', ds: 'FTP', scenario: `cp -r fixture tree (${NFILES} files: unicode, empty file, nested dirs) → xf://${RUNID}/tree`, face: 'CLI' }, async () => {
      const r = await cli(['cp', '-r', path.join(FIX, 'data'), `xf://${RUNID}/tree`, '--json']);
      need(r.code === 0 && r.out.includes(`"items": ${NFILES}`), `cp -r: ${r.out}${r.err}`);
      const l = await cli(['ls', `xf://${RUNID}/tree`, '--recursive', '--json']);
      for (const f of Object.keys(FIXTREE)) need(l.out.includes(f), `${f} missing from the FTP listing`);
      return `${NFILES} files on the FTP source`;
    });

    await verify({ id: 'CLI-X-03', area: 'transfers', action: 'Multi-file copy FTP→S3 (cross-engine)', ds: 'FTP → S3', scenario: 'cp -r the FTP tree into the bucket; count + full byte round-trip back to disk', face: 'CLI' }, async () => {
      let r = await cli(['cp', '-r', `xf://${RUNID}/tree`, `s3://${BUCKET}/from-ftp/`, '--json']);
      need(r.code === 0 && r.out.includes(`"items": ${NFILES}`), `cp: ${r.out}${r.err}`);
      r = await cli(['ls', `s3://${BUCKET}/from-ftp/`, '--recursive', '--json']);
      need(countLines(r.out, '"key"') === NFILES, 's3 count after copy');
      const out = path.join(ART, 'from-ftp');
      await rm(out, { recursive: true, force: true });
      await cli(['cp', '-r', `s3://${BUCKET}/from-ftp/`, out]);
      const want = await treeOf(path.join(FIX, 'data'));
      const got = await treeOf(out);
      need(sameTree(want, got), 'FTP→S3 bytes differ on round-trip');
      return `${NFILES} files cross-engine, byte-identical`;
    });

    await verify({ id: 'CLI-X-04', area: 'objects', action: 'Remote browse + delete gates', ds: 'FTP', scenario: 'ls/du/stat/tree on the remote; rm -r dry-run counts; --force deletes; prefix gone', face: 'CLI' }, async () => {
      let r = await cli(['ls', `xf://${RUNID}/tree/docs/`]);
      need(/doc-01/.test(r.out), `remote ls: ${r.out}`);
      r = await cli(['du', `xf://${RUNID}/tree`]);
      need(/object/.test(r.out), `remote du: ${r.out}`);
      r = await cli(['stat', `xf://${RUNID}/tree/readme.md`]);
      need(/size:/.test(r.out), `remote stat: ${r.out}`);
      r = await cli(['tree', `xf://${RUNID}/tree`]);
      need(/docs\//.test(r.out), `remote tree: ${r.out}`);
      r = await cli(['rm', `xf://${RUNID}/tree`, '-r', '--dry-run']);
      need(/total: \d+/.test(r.out), `remote dry-run: ${r.out}`);
      r = await cli(['rm', `xf://${RUNID}/tree`, '-r', '--force']);
      need(r.code === 0, `remote rm: ${r.out}${r.err}`);
      r = await cli(['ls', `xf://${RUNID}/`, '--json']);
      need(!r.out.includes('tree/'), 'tree still listed');
      return 'browse + gated delete verified';
    });
  }

  if (haveSftp) {
    await verify({ id: 'CLI-X-05', area: 'transfers', action: 'SFTP round-trip', ds: 'SFTP', scenario: 'upload tree → download tree → byte-identical diff', face: 'CLI' }, async () => {
      let r = await cli(['cp', '-r', path.join(FIX, 'data'), `xt://${RUNID}/tree`]);
      need(r.code === 0, `sftp up: ${r.err}`);
      const out = path.join(ART, 'from-sftp');
      await rm(out, { recursive: true, force: true });
      r = await cli(['cp', '-r', `xt://${RUNID}/tree`, out]);
      need(r.code === 0, `sftp down: ${r.err}`);
      need(sameTree(await treeOf(path.join(FIX, 'data')), await treeOf(out)), 'sftp bytes differ');
      return 'round-trip byte-identical';
    });
  }

  if (haveDav) {
    await verify({ id: 'CLI-X-06', area: 'transfers', action: 'WebDAV round-trip', ds: 'WebDAV', scenario: 'upload tree → download tree → byte-identical diff', face: 'CLI' }, async () => {
      let r = await cli(['cp', '-r', path.join(FIX, 'data'), `xw://${RUNID}/tree`]);
      need(r.code === 0, `webdav up: ${r.err}`);
      const out = path.join(ART, 'from-dav');
      await rm(out, { recursive: true, force: true });
      r = await cli(['cp', '-r', `xw://${RUNID}/tree`, out]);
      need(r.code === 0, `webdav down: ${r.err}`);
      need(sameTree(await treeOf(path.join(FIX, 'data')), await treeOf(out)), 'webdav bytes differ');
      return 'round-trip byte-identical';
    });
  }

  if (haveSftp && haveFtp) {
    await verify({ id: 'CLI-X-07', area: 'transfers', action: 'Cross-engine move (mv)', ds: 'SFTP → FTP', scenario: 'mv -r sftp tree → ftp; source gone; destination byte-identical', face: 'CLI' }, async () => {
      let r = await cli(['mv', '-r', `xt://${RUNID}/tree`, `xf://${RUNID}/moved`]);
      need(r.code === 0, `mv: ${r.err}`);
      r = await cli(['ls', `xt://${RUNID}/`, '--json']);
      need(!r.out.includes('tree/'), 'source still listed after mv');
      const out = path.join(ART, 'moved');
      await rm(out, { recursive: true, force: true });
      await cli(['cp', '-r', `xf://${RUNID}/moved`, out]);
      need(sameTree(await treeOf(path.join(FIX, 'data')), await treeOf(out)), 'moved bytes differ');
      return 'moved + verified';
    });
  }

  await verify({ id: 'CLI-X-08', area: 'sources', action: 'Export + import sources', ds: 'all', scenario: 'encrypted export (--password); import into a FRESH config lists the same sources', face: 'CLI' }, async () => {
    const f = path.join(ART, 'sources.json');
    let r = await cli(['source', 'export', f, '--password', 'verify-export-pw']);
    need(r.code === 0, `export: ${r.err}`);
    const blob = await readFile(f, 'utf8');
    need(!blob.includes('minioadmin'), 'export must be encrypted, not plaintext');
    const altCfg = path.join(ART, 'alt-config');
    await mkdir(altCfg, { recursive: true });
    r = await cli(['source', 'import', f, '--password', 'verify-export-pw'], { cfg: altCfg });
    need(r.code === 0, `import: ${r.err}`);
    r = await cli(['source', 'list'], { cfg: altCfg });
    need(r.out.includes('verifys3'), 'imported store missing verifys3');
    return 'encrypted export/import round-trip';
  });

  await verify({ id: 'CLI-X-09', area: 'sources', action: 'Source lifecycle: profile test + remove', ds: 'S3 (MinIO)', scenario: 'add a temp source; profile test dials it (OK + bucket count); source remove drops it from BOTH source list and profile mirror', face: 'CLI' }, async () => {
    let r = await cli(['source', 'add', 'verifytmp', '--type', 's3', '--endpoint', ENDPOINT, '--access-key', KEY, '--secret-key', SECRET]);
    need(r.code === 0, `add: ${r.err}`);
    r = await cli(['profile', 'test', 'verifytmp']);
    need(r.code === 0 && /OK/.test(r.out), `profile test: ${r.out}${r.err}`);
    r = await cli(['source', 'remove', 'verifytmp']);
    need(r.code === 0 && /removed source/.test(r.out), `remove: ${r.out}${r.err}`);
    r = await cli(['source', 'list']);
    need(!r.out.includes('verifytmp'), 'removed source still in source list');
    r = await cli(['profile', 'list']);
    need(!r.out.includes('verifytmp'), 'removed source still in profile mirror');
    return 'tested, removed, gone from both lists';
  });

  await verify({ id: 'CLI-X-10', area: 'sources', action: 'Wrong-password import fails closed', ds: 'all', scenario: 'an encrypted export imported with the WRONG password: decrypt must fail BEFORE any source is upserted (no partial import, no half-populated store); the correct password still imports cleanly afterwards', face: 'CLI' }, async () => {
    const f = path.join(ART, 'sources-x10.json');
    let r = await cli(['source', 'export', f, '--password', 'x10-correct-horse-battery']);
    need(r.code === 0, `export: ${r.err}`);
    const bad = path.join(ART, 'x10-config');
    await rm(bad, { recursive: true, force: true });
    await mkdir(bad, { recursive: true });
    r = await cli(['source', 'import', f, '--password', 'x10-DEFINITELY-wrong'], { cfg: bad, timeout: 60000 });
    need(r.code !== 0, 'import with a wrong password exited 0');
    need(/decrypt|password|cipher|auth|gcm/i.test(r.out + r.err), `failure not labeled as a crypto fault: ${r.out}${r.err}`);
    // nothing may have landed — no partial import, ever
    r = await cli(['source', 'list'], { cfg: bad });
    need(!r.out.includes('verifys3'), 'wrong password still imported sources (partial import)');
    need(!r.out.includes('xf'), 'wrong password still imported the ftp source (partial import)');
    // the same container with the right password imports cleanly afterwards
    r = await cli(['source', 'import', f, '--password', 'x10-correct-horse-battery'], { cfg: bad, timeout: 60000 });
    need(r.code === 0, `import retry: ${r.out}${r.err}`);
    r = await cli(['source', 'list'], { cfg: bad });
    need(r.out.includes('verifys3'), 'correct-password import did not land verifys3');
    return 'rejected before any upsert; clean import after';
  });

  await verify({ id: 'CLI-X-11', area: 'transfers', action: 'Hostile filenames cross-engine', ds: 'SFTP/FTP/WebDAV/S3', scenario: 'names that stress protocol and shell escaping — spaces, # & % + ^ $ !, apostrophes, brackets, semicolons, CJK, a 120-char name — must round-trip through EVERY live engine and S3 with names and bytes intact', face: 'CLI' }, async () => {
    const hostile = [
      'spaced name #1 & amp plus + percent %25.txt',
      "single'quote caret ^grave $dollar !bang.txt",
      'brackets [a] (b) {c} semicolon ;.txt',
      'uni-日本語-CJK name.txt',
      `long-${'x'.repeat(110)}-name.txt`,
      'dots...before ext.txt',
    ];
    const hdir = path.join(ART, 'hostile');
    await rm(hdir, { recursive: true, force: true });
    await mkdir(hdir, { recursive: true });
    for (const [i, n] of hostile.entries()) await writeFile(path.join(hdir, n), `hostile payload ${i} ${RUNID}\n`);
    const want = await treeOf(hdir);
    const legs = [];
    const leg = async (label, destUri) => {
      let r = await cli(['cp', '-r', hdir, destUri]);
      need(r.code === 0, `${label} up: ${r.out}${r.err}`);
      r = await cli(['ls', destUri, '--recursive', '--json']);
      // the CLI's JSON printer HTML-escapes & < > (Go encoding/json) — build
      // the needle with exactly the escaping the listing actually contains
      const jsonNeedle = (n) => JSON.stringify(n).slice(1, -1)
        .replace(/&/g, '\\u0026').replace(/</g, '\\u003c').replace(/>/g, '\\u003e');
      for (const n of hostile) {
        need(r.out.includes(jsonNeedle(n)), `${label} listing lost "${n}"`);
      }
      const back = path.join(ART, `hostile-back-${label}`);
      await rm(back, { recursive: true, force: true });
      r = await cli(['cp', '-r', destUri, back]);
      need(r.code === 0, `${label} down: ${r.out}${r.err}`);
      need(sameTree(want, await treeOf(back)), `${label} round-trip altered names or bytes`);
      legs.push(label);
    };
    await leg('s3', `s3://${BUCKET}/hostile-names/`);
    if (haveSftp) await leg('sftp', `xt://${RUNID}/hostile`);
    if (haveFtp) await leg('ftp', `xf://${RUNID}/hostile`);
    if (haveDav) await leg('dav', `xw://${RUNID}/hostile`);
    return `${hostile.length} hostile names intact through ${legs.join('+')}`;
  });

  await verify({ id: 'CLI-X-12', area: 'sources', action: 'Re-import + name collision: the store never forks', ds: 'all', scenario: 'importing the SAME encrypted export twice into one config must not duplicate anything (source-ID collision) and the imported entry must dial the LIVE endpoint; importing over a pre-existing source that already owns the incoming name must never fork the store — exactly one row per name, nothing else lost, everything still removable', face: 'CLI' }, async () => {
    const f = path.join(ART, 'sources-x12.json');
    let r = await cli(['source', 'export', f, '--password', 'x12-collision-pw']);
    need(r.code === 0, `export: ${r.err}`);
    const dir = path.join(ART, 'x12-config');
    await rm(dir, { recursive: true, force: true });
    await mkdir(dir, { recursive: true });
    // every isolated-dir call runs keyring-less: profile keyring slots are
    // keyed by NAME ONLY (pkg/core/profile/keyring.go), so a keyring-backed
    // add/remove/import of "verifys3" here would clobber/delete the live
    // faces' global slot — the run-#1 mass-403 lesson
    // leg 1 — the same file twice: no duplicated source may ever appear,
    // and the entry the imports produced must be the LIVE one
    r = await cli(['source', 'import', f, '--password', 'x12-collision-pw'], { cfg: dir, timeout: 60000, nokeyring: true });
    need(r.code === 0, `import #1: ${r.out}${r.err}`);
    r = await cli(['source', 'import', f, '--password', 'x12-collision-pw'], { cfg: dir, timeout: 60000, nokeyring: true });
    need(r.code === 0, `import #2 (same file): ${r.out}${r.err}`);
    let l = await cli(['source', 'list'], { cfg: dir, nokeyring: true });
    need(l.code === 0 && countLines(l.out, 'verifys3') === 1, `double import left ${countLines(l.out, 'verifys3')} verifys3 row(s)`);
    const t1 = await cli(['source', 'test', 'verifys3'], { cfg: dir, timeout: 60000, nokeyring: true });
    need(t1.code === 0, `the imported verifys3 does not dial the live endpoint: ${t1.out}${t1.err}`);
    // leg 2 — a pre-existing owner of the incoming name (an update-in-place
    // add moves it to a dead endpoint), then import on top: the store must
    // not fork, must not lose anything else, and must stay removable
    const before = (await cli(['source', 'list'], { cfg: dir, nokeyring: true })).out.split('\n').filter((x) => x.trim());
    r = await cli(['source', 'add', 'verifys3', '--type', 's3', '--endpoint', 'http://127.0.0.1:1', '--access-key', 'x12', '--secret-key', 'x12'], { cfg: dir, nokeyring: true });
    need(r.code === 0, `name-priming add: ${r.out}${r.err}`);
    r = await cli(['source', 'import', f, '--password', 'x12-collision-pw'], { cfg: dir, timeout: 60000, nokeyring: true });
    need(r.code === 0, `import over a name collision: ${r.out}${r.err}`);
    l = await cli(['source', 'list'], { cfg: dir, nokeyring: true });
    need(l.code === 0, `list after colliding import: ${l.err}`);
    need(countLines(l.out, 'verifys3') === 1, `the collision import forked the name (${countLines(l.out, 'verifys3')} rows)`);
    // "nothing lost" means by NAME: the collision add deliberately moved
    // verifys3's endpoint (update-in-place), so its LIST LINE legitimately
    // changes — only a disappearing NAME is a loss
    const namesOf = (lines) => lines.map((x) => x.trim().split(/\s+/)[0]).filter(Boolean);
    const afterNames = new Set(namesOf(l.out.split('\n').filter((x) => x.trim())));
    for (const n of namesOf(before)) {
      need(afterNames.has(n), `the collision import LOST the source "${n}"`);
    }
    for (const n of ['verifys3', 'verifys3-2', 'xf', 'xt', 'xw', 'verifyfault']) {
      await cli(['source', 'remove', n], { cfg: dir, nokeyring: true });
    }
    const empty = await cli(['source', 'list'], { cfg: dir, nokeyring: true });
    need(!/verifys3|verifyfault|127\.0\.0\.1:1/.test(empty.out), `cleanup left residue: ${empty.out}`);
    return 'double import idempotent + live; collision kept one row per name, nothing lost';
  });
}

// ============================================================
// CLI battery — resilience (faultproxy) + invocation contract
// ============================================================
async function cliResilience() {
  const FPORT = 19010, FCTL = 19011;
  let proxy = null;
  const stop = () => { if (proxy && proxy.exitCode === null) proxy.kill(); };
  const fmode = async (body) => {
    await fetch(`http://127.0.0.1:${FCTL}/mode`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) });
  };
  await verify({ id: 'CLI-RES-01', area: 'resilience', action: 'Slow link: latency +600ms/chunk', ds: 'S3 via faultproxy', scenario: 'listing through a delayed proxy completes with correct output, measurably slower', face: 'CLI' }, async () => {
    proxy = spawn('node', [path.join(ROOT, 'scripts', 'faultproxy.mjs'), '--listen', String(FPORT), '--control', String(FCTL), '--target', '127.0.0.1:9000'], { stdio: 'ignore', windowsHide: true });
    for (let i = 0; i < 50; i++) { try { await fetch(`http://127.0.0.1:${FCTL}/state`, { signal: AbortSignal.timeout(500) }); break; } catch { await sleep(100); } }
    await cli(['source', 'add', 'verifyfault', '--type', 's3', '--endpoint', `http://127.0.0.1:${FPORT}`, '--access-key', KEY, '--secret-key', SECRET]);
    await fmode({ mode: 'latency', delayMs: 600 });
    const t0 = Date.now();
    const r = await cli(['ls', `s3://${BUCKET}`, '--recursive', '--json', '--profile', 'verifyfault']);
    const dt = Date.now() - t0;
    need(r.code === 0 && r.out.includes('readme'), `latency ls: ${r.out}${r.err}`);
    need(dt >= 1000, `finished in ${dt}ms — delay not applied`);
    return `correct listing in ${(dt / 1000).toFixed(1)}s under latency`;
  });

  await verify({ id: 'CLI-RES-02', area: 'resilience', action: 'Dead link: RST mid-session', ds: 'S3 via faultproxy', scenario: 'connection reset → non-zero exit, error classified, no partial success', face: 'CLI' }, async () => {
    if (!proxy) return skip('proxy not running');
    await fmode({ mode: 'reset' });
    const r = await cli(['ls', `s3://${BUCKET}`, '--profile', 'verifyfault']);
    need(r.code !== 0, 'ls survived a reset connection');
    need((r.out + r.err).length > 0, 'no error message');
    return 'clean classified failure';
  });

  await verify({ id: 'CLI-RES-03', area: 'resilience', action: 'Blackhole: endpoint never answers', ds: 'S3 via faultproxy', scenario: 'watchdog timeout within the --timeout budget (no default 5-minute hang)', face: 'CLI' }, async () => {
    if (!proxy) return skip('proxy not running');
    await fmode({ mode: 'blackhole' });
    const t0 = Date.now();
    const r = await cli(['ls', `s3://${BUCKET}`, '--profile', 'verifyfault', '--timeout', '8s'], { timeout: 40000 });
    const dt = Date.now() - t0;
    need(r.code !== 0, 'ls survived a blackhole');
    need(dt < 30000, `took ${dt}ms — budget blown`);
    return `failed cleanly in ${(dt / 1000).toFixed(1)}s`;
  });

  await verify({ id: 'CLI-RES-04', area: 'resilience', action: 'Hard kill mid-upload: no partial object', ds: 'S3 via faultproxy', scenario: 'a 64 MiB upload killed mid-flight (process termination) must land NO object — no corrupt or half-written key ever becomes visible; the clean retry is byte-identical', face: 'CLI' }, async () => {
    if (!proxy) return skip('proxy not running');
    await fmode({ mode: 'latency', delayMs: 600 });
    const src = path.join(ART, 'crash-64m.bin');
    await writeFile(src, randomBytes(64 * 1024 * 1024));
    const sha = await sha256file(src);
    const child = spawn(CLI_EXE, ['cp', src, `s3://${BUCKET}/verify-big/crashed.bin`, '--profile', 'verifyfault'], {
      cwd: ROOT, windowsHide: true, stdio: 'ignore',
      env: { ...process.env, S3B_CONFIG: CFG, NO_COLOR: '1', TERM: 'dumb' },
    });
    await sleep(3000); // 64 MiB through a 600ms/chunk link is nowhere near done
    need(child.exitCode === null, 'cp finished before the kill — not a mid-flight probe');
    child.kill(); // Windows: process termination — the hard-kill contract
    await new Promise((res) => { child.once('exit', res); if (child.exitCode !== null) res(); });
    // the crashed object must NOT have landed (no Complete → no visible object,
    // and a healthy-link stat must not see a partial). --profile verifys3:
    // the fault source makes the default profile ambiguous.
    const st = await cli(['stat', `s3://${BUCKET}/verify-big/crashed.bin`, '--profile', 'verifys3'], { timeout: 30000 });
    need(!/multiple profiles/.test(st.out + st.err), `stat probe ambiguous: ${st.out}${st.err}`);
    need(st.code !== 0, 'a partial/corrupt object became visible after the hard kill');
    // retry over the healthy link lands byte-identical bytes
    const r = await cli(['cp', src, `s3://${BUCKET}/verify-big/crashed.bin`, '--profile', 'verifys3'], { timeout: 300000 });
    need(r.code === 0, `retry cp: ${r.out}${r.err}`);
    const dl = path.join(ART, 'crash-64m.dl.bin');
    await cli(['cp', `s3://${BUCKET}/verify-big/crashed.bin`, dl, '--profile', 'verifys3'], { timeout: 300000 });
    need(await sha256file(dl) === sha, 'post-crash retry bytes differ from the source');
    return 'kill left no visible object; retry sha-identical';
  });

  await verify({ id: 'CLI-RES-05', area: 'resilience', action: 'Transient 5xx storm: absorbed by retries / exhausted honestly', ds: 'S3 via faultproxy', scenario: 'a canned-503 flap: a 2-failure storm must be retried away INSIDE the SDK budget (the put lands byte-identical); a 50-failure storm must exhaust it — non-zero exit, a surfaced error, and NO object half-landed', face: 'CLI' }, async () => {
    if (!proxy) return skip('proxy not running');
    const src = path.join(ART, 'flap.txt');
    await writeFile(src, `flap payload ${RUNID}\n`);
    const sha = await sha256file(src);
    const flapped = async () => (await (await fetch(`http://127.0.0.1:${FCTL}/state`)).json()).flapped;
    // part 1 — absorb: two canned 503s die inside the retry budget
    await fmode({ mode: 'flap', failFirst: 2 });
    let r = await cli(['cp', src, `s3://${BUCKET}/verify-res/flap-ok.txt`, '--profile', 'verifyfault'], { timeout: 180000 });
    need(r.code === 0, `cp under a 2×503 storm: ${r.out}${r.err}`);
    const storm1 = await flapped();
    need(storm1 >= 2, `storm never fired (flapped=${storm1}) — vacuous pass`);
    const got = path.join(ART, 'flap-ok.dl.txt');
    const dl = await cli(['cp', `s3://${BUCKET}/verify-res/flap-ok.txt`, got, '--profile', 'verifys3']);
    need(dl.code === 0, `download of the storm-surviving object: ${dl.out}${dl.err}`);
    need(await sha256file(got) === sha, 'storm-surviving upload bytes differ');
    // part 2 — exhaustion: 503s outlive any retry budget
    await fmode({ mode: 'flap', failFirst: 50 });
    const dead = `s3://${BUCKET}/verify-res/flap-dead.txt`;
    r = await cli(['cp', src, dead, '--profile', 'verifyfault'], { timeout: 180000 });
    need(r.code !== 0, 'cp survived an unbounded 503 storm');
    need((r.out + r.err).trim().length > 0, 'storm exhaustion produced no error output');
    const storm2 = await flapped();
    need(storm2 >= 3, `exhaustion storm never fired (flapped=${storm2})`);
    const probe = await cli(['stat', dead, '--profile', 'verifys3']);
    need(probe.code !== 0, 'an object landed despite a fully-failed upload');
    await fmode({ mode: 'direct' });
    return `absorbed ${storm1}×503 (bytes intact), then failed honestly after ${storm2}`;
  });

  await verify({ id: 'CLI-RES-06', area: 'resilience', action: 'Blackholed transfer: --timeout bounds the hang, no partial lands', ds: 'S3 via faultproxy', scenario: 'a blackholed endpoint would hang a transfer for the default 5 minutes: --timeout 8s must cut it to a fast, clean non-zero failure (well inside the budget), and the half-sent object must NOT exist — a timeout may cost the attempt, never the store’s integrity', face: 'CLI' }, async () => {
    if (!proxy) return skip('proxy not running');
    await fmode({ mode: 'blackhole' });
    const src = path.join(ART, 'bh6.txt');
    await writeFile(src, randomBytes(1024 * 1024));
    const dst = `s3://${BUCKET}/verify-res/blackholed.txt`;
    const t0 = Date.now();
    const r = await cli(['cp', src, dst, '--profile', 'verifyfault', '--timeout', '8s'], { timeout: 60000 });
    const dt = Date.now() - t0;
    need(r.code !== 0, 'cp survived a blackholed endpoint');
    need(dt < 30000, `the 8s budget took ${dt}ms to enforce`);
    need((r.out + r.err).trim().length > 0, 'the timeout failure printed nothing');
    const probe = await cli(['stat', dst, '--profile', 'verifys3'], { timeout: 30000 });
    need(probe.code !== 0, 'a partial object survived the timeout');
    await fmode({ mode: 'direct' });
    // the store is still writable through the healthy path afterwards
    const retry = await cli(['cp', src, dst, '--profile', 'verifys3']);
    need(retry.code === 0, `healthy retry after blackhole: ${retry.out}${retry.err}`);
    return `failed in ${dt}ms under an 8s budget; no partial; healthy retry landed`;
  });
  stop();
}

async function cliMeta() {
  await verify({ id: 'CLI-M-01', area: 'meta', action: 'Version identity', ds: '—', scenario: 'version prints the build version, exit 0', face: 'CLI' }, async () => {
    const r = await cli(['version']);
    need(r.code === 0 && r.out.trim() === VERSION, `version: ${r.out}`);
    return VERSION;
  });
  await verify({ id: 'CLI-M-02', area: 'meta', action: 'Usage-error contract', ds: '—', scenario: 'unknown command → exit 2, stderr reads "usage error:" and points at --help', face: 'CLI' }, async () => {
    const r = await cli(['definitely-not-a-cmd']);
    need(r.code === 2, `exit ${r.code}, want 2`);
    need(r.err.includes('usage error:') && r.err.includes('--help'), `stderr: ${r.err}`);
    return 'exit 2 + labeled';
  });
  await verify({ id: 'CLI-M-03', area: 'meta', action: 'Shell completion', ds: '—', scenario: 'completion bash emits a working completion script; other shells answer too', face: 'CLI' }, async () => {
    let r = await cli(['completion', 'bash']);
    need(r.code === 0 && r.out.includes('_s3b') && r.out.includes('s3b'), `bash: exit ${r.code}`);
    const emitted = ['bash'];
    for (const sh of ['zsh', 'fish', 'powershell']) {
      r = await cli(['completion', sh]);
      need(r.code !== 0 || r.out.includes('s3b'), `${sh}: exit ${r.code} garbage`);
      if (r.code === 0) emitted.push(sh);
    }
    return `completion scripts emitted: ${emitted.join(', ')}`;
  });

  await verify({ id: 'CLI-M-04', area: 'security', action: 'Secrets never printed', ds: 'S3 (MinIO)', scenario: 'a source with a distinctive secret key: every output surface — source list (text + json), source test, the 403 transfer path, profile list, activity log — must run but never echo the secret, even on failure paths', face: 'CLI' }, async () => {
    const TOKEN = `zz9-secret-${RUNID}-never-print`;
    let r = await cli(['source', 'add', 'verifyleak', '--type', 's3', '--endpoint', ENDPOINT, '--access-key', KEY, '--secret-key', TOKEN]);
    need(r.code === 0, `source add: ${r.err}`);
    const surfaces = [];
    const grab = (label, res) => { surfaces.push([label, (res.out + res.err)]); return res; };
    r = grab('source list', await cli(['source', 'list']));
    r = grab('source list --json', await cli(['source', 'list', '--json']));
    r = grab('source test (auth-fail path)', await cli(['source', 'test', 'verifyleak'], { timeout: 90000 }));
    r = grab('cp 403 path', await cli(['cp', `s3://${BUCKET}/readme.md`, path.join(ART, 'leak-dl.txt'), '--profile', 'verifyleak'], { timeout: 90000 }));
    r = grab('profile list', await cli(['profile', 'list']));
    r = grab('activity log', await cli(['log']));
    let nonEmpty = 0;
    for (const [label, text] of surfaces) {
      if (text.trim().length > 0) nonEmpty++;
      need(!text.includes(TOKEN), `SECRET LEAKED in ${label}`);
    }
    need(nonEmpty >= 4, `masking probe too weak: only ${nonEmpty}/${surfaces.length} surfaces produced output`);
    await cli(['source', 'remove', 'verifyleak']);
    return `secret absent from all ${surfaces.length} surfaces (${nonEmpty} produced output)`;
  });

  await verify({ id: 'CLI-M-05', area: 'security', action: 'Secrets encrypted at rest', ds: 'config store + OS keyring', scenario: 'a distinctive secret added to the store, then a RAW byte-scan of every file under the whole config directory (recursive): the secret may live only inside the OS keyring — never on disk in any file the app wrote this run', face: 'CLI' }, async () => {
    if (process.env.S3B_NO_KEYRING) return skip('S3B_NO_KEYRING set — documented headless plaintext mode, at-rest scan N/A');
    const TOKEN = `zz9-atrest-${RUNID}-secret`;
    let r = await cli(['source', 'add', 'verifyrest', '--type', 's3', '--endpoint', ENDPOINT, '--access-key', KEY, '--secret-key', TOKEN]);
    need(r.code === 0, `add: ${r.err}`);
    await cli(['source', 'list']); // settle the store
    const needle = Buffer.from(TOKEN, 'utf8');
    const files = [];
    const walk = async (d) => {
      for (const e of await readdir(d, { withFileTypes: true })) {
        const p = path.join(d, e.name);
        if (e.isDirectory()) await walk(p);
        else files.push(p);
      }
    };
    await walk(CFG);
    need(files.length > 0, 'config dir empty — the at-rest scan had nothing to scan');
    const hits = [];
    for (const f of files) {
      if ((await readFile(f)).includes(needle)) hits.push(path.relative(CFG, f));
    }
    await cli(['source', 'remove', 'verifyrest']);
    need(hits.length === 0, `SECRET WRITTEN IN PLAINTEXT: ${hits.join(', ')}`);
    return `token absent from all ${files.length} file(s) under the config dir (keyring holds it)`;
  });

  await verify({ id: 'CLI-M-06', area: 'sources', action: 'Legacy store migration: profiles seed as sources, plaintext secrets leave the file', ds: 'S3 (MinIO)', scenario: 'a hand-written LEGACY profiles.json (two profiles, inline secrets, one carrying a session token, no sources array): ONE CLI load seeds both as data sources (M8), the keyring takes every secret and the token (M5 — none remain in the file); the token-less migrated source still dials MinIO through the keyring-held secret, while the token profile must fail with an invalid-token refusal — the synthetic token being IN the signature is itself proof it was read back from the keyring; both profiles are then removed so no keyring residue survives the run', face: 'CLI' }, async () => {
    if (process.env.S3B_NO_KEYRING) return skip('headless plaintext mode: S3B_NO_KEYRING');
    const dir = path.join(ART, 'm06-config');
    await rm(dir, { recursive: true, force: true });
    await mkdir(dir, { recursive: true });
    // names unique per run: the OS keyring is machine-global and keyed by
    // profile name only, so a fixed name would resurrect a stale token from
    // an earlier run (observed: run 1 of this row poisoned every later dial)
    const N1 = `legacy-${RUNID}`;
    const N2 = `legacytok-${RUNID}`;
    const TOKEN = 'm06-zz9-legacy-session-token';
    const now = '2026-01-01T00:00:00Z';
    const prof = (name, token) => ({
      name, endpoint: 'http://127.0.0.1:9000', region: 'us-east-1',
      accessKeyId: KEY, secretKey: SECRET, sessionToken: token, pathStyle: true,
      createdAt: now, updatedAt: now,
    });
    await writeFile(path.join(dir, 'profiles.json'), JSON.stringify({
      profiles: [prof(N1, ''), prof(N2, TOKEN)],
    }, null, 2));
    let r = await cli(['source', 'list'], { cfg: dir });
    need(r.code === 0 && r.out.includes(N1) && r.out.includes(N2), `M8 seed did not surface the profiles: ${r.out}${r.err}`);
    r = await cli(['profile', 'list'], { cfg: dir });
    need(r.out.includes(N1) && r.out.includes(N2), `legacy profiles no longer resolve: ${r.out}`);
    const raw = (await readFile(path.join(dir, 'profiles.json'))).toString();
    need(!raw.includes(TOKEN), 'the plaintext session token survived in profiles.json (M5 did not migrate it)');
    need(!raw.includes('"secretKey": "minioadmin"') && !raw.includes('"secretKey":"minioadmin"'), 'the plaintext secret survived in profiles.json');
    // each seeded profile is marked in BOTH the profiles array and its
    // mirrored sources[].s3 entry — require at least one marker per profile
    need((raw.match(/"secretInKeyring":\s*true/g) || []).length >= 2, `keyring markers missing from the migrated store: ${raw.slice(0, 160)}`);
    // the token-less profile must dial: the secret came back from the keyring
    r = await cli(['source', 'test', N1], { cfg: dir });
    need(r.code === 0, `the migrated source does not dial: ${r.out}${r.err}`);
    // the token profile must dial-and-be-refused: MinIO answers InvalidTokenId
    // for a synthetic STS token, which proves the keyring-held token reached
    // the SigV4 signature (a dropped token would have dialed fine)
    r = await cli(['source', 'test', N2], { cfg: dir });
    need(r.code !== 0 && /invalidtoken/i.test(r.out + r.err), `the synthetic token was not used in the signature: ${r.out}${r.err}`);
    // leave the machine-global keyring exactly as we found it
    await cli(['source', 'remove', N1], { cfg: dir });
    await cli(['source', 'remove', N2], { cfg: dir });
    return `${N1} dials through the keyring-held secret; ${N2}'s token reached the signature (refused as synthetic); no keyring residue`;
  });

  await verify({ id: 'CLI-M-07', area: 'meta', action: '--help contract: every command self-documents', ds: '—', scenario: 'every top-level command (22) plus the root: --help exits 0, prints a Usage: block and lists its subcommands where it has any — a missing or crashing help page is a broken contract for scripting humans', face: 'CLI' }, async () => {
    const CMDS = ['source', 'profile', 'ls', 'tree', 'du', 'stat', 'mb', 'rb', 'mkdir', 'cp', 'mv', 'rm', 'sync', 'presign', 'doctor', 'versions', 'bucket', 'find', 'sc', 'lock', 'log', 'version'];
    for (const c of CMDS) {
      const r = await cli([c, '--help']);
      need(r.code === 0, `${c} --help exited ${r.code}: ${(r.out + r.err).slice(0, 120)}`);
      need(/usage:/i.test(r.out + r.err), `${c} --help printed no Usage: block`);
    }
    const root = await cli(['--help']);
    need(root.code === 0, `root --help exited ${root.code}`);
    need(/usage:/i.test(root.out + root.err), 'root --help printed no Usage: block');
    for (const c of CMDS) {
      need(new RegExp(`\\b${c}\\b`).test(root.out), `root --help does not list "${c}"`);
    }
    return `${CMDS.length} commands + root all document themselves`;
  });
}

// ============================================================
// GUI battery — the Wails v3 server stack in a real browser
// ============================================================
let page = null, context = null, srv = null;
let pageErrors = [];
const SRVLOG = path.join(ART, 'server.log');
const USERDIR = path.join(ART, 'browser-profile');

const evalPage = (fn, ...args) => page.evaluate(fn, ...args);
const elOrNull = (js, arg) => page.evaluateHandle(js, arg).then(async (h) => ((await h.asElement()) ? h : null));
const rowKeys = () => evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
  .map((r) => r.dataset.key || r.querySelector('.tname')?.textContent || ''));
const txt = (sel) => evalPage((s) => document.querySelector(s)?.textContent || '', sel);
async function waitFor(fn, ms = 15000, what = 'condition') {
  const t0 = Date.now();
  for (;;) {
    let v; try { v = await fn(); } catch { /* retry */ }
    if (v) return v;
    if (Date.now() - t0 > ms) throw new Error(`timeout waiting for ${what}`);
    await sleep(100);
  }
}
async function rowAction(label, kind = 'grid') {
  const h = await elOrNull((src) => {
    const [l, k] = src;
    const root = document.getElementById(k === 'grid' ? 'grid-body' : 'local-grid-body');
    if (!root) return null;
    return Array.from(root.querySelectorAll('.grid-row'))
      .find((r) => ((r.querySelector('.tname')?.textContent || '').trim() === l
        || (r.dataset.key || '') === l)) || null;
  }, [label, kind]);
  if (!h) throw new Error(`row "${label}" not found`);
  return h.asElement();
}
const clickRow = (l) => rowAction(l).then((e) => e.click());
const ctrlClickRow = (l) => rowAction(l).then((e) => e.click({ modifiers: ['Control'] }));
const dblClickRow = (l) => rowAction(l).then((e) => e.dblclick());
async function clickFooter(re) {
  const h = await elOrNull((src) => {
    const btns = Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'));
    return btns.find((b) => new RegExp(src, 'i').test(b.textContent.trim())) || null;
  }, re.source);
  if (!h) throw new Error(`no footer button /${re.source}/`);
  await h.asElement().click();
  await sleep(120);
}
async function modalText() { return evalPage(() => document.querySelector('#modal-root .modal')?.textContent || ''); }
async function answerPrompt(value) {
  const input = page.locator('#modal-root .modal input.input').last();
  await waitFor(() => input.count().then((n) => n > 0), 5000, 'prompt input');
  await input.fill(value);
  await input.press('Enter');
  await sleep(150);
}
async function treeOpen(label) {
  await waitFor(() => evalPage((l) => Array.from(document.querySelectorAll('#tree .tnode'))
    .some((n) => (n.querySelector('.tlabel')?.textContent || '').trim() === l), label), 10000, `tree node "${label}"`);
  await evalPage((l) => {
    Array.from(document.querySelectorAll('#tree .tnode'))
      .find((n) => (n.querySelector('.tlabel')?.textContent || '').trim() === l)
      ?.querySelector('.tlabel')?.click();
  }, label);
  await sleep(300);
}
async function refresh() { await page.locator('#btn-refresh').click(); await sleep(400); }
// Enter a folder row and PROVE it via the breadcrumb — a grid re-render
// between lookup and dblclick can land the click on a detached element,
// so retry until the breadcrumb shows we are inside.
async function enterFolder(label) {
  await waitFor(async () => {
    if ((await txt('#breadcrumb')).includes(label)) return true;
    // awaited + short timeout: a floating popout over the grid makes the
    // dblclick time out, and an UNAWAITED rejection here once killed the
    // whole runner (run #2, GUI-36) — never leave a pending Playwright
    // action floating outside the row's error boundary
    try { await (await rowAction(label)).dblclick({ timeout: 4000 }); } catch { /* row mid-render or covered */ }
    await sleep(300);
    return false;
  }, 12000, `enter ${label}`);
  await sleep(200);
}
async function shot(name) { try { await page.screenshot({ path: path.join(SHOTS, `${name}.png`) }); } catch { /* non-fatal */ } }

// ---- helpers ported from gui-v3live.mjs for the second GUI battery ----
async function modalVisible() {
  return evalPage(() => !!document.querySelector('#modal-root .modal') && !document.getElementById('modal-root').classList.contains('hidden'));
}
async function closeModal() {
  const btn = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
    .reverse().find((b) => /^(close|cancel)$/i.test(b.textContent.trim())) || null);
  if (btn) { await btn.asElement().click(); await sleep(100); return; }
  await page.keyboard.press('Escape');
  await sleep(100);
  await evalPage(() => document.getElementById('modal-root')?.classList.add('hidden'));
}
const rightClickRow = (l) => rowAction(l).then((e) => e.click({ button: 'right' }));
async function ctxItem(re) {
  const h = await elOrNull((src) => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
    .find((i) => new RegExp(src, 'i').test(i.textContent)) || null, re.source);
  if (!h) throw new Error(`no ctxmenu item /${re.source}/`);
  await h.asElement().click();
  await sleep(100);
}
async function menuClick(menuRe, itemRe) {
  await page.locator('#menubar .mb-title', { hasText: menuRe }).first().click();
  await sleep(100);
  const h = await elOrNull((src) => {
    const [m, i] = src;
    const dd = Array.from(document.querySelectorAll('#menubar .mb-dd')).find((d) => !d.classList.contains('hidden'));
    if (!dd) return null;
    return Array.from(dd.querySelectorAll('.mb-item')).find((it) => new RegExp(i, 'i').test(it.textContent)) || null;
  }, [menuRe.source, itemRe.source]);
  if (!h) { await page.keyboard.press('Escape'); throw new Error(`no menu item ${menuRe}/${itemRe}`); }
  await h.asElement().click();
  await sleep(120);
}
async function dnd(fromH, toH, { shift = false, ctrl = false } = {}) {
  if (!fromH || !toH) throw new Error(`dnd: ${!fromH ? 'source' : 'target'} element not found`);
  await page.evaluate(([f, t, sh, ct]) => {
    const dt = new DataTransfer();
    f.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }));
    const init = { bubbles: true, cancelable: true, dataTransfer: dt, shiftKey: sh, ctrlKey: ct };
    t.dispatchEvent(new DragEvent('dragover', init));
    t.dispatchEvent(new DragEvent('drop', init));
    f.dispatchEvent(new DragEvent('dragend', { bubbles: true, cancelable: true, dataTransfer: dt }));
  }, [fromH.asElement(), toH.asElement(), shift, ctrl]);
  await sleep(150);
}
async function startIfAsked(timeoutMs = 8000) {
  try {
    await waitFor(async () => {
      if (!(await modalVisible())) return false;
      const h = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
        .find((b) => /^start$/i.test(b.textContent.trim())) || null);
      return h && await h.asElement() ? true : false;
    }, timeoutMs, 'transfer dialog');
    await clickFooter(/^start$/i);
    return 'dialog';
  } catch { return 'silent'; }
}
async function filterTo(text) {
  await page.locator('#filter').fill(text);
  await sleep(250); // the app debounces the filter input 120ms
}
async function clearFilter() { await filterTo(''); }
const sideKeys = () => evalPage(() => Array.from(document.querySelectorAll('#local-grid-body .grid-row'))
  .filter((r) => r.style.display !== 'none' && r._model).map((r) => String(r._model.key).replace(/[\\/]+$/, '').split(/[\\/]/).pop()));
const sideRow = (label) => elOrNull((l) => {
  const norm = (s) => String(s || '').replace(/\/+$/, '');
  return Array.from(document.querySelectorAll('#local-grid-body .grid-row'))
    .find((r) => norm(r.querySelector('.tname')?.textContent) === norm(l)) || null;
}, label);
const bodyH = () => page.evaluateHandle(() => document.getElementById('grid-body'));
const sideBodyH = () => page.evaluateHandle(() => document.getElementById('local-grid-body'));
// binding calls straight through the bridge the app itself uses
const call = (method, ...args) => evalPage(([m, a]) => window.go['github.com/MikkoP88/s3-bucket-browser/pkg/api'].App[m](...a), [method, args]);

function startServer() {
  return new Promise((resolve, reject) => {
    const proc = spawn(SRV_EXE, [], {
      cwd: ROOT,
      windowsHide: true,
      env: { ...process.env, WAILS_SERVER_HOST: '127.0.0.1', WAILS_SERVER_PORT: String(GUI_PORT), S3B_CONFIG: GUICFG },
    });
    proc.stdout.on('data', (d) => fs.appendFileSync(SRVLOG, d));
    proc.stderr.on('data', (d) => fs.appendFileSync(SRVLOG, d));
    const t0 = Date.now();
    (function poll() {
      fetch(`http://127.0.0.1:${GUI_PORT}/health`, { signal: AbortSignal.timeout(1500) })
        .then((r) => r.json())
        .then((j) => { if (j.status === 'ok') resolve({ proc }); else retry(); })
        .catch(retry);
      function retry() {
        if (proc.exitCode !== null) return reject(new Error(`server exited early (code ${proc.exitCode}) — see ${SRVLOG}`));
        if (Date.now() - t0 > 30000) return reject(new Error('server did not answer /health in 30s'));
        setTimeout(poll, 400);
      }
    })();
  });
}

async function stopServer() {
  if (!srv || srv.proc.exitCode !== null) return;
  const exited = new Promise((r) => srv.proc.on('exit', r));
  srv.proc.kill();
  await Promise.race([exited, sleep(3000)]);
  if (srv.proc.exitCode === null) srv.proc.kill();
}

async function guiBattery() {
  const { chromium } = await import('playwright-core');
  // ---- self-sufficiency prologue: --only gui must run green alone ----
  // Both S3B_CONFIG stores are wiped fresh every run, so leftovers from an
  // earlier invocation can never help: a FULL run builds the GUI battery's
  // prerequisites in this same process — cliS3 adds the verifys3 profile,
  // creates the versioned run bucket with its data/ + docs/ roots, and
  // cliCross adds the xf FTP source this battery seeds through. Recreate
  // anything missing here so the documented standalone unit (--only gui)
  // is real; in a full run every branch resolves to a no-op and the rows
  // below see exactly the state they saw before.
  const s3p = (args) => cli(['--profile', 'verifys3', ...args]);
  {
    const ftpUp = await portOpen(FTP_PORT);
    const srcs = await cli(['source', 'list']).catch(() => ({ out: '' }));
    if (!srcs.out.includes('verifys3')) {
      const r = await cli(['source', 'add', 'verifys3', '--type', 's3', '--endpoint', ENDPOINT, '--access-key', KEY, '--secret-key', SECRET]);
      need(r.code === 0, `prologue: verifys3 source add: ${r.err}`);
    }
    if (ftpUp && !srcs.out.includes('xf')) {
      const r = await cli(['source', 'add', 'xf', '--type', 'ftp', '--host', '127.0.0.1', '--port', String(FTP_PORT), '--username', E2E_USER, '--password', E2E_PASS]);
      need(r.code === 0, `prologue: xf source add: ${r.err}`);
    }
    const rootLs = await s3p(['ls', `s3://${BUCKET}`]).catch(() => ({ code: 1, out: '' }));
    if (rootLs.code !== 0) {
      const r = await s3p(['mb', `s3://${BUCKET}`]);
      need(r.code === 0, `prologue: mb ${BUCKET}: ${r.out}${r.err}`);
    }
    const v = await s3p(['bucket', 'versioning', `s3://${BUCKET}`, 'on']);
    need(v.code === 0, `prologue: versioning on: ${v.out}${v.err}`);
    if (!/docs/.test(rootLs.out)) await s3p(['mkdir', `s3://${BUCKET}/docs/`]).catch(() => {});
    if (!/data/.test(rootLs.out)) await s3p(['cp', '-r', path.join(FIX, 'data'), `s3://${BUCKET}/data/`, '--json']).catch(() => {});
    if (ftpUp) {
      const f = await cli(['ls', `xf://${RUNID}/gui`]).catch(() => ({ code: 1 }));
      if (f.code !== 0) await cli(['cp', '-r', path.join(FIX, 'data'), `xf://${RUNID}/gui`, '--json']).catch(() => {});
    }
  }


  await verify({ id: 'GUI-01', area: 'gui', action: 'Live stack boots', ds: 'Wails v3 server', scenario: 'server /health ok; page loads; GetVersion binding round-trips the build version', face: 'GUI' }, async () => {
    srv = await startServer();
    const channels = [...new Set([process.env.S3B_BROWSER_CHANNEL, 'msedge', 'chrome'].filter(Boolean))];
    let lastErr;
    for (const ch of channels) {
      try { context = await chromium.launchPersistentContext(USERDIR, { channel: ch, headless: !HEADED }); break; } catch (err) { lastErr = err; }
    }
    need(context, `no usable browser: ${lastErr?.message}`);
    page = context.pages()[0] || await context.newPage();
    page.on('pageerror', (e) => pageErrors.push(e.message));
    page.setDefaultTimeout(20000);
    await page.goto(`http://127.0.0.1:${GUI_PORT}/`);
    await evalPage(() => { localStorage.setItem('s3b-lang', 'en'); });
    await page.reload();
    await waitFor(() => evalPage(() => !!window.go && !!window.runtime), 15000, 'bridge surface');
    const ver = await evalPage(() => window.go['github.com/MikkoP88/s3-bucket-browser/pkg/api'].App.GetVersion());
    need(ver === VERSION, `GetVersion=${ver}`);
    await shot('01-boot');
    return `v3 stack up, ${ver}`;
  });

  await verify({ id: 'GUI-02', area: 'sources', action: 'Add S3 source (GUI)', ds: 'S3 (MinIO)', scenario: 'onboarding → editor → Test ✅ → Save → bucket root lists', face: 'GUI' }, async () => {
    await page.locator('#empty-actions .btn.primary').first().click();
    await waitFor(() => page.locator('#modal-root .modal input.input').count().then((n) => n >= 6), 5000, 'source editor');
    const inputs = page.locator('#modal-root .modal input.input');
    await inputs.nth(0).fill(SRCNAME);
    await inputs.nth(1).fill(BUCKET);
    await inputs.nth(2).fill(ENDPOINT);
    await inputs.nth(3).fill(REGION);
    await inputs.nth(4).fill(KEY);
    await inputs.nth(5).fill(SECRET);
    await page.locator('#modal-root .modal input[type="checkbox"]').first().check();
    await clickFooter(/^test$/i);
    await waitFor(async () => /✅|❌/.test(await modalText()), 30000, 'Test result');
    need((await modalText()).includes('✅'), 'source test failed');
    await shot('02-s3-test');
    await clickFooter(/^save$/i);
    // the CLI batteries already left data/ (and docs/) in the bucket root —
    // wait for those, not for rows later batteries create.
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('data') || k.includes('docs')), 20000, 'bucket root');
    return 'tested ✅ and listed';
  });

  // Seed the FTP side for the GUI copy scenarios through the CLI face.
  // --profile verifys3: the resilience battery added a second s3 source, so
  // bare s3:// URIs would be ambiguous in this store.
  const s3 = (args) => cli(['--profile', 'verifys3', ...args]);
  await cli(['cp', '-r', path.join(FIX, 'data'), `xf://${RUNID}/gui`, '--json']).catch(() => {});
  await s3(['mkdir', `s3://${BUCKET}/verify-gui/`]).catch(() => {});

  const haveFtp = await portOpen(FTP_PORT);
  if (haveFtp) {
    await verify({ id: 'GUI-03', area: 'sources', action: 'Add FTP source (GUI)', ds: 'FTP', scenario: 'sidebar + → FTP fields → Test ✅ → Save → root lists', face: 'GUI' }, async () => {
      await page.locator('#sidebar-head .side-add').click();
      await waitFor(() => page.locator('#modal-root .modal select').count().then((n) => n > 0), 5000, 'source editor');
      await page.locator('#modal-root .modal select').selectOption('ftp');
      const inputs = page.locator('#modal-root .modal input.input');
      await waitFor(() => inputs.count().then((n) => n >= 6), 5000, 'ftp fields');
      await inputs.nth(0).fill(FTPNAME);
      await inputs.nth(1).fill('127.0.0.1');
      await inputs.nth(2).fill(String(FTP_PORT));
      await inputs.nth(3).fill(E2E_USER);
      await inputs.nth(4).fill(E2E_PASS);
      await inputs.nth(5).fill('');
      await clickFooter(/^test$/i);
      await waitFor(async () => /✅|❌/.test(await modalText()), 20000, 'ftp test');
      need((await modalText()).includes('✅'), 'ftp source test failed');
      await shot('03-ftp-test');
      await clickFooter(/^save$/i);
      await waitFor(async () => (await txt('#breadcrumb')).includes(FTPNAME), 15000, 'ftp root');
      return 'tested ✅ and listed';
    });

    await verify({ id: 'GUI-04', area: 'transfers', action: 'Multi-file copy FTP→S3 (GUI)', ds: 'FTP → S3', scenario: 'browse FTP seed dir; ctrl-click 2 files; Ctrl+C; open S3 verify-gui/; Ctrl+V; both rows land', face: 'GUI' }, async () => {
      // browse into the seeded FTP dir
      await treeOpen(FTPNAME);
      await waitFor(async () => (await rowKeys()).some((k) => k.includes(RUNID)), 10000, 'ftp run dir');
      await rowAction(RUNID).then((e) => e.dblclick());
      await waitFor(async () => (await rowKeys()).some((k) => k.includes('gui')), 8000, 'gui dir');
      await rowAction('gui').then((e) => e.dblclick());
      await waitFor(async () => (await rowKeys()).some((k) => k.includes('readme.md')), 8000, 'ftp files');
      await shot('04-ftp-browse');
      // multi-select two files and copy
      await clickRow('readme.md');
      await ctrlClickRow('root-1.txt');
      await page.keyboard.press('Control+c');
      await sleep(300);
      // paste into the S3 side
      await treeOpen(SRCNAME);
      await waitFor(async () => (await rowKeys()).some((k) => k.includes('verify-gui')), 10000, 's3 root');
      await enterFolder('verify-gui');
      // an empty folder shows the empty-state OVERLAY while stale rows linger
      // in the DOM — prove emptiness by the overlay, not by row absence
      await waitFor(async () => evalPage(() => {
        const e = document.getElementById('empty-state');
        return !!e && !e.classList.contains('hidden') && !e.classList.contains('is-loading');
      }), 8000, 'empty verify-gui');
      await page.keyboard.press('Control+v');
      await waitFor(async () => {
        await refresh();
        const keys = await rowKeys();
        return ['readme.md', 'root-1.txt'].every((f) => keys.some((k) => k.includes(f)));
      }, 30000, 'pasted rows');
      await shot('05-ftp-to-s3-pasted');
      return '2 files FTP→S3 through the GUI';
    });

    await verify({ id: 'GUI-05', area: 'transfers', action: 'GUI transfer byte verification', ds: 'FTP → S3', scenario: 'download the GUI-pasted objects via the CLI; bytes match the FTP originals', face: 'GUI' }, async () => {
      const out = path.join(ART, 'gui-verify');
      await rm(out, { recursive: true, force: true });
      const r = await s3(['cp', '-r', `s3://${BUCKET}/verify-gui/`, out]);
      need(r.code === 0, `download: ${r.err}`);
      for (const f of ['readme.md', 'root-1.txt']) {
        const a = await readFile(path.join(FIX, 'data', f));
        const b = await readFile(path.join(out, f));
        need(a.equals(b), `${f} bytes differ after GUI copy`);
      }
      return 'both files byte-identical';
    });
  } else {
    await verify({ id: 'GUI-03', area: 'sources', action: 'Add FTP source (GUI)', ds: 'FTP', scenario: 'FTP container not running', face: 'GUI' }, () => skip('FTP :2121 not reachable'));
  }

  await verify({ id: 'GUI-06', area: 'objects', action: 'Single object deletion (GUI)', ds: 'S3 (MinIO)', scenario: 'CLI-seeded object; row selected; Del → Delete Window (marker default) → confirm; row gone; CLI timeline shows the marker', face: 'GUI' }, async () => {
    await s3(['cp', path.join(FIX, 'data', 'root-2.txt'), `s3://${BUCKET}/verify-gui/gui-del.txt`]);
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('verify-gui')), 10000, 's3 root');
    await enterFolder('verify-gui');
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('gui-del.txt'));
    }, 15000, 'gui-del row');
    await clickRow('gui-del.txt');
    await shot('06-before-delete');
    await page.keyboard.press('Delete');
    await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root input[name="delmode"]').length)) >= 1, 5000, 'delete window');
    // pick the marker radio explicitly (runDeleteWindow parity) when modes are offered
    await evalPage(() => { document.querySelector('#modal-root input[name="delmode"][value=""]')?.click(); });
    const armed = await evalPage(() => {
      const i = document.querySelector('#modal-root .delw-confirm input');
      return !!i && !i.disabled;
    });
    if (armed) await page.locator('#modal-root .delw-confirm input').fill('delete');
    await shot('07-delete-window');
    await clickFooter(/^delete$/i);
    // deleting the only object empties the folder → the empty-state overlay
    // appears, but the stale row may linger in the DOM: gone = row absent
    // OR the settled empty-state
    await waitFor(async () => {
      if ((await rowKeys()).every((k) => !k.includes('gui-del.txt'))) return true;
      return evalPage(() => {
        const e = document.getElementById('empty-state');
        return !!e && !e.classList.contains('hidden') && !e.classList.contains('is-loading');
      });
    }, 45000, 'row gone');
    const v = await s3(['versions', 'ls', `s3://${BUCKET}/verify-gui/gui-del.txt`, '--json']);
    need(v.out.includes('"isDeleteMarker": true'), 'no marker on the CLI side');
    await shot('08-after-delete');
    return 'marker deletion verified both faces';
  });

  await verify({ id: 'GUI-07', area: 'objects', action: 'New folder (GUI)', ds: 'S3 (MinIO)', scenario: 'empty-area context menu → New folder → prompt; CLI ls shows the marker', face: 'GUI' }, async () => {
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('verify-gui')), 10000, 's3 root');
    await enterFolder('verify-gui');
    await evalPage(() => {
      const el = document.getElementById('grid-body');
      const r = el.getBoundingClientRect();
      el.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: r.right - 40, clientY: r.bottom - 30 }));
    });
    await sleep(120);
    const item = await elOrNull((src) => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
      .find((i) => new RegExp(src, 'i').test(i.textContent)) || null, 'new folder');
    need(item, 'no New folder ctx item');
    await item.asElement().click();
    await answerPrompt('verify-folder');
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('verify-folder'));
    }, 15000, 'folder row');
    const l = await s3(['ls', `s3://${BUCKET}/verify-gui/`, '--json']);
    need(l.out.includes('verify-folder'), 'CLI cannot see the GUI-created folder');
    return 'folder created + CLI-verified';
  });

  await verify({ id: 'GUI-08', area: 'objects', action: 'Rename (F2, GUI)', ds: 'S3 (MinIO)', scenario: 'CLI-seeded object; F2 → new name; CLI stat sees the new key', face: 'GUI' }, async () => {
    await s3(['cp', path.join(FIX, 'data', 'empty.txt'), `s3://${BUCKET}/verify-gui/verify-rename-me.txt`]);
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('verify-gui')), 10000, 's3 root');
    await enterFolder('verify-gui');
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('verify-rename-me.txt'));
    }, 15000, 'rename target row');
    await clickRow('verify-rename-me.txt');
    await page.keyboard.press('F2');
    await answerPrompt('verify-renamed.txt');
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('verify-renamed.txt'));
    }, 15000, 'renamed row');
    const s = await s3(['stat', `s3://${BUCKET}/verify-gui/verify-renamed.txt`]);
    need(/size:/.test(s.out), `CLI stat of renamed key: ${s.out}${s.err}`);
    return 'renamed + CLI-verified';
  });

  // ---------------- second battery: the deeper GUI surface ----------------
  // Every row below re-verifies BACK through the CLI (s3 helper) — a green
  // GUI row still means bytes moved. Shared context: dual pane open, local
  // side pointed at the fixture tree, remote side inside verify-gui/.

  const visKeys = () => evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .filter((r) => r.style.display !== 'none' && r._model).map((r) => String(r._model.key)));
  const navCertGui = async () => {
    // cascade insurance: an earlier row can leave a floating popout open
    // over the grid (GUI-34 leaves the transfer manager) where it
    // intercepts pointer events and starves every later navigation —
    // close any raised popout through its real UI affordance first
    try {
      await evalPage(() => { document.querySelectorAll('#popout-root .popout .modal-head .x').forEach((b) => b.click()); return true; });
      await sleep(150);
    } catch { /* best effort */ }
    // same insurance for a raised modal + armed filter (a row that dies
    // mid-dialog leaves both over the grid): dismiss through the real
    // affordance, force-hide as a last resort — closeModal already does
    // exactly that escalation
    try { await closeModal(); } catch { /* best effort */ }
    try { await clearFilter(); } catch { /* best effort */ }
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('verify-gui')), 10000, 's3 root');
    await enterFolder('verify-gui');
  };
  // The verify-minio source is bucket-scoped, so sibling buckets the CLI
  // creates (…-mig, …-adm, …-l2x) never appear while it is open. The
  // breadcrumb's ROOT crumb opens the source's account-wide buckets view —
  // the grid then lists every bucket of the endpoint (and the tree adopts
  // the live bucket set).
  const goBuckets = async () => {
    await evalPage(() => { document.querySelector('#breadcrumb .crumb')?.click(); });
    try {
      // the buckets view's breadcrumb is exactly ONE crumb (the source
      // root, current) — a virtualization-proof signature: this endpoint
      // accumulates dozens of leftover buckets, far more than the grid's
      // rendered viewport, so row-based probes are blind down the list
      await waitFor(() => evalPage(() => document.querySelectorAll('#breadcrumb .crumb').length === 1), 10000, 'the buckets view (single-crumb breadcrumb)');
    } catch (e) {
      // one-line state dump + screenshot: FAIL details are truncated to
      // their first line, so the whole view state must fit on one line
      await shot('buckets-timeout');
      const state = await evalPage(() => JSON.stringify({
        crumbs: Array.from(document.querySelectorAll('#breadcrumb .crumb')).map((c) => c.textContent),
        rows: Array.from(document.querySelectorAll('#grid-body .grid-row')).slice(0, 12)
          .map((r) => r.dataset.key ?? (r.textContent || '').slice(0, 24)),
        empty: document.getElementById('empty-state')?.className || 'none',
        cur: document.querySelector('#breadcrumb .crumb.current')?.textContent || '',
      }));
      throw new Error(`${e.message}: ${state}`);
    }
  };
  // #local-grid-body is permanently mounted (index.html) and only hidden via
  // the pane's .hidden class, so visibility — not existence — is the probe.
  const dualOpen = () => evalPage(() => {
    const p = document.getElementById('local-pane');
    return !!p && !p.classList.contains('hidden');
  });
  const ensureDualPane = async () => {
    if (!(await dualOpen())) {
      await page.keyboard.press('F9');
      await waitFor(dualOpen, 5000, 'dual pane');
    }
  };
  const localDir = async (dir, expect) => {
    await page.locator('#local-crumb').click();
    await answerPrompt(dir);
    await waitFor(async () => (await sideKeys()).includes(expect), 10000, `local side shows ${expect}`);
  };
  const cliVerCount = async (key) => {
    const r = await s3(['versions', 'ls', key, '--json']);
    return countLines(r.out, '"versionId"');
  };

  await verify({ id: 'GUI-10', area: 'transfers', action: 'DnD upload local→S3 (GUI)', ds: 'S3 (MinIO)', scenario: 'dual pane: drag uni-åäö.txt from the local side onto the bucket folder; Start; CLI sees the object; bytes identical', face: 'GUI' }, async () => {
    await navCertGui();
    await ensureDualPane();
    await localDir(path.join(FIX, 'data'), 'readme.md');
    await dnd(await sideRow('uni-åäö.txt'), await bodyH());
    await startIfAsked(8000);
    const K = `s3://${BUCKET}/verify-gui/uni-åäö.txt`;
    await waitFor(async () => /size:/.test((await s3(['stat', K])).out), 30000, 'uploaded object');
    const out = path.join(ART, 'gui-dl-uni.txt');
    await s3(['cp', K, out]);
    need((await readFile(out)).equals(await readFile(path.join(FIX, 'data', 'uni-åäö.txt'))), 'DnD upload bytes differ');
    await shot('10-dnd-upload');
    return 'unicode filename uploaded, byte-identical';
  });

  await verify({ id: 'GUI-11', area: 'transfers', action: 'DnD download S3→local (GUI)', ds: 'S3 (MinIO)', scenario: 'drag an S3 row onto the local pane; Start; file lands on disk; bytes identical', face: 'GUI' }, async () => {
    const dst = path.join(ART, 'gui-dl');
    await mkdir(dst, { recursive: true });
    await navCertGui();
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('verify-renamed.txt'));
    }, 15000, 'download source row');
    await ensureDualPane();
    await page.locator('#local-crumb').click();
    await answerPrompt(dst);
    await waitFor(async () => (await sideKeys()).length === 0, 8000, 'empty local dst');
    await dnd(await rowAction('verify-renamed.txt'), await sideBodyH());
    await startIfAsked(8000);
    await waitFor(async () => (await sideKeys()).includes('verify-renamed.txt'), 45000, 'downloaded row');
    const got = await readFile(path.join(dst, 'verify-renamed.txt'));
    need(got.equals(await readFile(path.join(FIX, 'data', 'empty.txt'))), 'DnD download bytes differ');
    await shot('11-dnd-download');
    return 'downloaded via DnD, byte-identical';
  });

  await verify({ id: 'GUI-12', area: 'versions', action: 'Versions dialog: restore as latest', ds: 'S3 (MinIO)', scenario: '2 CLI-seeded versions; context menu → Versions shows the timeline; Restore as latest on the older one; CLI downloads the restored bytes', face: 'GUI' }, async () => {
    const K = `s3://${BUCKET}/verify-gui/gui-ver.txt`;
    await s3(['cp', path.join(FIX, 'data', 'root-1.txt'), K]);
    await s3(['cp', path.join(FIX, 'data', 'root-2.txt'), K, '--force']);
    await navCertGui();
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('gui-ver.txt'));
    }, 15000, 'gui-ver row');
    await rightClickRow('gui-ver.txt');
    await ctxItem(/^versions/i);
    await waitFor(async () => await page.locator('#modal-root .ver-row').count().then((n) => n >= 2), 15000, 'version rows');
    await shot('12-versions-dialog');
    await page.locator('#modal-root .ver-row').last().locator('button', { hasText: 'Restore as latest' }).click();
    await sleep(600);
    await closeModal();
    await waitFor(async () => (await cliVerCount(K)) >= 3, 30000, 'restore landed as a new version');
    const out = path.join(ART, 'gui-restore.txt');
    await s3(['cp', K, out]);
    need((await readFile(out)).equals(await readFile(path.join(FIX, 'data', 'root-1.txt'))), 'latest is not the restored v1');
    return 'restored as latest, CLI-verified';
  });

  await verify({ id: 'GUI-13', area: 'transfers', action: 'Overwrite conflict dialog', ds: 'S3 (MinIO)', scenario: 'DnD a file onto an existing name → conflict dialog offers Start; confirming creates the next version', face: 'GUI' }, async () => {
    const K = `s3://${BUCKET}/verify-gui/readme.md`;
    const base = await cliVerCount(K);
    need(base >= 1, 'no baseline version for conflict target');
    await navCertGui();
    await localDir(path.join(FIX, 'data'), 'readme.md');
    await dnd(await sideRow('readme.md'), await bodyH());
    await waitFor(async () => /already exist/i.test(await modalText()), 10000, 'conflict dialog');
    await shot('13-conflict');
    await clickFooter(/^start$/i);
    await waitFor(async () => (await cliVerCount(K)) === base + 1, 60000, 'overwrite version landed');
    if (await modalVisible()) await closeModal();
    return 'conflict → Start → new version';
  });

  await verify({ id: 'GUI-14', area: 'objects', action: 'Delete Window CANCEL keeps the object', ds: 'S3 (MinIO)', scenario: 'Del on a row opens the Delete Window; Cancel/Esc leaves the object untouched on BOTH faces (the cancellation contract)', face: 'GUI' }, async () => {
    const K = `s3://${BUCKET}/verify-gui/gui-cancel.txt`;
    await s3(['cp', path.join(FIX, 'data', 'root-1.txt'), K]);
    await navCertGui();
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('gui-cancel.txt'));
    }, 15000, 'cancel target row');
    await clickRow('gui-cancel.txt');
    await page.keyboard.press('Delete');
    await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root input[name="delmode"]').length)) >= 1, 5000, 'delete window');
    await shot('14-delete-window-cancel');
    await closeModal();
    need(!(await modalVisible()), 'delete window still open after cancel');
    const s = await s3(['stat', K]);
    need(/size:/.test(s.out), 'object vanished after CANCELLED delete');
    await refresh();
    need((await rowKeys()).some((k) => k.includes('gui-cancel.txt')), 'row gone after cancelled delete');
    return 'cancelled; object intact on both faces';
  });

  await verify({ id: 'GUI-15', area: 'admin', action: 'Doctor over the bridge', ds: 'S3 (MinIO)', scenario: 'Help → Doctor → pick the source → run all checks in the popout; summary reports pass; task completes', face: 'GUI' }, async () => {
    await menuClick(/help/i, /doctor/i);
    await waitFor(() => evalPage(() => document.querySelectorAll('#modal-root .picker-row').length > 0), 5000, 'doctor picker');
    const row = await elOrNull((want) => Array.from(document.querySelectorAll('#modal-root .picker-row'))
      .find((r) => r.textContent.includes(want)) || null, SRCNAME);
    need(row, 'doctor picker missing the source');
    await row.asElement().click();
    await waitFor(() => evalPage(() => !!document.querySelector('#popout-root .popout')), 10000, 'doctor popout');
    const runBtn = await elOrNull(() => Array.from(document.querySelectorAll('#popout-root .popout button, #popout-root .popout .btn'))
      .find((b) => /run all/i.test(b.textContent)) || null);
    if (runBtn) await runBtn.asElement().click();
    await waitFor(() => evalPage(() => /pass/i.test(document.querySelector('#popout-root .doc-summary')?.textContent || '')), 60000, 'doctor summary');
    const tasks = JSON.stringify(await call('RunningTasks'));
    need(tasks.includes('doctor'), 'no doctor task recorded');
    await shot('15-doctor');
    await page.keyboard.press('Escape').catch(() => {});
    return 'ladder ran, summary pass';
  });

  await verify({ id: 'GUI-16', area: 'admin', action: 'Admin dialog (bucket info)', ds: 'S3 (MinIO)', scenario: 'bucket guard in the tree opens the Admin panel; versioning reported; tabs render; close', face: 'GUI' }, async () => {
    await page.locator('#tree .tguard').first().click();
    await waitFor(async () => /versioning/i.test(await modalText()), 10000, 'admin overview');
    need((await modalText()).includes('Admin panel'), 'not the Admin panel');
    const tabs = await evalPage(() => Array.from(document.querySelectorAll('#modal-root .tab')).length);
    need(tabs >= 1, 'no tabs');
    await shot('16-admin');
    await closeModal();
    return `admin panel, ${tabs} tab(s)`;
  });

  await verify({ id: 'GUI-17', area: 'sources', action: 'Profile file round-trip (bindings)', ds: 'Wails v3 server', scenario: 'SaveProfileFileAs → state open; Close → onboarding; wrong password rejected through the bridge; correct password restores the sources', face: 'GUI' }, async () => {
    const PROFILE = path.join(ART, 'verify-walk.s3bprofile');
    const PW = 'verify-pass-123';
    await call('SaveProfileFileAs', PROFILE, PW);
    const st = await call('GetProfileFileState');
    need(st?.open === true && st?.sourceCount >= 1, `state after save: ${JSON.stringify(st)}`);
    need((await readFile(PROFILE)).length > 0, 'profile file not written');
    await call('CloseProfileFile', true);
    await page.reload();
    await waitFor(() => evalPage(() => Array.from(document.querySelectorAll('#empty-actions .btn')).length > 0), 15000, 'onboarding after close');
    let err = '';
    try { await call('OpenProfileFile', PROFILE, 'definitely-wrong'); } catch (e) { err = String(e?.message || e); }
    need(/password|decrypt|corrupt/i.test(err), `wrong password not rejected: ${err}`);
    await call('OpenProfileFile', PROFILE, PW);
    await page.reload();
    await waitFor(() => evalPage(() => !!window.go && !!window.runtime), 15000, 'bridge after reopen');
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('verify-gui')), 20000, 'sources restored');
    await shot('17-profile-roundtrip');
    return 'save/close/reject/reopen all good';
  });

  await verify({ id: 'GUI-18', area: 'gui', action: 'Workbench surfaces: transfer manager + dual pane + filter', ds: 'S3 (MinIO)', scenario: 'View → Transfers opens the manager popout; dual pane toggle; filter box narrows the grid to matching rows and clearing restores them', face: 'GUI' }, async () => {
    await menuClick(/view/i, /transfers/i);
    await waitFor(() => evalPage(() => !!document.querySelector('#popout-root .popout')), 5000, 'transfer manager popout');
    await shot('18-transfers');
    await page.keyboard.press('Escape').catch(() => {});
    await ensureDualPane();
    need(await dualOpen(), 'dual pane not open');
    await navCertGui();
    await filterTo('gui-cancel');
    let keys = await visKeys();
    need(keys.length >= 1 && keys.every((k) => k.toLowerCase().includes('gui-cancel')), `filter narrow: ${JSON.stringify(keys)}`);
    await clearFilter();
    await waitFor(async () => (await visKeys()).some((k) => !k.toLowerCase().includes('gui-cancel')), 5000, 'filter cleared');
    keys = await visKeys();
    need(keys.length >= 2, `filter restore: ${keys.length} rows`);
    return 'popout + dual pane + filter all live';
  });

  await verify({ id: 'GUI-19', area: 'objects', action: 'New file dialog: cancel + create', ds: 'S3 (MinIO)', scenario: 'Shift+F4 prompt (defaults new-file/txt); Cancel creates NOTHING (CLI 404); then create verify-newfile.txt for real; CLI stats it', face: 'GUI' }, async () => {
    await navCertGui();
    await page.keyboard.press('Shift+F4');
    await waitFor(() => evalPage(() => !!document.querySelector('#modal-root input.input')), 5000, 'new-file prompt');
    const defaults = await evalPage(() => {
      const m = document.getElementById('modal-root');
      return m.querySelector('input.input')?.value + '|' + m.querySelector('select')?.value;
    });
    need(defaults === 'new-file|txt', `prompt defaults: ${defaults}`);
    await shot('19-newfile-prompt');
    await closeModal();
    let s = await s3(['stat', `s3://${BUCKET}/verify-gui/new-file.txt`]);
    need(s.code !== 0, 'cancel created an object');
    await page.keyboard.press('Shift+F4');
    await waitFor(() => evalPage(() => !!document.querySelector('#modal-root input.input')), 5000, 'new-file prompt again');
    await answerPrompt('verify-newfile');
    await waitFor(async () => /size:/.test((await s3(['stat', `s3://${BUCKET}/verify-gui/verify-newfile.txt`])).out), 20000, 'new file object');
    return 'cancel inert; create landed';
  });

  // ---- extreme-level rows: critical-data contracts through the GUI face ----

  await verify({ id: 'GUI-20', area: 'transfers', action: 'Conflict matrix: per-file skip + rename', ds: 'S3 (MinIO)', scenario: 'both dragged files conflict; a.txt→skip keeps v1 untouched (still 1 version, original bytes); b.txt→rename keeps the original AND lands the new bytes beside it', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/cfmix`;
    const seed = path.join(ART, 'cfmix');
    await mkdir(seed, { recursive: true });
    await writeFile(path.join(seed, 'a.orig'), 'CF-A-ORIGINAL\n');
    await writeFile(path.join(seed, 'b.orig'), 'CF-B-ORIGINAL\n');
    await s3(['cp', path.join(seed, 'a.orig'), `${P}/a.txt`]);
    await s3(['cp', path.join(seed, 'b.orig'), `${P}/b.txt`]);
    // the local files must carry the SAME names as the destination objects —
    // a collision (and thus the matrix) only exists when the names match
    await writeFile(path.join(FIX, 'data', 'a.txt'), 'CF-A-NEW\n');
    await writeFile(path.join(FIX, 'data', 'b.txt'), 'CF-B-NEW\n');
    await navCertGui();
    await waitFor(async () => { await refresh(); return (await rowKeys()).some((k) => k.includes('cfmix')); }, 15000, 'cfmix folder row');
    await enterFolder('cfmix');
    await ensureDualPane();
    await localDir(path.join(FIX, 'data'), 'a.txt');
    // multi-select both local files, then drag the selection into the folder:
    // grid dragstart carries the whole selection (grid.js), so ONE drag moves
    // both files and the destination pre-check offers the per-file matrix —
    // the same funnel GUI-13 proves for a single file
    await (await sideRow('a.txt')).click({ modifiers: ['Control'] });
    await (await sideRow('b.txt')).click({ modifiers: ['Control'] });
    await dnd(await sideRow('b.txt'), await bodyH());
    await waitFor(async () => /already exist/i.test(await modalText()), 10000, 'conflict matrix dialog');
    await shot('20-conflict-matrix');
    await evalPage(() => {
      const rows = Array.from(document.querySelectorAll('#modal-root .cf-row'));
      const setFor = (suffix, action) => {
        const row = rows.find((r) => ((r.querySelector('.cf-name')?.textContent) || '').trim().endsWith(suffix));
        const sel = row?.querySelector('select.cf-action');
        if (!sel) throw new Error(`no decision select for ${suffix}`);
        sel.value = action;
        sel.dispatchEvent(new Event('change', { bubbles: true }));
      };
      setFor('a.txt', 'skip');
      setFor('b.txt', 'rename');
      return true;
    });
    await clickFooter(/^start$/i);
    await waitFor(async () => countLines((await s3(['ls', `${P}/`, '--recursive', '--json'])).out, '"key"') >= 3, 60000, 'conflict transfer landed');
    const names = (await s3(['ls', `${P}/`, '--recursive', '--json'])).out
      .split('\n').filter((l) => l.includes('"key"'))
      .map((l) => ((l.match(/"key":\s*"([^"]*)"/) || [])[1] || '').split('/').pop())
      .filter(Boolean).sort();
    need(names.length === 3, `cfmix holds ${names.length} objects: ${names.join(', ')}`);
    const outA = path.join(ART, 'cf-a-got.txt');
    await s3(['cp', `${P}/a.txt`, outA]);
    need((await readFile(outA)).toString() === 'CF-A-ORIGINAL\n', 'skip overwrote a.txt');
    need((await cliVerCount(`${P}/a.txt`)) === 1, 'skip still created a version');
    const outB = path.join(ART, 'cf-b-got.txt');
    await s3(['cp', `${P}/b.txt`, outB]);
    need((await readFile(outB)).toString() === 'CF-B-ORIGINAL\n', 'rename modified the original b.txt');
    const twin = names.find((n) => n !== 'a.txt' && n !== 'b.txt');
    need(twin, 'no renamed twin landed beside the original');
    const outT = path.join(ART, 'cf-twin-got.txt');
    await s3(['cp', `${P}/${twin}`, outT]);
    need((await readFile(outT)).toString() === 'CF-B-NEW\n', 'renamed twin does not carry the new bytes');
    return `skip kept v1 untouched; rename landed "${twin}" with the new bytes`;
  });

  await verify({ id: 'GUI-21', area: 'transfers', action: 'Cancel mid-transfer: no corrupt object; retry clean', ds: 'S3 (MinIO)', scenario: '8 MiB upload throttled to 256 kB/s; Cancel while running → job canceled and the object ABSENT (no partial lands); the unthrottled retry is sha-identical', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/cancel`;
    await s3(['mkdir', `${P}/`]);
    const src = path.join(FIX, 'data', 'cancel-8m.bin');
    await writeFile(src, randomBytes(8 * 1024 * 1024));
    const sha = await sha256file(src);
    await evalPage(() => { localStorage.setItem('s3b-throttle', '262144'); localStorage.setItem('s3b-show-throttle', '1'); return true; });
    await navCertGui();
    await waitFor(async () => { await refresh(); return (await rowKeys()).some((k) => k.includes('cancel')); }, 15000, 'cancel folder row');
    await enterFolder('cancel');
    await ensureDualPane();
    await localDir(path.join(FIX, 'data'), 'cancel-8m.bin');
    await dnd(await sideRow('cancel-8m.bin'), await bodyH());
    // clean destination: the transfer starts silently (resolveTransferOpts) or
    // through the classic confirm — either way maxBps comes from the
    // remembered throttle, so the job must be slow in both branches
    await startIfAsked(3000);
    await waitFor(async () => {
      const j = ((await call('ActiveTransfers')) || []).find((x) => x.status === 'running' && !x.hidden);
      return j && j.sentBytes > 0 && j.sentBytes < j.totalBytes;
    }, 20000, 'throttled running job');
    await shot('21-cancel-running');
    // the manager re-renders its rows on every progress tick — any element
    // handle is stale within one tick (the b496f31 run died exactly there:
    // "Element is not attached to the DOM"), so the click must be found AND
    // clicked in the same in-page JS turn, retried until it lands
    await waitFor(async () => evalPage(() => {
      const b = Array.from(document.querySelectorAll('.tr-job.running button'))
        .find((x) => /cancel/i.test(x.textContent || ''));
      if (!b) return false;
      b.click();
      return true;
    }), 10000, 'Cancel click on the running job');
    await waitFor(async () => JSON.stringify(await call('ActiveTransfers')).includes('canceled'), 20000, 'job canceled');
    await evalPage(() => { localStorage.removeItem('s3b-throttle'); localStorage.removeItem('s3b-show-throttle'); return true; });
    const st = await s3(['stat', `${P}/cancel-8m.bin`]);
    need(st.code !== 0, 'a partial object survived the cancel');
    // the clean retry must work with zero stuck state
    await dnd(await sideRow('cancel-8m.bin'), await bodyH());
    await startIfAsked(3000);
    await waitFor(async () => /size:/.test((await s3(['stat', `${P}/cancel-8m.bin`])).out), 60000, 'retried upload');
    const dl = path.join(ART, 'cancel-8m.dl.bin');
    await s3(['cp', `${P}/cancel-8m.bin`, dl]);
    need(await sha256file(dl) === sha, 'retry after cancel bytes differ');
    return 'canceled job left no object; retry sha-identical';
  });

  await verify({ id: 'GUI-22', area: 'gates', action: 'L2 identity gate: Empty-bucket window', ds: 'S3 (MinIO)', scenario: 'the destructive Empty-bucket window demands the bucket\'s OWN name: a wrong word keeps the destructive button disabled; Cancel preserves every object', face: 'GUI' }, async () => {
    const guard = await elOrNull((b) => {
      const n = Array.from(document.querySelectorAll('#tree .tnode'))
        .find((x) => (x.querySelector('.tlabel')?.textContent || '').trim() === b);
      return n?.querySelector('.tguard') || document.querySelector('#tree .tguard');
    }, BUCKET);
    need(guard, 'no bucket guard in the tree');
    await guard.asElement().click();
    await waitFor(async () => /admin panel/i.test(await modalText()), 10000, 'admin panel');
    const nTabs = await evalPage(() => document.querySelectorAll('#modal-root .tab').length);
    need(nTabs >= 1, 'no admin tabs');
    let found = false;
    for (let i = 0; i < nTabs && !found; i++) {
      await page.locator('#modal-root .tab').nth(i).click();
      found = /empty bucket \(all versions\)/i.test(await modalText());
      if (!found) {
        // tab content can render a beat late under load — give each tab a
        // short grace window before concluding the tool is not there
        try {
          await waitFor(async () => /empty bucket \(all versions\)/i.test(await modalText()), 1500, 'tab render');
          found = true;
        } catch { /* not this tab */ }
      }
    }
    need(found, 'no Empty-bucket tool in any admin tab');
    await shot('22-l2-typed-window');
    await evalPage(() => {
      Array.from(document.querySelectorAll('#modal-root button'))
        .find((b) => /empty bucket \(all versions\)/i.test(b.textContent || ''))?.click();
      return true;
    });
    await waitFor(() => page.locator('#modal-root .modal input.input').count().then((n) => n > 0), 8000, 'typed-word input');
    const input = page.locator('#modal-root .modal input.input').last();
    const btnDisabled = () => evalPage(() => {
      const b = Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
        .find((x) => /empty bucket/i.test(x.textContent.trim()));
      return b === undefined ? null : !!b.disabled;
    });
    await input.fill(`${BUCKET}-typo`);
    need((await btnDisabled()) === true, 'destructive button enabled with a WRONG word');
    await input.fill(BUCKET);
    need((await btnDisabled()) === false, 'destructive button stays disabled with the right word');
    await closeModal(); // the Delete Window's Cancel — never the destructive path
    await closeModal(); // the admin panel
    const s = await s3(['stat', `s3://${BUCKET}`]);
    need(/region|created|size/i.test(s.out), `bucket damaged by a cancelled window: ${s.out}`);
    const l = await s3(['ls', `s3://${BUCKET}`, '--recursive', '--json']);
    need(countLines(l.out, '"key"') > 0, 'bucket emptied by a CANCELLED window');
    return 'wrong word disabled; cancel preserved everything';
  });

  await verify({ id: 'GUI-23', area: 'objects', action: 'Editor auto-upload round-trip (bridge)', ds: 'S3 (MinIO)', scenario: 'EditObject stages the object, hands it to the OS (an inert .cmd probe file — the handoff is real but the "editor" is a no-op) and watches: bytes written to the staged file upload automatically; StopEdit ends the session', face: 'GUI' }, async () => {
    const K = 'verify-gui/edit/verify-edit.cmd';
    const v1 = 'rem s3b verify editor probe v1\n';
    const seed = path.join(ART, 'edit-v1.cmd');
    await writeFile(seed, v1);
    await s3(['cp', seed, `s3://${BUCKET}/${K}`]);
    const info = await call('EditObject', BUCKET, K, false);
    const local = info?.Local || info?.local;
    need(local && fs.existsSync(local), `EditObject did not stage the file: ${JSON.stringify(info)}`);
    need((await readFile(local)).toString() === v1, 'staged bytes differ from the object');
    const v2 = `rem s3b verify editor probe v2 EDITED-${RUNID}\n`;
    await writeFile(local, v2); // the "editor save" — the watcher must catch it
    const out = path.join(ART, 'edit-got.cmd');
    await waitFor(async () => {
      const r = await s3(['cp', `s3://${BUCKET}/${K}`, out]);
      return r.code === 0 && (await readFile(out)).toString() === v2;
    }, 30000, 'watcher auto-upload');
    need(JSON.stringify(await call('EditingFiles')).includes(K), 'session not listed while editing');
    await call('StopEdit', BUCKET, K, false);
    need(!JSON.stringify(await call('EditingFiles')).includes(K), 'session survived StopEdit');
    return `edit auto-uploaded and round-tripped (${K})`;
  });

  await verify({ id: 'GUI-24', area: 'security', action: 'Pre-sign URL dialog (GUI)', ds: 'S3 (MinIO)', scenario: 'context menu → Pre-sign URL: the dialog exposes a READ-ONLY signed URL that a bare HTTP client outside the app can actually use to fetch the exact object bytes', face: 'GUI' }, async () => {
    await navCertGui();
    await waitFor(async () => { await refresh(); return (await rowKeys()).some((k) => k.includes('readme.md')); }, 15000, 'readme row');
    await rightClickRow('readme.md');
    await ctxItem(/^pre-sign url/i);
    await waitFor(async () => /pre-signed url/i.test(await modalText()), 10000, 'presign dialog');
    const url = await evalPage(() => document.querySelector('#modal-root input.input.mono')?.value || '');
    need(/X-Amz-Signature=/.test(url), `URL is not signed: ${url.slice(0, 90)}`);
    const ro = await evalPage(() => document.querySelector('#modal-root input.input.mono')?.readOnly);
    need(ro === true, 'the URL field is editable — share dialogs must be read-only');
    await shot('24-presign');
    const res = await fetch(url);
    need(res.ok, `presigned fetch: HTTP ${res.status}`);
    const got = Buffer.from(await res.arrayBuffer());
    const want = await readFile(path.join(FIX, 'data', 'readme.md'));
    need(got.equals(want), 'presigned URL served wrong bytes');
    await closeModal();
    return 'read-only signed URL, verified by an out-of-app client';
  });

  await verify({ id: 'GUI-25', area: 'transfers', action: 'Pane compare: all six categories exact', ds: 'S3 (MinIO) + local', scenario: 'a hand-built pair of dirs covering EVERY compare class — identical, only-left, only-right, different-size, newer-left, newer-right — the Compare button must count each category exactly 1', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/cmp`;
    await s3(['mkdir', `${P}/`]);
    const ld = path.join(ART, 'gcmp');
    await rm(ld, { recursive: true, force: true });
    await mkdir(ld, { recursive: true });
    const up = async (name, body, remote) => {
      const tmp = path.join(ART, `cmp-${name}`);
      await writeFile(tmp, body);
      await s3(['cp', tmp, `${P}/${remote}`]);
    };
    // remote seeds first; same.txt LAST so its upload time is fresh when the
    // local twin stamps the same instant (compare tolerance is 2 s)
    await up('or', 'only right\n', 'only-right.txt');
    await up('sd', 'REMOTE-MUCH-LONGER\n', 'size-diff.txt');   // vs local 4 B
    await up('nl', 'NL\n', 'newer-left.txt');                  // local is +1 day
    await up('nr', 'NR\n', 'newer-right.txt');                 // local is -1 day
    await up('same', 'SAME-BYTES\n', 'same.txt');
    const ut = (f, ms) => fs.utimesSync(path.join(ld, f), new Date(ms), new Date(ms));
    await writeFile(path.join(ld, 'only-left.txt'), 'only left\n');
    await writeFile(path.join(ld, 'size-diff.txt'), 'LOC\n');
    await writeFile(path.join(ld, 'newer-left.txt'), 'NL\n');
    ut('newer-left.txt', Date.now() + 86400000);   // beats any upload time
    await writeFile(path.join(ld, 'newer-right.txt'), 'NR\n');
    ut('newer-right.txt', Date.now() - 86400000);  // loses to any upload time
    await writeFile(path.join(ld, 'same.txt'), 'SAME-BYTES\n');
    ut('same.txt', Date.now());                    // ≈ the just-finished upload
    await navCertGui();
    await waitFor(async () => { await refresh(); return (await rowKeys()).some((k) => k.includes('cmp')); }, 15000, 'cmp folder row');
    await enterFolder('cmp');
    await ensureDualPane();
    await localDir(ld, 'only-left.txt');
    await page.locator('#local-compare').click();
    await waitFor(async () => /compare —/i.test(await modalText()), 30000, 'compare summary');
    await shot('25-compare');
    const counts = await evalPage(() => {
      const kids = Array.from(document.querySelectorAll('#modal-root .kv > *'));
      const out = {};
      for (let i = 0; i + 1 < kids.length; i += 2) out[kids[i].textContent.trim()] = kids[i + 1].textContent.trim();
      return out;
    });
    const want = { Identical: '1', 'Only on left': '1', 'Only on right': '1', 'Different size': '1', 'Newer on left': '1', 'Newer on right': '1' };
    for (const [label, val] of Object.entries(want)) {
      const key = Object.keys(counts).find((k) => k.startsWith(label));
      need(key && counts[key] === val, `${label}: want ${val}, got ${key ? counts[key] : 'missing'} (${JSON.stringify(counts)})`);
    }
    await closeModal();
    return 'all six categories counted exactly';
  });

  await verify({ id: 'GUI-26', area: 'transfers', action: 'Ctrl+C stages HIDDEN — nothing lands until Paste', ds: 'S3 (MinIO)', scenario: 'the two-part copy contract: select remote rows, Ctrl+C → the Explorer mirror runs as a HIDDEN staging job (a scratch download for paste-out, invisible in every list) while the bucket itself stays bit-for-bit unchanged until a Paste happens', face: 'GUI' }, async () => {
    await navCertGui();
    await waitFor(async () => { await refresh(); return (await rowKeys()).some((k) => k.includes('readme.md')); }, 15000, 'readme row');
    const cnt = async () => countLines((await s3(['ls', `s3://${BUCKET}/verify-gui/`, '--recursive', '--json'])).out, '"key"');
    const c0 = await cnt();
    // the clip workspace is minted FRESH per copy (os.MkdirTemp under the
    // secure-storage clip root, else %TEMP%\s3b-clip) — snapshot the dirs,
    // then discover the new one after the staging download finishes
    const clipRoots = [path.join(GUICFG, 'clip'), path.join(os.tmpdir(), 's3b-clip')];
    const clipDirs = async () => {
      const out = [];
      for (const root of clipRoots) {
        try {
          for (const e of await readdir(root, { withFileTypes: true })) if (e.isDirectory()) out.push(path.join(root, e.name));
        } catch { /* root not present */ }
      }
      return out;
    };
    const beforeDirs = new Set(await clipDirs());
    const before = new Set(((await call('ActiveTransfers')) || []).map((j) => String(j.id)));
    await clickRow('readme.md');
    await ctrlClickRow('uni-åäö.txt');
    await page.keyboard.press('Control+c');
    let fresh = [];
    await waitFor(async () => {
      const now = (await call('ActiveTransfers')) || [];
      fresh = now.filter((j) => !before.has(String(j.id)));
      return fresh.length > 0;
    }, 10000, 'clipboard staging job');
    await shot('26-hidden-staging');
    // the staging download is REAL (Explorer paste-out needs the bytes) —
    // the contract is that it is HIDDEN and that nothing lands anywhere
    // except the internal scratch dir until a Paste happens
    need(fresh.every((j) => j.hidden === true), `staging job visible in the manager: ${JSON.stringify(fresh).slice(0, 220)}`);
    await waitFor(async () => {
      const now = (await call('ActiveTransfers')) || [];
      return now.filter((j) => !before.has(String(j.id))).every((j) => j.status === 'done');
    }, 45000, 'hidden staging download to finish');
    // the staged bytes must actually exist for Explorer to paste out
    let staged = null;
    await waitFor(async () => {
      for (const d of await clipDirs()) {
        if (beforeDirs.has(d)) continue;
        try { await readFile(path.join(d, 'readme.md')); staged = path.join(d, 'readme.md'); return true; } catch { /* not yet */ }
      }
      return false;
    }, 15000, 'staged readme.md in a fresh clip dir');
    need((await readFile(staged)).equals(await readFile(path.join(FIX, 'data', 'readme.md'))), 'staged bytes differ from the object');
    // and the two-part contract: a bare Ctrl+C must not change the source
    need(await cnt() === c0, 'objects changed after a bare Ctrl+C — copy is not two-part');
    return 'staging hidden + bytes staged for Explorer; bucket unchanged';
  });

  await verify({ id: 'GUI-27', area: 'transfers', action: 'Download-side conflict matrix', ds: 'S3 (MinIO) → local', scenario: 'drag three remote rows onto a local folder holding two of the SAME names: the per-file decision matrix (skip + rename) must keep the skipped local file untouched, land a renamed twin with the remote bytes, and download the clean third file with no prompt', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/dlcf`;
    await s3(['mkdir', `${P}/`]);
    const seed = path.join(ART, 'dlcf-seed');
    await rm(seed, { recursive: true, force: true });
    await mkdir(seed, { recursive: true });
    await writeFile(path.join(seed, 'a.txt'), 'DLCF-A-REMOTE\n');
    await writeFile(path.join(seed, 'b.txt'), 'DLCF-B-REMOTE\n');
    await writeFile(path.join(seed, 'fresh.txt'), 'DLCF-FRESH-REMOTE\n');
    for (const f of ['a.txt', 'b.txt', 'fresh.txt']) await s3(['cp', path.join(seed, f), `${P}/${f}`]);
    const dst = path.join(ART, 'dlcf-dst');
    await rm(dst, { recursive: true, force: true });
    await mkdir(dst, { recursive: true });
    await writeFile(path.join(dst, 'a.txt'), 'DLCF-A-LOCAL-KEEP\n');
    await writeFile(path.join(dst, 'b.txt'), 'DLCF-B-LOCAL-KEEP\n');
    await navCertGui();
    await waitFor(async () => { await refresh(); return (await rowKeys()).some((k) => k.includes('dlcf')); }, 15000, 'dlcf folder row');
    await enterFolder('dlcf');
    await ensureDualPane();
    await localDir(dst, 'a.txt');
    // anchor + extend the selection, then drag the WHOLE selection onto the
    // local pane (grid dragstart carries the selection — one drag, three files)
    await clickRow('a.txt');
    await ctrlClickRow('b.txt');
    await ctrlClickRow('fresh.txt');
    await dnd(await rowAction('fresh.txt'), await sideBodyH());
    await waitFor(async () => /already exist/i.test(await modalText()), 10000, 'download conflict matrix');
    await shot('27-dl-conflict');
    await evalPage(() => {
      const rows = Array.from(document.querySelectorAll('#modal-root .cf-row'));
      const setFor = (suffix, action) => {
        const row = rows.find((r) => ((r.querySelector('.cf-name')?.textContent) || '').trim().endsWith(suffix));
        const sel = row?.querySelector('select.cf-action');
        if (!sel) throw new Error(`no decision select for ${suffix}`);
        sel.value = action;
        sel.dispatchEvent(new Event('change', { bubbles: true }));
      };
      setFor('a.txt', 'skip');
      setFor('b.txt', 'rename');
      return true;
    });
    await clickFooter(/^start$/i);
    await waitFor(async () => (await sideKeys()).includes('fresh.txt'), 45000, 'clean file downloaded');
    need((await readFile(path.join(dst, 'a.txt'))).toString() === 'DLCF-A-LOCAL-KEEP\n', 'skip overwrote the local a.txt');
    need((await readFile(path.join(dst, 'b.txt'))).toString() === 'DLCF-B-LOCAL-KEEP\n', 'rename modified the local b.txt');
    const names = await sideKeys();
    const twin = names.find((n) => n.startsWith('b') && n !== 'b.txt');
    need(twin, `no renamed twin landed: ${names.join(', ')}`);
    need((await readFile(path.join(dst, twin))).toString() === 'DLCF-B-REMOTE\n', 'the twin does not carry the remote bytes');
    need((await readFile(path.join(dst, 'fresh.txt'))).toString() === 'DLCF-FRESH-REMOTE\n', 'clean file bytes differ');
    return `skip kept local a.txt; rename landed "${twin}"`;
  });

  // ---- round-3 rows: selection, multi-delete, versions deep, migrator,
  // admin mutations, L2 execution, mid-batch cancel ----

  await verify({ id: 'GUI-28', area: 'gui', action: 'Selection mechanics: plain anchor, shift-range, ctrl-toggle, invert, select-all', ds: 'S3 (MinIO)', scenario: 'a six-file folder: a plain click anchors row 1 (1 of 6 on the status bar); a shift-click on row 3 selects exactly the range (3 of 6); ctrl-click drops the middle member (2 of 6); Ctrl+I flips to the exact complement (4 of 6); Ctrl+A finally selects everything (6 of 6) — every count read from the visible selection bar. The plain click MUST come first: clicking a row that is already selected keeps the selection (drag-friendly semantics), so select-all has to end the ladder', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/selmech`;
    await s3(['mkdir', `${P}/`]);
    for (let i = 1; i <= 6; i++) await s3(['cp', path.join(FIX, 'data', 'root-1.txt'), `${P}/f${i}.txt`]);
    await navCertGui();
    await enterFolder('verify-gui');
    await enterFolder('selmech');
    const statusSel = () => evalPage(() => document.getElementById('status-selection')?.textContent || '');
    await waitFor(async () => (await rowKeys()).filter((k) => k.includes('.txt')).length >= 6, 20000, 'six rows');
    const selIs = async (n) => {
      await waitFor(async () => new RegExp(`${n} of 6`).test(await statusSel()), 4000, `status bar to read "${n} of 6"`);
    };
    await clickRow('f1.txt');
    await selIs(1);
    await (await rowAction('f3.txt')).click({ modifiers: ['Shift'] });
    await selIs(3);
    await ctrlClickRow('f2.txt');
    await selIs(2);
    await page.keyboard.press('Control+i');
    await selIs(4);
    await page.keyboard.press('Control+a');
    await selIs(6);
    await shot('28-selection');
    return 'plain anchor 1; shift-range 3; ctrl-toggle 2; invert 4; select-all 6/6 — counted on the status bar';
  });

  await verify({ id: 'GUI-29', area: 'gates', action: 'Multi-delete ladder: markers keep history; Shift+Del destroys permanently', ds: 'S3 (MinIO)', scenario: 'four objects selected at once: the Delete Window counts all four AND offers all three delete types; the plain marker delete hides every object while each full timeline survives — CLI re-reads the data version AND its delete marker per object; a second batch through Shift+Del (the permanent preset) destroys versions entirely — nothing left in any timeline', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/mdel`;
    await s3(['mkdir', `${P}/`]);
    for (const n of ['a', 'b', 'c', 'd']) await s3(['cp', path.join(FIX, 'data', 'root-1.txt'), `${P}/${n}.txt`]);
    await navCertGui();
    await enterFolder('verify-gui');
    await enterFolder('mdel');
    await waitFor(async () => (await rowKeys()).filter((k) => k.includes('.txt')).length >= 4, 20000, 'mdel rows');
    await page.keyboard.press('Control+a');
    await page.keyboard.press('Delete');
    await waitFor(async () => /object/.test(await modalText())
      && (await evalPage(() => document.querySelectorAll('#modal-root .delw-mode').length)) === 3, 8000, 'Delete Window with all three delete types');
    need(/4 object/.test(await modalText()), `window did not count the four objects: ${await modalText()}`);
    await shot('29-multi-delete-window');
    await clickFooter(/^delete$/i);
    await waitFor(async () => countLines((await s3(['ls', P, '--recursive', '--json'])).out, '"key"') === 0, 20000, 'objects hidden after the marker delete');
    for (const n of ['a', 'b', 'c', 'd']) {
      const v = await s3(['versions', 'ls', `${P}/${n}.txt`, '--json']);
      // parse the JSON (indented output puts every field on its own line,
      // so line-contains checks silently miss fields on neighboring lines)
      let vers = [];
      try { vers = JSON.parse(v.out || '[]') || []; } catch { /* asserted below */ }
      // the surviving timeline is the data version PLUS its delete marker
      need(vers.length === 2, `marker delete changed ${n}'s history: ${JSON.stringify(vers).slice(0, 160)}`);
      need(vers.some((x) => x.isDeleteMarker === true), `no delete marker recorded for ${n}: ${JSON.stringify(vers).slice(0, 160)}`);
    }
    // second batch: Shift+Del = the permanent preset — versions must NOT survive
    for (const n of ['e', 'f', 'g']) await s3(['cp', path.join(FIX, 'data', 'root-2.txt'), `${P}/${n}.txt`]);
    await refresh();
    await waitFor(async () => (await rowKeys()).filter((k) => k.includes('.txt')).length >= 3, 20000, 'second batch rows');
    await page.keyboard.press('Control+a');
    await page.keyboard.press('Shift+Delete');
    await waitFor(async () => (await evalPage(() => {
      const r = document.querySelector('#modal-root input[value="permanent"]');
      return r ? r.checked : false;
    })), 8000, 'permanent preset radio checked');
    await clickFooter(/^delete$/i);
    await waitFor(async () => countLines((await s3(['ls', P, '--recursive', '--json'])).out, '"key"') === 0, 20000, 'all gone after the permanent delete');
    for (const n of ['e', 'f', 'g']) {
      const v = await s3(['versions', 'ls', `${P}/${n}.txt`, '--json']);
      need(countLines(v.out, '"versionId"') === 0, `permanent delete left history for ${n}: ${v.out}`);
    }
    return 'marker multi-delete kept every timeline; Shift+Del destroyed the second batch entirely';
  });

  await verify({ id: 'GUI-30', area: 'versions', action: 'Versions dialog: A/B compare diff + per-version Destroy', ds: 'S3 (MinIO)', scenario: 'three CLI-seeded text versions: the timeline lists all three; picking the OLDEST as A and the NEWEST as B and comparing shows BOTH sides\' changed lines in the diff; destroying the oldest through its confirmation window drops the timeline to exactly two — re-read by CLI', face: 'GUI' }, async () => {
    const K = `s3://${BUCKET}/verify-gui/deepver.txt`;
    await writeFile(path.join(ART, 'deepver-v1.txt'), 'deepver alpha one\n');
    await writeFile(path.join(ART, 'deepver-v2.txt'), 'deepver beta two\n');
    await writeFile(path.join(ART, 'deepver-v3.txt'), 'deepver gamma three\n');
    await s3(['cp', path.join(ART, 'deepver-v1.txt'), K]);
    await s3(['cp', path.join(ART, 'deepver-v2.txt'), K, '--force']);
    await s3(['cp', path.join(ART, 'deepver-v3.txt'), K, '--force']);
    const j = await s3(['versions', 'ls', K, '--json']);
    const ids = [...j.out.matchAll(/"versionId":\s*"([^"]+)"/g)].map((m) => m[1]);
    need(ids.length === 3, `seeded timeline: ${j.out}`);
    const oldest = ids[ids.length - 1]; // versions ls lists newest first
    const newest = ids[0];
    const clickVerBtn = (id, label) => evalPage(([vid, lbl]) => {
      const row = Array.from(document.querySelectorAll('#modal-root .ver-row'))
        .find((r) => (r.textContent || '').includes(vid.slice(-6)));
      if (!row) return false;
      const b = Array.from(row.querySelectorAll('button')).find((x) => (x.textContent || '').trim() === lbl);
      if (!b) return false;
      b.click();
      return true;
    }, [id, label]);
    const openVersions = async (n = 3) => {
      await rightClickRow('deepver.txt');
      await ctxItem(/^versions/i);
      await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root .ver-row').length)) === n, 15000, `${n} version rows`);
    };
    await navCertGui();
    await enterFolder('verify-gui');
    await waitFor(async () => { await refresh(); return (await rowKeys()).some((k) => k.includes('deepver.txt')); }, 15000, 'deepver row');
    await openVersions();
    await shot('30-versions-ab');
    await waitFor(() => clickVerBtn(oldest, 'A'), 5000, 'pick A (oldest)');
    await sleep(200);
    await waitFor(() => clickVerBtn(newest, 'B'), 5000, 'pick B (newest)');
    await sleep(200);
    await waitFor(() => evalPage(() => {
      const b = Array.from(document.querySelectorAll('#modal-root button')).find((x) => /compare a/i.test(x.textContent || ''));
      if (!b || b.disabled) return false;
      b.click();
      return true;
    }), 5000, 'Compare A↔B armed');
    await waitFor(async () => {
      const t = await modalText();
      return /deepver alpha one/.test(t) && /deepver gamma three/.test(t);
    }, 8000, 'the diff to show both sides');
    await closeModal();
    // the diff modal may have replaced the timeline — reopen if needed
    if (!(await evalPage(() => document.querySelectorAll('#modal-root .ver-row').length))) await openVersions();
    await waitFor(() => clickVerBtn(oldest, 'Destroy'), 5000, 'Destroy on the oldest');
    await waitFor(async () => /permanently delete version/i.test(await modalText()), 8000, 'the destroy window');
    if (await evalPage(() => !!document.querySelector('#modal-root .delw-confirm input.input'))) {
      await page.locator('#modal-root .delw-confirm input.input').fill('delete');
    }
    await shot('30-destroy-oldest');
    await clickFooter(/^destroy$/i);
    // the delete window occupies the single modal slot, so the timeline it
    // replaced is gone once the destroy confirms — reopen and count
    await openVersions(2);
    const v = await s3(['versions', 'ls', K, '--json']);
    need(countLines(v.out, '"versionId"') === 2, `CLI disagrees after destroy: ${v.out}`);
    need(!v.out.includes(oldest), 'the destroyed version id still lists');
    await closeModal();
    return 'A/B diff showed both sides; destroying the oldest left exactly two versions';
  });

  await verify({ id: 'GUI-31', area: 'transfers', action: 'Versioned copy: the full timeline S3→S3 (the migrator)', ds: 'S3 (MinIO)', scenario: 'an object with THREE versions pasted into a fresh versioned bucket: the version-choice dialog appears (the destination is versioned), the GUI carries the ENTIRE timeline across in a background job — the destination lists all three versions and the current bytes round-trip', face: 'GUI' }, async () => {
    const MIG = `${BUCKET}-mig`;
    let r = await s3(['mb', `s3://${MIG}`]);
    need(r.code === 0, `mb mig: ${r.out}${r.err}`);
    await s3(['bucket', 'versioning', `s3://${MIG}`, 'on']);
    const K = `s3://${BUCKET}/verify-gui/mighist.txt`;
    await writeFile(path.join(ART, 'mig-v1.txt'), 'mig one\n');
    await writeFile(path.join(ART, 'mig-v2.txt'), 'mig two\n');
    await writeFile(path.join(ART, 'mig-v3.txt'), 'mig three\n');
    await s3(['cp', path.join(ART, 'mig-v1.txt'), K]);
    await s3(['cp', path.join(ART, 'mig-v2.txt'), K, '--force']);
    await s3(['cp', path.join(ART, 'mig-v3.txt'), K, '--force']);
    const nv = async (uri) => countLines((await s3(['versions', 'ls', uri, '--json'])).out, '"versionId"');
    need(await nv(K) === 3, 'seed timeline');
    await navCertGui();
    await waitFor(async () => { await refresh(); return (await rowKeys()).some((k) => k.includes('mighist.txt')); }, 15000, 'mighist row');
    await clickRow('mighist.txt');
    await page.keyboard.press('Control+c');
    await sleep(600); // the hidden Explorer staging job also starts — irrelevant here
    await goBuckets();
    // dozens of leftover buckets out-scroll the virtualized grid — filter
    // the list so the target row materializes, then open the bucket
    await filterTo(MIG);
    await waitFor(async () => (await visKeys()).some((k) => k === MIG), 8000, 'the mig bucket row');
    await dblClickRow(MIG);
    await clearFilter();
    await waitFor(async () => (await txt('#breadcrumb')).includes(MIG), 10000, 'inside the mig bucket');
    // Ctrl+V is swallowed while focus sits in the filter input (wireKeys
    // ignores in-input keys) — hand focus back to the page body first
    await evalPage(() => { document.getElementById('filter')?.blur(); });
    await page.keyboard.press('Control+v');
    await waitFor(async () => /copy s3/i.test(await modalText()), 8000, 'the version-choice dialog');
    await evalPage(() => {
      const c = document.querySelector('#modal-root .vcv-row input[type="checkbox"]');
      if (c) c.checked = true;
      return true;
    });
    await shot('31-versioned-copy-dialog');
    await clickFooter(/^copy$/i);
    const DK = `s3://${MIG}/mighist.txt`;
    await waitFor(async () => await nv(DK) === 3, 60000, 'all three versions on the destination');
    const dl = path.join(ART, 'mig-dl.txt');
    await s3(['cp', DK, dl]);
    need((await readFile(dl)).toString() === 'mig three\n', 'destination current bytes differ');
    await s3(['rb', `s3://${MIG}`, '--force']);
    return 'version-choice dialog + background job carried the whole 3-version timeline across buckets';
  });

  await verify({ id: 'GUI-32', area: 'admin', action: 'Admin panel mutations: versioning toggle + tags', ds: 'S3 (MinIO)', scenario: 'a scratch bucket driven ENTIRELY through the Admin panel: Overview suspends versioning (CLI reads it back suspended), re-enables it (CLI reads enabled); Tags saves a pair (CLI reads it back) then deletes all — every GUI mutation verified out-of-band', face: 'GUI' }, async () => {
    const AB = `${BUCKET}-adm`;
    let r = await s3(['mb', `s3://${AB}`]);
    need(r.code === 0, `mb: ${r.out}${r.err}`);
    await s3(['bucket', 'versioning', `s3://${AB}`, 'on']);
    await navCertGui();
    await goBuckets();
    // the unfiltered grid virtualizes: the adm bucket sorts far down the
    // leftover-bucket list and never renders — filter it into view
    await filterTo(AB);
    await waitFor(async () => (await visKeys()).some((k) => k === AB), 8000, 'the adm bucket row');
    await rightClickRow(AB);
    await ctxItem(/admin panel/i);
    await waitFor(async () => /admin panel/i.test(await modalText()), 10000, 'the admin panel');
    const clickBtn = (re) => evalPage((src) => {
      const b = Array.from(document.querySelectorAll('#modal-root button'))
        .find((x) => new RegExp(src, 'i').test((x.textContent || '').trim()));
      if (!b) return false;
      b.click();
      return true;
    }, re);
    await waitFor(() => clickBtn('^suspend versioning$'), 5000, 'Suspend versioning button');
    await waitFor(async () => /suspended/i.test((await s3(['bucket', 'info', `s3://${AB}`])).out), 20000, 'CLI to read suspended');
    await waitFor(() => clickBtn('^enable versioning$'), 5000, 'Enable versioning button');
    await waitFor(async () => {
      const t = (await s3(['bucket', 'info', `s3://${AB}`])).out;
      return /enabled/i.test(t) && !/suspended/i.test(t);
    }, 20000, 'CLI to read enabled');
    await page.locator('#modal-root .tab[data-tab="Tags"]').click();
    await sleep(400);
    await waitFor(() => clickBtn('\\+ add tag'), 5000, 'Add tag');
    await sleep(200);
    await evalPage(() => {
      const row = document.querySelector('#modal-root .tag-row');
      if (!row) return false;
      const [k, v] = row.querySelectorAll('input');
      k.value = 'roundtrip';
      k.dispatchEvent(new Event('change', { bubbles: true }));
      v.value = 'gui';
      v.dispatchEvent(new Event('change', { bubbles: true }));
      return true;
    });
    await waitFor(() => clickBtn('^save$'), 5000, 'Tags Save');
    await waitFor(async () => (await s3(['bucket', 'tags', 'get', `s3://${AB}`])).out.includes('roundtrip'), 20000, 'CLI to read the tag back');
    await waitFor(() => clickBtn('delete all'), 5000, 'Tags Delete all');
    await waitFor(async () => !(await s3(['bucket', 'tags', 'get', `s3://${AB}`])).out.includes('roundtrip'), 20000, 'the tag to be gone');
    await closeModal();
    await clearFilter();
    await shot('32-admin-mutations');
    await s3(['rb', `s3://${AB}`, '--force']);
    return 'versioning suspend/enable + tags put/delete landed server-side, CLI-verified';
  });

  await verify({ id: 'GUI-33', area: 'gates', action: 'L2 execution: Empty bucket destroys EVERY version, keeps the bucket', ds: 'S3 (MinIO)', scenario: 'a scratch bucket seeded with nested objects, extra old versions and delete markers: typing the bucket\'s own name arms the Empty-bucket window and EXECUTES — afterwards nothing lists anywhere (recursive listing empty, version statistics all zero, sampled timelines empty), yet the bucket itself survives and still takes writes', face: 'GUI' }, async () => {
    const EB = `${BUCKET}-l2x`;
    let r = await s3(['mb', `s3://${EB}`]);
    need(r.code === 0, `mb: ${r.out}${r.err}`);
    await s3(['bucket', 'versioning', `s3://${EB}`, 'on']);
    for (const d of ['x', 'y']) await s3(['mkdir', `s3://${EB}/${d}/`]);
    for (let i = 1; i <= 12; i++) await s3(['cp', path.join(FIX, 'data', 'root-1.txt'), `s3://${EB}/${i < 7 ? 'x' : 'y'}/l${i}.txt`]);
    for (let i = 1; i <= 6; i++) await s3(['cp', path.join(FIX, 'data', 'root-2.txt'), `s3://${EB}/x/l${i}.txt`, '--force']); // old versions
    for (let i = 7; i <= 8; i++) await s3(['rm', `s3://${EB}/y/l${i}.txt`]); // delete markers
    let st = JSON.parse((await s3(['versions', 'stat', `s3://${EB}`, '--json'])).out || '{}');
    need((st.versions || 0) >= 20 && (st.deleteMarkers || 0) >= 2, `seed stats: ${JSON.stringify(st)}`);
    await navCertGui();
    await goBuckets();
    // same virtualized-grid blindness as GUI-32 — filter the bucket into view
    await filterTo(EB);
    await waitFor(async () => (await visKeys()).some((k) => k === EB), 8000, 'the l2x bucket row');
    await rightClickRow(EB);
    await ctxItem(/admin panel/i);
    await waitFor(async () => /admin panel/i.test(await modalText()), 10000, 'the admin panel');
    await page.locator('#modal-root .tab[data-tab="Versions"]').click();
    await waitFor(async () => /empty bucket \(all versions\)/i.test(await modalText()), 8000, 'the Empty-bucket tool');
    await shot('33-l2-execute');
    // click-and-verify in one poll: re-click until the Delete Window's typed
    // partition is live (a silently-lost click left later steps probing the
    // admin panel's own inputs), and fail with forensics if it never opens
    try {
      await waitFor(() => evalPage(() => {
        if (document.querySelector('#modal-root .modal .delw-confirm')) return true;
        const b = Array.from(document.querySelectorAll('#modal-root button'))
          .find((x) => /empty bucket \(all versions\)/i.test(x.textContent || ''));
        if (b) b.click();
        return false;
      }), 8000, 'the Empty-bucket Delete Window to open');
    } catch (e) {
      await shot('33-window-fail');
      const dump = await evalPage(() => (document.querySelector('#modal-root .modal')?.textContent || '').replace(/\s+/g, ' ').slice(0, 300));
      throw new Error(`${e.message} — modal: ${dump}`);
    }
    // the typed partition is the Delete Window's fingerprint; a bare
    // input.input probe also matches admin-panel inputs in other tabs
    const typedInput = page.locator('#modal-root .modal .delw-confirm input.input');
    await typedInput.fill(EB);
    try {
      await waitFor(async () => (await evalPage(() => {
        const b = Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
          .find((x) => /empty bucket/i.test(x.textContent.trim()));
        return b === undefined ? null : !b.disabled;
      })) === true, 8000, 'the button armed with the bucket name');
    } catch (e) {
      await shot('33-arming-fail');
      const dump = await evalPage(() => ({
        typed: document.querySelector('#modal-root .modal .delw-confirm input')?.value ?? null,
        foot: Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
          .map((b) => `${b.textContent.trim()}=${b.disabled ? 'disabled' : 'armed'}`),
      }));
      throw new Error(`${e.message} — ${JSON.stringify(dump)}`);
    }
    await evalPage(() => {
      const b = Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
        .find((x) => /empty bucket/i.test(x.textContent.trim()));
      if (b && !b.disabled) { b.click(); return true; }
      return false;
    });
    await waitFor(async () => {
      const j = JSON.parse((await s3(['versions', 'stat', `s3://${EB}`, '--json'])).out || '{"versions":1}');
      return (j.versions || 0) === 0 && (j.deleteMarkers || 0) === 0 && (j.currentObjects || 0) === 0;
    }, 90000, 'all version statistics to reach zero');
    need(countLines((await s3(['ls', `s3://${EB}`, '--recursive', '--json'])).out, '"key"') === 0, 'objects survived the empty');
    for (const k of ['x/l1.txt', 'y/l7.txt']) {
      const v = await s3(['versions', 'ls', `s3://${EB}/${k}`, '--json']);
      need(countLines(v.out, '"versionId"') === 0, `a timeline survived for ${k}: ${v.out}`);
    }
    r = await s3(['stat', `s3://${EB}`]);
    need(/region|created|size/i.test(r.out), `the bucket itself was destroyed: ${r.out}`);
    await writeFile(path.join(ART, 'l2x-after.txt'), 'still writable\n');
    r = await s3(['cp', path.join(ART, 'l2x-after.txt'), `s3://${EB}/after.txt`]);
    need(r.code === 0, `the emptied bucket no longer takes writes: ${r.out}${r.err}`);
    await closeModal();
    await clearFilter();
    await s3(['rb', `s3://${EB}`, '--force']);
    return 'executed: every version and marker destroyed; the bucket survives and takes writes';
  });

  await verify({ id: 'GUI-34', area: 'transfers', action: 'Cancel mid-batch: finished files stay, the canceled one never lands', ds: 'S3 (MinIO)', scenario: 'five 2 MiB files drag-dropped as ONE batch job under a 128 kB/s app throttle (files process sequentially): the job is canceled only once its first file has fully landed — exactly the finished file(s) exist remotely afterwards (byte-identical, CLI-verified), while the in-flight and still-queued ones are ABSENT with no partial left behind (S3 materializes nothing until an upload completes)', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/cancel2`;
    await s3(['mkdir', `${P}/`]);
    const src = path.join(ART, 'cancel2');
    await rm(src, { recursive: true, force: true });
    await mkdir(src, { recursive: true });
    const names = [];
    const shas = {};
    for (let i = 1; i <= 5; i++) {
      const n = `cb${i}.bin`;
      const f = path.join(src, n);
      await writeFile(f, randomBytes(2 * 1024 * 1024));
      names.push(n);
      shas[n] = await sha256file(f);
    }
    await evalPage(() => { localStorage.setItem('s3b-throttle', '131072'); localStorage.setItem('s3b-show-throttle', '1'); return true; });
    // cascade insurance: a failed prior row can leave its modal open and the
    // filter set — best-effort no-ops on a clean UI (Cancel never fires the
    // destructive path)
    try { await closeModal(); await clearFilter(); } catch { /* best effort */ }
    await navCertGui();
    await enterFolder('verify-gui');
    await enterFolder('cancel2');
    await ensureDualPane();
    await localDir(src, 'cb1.bin');
    await waitFor(async () => (await sideKeys()).filter((k) => k.startsWith('cb')).length === 5, 10000, 'five local rows');
    await (await sideRow('cb1.bin')).click();
    for (let i = 2; i <= 5; i++) await (await sideRow(`cb${i}.bin`)).click({ modifiers: ['Control'] });
    await dnd(await sideRow('cb3.bin'), await bodyH());
    await startIfAsked(5000);
    await shot('34-batch-running');
    // the manager popout is where job rows live — open it explicitly instead
    // of relying on whatever earlier rows left behind
    await menuClick(/view/i, /transfers/i);
    await waitFor(() => evalPage(() => !!document.querySelector('#popout-root .popout')), 5000, 'transfer manager popout');
    // one job must be visibly mid-flight AND past its first file before
    // anything is canceled: the batch is ONE job that processes files
    // sequentially, so canceling during file 1 lands NOTHING (run 2: 7%
    // into cb1, 0/5 files done → zero objects). "Cancel mid-batch keeps
    // finished files" is only observable between files.
    let doneAtCancel = 0;
    await waitFor(async () => {
      const j = ((await call('ActiveTransfers')) || []).find((x) => x.status === 'running' && !x.hidden);
      if (j && (j.doneFiles || 0) >= 1 && (j.doneFiles || 0) < (j.totalFiles || 5)) {
        doneAtCancel = j.doneFiles;
        return true;
      }
      return false;
    }, 300000, 'a running job past its first file');
    // the manager re-renders its rows on every progress tick, so find AND
    // click the button in the same in-page turn, retried until it lands
    await waitFor(async () => evalPage(() => {
      const b = Array.from(document.querySelectorAll('.tr-job.running button'))
        .find((x) => /cancel/i.test(x.textContent || ''));
      if (!b) return false;
      b.click();
      return true;
    }), 15000, 'Cancel on the running job');
    await waitFor(async () => JSON.stringify(await call('ActiveTransfers')).includes('canceled'), 30000, 'a canceled job');
    await waitFor(async () => ((await call('ActiveTransfers')) || []).every((j) => j.status !== 'running'), 120000, 'all jobs to settle');
    await evalPage(() => { localStorage.removeItem('s3b-throttle'); localStorage.removeItem('s3b-show-throttle'); return true; });
    const l = await s3(['ls', P, '--recursive', '--json']);
    const landed = names.filter((n) => l.out.includes(n));
    // exactly the files that had FINISHED when the cancel landed stay; a
    // 2 MiB file takes ~85 s under the throttle, so none can complete in
    // the ~100 ms between reading doneFiles and the click landing
    need(landed.length === doneAtCancel && landed.length >= 1,
      `expected exactly ${doneAtCancel} finished file(s) to stay, got: ${landed.join(',') || 'none'}`);
    const missing = names.filter((n) => !landed.includes(n));
    need(missing.length === names.length - doneAtCancel, 'the canceled/queued files were counted wrong');
    for (const n of landed) {
      const f = path.join(ART, `cb-dl-${n}`);
      await s3(['cp', `${P}/${n}`, f]);
      need(await sha256file(f) === shas[n], `${n} bytes differ after the batch`);
    }
    for (const n of missing) {
      const s = await s3(['stat', `${P}/${n}`]);
      need(s.code !== 0, `the canceled/queued file ${n} landed (partial or whole)`);
    }
    return `canceled mid-batch: ${missing.length} file(s) never landed, the ${landed.length} finished one(s) are byte-identical`;
  });

  await verify({ id: 'GUI-35', area: 'admin', action: 'Admin panel: CORS tab round-trip', ds: 'S3 (MinIO)', scenario: 'a rule authored in the CORS tab (+ Add rule, comma lists, max-age) saves through the panel and the CLI reads every field back; Delete all clears it — the panel is the writer, the CLI the oracle', face: 'GUI' }, async () => {
    const AB = `${BUCKET}-gcors`;
    let r = await s3(['mb', `s3://${AB}`]);
    need(r.code === 0, `mb: ${r.out}${r.err}`);
    await navCertGui();
    await goBuckets();
    await filterTo(AB);
    await waitFor(async () => (await visKeys()).some((k) => k === AB), 8000, 'the gcors bucket row');
    await rightClickRow(AB);
    await ctxItem(/admin panel/i);
    await waitFor(async () => /admin panel/i.test(await modalText()), 10000, 'the admin panel');
    const clickBtn = (re) => evalPage((src) => {
      const b = Array.from(document.querySelectorAll('#modal-root button'))
        .find((x) => new RegExp(src, 'i').test((x.textContent || '').trim()));
      if (!b) return false;
      b.click();
      return true;
    }, re);
    await page.locator('#modal-root .tab[data-tab="CORS"]').click();
    // the pane renders only after GetBucketAdmin resolves (drawTab no-ops
    // while panel is null) — wait that out, then read what actually drew:
    // on providers without the CORS API the pane is an error banner and
    // there is no editor to drive at all
    await waitFor(async () => {
      const t = await evalPage(() => document.querySelector('#modal-root .tabbody')?.textContent || '');
      return t && !/Loading/.test(t);
    }, 15000, 'the CORS pane to draw');
    const paneErr = await evalPage(() => document.querySelector('#modal-root .tabbody .banner.warn')?.textContent || '');
    if (/not supported/i.test(paneErr)) {
      // out-of-band CLI probe decides provider gap vs GUI bug
      const f = path.join(ART, 'gui-cors.json');
      await writeFile(f, JSON.stringify([{ origins: ['https://verify-gui.example.com'], methods: ['GET'], headers: [], expose: [], maxAge: 60 }]));
      const put = await s3(['bucket', 'cors', 'put', `s3://${AB}`, f]);
      await closeModal();
      await clearFilter();
      await s3(['rb', `s3://${AB}`, '--force']);
      need(put.code !== 0, `CORS put unexpectedly succeeded where the panel said unsupported: ${put.out}`);
      return skip(`provider refused the CORS API (recorded gap): pane "${paneErr.trim().slice(0, 80)}"; CLI put: ${(put.out + put.err).trim().slice(0, 80)}`);
    }
    need(!paneErr, `the CORS pane opened with an error: ${paneErr.trim().slice(0, 160)}`);
    await waitFor(() => clickBtn('\\+ add rule'), 5000, 'Add rule');
    await sleep(200);
    const filled = await evalPage(() => {
      const card = document.querySelector('#modal-root .rule-card');
      if (!card) return false;
      const inputs = Array.from(card.querySelectorAll('input'));
      if (inputs.length < 5) return false;
      const set = (i, v) => { inputs[i].value = v; inputs[i].dispatchEvent(new Event('change', { bubbles: true })); };
      set(0, 'https://verify-gui.example.com');
      set(1, 'GET, PUT');
      set(2, 'content-type');
      set(3, 'ETag');
      set(4, '1800');
      return true;
    });
    need(filled, 'the CORS rule card did not expose its five inputs');
    await waitFor(() => clickBtn('^save$'), 5000, 'CORS Save');
    // out-of-band oracle; if it never lands, decide provider-gap vs GUI bug
    // with a direct CLI put of the same rule
    let echoed = false;
    for (let i = 0; i < 40 && !echoed; i++) {
      echoed = (await s3(['bucket', 'cors', 'get', `s3://${AB}`, '--json'])).out.includes('verify-gui.example.com');
      if (!echoed) await sleep(500);
    }
    if (!echoed) {
      const f = path.join(ART, 'gui-cors.json');
      await writeFile(f, JSON.stringify([{ origins: ['https://verify-gui.example.com'], methods: ['GET'], headers: [], expose: [], maxAge: 60 }]));
      const put = await s3(['bucket', 'cors', 'put', `s3://${AB}`, f]);
      await closeModal();
      await clearFilter();
      await s3(['rb', `s3://${AB}`, '--force']);
      if (put.code !== 0 && /not supported|not implemented|malformed|invalid/i.test(put.out + put.err)) {
        return skip(`provider refused CORS on this build (recorded gap): ${(put.out + put.err).trim().slice(0, 120)}`);
      }
      need(false, `the panel Save did not land a rule the CLI can read (direct CLI put exited ${put.code})`);
    }
    const g = await s3(['bucket', 'cors', 'get', `s3://${AB}`, '--json']);
    for (const needle of ['verify-gui.example.com', 'PUT', '1800']) {
      need(g.out.includes(needle), `CLI cors get lost "${needle}": ${g.out}`);
    }
    await waitFor(() => clickBtn('delete all'), 5000, 'CORS Delete all');
    await waitFor(async () => !(await s3(['bucket', 'cors', 'get', `s3://${AB}`, '--json'])).out.includes('verify-gui.example.com'), 20000, 'the rule to be gone');
    await closeModal();
    await clearFilter();
    await shot('35-admin-cors');
    await s3(['rb', `s3://${AB}`, '--force']);
    return 'CORS rule authored in the panel, CLI-verified, deleted';
  });

  await verify({ id: 'GUI-36', area: 'admin', action: 'Admin panel: Website tab round-trip', ds: 'S3 (MinIO)', scenario: 'index + error documents saved from the Website tab are read back by the CLI verbatim; Disable clears the config — hosting on/off proven out-of-band', face: 'GUI' }, async () => {
    const AB = `${BUCKET}-gweb`;
    let r = await s3(['mb', `s3://${AB}`]);
    need(r.code === 0, `mb: ${r.out}${r.err}`);
    await navCertGui();
    await goBuckets();
    await filterTo(AB);
    await waitFor(async () => (await visKeys()).some((k) => k === AB), 8000, 'the gweb bucket row');
    await rightClickRow(AB);
    await ctxItem(/admin panel/i);
    await waitFor(async () => /admin panel/i.test(await modalText()), 10000, 'the admin panel');
    const clickBtn = (re) => evalPage((src) => {
      const b = Array.from(document.querySelectorAll('#modal-root button'))
        .find((x) => new RegExp(src, 'i').test((x.textContent || '').trim()));
      if (!b) return false;
      b.click();
      return true;
    }, re);
    await page.locator('#modal-root .tab[data-tab="Website"]').click();
    // same settle + provider-gap read as GUI-35: a pane that drew as an
    // error banner has no inputs to drive
    await waitFor(async () => {
      const t = await evalPage(() => document.querySelector('#modal-root .tabbody')?.textContent || '');
      return t && !/Loading/.test(t);
    }, 15000, 'the Website pane to draw');
    const paneErr = await evalPage(() => document.querySelector('#modal-root .tabbody .banner.warn')?.textContent || '');
    if (/not supported/i.test(paneErr)) {
      const put = await s3(['bucket', 'website', 'put', `s3://${AB}`, '--index', 'gui-index.html', '--error', 'gui-404.html']);
      await closeModal();
      await clearFilter();
      await s3(['rb', `s3://${AB}`, '--force']);
      need(put.code !== 0, `website put unexpectedly succeeded where the panel said unsupported: ${put.out}`);
      return skip(`provider refused the website API (recorded gap): pane "${paneErr.trim().slice(0, 80)}"; CLI put: ${(put.out + put.err).trim().slice(0, 80)}`);
    }
    need(!paneErr, `the Website pane opened with an error: ${paneErr.trim().slice(0, 160)}`);
    const filled = await evalPage(() => {
      const err = document.querySelector('#modal-root input[placeholder="404.html"]');
      if (!err) return false;
      const inputs = Array.from(document.querySelectorAll('#modal-root .input.mono'));
      const index = inputs[inputs.indexOf(err) - 1];
      if (!index) return false;
      // the website tab binds at Save-click time — plain value writes
      index.value = 'gui-index.html';
      err.value = 'gui-404.html';
      return true;
    });
    need(filled, 'the Website tab did not expose its index/error inputs');
    await waitFor(() => clickBtn('^save$'), 5000, 'Website Save');
    let ok = false;
    for (let i = 0; i < 40 && !ok; i++) {
      const g = await s3(['bucket', 'website', 'get', `s3://${AB}`, '--json']);
      ok = g.out.includes('gui-index.html') && g.out.includes('gui-404.html');
      if (!ok) await sleep(500);
    }
    if (!ok) {
      const put = await s3(['bucket', 'website', 'put', `s3://${AB}`, '--index', 'gui-index.html', '--error', 'gui-404.html']);
      await closeModal();
      await clearFilter();
      await s3(['rb', `s3://${AB}`, '--force']);
      if (put.code !== 0 && /not supported|not implemented|malformed|invalid/i.test(put.out + put.err)) {
        return skip(`provider refused website config on this build (recorded gap): ${(put.out + put.err).trim().slice(0, 120)}`);
      }
      need(false, `the panel Website Save did not land (direct CLI put exited ${put.code})`);
    }
    await waitFor(() => clickBtn('^disable$'), 5000, 'Website Disable');
    await waitFor(async () => !(await s3(['bucket', 'website', 'get', `s3://${AB}`, '--json'])).out.includes('gui-index.html'), 20000, 'the config to be gone');
    await closeModal();
    await clearFilter();
    await shot('36-admin-website');
    await s3(['rb', `s3://${AB}`, '--force']);
    return 'website documents saved from the panel, CLI-verified, disabled';
  });

  await verify({ id: 'GUI-37', area: 'gates', action: 'Delete Window keepcurrent mode: history destroyed, current version survives', ds: 'S3 (MinIO)', scenario: 'an object with THREE versions through the keepcurrent mode: the older versions are destroyed while the CURRENT bytes stay live and identical — the window never auto-confirms (explicit mode + typed word), and CLI re-reads the collapsed timeline (exactly 1 version, zero markers) and the bytes', face: 'GUI' }, async () => {
    const K = `s3://${BUCKET}/verify-gui/gui-kc.txt`;
    // self-seeded distinct versions — the shared fixture trio only exists
    // under full gates, and an unasserted seed cp silently skips a missing
    // file (run #5: the third version never landed, the byte compare then
    // ENOENT'd against a local file that was never there)
    const kcSeeds = [1, 2, 3].map((i) => path.join(ART, `gui-kc-v${i}.txt`));
    for (let i = 0; i < kcSeeds.length; i++) {
      await writeFile(kcSeeds[i], `keepcurrent seed ${i + 1} — ${RUNID}\n`);
      const r = await s3(['cp', kcSeeds[i], K, ...(i ? ['--force'] : [])]);
      need(r.code === 0, `seed cp v${i + 1}: ${r.out}${r.err}`);
    }
    await navCertGui();
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('gui-kc.txt'));
    }, 15000, 'gui-kc row');
    await clickRow('gui-kc.txt');
    await page.keyboard.press('Delete');
    await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root input[name="delmode"]').length)) >= 3, 5000, 'delete window with 3 modes');
    await evalPage(() => { document.querySelector('#modal-root input[name="delmode"][value="keepcurrent"]')?.click(); });
    const armed = await evalPage(() => {
      const i = document.querySelector('#modal-root .delw-confirm input');
      return !!i && !i.disabled;
    });
    if (armed) await page.locator('#modal-root .delw-confirm input').fill('delete');
    await shot('37-keepcurrent-window');
    await clickFooter(/^delete$/i);
    // the current version SURVIVES — the row must still be there
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('gui-kc.txt'));
    }, 30000, 'the current version to survive');
    await waitFor(async () => {
      const out = (await s3(['versions', 'ls', K, '--json'])).out;
      return countLines(out, '"versionId"') === 1;
    }, 30000, 'the timeline to collapse to exactly 1 version');
    const v = await s3(['versions', 'ls', K, '--json']);
    need(!v.out.includes('"isDeleteMarker": true'), 'keepcurrent left a delete marker');
    const out = path.join(ART, 'gui-kc.txt');
    await s3(['cp', K, out]);
    need((await readFile(out)).equals(await readFile(kcSeeds[2])), 'the surviving current version has the wrong bytes');
    return 'history destroyed; current version byte-identical; timeline == 1';
  });

  await verify({ id: 'GUI-38', area: 'versions', action: 'Versions dialog: Undo delete removes the marker only', ds: 'S3 (MinIO)', scenario: 'an object that was marker-deleted and then written again (latest = data, a marker sits mid-timeline): Undo delete must remove THAT marker without touching the data versions — CLI re-reads the timeline (one row fewer, zero markers) and the live bytes', face: 'GUI' }, async () => {
    const K = `s3://${BUCKET}/verify-gui/gui-ud.txt`;
    await s3(['cp', path.join(FIX, 'data', 'root-1.txt'), K]);
    await s3(['rm', K]); // the marker in the middle of the timeline
    await s3(['cp', path.join(FIX, 'data', 'root-2.txt'), K, '--force']); // latest = data again
    await navCertGui();
    // the versions dialog HIDES delete-marker rows unless this setting is
    // on (dialogs.js filters isDeleteMarker) — and the marker is exactly
    // the row under test here
    await evalPage(() => { localStorage.setItem('s3b-show-markers', '1'); return true; });
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('gui-ud.txt'));
    }, 15000, 'gui-ud row');
    await rightClickRow('gui-ud.txt');
    await ctxItem(/^versions/i);
    await waitFor(async () => await page.locator('#modal-root .ver-row').count().then((n) => n >= 3), 15000, 'v1 + marker + v2 rows');
    // same-turn find-and-click: the dialog redraws after the action
    await waitFor(async () => evalPage(() => {
      const row = Array.from(document.querySelectorAll('#modal-root .ver-row'))
        .find((r) => Array.from(r.querySelectorAll('button')).some((b) => /undo delete/i.test(b.textContent || '')));
      if (!row) return false;
      Array.from(row.querySelectorAll('button')).find((b) => /undo delete/i.test(b.textContent || '')).click();
      return true;
    }), 8000, 'Undo delete on the marker row');
    await sleep(800);
    await closeModal();
    await evalPage(() => { localStorage.removeItem('s3b-show-markers'); return true; });
    await waitFor(async () => {
      const out = (await s3(['versions', 'ls', K, '--json'])).out;
      return countLines(out, '"versionId"') === 2;
    }, 30000, 'the timeline to drop to 2 (marker gone)');
    const v = await s3(['versions', 'ls', K, '--json']);
    need(!v.out.includes('"isDeleteMarker": true'), 'a marker survived the undo');
    const out = path.join(ART, 'gui-ud.txt');
    await s3(['cp', K, out]);
    need((await readFile(out)).equals(await readFile(path.join(FIX, 'data', 'root-2.txt'))), 'undo delete changed the live bytes');
    return 'marker removed; 2 data versions; live bytes untouched';
  });

  await verify({ id: 'GUI-39', area: 'transfers', action: 'Transfer-manager hygiene: ClearFinishedTransfers', ds: 'S3 (MinIO)', scenario: 'after a bridge-driven upload completes, the finished job must be prunable by ID (ClearFinishedTransfers([id]) — an empty/absent list is a no-op by contract: null means all, named ids mean those) — the manager history is user-controllable; the upload itself is proven present by the CLI', face: 'GUI' }, async () => {
    const src = path.join(ART, 'gui-tm.txt');
    await writeFile(src, `transfer-manager probe ${RUNID}\n`);
    // clean app state first: rows above can die with dialogs raised, and
    // the bridge upload must not inherit a wedged view
    await navCertGui();
    const jobId = await call('Upload', [src], BUCKET, 'verify-gui/gui-tm/', 'skip', 0, {});
    need(typeof jobId === 'string' && jobId.length > 0, `Upload returned no job id: ${JSON.stringify(jobId)}`);
    let ended = null;
    await waitFor(async () => {
      const j = ((await call('ActiveTransfers')) || []).find((x) => x.id === jobId);
      if (!j) return false;
      ended = j;
      return ['done', 'error', 'canceled'].includes(j.status);
    }, 60000, 'the probe upload to finish');
    need(ended?.status === 'done', `the probe upload ended "${ended?.status}": ${JSON.stringify(ended)?.slice(0, 200)}`);
    need((ended?.sentBytes ?? 0) >= 35, `the upload sent no bytes — 'skip' must never drop a NON-conflicting file: ${JSON.stringify(ended)?.slice(0, 200)}`);
    await call('ClearFinishedTransfers', [jobId]);
    const after = await call('ActiveTransfers');
    need(!JSON.stringify(after).includes(jobId), 'the finished job survived ClearFinishedTransfers([id])');
    const st = await s3(['stat', `s3://${BUCKET}/verify-gui/gui-tm/gui-tm.txt`]);
    need(st.code === 0, `probe object missing: ${st.out}${st.err}`);
    // the same upload again: the destination now EXISTS, so 'skip' must
    // keep the remote file untouched — re-sending bytes would be an
    // overwrite the caller never asked for
    const j2id = await call('Upload', [src], BUCKET, 'verify-gui/gui-tm/', 'skip', 0, {});
    let ended2 = null;
    await waitFor(async () => {
      const j = ((await call('ActiveTransfers')) || []).find((x) => x.id === j2id);
      if (!j) return false;
      ended2 = j;
      return ['done', 'error', 'canceled'].includes(j.status);
    }, 60000, 'the conflicting upload to finish');
    need(ended2?.status === 'done' && (ended2?.skippedFiles ?? 0) >= 1 && (ended2?.sentBytes ?? 0) === 0,
      `the conflicting upload did not skip the existing destination: ${JSON.stringify(ended2)?.slice(0, 200)}`);
    return 'non-conflicting file uploaded under skip policy; existing one kept; finished job pruned by id';
  });

  await verify({ id: 'GUI-40', area: 'objects', action: 'Deep search: token + CancelSearch contract', ds: 'S3 (MinIO)', scenario: 'DeepSearch returns a live token and registers a search task; CancelSearch stops it without error, and canceling an UNKNOWN token must also resolve cleanly — a bogus cancel may never throw or wedge the registry', face: 'GUI' }, async () => {
    const tok = await call('DeepSearch', BUCKET, 'verify-gui/', { pattern: 'zzz-verify-none', limit: 10 });
    need(typeof tok === 'string' && tok.length > 0, `DeepSearch returned no token: ${JSON.stringify(tok)}`);
    await sleep(300);
    await call('CancelSearch', tok);
    await call('CancelSearch', 'verify-bogus-token');
    await waitFor(async () => !((await call('RunningTasks')) || []).some((t) => t.kind === 'search' && (t.status === 'running' || t.status === 'queued')), 20000, 'the search task to stop');
    return 'token issued; cancel + bogus-cancel both resolve; task stopped';
  });

  await verify({ id: 'GUI-41', area: 'sources', action: 'Credential file import over the bridge', ds: 'S3 (MinIO) via INI', scenario: 'an AWS-style INI: ParseCredentialFile finds the candidate WITH its secret, TestCredentialDraft dials the live MinIO, ImportCredentials lands the source in the GUI store — seen through ListSources over the bridge (the GUI config is isolated from the CLI face) — and RemoveSource cleans up', face: 'GUI' }, async () => {
    const f = path.join(ART, 'gui-creds.ini');
    await writeFile(f, [
      '[verifyimport]',
      'aws_access_key_id = minioadmin',
      'aws_secret_access_key = minioadmin',
      'endpoint = http://127.0.0.1:9000',
      'region = us-east-1',
      '',
    ].join('\n'));
    const cands = await call('ParseCredentialFile', f, '');
    need(Array.isArray(cands) && cands.length >= 1, `ParseCredentialFile returned nothing: ${JSON.stringify(cands)?.slice(0, 120)}`);
    const cand = cands[0];
    need((cand?.hasSecret ?? cand?.has_secret) === true, `the candidate does not carry its secret: ${JSON.stringify(cand)?.slice(0, 120)}`);
    const tr = await call('TestCredentialDraft', cand.id);
    const trj = JSON.stringify(tr) || '';
    need((tr?.bucketCount ?? 0) > 0 || /"ok"\s*:\s*(true|"ok")/.test(trj), `TestCredentialDraft did not verify against live MinIO: ${trj.slice(0, 160)}`);
    const res = await call('ImportCredentials', [cand.id]);
    const imp = (res?.imported) || [];
    need(imp.length >= 1, `ImportCredentials imported nothing: ${JSON.stringify(res)?.slice(0, 160)}`);
    const srcs = (await call('ListSources')) || [];
    const hit = srcs.find((s) => imp.includes(s.name) || /verifyimport/i.test(String(s.name)));
    need(hit, `the imported source is not visible through ListSources: ${JSON.stringify(srcs.map((s) => s.name))}`);
    await call('RemoveSource', hit.id || hit.name);
    const after = (await call('ListSources')) || [];
    need(!after.some((s) => s.name === hit.name), 'the imported source survived RemoveSource');
    return `imported "${hit.name}", live-tested, removed cleanly`;
  });

  // ---- the bridge wire spy, shared by the Copy rows (GUI-42/49/50) ----
  // Oracle: the Wails binding objects are frozen and api.js caches the App
  // object in module scope on first call, so an in-page property patch
  // cannot intercept copyAsText's api.ClipboardSetText. But every binding
  // call crosses window.fetch to the Wails server — an ordinary host
  // object. Spy fetch (+XHR/WS hedge) and assert the EXACT JSON args of
  // the ClipboardSetText call; the spy is best-effort by design and may
  // never break the app.
  const installWireSpy = () => evalPage(() => {
    window.__wireSpy = [];
    const rec = (url, body) => { try { window.__wireSpy.push({ url: String(url), body: body == null ? '' : String(body) }); } catch { /* never break the app */ } };
    window.__wireUndo = [];
    const of = window.fetch;
    if (of) {
      window.fetch = function (input, init) { rec(typeof input === 'string' ? input : (input && input.url) || '', init && init.body); return of.apply(this, arguments); };
      window.__wireUndo.push(() => { window.fetch = of; });
    }
    const ox = XMLHttpRequest.prototype.open, os = XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.open = function (m, u) { this.__url = String(u); return ox.apply(this, arguments); };
    XMLHttpRequest.prototype.send = function (b) { rec(this.__url || '', b); return os.apply(this, arguments); };
    window.__wireUndo.push(() => { XMLHttpRequest.prototype.open = ox; XMLHttpRequest.prototype.send = os; });
    try {
      const ows = WebSocket.prototype.send;
      WebSocket.prototype.send = function (d) { rec('ws:' + String(this.url), typeof d === 'string' ? d : '[binary]'); return ows.apply(this, arguments); };
      window.__wireUndo.push(() => { WebSocket.prototype.send = ows; });
    } catch { /* no WebSocket in this build */ }
    return true;
  });
  const undoWireSpy = () => evalPage(() => { (window.__wireUndo || []).forEach((u) => u()); return true; });
  // binding bodies cross fetch as {"object":N,"method":N,"args":{...}} where
  // args is EITHER the positional array or the ByName wrapper {"call-id",
  // "methodName":"<go path>","args":[...]} — the method's full Go name rides
  // along, so anchor on BOTH the method name and the exact positional args
  const waitForClipArgs = async (expected, what) => {
    const want = JSON.stringify([expected]);
    for (let i = 0; i < 10; i++) {
      const seen = await evalPage(() => window.__wireSpy
        .map((r) => { try { return JSON.parse(r.body); } catch { return null; } })
        .filter((c) => c && c.args && String(c.args.methodName || '').includes('ClipboardSetText'))
        .map((c) => (Array.isArray(c.args) ? c.args : c.args.args)));
      if (seen.some((a) => JSON.stringify(a) === want)) return;
      await sleep(300);
    }
    const dump = await evalPage(() => window.__wireSpy.slice(-15));
    need(false, `${what}: no ClipboardSetText binding call carried args ${want} — recent wire: ${JSON.stringify(dump.map((r) => ({ u: r.url.slice(0, 70), b: r.body.slice(0, 160) })))}`);
  };
  // ctxItem happily clicks a DISABLED item (a silent no-op): wait for the
  // item to be enabled so a lost selection fails with its name up
  const copyViaMenu = async (re) => {
    await waitFor(async () => await evalPage((src) => {
      const it = Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
        .find((i) => new RegExp(src, 'i').test(i.textContent || ''));
      return !!it && !it.classList.contains('disabled');
    }, re.source), 4000, `an ENABLED "${re.source}" item — the row selection was lost`);
    await ctxItem(re);
    await sleep(300);
  };
  await verify({ id: 'GUI-42', area: 'objects', action: 'Copy As: exact text shapes for name / path / S3 URI (single + multi)', ds: 'S3 (MinIO)', scenario: 'a clipboard spy on the bridge binding captures exactly what the app sends: single row → the bare name, bucket/key, s3://bucket/key; Ctrl+A multi-select → newline-joined names — the exact strings an editor, a ticket and a terminal receive', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/gui-copy`;
    await s3(['mkdir', `${P}/`]);
    for (const n of ['a.txt', 'b.txt', 'c.txt']) await s3(['cp', path.join(FIX, 'data', 'root-1.txt'), `${P}/${n}`]);
    await navCertGui();
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('gui-copy'));
    }, 15000, 'gui-copy folder');
    await enterFolder('gui-copy');
    await waitFor(async () => (await visKeys()).filter((k) => k.endsWith('.txt')).length >= 3, 15000, 'three file rows');
    // oracle: the Wails binding objects are frozen and api.js caches the
    // App object in module scope on first call, so an in-page property
    // patch cannot intercept copyAsText's api.ClipboardSetText (runs #6-
    // #8 proved it). But every binding call crosses window.fetch to the
    // Wails server — an ordinary host object. Spy fetch (+XHR/WS hedge),
    // assert the EXACT JSON args of the ClipboardSetText call, and let
    // the app's toast prove the Go side answered: the server build has
    // no desktop shell BY DESIGN, so its honest answer is the "no desktop
    // shell attached" error (the desktop build's OS-clipboard write is
    // asserted by the gui-live seam walk in the sweep rows)
    await evalPage(() => {
      window.__wireSpy = [];
      const rec = (url, body) => { try { window.__wireSpy.push({ url: String(url), body: body == null ? '' : String(body) }); } catch { /* never break the app */ } };
      window.__wireUndo = [];
      const of = window.fetch;
      if (of) {
        window.fetch = function (input, init) { rec(typeof input === 'string' ? input : (input && input.url) || '', init && init.body); return of.apply(this, arguments); };
        window.__wireUndo.push(() => { window.fetch = of; });
      }
      const ox = XMLHttpRequest.prototype.open, os = XMLHttpRequest.prototype.send;
      XMLHttpRequest.prototype.open = function (m, u) { this.__url = String(u); return ox.apply(this, arguments); };
      XMLHttpRequest.prototype.send = function (b) { rec(this.__url || '', b); return os.apply(this, arguments); };
      window.__wireUndo.push(() => { XMLHttpRequest.prototype.open = ox; XMLHttpRequest.prototype.send = os; });
      try {
        const ows = WebSocket.prototype.send;
        WebSocket.prototype.send = function (d) { rec('ws:' + String(this.url), typeof d === 'string' ? d : '[binary]'); return ows.apply(this, arguments); };
        window.__wireUndo.push(() => { WebSocket.prototype.send = ows; });
      } catch { /* no WebSocket in this build */ }
      return true;
    });
    const clipArgs = async (expected, what) => {
      // run #10's wire dump: binding bodies cross fetch as {"object":N,
      // "method":N,"args":{...}} where args is EITHER the positional array
      // or the ByName wrapper {"call-id","methodName":"<go path>","args":
      // [...]} — the method's full Go name rides along, so anchor on BOTH
      // the method name and the exact positional args
      const want = JSON.stringify([expected]);
      for (let i = 0; i < 10; i++) {
        const seen = await evalPage(() => window.__wireSpy
          .map((r) => { try { return JSON.parse(r.body); } catch { return null; } })
          .filter((c) => c && c.args && String(c.args.methodName || '').includes('ClipboardSetText'))
          .map((c) => (Array.isArray(c.args) ? c.args : c.args.args)));
        if (seen.some((a) => JSON.stringify(a) === want)) return;
        await sleep(300);
      }
      const dump = await evalPage(() => window.__wireSpy.slice(-15));
      need(false, `${what}: no ClipboardSetText binding call carried args ${want} — recent wire: ${JSON.stringify(dump.map((r) => ({ u: r.url.slice(0, 70), b: r.body.slice(0, 160) })))}`);
    };
    // ctxItem happily clicks a DISABLED item (a silent no-op): wait for
    // the item to be enabled so a lost selection fails with its name up
    const copyViaMenu = async (re) => {
      await waitFor(async () => await evalPage((src) => {
        const it = Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
          .find((i) => new RegExp(src, 'i').test(i.textContent || ''));
        return !!it && !it.classList.contains('disabled');
      }, re.source), 4000, `an ENABLED "${re.source}" item — the row selection was lost`);
      await ctxItem(re);
      await sleep(300);
    };
    try {
      await clickRow('a.txt');
      await rightClickRow('a.txt');
      await copyViaMenu(/copy name/i);
      await clipArgs('a.txt', 'copy name');
      await rightClickRow('a.txt');
      await copyViaMenu(/copy path/i);
      await clipArgs(`${BUCKET}/verify-gui/gui-copy/a.txt`, 'copy path');
      await rightClickRow('a.txt');
      await copyViaMenu(/copy s3 uri/i);
      await clipArgs(`s3://${BUCKET}/verify-gui/gui-copy/a.txt`, 'copy s3 uri');
      await page.keyboard.press('Control+a');
      await sleep(250);
      await rightClickRow('c.txt');
      await copyViaMenu(/copy name/i);
      await clipArgs('a.txt\nb.txt\nc.txt', 'multi copy name');
      // the Go side answered — the real write (a working clipboard), or
      // one of the two designed headless refusals (run #11: the wails
      // clipboard service itself fails in the server-build context)
      const toasts = await evalPage(() => Array.from(document.querySelectorAll('#toasts .toast')).map((t) => String(t.textContent)));
      need(toasts.some((t) => /^Copied 3 names/.test(t) || /^Copy failed: (no desktop shell attached|.*clipboard: write failed)/.test(t)),
        `the backend never answered the copy: toasts ${JSON.stringify(toasts)}`);
      return 'name / bucket-key / s3-uri exact at the binding boundary; multi copy newline-joined; backend answered';
    } finally {
      await evalPage(() => { (window.__wireUndo || []).forEach((u) => u()); return true; });
    }
  });

  await verify({ id: 'GUI-43', area: 'gates', action: 'Exit gates: busy work survives ExitApp and the close request', ds: 'S3 (MinIO)', scenario: 'with a throttled transfer CONFIRMED running: the close request is vetoed with the transfer named as the reason, ExitApp only asks — the app stays fully alive (/health, GetVersion and the running job all still answer) — and the confirmation is dismissed, never force-confirmed; the transfer is then canceled normally and the throttle restored', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/exitgate`;
    await s3(['mkdir', `${P}/`]);
    // a dedicated source dir: ART itself holds far too many entries for
    // the local grid's virtualized viewport, and the drag source row must
    // actually render to be grabbed
    const egdir = path.join(ART, 'exitgate-src');
    await rm(egdir, { recursive: true, force: true });
    await mkdir(egdir, { recursive: true });
    const src = path.join(egdir, 'exit-8m.bin');
    await writeFile(src, randomBytes(8 * 1024 * 1024));
    await evalPage(() => { localStorage.setItem('s3b-throttle', '262144'); localStorage.setItem('s3b-show-throttle', '1'); return true; });
    await navCertGui();
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('exitgate'));
    }, 15000, 'exitgate folder row');
    await enterFolder('exitgate');
    await ensureDualPane();
    await localDir(egdir, 'exit-8m.bin');
    await dnd(await sideRow('exit-8m.bin'), await bodyH());
    await startIfAsked(3000);
    const job = await waitFor(async () => ((await call('ActiveTransfers')) || []).find((x) => x.status === 'running' && !x.hidden && x.sentBytes > 0 && x.sentBytes < x.totalBytes), 20000, 'the throttled running job');
    await shot('43-exit-gate-busy');
    await evalPage(() => { window.__exitReason = null; window.runtime?.EventsOn('exit:confirm', (p) => { window.__exitReason = String(p?.reason ?? p); }); return true; });
    const veto = await call('ShouldClose');
    need(veto === true, `ShouldClose did not veto while a transfer runs (${JSON.stringify(veto)})`);
    await waitFor(async () => /transfer|job|running/i.test(String(await evalPage(() => window.__exitReason))), 5000, 'the exit:confirm reason naming the transfer');
    // ExitApp while busy only ASKS — the app must stay fully alive
    await call('ExitApp');
    await sleep(1200);
    const health = await (await fetch(`http://127.0.0.1:${GUI_PORT}/health`)).json();
    need(health?.status === 'ok', `the app died on a guarded ExitApp: ${JSON.stringify(health)}`);
    const ver = await call('GetVersion');
    need(String(ver).length > 0, 'GetVersion stopped answering after the guarded ExitApp');
    const still = ((await call('ActiveTransfers')) || []).some((x) => x.id === job.id && x.status === 'running');
    need(still, 'the running transfer did not survive the guarded ExitApp');
    // dismiss the confirmation — NEVER ConfirmExit — then cancel normally
    await evalPage(() => {
      const b = Array.from(document.querySelectorAll('#modal-root button'))
        .find((x) => /cancel|keep/i.test((x.textContent || '').trim()) && !/exit|quit|anyway/i.test(x.textContent || ''));
      if (b) b.click();
      return true;
    });
    await closeModal();
    await call('CancelTransfer', job.id);
    await waitFor(async () => JSON.stringify(await call('ActiveTransfers')).includes('canceled'), 30000, 'the job to be canceled');
    await evalPage(() => { localStorage.removeItem('s3b-throttle'); localStorage.removeItem('s3b-show-throttle'); return true; });
    const st = await s3(['stat', `${P}/exit-8m.bin`]);
    need(st.code !== 0, 'a partial object survived the canceled exit-gate transfer');
    return 'close vetoed with a reason; guarded ExitApp did not kill the app; transfer canceled clean';
  });

  await verify({ id: 'GUI-44', area: 'sources', action: 'Local filesystem bindings: list / preview / remove', ds: 'local disk', scenario: 'ListLocal reports entries with correct dir flags; LocalDeletePreview counts what a delete would take; LocalRemove actually deletes — and the deletion is proven from OUTSIDE the app (Node fs), never from the app’s own view', face: 'GUI' }, async () => {
    const d = path.join(ART, 'gui-local');
    await rm(d, { recursive: true, force: true });
    await mkdir(path.join(d, 'sub'), { recursive: true });
    const f1 = path.join(d, 'l-one.txt'), f2 = path.join(d, 'sub', 'l-two.txt');
    await writeFile(f1, 'one');
    await writeFile(f2, 'two');
    const entries = (await call('ListLocal', d)) || [];
    const one = entries.find((e) => e.name === 'l-one.txt');
    const sub = entries.find((e) => e.name === 'sub');
    need(one && one.isDir === false, `ListLocal misreported the file: ${JSON.stringify(one)}`);
    need(sub && sub.isDir === true, `ListLocal misreported the dir: ${JSON.stringify(sub)}`);
    const prev = await call('LocalDeletePreview', [f2]);
    need(prev && prev.count === 1 && prev.objects === 1 && prev.folders === 0 && prev.bytes === 3,
      `the preview miscounted one 3-byte file: ${JSON.stringify(prev)?.slice(0, 160)}`);
    await call('LocalRemove', [f2]);
    let gone = false;
    try { await readFile(f2); } catch { gone = true; }
    need(gone, 'LocalRemove left the file on disk (Node fs still reads it)');
    const again = (await call('ListLocal', path.join(d, 'sub'))) || [];
    need(!again.some((e) => e.name === 'l-two.txt'), 'ListLocal still shows the removed file');
    await rm(d, { recursive: true, force: true });
    return 'list / preview / remove verified; removal proven from outside';
  });

  await verify({ id: 'GUI-45', area: 'settings', action: 'Settings round-trips: log-file prefs + tuning', ds: 'Wails v3 server', scenario: 'SetLogSettings(off) reads back off and the original preference restores exactly; SetTuning echoes the requested values and the originals restore — settings are reversible state, never one-way doors', face: 'GUI' }, async () => {
    const orig = await call('GetLogSettings');
    need(orig && typeof orig.mode === 'string', `GetLogSettings shape: ${JSON.stringify(orig)?.slice(0, 120)}`);
    const off = await call('SetLogSettings', 'off', '', [], [], []);
    need(off?.mode === 'off', `set off did not read back: ${JSON.stringify(off)}`);
    const mid = await call('GetLogSettings');
    need(mid?.mode === 'off', `GetLogSettings after off: ${JSON.stringify(mid)}`);
    const restored = await call('SetLogSettings', orig.mode, orig.dir || '', orig.levels || [], orig.scopes || [], orig.sources || []);
    need(restored?.mode === orig.mode, `restore did not read back: ${JSON.stringify(restored)}`);
    const t0 = await call('GetTuning');
    // compareTimeoutMs floors at 10s — request above every floor
    const t1 = await call('SetTuning', 777, 10888, 3, 9, 4, 12345);
    need(t1?.listingTimeoutMs === 777 && t1?.compareTimeoutMs === 10888 && t1?.retryAttempts === 3
      && t1?.partSizeMiB === 9 && t1?.partConcurrency === 4 && t1?.stallAfterMs === 12345,
      `SetTuning echo drifted: ${JSON.stringify(t1)}`);
    const vals = [t0?.listingTimeoutMs, t0?.compareTimeoutMs, t0?.retryAttempts, t0?.partSizeMiB, t0?.partConcurrency, t0?.stallAfterMs];
    if (vals.every((v) => typeof v === 'number')) {
      await call('SetTuning', ...vals);
    } else {
      await call('SetTuning', 5000, 8000, 4, 16, 8, 120000);
    }
    return 'log settings + tuning set, read back, restored';
  });

  await verify({ id: 'GUI-46', area: 'security', action: 'Secure storage toggle round-trip', ds: 'OS keyring (Windows)', scenario: 'GetSecureStorage reports availability; where the keyring is usable the toggle flips and restores with every read consistent — where it is not, that is recorded, never papered over', face: 'GUI' }, async () => {
    const s0 = await call('GetSecureStorage');
    need(s0 && typeof s0.enabled === 'boolean', `GetSecureStorage shape: ${JSON.stringify(s0)?.slice(0, 160)}`);
    if (!s0.keyringAvailable) return skip(`keyring not available on this host (backend: ${s0.keyringBackend || 'none'})`);
    const flip = await call('SetSecureStorage', !s0.enabled);
    need(flip?.enabled === !s0.enabled, `the toggle did not flip: ${JSON.stringify(flip)}`);
    const mid = await call('GetSecureStorage');
    need(mid?.enabled === !s0.enabled, `GetSecureStorage disagrees after the toggle: ${JSON.stringify(mid)}`);
    const back = await call('SetSecureStorage', s0.enabled);
    need(back?.enabled === s0.enabled, 'the restore did not flip back');
    return `keyring (${s0.keyringBackend}) toggled and restored`;
  });

  await verify({ id: 'GUI-47', area: 'settings', action: 'Language switch: Finnish UI, then restore English', ds: 'Wails v3 server', scenario: 's3b-lang=fi + reload: the grid’s own chrome speaks Finnish (the Name column becomes Nimi) with no English column label left; restoring en + reload returns English — the locale switch is total, not partial', face: 'GUI' }, async () => {
    await evalPage(() => { localStorage.setItem('s3b-lang', 'fi'); return true; });
    await page.reload();
    await sleep(1200);
    await waitFor(async () => await evalPage(() => document.body.textContent.includes('Nimi')), 15000, 'the Finnish column header');
    const fi = await evalPage(() => ({
      nimi: document.body.textContent.includes('Nimi'),
      enName: /\bName\b/.test(document.body.textContent),
    }));
    need(fi.nimi, 'the Name column did not switch to Nimi');
    need(!fi.enName, 'an English "Name" label survived the Finnish locale');
    await evalPage(() => { localStorage.setItem('s3b-lang', 'en'); return true; });
    await page.reload();
    await sleep(1200);
    await waitFor(async () => await evalPage(() => document.body.textContent.includes('Name')), 15000, 'English restored');
    return 'Finnish chrome verified (Nimi); English restored';
  });

  await verify({ id: 'GUI-48', area: 'objects', action: 'Task registry: running view + ClearFinishedTasks', ds: 'S3 (MinIO)', scenario: 'a deep search registers as a running task (kind search); once finished, ClearFinishedTasks(null) prunes EVERY finished row (null means all — an empty list is a no-op by contract) — the registry mirrors live work and forgets dead work on command', face: 'GUI' }, async () => {
    const tok = await call('DeepSearch', BUCKET, 'verify-gui/', { pattern: 'gui-', limit: 50 });
    need(typeof tok === 'string' && tok.length > 0, 'DeepSearch returned no token');
    let sawRunning = false;
    for (let i = 0; i < 15 && !sawRunning; i++) {
      sawRunning = ((await call('RunningTasks')) || []).some((t) => t.kind === 'search');
      if (!sawRunning) await sleep(100);
    }
    await waitFor(async () => !((await call('RunningTasks')) || []).some((t) => t.kind === 'search' && (t.status === 'running' || t.status === 'queued')), 20000, 'the search task to finish');
    await call('ClearFinishedTasks', null);
    const after = (await call('RunningTasks')) || [];
    need(after.every((t) => !['done', 'canceled', 'error', 'failed'].includes(String(t.status).toLowerCase())), `a finished task survived the clear: ${JSON.stringify(after)?.slice(0, 160)}`);
    return sawRunning ? 'search registered, finished, pruned (clear-all)' : 'search finished too fast to observe running; prune verified';
  });

  // Navigation oracle: a breadcrumb substring LIES when the row starts
  // deeper than its target (an earlier row may have left the view inside
  // verify-gui/<subfolder>, and "…/verify-gui/sub" includes "verify-gui"),
  // and during a stalled remote listing the DOM keeps the PREVIOUS rows.
  // The only trustworthy proof of "we are inside X" is X's OWN listing:
  // retry the dblclick until the expected CHILD row renders.
  const enterFolderByChild = async (label, childNeedle) => {
    try {
      return await waitFor(async () => {
        if ((await rowKeys()).some((k) => k.includes(childNeedle))) return true;
        try { await (await rowAction(label)).dblclick({ timeout: 4000 }); } catch { /* row not rendered yet / mid-render */ }
        await sleep(300);
        return false;
      }, 25000, `inside ${label} (its ${childNeedle} row visible)`);
    } catch (e) {
      // one-line state dump + screenshot: a covered row starves the
      // dblclick and the bare timeout says nothing about WHY (run 4 was
      // exactly this — a floating popout over the target rows)
      await shot(`navfail-${label}`.replace(/[^a-z0-9-]/gi, '')).catch(() => {});
      const st = await evalPage(() => JSON.stringify({
        crumb: (document.querySelector('#breadcrumb')?.textContent || '').trim(),
        rows: Array.from(document.querySelectorAll('#grid-body .grid-row')).slice(0, 10)
          .map((r) => r.dataset.key || (r.querySelector('.tname')?.textContent || '')),
        popouts: Array.from(document.querySelectorAll('#popout-root .popout')).map((p) => {
          const r = p.getBoundingClientRect();
          return `${Math.round(r.x)},${Math.round(r.y)} ${Math.round(r.width)}x${Math.round(r.height)}`;
        }),
      })).catch(() => '?');
      throw new Error(`${e.message}: ${st}`);
    }
  };
  // A floating popout over the grid intercepts pointer events and starves
  // row clicks — the battery hit this before (GUI-34 leaves the transfer
  // manager; navCertGui carries this insurance for the mid-battery rows).
  // The rows right before these (GUI-44..48) never navigate, so whatever
  // they leave floating sails straight into the walk: close raised popouts
  // through their real affordance, dismiss any modal, clear any armed
  // filter — all programmatically, immune to what covers what.
  const sweepOverlays = async () => {
    try {
      await evalPage(() => { document.querySelectorAll('#popout-root .popout .modal-head .x').forEach((b) => b.click()); return true; });
      await sleep(150);
    } catch { /* best effort */ }
    try { await closeModal(); } catch { /* best effort */ }
    try {
      await evalPage(() => {
        const f = document.querySelector('#filter');
        if (f && f.value) { f.value = ''; f.dispatchEvent(new Event('input', { bubbles: true })); }
        return true;
      });
      await sleep(200);
    } catch { /* best effort */ }
  };

  await verify({ id: 'GUI-49', area: 'objects', action: 'Copy URL: the real address, resolved from the viewing source (S3)', ds: 'S3 (MinIO)', scenario: 'Copy URL on an object resolves through the source\'s OWN endpoint and addressing style: the clipboard receives exactly http://localhost:9000/bucket/key (MinIO path-style, no signature query) — and a bare GET from OUTSIDE the app, with no credentials, reaches the real object address and is answered 403: the address is real, the grant is not (a presigned URL would have served bytes)', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/gui-url`;
    await s3(['mkdir', `${P}/`]);
    await s3(['cp', path.join(FIX, 'data', 'root-1.txt'), `${P}/url.txt`]);
    await treeOpen(SRCNAME);
    await sweepOverlays();
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('verify-gui')), 15000, 's3 root');
    await enterFolderByChild('verify-gui', 'gui-url');
    await enterFolderByChild('gui-url', 'url.txt');
    const want = `http://localhost:9000/${BUCKET}/verify-gui/gui-url/url.txt`;
    await installWireSpy();
    try {
      await clickRow('url.txt');
      await rightClickRow('url.txt');
      await copyViaMenu(/copy url/i);
      await waitForClipArgs(want, 'copy url');
      // out-of-band: the address is REAL — MinIO answers it, access
      // controlled (403 for a bare anonymous GET: no grant rides along)
      const res = await fetch(want);
      need(res.status === 403, `bare GET on the copied URL answered ${res.status}, not the anonymous 403 of a real ungranted object address`);
      await shot('49-copy-url-s3');
      return 'exact endpoint-resolved URL; out-of-band bare GET → 403 (real address, no grant)';
    } finally {
      await undoWireSpy();
    }
  });

  if (haveFtp) {
    await verify({ id: 'GUI-50', area: 'sources', action: 'Copy URL: the real address of a remote source row', ds: 'FTP', scenario: 'a file row of the FTP source: Copy URL puts ftp://user@host:port/server-path on the clipboard — username and the non-default port from the source\'s own connection fields, the anchored server path exactly as the engine addresses it, and never the password', face: 'GUI' }, async () => {
      await treeOpen(FTPNAME);
      await sweepOverlays();
      // PROVE the switch before trusting any row: the crumb must END with
      // the source name (the root crumb), and a stalled remote listing
      // keeps the previous grid under a loading state
      await waitFor(async () => {
        await refresh();
        return (await txt('#breadcrumb')).trim().endsWith(FTPNAME);
      }, 25000, `the view on the ${FTPNAME} root`);
      // the row's own seed is deterministic: this run uploaded the fixture
      // tree to <runid>/gui — descend into IT, never whatever else the
      // shared engine holds at its root (old runids, other harnesses).
      // The engine sat idle since the early rows, so the server has since
      // dropped the control connection (vsftpd idle timeout) — the first
      // listing fails on the dead socket and the app redials on the next
      // attempt, so KEEP REFRESHING until a real listing lands; polling a
      // stale DOM without new requests would wait forever (run 4 did).
      await waitFor(async () => {
        await refresh();
        return (await rowKeys()).some((k) => k.startsWith(RUNID));
      }, 50000, `this run's ${RUNID}/ seed folder`);
      await enterFolderByChild(RUNID, 'gui');
      await enterFolderByChild('gui', 'readme.md');
      // the expected address is built from the ROW MODEL's anchored key —
      // dataset.key is the bare display name; the backend joins the model
      // key (rooted at the source root, here empty) onto the URL path
      const key = await evalPage(() => {
        const r = Array.from(document.querySelectorAll('#grid-body .grid-row'))
          .find((x) => String(x._model?.key ?? '').endsWith('readme.md'));
        return r ? String(r._model.key) : '';
      });
      need(key.startsWith('/'), `readme.md row model key not anchored: "${key}"`);
      const want = `ftp://${E2E_USER}@127.0.0.1:${FTP_PORT}${key}`;
      await installWireSpy();
      try {
        // bounded, retried clicks: a re-rendering remote grid must fail
        // the row fast and let the retry land it, never hang 20 s
        await waitFor(async () => {
          try { await (await rowAction('readme.md')).click({ timeout: 4000 }); return true; } catch { return false; }
        }, 15000, 'a stable click on readme.md');
        await waitFor(async () => {
          try { await (await rowAction('readme.md')).click({ button: 'right', timeout: 4000 }); return true; } catch { return false; }
        }, 15000, 'a stable right-click on readme.md');
        await copyViaMenu(/copy url/i);
        await waitForClipArgs(want, 'remote copy url');
        await shot('50-copy-url-ftp');
        return `${want} — user + non-default port, anchored path, no password`;
      } finally {
        await undoWireSpy();
      }
    });
  } else {
    await verify({ id: 'GUI-50', area: 'sources', action: 'Copy URL: the real address of a remote source row', ds: 'FTP', scenario: 'FTP container not running', face: 'GUI' }, () => skip('FTP :2121 not reachable'));
  }

  await verify({ id: 'GUI-51', area: 'deletion', action: 'Delete-marker windows: merged multi view, single-object view, Undo delete restores', ds: 'S3 (MinIO)', scenario: 'two objects whose timelines carry markers (delete then overwrite, so the rows stay visible): selecting both opens the merged Delete markers window — both keys listed, bulk checkboxes; one object alone opens the fitted single view — no checkboxes, Close footer; Remove selected un-deletes both and the single view\'s own Remove restores its object, every recovery proven by the CLI timeline afterwards', face: 'GUI' }, async () => {
    const P = `s3://${BUCKET}/verify-gui/gui-mark`;
    await s3(['mkdir', `${P}/`]);
    for (const n of ['m1.txt', 'm2.txt']) {
      await s3(['cp', path.join(FIX, 'data', 'root-1.txt'), `${P}/${n}`]);
      await s3(['rm', `${P}/${n}`]); // marker: the object hides
      await s3(['cp', path.join(FIX, 'data', 'root-2.txt'), `${P}/${n}`]); // newer data: the row is back, marker in history
    }
    await treeOpen(SRCNAME);
    await sweepOverlays();
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('verify-gui')), 15000, 's3 root');
    await enterFolderByChild('verify-gui', 'gui-mark');
    await enterFolderByChild('gui-mark', 'm1.txt');
    await waitFor(async () => {
      await refresh();
      const keys = await rowKeys();
      return ['m1.txt', 'm2.txt'].every((n) => keys.some((k) => k.endsWith(n)));
    }, 20000, 'marker-carrying rows');
    // the marker counts land on a second async pass (the version summary) —
    // wait until the menu actually offers the entry, never a fixed sleep
    const entryEnabled = (re) => evalPage((src) => {
      const it = Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
        .find((i) => new RegExp(src, 'i').test(i.textContent || ''));
      return !!it && !it.classList.contains('disabled');
    }, re.source);
    const rightClickStable = (l) => waitFor(async () => {
      try { await (await rowAction(l)).click({ button: 'right', timeout: 4000 }); return true; } catch { return false; }
    }, 15000, `a stable right-click on ${l}`);
    // --- merged multi window ---
    await clickRow('m1.txt');
    await ctrlClickRow('m2.txt');
    let opened = false;
    for (let i = 0; i < 12 && !opened; i++) {
      await rightClickStable('m1.txt');
      opened = await entryEnabled(/delete markers \(2 selected\)/i);
      if (!opened) { await page.keyboard.press('Escape'); await sleep(500); await refresh(); }
    }
    need(opened, 'the merged-window entry never appeared (version summary missing?)');
    await ctxItem(/delete markers \(2 selected\)/i);
    // every marker surface honors the Settings → View toggle: while off
    // the window shows the hidden-notice + its own inline opt-in instead
    // of rows — click the window's real affordance, never reach around
    await waitFor(async () => {
      if (await evalPage(() => document.querySelectorAll('#modal-root .ver-row').length >= 2)) return true;
      await evalPage(() => {
        const b = Array.from(document.querySelectorAll('#modal-root button'))
          .find((x) => /show delete markers/i.test(x.textContent || ''));
        if (b) b.click();
        return true;
      });
      return false;
    }, 12000, 'merged window rows');
    const multiShape = await evalPage(() => ({
      title: (document.querySelector('#modal-root .modal-head span') || {}).textContent || '',
      checks: document.querySelectorAll('#modal-root .ver-check input').length,
      keys: Array.from(document.querySelectorAll('#modal-root .ver-row .ver-main > div:first-child')).map((d) => d.textContent.trim()),
    }));
    need(/delete markers \(2 selected\)/i.test(multiShape.title), `merged title: "${multiShape.title}"`);
    need(multiShape.checks >= 2, `merged window must carry bulk checkboxes: ${multiShape.checks}`);
    need(multiShape.keys.some((k) => k.includes('m1.txt')) && multiShape.keys.some((k) => k.includes('m2.txt')),
      `merged window misses a key: ${JSON.stringify(multiShape.keys)}`);
    await shot('51-marker-window-multi');
    // select both markers and remove them (a confirm stands between)
    await evalPage(() => { document.querySelectorAll('#modal-root .ver-check input').forEach((c) => c.click()); return true; });
    await clickFooter(/remove selected/i);
    await clickFooter(/remove selected/i); // the confirm
    await sleep(800);
    for (const n of ['m1.txt', 'm2.txt']) {
      const v = await s3(['versions', 'ls', `${P}/${n}`, '--json']);
      need(!v.out.includes('"isDeleteMarker": true'), `${n}: a marker survived the bulk undo`);
    }
    await closeModal();
    // --- fitted single view ---
    await s3(['rm', `${P}/m1.txt`]);
    await s3(['cp', path.join(FIX, 'data', 'root-1.txt'), `${P}/m1.txt`]);
    await waitFor(async () => { await refresh(); return true; }, 1, 'one refresh');
    await clickRow('m1.txt');
    opened = false;
    for (let i = 0; i < 12 && !opened; i++) {
      await rightClickStable('m1.txt');
      opened = await entryEnabled(/^delete marker(?!s)/i);
      if (!opened) { await page.keyboard.press('Escape'); await sleep(500); await refresh(); }
    }
    need(opened, 'the single-view entry never appeared');
    await ctxItem(/^delete marker(?!s)/i);
    await waitFor(async () => await evalPage(() => !!document.querySelector('#modal-root .ver-row')), 8000, 'single view row');
    const singleShape = await evalPage(() => ({
      title: (document.querySelector('#modal-root .modal-head span') || {}).textContent || '',
      checks: document.querySelectorAll('#modal-root .ver-check input').length,
      foot: Array.from(document.querySelectorAll('#modal-root .modal-foot .btn')).map((b) => b.textContent.trim()),
    }));
    need(/delete marker — s3:\/\//i.test(singleShape.title), `single title: "${singleShape.title}"`);
    need(singleShape.checks === 0, `the fitted single view must not carry bulk checkboxes: ${singleShape.checks}`);
    need(singleShape.foot.some((b) => /^close$/i.test(b)), `single view footer lost its Close: ${JSON.stringify(singleShape.foot)}`);
    await shot('52-marker-window-single');
    // its own Remove undoes the marker and closes the fitted view
    const clicked = await evalPage(() => {
      const b = Array.from(document.querySelectorAll('#modal-root .ver-row .btn'))
        .find((x) => /^remove$/i.test(x.textContent.trim()));
      if (b) b.click();
      return !!b;
    });
    need(clicked, 'no Remove button in the single view');
    await waitFor(async () => !(await modalVisible()), 8000, 'the single view to close after its last undo');
    const v1 = await s3(['versions', 'ls', `${P}/m1.txt`, '--json']);
    need(!v1.out.includes('"isDeleteMarker": true'), 'm1.txt: a marker survived the single-view undo');
    need((v1.out.match(/"(key|versionId)"/g) || []).length >= 2, 'm1.txt: the data timeline did not survive the undo');
    return 'merged + single windows shaped right; both undos CLI-proven, data timeline intact';
  });
  await verify({ id: 'GUI-52', area: 'gui', action: 'Help → License window: popout payload + external-link guard', ds: 'Wails v3 server', scenario: 'the exact URL a native popout window loads (?popout=license) renders the three-partition window — About (version, publisher, links), License (the name linked to the published text), Third-party — with no Unknown-popout dead end, the guide sibling still routing, and OpenExternal refusing file:// so a hostile href can never reach the shell', face: 'GUI' }, async () => {
    // the native window payload — the path the sweeps never load (they
    // exercise the DOM fallback), which is how an empty window shipped
    await page.goto(`http://127.0.0.1:${GUI_PORT}/?popout=license`);
    await waitFor(async () => await evalPage(() => !!document.querySelector('.tabstrip .tab.active')), 8000, 'license popout tabs');
    const shape = await evalPage(() => ({
      dead: document.body.textContent.includes('Unknown popout'),
      tabs: Array.from(document.querySelectorAll('.tabstrip .tab')).map((t) => t.dataset.tab),
      active: (document.querySelector('.tabstrip .tab.active') || {}).dataset.tab || '',
      links: Array.from(document.querySelectorAll('a.ext-link')).map((a) => a.href),
      version: (document.querySelector('.tabbody .v.mono') || {}).textContent || '',
    }));
    need(!shape.dead, 'rendered the Unknown-popout dead end');
    need(shape.tabs.join(',') === 'about,license,third', `partitions: ${shape.tabs.join(',')}`);
    need(shape.active === 'about', 'About is not the default partition');
    need(/^v\d/.test(shape.version) && !/vv\d/.test(shape.version), `version row: "${shape.version}"`);
    need(shape.links.includes('https://polyformproject.org/licenses/internal-use/1.0.0.txt'), `license link missing: ${shape.links.join(',')}`);
    need(shape.links.includes('https://github.com/MikkoP88/s3-bucket-browser'), `project link missing: ${shape.links.join(',')}`);
    await shot('52-license-about');
    await evalPage(() => { document.querySelector('.tab[data-tab="license"]').click(); return true; });
    const lic = await evalPage(() => ({
      links: Array.from(document.querySelectorAll('a.ext-link')).map((a) => a.href),
      text: (document.querySelector('.tabbody') || document.body).textContent,
    }));
    need(lic.links.length === 1 && lic.links[0] === 'https://polyformproject.org/licenses/internal-use/1.0.0.txt', `license partition links: ${lic.links.join(',')}`);
    need(lic.text.includes('Copyright (c) 2026 Mikko Pesonen (MikkoP88).'), 'license partition lost the copyright line');
    await shot('52-license-license');
    // dispatcher regression (guide still routes) + the scheme guard,
    // proven WITHOUT opening anything: file:// must be refused
    await page.goto(`http://127.0.0.1:${GUI_PORT}/?popout=guide`);
    await waitFor(async () => await evalPage(() => !!document.querySelector('.tabstrip .tab.active')), 8000, 'guide popout tabs');
    need(!(await evalPage(() => document.body.textContent.includes('Unknown popout'))), 'guide popout stopped rendering');
    const refused = await evalPage(async () => {
      const mod = await import('/js/api.js');
      try { await mod.api.OpenExternal('file:///C:/Windows/win.ini'); return false; } catch { return true; }
    });
    need(refused, 'OpenExternal accepted file:// — the guard is down');
    return 'popout payload renders; guards hold';
  });
  await verify({ id: 'GUI-09', area: 'gui', action: 'Page-error gate', ds: 'Wails v3 server', scenario: 'zero uncaught page errors across the whole GUI battery', face: 'GUI' }, async () => {
    need(pageErrors.length === 0, `${pageErrors.length} page error(s): ${pageErrors[0]}`);
    return 'clean console';
  });
}

// ============================================================
// Sweeps — the full harnesses as subprocesses
// ============================================================
function runSweep(script, re) {
  return new Promise((resolve) => {
    const p = spawn(process.execPath, [path.join(ROOT, 'scripts', script)], {
      cwd: ROOT, windowsHide: true,
      // S3B_EXPECT_VERSION: the sweep reuses the exes this run stamped, so its
      // GetVersion round-trip must assert THIS stamp, not a hardcoded one.
      env: { ...process.env, NO_COLOR: '1', S3B_EXPECT_VERSION: VERSION },
    });
    let out = '';
    p.stdout.on('data', (d) => { out += d; });
    p.stderr.on('data', (d) => { out += d; });
    p.on('exit', (code) => resolve({ code, out }));
  });
}

// A sweep is tens of minutes of browser + engine work; a transient timing
// flake should not fail the release gate. One transparent retry: a
// deterministic regression fails both attempts, and a pass-on-retry is
// disclosed in the PASS detail — never silent.
async function sweepGate(script, re, fmt) {
  const attempt = async () => {
    const { code, out } = await runSweep(script);
    return { code, out, m: re.exec(out), fl: out.split(/\r?\n/).filter((l) => /FAIL/i.test(l)).slice(0, 3).join(' | ') };
  };
  const describe = (a) => (!a.m
    ? `no summary (exit ${a.code}): ${a.out.split(/\r?\n/).filter(Boolean).slice(-3).join(' | ')}`
    : fmt.fail(a.m, a.code, a.fl));
  const a = await attempt();
  if (a.m && a.code === 0 && fmt.ok(a.m)) return fmt.pass(a.m);
  const first = describe(a);
  const b = await attempt();
  if (b.m && b.code === 0 && fmt.ok(b.m)) return `${fmt.pass(b.m)} — passed on retry (first attempt: ${first})`;
  throw new Error(describe(b));
}

// ============================================================
// Release report — committed evidence under docs/verification/
// ============================================================
// `--release <tag>` writes the run's evidence into the repo:
//   docs/verification/<tag>/<os>-<arch>/REPORT.md         human report
//   docs/verification/<tag>/<os>-<arch>/verification.json machine copy
//   docs/verification/README.md                           index (regenerated)
// The release workflow gates publishing on this directory (full matrix,
// zero FAIL) and links it from the release notes — no report, no release.
const esc = (s) => String(s ?? '').replace(/\|/g, '\\|');

async function writeReleaseReport(v) {
  const dir = path.join(ROOT, 'docs', 'verification', RELEASE, OSID);
  await rm(dir, { recursive: true, force: true });
  await mkdir(dir, { recursive: true });
  await writeFile(path.join(dir, 'verification.json'), JSON.stringify(v, null, 2) + '\n');

  const s = v.summary;
  const verdict = s.fail === 0
    ? `**RELEASE GATE: PASS — ${s.pass} PASS · ${s.skip} SKIP · 0 FAIL**`
    : `**RELEASE GATE: FAIL — ${s.pass} PASS · ${s.skip} SKIP · ${s.fail} FAIL**`;
  const L = [];
  L.push(`# Verification report — s3b ${RELEASE}`);
  L.push('');
  L.push(`${verdict} — full matrix in ${s.seconds}s.`);
  L.push('');
  L.push(`**Version stamp:** \`${v.version}\` — binary-verified by the run itself:`);
  L.push('CLI-M-01 requires the binary to print exactly this stamp; GUI-01 round-trips');
  L.push('it through the Wails bridge. The stamp matches what the release workflow');
  L.push('builds (`.github/workflows/release.yml`).');
  L.push('');
  if (v.commit) L.push(`**Verified commit:** \`${v.commit}\`${v.commitSubject ? ` — ${esc(v.commitSubject)}` : ''}`);
  L.push(`**OS:** ${v.os}`);
  L.push(`**Node:** ${v.node}`);
  L.push(`**Run:** id \`${v.runId}\`, bucket \`${v.bucket}\`, started ${v.generatedAt}`);
  L.push(`**Command:** \`node scripts/verify.mjs --release ${RELEASE}\``);
  L.push('');
  L.push('Every row below ran end-to-end through binaries built by this run from the');
  L.push('verified commit — CLI face and GUI face against live engines (MinIO S3, SFTP,');
  L.push('FTP, WebDAV), with byte-level verification, safety-gate probes, cancellation');
  L.push('probes and fault injection. The gate itself is documented in');
  L.push('[docs/VERIFICATION.md](../../../VERIFICATION.md).');
  L.push('');
  L.push('## Certificate');
  L.push('');
  L.push('| ID | Face | Action | Source | Scenario | Result |');
  L.push('|---|---|---|---|---|---|');
  for (const r of v.rows) {
    const res = r.result === 'PASS' ? 'PASS' : `**${r.result}**${r.detail ? ` — ${esc(r.detail)}` : ''}`;
    L.push(`| ${r.id} | ${r.face} | ${esc(r.action)} | ${esc(r.ds)} | ${esc(r.scenario)} | ${res} |`);
  }
  const noted = v.rows.filter((r) => r.result === 'PASS' && r.detail);
  if (v.rows.some((r) => r.result !== 'PASS') || noted.length) {
    L.push('');
    L.push('## Notes');
    L.push('');
    for (const r of v.rows.filter((x) => x.result !== 'PASS')) L.push(`- **${r.id} ${r.result}** — ${esc(r.detail)}`);
    for (const r of noted) L.push(`- ${r.id} PASS — ${esc(r.detail)}`);
  }
  L.push('');
  L.push('## Reproduce');
  L.push('');
  L.push('```bash');
  L.push(`node scripts/verify.mjs --release ${RELEASE}`);
  L.push('```');
  L.push('');
  L.push('Prerequisites (live engine containers) and the full row matrix:');
  L.push('[docs/VERIFICATION.md](../../../VERIFICATION.md). `verification.json` next to');
  L.push('this file is the machine-readable copy of the same run.');
  await writeFile(path.join(dir, 'REPORT.md'), L.join('\n') + '\n');

  // Index: regenerated from disk — one row per report found, newest release
  // first, so the table stays true even if a report is added by hand.
  const base = path.join(ROOT, 'docs', 'verification');
  const found = [];
  for (const d of await readdir(base, { withFileTypes: true })) {
    if (!d.isDirectory()) continue;
    for (const o of await readdir(path.join(base, d.name), { withFileTypes: true })) {
      if (!o.isDirectory()) continue;
      try {
        const j = JSON.parse(await readFile(path.join(base, d.name, o.name, 'verification.json'), 'utf8'));
        found.push({ tag: d.name, osid: o.name, j });
      } catch { /* directory without a report — not listed */ }
    }
  }
  const triple = (t) => t.replace(/^v/, '').split('-')[0].split('.').map(Number);
  const suffix = (t) => t.replace(/^v/, '').split('-').slice(1).join('-');
  found.sort((a, b) => {
    const [x, y] = [triple(a.tag), triple(b.tag)];
    for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return y[i] - x[i];
    return suffix(b.tag).localeCompare(suffix(a.tag), undefined, { numeric: true });
  });
  const I = [];
  I.push('# Verification reports');
  I.push('');
  I.push('Release evidence for the action-verification gate');
  I.push('([docs/VERIFICATION.md](../VERIFICATION.md)): one directory per release tag,');
  I.push('one sub-directory per OS the gate ran on — `REPORT.md` (build + OS + the full');
  I.push('certificate) and `verification.json` (machine copy). Produced by');
  I.push('`node scripts/verify.mjs --release <tag>` on the tagged commit; the release');
  I.push('workflow refuses to publish a tag without a green report here, and links the');
  I.push('report in the release notes.');
  I.push('');
  I.push('<!-- Generated by scripts/verify.mjs --release — do not edit by hand. -->');
  I.push('');
  I.push('| Release | OS | Date (UTC) | Result | Checks | Report |');
  I.push('|---|---|---|---|---|---|');
  for (const { tag, osid, j } of found) {
    const ok = j.summary?.fail === 0;
    I.push(`| [${tag}](${tag}/) | ${osid} | ${(j.generatedAt || '').slice(0, 10)} | ${ok ? 'PASS' : '**FAIL**'} | ${j.summary?.pass ?? '?'} PASS · ${j.summary?.skip ?? '?'} SKIP · ${j.summary?.fail ?? '?'} FAIL | [REPORT.md](${tag}/${osid}/REPORT.md) |`);
  }
  await writeFile(path.join(base, 'README.md'), I.join('\n') + '\n');
  return path.join('docs', 'verification', RELEASE, OSID);
}

// ============================================================
// main
// ============================================================
async function main() {
  const t0 = Date.now();
  const COMMIT = await gitOut(['log', '-1', '--pretty=%h']);
  const COMMIT_SUBJ = await gitOut(['log', '-1', '--pretty=%s']);
  console.log(`s3b action verification — ${VERSION} on ${os.type()} ${os.release()} (${os.arch()})`);
  console.log(`run id ${RUNID}, bucket ${BUCKET}${QUICK ? ', quick mode (sweeps skipped)' : ''}${ONLY !== 'all' ? `, category: ${ONLY}` : ''}${RELEASE ? `, RELEASE EVIDENCE for ${RELEASE} @ ${COMMIT || 'unknown commit'}` : ''}\n`);

  await rm(ART, { recursive: true, force: true });
  await mkdir(SHOTS, { recursive: true });
  await mkdir(CFG, { recursive: true });
  await mkdir(GUICFG, { recursive: true });
  await writeFixtures();

  if (!(await portOpen(9000))) {
    console.error('FATAL: MinIO is not listening on :9000 — start the e2e containers (scripts/e2e-cross.sh header).');
    process.exit(2);
  }

  if (!NO_BUILD) {
    process.stdout.write('building exes … ');
    for (const [tags, out] of [['s3b_headless', CLI_EXE], ['server', SRV_EXE]]) {
      await new Promise((res, rej) => {
        const p = spawn('go', ['build', '-tags', tags, '-o', out, '-ldflags', `-X main.version=${VERSION}`, './cmd/s3b'], { cwd: ROOT, windowsHide: true });
        p.on('exit', (c) => (c === 0 ? res() : rej(new Error(`go build -tags ${tags} exited ${c}`))));
        p.stderr.on('data', (d) => process.stderr.write(d));
      });
    }
    console.log('done');
  }

  if (want('s3')) await cliS3();
  if (want('cross')) await cliCross();
  if (want('resilience')) await cliResilience();
  if (want('meta')) await cliMeta();

  if (!SKIP_GUI && want('gui')) {
    try { await guiBattery(); } catch (err) {
      rows.push({ id: 'GUI-ERR', area: 'gui', action: 'GUI battery', ds: '—', scenario: 'battery aborted', face: 'GUI', result: 'FAIL', detail: String(err?.message || err).slice(0, 200) });
      failures.push(`GUI battery: ${err?.message || err}`);
    } finally {
      try { await stopServer(); } catch { /* best effort */ }
      try { await context?.close(); } catch { /* best effort */ }
    }
  }

  if (!QUICK && want('sweeps')) {
    await verify({ id: 'SWEEP-VIS-01', area: 'sweeps', action: 'Full visual sweep', ds: 'shim world', scenario: 'node scripts/gui-visual.mjs — every dialog/popout/menu/viewport contract', face: 'SWEEP' }, async () => sweepGate('gui-visual.mjs', /gui-visual: (\d+)\/(\d+) checks passed/, {
      ok: (m) => +m[2] > 0 && +m[1] === +m[2],
      pass: (m) => `${m[1]}/${m[2]} checks`,
      fail: (m, code, fl) => `${m[1]}/${m[2]}, exit ${code}${fl ? ` — ${fl}` : ''}`,
    }));
    await verify({ id: 'SWEEP-LIVE-01', area: 'sweeps', action: 'Full live walk', ds: 'real engines', scenario: 'node scripts/gui-v3live.mjs — real bindings, transfers, versions, fault lab', face: 'SWEEP' }, async () => sweepGate('gui-v3live.mjs', /v3 live walk: (\d+) check\(s\) passed, (\d+) failed/, {
      ok: (m) => +m[1] > 0 && +m[2] === 0,
      pass: (m) => `${m[1]} checks, no page errors`,
      fail: (m, code, fl) => `${m[1]} passed, ${m[2]} failed, exit ${code}${fl ? ` — ${fl}` : ''}`,
    }));
  }

  // ---------- cleanup live state (best effort, never fails the run) ----------
  console.log('\ncleanup …');
  await cli(['rb', `s3://${BUCKET}`, '--force', '--profile', 'verifys3']).then((r) => console.log(`  bucket: exit ${r.code}`)).catch(() => {});
  for (const s of ['xt', 'xf', 'xw']) {
    await cli(['rm', `${s}://${RUNID}`, '-r', '--force']).then((r) => console.log(`  ${s}: exit ${r.code}`)).catch(() => {});
  }

  // ---------- report ----------
  const osName = `${os.type()} ${os.release()} (${os.arch()})`;
  const byResult = (r) => rows.filter((x) => x.result === r).length;
  const verification = {
    version: VERSION, tag: RELEASE, commit: COMMIT, commitSubject: COMMIT_SUBJ,
    os: osName, osId: OSID, node: process.version,
    generatedAt: new Date().toISOString(), runId: RUNID, bucket: BUCKET,
    category: ONLY,
    faces: ['CLI', 'GUI', 'SWEEP'],
    summary: { total: rows.length, pass: byResult('PASS'), fail: byResult('FAIL'), skip: byResult('SKIP'), seconds: Math.round((Date.now() - t0) / 1000) },
    rows,
  };
  await writeFile(path.join(ART, 'verification.json'), JSON.stringify(verification, null, 2));

  console.log(`\n=== VERIFICATION REPORT ===`);
  const w = [6, 13, 34, 16, 46, 6];
  const pad = (s, n) => String(s ?? '').slice(0, n - 1).padEnd(n);
  console.log([pad('ID', w[0]), pad('FACE', w[1]), pad('ACTION', w[2]), pad('SOURCE', w[3]), pad('SCENARIO', w[4]), pad('OS', 12), 'RESULT'].join(' '));
  for (const r of rows) {
    console.log([pad(r.id, w[0]), pad(r.face, w[1]), pad(r.action, w[2]), pad(r.ds, w[3]), pad(r.scenario, w[4]), pad(osName.replace(/ \(.*\)/, ''), 12), r.result].join(' '));
  }
  console.log(`\n${verification.summary.pass} PASS · ${verification.summary.skip} SKIP · ${verification.summary.fail} FAIL — ${verification.summary.seconds}s — ${path.join('testartifacts', 'verification', 'verification.json')}`);
  if (RELEASE) {
    const rel = await writeReleaseReport(verification);
    console.log(`release report: ${path.join(rel, 'REPORT.md')}${verification.summary.fail ? ' — FAILING: fix before tagging (the workflow will refuse the release)' : ' — commit this with the tag (see CONTRIBUTING.md "Cutting a release")'}`);
  }
  if (failures.length) {
    console.log('\nFAILURES:');
    for (const f of failures) console.log(`  ${f}`);
  }
  process.exitCode = failures.length ? 1 : 0;
}

main().catch((err) => { console.error('verify:', err); process.exit(1); });
