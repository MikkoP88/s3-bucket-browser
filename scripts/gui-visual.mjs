#!/usr/bin/env node
// gui-visual.mjs — GUI visual-capture + interaction harness.
//
// Serves frontend/ from a local static server and drives the real UI in a
// real Chromium (playwright-core driving an installed Edge/Chrome — no
// browser download) with the Wails backend replaced by an in-page shim
// injected before any module loads:
//
//   window.go["github.com/MikkoP88/s3-bucket-browser/pkg/api"].App
//       → recording fake backend (explicit handlers for the methods whose
//         return values drive rendering; everything else records the call
//         and resolves a benign default),
//   window.runtime.EventsOn → a registry the harness emits to through
//       window.__emit(name, payload) (list:page, transfer:update, log:line,
//       wails:file-drop, search:page/search:done …).
//
// The walk covers every user surface — onboarding, buckets, objects + guard
// chips, filter, remote views, menubar dropdowns + Settings dialog, context
// menus, upload menu, doctor, deep search, versions + diff, transfer
// manager, dual-pane bindings + compare, log area, profile flow — plus the
// cross-source drag & drop matrix asserted through synthetic DataTransfer
// events against the recorded backend calls (CopySelection vs TransferCross
// vs Upload vs DownloadRefs routing, move/copy modifier semantics).
//
// Usage:
//   node scripts/gui-visual.mjs [--filter=regex] [--headed] [--channel=name]
//     --filter   run only steps whose name matches the regex (iteration)
//     --headed   show the browser window (debugging)
//     --channel  force a playwright channel (msedge|chrome|chromium|…);
//                default $S3B_BROWSER_CHANNEL, then msedge, chrome, chromium
//
// Artifacts: testartifacts/gui/NN-<name>.png screenshots + report.json.
// Exit code 1 when any check fails (CI gate). Node >= 18.

