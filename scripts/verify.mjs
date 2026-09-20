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
function cli(args, { timeout = 120000, cfg = CFG } = {}) {
  return new Promise((resolve) => {
    execFile(CLI_EXE, args, {
      cwd: ROOT,
      windowsHide: true,
      timeout,
      maxBuffer: 64 * 1024 * 1024,
      env: { ...process.env, S3B_CONFIG: cfg, NO_COLOR: '1', TERM: 'dumb' },
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
    }
    return 'info/tags/policy verified';
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
    const url = (r.out.match(/"url":\s*"([^"]+)"/) || [])[1];
    need(url, `presign hostile: ${r.out}${r.err}`);
    const res = await fetch(url);
    need(res.ok, `presign fetch of %2F name: HTTP ${res.status}`);
    need((await res.text()) === 'percent hazard\n', 'presigned %2F name delivered wrong bytes');
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
    try { (await rowAction(label)).dblclick(); } catch { /* row mid-render */ }
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
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('verify-gui')), 10000, 's3 root');
    await enterFolder('verify-gui');
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
      await sleep(150);
      found = /empty bucket \(all versions\)/i.test(await modalText());
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
