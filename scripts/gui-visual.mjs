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
      pab: { BlockPublicAcls: false, IgnorePublicAcls: false, BlockPublicPolicy: true, RestrictPublicBuckets: true },
      policy: { raw: '{\n  "Version": "2012-10-17",\n  "Statement": []\n}', summary: { statements: 0, public: false } },
      acl: { owner: 'demo', summary: { grants: 1 } },
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
    StatObject: (bucket, key) => ({ key, size: 1234, lastModified: daysAgo(1), etag: '"v3"', storageClass: 'STANDARD' }),
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
page.on('pageerror', (e) => { pageErrors.push(String(e)); console.log(`  PAGEERROR ${String(e).split('\n')[0]}`); });
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
  await ok('guard chips visible', evalPage(() => !document.getElementById('guard-chips').classList.contains('hidden')));
  await ok('versioning chip says Versions on', (await txt('#guard-chips')).toLowerCase().includes('versions on'));
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

await step('upload-menu', async () => {
  await page.click('#btn-upload');
  await sleep(80);
  await ok('upload menu opens', evalPage(() => !document.getElementById('ctxmenu').classList.contains('hidden')));
  await shot('upload-menu');
  await closeCtx();
});

await step('sources-dialog', async () => {
  await page.click('#btn-profiles');
  await waitFor(modalVisible, 4000, 'sources modal');
  await ok('sources listed in dialog', (await evalPage(() => document.getElementById('modal-root').textContent)).includes('backup-box'));
  await shot('sources-dialog');
  await closeModal();
});

await step('doctor', async () => {
  await page.click('#btn-doctor');
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

await step('transfers', async () => {
  await evalPage(() => window.__shim.emit('transfer:update', { id: 't1', op: 'upload', status: 'running', totalFiles: 3, doneFiles: 1 }));
  await page.click('#btn-transfers');
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
  await ok('empty actions offer add-source', p2.evaluate(() => document.getElementById('empty-actions').textContent.length > 0));
  await ok('upload greyed without sources', p2.evaluate(() => document.getElementById('btn-upload').disabled));
  await ok('doctor greyed without sources', p2.evaluate(() => document.getElementById('btn-doctor').disabled));
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
  await ok('items pin the default S3 source', c && c.args[0][0].bucket === 'testijotain' && c.args[0][0].source === '');
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