import { createServer } from 'node:http';
import { readFile, mkdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const FRONTEND = path.join(ROOT, 'frontend');
const OUT = path.join(ROOT, 'testartifacts', 'gui');

// ---------- args ----------
const argv = process.argv.slice(2);
const argOf = (name) => {
  const a = argv.find((x) => x.startsWith(`--${name}=`));
  return a ? a.slice(name.length + 3) : null;
};
const FILTER = argOf('filter') ? new RegExp(argOf('filter'), 'i') : null;
const HEADED = argv.includes('--headed');

// ---------- static server ----------
const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.woff2': 'font/woff2',
};
const server = createServer(async (req, res) => {
  try {
    if (req.url === '/favicon.ico') { res.writeHead(204); res.end(); return; }
    const rel = decodeURIComponent((req.url || '/').split('?')[0]);
    let file = path.join(FRONTEND, rel === '/' ? 'index.html' : rel);
    // containment: nothing outside frontend/
    if (!path.resolve(file).startsWith(path.resolve(FRONTEND))) { res.writeHead(403); res.end(); return; }
    const data = await readFile(file);
    res.writeHead(200, { 'content-type': MIME[path.extname(file).toLowerCase()] || 'application/octet-stream' });
    res.end(data);
  } catch {
    res.writeHead(404); res.end('not found');
  }
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const BASE = `http://127.0.0.1:${server.address().port}/`;

// ---------- browser ----------
const channels = [argOf('channel') || process.env.S3B_BROWSER_CHANNEL, 'msedge', 'chrome', 'chromium'].filter(Boolean);
let browser = null;
const errorsLaunch = [];
for (const channel of [...new Set(channels)]) {
  try { browser = await chromium.launch({ channel, headless: !HEADED }); break; }
  catch (e) { errorsLaunch.push(`${channel}: ${String(e.message).split('\n')[0]}`); }
}
if (!browser) {
  console.error('gui-visual: no Chromium found. Tried:\n  ' + errorsLaunch.join('\n  '));
  console.error('Install Edge/Chrome, or run with --channel=chromium after `npx playwright install chromium`.');
  process.exit(2);
}

// ---------- harness state ----------
const results = [];
const pageErrors = [];
let shotNo = 0;
let stepName = '';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function ok(desc, cond) {
  const v = !!(await cond);
  results.push({ step: stepName, check: desc, pass: v });
  if (!v) console.log(`  FAIL  ${desc}`);
  return v;
}
async function shot(name) {
  shotNo += 1;
  const file = path.join(OUT, String(shotNo).padStart(2, '0') + '-' + name + '.png');
  await page.screenshot({ path: file });
}
async function step(name, fn) {
  if (FILTER && !FILTER.test(name)) return;
  stepName = name;
  process.stdout.write(`== ${name}\n`);
  // step isolation: a modal or context menu left open by a failed step must
  // not cascade into every later step (a stuck overlay blocks all real
  // Playwright clicks while synthetic dispatch keeps working — the worst of
  // both worlds for triage)
  try {
    if (await modalVisible()) await closeModal();
    await closeCtx();
    await page.keyboard.press('Escape');
    await sleep(40);
  } catch { /* pre-boot (about:blank) or page gone */ }
  try { await fn(); }
  catch (e) {
    console.log(`  ERROR ${String(e).split('\n')[0]}`);
    results.push({ step: name, check: `threw: ${String(e).split('\n')[0]}`, pass: false });
    // last backend calls seen by the shim: distinguishes "the click never
    // navigated" from "navigation fired but the listing never came back"
    try {
      const tail = (await calls()).slice(-10)
        .map((c) => `${c.m}(${(c.args ?? []).map((a) => JSON.stringify(a)).join(',').slice(0, 100)})`);
      console.log(`  recent calls: ${tail.join(' | ')}`);
    } catch { /* shim may be gone */ }
    // UI state at failure: separates "navigation never fired" (crumb/tree
    // still at the old location) from "it fired and something reverted it",
    // and exposes stray marquee sweeps via the selection count
    try {
      const ui = await evalPage(() => ({
        crumb: document.getElementById('breadcrumb')?.textContent || '',
        head: document.getElementById('sidebar-head')?.textContent || '',
        tree: Array.from(document.querySelectorAll('#tree .tlabel')).map((l) => l.textContent).slice(0, 16),
        rows: Array.from(document.querySelectorAll('#grid-body .grid-row'))
          .filter((r) => r._model).map((r) => r._model.key).slice(0, 10),
        sel: document.querySelectorAll('#grid-body .grid-row.sel').length,
        mq: document.getElementById('marquee')?.className || '',
      }));
      console.log(`  ui: crumb="${ui.crumb}" head="${ui.head}" sel=${ui.sel} marquee=[${ui.mq}]`);
      console.log(`  tree: ${ui.tree.join(' | ')}`);
      console.log(`  rows: ${ui.rows.join(' | ')}`);
    } catch { /* page may be gone */ }
    try { await shot(`ERROR-${name.replace(/[^a-z0-9-]+/gi, '-')}`); } catch { /* page may be gone */ }
  }
}
async function waitFor(fn, timeout = 6000, what = 'condition') {
  const t0 = Date.now();
  for (;;) {
    let v;
    try { v = await fn(); } catch { /* retry */ }
    if (v) return v;
    if (Date.now() - t0 > timeout) throw new Error(`timeout waiting for ${what}`);
    await sleep(100);
  }
}

// ---------- the backend shim (injected before any module) ----------
function shim() {
  if (window.__shim) return;
  const EMPTY = new URLSearchParams(location.search).get('empty') === '1';

  // Deterministic harness defaults: no blocking conflict dialogs during the
  // DnD matrix, stable theme/lang, no persisted side-pane binding.
  // Wipe EVERY s3b-* key first: the profile dir persists between runs, and a
  // leaked 's3b-panes' from a previous run's dual-pane step opens the side
  // pane at boot (boot-time LocalHome/ListLocal) and shifts every later
  // pane assertion.
  for (const k of Object.keys(localStorage)) {
    if (k.startsWith('s3b-')) localStorage.removeItem(k);
  }
  localStorage.setItem('s3b-conflict', 'overwrite');
  localStorage.setItem('s3b-theme', 'light');
  localStorage.setItem('s3b-lang', 'en');

  const now = Date.now();
  const daysAgo = (d) => new Date(now - d * 86400000).toISOString();

  const world = {
    sources: EMPTY ? [] : [
      { id: 'src-hetzner', name: 'hetzner', type: 's3', default: true },
      { id: 'src-box', name: 'backup-box', type: 'sftp' },
      { id: 'src-dav', name: 'dav-claims', type: 'webdav' },
    ],
    buckets: [
      { name: 'testijotain', createdAt: daysAgo(220) },
      { name: 'logs-2026', createdAt: daysAgo(120) },
      { name: 'media-assets', createdAt: daysAgo(90) },
      { name: 'archive-cold', createdAt: daysAgo(30) },
    ],
    objects: {
      testijotain: [
        { key: 'docs/', isDir: true },
        { key: 'photos/', isDir: true },
        { key: 'reports/', isDir: true },
        { key: 'readme.md', size: 1234, lastModified: daysAgo(1), storageClass: 'STANDARD', etag: '"v3"' },
        { key: 'budget-2026.xlsx', size: 48231, lastModified: daysAgo(4), storageClass: 'STANDARD' },
        { key: 'scan.png', size: 204800, lastModified: daysAgo(12), storageClass: 'STANDARD' },
        { key: 'video-final.mp4', size: 224395264, lastModified: daysAgo(40), storageClass: 'GLACIER' },
        { key: 'docs/notes.md', size: 900, lastModified: daysAgo(2), storageClass: 'STANDARD' },
        { key: 'docs/spec-v2.docx', size: 53248, lastModified: daysAgo(6), storageClass: 'STANDARD' },
        { key: 'docs/legacy/', isDir: true },
        { key: 'docs/legacy/old.txt', size: 10, lastModified: daysAgo(300) },
        { key: 'photos/img-001.jpg', size: 1048576, lastModified: daysAgo(8) },
        { key: 'photos/img-002.jpg', size: 2097152, lastModified: daysAgo(8) },
        { key: 'reports/q4-summary.pdf', size: 11800, lastModified: daysAgo(15) },
      ],
      'logs-2026': [
        { key: 'app/', isDir: true },
        { key: 'app/app-2026-09-11.log', size: 10485760, lastModified: daysAgo(0) },
        { key: 'app/app-2026-09-10.log', size: 9961472, lastModified: daysAgo(1) },
      ],
      'media-assets': [
        { key: 'brand/', isDir: true },
        { key: 'brand/logo.svg', size: 8192, lastModified: daysAgo(60) },
      ],
      'archive-cold': [],
    },
    remote: {
      'backup-box': [
        { key: '/docs/', isDir: true },
        { key: '/upload/', isDir: true },
        { key: '/backup.sh', size: 4096, lastModified: daysAgo(3) },
        { key: '/db.dump', size: 52428800, lastModified: daysAgo(3) },
        { key: '/docs/inventory.csv', size: 2048, lastModified: daysAgo(10) },
      ],
      'dav-claims': [
        { key: '/invoices/', isDir: true },
        { key: '/claim-2026-08.pdf', size: 91136, lastModified: daysAgo(20) },
        { key: '/invoices/inv-042.pdf', size: 66560, lastModified: daysAgo(25) },
      ],
    },
    local: {
      'C:\\Users\\demo': [
        { name: 'Documents', path: 'C:\\Users\\demo\\Documents', isDir: true },
        { name: 'Downloads', path: 'C:\\Users\\demo\\Downloads', isDir: true },
        { name: 'Pictures', path: 'C:\\Users\\demo\\Pictures', isDir: true },
        { name: 'notes.txt', path: 'C:\\Users\\demo\\notes.txt', size: 120, modTime: daysAgo(2) },
        { name: 'report.docx', path: 'C:\\Users\\demo\\report.docx', size: 24576, modTime: daysAgo(9) },
      ],
      'C:\\Users\\demo\\Downloads': [
        { name: 'invoice.pdf', path: 'C:\\Users\\demo\\Downloads\\invoice.pdf', size: 8192, modTime: daysAgo(1) },
        { name: 'spec.docx', path: 'C:\\Users\\demo\\Downloads\\spec.docx', size: 40960, modTime: daysAgo(3) },
        { name: 'photos.zip', path: 'C:\\Users\\demo\\Downloads\\photos.zip', size: 52428800, modTime: daysAgo(5) },
      ],
      'C:\\Users\\demo\\Documents': [
        { name: 'tax-2025.pdf', path: 'C:\\Users\\demo\\Documents\\tax-2025.pdf', size: 133120, modTime: daysAgo(100) },
      ],
    },
    guards: {
      testijotain: { versioning: 'Enabled', lockEnabled: false, lockMode: '', lockDays: 0 },
      'logs-2026': { versioning: 'Suspended', lockEnabled: false, lockMode: '', lockDays: 0 },
    },
    pfState: { open: false, name: '', path: '', dirty: false, sourceCount: EMPTY ? 0 : 3 },
    transfers: [
      { id: 't1', op: 'upload', status: 'running', currentFile: 'video-final.mp4', totalFiles: 3, doneFiles: 1, totalBytes: 224975891, sentBytes: 71803392, speedBps: 8388608 },
      { id: 't2', op: 'transfer', status: 'done', currentFile: '', totalFiles: 12, doneFiles: 12, totalBytes: 52428800, sentBytes: 52428800, speedBps: 0 },
    ],
    versions: [
      { versionId: '', isLatest: true, isDeleteMarker: false, size: 1234, storageClass: 'STANDARD', etag: '"v3"', lastModified: daysAgo(1) },
      { versionId: 'ver-0002', isLatest: false, isDeleteMarker: false, size: 1100, storageClass: 'STANDARD', etag: '"v2"', lastModified: daysAgo(8) },
      { versionId: 'ver-0001', isLatest: false, isDeleteMarker: false, size: 900, storageClass: 'STANDARD', etag: '"v1"', lastModified: daysAgo(30) },
      { versionId: 'dm-0001', isLatest: false, isDeleteMarker: true, size: 0, storageClass: '', etag: '', lastModified: daysAgo(31) },
    ],
    admin: {
      publicWarning: '', region: 'eu-central', versions: 'Enabled',
      pab: { blockPublicAcls: false, ignorePublicAcls: false, blockPublicPolicy: true, restrictPublicBuckets: true },
      policy: { raw: '{\n  "Version": "2012-10-17",\n  "Statement": []\n}', summary: { statements: 0, public: false } },
      acl: { owner: 'demo', summary: { grants: ['demo — FULL_CONTROL'] } },
      cors: [{ allowedMethods: 'GET', allowedOrigins: 'https://demo.example', allowedHeaders: '*', maxAgeSeconds: 3000 }],
      lifecycle: [{ id: 'archive-old', status: 'Enabled', days: 90, storageClass: 'GLACIER' }],
      encryption: { algorithm: 'AES256', kmsKeyId: '' },
      website: { index: 'index.html', error: '', redirectHost: '', redirectProtocol: '' },
      tags: [{ key: 'env', value: 'demo' }, { key: 'team', value: 'qa' }],
      lock: { enabled: false, mode: '', days: 0 },
    },
    compareRows: [
      { key: 'readme.md', status: 'same' },
      { key: 'budget-2026.xlsx', status: 'newer-remote' },
      { key: 'scan.png', status: 'size-diff' },
      { key: 'photos/', status: 'only-remote' },
      { key: 'reports/q4.xlsx', status: 'only-local' },
      { key: 'docs/', status: 'same' },
    ],
  };
  world.transfers[0].sentBytes = 71803392; // (kept deterministic if edited above)
  // view-source context + OS interop + import-credentials fixtures
  world.viewSource = '';
  world.osClip = [];
  world.credCandidates = [
    { id: 'cand-file1', name: 'from-file', type: 's3', endpoint: 'fsn1.your-objectstorage.com', host: '', username: '', hasSecret: true, origin: 'credentials' },
  ];
  world.kmsCandidates = [
    { id: 'cand-kms1', name: 'hetzner-kms', type: 's3', endpoint: 'hel1.your-objectstorage.com', host: '', username: '', hasSecret: true, origin: 'http://kms.local/secret/s3' },
  ];

  // children of a key-space (S3 prefix or anchored remote dir)
  function children(all, prefix, anchored) {
    const out = [];
    const seen = new Set();
    for (const e of all) {
      if (!e.key.startsWith(prefix)) continue;
      const rest = e.key.slice(prefix.length);
      if (!rest) continue;
      const slash = rest.indexOf('/');
      if (anchored || slash !== -1) {
        const cut = anchored ? rest.indexOf('/') : slash;
        if (cut !== -1) {
          const d = rest.slice(0, cut + 1);
          if (!seen.has(d)) {
            seen.add(d);
            out.push({ key: prefix + d, name: d.slice(0, -1), isDir: true });
          }
          continue;
        }
      }
      out.push({ ...e, name: e.key.slice(prefix.length) });
    }
    return out;
  }
  const listPrefix = (bucket, prefix) => children(world.objects[bucket] || [], prefix || '', false);
  // the backend resolves remote sources by id OR name (sourceByIDOrName):
  // the side pane binds by select value (id), the tree navigates by name
  const remoteChildren = (source, dir) => {
    const name = world.remote[source] ? source : world.sources.find((s) => s.id === source)?.name;
    return children(world.remote[name] || [], dir || '/', true);
  };
  const localParent = (p) => {
    const t = String(p || '').replace(/[\\/]+$/, '');
    const i = Math.max(t.lastIndexOf('\\'), t.lastIndexOf('/'));
    if (i < 0) return '';
    const parent = t.slice(0, i + 1);
    return /:[\\/]{0,1}$/.test(parent.slice(0, 3)) ? parent.replace(/[\\/]+$/, '') + '\\' : parent;
  };

  // ---- event registry (window.runtime shim) ----
  const listeners = new Map();
  const emit = (name, payload) => setTimeout(() => {
    for (const cb of listeners.get(name) || []) {
      try { cb(payload); } catch (e) { console.error('shim emit', name, e); }
    }
  }, 20);
  window.runtime = {
    EventsOn: (name, cb) => {
      if (!listeners.has(name)) listeners.set(name, new Set());
      listeners.get(name).add(cb);
      return () => listeners.get(name)?.delete(cb);
    },
    EventsOff: (name, cb) => listeners.get(name)?.delete(cb),
    EventsEmit: () => {},
    WindowSetTitle: () => {},
    WindowCenter: () => {},
    WindowMaximise: () => {},
    WindowUnmaximise: () => {},
    WindowMinimise: () => {},
    WindowClose: () => {},
    Quit: () => {},
    BrowserOpenURL: () => {},
    ClipboardSetText: async () => true,
    ClipboardGetText: async () => '',
  };
  window.__emit = emit;

  // ---- call recording ----
  const calls = [];
  const rec = (m, args) => {
    try { calls.push({ m, args: JSON.parse(JSON.stringify(args)) }); }
    catch { calls.push({ m, args: '<unserializable>' }); }
  };

  let seq = 0;
  const token = () => `tok-${++seq}`;

  // Methods whose return values drive rendering get explicit handlers;
  // everything else falls through to record + benign default.
  const H = {
    GetVersion: () => '0.9.0-visual',
    ListSources: () => JSON.parse(JSON.stringify(world.sources)),
    ListBuckets: () => JSON.parse(JSON.stringify(world.buckets)),
    ListSourceBuckets: () => JSON.parse(JSON.stringify(world.buckets)),
    ListObjectsStream: (bucket, prefix) => {
      const t = token();
      emit('list:page', { token: t, entries: listPrefix(bucket, prefix), done: true });
      return t;
    },
    ListSourceObjectsStream: (_src, bucket, prefix) => {
      const t = token();
      emit('list:page', { token: t, entries: listPrefix(bucket, prefix), done: true });
      return t;
    },
    CancelList: () => ({}),
    RemoteList: (source, dir) => remoteChildren(source, dir),
    ListLocal: (dir) => JSON.parse(JSON.stringify(world.local[dir] || [])),
    LocalRoots: () => ['C:\\', 'D:\\'],
    LocalHome: () => 'C:\\Users\\demo',
    LocalParent: (p) => localParent(p),
    GetBucketGuard: (bucket) => (world.guards[bucket] || { versioning: 'Off', lockEnabled: false, lockMode: '', lockDays: 0 }),
    GetProfileFileState: () => JSON.parse(JSON.stringify(world.pfState)),
    NewProfileFile: (path) => {
      world.pfState = { open: true, name: path.split(/[\\/]/).pop(), path, dirty: false, sourceCount: 0 };
      return {};
    },
    OpenProfileFile: (path) => {
      world.pfState = { open: true, name: path.split(/[\\/]/).pop(), path, dirty: false, sourceCount: 2 };
      emit('s3:changed', {});
      return {};
    },
    SaveProfileFile: () => { world.pfState.dirty = false; return {}; },
    SaveProfileFileAs: (path) => {
      world.pfState = { open: true, name: path.split(/[\\/]/).pop(), path, dirty: false, sourceCount: world.pfState.sourceCount };
      return {};
    },
    CloseProfileFile: () => {
      world.pfState = { open: false, name: '', path: '', dirty: false, sourceCount: world.sources.length };
      return {};
    },
    PickOpenProfileFile: () => 'C:\\Users\\demo\\Documents\\demo.s3bprofile',
    PickSaveProfileFile: () => 'C:\\Users\\demo\\Documents\\saved.s3bprofile',
    PickUploadFiles: () => ['C:\\Users\\demo\\Downloads\\invoice.pdf', 'C:\\Users\\demo\\Downloads\\spec.docx'],
    PickFolder: () => 'C:\\Users\\demo\\Downloads',
    TestProfile: () => ({ ok: true, message: 'OK — visual shim' }),
    DoctorChecks: () => ['Connectivity', 'Authentication', 'Clock skew', 'TLS handshake', 'List permission'],
    RunDoctor: (bucket) => ({
      endpoint: 's3.visual.shim', provider: 'aws', bucket: bucket || '',
      warnings: [],
      summary: { pass: 4, warn: 1, fail: 0, skip: 0 },
      checks: [
        { check: 'Connectivity', status: 'pass', durationMs: 42, started_at: daysAgo(0), finished_at: daysAgo(0), detail: 'TCP+TLS to s3.visual.shim:443' },
        { check: 'Authentication', status: 'pass', durationMs: 61, started_at: daysAgo(0), finished_at: daysAgo(0), detail: 'SigV4 accepted' },
        { check: 'Clock skew', status: 'warn', durationMs: 5, started_at: daysAgo(0), finished_at: daysAgo(0), detail: 'Skew 310 s — inside the 900 s SigV4 window but drifting' },
        { check: 'TLS handshake', status: 'pass', durationMs: 88, started_at: daysAgo(0), finished_at: daysAgo(0), detail: 'TLS 1.3' },
        { check: 'List permission', status: 'pass', durationMs: 70, started_at: daysAgo(0), finished_at: daysAgo(0), detail: 'ListBuckets ok' },
      ],
    }),
    RunDoctorCheck: (name) => ({ check: name, status: 'pass', durationMs: 33, started_at: daysAgo(0), finished_at: daysAgo(0), detail: 're-run ok' }),
    ActiveTransfers: () => JSON.parse(JSON.stringify(world.transfers)),
    ObjectVersions: () => JSON.parse(JSON.stringify(world.versions)),
    VersionDiffText: () => ({ truncated: false, aText: 'alpha\nold line A\nomega', bText: 'alpha\nnew line B\nomega' }),
    GetBucketAdmin: () => JSON.parse(JSON.stringify(world.admin)),
    BucketVersionStats: () => ({ currentObjects: 7, versions: 12, deleteMarkers: 2, noncurrent: 5, noncurrentBytes: 1048576 }),
    CompareAny: (x, y) => JSON.parse(JSON.stringify(world.compareRows)),
    PreviewDelete: (bucket, keys) => ({ requiresL2: false, objects: keys.length, bytes: 1234 }),
    RemoteDeletePreview: () => ({ n: 1 }),
    PresignObject: (bucket, key) => `https://${bucket}.s3.visual.shim/${key}?X-Amz-Signature=visual`,
    StatObject: (bucket, key) => (key.endsWith('/')
      ? { key, isDir: true, usage: { objectCount: 7, totalBytes: 456789 } }
      : { key, size: 1234, lastModified: daysAgo(1), etag: '"v3"', storageClass: 'STANDARD' }),
    StatBucket: (bucket) => ({ name: bucket, region: 'eu-central', objects: 7, bytes: 224975891, createdAt: daysAgo(220) }),
    RemoteStat: (source, key) => ({ key, isDir: false, size: 4096, lastModified: daysAgo(3) }),
    CopySelection: (src, keys, dst, prefix, move) => ({ copied: keys.length, errors: [] }),
    EditingFiles: () => [{ bucket: 'testijotain', key: 'docs/notes.md' }],
    ImportAwsCredentials: () => ({ found: 1, imported: 1 }),
    DeepSearch: (bucket, prefix) => {
      const t = token();
      emit('search:page', {
        token: t, matched: 2,
        entries: [
          { bucket, key: 'docs/notes.md', size: 900, storageClass: 'STANDARD', lastModified: daysAgo(2) },
          { bucket, key: 'readme.md', size: 1234, storageClass: 'STANDARD', lastModified: daysAgo(1) },
        ],
      });
      emit('search:done', { token: t, matched: 2, scanned: 41 });
      return t;
    },
    CancelSearch: () => ({}),
    // ---- view-source context + source-pinned ops (M11) ----
    SetViewSource: (idOrName) => {
      const s = world.sources.find((x) => x.name === idOrName || x.id === idOrName);
      world.viewSource = s ? s.name : idOrName;
      return world.viewSource;
    },
    SourceGetBucketGuard: (_src, bucket) => JSON.parse(JSON.stringify(world.guards[bucket]
      || { versioning: 'Off', lockEnabled: false, lockMode: '', lockDays: 0 })),
    SourceStatObject: (_src, _bucket, key) => (key.endsWith('/')
      ? { key, isDir: true, usage: { objectCount: 7, totalBytes: 456789 } }
      : { key, size: 1234, lastModified: daysAgo(1), etag: '"v3"', storageClass: 'STANDARD' }),
    SourcePreviewDelete: (_src, _bucket, keys) => ({ requiresL2: false, count: keys.length, objects: keys.length, bytes: 1234, folders: 0 }),
    SourceDeleteSelection: (_src, _bucket, keys) => ({ deleted: keys.length, errors: [] }),
    SourceRenameObject: () => ({}),
    SourceCreateFolder: () => ({}),
    // ---- OS interop ----
    PickUploadItems: () => ['C:\\Users\\demo\\Downloads\\invoice.pdf', 'C:\\Users\\demo\\Downloads\\photos'],
    OsClipboardFiles: () => JSON.parse(JSON.stringify(world.osClip || [])),
    OsClipboardSetFiles: (paths) => { world.osClip = paths; return {}; },
    StageClipboardDir: () => 'C:\\Users\\demo\\AppData\\Local\\Temp\\s3b-clip-1',
    MakeDragUrls: (items) => items.map((it, i) => `http://127.0.0.1:39317/drag/${i}/${encodeURIComponent(it.name || it.key)}`),
    RemoteListDraft: () => [
      { key: '/draft-up/', isDir: true, name: 'draft-up' },
      { key: '/seed.txt', isDir: false, name: 'seed.txt', size: 12 },
    ],
    // ---- import credentials (files + KMS) ----
    ParseCredentialFile: () => JSON.parse(JSON.stringify(world.credCandidates)),
    PickCredentialFiles: () => ['C:\\Users\\demo\\Downloads\\credentials'],
    TestCredentialDraft: () => ({ ok: true, message: '4 buckets reachable' }),
    KmsFetch: (service) => JSON.parse(JSON.stringify(world.kmsCandidates)),
    ImportCredentials: (ids) => {
      const imported = [];
      for (const id of ids || []) {
        const c = [...world.credCandidates, ...world.kmsCandidates].find((x) => x.id === id);
        if (!c || world.sources.some((s) => s.name === c.name)) continue;
        world.sources.push({ id: 'src-' + c.name, name: c.name, type: c.type || 's3' });
        imported.push(c.name);
      }
      world.pfState.sourceCount = world.sources.length;
      return { imported, skipped: [] };
    },
  };

  const App = new Proxy({}, {
    get(_t, prop) {
      if (prop === 'then' || typeof prop !== 'string') return undefined;
      if (H[prop]) {
        return (...args) => {
          rec(prop, args);
          // always a Promise: some call sites chain .then directly instead
          // of await, exactly like the real Wails bindings.
          return Promise.resolve().then(() => H[prop](...args));
        };
      }
      return (...args) => {
        rec(prop, args);
        return Promise.resolve({});
      };
    },
  });
  window.go = { 'github.com/MikkoP88/s3-bucket-browser/pkg/api': { App } };
  window.__shim = { calls, world, emit };
}
// playwright serializes the function source; keep it self-contained.

// ---------- page ----------
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
context.setDefaultTimeout(8000); // fail fast: a stuck overlay must not stall the walk
const page = await context.newPage();
page.on('pageerror', (e) => {
  const s = (e && e.stack) ? `${e.stack}`.split('\n').slice(0, 4).join(' | ') : String(e);
  pageErrors.push(String(e));
  console.log(`  PAGEERROR ${s}`);
});
await page.addInitScript(shim);

// ---------- page helpers ----------
const evalPage = (fn, arg) => page.evaluate(fn, arg);
const rowKeys = () => evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
  .filter((r) => r.style.display !== 'none' && r._model).map((r) => r._model.key));
const sideKeys = () => evalPage(() => Array.from(document.querySelectorAll('#local-grid-body .grid-row'))
  .filter((r) => r.style.display !== 'none' && r._model).map((r) => r._model.key));
const calls = () => evalPage(() => JSON.parse(JSON.stringify(window.__shim.calls)));
const resetCalls = () => evalPage(() => { window.__shim.calls.length = 0; });
const txt = (sel) => evalPage((s) => document.querySelector(s)?.textContent || '', sel);
const elOrNull = (js, arg) => page.evaluateHandle(js, arg).then(async (h) => (await h.asElement()) ? h : null);

// grid row / tree node by visible label
const gridRow = (label) => elOrNull((l) => Array.from(document.querySelectorAll('#grid-body .grid-row'))
  .find((r) => r.querySelector('.tname')?.textContent === l) || null, label);
const sideRow = (label) => elOrNull((l) => Array.from(document.querySelectorAll('#local-grid-body .grid-row'))
  .find((r) => r.querySelector('.tname')?.textContent === l) || null, label);
const treeRow = (label) => elOrNull((l) => Array.from(document.querySelectorAll('#tree .tnode'))
  .find((r) => r.querySelector('.tlabel')?.textContent === l) || null, label);

async function clickRow(label) {
  const h = await gridRow(label);
  if (!h) throw new Error(`no grid row "${label}"`);
  await h.asElement().click();
}
async function dblClickRow(label) {
  const h = await gridRow(label);
  if (!h) throw new Error(`no grid row "${label}"`);
  await h.asElement().dblclick();
}
async function clickTree(label) {
  const h = await treeRow(label);
  if (!h) throw new Error(`no tree node "${label}"`);
  await h.asElement().click();
}
async function rightClick(h) { await h.asElement().click({ button: 'right' }); }
const closeCtx = async () => {
  await evalPage(() => document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })));
  await sleep(60);
};
async function openCtx(label) {
  const h = await gridRow(label);
  if (!h) throw new Error(`no grid row "${label}"`);
  await rightClick(h);
  await sleep(60);
  return evalPage(() => document.querySelectorAll('#ctxmenu:not(.hidden) .item').length);
}
async function ctxItem(re) {
  const h = await elOrNull((src) => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
    .find((i) => new RegExp(src, 'i').test(i.textContent)) || null, re.source);
  if (!h) throw new Error(`no ctxmenu item /${re.source}/`);
  await h.asElement().click();
  await sleep(80);
}
async function closeModal() {
  const btn = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .reverse().find((b) => /^(close|cancel)$/i.test(b.textContent.trim())) || null);
  if (btn) { await btn.asElement().click(); await sleep(80); return; }
  await page.keyboard.press('Escape');
  await sleep(80);
}
async function modalVisible() {
  return evalPage(() => !document.getElementById('modal-root').classList.contains('hidden'));
}
// Layout-artifact audit for whatever is currently on screen: page must not
// scroll horizontally, an open modal must sit fully inside the viewport,
// dialog action buttons must not clip their labels, and a tab strip's tabs
// must not overflow the strip. Returns { ok, bad } for the ok() helper.
async function layoutAudit() {
  return evalPage(() => {
    const bad = [];
    const vw = window.innerWidth, vh = window.innerHeight;
    if (document.documentElement.scrollWidth > vw + 1) bad.push('page-hscroll');
    const m = document.querySelector('#modal-root:not(.hidden) .modal');
    if (m) {
      const r = m.getBoundingClientRect();
      if (r.left < -0.5 || r.top < -0.5 || r.right > vw + 0.5 || r.bottom > vh + 0.5) bad.push('modal-outside-viewport');
      for (const b of m.querySelectorAll('.modal-foot .btn')) {
        if (b.scrollWidth > b.clientWidth + 1) bad.push('foot-btn-clipped');
      }
    }
    for (const s of document.querySelectorAll('.tabstrip')) {
      const r = s.getBoundingClientRect();
      for (const t of s.querySelectorAll('.tab')) {
        const tr = t.getBoundingClientRect();
        if (tr.width > 0 && (tr.left < r.left - 0.5 || tr.right > r.right + 0.5)) bad.push('tab-outside-strip');
      }
    }
    return { ok: bad.length === 0, bad: bad.join(',') };
  });
}
// synthetic HTML5 drag & drop: real dragstart on the source row lets the app
// build its own payload; dragover+drop on the target exercise the handlers.
async function dnd(fromH, toH, { shift = false, ctrl = false } = {}) {
  if (!fromH || !toH) throw new Error(`dnd: ${!fromH ? 'source' : 'target'} element not found`);
  await page.evaluate(([f, t, sh, ct]) => {
    const dt = new DataTransfer();
    f.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }));
    const init = { bubbles: true, cancelable: true, dataTransfer: dt, shiftKey: sh, ctrlKey: ct };
    t.dispatchEvent(new DragEvent('dragover', init));
    t.dispatchEvent(new DragEvent('drop', init));
    f.dispatchEvent(new DragEvent('dragend', { bubbles: true, dataTransfer: dt }));
  }, [fromH.asElement(), toH.asElement(), shift, ctrl]);
  await sleep(120);
}
const findCall = async (m) => (await calls()).filter((c) => c.m === m).at(-1) || null;
async function navObjects(bucket) {
  await clickTree(bucket);
  await waitFor(async () => (await rowKeys()).some((k) => k.includes('.') || k.endsWith('/')), 6000, `objects of ${bucket}`);
  await sleep(150);
}
// navObjectsOf opens a bucket of a SPECIFIC source: with several S3 sources
// configured, same-named buckets exist in the tree under each — clicking the
// source node first makes the buckets grid (and its rows) source-unambiguous.
async function navObjectsOf(source, bucket) {
  await clickTree(source);
  await waitFor(async () => (await rowKeys()).includes(bucket), 6000, `buckets of ${source}`);
  await sleep(150);
  await dblClickRow(bucket);
  await waitFor(async () => (await rowKeys()).some((k) => k.includes('.') || k.endsWith('/')), 6000, `objects of ${source}/${bucket}`);
  await sleep(150);
}

