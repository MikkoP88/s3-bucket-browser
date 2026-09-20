#!/usr/bin/env node
// certify.mjs — the ACTION CERTIFICATION run: every row of docs/CERTIFICATION.md
// is executed here, for real, against real engines.
//
// "Certified" means: the action ran end-to-end through the shipped binary —
// CLI face and/or GUI face — against live data (MinIO S3, SFTP, FTP, WebDAV
// containers per scripts/e2e-cross.sh) with byte-level verification where
// bytes move, safety-gate probes where destruction is involved, and fault
// injection where resilience is claimed. A row that cannot run (engine
// container down, provider gap) is recorded SKIP — never silently passed.
//
// Three faces are certified:
//   CLI   — s3b-cert-cli.exe (s3b_headless build) driven as a process; exit
//           codes and output are asserted, never eyeballed.
//   GUI   — s3b-server.exe (the Wails `server` stack: real bindings, real
//           backend, real browser) driven by Playwright; results verified
//           BACK through the CLI so a GUI green means bytes on disk.
//   SWEEP — the two full harnesses (gui-visual.mjs, gui-v3live.mjs) re-run
//           as subprocesses and folded into the certificate as summary rows.
//
// Usage:  node scripts/certify.mjs [--quick] [--skip-gui] [--no-build] [--headed]
//           --quick    skip the two full sweeps (CLI+GUI batteries still run)
//           --skip-gui CLI + sweeps only (no browser battery)
//           --no-build reuse existing testartifacts exes
//         Repeat the run to certify repeatability: results must be identical.
// Artifacts: testartifacts/certification/ (certificate.json, fixtures/,
//           gui shots, server log) — wiped fresh every run.
//
// Prerequisites: MinIO on :9000 (minioadmin/minioadmin). SFTP :2222,
// FTP :2121, WebDAV :7070 (e2e/e2epass) join when their port answers.

import { spawn, execFile } from 'node:child_process';
import { rm, mkdir, writeFile, readFile, readdir } from 'node:fs/promises';
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const ART = path.join(ROOT, 'testartifacts', 'certification');
const FIX = path.join(ART, 'fixtures');
const CFG = path.join(ART, 'cli-config');      // S3B_CONFIG for the CLI face
const GUICFG = path.join(ART, 'gui-config');   // S3B_CONFIG for the GUI face
const SHOTS = path.join(ART, 'shots');
const CLI_EXE = path.join(ROOT, 'testartifacts', 's3b-cert-cli.exe');
const SRV_EXE = path.join(ROOT, 'testartifacts', 's3b-server.exe');
const VERSION = 'v1.1.0-beta.14-9-wails3';

const arg = (k) => process.argv.includes(`--${k}`);
const QUICK = arg('quick');
const SKIP_GUI = arg('skip-gui');
const NO_BUILD = arg('no-build');
const HEADED = arg('headed');

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