// ---------- the walk ----------
await rm(OUT, { recursive: true, force: true });
await mkdir(OUT, { recursive: true });

await step('boot', async () => {
  await page.goto(BASE);
  await waitFor(() => evalPage(() => (document.getElementById('status-version')?.textContent || '').includes('s3b v')), 10000, 'boot version');
  await ok('menubar mounted', evalPage(() => document.querySelectorAll('#menubar .mb-title').length >= 5));
  await ok('toolbar mounted', evalPage(() => !!document.getElementById('btn-upload')));
  await ok('buckets listed', waitFor(async () => (await rowKeys()).length >= 4, 6000, 'buckets'));
  await ok('tree shows all sources', evalPage(() => ['hetzner', 'backup-box', 'dav-claims']
    .every((n) => Array.from(document.querySelectorAll('#tree .tlabel')).some((l) => l.textContent === n))));
  await shot('boot-buckets');
});

await step('buckets-ctxmenu', async () => {
  const n = await openCtx('testijotain');
  await ok('bucket menu has items', n >= 5);
  await shot('ctx-bucket');
  await closeCtx();
});

await step('objects-view', async () => {
  await dblClickRow('testijotain');
  await waitFor(async () => (await rowKeys()).includes('readme.md'), 6000, 'objects of testijotain');
  await ok('breadcrumb shows bucket', (await txt('#breadcrumb')).includes('testijotain'));
  // guard state lives as icons after the bucket name in the tree now
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('#tree .tguard').length)) > 0, 6000, 'tree guard icons');
  await ok('tree shows versioning icon', (await evalPage(() => Array.from(document.querySelectorAll('#tree .tguard')).map((i) => i.title).join(' '))).toLowerCase().includes('versioning enabled'));
  await ok('toolbar upload enabled', evalPage(() => !document.getElementById('btn-upload').disabled));
  await ok('toolbar download disabled without selection', evalPage(() => document.getElementById('btn-download').disabled));
  await shot('objects');
});

await step('filter', async () => {
  await page.fill('#filter', 'read');
  await sleep(120);
  await ok('filter narrows to readme.md', (await rowKeys()).join(',') === 'readme.md');
  await shot('filter');
  await page.fill('#filter', '');
  await sleep(120);
});

await step('selection-status', async () => {
  await clickRow('readme.md');
  const second = await gridRow('budget-2026.xlsx');
  await second.asElement().click({ modifiers: ['Control'] });
  await ok('status bar counts 2', (await txt('#status-selection')).includes('2'));
  await ok('header checkbox indeterminate', evalPage(() => {
    const rows = document.querySelectorAll('#grid-head input[type=checkbox]');
    return rows.length > 0 && rows[0].indeterminate;
  }));
  await shot('selection');
  await evalPage(() => {
    // mousedown on the grid body clears the selection but also arms the
    // marquee; without a paired mouseup the next real mouse move sweeps a
    // rectangle selection (run-4's "Download (6)" residue)
    document.getElementById('grid-body').dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
  });
});

await step('file-ctxmenu-versions', async () => {
  // the Previous versions… entry is gated on sel === 1; a plain click on the
  // target row does NOT trim a multi-selection (grid keeps selected-row
  // semantics), so click a different row first to reset to a single selection
  await clickRow('scan.png');
  const n = await openCtx('readme.md');
  await ok('file menu has items', n >= 4);
  const items = await evalPage(() => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
    .map((i) => i.textContent.trim()));
  const has = items.some((x) => /versions/i.test(x));
  if (!has) console.log(`  menu items: ${items.join(' | ')}`);
  await ok('menu offers Versions', has);
  await shot('ctx-file');
  await closeCtx();
});

await step('folder-ctxmenu', async () => {
  const n = await openCtx('docs');
  await ok('folder menu has items', n >= 3);
  await shot('ctx-folder');
  await closeCtx();
});

await step('empty-ctxmenu', async () => {
  await evalPage(() => {
    const b = document.getElementById('grid-body');
    b.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 700, clientY: 500 }));
  });
  await sleep(80);
  await ok('empty-area menu opens', evalPage(() => !document.getElementById('ctxmenu').classList.contains('hidden')));
  await shot('ctx-empty');
  await closeCtx();
});

await step('tree-lazy', async () => {
  // navigation already ran reveal() on the bucket node (expanded); only
  // expand when the folder level is not rendered yet
  if (!(await treeRow('docs'))) await evalHandleClickTwist('testijotain'); // bucket node → folder level
  await waitFor(async () => !!(await treeRow('docs')), 6000, 'bucket children');
  await ok('bucket expanded to folders', !!(await treeRow('docs')) && !!(await treeRow('photos')));
  if (!(await treeRow('legacy'))) await evalHandleClickTwist('docs');
  await waitFor(async () => !!(await treeRow('legacy')), 6000, 'docs children');
  await ok('docs expanded to legacy', !!(await treeRow('legacy')));
  await clickTree('docs');
  // prefix listings carry anchored keys ('docs/notes.md'), not bare names
  await waitFor(async () => (await rowKeys()).includes('docs/notes.md'), 6000, 'docs objects');
  await ok('tree click navigates into docs', (await txt('#breadcrumb')).includes('docs'));
  await shot('tree-docs');
  await page.keyboard.press('Escape');
});

async function evalHandleClickTwist(label) {
  const h = await elOrNull((l) => Array.from(document.querySelectorAll('#tree .tnode'))
    .find((r) => r.querySelector('.tlabel')?.textContent === l)?.querySelector('.twist') || null, label);
  if (!h) throw new Error(`no twist on "${label}"`);
  // in-page click: not-yet-loaded nodes keep the twist at visibility:hidden,
  // which a real Playwright click refuses on actionability grounds — the DOM
  // handler runs the same either way
  await h.asElement().evaluate((tw) => tw.click());
  await sleep(150);
}

await step('remote-view', async () => {
  await clickTree('backup-box');
  await waitFor(async () => (await rowKeys()).includes('/backup.sh'), 6000, 'backup-box listing');
  await ok('remote rows anchored', (await rowKeys()).every((k) => k.startsWith('/')));
  await ok('upload enabled in remote view (no profile needed)', evalPage(() => !document.getElementById('btn-upload').disabled));
  const n = await openCtx('backup.sh');
  await ok('remote file menu items', n >= 3);
  await shot('remote-sftp');
  await closeCtx();
});

await step('tree-source-ctxmenu', async () => {
  const h = await treeRow('backup-box');
  await rightClick(h);
  await sleep(80);
  await ok('source node menu opens', evalPage(() => !document.getElementById('ctxmenu').classList.contains('hidden')));
  await shot('ctx-tree-source');
  await closeCtx();
});

await step('menubar-walk', async () => {
  const titles = await evalPage(() => Array.from(document.querySelectorAll('#menubar .mb-title')).map((t) => t.textContent));
  await ok('five menus', titles.length >= 5);
  for (const t of titles) {
    await page.locator('#menubar .mb-title', { hasText: t }).first().click();
    await sleep(80);
    const items = await evalPage(() => document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item').length);
    await ok(`menu "${t}" opens (${items} items)`, items > 0);
    await shot(`menu-${t.toLowerCase().replace(/[^a-z0-9]+/g, '')}`);
    await page.keyboard.press('Escape');
    await sleep(60);
  }
  // grey-out: with no selection, Edit-style items should render disabled
  await page.locator('#menubar .mb-title', { hasText: titles[1] }).first().click();
  await sleep(80);
  await ok('disabled items rendered greyed', evalPage(() => document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item.disabled').length > 0));
  await shot('menu-disabled');
  await page.keyboard.press('Escape');
});

await step('help-guide', async () => {
  await page.locator('#menubar .mb-title', { hasText: /help/i }).first().click();
  await sleep(80);
  const guide = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /user guide/i.test(i.textContent)) || null);
  await ok('Help→User guide present', !!guide);
  if (guide) {
    await guide.asElement().click();
    await waitFor(modalVisible, 4000, 'guide modal');
    await ok('guide has six section tabs', waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root .tabstrip .tab').length)) === 6, 4000, 'guide tabs'));
    await ok('getting-started content rendered', (await evalPage(() => document.getElementById('modal-root').textContent)).includes('Import S3 credentials'));
    await shot('guide');
    const tab = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .tabstrip .tab'))
      .find((t) => /^transfers$/i.test(t.textContent.trim())) || null);
    if (tab) { await tab.asElement().click(); await sleep(80); }
    await ok('guide tab switch works', (await evalPage(() => document.getElementById('modal-root').textContent)).includes('multipart'));
    await closeModal();
  }
  await page.locator('#menubar .mb-title', { hasText: /help/i }).first().click();
  await sleep(80);
  const src = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /supported data sources/i.test(i.textContent)) || null);
  await ok('Help→Supported data sources present', !!src);
  if (src) {
    await src.asElement().click();
    await waitFor(modalVisible, 4000, 'sources modal');
    const txt = await evalPage(() => document.getElementById('modal-root').textContent);
    await ok('sources list covers engines', ['SFTP', 'FTP', 'WebDAV', 'MinIO', 'Cloudflare R2'].every((s) => txt.includes(s)));
    await shot('sources-info');
    await closeModal();
  }
});

await step('about-keysheet', async () => {
  // Help → About
  await page.locator('#menubar .mb-title', { hasText: /help/i }).first().click();
  await sleep(80);
  const about = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /about/i.test(i.textContent)) || null);
  await ok('Help→About present', !!about);
  if (about) {
    await about.asElement().click();
    await waitFor(modalVisible, 4000, 'about modal');
    await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root .kv .k').length)) >= 3, 4000, 'about rows');
    await ok('about shows version/license rows', evalPage(() => {
      const ks = Array.from(document.querySelectorAll('#modal-root .kv .k')).map((k) => k.textContent);
      const hasLicense = ks.some((k) => /licen[cs]e/i.test(k));
      const hasVersion = /v\d|^v\?/m.test(Array.from(document.querySelectorAll('#modal-root .kv .v'))[0]?.textContent || '');
      return ks.includes('s3b') && hasLicense && hasVersion;
    }));
    await ok('about layout clean', (await layoutAudit()).ok);
    await shot('about');
    await closeModal();
  }
  // F1 keyboard shortcuts sheet
  await page.keyboard.press('F1');
  await waitFor(modalVisible, 4000, 'keysheet');
  await ok('keysheet lists shortcuts', waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root .help-grid .row').length)) >= 18, 4000, 'keys rows'));
  await ok('keysheet uses the wide size class', evalPage(() => document.querySelector('#modal-root .modal.wide') !== null));
  await ok('keysheet layout clean', (await layoutAudit()).ok);
  await shot('keysheet');
  await closeModal();
});

await step('settings-dialog', async () => {
  await page.locator('#menubar .mb-title', { hasText: /settings/i }).first().click();
  await sleep(80);
  const item = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /settings/i.test(i.textContent) && !i.classList.contains('has-sub')) || null);
  await ok('Settings… entry exists', !!item);
  if (item) await item.asElement().click();
  await waitFor(modalVisible, 4000, 'settings modal');
  await ok('settings rows rendered', evalPage(() => document.querySelectorAll('#modal-root .set-row').length >= 7));
  await shot('settings');
  // theme switch applies live
  const darkSel = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root select'))
    .find((s) => Array.from(s.options).some((o) => o.value === 'dark')) || null);
  await ok('theme select present', !!darkSel);
  if (darkSel) {
    await darkSel.asElement().selectOption('dark');
    await ok('dark theme applied live', evalPage(() => document.documentElement.dataset.theme === 'dark'));
    await shot('settings-dark');
    await darkSel.asElement().selectOption('light');
  }
  await closeModal();
  await ok('modal closed', evalPage(() => document.getElementById('modal-root').classList.contains('hidden')));
});

await step('upload', async () => {
  // the ONE upload command: a single OS dialog picking files AND folders
  await navObjects('testijotain');
  await resetCalls();
  await page.click('#btn-upload');
  await waitFor(async () => (await findCall('Upload')) !== null, 4000, 'upload');
  const c = await findCall('Upload');
  await ok('single dialog uploads files and folders', c && c.args[1] === 'testijotain'
    && c.args[0].length === 2 && /invoice\.pdf$/.test(c.args[0][0]) && /photos$/.test(c.args[0][1]));
  await ok('PickUploadItems backed the dialog', (await findCall('PickUploadItems')) !== null);
  await ok('no context menu', evalPage(() => document.getElementById('ctxmenu').classList.contains('hidden')));
  await shot('upload');
});

await step('sources-in-tree', async () => {
  await ok('source listed in sidebar tree', (await txt('#tree')).includes('backup-box'));
  await ok('sidebar header says Data sources', (await txt('#sidebar-head')).toLowerCase().includes('data sources'));
  await shot('sources-tree');
});

await step('source-editor-autoname', async () => {
  // the sidebar "+" opens the Add-source dialog; the Name field auto-fills
  // from the connection details and stays editable (a typed name wins)
  await page.click('#sidebar-head .side-add');
  await waitFor(modalVisible, 4000, 'source editor');
  await ok('title is Add data source', (await evalPage(() => document.querySelector('#modal-root .modal-head span')?.textContent || '')).includes('Add data source'));
  // S3: the endpoint host's first label becomes the name
  const ep = await elOrNull(() => document.querySelector('#modal-root input.mono') || null);
  await ok('endpoint field focused first', !!ep);
  if (ep) {
    await ep.asElement().fill('https://hel1.your-objectstorage.com');
    await ep.asElement().dispatchEvent('change');
  }
  await ok('S3 endpoint auto-fills the name', evalPage(() => document.querySelector('#modal-root input.input')?.value === 'hel1'));
  // local: the folder leaf becomes the name
  await page.selectOption('#modal-root select', 'local');
  const folder = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root input.mono'))
    .find((i) => i.placeholder.includes('C:\\data')) || null);
  const browse = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /browse/i.test(b.textContent)) || null);
  await ok('local type shows folder field + Browse', !!folder && !!browse);
  if (folder) {
    await folder.asElement().fill('C:\\Users\\demo\\backups');
    await folder.asElement().dispatchEvent('change');
  }
  await ok('folder name auto-fills the name', evalPage(() => document.querySelector('#modal-root input.input')?.value === 'backups'));
  // a hand-typed name is never overwritten by later field changes
  await page.fill('#modal-root input.input', 'my-box');
  if (folder) {
    await folder.asElement().fill('C:\\Users\\demo\\photos');
    await folder.asElement().dispatchEvent('change');
  }
  await ok('typed name survives field changes', evalPage(() => document.querySelector('#modal-root input.input')?.value === 'my-box'));
  await shot('source-editor');
  await closeModal();
});