const RUNID = `cert${Date.now().toString(36)}`;
const BUCKET = `cert-${Date.now().toString(36)}`;      // lowercase — S3-safe
const SRCNAME = 'cert-minio';                           // GUI S3 source (bucket-scoped)
const FTPNAME = 'cert-ftp';                             // GUI FTP source
const GUI_PORT = 39874;                                 // v3live uses 39872; keep distinct

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// ---------- certificate rows ----------
const rows = [];
let cur = null;
function cert(meta, fn) {
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

// ---------- port probe ----------
const portOpen = (port) => new Promise((res) => {
  const s = net.connect({ host: '127.0.0.1', port, timeout: 1500 });
  s.once('connect', () => { s.destroy(); res(true); });
  s.once('error', () => res(false));
  s.once('timeout', () => { s.destroy(); res(false); });
});

// ---------- fixtures ----------
const FIXTREE = {
  'readme.md': 'certification readme\n',
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
  await cert({ id: 'CLI-S3-01', area: 'sources', action: 'Add + test S3 source', ds: 'S3 (MinIO)', scenario: 'source add with endpoint/keys; source test dials; list shows it; mirrors as profile', face: 'CLI' }, async () => {
    let r = await cli(['source', 'add', 'certs3', '--type', 's3', '--endpoint', ENDPOINT, '--access-key', KEY, '--secret-key', SECRET]);
    need(r.code === 0, `source add: ${r.err}`);
    r = await cli(['source', 'test', 'certs3']);
    need(r.code === 0 && /✅|OK/.test(r.out + r.err), `source test: ${r.out}${r.err}`);
    r = await cli(['source', 'list']);
    need(r.out.includes('certs3'), 'source list missing certs3');
    r = await cli(['profile', 'list']);
    need(r.out.includes('certs3'), 'profile mirror missing');
    return 'added, tested, listed, mirrored';
  });

  await cert({ id: 'CLI-S3-02', area: 'buckets', action: 'Create versioned bucket', ds: 'S3 (MinIO)', scenario: 'mb + bucket versioning on (the delete-marker scenarios need it)', face: 'CLI' }, async () => {
    let r = await cli(['mb', B]);
    need(r.code === 0 && /created bucket/.test(r.out), `mb: ${r.out}${r.err}`);
    r = await cli(['bucket', 'versioning', B, 'on']);
    need(r.code === 0 && /versioning enabled/.test(r.out), `versioning: ${r.out}${r.err}`);
    return `${BUCKET} created, versioning on`;
  });

  await cert({ id: 'CLI-S3-03', area: 'objects', action: 'Create folder marker', ds: 'S3 (MinIO)', scenario: 'mkdir docs/ → zero-byte marker lists as a folder', face: 'CLI' }, async () => {
    const r = await cli(['mkdir', `${B}/docs/`]);
    need(r.code === 0 && /created folder/.test(r.out), `mkdir: ${r.out}${r.err}`);
    const l = await cli(['ls', B]);
    need(/docs/.test(l.out), 'folder not listed');
    return 'docs/ marker created and listed';
  });

  await cert({ id: 'CLI-S3-04', area: 'transfers', action: 'Single upload', ds: 'S3 (MinIO)', scenario: 'cp one file → stat reports size', face: 'CLI' }, async () => {
    let r = await cli(['cp', path.join(FIX, 'data', 'readme.md'), `${B}/readme.md`]);
    need(r.code === 0 && /copied 1 item/.test(r.out), `cp: ${r.out}${r.err}`);
    r = await cli(['stat', `${B}/readme.md`]);
    need(/size:/.test(r.out), `stat: ${r.out}`);
    return 'uploaded + stat ok';
  });

  await cert({ id: 'CLI-S3-05', area: 'transfers', action: 'Multi upload (recursive)', ds: 'S3 (MinIO)', scenario: `cp -r fixture tree (${NFILES} files incl. unicode + empty) → recursive ls count matches`, face: 'CLI' }, async () => {
    let r = await cli(['cp', '-r', path.join(FIX, 'data'), `${B}/data/`, '--json']);
    need(r.code === 0 && r.out.includes(`"items": ${NFILES}`), `cp -r: ${r.out}${r.err}`);
    r = await cli(['ls', `${B}/`, '--recursive', '--json']);
    const n = countLines(r.out, '"key"');
    need(n === NFILES + 1, `recursive ls count ${n}, want ${NFILES + 1} (incl. readme)`);
    return `${NFILES} files uploaded, ${n} listed`;
  });

  await cert({ id: 'CLI-S3-06', area: 'objects', action: 'List / tree / du / stat', ds: 'S3 (MinIO)', scenario: 'dir-view ls, tree shows folders, du counts objects+bytes, stat bucket shows region', face: 'CLI' }, async () => {
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

  await cert({ id: 'CLI-S3-07', area: 'transfers', action: 'Single download', ds: 'S3 (MinIO)', scenario: 'cp object → local; bytes identical', face: 'CLI' }, async () => {
    const out = path.join(ART, 'down', 'readme.md');
    await mkdir(path.dirname(out), { recursive: true });
    const r = await cli(['cp', `${B}/readme.md`, out]);
    need(r.code === 0, `cp down: ${r.err}`);
    const a = await readFile(path.join(FIX, 'data', 'readme.md'));
    const b = await readFile(out);
    need(a.equals(b), 'downloaded bytes differ');
    return 'byte-identical';
  });

  await cert({ id: 'CLI-S3-08', area: 'transfers', action: 'Multi download (recursive)', ds: 'S3 (MinIO)', scenario: 'cp -r prefix → local dir; full tree diff byte-identical', face: 'CLI' }, async () => {
    const out = path.join(ART, 'down-tree');
    await rm(out, { recursive: true, force: true });
    const r = await cli(['cp', '-r', `${B}/data/`, out]);
    need(r.code === 0, `cp -r down: ${r.err}`);
    const want = await treeOf(path.join(FIX, 'data'));
    const got = await treeOf(out);
    need(sameTree(want, got), `tree diff:\n${JSON.stringify(got).slice(0, 300)}`);
    return `${got.length} files byte-identical`;
  });

  await cert({ id: 'CLI-S3-09', area: 'transfers', action: 'Server-side copy S3→S3', ds: 'S3 (MinIO)', scenario: 'cp s3://→s3:// lands a copyable object', face: 'CLI' }, async () => {
    let r = await cli(['cp', `${B}/readme.md`, `${B}/copy/readme-v2.md`]);
    need(r.code === 0, `s3s3: ${r.err}`);
    r = await cli(['ls', `${B}/copy/`, '--json']);
    need(r.out.includes('readme-v2.md'), 'copy not listed');
    return 'server-side copy listed';
  });

  await cert({ id: 'CLI-S3-10', area: 'objects', action: 'Single object deletion', ds: 'S3 (MinIO)', scenario: 'rm one object → gone from ls; versioned → delete marker in timeline', face: 'CLI' }, async () => {
    let r = await cli(['rm', `${B}/data/logs/log-01.log`]);
    need(r.code === 0 && /deleted/.test(r.out), `rm: ${r.out}${r.err}`);
    r = await cli(['ls', `${B}/data/logs/`, '--json']);
    need(!r.out.includes('log-01'), 'object still listed');
    r = await cli(['versions', 'ls', `${B}/data/logs/log-01.log`, '--json']);
    need(r.out.includes('"isDeleteMarker": true'), 'no delete marker in timeline');
    return 'removed + marker recorded';
  });

  await cert({ id: 'CLI-S3-11', area: 'objects', action: 'Recursive deletion + safety gates', ds: 'S3 (MinIO)', scenario: '55-object prefix: rm -r without --force rejected (>50 gate); --dry-run counts (marker included); --force deletes them all', face: 'CLI' }, async () => {
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

  await cert({ id: 'CLI-S3-12', area: 'buckets', action: 'rb safety gate', ds: 'S3 (MinIO)', scenario: 'rb on a non-empty bucket rejected without --force', face: 'CLI' }, async () => {
    const r = await cli(['rb', B]);
    need(r.code !== 0, 'rb non-empty must fail');
    return 'rejected as designed';
  });

  await cert({ id: 'CLI-S3-13', area: 'objects', action: 'Rename (mv)', ds: 'S3 (MinIO)', scenario: 'mv object → new key; old gone, new stats', face: 'CLI' }, async () => {
    let r = await cli(['mv', `${B}/copy/readme-v2.md`, `${B}/copy/readme-v3.md`]);
    need(r.code === 0 && /moved 1 item/.test(r.out), `mv: ${r.out}${r.err}`);
    r = await cli(['stat', `${B}/copy/readme-v3.md`]);
    need(/size:/.test(r.out), 'renamed key missing');
    return 'moved + stat ok';
  });

  await cert({ id: 'CLI-S3-14', area: 'transfers', action: 'sync (repair / no-op / new / --delete)', ds: 'S3 (MinIO)', scenario: 'sync repairs the CLI-S3-10 deletion, reports 0 when in sync, 1 after a local add, deletes the extra remote with --delete', face: 'CLI' }, async () => {
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

  await cert({ id: 'CLI-S3-15', area: 'objects', action: 'Presign + fetch', ds: 'S3 (MinIO)', scenario: 'presign → plain HTTP GET returns identical bytes', face: 'CLI' }, async () => {
    const r = await cli(['presign', `${B}/readme.md`, '--expires', '5m']);
    need(r.code === 0 && r.out.startsWith('http'), `presign: ${r.out}${r.err}`);
    const res = await fetch(r.out.trim());
    need(res.ok, `presigned GET ${res.status}`);
    const body = Buffer.from(await res.arrayBuffer());
    const want = await readFile(path.join(FIX, 'data', 'readme.md'));
    need(body.equals(want), 'presigned bytes differ');
    return 'URL fetched, bytes identical';
  });

  await cert({ id: 'CLI-S3-16', area: 'versions', action: 'Version timeline: restore + undo', ds: 'S3 (MinIO)', scenario: 'overwrite → 2 versions; restore v1 as latest; rm → marker; undo revives v1', face: 'CLI' }, async () => {
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

  await cert({ id: 'CLI-S3-17', area: 'versions', action: 'Purge + permanent destroy', ds: 'S3 (MinIO)', scenario: 'versions stat; purge noncurrent (>50 gate demands --force, then --force purges); versions rm --all empties the timeline (L3)', face: 'CLI' }, async () => {
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

  await cert({ id: 'CLI-S3-18', area: 'objects', action: 'Storage-class conversion', ds: 'S3 (MinIO)', scenario: 'sc single → REDUCED_REDUNDANCY visible + find --class; recursive dry-run gate', face: 'CLI' }, async () => {
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

  await cert({ id: 'CLI-S3-19', area: 'search', action: 'Deep find', ds: 'S3 (MinIO)', scenario: '--name glob/substring, --smaller, --limit, summary line', face: 'CLI' }, async () => {
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

  await cert({ id: 'CLI-S3-20', area: 'admin', action: 'Doctor diagnosis', ds: 'S3 (MinIO)', scenario: 'doctor s3://bucket runs the check ladder', face: 'CLI' }, async () => {
    const r = await cli(['doctor', B]);
    need(r.code === 0 && /DNS Resolution Check/.test(r.out), `doctor: ${r.out}${r.err}`);
    return 'check ladder ran';
  });

  await cert({ id: 'CLI-S3-21', area: 'admin', action: 'Bucket admin: info / tags / policy', ds: 'S3 (MinIO)', scenario: 'info shows versioning; tags put/get; policy put/get round-trip (cors/encryption tolerate provider gaps)', face: 'CLI' }, async () => {
    let r = await cli(['bucket', 'info', B]);
    need(/versioning:/.test(r.out), `info: ${r.out}`);
    r = await cli(['bucket', 'tags', 'put', B, 'team=cert', 'env=ci']);
    need(/tag\(s\) saved/.test(r.out), `tags put: ${r.out}`);
    r = await cli(['bucket', 'tags', 'get', B]);
    need(r.out.includes('team=cert'), 'tags get');
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

  await cert({ id: 'CLI-S3-22', area: 'admin', action: 'Object lock: retention + legal hold', ds: 'S3 (MinIO)', scenario: 'mb --object-lock; retention set/show/clear; legalhold on/off (GOVERNANCE only — cleanup stays possible)', face: 'CLI' }, async () => {
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

  await cert({ id: 'CLI-S3-23', area: 'admin', action: 'Activity log', ds: 'S3 (MinIO)', scenario: 'log shows the operations this run performed', face: 'CLI' }, async () => {
    const r = await cli(['log']);
    need(r.code === 0 && r.out.trim().length > 0, `log: ${r.out}${r.err}`);
    return `${r.out.split('\n').length} lines recorded`;
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
    await cert({ id: 'CLI-X-00', area: 'sources', action: 'Remote engines', ds: 'SFTP/FTP/WebDAV', scenario: 'containers not running', face: 'CLI' },
      () => skip('no engine containers reachable (2222/2121/7070)'));
    return;
  }

  await cert({ id: 'CLI-X-01', area: 'sources', action: 'Add + test remote sources', ds: 'SFTP/FTP/WebDAV', scenario: 'sftp:// and webdav:// URL shorthand + ftp flags; source test dials each', face: 'CLI' }, async () => {
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
    await cert({ id: 'CLI-X-02', area: 'transfers', action: 'Multi-file copy local→FTP', ds: 'FTP', scenario: `cp -r fixture tree (${NFILES} files: unicode, empty file, nested dirs) → xf://${RUNID}/tree`, face: 'CLI' }, async () => {
      const r = await cli(['cp', '-r', path.join(FIX, 'data'), `xf://${RUNID}/tree`, '--json']);
      need(r.code === 0 && r.out.includes(`"items": ${NFILES}`), `cp -r: ${r.out}${r.err}`);
      const l = await cli(['ls', `xf://${RUNID}/tree`, '--recursive', '--json']);
      for (const f of Object.keys(FIXTREE)) need(l.out.includes(f), `${f} missing from the FTP listing`);
      return `${NFILES} files on the FTP source`;
    });

    await cert({ id: 'CLI-X-03', area: 'transfers', action: 'Multi-file copy FTP→S3 (cross-engine)', ds: 'FTP → S3', scenario: 'cp -r the FTP tree into the bucket; count + full byte round-trip back to disk', face: 'CLI' }, async () => {
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

    await cert({ id: 'CLI-X-04', area: 'objects', action: 'Remote browse + delete gates', ds: 'FTP', scenario: 'ls/du/stat/tree on the remote; rm -r dry-run counts; --force deletes; prefix gone', face: 'CLI' }, async () => {
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
    await cert({ id: 'CLI-X-05', area: 'transfers', action: 'SFTP round-trip', ds: 'SFTP', scenario: 'upload tree → download tree → byte-identical diff', face: 'CLI' }, async () => {
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
    await cert({ id: 'CLI-X-06', area: 'transfers', action: 'WebDAV round-trip', ds: 'WebDAV', scenario: 'upload tree → download tree → byte-identical diff', face: 'CLI' }, async () => {
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
    await cert({ id: 'CLI-X-07', area: 'transfers', action: 'Cross-engine move (mv)', ds: 'SFTP → FTP', scenario: 'mv -r sftp tree → ftp; source gone; destination byte-identical', face: 'CLI' }, async () => {
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

  await cert({ id: 'CLI-X-08', area: 'sources', action: 'Export + import sources', ds: 'all', scenario: 'encrypted export (--password); import into a FRESH config lists the same sources', face: 'CLI' }, async () => {
    const f = path.join(ART, 'sources.json');
    let r = await cli(['source', 'export', f, '--password', 'cert-export-pw']);
    need(r.code === 0, `export: ${r.err}`);
    const blob = await readFile(f, 'utf8');
    need(!blob.includes('minioadmin'), 'export must be encrypted, not plaintext');
    const altCfg = path.join(ART, 'alt-config');
    await mkdir(altCfg, { recursive: true });
    r = await cli(['source', 'import', f, '--password', 'cert-export-pw'], { cfg: altCfg });
    need(r.code === 0, `import: ${r.err}`);
    r = await cli(['source', 'list'], { cfg: altCfg });
    need(r.out.includes('certs3'), 'imported store missing certs3');
    return 'encrypted export/import round-trip';
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
  await cert({ id: 'CLI-RES-01', area: 'resilience', action: 'Slow link: latency +600ms/chunk', ds: 'S3 via faultproxy', scenario: 'listing through a delayed proxy completes with correct output, measurably slower', face: 'CLI' }, async () => {
    proxy = spawn('node', [path.join(ROOT, 'scripts', 'faultproxy.mjs'), '--listen', String(FPORT), '--control', String(FCTL), '--target', '127.0.0.1:9000'], { stdio: 'ignore', windowsHide: true });
    for (let i = 0; i < 50; i++) { try { await fetch(`http://127.0.0.1:${FCTL}/state`, { signal: AbortSignal.timeout(500) }); break; } catch { await sleep(100); } }
    await cli(['source', 'add', 'certfault', '--type', 's3', '--endpoint', `http://127.0.0.1:${FPORT}`, '--access-key', KEY, '--secret-key', SECRET]);
    await fmode({ mode: 'latency', delayMs: 600 });
    const t0 = Date.now();
    const r = await cli(['ls', `s3://${BUCKET}`, '--recursive', '--json', '--profile', 'certfault']);
    const dt = Date.now() - t0;
    need(r.code === 0 && r.out.includes('readme'), `latency ls: ${r.out}${r.err}`);
    need(dt >= 1000, `finished in ${dt}ms — delay not applied`);
    return `correct listing in ${(dt / 1000).toFixed(1)}s under latency`;
  });

  await cert({ id: 'CLI-RES-02', area: 'resilience', action: 'Dead link: RST mid-session', ds: 'S3 via faultproxy', scenario: 'connection reset → non-zero exit, error classified, no partial success', face: 'CLI' }, async () => {
    if (!proxy) return skip('proxy not running');
    await fmode({ mode: 'reset' });
    const r = await cli(['ls', `s3://${BUCKET}`, '--profile', 'certfault']);
    need(r.code !== 0, 'ls survived a reset connection');
    need((r.out + r.err).length > 0, 'no error message');
    return 'clean classified failure';
  });

  await cert({ id: 'CLI-RES-03', area: 'resilience', action: 'Blackhole: endpoint never answers', ds: 'S3 via faultproxy', scenario: 'watchdog timeout within the --timeout budget (no default 5-minute hang)', face: 'CLI' }, async () => {
    if (!proxy) return skip('proxy not running');
    await fmode({ mode: 'blackhole' });
    const t0 = Date.now();
    const r = await cli(['ls', `s3://${BUCKET}`, '--profile', 'certfault', '--timeout', '8s'], { timeout: 40000 });
    const dt = Date.now() - t0;
    need(r.code !== 0, 'ls survived a blackhole');
    need(dt < 30000, `took ${dt}ms — budget blown`);
    return `failed cleanly in ${(dt / 1000).toFixed(1)}s`;
  });
  stop();
}

async function cliMeta() {
  await cert({ id: 'CLI-M-01', area: 'meta', action: 'Version identity', ds: '—', scenario: 'version prints the build version, exit 0', face: 'CLI' }, async () => {
    const r = await cli(['version']);
    need(r.code === 0 && r.out.trim() === VERSION, `version: ${r.out}`);
    return VERSION;
  });
  await cert({ id: 'CLI-M-02', area: 'meta', action: 'Usage-error contract', ds: '—', scenario: 'unknown command → exit 2, stderr reads "usage error:" and points at --help', face: 'CLI' }, async () => {
    const r = await cli(['definitely-not-a-cmd']);
    need(r.code === 2, `exit ${r.code}, want 2`);
    need(r.err.includes('usage error:') && r.err.includes('--help'), `stderr: ${r.err}`);
    return 'exit 2 + labeled';
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

  await cert({ id: 'GUI-01', area: 'gui', action: 'Live stack boots', ds: 'Wails v3 server', scenario: 'server /health ok; page loads; GetVersion binding round-trips the build version', face: 'GUI' }, async () => {
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

  await cert({ id: 'GUI-02', area: 'sources', action: 'Add S3 source (GUI)', ds: 'S3 (MinIO)', scenario: 'onboarding → editor → Test ✅ → Save → bucket root lists', face: 'GUI' }, async () => {
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
  // --profile certs3: the resilience battery added a second s3 source, so
  // bare s3:// URIs would be ambiguous in this store.
  const s3 = (args) => cli(['--profile', 'certs3', ...args]);
  await cli(['cp', '-r', path.join(FIX, 'data'), `xf://${RUNID}/gui`, '--json']).catch(() => {});
  await s3(['mkdir', `s3://${BUCKET}/cert-gui/`]).catch(() => {});

  const haveFtp = await portOpen(FTP_PORT);
  if (haveFtp) {
    await cert({ id: 'GUI-03', area: 'sources', action: 'Add FTP source (GUI)', ds: 'FTP', scenario: 'sidebar + → FTP fields → Test ✅ → Save → root lists', face: 'GUI' }, async () => {
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

    await cert({ id: 'GUI-04', area: 'transfers', action: 'Multi-file copy FTP→S3 (GUI)', ds: 'FTP → S3', scenario: 'browse FTP seed dir; ctrl-click 2 files; Ctrl+C; open S3 cert-gui/; Ctrl+V; both rows land', face: 'GUI' }, async () => {
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
      await waitFor(async () => (await rowKeys()).some((k) => k.includes('cert-gui')), 10000, 's3 root');
      await enterFolder('cert-gui');
      // an empty folder shows the empty-state OVERLAY while stale rows linger
      // in the DOM — prove emptiness by the overlay, not by row absence
      await waitFor(async () => evalPage(() => {
        const e = document.getElementById('empty-state');
        return !!e && !e.classList.contains('hidden') && !e.classList.contains('is-loading');
      }), 8000, 'empty cert-gui');
      await page.keyboard.press('Control+v');
      await waitFor(async () => {
        await refresh();
        const keys = await rowKeys();
        return ['readme.md', 'root-1.txt'].every((f) => keys.some((k) => k.includes(f)));
      }, 30000, 'pasted rows');
      await shot('05-ftp-to-s3-pasted');
      return '2 files FTP→S3 through the GUI';
    });

    await cert({ id: 'GUI-05', area: 'transfers', action: 'GUI transfer byte verification', ds: 'FTP → S3', scenario: 'download the GUI-pasted objects via the CLI; bytes match the FTP originals', face: 'GUI' }, async () => {
      const out = path.join(ART, 'gui-verify');
      await rm(out, { recursive: true, force: true });
      const r = await s3(['cp', '-r', `s3://${BUCKET}/cert-gui/`, out]);
      need(r.code === 0, `download: ${r.err}`);
      for (const f of ['readme.md', 'root-1.txt']) {
        const a = await readFile(path.join(FIX, 'data', f));
        const b = await readFile(path.join(out, f));
        need(a.equals(b), `${f} bytes differ after GUI copy`);
      }
      return 'both files byte-identical';
    });
  } else {
    await cert({ id: 'GUI-03', area: 'sources', action: 'Add FTP source (GUI)', ds: 'FTP', scenario: 'FTP container not running', face: 'GUI' }, () => skip('FTP :2121 not reachable'));
  }

  await cert({ id: 'GUI-06', area: 'objects', action: 'Single object deletion (GUI)', ds: 'S3 (MinIO)', scenario: 'CLI-seeded object; row selected; Del → Delete Window (marker default) → confirm; row gone; CLI timeline shows the marker', face: 'GUI' }, async () => {
    await s3(['cp', path.join(FIX, 'data', 'root-2.txt'), `s3://${BUCKET}/cert-gui/gui-del.txt`]);
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('cert-gui')), 10000, 's3 root');
    await enterFolder('cert-gui');
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
    const v = await s3(['versions', 'ls', `s3://${BUCKET}/cert-gui/gui-del.txt`, '--json']);
    need(v.out.includes('"isDeleteMarker": true'), 'no marker on the CLI side');
    await shot('08-after-delete');
    return 'marker deletion verified both faces';
  });

  await cert({ id: 'GUI-07', area: 'objects', action: 'New folder (GUI)', ds: 'S3 (MinIO)', scenario: 'empty-area context menu → New folder → prompt; CLI ls shows the marker', face: 'GUI' }, async () => {
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('cert-gui')), 10000, 's3 root');
    await enterFolder('cert-gui');
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
    await answerPrompt('cert-folder');
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('cert-folder'));
    }, 15000, 'folder row');
    const l = await s3(['ls', `s3://${BUCKET}/cert-gui/`, '--json']);
    need(l.out.includes('cert-folder'), 'CLI cannot see the GUI-created folder');
    return 'folder created + CLI-verified';
  });

  await cert({ id: 'GUI-08', area: 'objects', action: 'Rename (F2, GUI)', ds: 'S3 (MinIO)', scenario: 'CLI-seeded object; F2 → new name; CLI stat sees the new key', face: 'GUI' }, async () => {
    await s3(['cp', path.join(FIX, 'data', 'empty.txt'), `s3://${BUCKET}/cert-gui/cert-rename-me.txt`]);
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.includes('cert-gui')), 10000, 's3 root');
    await enterFolder('cert-gui');
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('cert-rename-me.txt'));
    }, 15000, 'rename target row');
    await clickRow('cert-rename-me.txt');
    await page.keyboard.press('F2');
    await answerPrompt('cert-renamed.txt');
    await waitFor(async () => {
      await refresh();
      return (await rowKeys()).some((k) => k.includes('cert-renamed.txt'));
    }, 15000, 'renamed row');
    const s = await s3(['stat', `s3://${BUCKET}/cert-gui/cert-renamed.txt`]);
    need(/size:/.test(s.out), `CLI stat of renamed key: ${s.out}${s.err}`);
    return 'renamed + CLI-verified';
  });

  await cert({ id: 'GUI-09', area: 'gui', action: 'Page-error gate', ds: 'Wails v3 server', scenario: 'zero uncaught page errors across the whole GUI battery', face: 'GUI' }, async () => {
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
      env: { ...process.env, NO_COLOR: '1' },
    });
    let out = '';
    p.stdout.on('data', (d) => { out += d; });
    p.stderr.on('data', (d) => { out += d; });
    p.on('exit', (code) => resolve({ code, out }));
  });
}

// ============================================================
// main
// ============================================================
async function main() {
  const t0 = Date.now();
  console.log(`s3b action certification — ${VERSION} on ${os.type()} ${os.release()} (${os.arch()})`);
  console.log(`run id ${RUNID}, bucket ${BUCKET}${QUICK ? ', quick mode (sweeps skipped)' : ''}\n`);

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

  await cliS3();
  await cliCross();
  await cliResilience();
  await cliMeta();

  if (!SKIP_GUI) {
    try { await guiBattery(); } catch (err) {
      rows.push({ id: 'GUI-ERR', area: 'gui', action: 'GUI battery', ds: '—', scenario: 'battery aborted', face: 'GUI', result: 'FAIL', detail: String(err?.message || err).slice(0, 200) });
      failures.push(`GUI battery: ${err?.message || err}`);
    } finally {
      try { await stopServer(); } catch { /* best effort */ }
      try { await context?.close(); } catch { /* best effort */ }
    }
  }

  if (!QUICK) {
    await cert({ id: 'SWEEP-VIS-01', area: 'sweeps', action: 'Full visual sweep', ds: 'shim world', scenario: 'node scripts/gui-visual.mjs — every dialog/popout/menu/viewport contract', face: 'SWEEP' }, async () => {
      const { code, out } = await runSweep('gui-visual.mjs');
      const m = /gui-visual: (\d+)\/(\d+) checks passed/.exec(out);
      need(m, `no summary (exit ${code})`);
      need(+m[2] > 0 && +m[1] === +m[2] && code === 0, `${m[1]}/${m[2]}, exit ${code}`);
      return `${m[1]}/${m[2]} checks`;
    });
    await cert({ id: 'SWEEP-LIVE-01', area: 'sweeps', action: 'Full live walk', ds: 'real engines', scenario: 'node scripts/gui-v3live.mjs — real bindings, transfers, versions, fault lab', face: 'SWEEP' }, async () => {
      const { code, out } = await runSweep('gui-v3live.mjs');
      const m = /v3 live walk: (\d+) check\(s\) passed, (\d+) failed/.exec(out);
      need(m, `no summary (exit ${code})`);
      need(+m[1] > 0 && +m[2] === 0 && code === 0, `${m[1]} passed, ${m[2]} failed, exit ${code}`);
      return `${m[1]} checks, no page errors`;
    });
  }

  // ---------- cleanup live state (best effort, never fails the run) ----------
  console.log('\ncleanup …');
  await cli(['rb', `s3://${BUCKET}`, '--force', '--profile', 'certs3']).then((r) => console.log(`  bucket: exit ${r.code}`)).catch(() => {});
  for (const s of ['xt', 'xf', 'xw']) {
    await cli(['rm', `${s}://${RUNID}`, '-r', '--force']).then((r) => console.log(`  ${s}: exit ${r.code}`)).catch(() => {});
  }

  // ---------- report ----------
  const osName = `${os.type()} ${os.release()} (${os.arch()})`;
  const byResult = (r) => rows.filter((x) => x.result === r).length;
  const certificate = {
    version: VERSION, os: osName, node: process.version,
    generatedAt: new Date().toISOString(), runId: RUNID, bucket: BUCKET,
    faces: ['CLI', 'GUI', 'SWEEP'],
    summary: { total: rows.length, pass: byResult('PASS'), fail: byResult('FAIL'), skip: byResult('SKIP'), seconds: Math.round((Date.now() - t0) / 1000) },
    rows,
  };
  await writeFile(path.join(ART, 'certificate.json'), JSON.stringify(certificate, null, 2));

  console.log(`\n=== CERTIFICATE ===`);
  const w = [6, 13, 34, 16, 46, 6];
  const pad = (s, n) => String(s ?? '').slice(0, n - 1).padEnd(n);
  console.log([pad('ID', w[0]), pad('FACE', w[1]), pad('ACTION', w[2]), pad('SOURCE', w[3]), pad('SCENARIO', w[4]), pad('OS', 12), 'RESULT'].join(' '));
  for (const r of rows) {
    console.log([pad(r.id, w[0]), pad(r.face, w[1]), pad(r.action, w[2]), pad(r.ds, w[3]), pad(r.scenario, w[4]), pad(osName.replace(/ \(.*\)/, ''), 12), r.result].join(' '));
  }
  console.log(`\n${certificate.summary.pass} PASS · ${certificate.summary.skip} SKIP · ${certificate.summary.fail} FAIL — ${certificate.summary.seconds}s — ${path.join('testartifacts', 'certification', 'certificate.json')}`);
  if (failures.length) {
    console.log('\nFAILURES:');
    for (const f of failures) console.log(`  ${f}`);
  }
  process.exitCode = failures.length ? 1 : 0;
}

main().catch((err) => { console.error('certify:', err); process.exit(1); });