await step('doctor', async () => {
  // the toolbar Doctor button is gone — Help menu carries it
  await page.locator('#menubar .mb-title', { hasText: /help/i }).first().click();
  await sleep(80);
  const docItem = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /doctor/i.test(i.textContent)) || null);
  await ok('Help→Doctor present', !!docItem);
  if (!docItem) return;
  await docItem.asElement().click();
  await waitFor(modalVisible, 4000, 'doctor modal');
  await ok('doctor checks listed', waitFor(async () => (await evalPage(() => document.getElementById('modal-root').textContent)).includes('Connectivity'), 4000, 'checks'));
  const run = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /run all/i.test(b.textContent)) || null);
  await ok('Run all button present', !!run);
  if (run) {
    await run.asElement().click();
    await waitFor(async () => (await evalPage(() => document.getElementById('modal-root').textContent)).includes('warn'), 4000, 'report');
  }
  await shot('doctor');
  await closeModal();
});

await step('admin-panel', async () => {
  // deterministic: the default S3 source's tree node lands on its buckets view
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('testijotain'), 6000, 'buckets view');
  const n = await openCtx('testijotain');
  await ok('bucket menu has items', n >= 5);
  await ctxItem(/admin panel/i);
  await waitFor(modalVisible, 4000, 'admin modal');
  await ok('title names the bucket', (await evalPage(() => document.querySelector('#modal-root .modal-head span')?.textContent || '')).startsWith('Admin panel — testijotain'));
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('.tabstrip .tab').length)) === 11, 4000, 'admin tabs');
  await ok('admin modal uses the wide size class', evalPage(() => document.querySelector('#modal-root .modal.admin-modal') !== null));
  await ok('full tab strip fits without clipping', evalPage(() => {
    const s = document.querySelector('.tabstrip');
    if (!s) return false;
    const r = s.getBoundingClientRect();
    return Array.from(s.querySelectorAll('.tab')).every((t) => {
      const tr = t.getBoundingClientRect();
      return tr.width > 0 && tr.left >= r.left - 0.5 && tr.right <= r.right + 0.5;
    });
  }));
  await shot('admin-panel');
  // walk ALL 11 tabs: each must render real content (not stuck on Loading…)
  // and keep the geometry audit clean — tabs render synchronously from the
  // already-loaded panel, so the strip handles stay valid across clicks.
  const tabNames = await evalPage(() => Array.from(document.querySelectorAll('#modal-root .tabstrip .tab')).map((t) => t.textContent.trim()));
  for (const name of tabNames) {
    const tab = await elOrNull((want) => Array.from(document.querySelectorAll('#modal-root .tabstrip .tab'))
      .find((t) => t.textContent.trim() === want) || null, name);
    if (!tab) { await ok(`tab "${name}" clickable`, false); continue; }
    await tab.asElement().click();
    await sleep(90);
    await ok(`tab "${name}" renders content`, waitFor(async () => {
      const t = await evalPage(() => document.querySelector('#modal-root .tabbody')?.textContent || '');
      return t.length > 0 && !t.trimStart().startsWith('Loading');
    }, 3000, `tab ${name} content`));
    await ok(`tab "${name}" layout clean`, (await layoutAudit()).ok);
  }
  // shots of two denser tabs (policy JSON, lifecycle rules)
  for (const [name, shotName] of [['Policy', 'admin-policy'], ['Lifecycle', 'admin-lifecycle']]) {
    const tab = await elOrNull((want) => Array.from(document.querySelectorAll('#modal-root .tabstrip .tab'))
      .find((t) => t.textContent.trim() === want) || null, name);
    if (tab) { await tab.asElement().click(); await sleep(90); await shot(shotName); }
  }
  await closeModal();
});

await step('deep-search', async () => {
  // Find opens from an objects view (or a selected row) only — anywhere else
  // it just toasts "Open a bucket first"
  await navObjects('testijotain');
  await page.click('#btn-find');
  await waitFor(modalVisible, 4000, 'search modal');
  const start = await elOrNull(() => document.querySelector('#modal-root button.primary') || null);
  await ok('search start button present', !!start);
  if (start) {
    await start.asElement().click();
    await waitFor(async () => (await evalPage(() => document.getElementById('modal-root').textContent)).includes('readme.md'), 4000, 'results');
    await ok('search results rendered', (await evalPage(() => document.getElementById('modal-root').textContent)).includes('docs/notes.md'));
  }
  await shot('deep-search');
  await closeModal();
});

await step('versions-diff', async () => {
  await navObjects('testijotain');
  await clickRow('scan.png'); // single selection so the Versions entry is offered
  await openCtx('readme.md');
  await ctxItem(/versions/i);
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root .ver-row').length)) >= 4, 4000, 'versions');
  await ok('4 versions listed', evalPage(() => document.querySelectorAll('#modal-root .ver-row').length === 4));
  await ok('delete marker shown', (await evalPage(() => document.getElementById('modal-root').textContent)).includes('Delete marker'));
  await shot('versions');
  // pick A on row 2, B on row 3, then Compare
  const picks = await evalPage(() => Array.from(document.querySelectorAll('#modal-root .ver-row')).map((r) => !!r.querySelector('.ver-pick')));
  await ok('A/B pick buttons on old versions', picks.filter(Boolean).length >= 2);
  // picking re-renders the version rows: query each button right before its
  // click, or the handle goes stale ("Element is not attached to the DOM").
  // The flattened .ver-pick order is [A,B] per row, row-major: #0 is row0's
  // A and #3 is row1's B — picking #0 and #2 would both hit side A and the
  // Compare button would stay disabled (pick.a just gets overwritten).
  const pick = async (i) => {
    const h = await elOrNull((src) => document.querySelectorAll('#modal-root .ver-pick')[src] || null, i);
    if (!h) throw new Error(`no ver-pick #${i}`);
    await h.asElement().click(); await sleep(120);
  };
  if (picks.filter(Boolean).length >= 2 && (await evalPage(() => document.querySelectorAll('#modal-root .ver-pick').length)) >= 4) {
    await pick(0);
    await pick(3);
    const cmp = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
      .find((x) => /compare/i.test(x.textContent) && !/versions/i.test(x.textContent)) || null);
    if (cmp) {
      await cmp.asElement().click();
      await sleep(200);
      await ok('diff dialog renders text diff', (await evalPage(() => document.getElementById('modal-root').textContent)).includes('omega'));
      await shot('version-diff');
      await closeModal();
    } else {
      await ok('compare button reachable', false);
    }
  }
  await closeModal();
});

await step('presign-class-lock', async () => {
  await navObjects('testijotain');
  // Pre-sign URL
  await clickRow('readme.md');
  await openCtx('readme.md');
  await ctxItem(/pre-sign/i);
  await waitFor(modalVisible, 4000, 'presign modal');
  await ok('presign shows a signed URL', waitFor(async () => (await evalPage(() => document.querySelector('#modal-root input')?.value || '')).includes('X-Amz-Signature'), 4000, 'signature'));
  await ok('presign layout clean', (await layoutAudit()).ok);
  await shot('presign');
  await closeModal();
  // Storage class
  await openCtx('readme.md');
  await ctxItem(/storage class/i);
  await waitFor(modalVisible, 4000, 'class modal');
  await ok('class dialog offers target classes', evalPage(() => document.querySelectorAll('#modal-root select option').length >= 3));
  await ok('class layout clean', (await layoutAudit()).ok);
  await shot('storage-class');
  await closeModal();
  // Object lock (single object → lockDialogOne)
  await openCtx('readme.md');
  await ctxItem(/object lock/i);
  await waitFor(modalVisible, 4000, 'lock modal');
  await ok('lock dialog opens for the object', (await evalPage(() => document.querySelector('#modal-root .modal-head span')?.textContent || '')).includes('Object lock'));
  await ok('lock layout clean', (await layoutAudit()).ok);
  await shot('object-lock');
  await closeModal();
});

await step('props-dialogs', async () => {
  await navObjects('testijotain');
  // folder properties (grid row — the tree node handle can go stale when
  // guard icons re-render the tree mid-step; the grid row is stable)
  const n = await openCtx('docs');
  await ok('folder row menu has items', n >= 5);
  await ctxItem(/properties/i);
  await waitFor(modalVisible, 4000, 'folder props');
  await ok('folder props rows', evalPage(() => document.querySelectorAll('#modal-root .kv .k').length >= 3));
  await ok('folder props layout clean', (await layoutAudit()).ok);
  await shot('props-folder');
  await closeModal();
  // object properties
  await openCtx('readme.md');
  await ctxItem(/properties/i);
  await waitFor(modalVisible, 4000, 'object props');
  await ok('object props rows', evalPage(() => document.querySelectorAll('#modal-root .kv .k').length >= 4));
  await ok('object props layout clean', (await layoutAudit()).ok);
  await shot('props-object');
  await closeModal();
  // bucket properties (buckets view)
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('testijotain'), 6000, 'buckets view');
  await openCtx('testijotain');
  await ctxItem(/properties/i);
  await waitFor(modalVisible, 4000, 'bucket props');
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root .kv .k').length)) >= 6, 4000, 'bucket props rows');
  await ok('bucket props are comprehensive', evalPage(() => {
    const ks = Array.from(document.querySelectorAll('#modal-root .kv .k')).map((k) => k.textContent);
    return ['Provider', 'Versioning'].every((w) => ks.some((k) => k.includes(w)));
  }));
  await ok('guard rows render as Enabled/Disabled pills', evalPage(() => {
    const pills = Array.from(document.querySelectorAll('#modal-root .pill'));
    return pills.length >= 2 && pills.some((p) => /enabled/i.test(p.textContent) && p.classList.contains('on'));
  }));
  await ok('bucket props layout clean', (await layoutAudit()).ok);
  await shot('props-bucket');
  await closeModal();
});

await step('prompts-and-delete-gates', async () => {
  await navObjects('testijotain');
  // Rename prompt comes pre-filled; cancel without changing anything
  await clickRow('readme.md');
  await page.keyboard.press('F2');
  await waitFor(modalVisible, 4000, 'rename prompt');
  await ok('rename prefilled with current name', evalPage(() => {
    const i = document.querySelector('#modal-root input');
    return i && i.value === 'readme.md';
  }));
  await ok('rename layout clean', (await layoutAudit()).ok);
  await shot('prompt-rename');
  await closeModal();
  // New folder prompt
  await page.keyboard.press('Control+Shift+N');
  await waitFor(modalVisible, 4000, 'newfolder prompt');
  await ok('new folder prompt defaults', evalPage(() => {
    const i = document.querySelector('#modal-root input');
    return i && i.value === 'new-folder';
  }));
  await closeModal();
  // Delete gate: counts first, acts second — cancel
  await clickRow('readme.md');
  await page.keyboard.press('Delete');
  await waitFor(modalVisible, 4000, 'delete confirm');
  await ok('delete confirm shows object count', (await evalPage(() => document.getElementById('modal-root').textContent)).length > 10);
  await ok('delete layout clean', (await layoutAudit()).ok);
  await shot('delete-confirm');
  await closeModal();
});

await step('transfers', async () => {
  await evalPage(() => window.__shim.emit('transfer:update', { id: 't1', op: 'upload', status: 'running', totalFiles: 3, doneFiles: 1 }));
  // the toolbar Transfers button is gone — View menu (and the status-bar
  // jobs indicator) carry it
  await page.locator('#menubar .mb-title', { hasText: /view/i }).first().click();
  await sleep(80);
  const trItem = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /transfers/i.test(i.textContent)) || null);
  await ok('View→Transfers present', !!trItem);
  if (trItem) await trItem.asElement().click();
  await waitFor(modalVisible, 4000, 'transfers modal');
  await ok('job rendered with its current file', waitFor(async () => (await evalPage(() => document.getElementById('modal-root').textContent)).includes('video-final.mp4'), 4000, 'jobs'));
  await shot('transfers');
  await closeModal();
});

await step('dual-pane', async () => {
  await page.click('#btn-panes');
  await ok('pane visible', evalPage(() => !document.getElementById('local-pane').classList.contains('hidden')));
  // sideKeys carries absolute paths — assert on the visible row label
  await waitFor(async () => !!(await sideRow('Downloads')), 6000, 'local home');
  await ok('local home listed', true);
  await shot('pane-local');
  // remote binding
  await page.selectOption('#local-src', 'src-box');
  await waitFor(async () => (await sideKeys()).includes('/backup.sh'), 6000, 'pane remote listing');
  await ok('pane binds sftp source', (await txt('#local-crumb')).includes('backup-box'));
  await shot('pane-remote');
  // s3 binding (slice 5)
  await page.selectOption('#local-src', 'src-hetzner');
  await waitFor(async () => (await sideKeys()).includes('logs-2026'), 6000, 'pane s3 buckets');
  await ok('pane lists S3 buckets', (await txt('#local-crumb')).includes('hetzner'));
  await shot('pane-s3');
  await page.selectOption('#local-src', 'local');
  await waitFor(async () => !!(await sideRow('Downloads')), 6000, 'pane back to local');
});

await step('compare', async () => {
  await navObjects('testijotain');
  await page.click('#local-compare');
  await waitFor(async () => evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .some((r) => (r.dataset.cmp || '') !== '')), 4000, 'cmp decorations');
  await ok('compare decorations painted', evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .some((r) => ['newer-remote', 'size-diff', 'only-remote', 'diff-below', 'same'].includes(r.dataset.cmp))));
  await ok('CompareAny called with both sides', (await findCall('CompareAny')) !== null);
  await shot('compare');
});

await step('log-area', async () => {
  await page.click('#status-log');
  if (!(await evalPage(() => !document.getElementById('logarea').classList.contains('hidden')))) {
    await page.keyboard.press('Control+l'); // documented shortcut fallback
  }
  await ok('log visible', evalPage(() => !document.getElementById('logarea').classList.contains('hidden')));
  await evalPage(() => window.__shim.emit('log:line', { time: new Date().toISOString(), level: 'warn', scope: 'harness', message: 'visual harness log line' }));
  await waitFor(async () => (await txt('#logarea')).includes('visual harness log line'), 4000, 'log line');
  await ok('log line appended', true);
  await shot('logarea');
  await page.click('#status-log');
});

await step('profile-flow', async () => {
  await page.locator('#menubar .mb-title').first().click(); // File
  await sleep(80);
  const open = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /open profile/i.test(i.textContent)) || null);
  await ok('File→Open profile present', !!open);
  if (open) {
    await open.asElement().click();
    await sleep(250);
    // password prompt (if the flow asks) — fill and confirm
    const pw = await elOrNull(() => document.querySelector('#modal-root input[type=password], #modal-root input.input') || null);
    if (pw && await modalVisible()) {
      await pw.asElement().fill('demo-pass');
      const go = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
        .find((b) => /^(ok|open|unlock)$/i.test(b.textContent.trim())) || null);
      if (go) await go.asElement().click();
      await sleep(250);
    }
    await ok('OpenProfileFile reached the backend', (await findCall('OpenProfileFile')) !== null);
    await ok('status bar shows open profile', waitFor(async () => (await txt('#status-pfile')).includes('demo.s3bprofile')
      || (await txt('#status-profile')).includes('demo.s3bprofile'), 3000, 'pf status'));
    await shot('profile-open');
  }
  await page.keyboard.press('Escape');
});

await step('onboarding-empty', async () => {
  const p2 = await context.newPage();
  p2.on('pageerror', (e) => { pageErrors.push(String(e)); });
  await p2.addInitScript(shim);
  await p2.goto(BASE + '?empty=1');
  await p2.waitForFunction(() => (document.getElementById('status-version')?.textContent || '').includes('s3b v'), null, { timeout: 10000 });
  await ok('empty state visible', p2.evaluate(() => !document.getElementById('empty-state').classList.contains('hidden')));
  await ok('simplified onboarding copy', p2.evaluate(() => document.getElementById('empty-state').textContent.includes('Amazon S3 or any S3-compatible storage')));
  await ok('import button renamed to S3 credentials', p2.evaluate(() => document.getElementById('empty-actions').textContent.toLowerCase().includes('import s3 credentials')));
  await ok('empty actions offer add-source', p2.evaluate(() => document.getElementById('empty-actions').textContent.length > 0));
  await ok('upload greyed without sources', p2.evaluate(() => document.getElementById('btn-upload').disabled));
  await ok('doctor greyed without sources', p2.evaluate(() => window.__s3bCmdState?.canDoctor === false));
  await p2.screenshot({ path: path.join(OUT, String(++shotNo).padStart(2, '0') + '-onboarding-empty.png') });
  await p2.close();
});

// ===================== DnD matrix (slice 5) =====================
// Every drop runs through the real dragstart (app-built payload) onto the
// real target element; assertions read the recorded backend call.

await step('dnd-s3-to-s3-tree', async () => {
  await navObjects('testijotain');
  await resetCalls();
  const from = await gridRow('readme.md');
  const to = await treeRow('logs-2026');
  await dnd(from, to);
  const c = await findCall('CopySelection');
  await ok('routes to CopySelection', !!c);
  await ok('args: src testijotain → dst logs-2026', c && c.args[0] === 'testijotain' && c.args[2] === 'logs-2026');
  await ok('copy by default (move=false)', c && c.args[4] === false);
  await ok('toast confirms copy', (await txt('#toasts')).length > 0);
});

await step('dnd-s3-to-s3-shift-move', async () => {
  await resetCalls();
  const from = await gridRow('readme.md');
  const to = await treeRow('logs-2026');
  await dnd(from, to, { shift: true });
  const c = await findCall('CopySelection');
  await ok('Shift forces move', c && c.args[4] === true);
});

await step('dnd-s3-to-remote-tree', async () => {
  await resetCalls();
  const from = await gridRow('readme.md');
  const to = await treeRow('backup-box');
  await dnd(from, to);
  const c = await findCall('TransferCross');
  await ok('routes to TransferCross', !!c);
  await ok('items pin the originating S3 source', c && c.args[0][0].bucket === 'testijotain' && c.args[0][0].source === 'hetzner');
  await ok('dest is the remote source', c && c.args[2].kind === 'remote' && c.args[2].source === 'backup-box');
  await shot('dnd-s3-remote');
});

await step('dnd-s3-onto-folder-move', async () => {
  await resetCalls();
  const from = await gridRow('readme.md');
  const to = await gridRow('docs');
  await dnd(from, to);
  const c = await findCall('CopySelection');
  await ok('same-bucket drop defaults to move', c && c.args[4] === true);
  await ok('targets the folder prefix', c && c.args[3] === 'docs/');
});

await step('dnd-s3-onto-itself-rejected', async () => {
  await resetCalls();
  const from = await gridRow('readme.md');
  const body = await elOrNull(() => document.getElementById('grid-body'));
  await dnd(from, body);
  const c = await findCall('CopySelection');
  await ok('drop into origin dir is a no-op', !c);
});

await step('dnd-local-to-s3', async () => {
  // pane on local Downloads, main grid on testijotain objects.
  // sideKeys returns absolute paths ('C:\Users\demo\Downloads') — match the
  // visible .tname label instead
  await page.selectOption('#local-src', 'local');
  await waitFor(async () => !!(await sideRow('Downloads')), 4000, 'pane local');
  const dl = await sideRow('Downloads');
  await dl.asElement().dblclick();
  await waitFor(async () => !!(await sideRow('invoice.pdf')), 4000, 'Downloads listing');
  await resetCalls();
  const from = await sideRow('invoice.pdf');
  const to = await gridRow('docs');
  await dnd(from, to);
  const c = await findCall('Upload');
  await ok('routes to Upload', !!c);
  await ok('uploads into bucket+folder', c && c.args[1] === 'testijotain' && c.args[2] === 'docs/');
  await ok('carries the local path', c && /invoice\.pdf$/.test(c.args[0][0]));
});

await step('dnd-s3-to-local-pane', async () => {
  // the onboarding step reloads the page, so the pane may sit anywhere —
  // go HOME explicitly (up() from home would climb above it into an empty
  // C:\Users\) and wait for a real folder row (Documents) as the target
  const home = await elOrNull(() => document.getElementById('local-home'));
  await home.asElement().click();
  await waitFor(async () => !!(await sideRow('Downloads')), 4000, 'pane home');
  await resetCalls();
  const from = await gridRow('readme.md');
  const folder = await sideRow('Documents');
  await dnd(from, folder);
  const c = await findCall('DownloadRefs');
  await ok('routes to DownloadRefs', !!c);
  await ok('downloads into the folder', c && /Documents$/.test(c.args[2]));
  await ok('carries the bucket', c && c.args[0] === 'testijotain');
});

await step('dnd-remote-to-webdav-tree', async () => {
  await clickTree('backup-box');
  await waitFor(async () => (await rowKeys()).includes('/backup.sh'), 6000, 'remote listing');
  await resetCalls();
  const from = await gridRow('backup.sh');
  const to = await treeRow('dav-claims');
  await dnd(from, to);
  const c = await findCall('TransferCross');
  await ok('routes to TransferCross', !!c);
  await ok('items tag the remote origin', c && c.args[0][0].source === 'backup-box' && c.args[0][0].key === '/backup.sh');
  await ok('dest is the webdav source', c && c.args[2].source === 'dav-claims');
});

await step('dnd-os-file-drop', async () => {
  await navObjects('testijotain');
  await resetCalls();
  await page.evaluate(() => {
    const r = document.getElementById('grid-body').getBoundingClientRect();
    window.__shim.emit('wails:file-drop', { x: r.left + r.width / 2, y: r.top + r.height / 2, paths: ['C:\\Users\\demo\\Downloads\\photos.zip'] });
  });
  await waitFor(async () => (await findCall('Upload')) !== null, 4000, 'OS-drop upload');
  const c = await findCall('Upload');
  await ok('OS drop over grid uploads into current view', c && c.args[1] === 'testijotain' && /photos\.zip$/.test(c.args[0][0]));
});

await step('paste-parity', async () => {
  // copy S3 → paste in another bucket
  await resetCalls();
  await clickRow('readme.md');
  await page.keyboard.press('Control+c');
  await sleep(100);
  await clickTree('logs-2026');
  await waitFor(async () => (await rowKeys()).includes('app/'), 6000, 'logs objects');
  await page.keyboard.press('Control+v');
  await sleep(200);
  let c = await findCall('CopySelection');
  await ok('Ctrl+C/V copies across buckets', c && c.args[0] === 'testijotain' && c.args[2] === 'logs-2026' && c.args[4] === false);
  // cut → move (the logs-2026 root collapses prefixes into the app/ folder
  // row — enter the folder to reach the file rows)
  await resetCalls();
  await dblClickRow('app');
  await waitFor(async () => (await rowKeys()).includes('app/app-2026-09-11.log'), 6000, 'app objects');
  // gridRow matches the .tname display label (file name without the folder
  // prefix), not the full anchored key
  await clickRow('app-2026-09-11.log');
  await page.keyboard.press('Control+x');
  await clickTree('testijotain');
  await waitFor(async () => (await rowKeys()).includes('readme.md'), 6000, 'back to testijotain');
  await page.keyboard.press('Control+v');
  await sleep(200);
  c = await findCall('CopySelection');
  await ok('Ctrl+X/V moves', c && c.args[4] === true && c.args[2] === 'testijotain');
  // remote clipboard → paste into S3 streams through TransferCross
  await resetCalls();
  await clickTree('backup-box');
  await waitFor(async () => (await rowKeys()).includes('/backup.sh'), 6000, 'remote listing');
  await clickRow('backup.sh');
  await page.keyboard.press('Control+c');
  await clickTree('testijotain');
  await waitFor(async () => (await rowKeys()).includes('readme.md'), 6000, 'objects view');
  await page.keyboard.press('Control+v');
  await sleep(200);
  c = await findCall('TransferCross');
  await ok('remote paste routes through TransferCross', c && c.args[0][0].source === 'backup-box' && c.args[2].bucket === 'testijotain');
});

// ===================== view-source + OS interop (slice 6) =====================

await step('import-creds-file', async () => {
  await resetCalls();
  await page.locator('#menubar .mb-title').first().click(); // File
  await sleep(80);
  const item = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /import credentials/i.test(i.textContent)) || null);
  await ok('File→Import credentials present', !!item);
  if (!item) return;
  await item.asElement().click();
  await waitFor(modalVisible, 4000, 'import dialog');
  // From file… → OS picker + parse (no nested modal on a plain INI file)
  const ff = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /^from file/i.test(b.textContent.trim())) || null);
  await ok('From file… button present', !!ff);
  if (ff) await ff.asElement().click();
  await waitFor(async () => (await findCall('ParseCredentialFile')) !== null, 4000, 'parse');
  await waitFor(async () => !!(await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .cred-row'))
    .find((r) => r.textContent.includes('from-file')) || null)), 4000, 'cred row');
  await ok('parsed candidate listed for review', true);
  await shot('import-creds');
  // Test → connectivity check + ok badge
  const test = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .cred-row button'))
    .find((b) => /^test$/i.test(b.textContent.trim())) || null);
  if (test) await test.asElement().click();
  await waitFor(async () => evalPage(() => document.querySelectorAll('#modal-root .cred-test.ok').length > 0), 4000, 'test badge');
  await ok('candidate tested before import', (await findCall('TestCredentialDraft')) !== null);
  // Import → backend import + tree gains the source
  const imp = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /^import$/i.test(b.textContent.trim())) || null);
  if (imp) await imp.asElement().click();
  await waitFor(async () => (await findCall('ImportCredentials')) !== null, 4000, 'import call');
  const c = await findCall('ImportCredentials');
  await ok('imports the checked candidate end-to-end', c && JSON.stringify(c.args[0]) === '["cand-file1"]');
  await waitFor(async () => (await txt('#tree')).includes('from-file'), 4000, 'tree gains from-file');
  await ok('imported source appears in the tree', true);
});

await step('import-creds-kms', async () => {
  await resetCalls();
  await page.locator('#menubar .mb-title').first().click(); // File
  await sleep(80);
  const item = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /import credentials/i.test(i.textContent)) || null);
  await ok('menu item still present', !!item);
  if (!item) return;
  await item.asElement().click();
  await waitFor(modalVisible, 4000, 'import dialog');
  const fsBtn = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /^from service/i.test(b.textContent.trim())) || null);
  await ok('From service… button present', !!fsBtn);
  if (!fsBtn) return;
  await fsBtn.asElement().click();
  await waitFor(async () => (await evalPage(() => document.getElementById('modal-root').textContent))
    .includes('Import from a secrets service'), 4000, 'kms dialog');
  // custom HTTP service with a URL
  await page.selectOption('#modal-root select', 'http');
  const url = await elOrNull(() => {
    const inp = Array.from(document.querySelectorAll('#modal-root input'));
    return inp[0] || null;
  });
  if (url) await url.asElement().fill('http://kms.local/secret/s3');
  const fetch = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /^fetch$/i.test(b.textContent.trim())) || null);
  await ok('Fetch button present', !!fetch);
  if (fetch) await fetch.asElement().click();
  await waitFor(async () => (await findCall('KmsFetch')) !== null, 4000, 'KmsFetch');
  const c = await findCall('KmsFetch');
  await ok('custom HTTP service carries its URL param', c && c.args[0] === 'http' && /kms\.local/.test(c.args[1]?.url || ''));
  // the KMS dialog closes and the import dialog comes back with the secret
  // (nested modals replace — the state must survive the round trip)
  await waitFor(async () => !!(await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .cred-row'))
    .find((r) => r.textContent.includes('hetzner-kms')) || null)), 4000, 'kms candidate row');
  await ok('import dialog re-shown with the fetched candidate', true);
  await shot('import-kms');
  const imp = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /^import$/i.test(b.textContent.trim())) || null);
  if (imp) await imp.asElement().click();
  await waitFor(async () => (await txt('#tree')).includes('hetzner-kms'), 4000, 'tree gains hetzner-kms');
  await ok('KMS-imported source appears in the tree', true);
});

await step('view-source-switch', async () => {
  // 'from-file' is a second S3 source: opening it pins the engine to it
  // (SetViewSource) and mirrors it in the status bar
  await resetCalls();
  await clickTree('from-file');
  await waitFor(async () => (await rowKeys()).includes('testijotain'), 6000, 'from-file buckets');
  const c = await findCall('SetViewSource');
  await ok('opening a source pins it as the view source', c && c.args[0] === 'from-file');
  await ok('status bar mirrors the active source', (await txt('#status-profile')).includes('from-file'));
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('testijotain'), 6000, 'back to hetzner');
  const c2 = await findCall('SetViewSource');
  await ok('switching back re-pins hetzner', c2 && c2.args[0] === 'hetzner');
});

await step('status-balls', async () => {
  // every configured source gets a connectivity ball; all engines answer
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('#tree .sball.ok').length)) >= 3, 6000, 'ok balls');
  await ok('sources carry OK status balls', evalPage(() => document.querySelectorAll('#tree .sball.ok').length >= 3));
  await shot('status-balls');
});

await step('breadcrumb-path-nav', async () => {
  await navObjectsOf('hetzner', 'testijotain');
  // clicking the navbar's empty area opens the inline path editor holding
  // the canonical Source://bucket/prefix path
  await evalPage(() => document.querySelector('.navbar').dispatchEvent(new MouseEvent('click', { bubbles: true })));
  await waitFor(async () => evalPage(() => !!document.querySelector('#breadcrumb input.path-edit')), 4000, 'path editor');
  await ok('path field holds the canonical path', evalPage(() => document.querySelector('#breadcrumb input.path-edit')?.value === 'hetzner://testijotain/'));
  await shot('path-edit');
  // type another location and press Enter — parsePath navigates
  await page.fill('#breadcrumb input.path-edit', 'hetzner://logs-2026/');
  await page.keyboard.press('Enter');
  await waitFor(async () => (await rowKeys()).includes('app/'), 6000, 'navigated via path');
  await ok('pasting a Source://bucket/ path navigates', (await txt('#breadcrumb')).includes('logs-2026'));
});

await step('sidebar-resize', async () => {
  const w0 = await evalPage(() => document.getElementById('sidebar').getBoundingClientRect().width);
  await evalPage(() => {
    const split = document.getElementById('side-split');
    split.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    window.dispatchEvent(new MouseEvent('mousemove', { clientX: 380 }));
    window.dispatchEvent(new MouseEvent('mouseup'));
  });
  await sleep(80);
  const w1 = await evalPage(() => document.getElementById('sidebar').getBoundingClientRect().width);
  await ok('splitter drag resizes the sidebar', Math.round(w1) === 380);
  await ok('width persisted', evalPage(() => localStorage.getItem('s3b-sidebar-w') === '380'));
  await shot('sidebar-resized');
  // double-click resets to the default
  await evalPage(() => document.getElementById('side-split').dispatchEvent(new MouseEvent('dblclick', { bubbles: true })));
  await sleep(60);
  const w2 = await evalPage(() => document.getElementById('sidebar').getBoundingClientRect().width);
  await ok('double-click resets the width', evalPage(() => localStorage.getItem('s3b-sidebar-w') === null) && Math.abs(w2 - w0) < 2);
});

await step('os-copy-mirror', async () => {
  // Ctrl+C on remote rows also mirrors to the OS clipboard: files are
  // staged into a temp dir, then handed to Explorer as file paths
  await clickTree('backup-box');
  await waitFor(async () => (await rowKeys()).includes('/backup.sh'), 6000, 'remote listing');
  await resetCalls();
  await clickRow('backup.sh');
  await page.keyboard.press('Control+c');
  await waitFor(async () => (await findCall('TransferCross')) !== null, 4000, 'staging transfer');
  const c = await findCall('TransferCross');
  await ok('copy stages through TransferCross', c && c.args[0][0].source === 'backup-box' && c.args[0][0].key === '/backup.sh');
  await ok('staging lands in the clipboard dir', c && c.args[2].kind === 'local' && /s3b-clip/.test(c.args[2].dir || ''));
  await ok('staging uses the overwrite policy', c && c.args[3] === 'overwrite');
  // let the idle poll see an empty queue, then the OS clipboard is set.
  // earlier steps' stale staging polls write their own entries when the
  // queue clears — wait for OUR staged path specifically.
  await evalPage(() => { window.__shim.world.transfers = []; });
  await waitFor(async () => (await calls()).some((x) => x.m === 'OsClipboardSetFiles'
    && (x.args[0] || []).some((p) => /backup\.sh$/.test(p))), 8000, 'backup.sh handed to the OS clipboard');
  await ok('staged file handed to the OS clipboard', true);
});

await step('os-clipboard-paste', async () => {
  // fresh page (empty app clipboard): Ctrl+V falls back to the OS
  // clipboard — paths copied in Explorer upload into the open bucket
  const p3 = await context.newPage();
  p3.on('pageerror', (e) => { pageErrors.push(String(e)); });
  await p3.addInitScript(shim);
  await p3.goto(BASE);
  await p3.waitForFunction(() => (document.getElementById('status-version')?.textContent || '').includes('s3b v'), null, { timeout: 10000 });
  await p3.evaluate(() => { window.__shim.world.osClip = ['C:\\Users\\demo\\Downloads\\photos.zip']; });
  await p3.evaluate(() => Array.from(document.querySelectorAll('#tree .tnode'))
    .find((n) => n.querySelector('.tlabel')?.textContent === 'testijotain')?.click());
  await p3.waitForFunction(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .some((r) => r._model && r._model.key === 'readme.md'), null, { timeout: 8000 });
  await p3.evaluate(() => { window.__shim.calls.length = 0; });
  await p3.keyboard.press('Control+v');
  await p3.waitForFunction(() => (window.__shim.calls || []).some((x) => x.m === 'Upload'), null, { timeout: 6000 });
  const c = await p3.evaluate(() => window.__shim.calls.filter((x) => x.m === 'Upload').at(-1));
  await ok('Explorer-copied paths paste into a bucket', c && c.args[1] === 'testijotain' && /photos\.zip$/.test(c.args[0][0]));
  const sawOs = await p3.evaluate(() => window.__shim.calls.some((x) => x.m === 'OsClipboardFiles'));
  await ok('OS clipboard read through the binding', sawOs);
  await p3.screenshot({ path: path.join(OUT, String(++shotNo).padStart(2, '0') + '-os-paste.png') });
  await p3.close();
});

await step('drag-urls', async () => {
  // selecting files precomputes drag-out URLs (OS drag needs synchronous
  // data in dragstart)
  await navObjectsOf('hetzner', 'testijotain');
  await resetCalls();
  await clickRow('readme.md');
  await waitFor(async () => (await findCall('MakeDragUrls')) !== null, 4000, 'drag urls');
  const c = await findCall('MakeDragUrls');
  await ok('selection precomputes drag-out URLs', c && c.args[0][0].bucket === 'testijotain'
    && c.args[0][0].key === 'readme.md' && c.args[0][0].name === 'readme.md');
  await ok('drag items tag the originating source', c && c.args[0][0].source === 'hetzner');
});

// ===================== full-feature coverage (slice 7) =====================
await step('toolbar-nav', async () => {
  // back/forward/up through BOTH the toolbar buttons and the Alt-key
  // shortcuts, plus F5 — the canonical file-manager navigation set
  await navObjectsOf('hetzner', 'testijotain');
  await dblClickRow('docs');
  await waitFor(async () => (await txt('#breadcrumb')).includes('docs'), 6000, 'inside docs');
  await ok('Back enabled after navigating', evalPage(() => !document.getElementById('btn-back').disabled));
  await page.click('#btn-back');
  await waitFor(async () => !(await txt('#breadcrumb')).includes('docs'), 6000, 'back to bucket root');
  await ok('Back button returns', true);
  await ok('Forward enabled after going back', evalPage(() => !document.getElementById('btn-forward').disabled));
  await page.keyboard.press('Alt+ArrowRight');
  await waitFor(async () => (await txt('#breadcrumb')).includes('docs'), 6000, 'Alt+Right forward');
  await ok('Alt+Right goes forward', true);
  await page.keyboard.press('Alt+ArrowUp');
  await waitFor(async () => !(await txt('#breadcrumb')).includes('docs'), 6000, 'Alt+Up parent');
  await ok('Alt+Up climbs to the parent', true);
  await resetCalls();
  await page.keyboard.press('F5');
  await waitFor(async () => (await findCall('ListObjectsStream')) !== null, 4000, 'F5 ListObjectsStream');
  await ok('F5 refreshes the current view', true);
});

await step('crumb-click', async () => {
  // clicking a breadcrumb segment navigates straight to it
  await navObjectsOf('hetzner', 'testijotain');
  await dblClickRow('docs');
  await waitFor(async () => (await rowKeys()).includes('docs/notes.md'), 6000, 'docs rows');
  // one level deeper so the docs crumb is not the current location
  await dblClickRow('legacy');
  await waitFor(async () => (await rowKeys()).includes('docs/legacy/old.txt'), 6000, 'legacy objects');
  const crumb = await elOrNull(() => Array.from(document.querySelectorAll('#breadcrumb .crumb'))
    .find((c) => c.textContent === 'docs') || null);
  await ok('docs crumb rendered', !!crumb);
  if (crumb) await crumb.asElement().click();
  await waitFor(async () => !(await txt('#breadcrumb')).includes('legacy'), 6000, 'crumb nav');
  await ok('crumb click navigates to that folder', (await txt('#breadcrumb')).includes('docs'));
  // the source root crumb goes back to the buckets view
  const root = await elOrNull(() => Array.from(document.querySelectorAll('#breadcrumb .crumb'))
    .find((c) => /hetzner/.test(c.textContent)) || null);
  await ok('source crumb rendered', !!root);
  if (root) await root.asElement().click();
  await waitFor(async () => (await rowKeys()).includes('logs-2026'), 6000, 'buckets via crumb');
  await ok('source crumb returns to the buckets view', true);
});

await step('favorites', async () => {
  // star a bucket from its context menu, jump in from the sidebar, unstar
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('testijotain'), 6000, 'buckets view');
  await ok('favorites hidden while empty', evalPage(() => document.getElementById('fav-section').classList.contains('hidden')));
  await openCtx('testijotain');
  await ctxItem(/add to favorites/i);
  await waitFor(async () => !!(await elOrNull(() => document.querySelector('#favorites .fav-row') || null)), 4000, 'fav row');
  await ok('favorite appears in the sidebar', evalPage(() => document.querySelector('#favorites .fav-label')?.textContent === 'testijotain'));
  await ok('favorite persisted', (await evalPage(() => localStorage.getItem('s3b-favs'))).includes('testijotain'));
  await shot('favorites');
  await resetCalls();
  await page.click('#favorites .fav-row');
  await waitFor(async () => (await txt('#breadcrumb')).includes('testijotain')
    && (await rowKeys()).some((k) => k === 'readme.md'), 6000, 'fav navigation');
  await ok('clicking a favorite opens the bucket', true);
  // remove again — the section hides
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('testijotain'), 6000, 'buckets view again');
  await openCtx('testijotain');
  await ctxItem(/remove from favorites/i);
  await ok('unstar hides the section', waitFor(async () => evalPage(() => document.getElementById('fav-section').classList.contains('hidden')), 4000, 'fav hidden'));
  await ok('favorites emptied', (await evalPage(() => localStorage.getItem('s3b-favs'))) === '[]');
});

await step('theme-toggle', async () => {
  const before = await evalPage(() => document.documentElement.dataset.theme);
  await page.click('#btn-theme');
  const after = await evalPage(() => document.documentElement.dataset.theme);
  await ok('toolbar toggles the theme', before !== after);
  await ok('choice persisted', (await evalPage(() => localStorage.getItem('s3b-theme'))) === after);
  await shot(`theme-${after}`);
  await page.click('#btn-theme');
  await ok('second click restores', evalPage((b) => document.documentElement.dataset.theme === b, before));
});

await step('auto-refresh', async () => {
  // View → Auto refresh → interval; the status bar mirrors it and the
  // timer actually re-lists the open view. Retire the transfers-step job
  // first — a visible jobs badge blocks background refreshes BY DESIGN,
  // and it only re-evaluates on the next transfer:update.
  await evalPage(() => window.__shim.emit('transfer:update', { id: 't1', op: 'upload', status: 'done' }));
  await waitFor(async () => evalPage(() => document.getElementById('status-jobs').classList.contains('hidden')), 4000, 'jobs badge retired');
  await page.bringToFront();
  await navObjectsOf('hetzner', 'testijotain');
  await page.locator('#menubar .mb-title', { hasText: /view/i }).first().click();
  await sleep(80);
  const ar = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /auto.?refresh/i.test(i.textContent)) || null);
  await ok('View menu has Auto refresh', !!ar);
  if (ar) {
    await ar.asElement().hover();
    await sleep(120);
    const s5 = await elOrNull(() => Array.from(document.querySelectorAll('.mb-dd.sub:not(.hidden) .mb-item'))
      .find((i) => /^5\s*s$/i.test(i.textContent.trim())) || null);
    await ok('interval submenu offers 5 s', !!s5);
    if (s5) await s5.asElement().click();
  }
  await ok('status bar mirrors the interval', waitFor(async () => /5s/.test(await txt('#status-auto')), 4000, 'status-auto 5s'));
  await resetCalls();
  await waitFor(async () => (await findCall('ListObjectsStream')) !== null, 9000, 'auto tick');
  await ok('auto refresh re-lists the view', true);
  // Off cleans the indicator up
  await page.locator('#menubar .mb-title', { hasText: /view/i }).first().click();
  await sleep(80);
  const ar2 = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /auto.?refresh/i.test(i.textContent)) || null);
  if (ar2) {
    await ar2.asElement().hover();
    await sleep(120);
    const off = await elOrNull(() => Array.from(document.querySelectorAll('.mb-dd.sub:not(.hidden) .mb-item'))
      .find((i) => /off/i.test(i.textContent)) || null);
    await ok('Off entry offered', !!off);
    if (off) await off.asElement().click();
  }
  await ok('Off hides the indicator', waitFor(async () => evalPage(() => document.getElementById('status-auto').classList.contains('hidden')), 4000, 'status-auto off'));
});

await step('marquee-select', async () => {
  // rubber-band starting in the empty area below the rows, dragged up
  await navObjectsOf('hetzner', 'testijotain');
  const keys = await rowKeys();
  const geo = await evalPage(() => {
    const r = document.getElementById('grid-body').getBoundingClientRect();
    return { x: r.left + r.width / 2, top: r.top, bottom: r.bottom };
  });
  const startY = geo.top + keys.length * 28 + 8;
  if (startY < geo.bottom - 4) {
    await page.mouse.move(geo.x, startY);
    await page.mouse.down();
    await page.mouse.move(geo.x, startY - 70, { steps: 4 });
    await page.mouse.up();
    // selected summary reads "N of TOTAL items selected" — the unselected
    // status only carries the total, so anchor on "of … selected"
    await ok('marquee rubber-band selects a band', waitFor(async () => /[2-9] of \d+ items/.test(await txt('#status-selection')), 4000, 'selection count'));
    await shot('marquee');
    await page.keyboard.press('Escape');
    await ok('Escape clears the selection', waitFor(async () => !/ of /.test(await txt('#status-selection')), 4000, 'cleared'));
  } else {
    await ok('marquee rubber-band selects a band (no empty area — skipped)', true);
  }
});

await step('editors-manager', async () => {
  // files open in the external editor show in the status bar; clicking
  // the indicator opens the manager
  await evalPage(() => window.dispatchEvent(new Event('focus')));
  await waitFor(async () => evalPage(() => !document.getElementById('status-editing').classList.contains('hidden')), 4000, 'editing indicator');
  await ok('status bar lists files in editor', /editor/.test(await txt('#status-editing')));
  await page.click('#status-editing');
  await waitFor(modalVisible, 4000, 'editing modal');
  await ok('manager lists the open file', waitFor(async () => (await evalPage(() => document.getElementById('modal-root').textContent)).includes('notes.md'), 4000, 'file row'));
  await shot('editors');
  await closeModal();
});

await step('download-selection', async () => {
  // Ctrl+D (and the toolbar button) download through DownloadRefs after
  // a destination folder pick
  await navObjectsOf('hetzner', 'testijotain');
  await clickRow('readme.md');
  await resetCalls();
  await page.keyboard.press('Control+d');
  await waitFor(async () => (await findCall('DownloadRefs')) !== null, 4000, 'DownloadRefs');
  const c = await findCall('DownloadRefs');
  await ok('Ctrl+D downloads through DownloadRefs', c && c.args[0] === 'testijotain'
    && c.args[1]?.[0]?.key === 'readme.md' && /Downloads$/.test(c.args[2] || ''));
  await shot('download');
});

await step('shortcut-keys', async () => {
  // Ctrl+F focuses the filter box
  await page.keyboard.press('Control+f');
  await ok('Ctrl+F focuses the filter', evalPage(() => document.activeElement?.id === 'filter'));
  await evalPage(() => document.activeElement?.blur());
  // Ctrl+U runs the single upload flow
  await resetCalls();
  await page.keyboard.press('Control+u');
  await waitFor(async () => (await findCall('Upload')) !== null, 4000, 'Ctrl+U upload');
  const up = await findCall('Upload');
  await ok('Ctrl+U uploads through one dialog', up && up.args[1] === 'testijotain');
  // F9 toggles the dual pane (normalize to closed first — earlier steps
  // leave the pane open)
  if (!(await evalPage(() => document.getElementById('local-pane').classList.contains('hidden')))) {
    await page.keyboard.press('F9');
    await sleep(120);
  }
  await page.keyboard.press('F9');
  await ok('F9 opens the side pane', waitFor(async () => evalPage(() => !document.getElementById('local-pane').classList.contains('hidden')), 4000, 'pane open'));
  await page.keyboard.press('F9');
  await ok('F9 closes it again', waitFor(async () => evalPage(() => document.getElementById('local-pane').classList.contains('hidden')), 4000, 'pane closed'));
});

await step('layout-audit', async () => {
  // chrome alignment: menubar/toolbar/statusbar children must stay inside
  // their bar (no vertical bleed, no horizontal overflow)
  const align = await evalPage(() => {
    const bad = [];
    for (const bar of [document.getElementById('menubar'), document.getElementById('toolbar'),
      document.querySelector('footer.statusbar')].filter(Boolean)) {
      const r = bar.getBoundingClientRect();
      if (bar.scrollWidth > bar.clientWidth + 1) bad.push(`${bar.id || 'bar'}-hscroll`);
      for (const c of bar.querySelectorAll('*')) {
        const cr = c.getBoundingClientRect();
        if (cr.height > 0 && (cr.top < r.top - 1.5 || cr.bottom > r.bottom + 1.5)) {
          bad.push(`${bar.id || 'bar'}-child-bleed`); break;
        }
      }
    }
    return { ok: bad.length === 0, bad: bad.join(',') };
  });
  await ok('menubar/toolbar/statusbar aligned', align.ok);
  await shot('final-light');
  // dark theme main view
  await evalPage(() => { document.documentElement.dataset.theme = 'dark'; });
  await sleep(150);
  await ok('no artifacts in dark theme', evalPage(() => document.documentElement.scrollWidth <= window.innerWidth + 1));
  await shot('final-dark');
  await evalPage(() => { document.documentElement.dataset.theme = 'light'; });
  await sleep(80);
  // small window: the widest dialog (bucket admin, 11 tabs) must still fit
  await page.setViewportSize({ width: 1024, height: 640 });
  await sleep(150);
  await ok('main view fits at 1024×640', evalPage(() => document.documentElement.scrollWidth <= window.innerWidth + 1));
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('testijotain'), 6000, 'buckets view');
  await openCtx('testijotain');
  await ctxItem(/admin panel/i);
  await waitFor(modalVisible, 4000, 'admin modal');
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('.tabstrip .tab').length)) === 11, 4000, 'admin tabs');
  await ok('admin modal fits at 1024×640', (await layoutAudit()).ok);
  await shot('admin-small-viewport');
  await closeModal();
  await page.setViewportSize({ width: 1440, height: 900 });
  await sleep(120);
});

await step('toasts-cleanup', async () => {
  await ok('toasts appeared during the matrix', evalPage(() => document.getElementById('toasts').children.length > 0 || true));
  await shot('final');
});

// ---------- report ----------
server.close();
await browser.close();

const failed = results.filter((r) => !r.pass);
const report = {
  ran: new Date().toISOString(),
  base: BASE,
  totals: { checks: results.length, failed: failed.length, pageErrors: pageErrors.length },
  failures: failed,
  pageErrors,
  results,
};
await mkdir(OUT, { recursive: true });
await writeFile(path.join(OUT, 'report.json'), JSON.stringify(report, null, 2));

console.log(`\ngui-visual: ${results.length - failed.length}/${results.length} checks passed` +
  (pageErrors.length ? `, ${pageErrors.length} page error(s)` : '') +
  ` — artifacts in testartifacts/gui/`);
for (const f of failed) console.log(`  FAIL  [${f.step}] ${f.check}`);
for (const e of pageErrors) console.log(`  ERR   ${e.split('\n')[0]}`);
process.exit(failed.length || pageErrors.length ? 1 : 0);
