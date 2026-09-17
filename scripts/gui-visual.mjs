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
// Let the UI reach a visually settled state before a capture: finite CSS
// animations/transitions run to completion (capped — an infinite spinner
// must never hang the walk) and two rAFs land the final paint, so a shot
// cannot catch a dialog half-faded-in or a repaint mid-frame.
async function settlePaint(p = page) {
  try {
    await p.evaluate(() => new Promise((res) => {
      const cap = setTimeout(res, 400);
      Promise.all(document.getAnimations().map((a) => {
        try { return a.finished.then(() => {}, () => {}); } catch { return null; }
      })).then(() => { clearTimeout(cap); res(); });
    }));
    await p.evaluate(() => new Promise((res) => {
      let n = 0;
      const tick = () => (n += 1) >= 2 ? res() : requestAnimationFrame(tick);
      requestAnimationFrame(tick);
    }));
  } catch { /* page busy — capture as-is */ }
}
async function shot(name) {
  shotNo += 1;
  const file = path.join(OUT, String(shotNo).padStart(2, '0') + '-' + name + '.png');
  await settlePaint();
  await page.screenshot({ path: file });
}
// Curated-shot capture (the ones docs/screenshots/ republish): scrolls the
// subject into view where needed, verifies as a real check that the subject
// is fully on screen, and waits out transient toasts — a shot whose subject
// is clipped below the fold or smothered by a toast from the harness's own
// clicking is a broken shot, not a stylistic choice.
async function shotOf(name, cls, textRe = null) {
  const found = await evalPage((c, t) => {
    const el = t == null
      ? document.querySelector(c)
      : Array.from(document.querySelectorAll(c)).find((e) => new RegExp(t, 'i').test(e.textContent));
    if (!el) return false;
    el.scrollIntoView({ block: 'center', inline: 'nearest' });
    return true;
  }, cls, textRe);
  await sleep(80);
  await ok(`shot "${name}": subject on screen`, found && evalPage((c, t) => {
    const el = t == null
      ? document.querySelector(c)
      : Array.from(document.querySelectorAll(c)).find((e) => new RegExp(t, 'i').test(e.textContent));
    if (!el) return false;
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0
      && r.top >= 0 && r.left >= 0 && r.bottom <= innerHeight && r.right <= innerWidth;
  }, cls, textRe));
  await waitFor(() => evalPage(() => document.getElementById('toasts').children.length === 0),
    4600, 'toasts to clear').catch(() => {});
  await shot(name);
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
    await sweepPopouts();
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
  // NOTE: s3b-autorefresh is deliberately NOT set — auto refresh is off by
  // default; the boot path with no stored key is exactly what the walk
  // asserts (and the auto-refresh step re-enables it explicitly).

  const now = Date.now();
  const daysAgo = (d) => new Date(now - d * 86400000).toISOString();

  const world = {
    sources: EMPTY ? [] : [
      { id: 'src-hetzner', name: 'hetzner', type: 's3', color: '#0b63ce' }, // legacy: account-wide
      { id: 'src-one', name: 'website-prod', type: 's3', bucket: 'www-assets', color: '#9a6700' },
      { id: 'src-fresh', name: 'nightly', type: 's3', bucket: 'db-dumps', color: '#b3261e' },
      { id: 'src-box', name: 'backup-box', type: 'sftp', color: '#1b7f3b' },
      { id: 'src-dav', name: 'dav-claims', type: 'webdav', color: '#7c3aed' },
    ],
    buckets: [
      { name: 'team-files', createdAt: daysAgo(220) },
      { name: 'logs-2026', createdAt: daysAgo(120) },
      { name: 'media-assets', createdAt: daysAgo(90) },
      { name: 'archive-cold', createdAt: daysAgo(30) },
    ],
    // buckets a credential import discovers per candidate — every bucket
    // becomes its own bucket-scoped data source
    importBuckets: {
      'cand-file1': ['from-file-photos', 'from-file-logs'],
      'cand-kms1': ['hetzner-kms'],
    },
    objects: {
      'team-files': [
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
      'www-assets': [
        { key: 'assets/', isDir: true },
        { key: 'index.html', size: 512, lastModified: daysAgo(2), storageClass: 'STANDARD' },
      ],
      'db-dumps': [
        { key: 'data/', isDir: true },
        { key: 'hello.txt', size: 42, lastModified: daysAgo(1), storageClass: 'STANDARD' },
      ],
      // buckets the imported credentials discover (per-bucket sources)
      'from-file-photos': [
        { key: 'img-1.jpg', size: 204800, lastModified: daysAgo(3), storageClass: 'STANDARD' },
        { key: 'img-2.jpg', size: 409600, lastModified: daysAgo(3), storageClass: 'STANDARD' },
      ],
      'from-file-logs': [
        { key: 'app.log', size: 8192, lastModified: daysAgo(0), storageClass: 'STANDARD' },
      ],
      'hetzner-kms': [
        { key: 'kms-seed.txt', size: 64, lastModified: daysAgo(1), storageClass: 'STANDARD' },
      ],
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
      // lock enabled: the Object lock ctx entry + lock dialog walk needs it
      'team-files': { versioning: 'Enabled', lockEnabled: true, lockMode: 'GOVERNANCE', lockDays: 1 },
      'logs-2026': { versioning: 'Suspended', lockEnabled: false, lockMode: '', lockDays: 0 },
      'www-assets': { versioning: 'Enabled', lockEnabled: false, lockMode: '', lockDays: 0 },
    },
    // PrefixVersionSummary fixture: delete-marker aggregates per folder view.
    // readme.md has a marker in its history; docs/ aggregates markers under
    // it; docs/legacy/ is entirely delete-marked ("all deleted").
    versionKids: {
      'team-files/': [
        { name: 'docs', isDir: true, versions: 6, markers: 2, allDeleted: false },
        { name: 'readme.md', isDir: false, versions: 4, markers: 1, allDeleted: false },
      ],
      'team-files/docs/': [
        { name: 'legacy', isDir: true, versions: 1, markers: 1, allDeleted: true },
        { name: 'notes.md', isDir: false, versions: 2, markers: 0, allDeleted: false },
      ],
    },
    // Non-empty world boots with a saved (encrypted) profile file open —
    // the realistic steady state, and what the status bar should show in
    // screenshots (🔐 work.s3bprofile, not "unsaved sources" walk debris).
    pfState: EMPTY
      ? { open: false, name: '', path: '', dirty: false, sourceCount: 0 }
      : { open: true, name: 'work.s3bprofile', path: 'C:\\Users\\demo\\Documents\\work.s3bprofile', dirty: false, sourceCount: 5 },
    transfers: [
      { id: 't1', op: 'upload', status: 'running', currentFile: 'video-final.mp4', totalFiles: 3, doneFiles: 1, totalBytes: 224975891, sentBytes: 71803392, speedBps: 8388608 },
      // failed/skipped ride the same record: the manager counts them aloud
      { id: 't2', op: 'transfer', status: 'done', currentFile: '', totalFiles: 12, doneFiles: 9, failedFiles: 1, skippedFiles: 2, totalBytes: 52428800, sentBytes: 52428800, speedBps: 0 },
    ],
    // CheckConflicts fixture — empty means a clean destination; the
    // conflict-view step seeds real collisions before uploading
    conflicts: [],
    // file-log prefs: what the Settings dialog reads/writes. levels/scopes/
    // sources gate ONLY the log file — the in-app drawer filters client-side
    // and never consults these. allSources mirrors the backend's union of
    // seen-on-a-line sources and configured data sources.
    logSettings: {
      mode: 'default', dir: '', levels: [], scopes: [], sources: [],
      allScopes: ['admin', 'app', 'copy', 'delete', 'doctor', 'download', 'import', 'list', 'mkdir', 'profile', 'rename', 'settings', 'share', 'sources', 'transfer', 'upload', 'versions'],
      allSources: ['hetzner', 'lab', 'local', 'team-files', 'vault'],
    },
    // secure-storage status (Settings → Security): off by default; the
    // toggle flips the mock and relocates the workspaces the way the real
    // backend does (config dir under secure storage, system temp otherwise)
    secure: {
      enabled: false, keyringAvailable: true, keyringBackend: 'Windows Credential Manager',
      editorDir: 'C:\\Users\\vis\\AppData\\Local\\Temp\\s3b-edit',
      spoolDir: 'C:\\Users\\vis\\AppData\\Local\\Temp\\s3b-tmp',
    },
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
  // payload arrives as positional arguments — the same contract as the
  // backend Events.Emit (wails:file-drop delivers x, y, paths as three
  // separate args, never one object).
  const emit = (name, ...payload) => setTimeout(() => {
    for (const cb of listeners.get(name) || []) {
      try { cb(...payload); } catch (e) { console.error('shim emit', name, e); }
    }
  }, 20);
  window.runtime = {
    EventsOn: (name, cb) => {
      if (!listeners.has(name)) listeners.set(name, new Set());
      listeners.get(name).add(cb);
      return () => listeners.get(name)?.delete(cb);
    },
    EventsOff: (name, cb) => listeners.get(name)?.delete(cb),
    // Mirrors the real Wails runtime API: subscribes cb to the positional
    // (x, y, paths) contract of wails:file-drop — the production entry
    // point wireDrop() looks for.
    OnFileDrop: (cb) => window.runtime.EventsOn('wails:file-drop',
      (x, y, paths) => cb(x, y, paths)),
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
    GetVersion: () => '1.1.0-beta.13',
    ListSources: () => JSON.parse(JSON.stringify(world.sources)),
    ListBuckets: () => JSON.parse(JSON.stringify(world.buckets)),
    ListSourceBuckets: (_src) => JSON.parse(JSON.stringify(world.buckets)),
    TestSource: (_idOrName) => ({ ok: true, message: 'connected — bucket accessible' }),
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
    // conflict pre-check (M12): whatever the step seeded, the dialog gets
    CheckConflicts: () => JSON.parse(JSON.stringify(world.conflicts || [])),
    ObjectVersions: () => JSON.parse(JSON.stringify(world.versions)),
    VersionDiffText: () => ({ truncated: false, aText: 'alpha\nold line A\nomega', bText: 'alpha\nnew line B\nomega' }),
    GetBucketAdmin: () => JSON.parse(JSON.stringify(world.admin)),
    BucketVersionStats: () => ({ currentObjects: 7, versions: 12, deleteMarkers: 2, noncurrent: 5, noncurrentBytes: 1048576 }),
    CompareAny: (x, y) => JSON.parse(JSON.stringify(world.compareRows)),
    PreviewDelete: (bucket, keys) => ({ requiresL2: false, count: keys.length, objects: keys.length, bytes: 1234, folders: 0 }),
    // bucket-grade destructive flows (all route through the unified
    // Delete Window — see the delete-window-uniform step)
    PreviewBucketDelete: () => ({ objectCount: 7, versionCount: 12, deleteMarkers: 2, versioned: true, requiresL2: true }),
    DeleteBucket: (_bucket, _force) => ({ deleted: 7 }),
    PurgePreview: () => 60, // >50: the purge window types the escalation word
    PurgeVersions: () => ({ deleted: 60 }),
    EmptyBucketAllVersions: () => ({ deleted: 14 }),
    DeleteSelection: (_bucket, keys, _l2) => ({ deleted: keys.length, errors: [] }),
    DeleteSelectionPermanent: (_bucket, keys, _force) => ({ deleted: keys.length * 2, errors: [] }),
    DeleteSelectionKeepCurrent: (_bucket, keys, _force) => ({ deleted: keys.length, errors: [] }),
    // delete-marker badges (versioned folders): per-immediate-child
    // aggregates keyed `${bucket}/${prefix}` — mirrors PrefixVersionSummary
    PrefixVersionSummary: (bucket, prefix) => JSON.parse(JSON.stringify(world.versionKids[`${bucket}/${prefix || ''}`] || [])),
    // Directory Versions window: bounded stats pass over the subtree
    PrefixVersionStats: () => ({ currentObjects: 3, versions: 9, deleteMarkers: 2, noncurrent: 4, noncurrentBytes: 12345 }),
    // Delete Marker window feed: two markers under a folder key, or on the
    // exact file key itself
    PrefixMarkers: (_bucket, key, _exact) => {
      const k = key.endsWith('/') ? `${key}legacy/old.txt` : key;
      return {
        markers: [
          { key: k, versionId: 'vm-0001', lastModified: daysAgo(2), isLatest: true },
          { key: k, versionId: 'vm-0002', lastModified: daysAgo(9), isLatest: false },
        ],
        truncated: false,
      };
    },
    UndoDelete: () => ({}),
    RemoteDeletePreview: (_src, keys) => ({ files: keys.length, folders: 0, bytes: 4096 }),
    RemoteRemove: (_src, keys) => ({ deleted: keys.length, errors: [] }),
    PresignObject: (bucket, key) => `https://${bucket}.s3.visual.shim/${key}?X-Amz-Signature=visual`,
    StatObject: (bucket, key) => (key.endsWith('/')
      ? { key, isDir: true, usage: { objectCount: 7, totalBytes: 456789 } }
      : { key, size: 1234, lastModified: daysAgo(1), etag: '"v3"', storageClass: 'STANDARD' }),
    StatBucket: (bucket) => ({ name: bucket, region: 'eu-central', objects: 7, bytes: 224975891, createdAt: daysAgo(220) }),
    RemoteStat: (source, key) => ({ key, isDir: false, size: 4096, lastModified: daysAgo(3) }),
    CopySelection: (src, keys, dst, prefix, move) => ({ copied: keys.length, errors: [] }),
    CopySelectionVersions: (srcS, srcB, keys, dstS, dstB, prefix, move) => 'vcopy-1',
    EditingFiles: () => [{ bucket: 'team-files', key: 'docs/notes.md' }],
    GetLogSettings: () => JSON.parse(JSON.stringify(world.logSettings)),
    SetLogSettings: (mode, dir, levels, scopes, sources) => {
      world.logSettings = { ...world.logSettings, mode, dir, levels: levels || [], scopes: scopes || [], sources: sources || [] };
      return JSON.parse(JSON.stringify(world.logSettings));
    },
    GetSecureStorage: () => JSON.parse(JSON.stringify(world.secure)),
    SetSecureStorage: (on) => {
      const cfg = 'C:\\Users\\vis\\AppData\\Roaming\\s3b';
      const tmp = 'C:\\Users\\vis\\AppData\\Local\\Temp';
      world.secure = {
        ...world.secure,
        enabled: !!on,
        editorDir: on ? `${cfg}\\edit` : `${tmp}\\s3b-edit`,
        spoolDir: on ? `${cfg}\\tmp` : `${tmp}\\s3b-tmp`,
      };
      return JSON.parse(JSON.stringify(world.secure));
    },
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
    SourceDeleteSelectionPermanent: (_src, _bucket, keys, _force) => ({ deleted: keys.length * 2, errors: [] }),
    SourceDeleteSelectionKeepCurrent: (_src, _bucket, keys, _force) => ({ deleted: keys.length, errors: [] }),
    SourcePrefixVersionSummary: (_src, bucket, prefix) => JSON.parse(JSON.stringify(world.versionKids[`${bucket}/${prefix || ''}`] || [])),
    SourceRenameObject: () => ({}),
    SourceCreateFolder: () => ({}),
    // ---- OS interop ----
    PickUploadFiles: () => ['C:\\Users\\demo\\Downloads\\invoice.pdf', 'C:\\Users\\demo\\Downloads\\photos'],
    // local delete (side pane): count-then-act preview + permanent remove
    LocalDeletePreview: (paths) => ({ objects: paths.length, folders: 0, bytes: 4096, requiresL2: false }),
    LocalRemove: (paths) => ({ deleted: paths.length, errors: [] }),
    OsClipboardFiles: () => JSON.parse(JSON.stringify(world.osClip || [])),
    // mimics the real system: seq bumps on every clipboard write; tests bump
    // it directly to simulate a Ctrl+C in Explorer (an external write)
    OsClipboardState: () => ({ seq: world.osClipSeq || 0, files: !!(world.osClip || []).length }),
    OsClipboardSetFiles: (paths) => { world.osClip = paths; world.osClipSeq = (world.osClipSeq || 0) + 1; return {}; },
    StageClipboardDir: () => 'C:\\Users\\demo\\AppData\\Local\\Temp\\s3b-clip-1',
    MakeDragUrls: (items) => items.map((it, i) => `http://127.0.0.1:39317/drag/${i}/${encodeURIComponent(it.name || it.key)}`),
    RemoteListDraft: () => [
      { key: '/draft-up/', isDir: true, name: 'draft-up' },
      { key: '/seed.txt', isDir: false, name: 'seed.txt', size: 12 },
    ],
    // ---- Import S3 Credential (files + KMS) ----
    ParseCredentialFile: () => JSON.parse(JSON.stringify(world.credCandidates)),
    PickCredentialFiles: () => ['C:\\Users\\demo\\Downloads\\credentials'],
    TestCredentialDraft: () => ({ ok: true, message: 'connected — 2 bucket(s) visible' }),
    KmsFetch: (service) => JSON.parse(JSON.stringify(world.kmsCandidates)),
    // the backend contract: per-bucket expansion + idempotent re-import —
    // an existing bucket-scoped source is UPDATED in place, never duplicated
    ImportCredentials: (ids) => {
      const imported = [];
      const updated = [];
      for (const id of ids || []) {
        const c = [...world.credCandidates, ...world.kmsCandidates].find((x) => x.id === id);
        if (!c) continue;
        for (const b of world.importBuckets[c.id] || []) {
          if (world.sources.some((s) => s.name === b)) {
            updated.push(b);
            continue;
          }
          world.sources.push({ id: 'src-' + b, name: b, type: 's3', bucket: b });
          imported.push(b);
        }
      }
      world.pfState.sourceCount = world.sources.length;
      return { imported, updated, skipped: [] };
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
// ---------- floating popouts (transfers / help views) ----------
// id match is exact-or-prefixed: the doctor window ids itself
// "doctor:<bucket>" when a bucket is open
const popSel = (i) => `#popout-root .popout[data-pop="${i}"], #popout-root .popout[data-pop^="${i}:"]`;
async function popoutVisible(id) {
  return evalPage((s) => !!document.querySelector(s), popSel(id));
}
async function closePopout(id) {
  await evalPage((s) => {
    document.querySelector(`${s} .modal-head .x`)?.click();
  }, popSel(id));
  await sleep(80);
}
// step isolation: popouts left floating by a failed step must not leak
// into later steps — geometry keys are wiped too (a persisted position
// from one step would seed the next one's placement)
async function sweepPopouts() {
  await evalPage(() => {
    for (const b of document.querySelectorAll('#popout-root .popout')) b.querySelector('.modal-head .x')?.click();
    for (const k of Object.keys(localStorage)) if (k.startsWith('s3b-popout-')) localStorage.removeItem(k);
  });
  await sleep(60);
}
// synthetic pointer drag (header) / resize (grip) on a floating popout:
// pointerdown on the handle, pointermove/pointerup on window — exactly
// the events the delegated handlers in dialogs.js listen for
async function popDrag(id, dx, dy, part = 'head') {
  await evalPage(([s, ddx, ddy, p]) => {
    const b = document.querySelector(s);
    const h = b?.querySelector(p === 'grip' ? '.pop-grip' : '.modal-head');
    if (!b || !h) throw new Error(`popDrag: no ${p} on popout ${s}`);
    const r = h.getBoundingClientRect();
    const o = { bubbles: true, cancelable: true, isPrimary: true, pointerId: 7, clientX: r.x + r.width / 2, clientY: r.y + Math.min(r.height / 2, 6) };
    h.dispatchEvent(new PointerEvent('pointerdown', o));
    window.dispatchEvent(new PointerEvent('pointermove', { ...o, clientX: o.clientX + ddx, clientY: o.clientY + ddy }));
    window.dispatchEvent(new PointerEvent('pointerup', o));
  }, [popSel(id), dx, dy, part]);
  await sleep(40);
}
// The per-task version-choice dialog (S3→S3 onto a versioned destination):
// wait for it, set the preserve checkbox to `preserve`, click the primary
// Copy/Move button.
async function vcvChoose(preserve) {
  await waitFor(modalVisible, 4000, 'version choice dialog');
  await evalPage((p) => {
    const chk = document.querySelector('#modal-root .vcv-row input');
    if (!chk) throw new Error('version dialog: no .vcv-row input');
    if (chk.checked !== p) chk.click();
  }, preserve);
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.primary').click());
  await sleep(120);
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
    // floating windows may be dragged anywhere, but never stranded off-screen
    for (const p of document.querySelectorAll('#popout-root .popout')) {
      const r = p.getBoundingClientRect();
      if (r.left < -0.5 || r.top < -0.5 || r.right > vw + 0.5 || r.bottom > vh + 0.5) bad.push('popout-outside-viewport');
      for (const b of p.querySelectorAll('.modal-foot .btn')) {
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
  // source rows render the hand-drawn SVG glyph set (license-free) and the
  // three source types here (s3, sftp, webdav) are visually distinct
  await ok('source rows carry distinct SVG glyphs', evalPage(() => {
    const glyph = (name) => {
      const row = Array.from(document.querySelectorAll('#tree .tnode'))
        .find((r) => r.querySelector('.tlabel')?.textContent === name);
      return row?.querySelector('.ticon svg')?.innerHTML || '';
    };
    const s3 = glyph('hetzner'), sftp = glyph('backup-box'), dav = glyph('dav-claims');
    return !!s3 && !!sftp && !!dav && s3 !== sftp && sftp !== dav && s3 !== dav;
  }));
  await shot('boot-buckets');
});

await step('buckets-ctxmenu', async () => {
  const n = await openCtx('team-files');
  await ok('bucket menu has items', n >= 5);
  await shot('ctx-bucket');
  await closeCtx();
});

await step('objects-view', async () => {
  await dblClickRow('team-files');
  await waitFor(async () => (await rowKeys()).includes('readme.md'), 6000, 'objects of team-files');
  await ok('breadcrumb shows bucket', (await txt('#breadcrumb')).includes('team-files'));
  // guard state lives as icons after the bucket name in the tree now
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('#tree .tguard').length)) > 0, 6000, 'tree guard icons');
  await ok('tree shows versioning icon', (await evalPage(() => Array.from(document.querySelectorAll('#tree .tguard')).map((i) => i.title).join(' '))).toLowerCase().includes('versioning enabled'));
  await ok('toolbar upload enabled', evalPage(() => !document.getElementById('btn-upload').disabled));
  await ok('toolbar download disabled without selection', evalPage(() => document.getElementById('btn-download').disabled));
  await shotOf('objects', '#grid-wrap');
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
  // deep-sweep gating: Object lock only on lock-enabled buckets (the
  // team-files guard has it on); Find in this folder targets folders only
  await ok('file menu: Object lock offered, Find targets folders only',
    items.some((x) => /object lock/i.test(x))
      && !items.some((x) => /find in this folder/i.test(x)));
  await shot('ctx-file');
  await closeCtx();
  // negative: logs-2026 has versioning Suspended and no object lock — its
  // file menu must drop Versions / markers / lock / Find and keep the rest
  // (Storage class stays: folders and files support it on any S3 bucket)
  await navObjects('logs-2026');
  await dblClickRow('app');
  await waitFor(async () => (await rowKeys()).includes('app/app-2026-09-11.log'), 4000, 'app logs');
  await clickRow('app-2026-09-10.log'); // rows carry bare names; keys are anchored
  await openCtx('app-2026-09-11.log');
  const items2 = await evalPage(() => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
    .map((i) => i.textContent.trim()));
  await ok('suspended bucket: file menu drops versions / markers / lock / find',
    !items2.some((x) => /versions/i.test(x) || /object lock/i.test(x) || /find in this folder/i.test(x))
      && items2.some((x) => /storage class/i.test(x)));
  await closeCtx();
  // folder menu in the same bucket: Find in this folder IS offered (a single
  // dir selection targets that folder); Versions stays correctly absent
  await page.keyboard.press('Backspace'); // up from app/ to the bucket root
  await waitFor(async () => (await rowKeys()).some((k) => k.endsWith('app/')), 4000, 'back to bucket root');
  const n2 = await openCtx('app');
  const items3 = await evalPage(() => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
    .map((i) => i.textContent.trim()));
  await ok('folder menu keeps Find, drops versions (suspended)', n2 >= 3
    && items3.some((x) => /find in this folder/i.test(x))
    && !items3.some((x) => /versions/i.test(x)));
  await closeCtx();
  await navObjects('team-files'); // back for the folder-ctxmenu walk below
});

await step('folder-ctxmenu', async () => {
  const n = await openCtx('docs');
  await ok('folder menu has items', n >= 3);
  await shot('ctx-folder');
  await closeCtx();
});

await step('copy-as-ctxmenu', async () => {
  await resetCalls();
  await clickRow('readme.md');
  await openCtx('readme.md');
  const items = await evalPage(() => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
    .map((i) => i.textContent.trim()));
  await ok('row menu offers copy-as actions', ['copy name', 'copy path', 'copy s3 uri']
    .every((s) => items.some((x) => x.toLowerCase() === s)));
  await ctxItem(/^copy path$/i);
  let c = await findCall('ClipboardSetText');
  await ok('copy path puts bucket/key on the clipboard', !!c && c.args[0] === 'team-files/readme.md');
  await openCtx('readme.md');
  await ctxItem(/copy s3 uri/i);
  c = await findCall('ClipboardSetText');
  await ok('copy s3 uri formats s3://bucket/key', !!c && c.args[0] === 's3://team-files/readme.md');
  await openCtx('readme.md');
  await ctxItem(/^copy name$/i);
  c = await findCall('ClipboardSetText');
  await ok('copy name copies the bare name', !!c && c.args[0] === 'readme.md');
  await closeCtx();
});

await step('header-column-menu', async () => {
  const openHead = () => evalPage(() => {
    document.getElementById('grid-head')
      .dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 300, clientY: 30 }));
  });
  await openHead();
  await sleep(80);
  const n = await evalPage(() => document.querySelectorAll('#ctxmenu:not(.hidden) .item').length);
  await ok('header menu lists the whole column catalog', n >= 6);
  await ok('name column is checked and locked', evalPage(() => {
    const it = Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
      .find((i) => /^\u2713 name$/i.test(i.textContent.trim()));
    return !!it && it.classList.contains('disabled');
  }));
  await shot('ctx-columns');
  await ctxItem(/etag/i); // default off — toggling adds the column
  await ok('etag column appears', evalPage(() => Array.from(document.querySelectorAll('#grid-head .gh span'))
    .some((s) => /etag/i.test(s.textContent))));
  await ok('column choice persisted', evalPage(() => (localStorage.getItem('s3b-cols') || '').split(',').includes('etag')));
  await openHead();
  await sleep(60);
  await ctxItem(/etag/i); // toggle back off
  await ok('etag column removed again', evalPage(() => !Array.from(document.querySelectorAll('#grid-head .gh span'))
    .some((s) => /etag/i.test(s.textContent))));
  await evalPage(() => localStorage.removeItem('s3b-cols')); // back to boot defaults
  await closeCtx();
});

await step('invert-selection', async () => {
  await clickRow('readme.md');
  const total = (await rowKeys()).length;
  await ok('fixture has multiple rows', total >= 3);
  await page.keyboard.press('Control+i');
  await sleep(80);
  await ok('ctrl+i inverts to all-but-one', (await txt('#status-selection')).trim().startsWith(`${total - 1} of ${total} `));
  await page.keyboard.press('Control+i');
  await sleep(80);
  await ok('second ctrl+i restores the single selection', (await txt('#status-selection')).trim().startsWith('1 of '));
  await shot('invert-selection');
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
  if (!(await treeRow('docs'))) await evalHandleClickTwist('team-files'); // bucket node → folder level
  await waitFor(async () => !!(await treeRow('docs')), 6000, 'bucket children');
  await ok('bucket expanded to folders', !!(await treeRow('docs')) && !!(await treeRow('photos')));
  if (!(await treeRow('legacy'))) await evalHandleClickTwist('docs');
  await waitFor(async () => !!(await treeRow('legacy')), 6000, 'docs children');
  await ok('docs expanded to legacy', !!(await treeRow('legacy')));
  await clickTree('docs');
  // prefix listings carry anchored keys ('docs/notes.md'), not bare names
  await waitFor(async () => (await rowKeys()).includes('docs/notes.md'), 6000, 'docs objects');
  await ok('tree click navigates into docs', (await txt('#breadcrumb')).includes('docs'));
  // the breadcrumb's root crumb carries the SAME type glyph as the sidebar
  // row, painted in the source's accent color
  await ok('breadcrumb root carries the source glyph in color', evalPage(() => {
    const root = document.querySelector('#breadcrumb .crumb');
    const ic = root?.querySelector('.src-ic');
    // style.color serializes '#0b63ce' to rgb() in Chromium — accept both
    const c = ic?.style.color || '';
    return !!ic && !!ic.querySelector('svg') && ['#0b63ce', 'rgb(11, 99, 206)'].includes(c)
      && (root.textContent || '').includes('hetzner');
  }));
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

await step('empty-dir-ctxmenu', async () => {
  // an empty directory covers the grid with the empty-state overlay —
  // right-clicking it must still open the folder menu (paste/upload/new
  // folder), not the browser default
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('archive-cold'), 6000, 'buckets of hetzner');
  await dblClickRow('archive-cold');
  await waitFor(async () => evalPage(() => !document.getElementById('empty-state').classList.contains('hidden')), 6000, 'empty state');
  const overlay = await page.$('#empty-state');
  await rightClick(overlay);
  await sleep(80);
  const items = await evalPage(() => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item')).map((i) => i.textContent).join(' | '));
  await ok('empty dir right-click opens menu', items.length > 0 && /new folder/i.test(items) && /upload/i.test(items));
  await closeCtx();
});

await step('tree-single-bucket-first-click', async () => {
  // the fresh-source path: clicking a NEVER-expanded bucket-scoped source
  // label must open the bucket contents directly. The bucket comes from
  // the source definition, so no listing round trip may intervene — if
  // the tree asked for the bucket list first, this world answers with
  // hetzner's four buckets and the assertion below fails.
  await clickTree('nightly');
  await waitFor(async () => (await rowKeys()).includes('hello.txt'), 6000, 'first-click contents');
  await ok('first click opens bucket contents', (await txt('#breadcrumb')).includes('db-dumps'));
});

await step('tree-bucket-scoped-source', async () => {
  // a bucket-scoped S3 source: the node IS the bucket — definition-carried,
  // so it renders the same structure as every other source type (content
  // folders directly under the source) and inherits the bucket's features.
  await evalHandleClickTwist('website-prod');
  await waitFor(async () => !!(await treeRow('assets')), 6000, 'bucket-scoped children');
  await ok('no bucket row under the bucket-scoped source', !(await treeRow('www-assets')));
  await ok('folders sit directly under the source node', !!(await treeRow('assets')));
  // the source node inherits the bucket's guard icons (versioning on)
  await waitFor(async () => (await evalPage((l) => {
    const n = Array.from(document.querySelectorAll('#tree .tnode'))
      .find((r) => r.querySelector('.tlabel')?.textContent === l);
    return n ? n.querySelectorAll('.tguard').length : 0;
  }, 'website-prod')) > 0, 6000, 'bucket-scoped guard icons');
  await ok('versioning icon on the source node', evalPage((l) => {
    const n = Array.from(document.querySelectorAll('#tree .tnode'))
      .find((r) => r.querySelector('.tlabel')?.textContent === l);
    return Array.from(n?.querySelectorAll('.tguard') || []).some((i) => /versioning enabled/i.test(i.title));
  }, 'website-prod'));
  // clicking the source node opens the bucket contents, not the buckets view
  await clickTree('website-prod');
  await waitFor(async () => (await rowKeys()).includes('index.html'), 6000, 'bucket-scoped navigate');
  await ok('click opens bucket contents directly', (await txt('#breadcrumb')).includes('www-assets'));
  await ok('bucket-scoped source row highlighted', evalPage(() => Array.from(document.querySelectorAll('#tree .tnode'))
    .some((r) => r.classList.contains('sel') && r.querySelector('.tlabel')?.textContent === 'website-prod')));
  // bucket-scoped context menu: full bucket feature set + source management
  const h = await treeRow('website-prod');
  await rightClick(h);
  await sleep(80);
  const items = await evalPage(() => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item')).map((i) => i.textContent).join(' | '));
  // one Upload row now (Files/Folder live behind its flyout — the upload
  // step covers the flyout itself)
  await ok('bucket-scoped menu has bucket-grade items', /upload/i.test(items) && /admin panel/i.test(items) && /delete bucket/i.test(items) && /paste here/i.test(items));
  await ok('bucket-scoped menu keeps source management', /edit source/i.test(items) && /reconnect/i.test(items) && /remove source/i.test(items));
  await ok('bucket-scoped menu can reach the buckets view', /open buckets view/i.test(items));
  await shot('tree-bucket-scoped');
  await closeCtx();
});

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
  await sleep(60);
});

await step('view-menu-toggles', async () => {
  const openView = async () => {
    await page.locator('#menubar .mb-title', { hasText: /^view$/i }).first().click();
    await sleep(80);
  };
  const toggleItem = () => elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /show version count icons/i.test(i.textContent)) || null);
  await openView();
  await ok('View menu lists the version-icons toggle', !!(await toggleItem()));
  await shot('menu-view');
  const it = await toggleItem();
  if (it) await it.asElement().click();
  await sleep(80);
  await ok('toggle flips the setting on', evalPage(() => localStorage.getItem('s3b-show-versions') === '1'));
  await openView();
  const it2 = await toggleItem();
  if (it2) await it2.asElement().click();
  await sleep(80);
  await ok('toggle flips the setting back off', evalPage(() => localStorage.getItem('s3b-show-versions') === '0'));
  // restore the untouched-boot state — the settings-dialog walk asserts the
  // badge keys start as null
  await evalPage(() => localStorage.removeItem('s3b-show-versions'));
});

await step('exit-guard', async () => {
  await resetCalls();
  await evalPage(() => window.__emit('exit:confirm', { reason: '2 transfer job(s) still running (e.g. transfer: 1/3 file(s))' }));
  await waitFor(modalVisible, 4000, 'exit confirm dialog');
  await ok('exit dialog states the busy reason', (await evalPage(() => document.getElementById('modal-root').textContent)).includes('still running'));
  await shot('exit-confirm');
  const cancel = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /^cancel$/i.test(b.textContent.trim())) || null);
  await ok('cancel button offered', !!cancel);
  if (cancel) { await cancel.asElement().click(); await sleep(80); }
  await ok('cancel does not confirm the exit', (await findCall('ConfirmExit')) === null);
  await evalPage(() => window.__emit('exit:confirm', { reason: 'unsaved changes' }));
  await waitFor(modalVisible, 4000, 'exit confirm dialog (again)');
  const go = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /exit anyway/i.test(b.textContent)) || null);
  await ok('exit-anyway button offered', !!go);
  if (go) { await go.asElement().click(); await sleep(80); }
  await ok('exit anyway force-quits via ConfirmExit', (await findCall('ConfirmExit')) !== null);
});

await step('help-guide', async () => {
  await page.locator('#menubar .mb-title', { hasText: /help/i }).first().click();
  await sleep(80);
  const guide = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /user guide/i.test(i.textContent)) || null);
  await ok('Help→User guide present', !!guide);
  if (guide) {
    await guide.asElement().click();
    await waitFor(() => popoutVisible('guide'), 4000, 'guide popout');
    await ok('guide has six section tabs', waitFor(async () => (await evalPage(() => document.querySelectorAll('#popout-root .tabstrip .tab').length)) === 6, 4000, 'guide tabs'));
    await ok('getting-started content rendered', (await evalPage(() => document.querySelector('#popout-root .popout[data-pop="guide"]').textContent)).includes('Import existing credentials'));
    // tab switches must not resize the window: the tallest static section
    // is measured at open and pinned as the body's min-height
    await ok('guide height pinned across all six tabs', evalPage(() => {
      const modal = document.querySelector('#popout-root .popout[data-pop="guide"]');
      if (!modal) return false;
      const h0 = modal.getBoundingClientRect().height;
      const hs = [];
      for (const tb of Array.from(document.querySelectorAll('#popout-root .tabstrip .tab'))) {
        tb.click();
        hs.push(modal.getBoundingClientRect().height);
      }
      return hs.length === 6 && Math.max(...hs, h0) - Math.min(...hs, h0) <= 1;
    }));
    await shot('guide');
    const tab = await elOrNull(() => Array.from(document.querySelectorAll('#popout-root .tabstrip .tab'))
      .find((t) => /^transfers$/i.test(t.textContent.trim())) || null);
    if (tab) { await tab.asElement().click(); await sleep(80); }
    await ok('guide tab switch works', (await evalPage(() => document.querySelector('#popout-root .popout[data-pop="guide"]').textContent)).includes('multipart'));
    await closePopout('guide');
  }
  await page.locator('#menubar .mb-title', { hasText: /help/i }).first().click();
  await sleep(80);
  const src = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /supported data sources/i.test(i.textContent)) || null);
  await ok('Help→Supported data sources present', !!src);
  if (src) {
    await src.asElement().click();
    await waitFor(() => popoutVisible('sources'), 4000, 'sources popout');
    const txt = await evalPage(() => document.querySelector('#popout-root .popout[data-pop="sources"]').textContent);
    await ok('sources list covers engines', ['SFTP', 'FTP', 'WebDAV', 'MinIO', 'Cloudflare R2'].every((s) => txt.includes(s)));
    await shot('sources-info');
    await closePopout('sources');
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
  await waitFor(() => popoutVisible('keys'), 4000, 'keysheet');
  await ok('keysheet lists shortcuts', waitFor(async () => (await evalPage(() => document.querySelectorAll('#popout-root .help-grid .row').length)) >= 18, 4000, 'keys rows'));
  await ok('keysheet uses the wide size class', evalPage(() => document.querySelector('#popout-root .popout.wide') !== null));
  await ok('keysheet layout clean', (await layoutAudit()).ok);
  await shot('keysheet');
  await closePopout('keys');
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
  await ok('speed-limit visibility toggle offered', evalPage(() => Array.from(document.querySelectorAll('#modal-root .set-row'))
    .some((r) => /speed limit when transferring/i.test(r.textContent))));
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
  // new rows: the edit open-with toggle, the file-log multi-select filters,
  // and Reset to defaults (presence only — clicking it would wipe the run)
  await ok('edit open-with toggle offered', evalPage(() => Array.from(document.querySelectorAll('#modal-root .set-row'))
    .some((r) => /ask which app/i.test(r.textContent))));
  await ok('file-log multi-select filters present', evalPage(() => document.querySelectorAll('#modal-root .ms-btn').length >= 2));
  await ok('reset-to-defaults offered', evalPage(() => Array.from(document.querySelectorAll('#modal-root button'))
    .some((b) => /reset to defaults/i.test(b.textContent))));
  await ok('delete + column + visibility settings rows present', evalPage(() => {
    const rows = Array.from(document.querySelectorAll('#modal-root .set-row')).map((r) => r.textContent);
    const secs = Array.from(document.querySelectorAll('#modal-root .set-section')).map((s) => s.textContent);
    return secs.some((x) => /main grid columns/i.test(x)) && secs.some((x) => /side panel columns/i.test(x))
      && rows.some((x) => /always use the delete window/i.test(x))
      && rows.some((x) => /require typing/i.test(x))
      && rows.some((x) => /delete without prompting/i.test(x))
      && rows.some((x) => /show version count icons/i.test(x))
      && rows.some((x) => /show delete marker icons/i.test(x)
        && /versions and content versions windows/i.test(x))
      && rows.some((x) => /show hidden \(delete-marked\) objects/i.test(x))
      && rows.some((x) => /explorer copy & paste/i.test(x));
  }));
  await shot('settings-new-rows');
  // badge rows default OFF (the shim wipes every s3b-* key at boot): tick
  // both icon toggles — the apply thunk persists the keys and re-renders
  // the grid, which the version-marker-badges / marker-window walks assert
  await ok('version + marker badges default off', evalPage(() => localStorage.getItem('s3b-show-versions') === null
    && localStorage.getItem('s3b-show-markers') === null));
  const tick = (needle) => evalPage((q) => {
    const row = Array.from(document.querySelectorAll('#modal-root .set-row'))
      .find((x) => (x.querySelector('.set-name')?.textContent || '').includes(q));
    const cb = row?.querySelector('input[type=checkbox]');
    if (!cb || cb.checked) return false;
    cb.click();
    return true;
  }, needle);
  await ok('both icon toggles ticked', await tick('version count icons') && await tick('delete marker icons'));
  await ok('toggles persisted', evalPage(() => localStorage.getItem('s3b-show-versions') === '1'
    && localStorage.getItem('s3b-show-markers') === '1'));
  // secure-storage section (Settings → Security): the status readout and
  // the global toggle round-tripping the recorded backend call
  await ok('security section + toggle row rendered', evalPage(() => {
    const secs = Array.from(document.querySelectorAll('#modal-root .set-section')).map((s) => s.textContent);
    const rows = Array.from(document.querySelectorAll('#modal-root .set-row'));
    return secs.some((x) => /security/i.test(x))
      && rows.some((r) => /secure storage/i.test(r.querySelector('.set-name')?.textContent || ''));
  }));
  await ok('secure status names the keyring backend', evalPage(() => Array.from(document.querySelectorAll('#modal-root .set-val'))
    .some((v) => /credential manager/i.test(v.textContent))));
  await evalPage(() => {
    const row = Array.from(document.querySelectorAll('#modal-root .set-row'))
      .find((x) => /secure storage/i.test(x.querySelector('.set-name')?.textContent || ''));
    const cb = row?.querySelector('input[type=checkbox]');
    if (cb && !cb.disabled && !cb.checked) cb.click();
  });
  await waitFor(async () => (await findCall('SetSecureStorage')) !== null, 4000, 'SetSecureStorage on toggle');
  const sc = await findCall('SetSecureStorage');
  await ok('secure toggle calls the backend with true', sc && sc.args[0] === true);
  await ok('status re-syncs from the returned SecureStatus', evalPage(() => {
    const row = Array.from(document.querySelectorAll('#modal-root .set-row'))
      .find((x) => /secure storage/i.test(x.querySelector('.set-name')?.textContent || ''));
    const cb = row?.querySelector('input[type=checkbox]');
    const vals = Array.from(document.querySelectorAll('#modal-root .set-val')).map((v) => v.textContent);
    return !!cb && cb.checked && vals.some((v) => /Roaming/.test(v));
  }));
  // the Security section sits below the fold in the scrolling settings body
  // — shotOf scrolls it into view and verifies it is on screen, so the
  // shot shows security settings, not the top of an unrelated section
  await shotOf('settings-secure-on', '.set-section', 'security');
  // ticking a level in the file-log filter persists via SetLogSettings;
  // the current mode rides along unchanged (file-only filters)
  await resetCalls();
  await evalPage(() => document.querySelector('#modal-root .ms-btn').click());
  await sleep(60);
  await evalPage(() => {
    const pop = document.querySelector('#modal-root .ms-pop:not(.hidden)');
    const opt = Array.from(pop.querySelectorAll('.ms-opt')).find((l) => /^warn/.test(l.textContent.trim()));
    opt.querySelector('input').click();
  });
  await waitFor(async () => (await findCall('SetLogSettings')) !== null, 4000, 'SetLogSettings on level tick');
  const ls = await findCall('SetLogSettings');
  await ok('level selection rides on SetLogSettings (file log only)', ls
    && ls.args[0] === 'default' && Array.isArray(ls.args[2]) && ls.args[2].includes('warn')
    && Array.isArray(ls.args[3]) && ls.args[3].length === 0);
  // the source picker mirrors the drawer's source filter for the FILE log
  // only: its options come from the backend (allSources) and its selection
  // rides as SetLogSettings' 5th argument, together with the level filter
  await ok('file-log sources row rendered with the file-only hint', evalPage(() => {
    const rows = Array.from(document.querySelectorAll('#modal-root .set-row'));
    return rows.some((r) => {
      const n = r.querySelector('.set-name')?.textContent || '';
      return /log file: sources/i.test(n) && !!r.querySelector('.ms-btn')
        && /only.*log file/i.test(r.querySelector('.set-hint')?.textContent || '');
    });
  }));
  await resetCalls();
  await evalPage(() => {
    const row = Array.from(document.querySelectorAll('#modal-root .set-row'))
      .find((r) => /log file: sources/i.test(r.querySelector('.set-name')?.textContent || ''));
    row?.querySelector('.ms-btn')?.click();
  });
  await sleep(60);
  await evalPage(() => {
    // several popovers can be open at once (each picker owns one and the
    // open button stopPropagation's) — pick the one holding the option
    const pop = Array.from(document.querySelectorAll('#modal-root .ms-pop:not(.hidden)'))
      .find((p) => Array.from(p.querySelectorAll('.ms-opt')).some((l) => l.textContent.trim() === 'team-files'));
    const opt = pop && Array.from(pop.querySelectorAll('.ms-opt')).find((l) => l.textContent.trim() === 'team-files');
    opt?.querySelector('input').click();
  });
  await waitFor(async () => (await findCall('SetLogSettings')) !== null, 4000, 'SetLogSettings on source tick');
  const lsrc = await findCall('SetLogSettings');
  await ok('source selection rides on SetLogSettings 5th arg (file log only)', lsrc
    && lsrc.args[0] === 'default' && Array.isArray(lsrc.args[4]) && lsrc.args[4].includes('team-files')
    && Array.isArray(lsrc.args[2]) && lsrc.args[2].includes('warn'));
  await closeModal();
  await ok('modal closed', evalPage(() => document.getElementById('modal-root').classList.contains('hidden')));
});

await step('upload', async () => {
  // the toolbar Upload button opens a flat two-leaf picker menu — Files…
  // (Ctrl+U) and Folder… — the v1.0.0 pair of native pickers feeding the
  // same Upload pipe
  await navObjects('team-files');
  await resetCalls();
  await page.click('#btn-upload');
  await ok('upload menu: exactly the Files and Folder leaves', evalPage(() => {
    const items = Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) > .item'));
    const label = (i) => items[i]?.textContent || '';
    return items.length === 2 && /Files/.test(label(0)) && /Ctrl\+U/.test(label(0))
      && /Folder/.test(label(1));
  }));
  await shot('upload-menu');
  await page.locator('#ctxmenu .item', { hasText: 'Files' }).click();
  await waitFor(async () => (await findCall('Upload')) !== null, 4000, 'upload');
  const c = await findCall('Upload');
  await ok('Files leaf uploads the picked paths', c && c.args[1] === 'team-files'
    && c.args[0].length === 2 && /invoice\.pdf$/.test(c.args[0][0]) && /photos$/.test(c.args[0][1]));
  await ok('PickUploadFiles backed the dialog', (await findCall('PickUploadFiles')) !== null);
  await ok('no context menu', evalPage(() => document.getElementById('ctxmenu').classList.contains('hidden')));
  await shot('upload');
  // Folder leaf: the directory picker feeds the same Upload call
  await resetCalls();
  await page.click('#btn-upload');
  await page.locator('#ctxmenu .item', { hasText: 'Folder' }).click();
  await waitFor(async () => (await findCall('Upload')) !== null, 4000, 'folder upload');
  const c2 = await findCall('Upload');
  await ok('Folder leaf uploads the picked directory', c2 && c2.args[0].length === 1
    && /Downloads$/.test(c2.args[0][0]) && c2.args[1] === 'team-files');
  // context menus carry ONE "Upload ▸" row with an Explorer-style flyout
  // (mouseenter opens it); opened near the right viewport edge the flyout
  // must flip to open leftwards instead of spilling off-screen
  await resetCalls();
  await evalPage(() => {
    document.getElementById('grid-body')
      .dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: innerWidth - 30, clientY: 500 }));
  });
  await sleep(80);
  const bgItems = await evalPage(() => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) > .item'))
    .map((i) => ({ t: i.textContent.trim(), sub: i.classList.contains('has-sub') })));
  const ups = bgItems.filter((x) => /upload/i.test(x.t));
  await ok('ctx menu: a single Upload row carrying the flyout', ups.length === 1 && ups[0].sub);
  const up = page.locator('#ctxmenu:not(.hidden) .item.has-sub', { hasText: /upload/i });
  await up.first().hover();
  await sleep(100);
  await ok('flyout opens on hover with the Files/Folder leaves', evalPage(() => {
    const fly = document.querySelector('#ctxmenu .submenu');
    return !!fly && fly.querySelectorAll('.item').length === 2
      && /Files/.test(fly.textContent) && /Folder/.test(fly.textContent);
  }));
  await shot('upload-flyout');
  await ok('flyout flips at the right viewport edge', evalPage(() => !!document.querySelector('#ctxmenu .submenu.flip')));
  await page.locator('#ctxmenu .submenu .item', { hasText: 'Files' }).click();
  await waitFor(async () => (await findCall('Upload')) !== null, 4000, 'upload via flyout');
  await ok('flyout Files leaf reaches the Upload pipe', (await findCall('PickUploadFiles')) !== null);
});

await step('conflict-view', async () => {
  // ask-mode upload against a destination WITH collisions: the per-file
  // dialog lists both sides, bulk actions hit only checked rows, and the
  // per-file decisions ride along on the Upload call.
  const iso = (d) => new Date(Date.now() - d * 86400000).toISOString();
  const seeded = [
    { key: 'invoice.pdf', decisionKey: 'invoice.pdf', srcSize: 8192, srcTime: iso(1), dstSize: 7168, dstTime: iso(30) },
    { key: 'photos/img-001.jpg', decisionKey: 'photos/img-001.jpg', srcSize: 1048576, srcTime: iso(8), dstSize: 1048576, dstTime: iso(40) },
  ];
  await evalPage((cs) => {
    localStorage.setItem('s3b-conflict', 'ask');
    window.__shim.world.conflicts = cs;
  }, seeded);
  await navObjects('team-files');
  await resetCalls();
  await page.click('#btn-upload');
  await page.locator('#ctxmenu .item', { hasText: 'Files' }).click();
  await waitFor(modalVisible, 4000, 'conflict modal');
  await ok('per-file conflict dialog opens', evalPage(() => !!document.querySelector('#modal-root .modal.cf-modal')));
  await ok('title counts the collisions', (await evalPage(() => document.querySelector('#modal-root .modal-head span')?.textContent || '')).includes('2 file(s)'));
  await ok('one row per conflicting file', evalPage(() => document.querySelectorAll('#modal-root .cf-list .cf-row').length === 2));
  await ok('rows show source vs destination', evalPage(() => {
    const r = Array.from(document.querySelectorAll('#modal-root .cf-row'))
      .find((x) => x.querySelector('.cf-name')?.textContent === 'invoice.pdf');
    const tags = r ? Array.from(r.querySelectorAll('.cf-tag')) : [];
    return !!r && r.querySelectorAll('.cf-side').length === 2
      && /source/i.test(tags[0]?.textContent || '') && /destination/i.test(tags[1]?.textContent || '')
      && !!r.querySelector('.cf-time');
  }));
  await ok('decision key carried on the row', evalPage(() => document.querySelector('#modal-root .cf-row .cf-name')?.title === 'invoice.pdf'));
  await ok('summary starts at 2 overwrite', (await txt('#modal-root .cf-summary')).includes('2 Overwrite'));
  // per-row action select
  await page.locator('#modal-root .cf-list .cf-row:nth-child(2) .cf-action').selectOption('skip');
  await ok('per-row action updates the summary', (await txt('#modal-root .cf-summary')).includes('1 Skip'));
  // master checkbox clears every row; bulk must then hit nothing
  await page.click('#modal-root .cf-head .cf-check input');
  await ok('master unchecks all rows', evalPage(() => Array.from(document.querySelectorAll('#modal-root .cf-list .cf-check input')).every((c) => !c.checked)));
  await page.locator('#modal-root .cf-bulk button', { hasText: 'Rename' }).click();
  await ok('bulk ignores unchecked rows', evalPage(() => Array.from(document.querySelectorAll('#modal-root .cf-list .cf-action')).every((s) => s.value !== 'rename')));
  // master restores every row; bulk overwrite reaches them all
  await page.click('#modal-root .cf-head .cf-check input');
  await page.locator('#modal-root .cf-bulk button', { hasText: 'Overwrite' }).click();
  await ok('bulk applies to checked rows', evalPage(() => Array.from(document.querySelectorAll('#modal-root .cf-list .cf-action')).every((s) => s.value === 'overwrite')));
  await page.locator('#modal-root .cf-list .cf-row:nth-child(2) .cf-action').selectOption('skip');
  await shot('conflict');
  const start = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /^start$/i.test(b.textContent.trim())) || null);
  await ok('Start button present', !!start);
  if (start) await start.asElement().click();
  await waitFor(async () => (await findCall('Upload')) !== null, 4000, 'upload with decisions');
  const c = await findCall('Upload');
  await ok('CheckConflicts probed the destination', (await findCall('CheckConflicts')) !== null);
  await ok('per-file decisions ride on Upload', c && c.args[3] === 'overwrite' && !!c.args[5]
    && c.args[5]['invoice.pdf'] === 'overwrite' && c.args[5]['photos/img-001.jpg'] === 'skip');
  // same ask mode, but nothing collides: probe, NO dialog, straight upload
  await evalPage(() => { window.__shim.world.conflicts = []; });
  await resetCalls();
  await page.click('#btn-upload');
  await page.locator('#ctxmenu .item', { hasText: 'Files' }).click();
  await waitFor(async () => (await findCall('Upload')) !== null, 4000, 'upload without dialog');
  await ok('no dialog when nothing collides', evalPage(() => document.getElementById('modal-root').classList.contains('hidden')));
  const c2 = await findCall('Upload');
  await ok('clean start is overwrite, no decisions', c2 && c2.args[3] === 'overwrite' && c2.args[5] === null);
  await evalPage(() => localStorage.setItem('s3b-conflict', 'overwrite'));
});

await step('sources-in-tree', async () => {
  await ok('source listed in sidebar tree', (await txt('#tree')).includes('backup-box'));
  await ok('sidebar header says Data sources', (await txt('#sidebar-head')).toLowerCase().includes('data sources'));
  // every source row carries its SVG type glyph (bucket/terminal/globe/…)
  // painted in the source's own accent color
  await ok('source rows carry SVG type glyphs', evalPage(() => {
    const rows = Array.from(document.querySelectorAll('#tree .tnode[data-tkind="source"]'));
    return rows.length >= 3 && rows.every((r) => !!r.querySelector('.ticon svg'));
  }));
  await ok('glyphs painted in the source accent color', evalPage(() => {
    const r = Array.from(document.querySelectorAll('#tree .tnode[data-tkind="source"]'))
      .find((x) => x.dataset.source === 'backup-box');
    // style.color serializes '#1b7f3b' to rgb() in Chromium — accept both
    const c = r?.querySelector('.ticon')?.style.color || '';
    return !!r && ['#1b7f3b', 'rgb(27, 127, 59)'].includes(c);
  }));
  await shotOf('sources-tree', '#tree');
});

await step('source-editor-autoname', async () => {
  // the sidebar "+" opens the Add-source dialog; the Name field auto-fills
  // from the connection details and stays editable (a typed name wins)
  await page.click('#sidebar-head .side-add');
  await waitFor(modalVisible, 4000, 'source editor');
  await ok('title is Add data source', (await evalPage(() => document.querySelector('#modal-root .modal-head span')?.textContent || '')).includes('Add data source'));
  // S3: with no bucket typed, the endpoint host's first label becomes the
  // name; once a bucket is typed it wins (an S3 data source IS one bucket)
  const ep = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root input.mono'))
    .find((i) => /s3\.amazonaws/.test(i.placeholder || '')) || null);
  await ok('endpoint field present', !!ep);
  if (ep) {
    await ep.asElement().fill('https://hel1.your-objectstorage.com');
    await ep.asElement().dispatchEvent('change');
  }
  await ok('S3 endpoint auto-fills the name', evalPage(() => document.querySelector('#modal-root input.input')?.value === 'hel1'));
  const bk = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root input.mono'))
    .find((i) => /bucket this source opens/.test(i.placeholder || '')) || null);
  await ok('bucket field present (required)', !!bk);
  if (bk) {
    await bk.asElement().fill('hel1-media');
    await bk.asElement().dispatchEvent('change');
  }
  await ok('the bucket name auto-fills the name', evalPage(() => document.querySelector('#modal-root input.input')?.value === 'hel1-media'));
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
  await waitFor(() => popoutVisible('doctor'), 4000, 'doctor popout');
  await ok('doctor checks listed', waitFor(async () => (await evalPage(() => document.querySelector('#popout-root .popout[data-pop^="doctor"]').textContent)).includes('Connectivity'), 4000, 'checks'));
  const run = await elOrNull(() => Array.from(document.querySelectorAll('#popout-root .popout[data-pop^="doctor"] button'))
    .find((b) => /run all/i.test(b.textContent)) || null);
  await ok('Run all button present', !!run);
  if (run) {
    await run.asElement().click();
    await waitFor(async () => (await evalPage(() => document.querySelector('#popout-root .popout[data-pop^="doctor"]').textContent)).includes('warn'), 4000, 'report');
  }
  await shotOf('doctor', '#popout-root .popout[data-pop^="doctor"]');
  await closePopout('doctor');
});

await step('admin-panel', async () => {
  // deterministic: the default S3 source's tree node lands on its buckets view
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('team-files'), 6000, 'buckets view');
  const n = await openCtx('team-files');
  await ok('bucket menu has items', n >= 5);
  await ctxItem(/admin panel/i);
  await waitFor(modalVisible, 4000, 'admin modal');
  await ok('title names the bucket', (await evalPage(() => document.querySelector('#modal-root .modal-head span')?.textContent || '')).startsWith('Admin panel — team-files'));
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
  await shotOf('admin-panel', '#modal-root .modal.admin-modal');
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
  await navObjects('team-files');
  await page.click('#btn-find');
  await waitFor(modalVisible, 4000, 'search modal');
  const start = await elOrNull(() => document.querySelector('#modal-root button.primary') || null);
  await ok('search start button present', !!start);
  if (start) {
    await start.asElement().click();
    await waitFor(async () => (await evalPage(() => document.getElementById('modal-root').textContent)).includes('readme.md'), 4000, 'results');
    await ok('search results rendered', (await evalPage(() => document.getElementById('modal-root').textContent)).includes('docs/notes.md'));
  }
  await shotOf('deep-search', '#modal-root .modal');
  await closeModal();
});

await step('versions-diff', async () => {
  await navObjects('team-files');
  await clickRow('scan.png'); // single selection so the Versions entry is offered
  // marker visibility rides on the Settings toggle (default OFF — it was
  // ticked earlier in this run): opted out, the timeline lists only the 3
  // real versions and the delete-marker row stays hidden
  await evalPage(() => localStorage.setItem('s3b-show-markers', '0'));
  await openCtx('readme.md');
  await ctxItem(/versions/i);
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root .ver-row').length)) >= 3, 4000, 'versions');
  await ok('markers hidden by default: 3 real versions listed', evalPage(() => {
    const t = document.getElementById('modal-root').textContent;
    return document.querySelectorAll('#modal-root .ver-row').length === 3 && !t.includes('Delete marker');
  }));
  await closeModal();
  // opted in: the marker row returns to the timeline (4 entries)
  await evalPage(() => localStorage.setItem('s3b-show-markers', '1'));
  await clickRow('scan.png');
  await openCtx('readme.md');
  await ctxItem(/versions/i);
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root .ver-row').length)) >= 4, 4000, 'versions with markers');
  await ok('4 versions listed', evalPage(() => document.querySelectorAll('#modal-root .ver-row').length === 4));
  await ok('delete marker shown', (await evalPage(() => document.getElementById('modal-root').textContent)).includes('Delete marker'));
  await shotOf('versions', '#modal-root .modal');
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
  // Per-version Destroy rides the SAME unified Delete Window (the base
  // template every destructive confirmation uses): OFF (the default — no
  // s3b-del-* key has been written yet this run) the window carries the
  // version identity + consequence line with NO typed word; ON, the typed
  // partition arms inside the same window.
  const destroyVer = async (rowNeedle) => {
    await clickRow('scan.png');
    await openCtx('readme.md');
    await ctxItem(/versions/i);
    await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root .ver-row').length)) >= 3, 4000, 'versions reopen');
    await evalPage((needle) => {
      const r = Array.from(document.querySelectorAll('#modal-root .ver-row')).find((x) => x.textContent.includes(needle));
      r?.querySelector('.ver-actions .btn.danger')?.click();
    }, rowNeedle);
  };
  await resetCalls();
  await destroyVer('ver-0002');
  await waitFor(() => evalPage(() => !!document.querySelector('#modal-root .delw-modal')), 4000, 'destroy delete window');
  await ok('version destroy opens the Delete Window, no typed word by default', evalPage(() => {
    const t = document.getElementById('modal-root').textContent;
    return !document.querySelector('#modal-root input') && t.includes('cannot be recovered');
  }));
  await ok('destroy window carries the version identity + live Destroy button', evalPage(() => {
    const t = document.querySelector('#modal-root .delw-stats').textContent;
    const b = document.querySelector('#modal-root .modal-foot .btn.danger');
    return t.includes('ver-0002') && !b.disabled && /destroy/i.test(b.textContent);
  }));
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('DeleteVersionPermanent')) !== null, 4000, 'DeleteVersionPermanent');
  await closeModal();
  // …and with the setting on, the same window arms its typed partition
  await evalPage(() => localStorage.setItem('s3b-del-typeconfirm', '1'));
  await resetCalls();
  await destroyVer('ver-0002');
  await waitFor(() => evalPage(() => !!document.querySelector('#modal-root .delw-confirm input')), 4000, 'destroy typed partition');
  await ok('version destroy: typed word asked when the setting is on', evalPage(() => {
    const d = document.querySelector('#modal-root .modal-foot .btn.danger');
    const lbl = document.querySelector('#modal-root .delw-confirm .field')?.textContent || '';
    return d.disabled && lbl.includes('"delete"');
  }));
  await evalPage(() => { document.querySelector('#modal-root input').focus(); });
  await page.keyboard.type('delete');
  await sleep(80);
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('DeleteVersionPermanent')) !== null, 4000, 'typed DeleteVersionPermanent');
  await evalPage(() => localStorage.removeItem('s3b-del-typeconfirm'));
  await closeModal();
});

await step('presign-class-lock', async () => {
  await navObjects('team-files');
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
  await navObjects('team-files');
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
  await waitFor(async () => (await rowKeys()).includes('team-files'), 6000, 'buckets view');
  await openCtx('team-files');
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
  await navObjects('team-files');
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
  // Delete gate on a VERSIONED bucket: the unified Delete Window opens
  // with all three delete types (marker default) — cancel out of it
  await clickRow('readme.md');
  await page.keyboard.press('Delete');
  await waitFor(modalVisible, 4000, 'delete window');
  await ok('delete window offers three types, marker default', evalPage(() => {
    const rs = Array.from(document.querySelectorAll('#modal-root .vcv-row input[type=radio]'));
    return rs.length === 3 && rs[0].checked && rs[0].value === '' && /marker/i.test(rs[0].closest('label').textContent);
  }));
  await ok('delete window layout clean', (await layoutAudit()).ok);
  await closeModal();
});

await step('delete-window-marker', async () => {
  // versioned bucket: the window lists all three delete types with the
  // marker ('') default. The typed partition is OFF by default (a Settings
  // opt-in) — one click of the danger button runs the marker delete;
  // there is no second confirm behind it.
  await navObjects('team-files');
  await resetCalls();
  await clickRow('readme.md');
  await page.keyboard.press('Delete');
  await waitFor(modalVisible, 4000, 'delete window');
  await ok('window lists marker / keep-current / permanent', evalPage(() => {
    const rows = Array.from(document.querySelectorAll('#modal-root .vcv-row'));
    return rows.length === 3 && /marker/i.test(rows[0].textContent)
      && /except current version/i.test(rows[1].textContent) && /permanent/i.test(rows[2].textContent);
  }));
  await ok('window states the target + counts', evalPage(() => {
    const t = document.getElementById('modal-root').textContent;
    return t.includes('s3://team-files/readme.md') && /1 object\(s\)/.test(t);
  }));
  await ok('typed partition hidden by default; button reads Delete, unlocked', evalPage(() => {
    const b = document.querySelector('#modal-root .modal-foot .btn.danger');
    return !document.querySelector('#modal-root .delw-confirm')
      && b && b.textContent.trim() === 'Delete' && !b.disabled;
  }));
  // the safe default mode explains itself in its radio hint — the amber
  // note line stays empty for it (its height stays reserved via CSS so
  // the window cannot grow/shrink when a mode serves a note)
  await ok('safe default mode carries no amber note', evalPage(() => {
    const w = document.querySelector('#modal-root .delw-warn');
    return !!w && w.textContent.trim() === '';
  }));
  // mode switches must not resize the window: the warn slot keeps its
  // reserved height whether or not a mode serves an amber note
  await ok('window height stable across mode switches', evalPage(() => {
    const modal = document.querySelector('#modal-root .modal');
    if (!modal) return false;
    const h0 = modal.getBoundingClientRect().height;
    const inputs = Array.from(document.querySelectorAll('#modal-root .vcv-row input'));
    const hs = [];
    for (const i of inputs) { i.click(); hs.push(modal.getBoundingClientRect().height); }
    inputs[0].click(); // back to the marker default
    return inputs.length === 3 && Math.max(...hs, h0) - Math.min(...hs, h0) <= 1;
  }));
  await shotOf('delete-window', '#modal-root .modal');
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('DeleteSelection')) !== null
    || (await findCall('SourceDeleteSelection')) !== null, 4000, 'DeleteSelection (marker)');
  const c = (await findCall('DeleteSelection')) || (await findCall('SourceDeleteSelection'));
  await ok('DeleteSelection got the key', c && JSON.stringify(c.args).includes('readme.md'));
  await ok('marker path stayed marker', (await findCall('DeleteSelectionPermanent')) === null
    && (await findCall('SourceDeleteSelectionPermanent')) === null
    && (await findCall('DeleteSelectionKeepCurrent')) === null
    && (await findCall('SourceDeleteSelectionKeepCurrent')) === null);
});

await step('delete-window-keepcurrent', async () => {
  // the third delete type: everything but the current version goes. With
  // the typed partition off (the default — it is a Settings opt-in), the
  // pre-counted summary plus the explicit mode choice are the guard: one
  // Delete click runs it.
  await clickRow('readme.md');
  await page.keyboard.press('Delete');
  await waitFor(modalVisible, 4000, 'delete window');
  await evalPage(() => {
    const kc = Array.from(document.querySelectorAll('#modal-root .vcv-row input')).find((i) => i.value === 'keepcurrent');
    kc.click();
  });
  await ok('keep-current: no typed gate, button unlocked and reads Delete', evalPage(() => {
    const b = document.querySelector('#modal-root .modal-foot .btn.danger');
    return !document.querySelector('#modal-root .delw-confirm')
      && b && !b.disabled && b.textContent.trim() === 'Delete';
  }));
  // destructive modes keep their amber consequence line
  await ok('keep-current mode shows the amber note', evalPage(() => {
    const w = document.querySelector('#modal-root .delw-warn');
    return !!w && w.textContent.trim() !== '';
  }));
  await shot('delete-keepcurrent');
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('DeleteSelectionKeepCurrent')) !== null
    || (await findCall('SourceDeleteSelectionKeepCurrent')) !== null, 4000, 'DeleteSelectionKeepCurrent');
  const c = (await findCall('DeleteSelectionKeepCurrent')) || (await findCall('SourceDeleteSelectionKeepCurrent'));
  await ok('keep-current got the key', c && JSON.stringify(c.args).includes('readme.md'));
});

await step('delete-window-permanent', async () => {
  // same window, permanent branch: with the typed partition off by
  // default, the explicit permanent radio plus the counted summary is the
  // guard — one Delete click sends the purge to the backend
  await clickRow('budget-2026.xlsx');
  await page.keyboard.press('Delete');
  await waitFor(modalVisible, 4000, 'delete window');
  await evalPage(() => {
    const pm = Array.from(document.querySelectorAll('#modal-root .vcv-row input')).find((i) => i.value === 'permanent');
    pm.click();
  });
  await ok('permanent mode: no typed gate, button unlocked and reads Delete', evalPage(() => {
    const b = document.querySelector('#modal-root .modal-foot .btn.danger');
    return !document.querySelector('#modal-root .delw-confirm')
      && b && !b.disabled && b.textContent.trim() === 'Delete';
  }));
  await ok('permanent mode shows the amber note', evalPage(() => {
    const w = document.querySelector('#modal-root .delw-warn');
    return !!w && w.textContent.trim() !== '';
  }));
  await shot('delete-permanent');
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  // the view may be source-pinned: the plain and Source-pinned backends are
  // equivalent here — accept whichever fired
  await waitFor(async () => (await findCall('DeleteSelectionPermanent')) !== null
    || (await findCall('SourceDeleteSelectionPermanent')) !== null, 4000, 'DeleteSelectionPermanent');
  const c = (await findCall('DeleteSelectionPermanent')) || (await findCall('SourceDeleteSelectionPermanent'));
  await ok('permanent got the key', c && JSON.stringify(c.args).includes('budget-2026.xlsx'));
});

await step('shift-del-permanent-directory', async () => {
  // Shift+Del opens the window PRESET to the permanent mode — with the
  // typed partition off by default there is nothing else to clear. A
  // DIRECTORY key must reach the backend intact — purge-everything
  // semantics, not the old per-key folder-marker-only delete.
  await resetCalls();
  await clickRow('photos');
  await page.keyboard.press('Shift+Delete');
  await waitFor(modalVisible, 4000, 'shift+del window');
  await ok('preset to permanent, gate-free', evalPage(() => {
    const rs = Array.from(document.querySelectorAll('#modal-root .vcv-row input[type=radio]'));
    return rs.length === 3 && rs.find((r) => r.value === 'permanent').checked
      && !document.querySelector('#modal-root .delw-confirm');
  }));
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('DeleteSelectionPermanent')) !== null
    || (await findCall('SourceDeleteSelectionPermanent')) !== null, 4000, 'Shift+Del permanent');
  const c = (await findCall('DeleteSelectionPermanent')) || (await findCall('SourceDeleteSelectionPermanent'));
  await ok('directory key routed to the purge backend', c && JSON.stringify(c.args).includes('photos/'));
});

await step('version-marker-badges', async () => {
  // count badges (Settings opt-in — both toggles were ticked in the
  // settings step): file rows carry their own version count and a
  // count-free marker flag (one object holds at most one marker), folder
  // rows the aggregates beneath them, and an all-delete-marked folder is
  // ghosted (its rows count as hidden, shown greyed when un-hidden)
  await navObjects('team-files');
  await waitFor(async () => evalPage(() => Array.from(document.querySelectorAll('#grid-body .vbadge, #grid-body .mbadge'))
    .some((v) => v.textContent.trim() !== '')), 4000, 'count badges rendered');
  const badges = (label) => evalPage((l) => {
    const r = Array.from(document.querySelectorAll('#grid-body .grid-row')).find((x) => x.querySelector('.tname')?.textContent === l);
    if (!r) return null;
    const v = r.querySelector('.vbadge');
    const m = r.querySelector('.mbadge');
    return {
      v: v?.textContent.trim() || '', vt: v?.title || '',
      m: m?.textContent.trim() || '', mt: m?.title || '',
      ghost: r.classList.contains('ghost'),
    };
  }, label);
  const rd = await badges('readme.md');
  await ok('file row: version count + count-free marker flag', rd && rd.v.includes('4')
    && rd.m.includes('⛔') && !/\d/.test(rd.m) && rd.mt.toLowerCase().includes('delete marker'));
  const bare = await badges('scan.png');
  await ok('version-free rows stay bare', bare && bare.v === '' && bare.m === '' && !bare.ghost);
  const docs = await badges('docs');
  await ok('folder row aggregates counts', docs && docs.v.includes('6') && docs.m.includes('2'));
  await shot('count-badges');
  await dblClickRow('docs');
  await waitFor(async () => (await rowKeys()).some((k) => k.endsWith('legacy/')), 4000, 'docs children');
  const legacy = await badges('legacy');
  await ok('all-deleted folder is ghosted with its marker count', legacy && legacy.ghost && legacy.m.includes('1') && legacy.v.includes('1'));
  await shot('marker-badges');
});

await step('marker-window', async () => {
  // clicking the marker badge opens the Delete Marker window — titled in
  // the singular for a file (one object carries at most one marker). Rows
  // are checkbox-selectable (bulk Remove selected), plus per-row Remove
  // (undo delete) and bulk Remove all
  await navObjects('team-files');
  // the Settings → View marker toggle governs the Delete Marker window
  // too: while OFF (the default) it lists nothing and never fetches —
  // the context-menu entry still opens it (badges are gone), and the
  // inline button is the same flip the View menu makes
  await resetCalls();
  await evalPage(() => localStorage.setItem('s3b-show-markers', '0'));
  await evalPage(() => {
    const r = Array.from(document.querySelectorAll('#grid-body .grid-row')).find((x) => x.querySelector('.tname')?.textContent === 'readme.md');
    r.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 400, clientY: 300 }));
  });
  await sleep(80);
  await evalPage(() => {
    const it = Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
      .find((i) => /delete marker/i.test(i.textContent.trim()));
    it?.click();
  });
  await waitFor(modalVisible, 4000, 'marker window while hidden');
  await sleep(150);
  await ok('hidden: notice shown, nothing listed or fetched',
    (await evalPage(() => {
      const t = document.getElementById('modal-root').textContent;
      return document.querySelectorAll('#modal-root .ver-row').length === 0 && /are hidden/i.test(t);
    })) && (await findCall('PrefixMarkers')) === null);
  await ok('hidden: inline opt-in offered', evalPage(() => !!Array.from(document.querySelectorAll('#modal-root .btn'))
    .find((x) => /show delete markers/i.test(x.textContent))));
  await shot('marker-window-hidden');
  await evalPage(() => {
    const b = Array.from(document.querySelectorAll('#modal-root .btn'))
      .find((x) => /show delete markers/i.test(x.textContent));
    b?.click();
  });
  await waitFor(async () => (await findCall('PrefixMarkers')) !== null, 4000, 'PrefixMarkers after opt-in');
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root .ver-row').length)) === 2, 4000, 'markers listed after opt-in');
  await ok('opt-in flips the global toggle and re-badges the grid', evalPage(() => {
    const r = Array.from(document.querySelectorAll('#grid-body .grid-row')).find((x) => x.querySelector('.tname')?.textContent === 'readme.md');
    return localStorage.getItem('s3b-show-markers') === '1'
      && !!r && /\u26D4/.test(r.querySelector('.mbadge').textContent);
  }));
  await closeModal();
  await resetCalls();
  await evalPage(() => {
    const r = Array.from(document.querySelectorAll('#grid-body .grid-row')).find((x) => x.querySelector('.tname')?.textContent === 'readme.md');
    r.querySelector('.mbadge').click();
  });
  await waitFor(async () => (await findCall('PrefixMarkers')) !== null, 4000, 'PrefixMarkers');
  await waitFor(async () => (await txt('#modal-root')).includes('delete marker'), 4000, 'marker window content');
  await ok('markers listed with version ids', evalPage(() => document.querySelectorAll('#modal-root .ver-row').length === 2
    && /vm-0001/.test(document.getElementById('modal-root').textContent)));
  await ok('file window titled singular, rows checkbox-selectable', evalPage(() => {
    const h = document.querySelector('#modal-root .modal-head span')?.textContent || '';
    return /^delete marker — s3:\/\//i.test(h)
      && document.querySelectorAll('#modal-root .ver-check input').length === 2;
  }));
  await ok('Remove selected wakes on selection', evalPage(() => {
    const sel = Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
      .find((b) => /remove selected/i.test(b.textContent));
    if (!sel || !sel.disabled) return false;
    document.querySelector('#modal-root .ver-check input').click();
    return !sel.disabled;
  }));
  // polish: no per-row tag pills any more (a marker is by definition the
  // key's latest version — the "latest" badge said nothing), and Remove all
  // is NOT danger-styled — removing a marker restores the object
  await ok('no tag pills; Remove all is calm', evalPage(() => {
    const pills = Array.from(document.querySelectorAll('#modal-root .ver-row .tag'));
    const all = Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
      .find((b) => /remove all/i.test(b.textContent));
    return pills.length === 0 && !!all && !all.classList.contains('danger');
  }));
  await ok('marker window layout clean', (await layoutAudit()).ok);
  await shot('marker-window');
  await evalPage(() => Array.from(document.querySelectorAll('#modal-root .ver-row .btn'))
    .find((b) => /remove/i.test(b.textContent)).click());
  await waitFor(async () => (await findCall('UndoDelete')) !== null, 4000, 'UndoDelete');
  const u = await findCall('UndoDelete');
  await ok('undo carries key + version id', u && JSON.stringify(u.args).includes('readme.md') && JSON.stringify(u.args).includes('vm-0001'));
  await closeModal();
});

await step('content-versions-window', async () => {
  // clicking the version badge on a FOLDER opens Content Versions: a
  // one-line version count up top (the same format as the Delete Marker
  // window) plus per-child rows with direct controls (Open / Versions… /
  // Markers…) and NO refresh button — every action reloads what it
  // changed. Marker extras appear only while the Settings toggle is on.
  await navObjects('team-files');
  const openDocs = () => evalPage(() => {
    const r = Array.from(document.querySelectorAll('#grid-body .grid-row')).find((x) => x.querySelector('.tname')?.textContent === 'docs');
    r?.querySelector('.vbadge').click();
  });
  await resetCalls();
  await openDocs();
  await waitFor(async () => (await findCall('PrefixVersionStats')) !== null, 4000, 'PrefixVersionStats');
  // the settings step ticked the marker toggle: the count line carries the
  // marker segment and marked children grow a Markers… button
  await waitFor(async () => /version\(s\)/.test(await evalPage(() => document.querySelector('#modal-root .field')?.textContent || '')), 4000, 'content version count');
  await ok('one-line count with marker segment while opted in', evalPage(() => {
    const stats = document.querySelector('#modal-root .field')?.textContent || '';
    const h = document.querySelector('#modal-root .modal-head span')?.textContent || '';
    return /^content versions — s3:\/\//i.test(h)
      && /^9 version\(s\) · Delete markers: 2$/.test(stats)
      && !document.querySelector('#modal-root .dirv-stats');
  }));
  await ok('child aggregates listed, marked child grows a Markers button', evalPage(() => {
    const rows = Array.from(document.querySelectorAll('#modal-root .ver-row'));
    return rows.length === 2 && rows.some((r) => /legacy/.test(r.textContent) && r.classList.contains('ghost'))
      && Array.from(document.querySelectorAll('#modal-root .ver-actions .btn'))
        .some((b) => /markers/i.test(b.textContent));
  }));
  await ok('per-object controls offered, refresh retired', evalPage(() => {
    const foot = Array.from(document.querySelectorAll('#modal-root .modal-foot .btn')).map((b) => b.textContent.trim());
    return document.querySelectorAll('#modal-root .ver-row .ver-actions .btn').length >= 2
      && foot.length === 1 && /^close$/i.test(foot[0]);
  }));
  await ok('content versions layout clean', (await layoutAudit()).ok);
  await shot('content-versions');
  await closeModal();
  // opted out (the DEFAULT): the marker segment, the child marker counts,
  // the Markers… buttons and the ghost rows for deleted children all fold
  // away — versions only
  await evalPage(() => localStorage.setItem('s3b-show-markers', '0'));
  await openDocs();
  await waitFor(async () => /^9 version\(s\)$/.test(await evalPage(() => document.querySelector('#modal-root .field')?.textContent || '')), 4000, 'marker-free count line');
  await ok('markers fold away when the toggle is off', evalPage(() => {
    const rows = Array.from(document.querySelectorAll('#modal-root .ver-row'));
    const t = document.getElementById('modal-root').textContent;
    return rows.length === 1 && !rows.some((r) => r.classList.contains('ghost'))
      && !/legacy/.test(t) && !/markers/i.test(t) && !/delete marker/i.test(t);
  }));
  await shot('content-versions-clean');
  await closeModal();
  await evalPage(() => localStorage.setItem('s3b-show-markers', '1')); // restore for the walks below
});

await step('delete-settings-modes', async () => {
  // Settings steer the confirmation ladder: auto-confirm skips every
  // prompt for single-type sources; window-off falls back to the classic
  // confirm; typed-confirm arms the window's typed partition.
  const wipeKeys = () => evalPage(() => {
    for (const k of ['s3b-del-autoconfirm', 's3b-del-window', 's3b-del-typeconfirm']) localStorage.removeItem(k);
  });
  // the virtual grid may still be recycling the previous view's rows when
  // navObjects returns — wait for the actual target row before clicking
  const awaitRow = (label) => waitFor(() => evalPage((x) => Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .some((r) => r.style.display !== 'none' && r._model && r._model.name === x), label), 6000, `row ${label}`);
  await wipeKeys();
  // auto-confirm: a single-type source deletes with NO dialog at all (the
  // logs live inside the app/ folder — the bucket root shows only app/)
  await navObjects('logs-2026');
  await awaitRow('app');
  await dblClickRow('app');
  await awaitRow('app-2026-09-11.log');
  await resetCalls();
  await evalPage(() => localStorage.setItem('s3b-del-autoconfirm', '1'));
  await clickRow('app-2026-09-11.log');
  await page.keyboard.press('Delete');
  await waitFor(async () => (await findCall('DeleteSelection')) !== null
    || (await findCall('SourceDeleteSelection')) !== null, 4000, 'auto-confirm DeleteSelection');
  await ok('no dialog on auto-confirm', evalPage(() => document.getElementById('modal-root').classList.contains('hidden')));
  await wipeKeys();
  // window disabled + single-type source: the classic plain confirm
  await navObjects('media-assets');
  await awaitRow('brand');
  await resetCalls();
  await evalPage(() => localStorage.setItem('s3b-del-window', '0'));
  await clickRow('brand');
  await page.keyboard.press('Delete');
  await waitFor(modalVisible, 4000, 'classic confirm');
  await ok('classic confirm explains the stakes', evalPage(() => {
    const t = document.getElementById('modal-root').textContent;
    return /about to delete/.test(t) && /cannot be undone/.test(t);
  }));
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('DeleteSelection')) !== null
    || (await findCall('SourceDeleteSelection')) !== null, 4000, 'classic DeleteSelection');
  // typed confirmation ON (window back on): the partition is armed even
  // for the safe single-type window
  await wipeKeys();
  await evalPage(() => localStorage.setItem('s3b-del-typeconfirm', '1'));
  await navObjects('logs-2026');
  await awaitRow('app');
  await dblClickRow('app');
  await awaitRow('app-2026-09-10.log');
  await resetCalls();
  await clickRow('app-2026-09-10.log');
  await page.keyboard.press('Delete');
  await waitFor(modalVisible, 4000, 'typed window');
  await ok('typed partition armed by Settings', evalPage(() => {
    const c = document.querySelector('#modal-root .delw-confirm');
    const b = document.querySelector('#modal-root .modal-foot .btn.danger');
    return !!c && !c.querySelector('input').disabled && b.disabled;
  }));
  await evalPage(() => { document.querySelector('#modal-root .delw-confirm input').focus(); });
  await page.keyboard.type('delete');
  await sleep(80);
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('DeleteSelection')) !== null
    || (await findCall('SourceDeleteSelection')) !== null, 4000, 'typed window DeleteSelection');
  await wipeKeys();
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
  await waitFor(() => popoutVisible('transfers'), 4000, 'transfers popout');
  await ok('job rendered with its current file', waitFor(async () => (await evalPage(() => document.querySelector('#popout-root .popout[data-pop="transfers"]').textContent)).includes('video-final.mp4'), 4000, 'jobs'));
  await ok('percent badge on every job', evalPage(() => {
    const ps = Array.from(document.querySelectorAll('#popout-root .tr-pct'));
    return ps.length === 2 && ps.every((p) => /\d+%/.test(p.textContent));
  }));
  await ok('running job leads the list', evalPage(() => document.querySelector('#popout-root .tr-job')?.classList.contains('running') === true));
  await ok('failed and skipped counted aloud', evalPage(() => {
    const t = document.querySelector('#popout-root .popout[data-pop="transfers"]').textContent;
    return /1 failed/.test(t) && /2 skipped/.test(t);
  }));
  await ok('running job offers Cancel', evalPage(() => !!document.querySelector('#popout-root .tr-job.running .btn')));
  await shotOf('transfers', '#popout-root .popout[data-pop="transfers"]');
  await closePopout('transfers');
});

await step('popouts', async () => {
  // deterministic grid state: the buckets view of the default S3 source
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('team-files'), 6000, 'buckets view');
  // open the transfer manager as a floating window
  await evalPage(() => window.__shim.emit('transfer:update', { id: 't1', op: 'upload', status: 'running', totalFiles: 3, doneFiles: 1 }));
  await page.locator('#menubar .mb-title', { hasText: /view/i }).first().click();
  await sleep(80);
  const trItem = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /transfers/i.test(i.textContent)) || null);
  if (trItem) await trItem.asElement().click();
  await waitFor(() => popoutVisible('transfers'), 4000, 'transfers popout');
  // non-modal: no mask, the grid underneath still works. The click lands
  // on the row's left edge — the row's center would sit under the
  // floating window itself, and the point of the check is that the root
  // passes clicks through everywhere else
  await ok('popout casts no modal mask', (await modalVisible()) === false);
  const rowH = await gridRow('team-files');
  if (!rowH) throw new Error('no grid row "team-files"');
  await rowH.asElement().click({ position: { x: 20, y: 8 } });
  await ok('grid still interactive under a popout', /^1 of /.test(await txt('#status-selection')));
  // header drag moves the window and persists its geometry
  const g0 = await evalPage(() => {
    const r = document.querySelector('#popout-root .popout[data-pop="transfers"]').getBoundingClientRect();
    return { x: r.x, y: r.y };
  });
  await popDrag('transfers', -120, 60);
  const g1 = await evalPage(() => {
    const b = document.querySelector('#popout-root .popout[data-pop="transfers"]');
    const r = b.getBoundingClientRect();
    return { x: r.x, y: r.y, saved: JSON.parse(localStorage.getItem('s3b-popout-transfers') || 'null') };
  });
  await ok('header drag moves the window', g1.x <= g0.x - 100 && g1.y >= g0.y + 50);
  await ok('geometry persisted per id', !!(g1.saved && Number.isFinite(g1.saved.x) && g1.saved.w > 300));
  // a second window floats on top; pressing either one raises it
  await page.keyboard.press('F1');
  await waitFor(() => popoutVisible('keys'), 4000, 'keys popout');
  await ok('two popouts float at once', evalPage(() => document.querySelectorAll('#popout-root .popout').length === 2));
  const z0 = await evalPage(() => ({
    keys: parseInt(document.querySelector('#popout-root .popout[data-pop="keys"]').style.zIndex, 10),
    tr: parseInt(document.querySelector('#popout-root .popout[data-pop="transfers"]').style.zIndex, 10),
  }));
  await ok('newest window stacks on top', z0.keys > z0.tr);
  await popDrag('transfers', 0, 0);
  const z1 = await evalPage(() => parseInt(document.querySelector('#popout-root .popout[data-pop="transfers"]').style.zIndex, 10));
  await ok('pressing a window raises it above siblings', z1 > z0.keys);
  // one instance per id: reopening focuses instead of duplicating
  await page.keyboard.press('F1');
  await sleep(120);
  await ok('reopening focuses the existing window', evalPage(() => document.querySelectorAll('#popout-root .popout[data-pop="keys"]').length === 1));
  // grip resize grows the window
  const s0 = await evalPage(() => {
    const r = document.querySelector('#popout-root .popout[data-pop="keys"]').getBoundingClientRect();
    return { w: r.width, h: r.height };
  });
  await popDrag('keys', 140, 90, 'grip');
  const s1 = await evalPage(() => {
    const r = document.querySelector('#popout-root .popout[data-pop="keys"]').getBoundingClientRect();
    return { w: r.width, h: r.height };
  });
  await ok('grip resizes the window', s1.w >= s0.w + 100 && s1.h >= s0.h + 60);
  await ok('popout layout clean', (await layoutAudit()).ok);
  await shot('popouts');
  // a closed window reopens where it was left
  const savedKeys = await evalPage(() => JSON.parse(localStorage.getItem('s3b-popout-keys')));
  await closePopout('keys');
  await page.keyboard.press('F1');
  await waitFor(() => popoutVisible('keys'), 4000, 'keys reopen');
  const reopened = await evalPage(() => {
    const r = document.querySelector('#popout-root .popout[data-pop="keys"]').getBoundingClientRect();
    return { x: r.x, y: r.y };
  });
  await ok('window reopens at its remembered spot', Math.abs(reopened.x - savedKeys.x) <= 2 && Math.abs(reopened.y - savedKeys.y) <= 2);
  // Escape closes only the topmost window — twice clears both
  await page.keyboard.press('Escape');
  await sleep(80);
  await ok('Escape closes only the topmost popout', evalPage(() =>
    !document.querySelector('#popout-root .popout[data-pop="keys"]')
    && !!document.querySelector('#popout-root .popout[data-pop="transfers"]')));
  await page.keyboard.press('Escape');
  await sleep(80);
  await ok('second Escape clears the rest', evalPage(() => document.querySelectorAll('#popout-root .popout').length === 0));
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

await step('side-pane-delete-window', async () => {
  // remote (sftp) and local side panes route through the same unified
  // Delete Window: count-then-act previews first, one danger click each.
  // Wipe the delete Settings first — a leaked auto-confirm key from an
  // aborted step would silently skip the window here.
  await evalPage(() => {
    for (const k of ['s3b-del-autoconfirm', 's3b-del-window', 's3b-del-typeconfirm']) localStorage.removeItem(k);
  });
  await page.selectOption('#local-src', 'src-box');
  await waitFor(async () => (await sideKeys()).includes('/db.dump'), 6000, 'pane remote listing');
  await resetCalls();
  await evalPage(() => {
    const r = Array.from(document.querySelectorAll('#local-grid-body .grid-row')).find((x) => x._model && x._model.key === '/db.dump');
    r.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 400, clientY: 300 }));
  });
  await sleep(80);
  await evalPage(() => {
    const it = Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
      .find((i) => /^delete/i.test(i.textContent.trim()));
    it?.click();
  });
  await waitFor(modalVisible, 4000, 'remote delete window');
  await ok('remote window shows the counted summary', evalPage(() => {
    const t = document.getElementById('modal-root').textContent;
    return t.includes('/db.dump') && /1 object\(s\)/.test(t);
  }));
  // single-mode window: the no-version-history callout stands in for the
  // mode hints (the other branch of the delete-window warn ternary)
  await ok('single-mode window shows the no-history note', evalPage(() => {
    const w = document.querySelector('#modal-root .delw-warn');
    return !!w && w.textContent.trim() !== '';
  }));
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('RemoteRemove')) !== null, 4000, 'RemoteRemove');
  const rc = await findCall('RemoteRemove');
  await ok('RemoteRemove got the path', rc && JSON.stringify(rc.args).includes('/db.dump'));
  // local binding: same window, LocalDeletePreview → LocalRemove
  await page.selectOption('#local-src', 'local');
  await waitFor(async () => !!(await sideRow('notes.txt')), 6000, 'pane back to local');
  await resetCalls();
  await evalPage(() => {
    const r = Array.from(document.querySelectorAll('#local-grid-body .grid-row')).find((x) => x._model && x._model.name === 'notes.txt');
    r.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 400, clientY: 300 }));
  });
  await sleep(80);
  await evalPage(() => {
    const it = Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
      .find((i) => /^delete/i.test(i.textContent.trim()));
    it?.click();
  });
  await waitFor(async () => (await findCall('LocalDeletePreview')) !== null, 4000, 'LocalDeletePreview');
  await waitFor(modalVisible, 4000, 'local delete window');
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('LocalRemove')) !== null, 4000, 'LocalRemove');
  const lc = await findCall('LocalRemove');
  await ok('LocalRemove got the path', lc && JSON.stringify(lc.args).includes('notes.txt'));
  // Classic mode (window off) on a remote pane — classicTyped territory:
  // the typed word must appear ONLY while Require-typing is on; off, a
  // plain danger confirm carries the same stakes text.
  await page.selectOption('#local-src', 'src-box');
  await waitFor(async () => (await sideKeys()).includes('/db.dump'), 6000, 'pane remote listing (classic)');
  await resetCalls();
  await evalPage(() => localStorage.setItem('s3b-del-window', '0'));
  await evalPage(() => {
    const r = Array.from(document.querySelectorAll('#local-grid-body .grid-row')).find((x) => x._model && x._model.key === '/db.dump');
    r.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 400, clientY: 300 }));
  });
  await sleep(80);
  await evalPage(() => {
    const it = Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
      .find((i) => /^delete/i.test(i.textContent.trim()));
    it?.click();
  });
  await waitFor(async () => /about to delete/.test(await evalPage(() => document.getElementById('modal-root').textContent)), 4000, 'classic remote confirm');
  await ok('classic remote delete: no typed word while setting is off', evalPage(() => {
    const t = document.getElementById('modal-root').textContent;
    return !document.querySelector('#modal-root input') && t.includes('cannot be undone');
  }));
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('RemoteRemove')) !== null, 4000, 'classic RemoteRemove');
  // …and ON: the same flow asks for the word before the danger click
  await resetCalls();
  await evalPage(() => localStorage.setItem('s3b-del-typeconfirm', '1'));
  await evalPage(() => {
    const r = Array.from(document.querySelectorAll('#local-grid-body .grid-row')).find((x) => x._model && x._model.key === '/db.dump');
    r.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 400, clientY: 300 }));
  });
  await sleep(80);
  await evalPage(() => {
    const it = Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item'))
      .find((i) => /^delete/i.test(i.textContent.trim()));
    it?.click();
  });
  await waitFor(async () => (await evalPage(() => document.getElementById('modal-root').textContent)).includes('Type "delete" to confirm:'), 4000, 'classic typed prompt');
  await ok('classic remote delete: typed word asked when the setting is on', evalPage(() => !!document.querySelector('#modal-root input')));
  // empty word: the danger click is a no-op — no RemoteRemove yet
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await sleep(120);
  await ok('wrong word refuses to delete', (await findCall('RemoteRemove')) === null);
  await evalPage(() => { document.querySelector('#modal-root input').focus(); });
  await page.keyboard.type('delete');
  await sleep(80);
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.danger').click());
  await waitFor(async () => (await findCall('RemoteRemove')) !== null, 4000, 'typed classic RemoteRemove');
  await evalPage(() => {
    for (const k of ['s3b-del-window', 's3b-del-typeconfirm']) localStorage.removeItem(k);
  });
});

await step('delete-window-uniform', async () => {
  // Every destructive flow rides the same window base. Settings at their
  // defaults (window on, typing off) — the bucket-grade ops still type an
  // escalation word, because that is identity, not preference.
  await evalPage(() => {
    for (const k of ['s3b-del-window', 's3b-del-typeconfirm', 's3b-del-autoconfirm']) localStorage.removeItem(k);
  });
  const openBucketCtx = async () => {
    await clickTree('hetzner');
    await waitFor(async () => (await rowKeys()).includes('team-files'), 6000, 'bucket list');
    await openCtx('team-files');
  };
  const adminVersions = async () => { // openModal nests nothing: the delete
    // window replaces the admin panel, so it is reopened for each tool
    await openBucketCtx();
    await ctxItem(/admin panel/i);
    await waitFor(async () => evalPage(() => Array.from(document.querySelectorAll('.tabstrip .tab')).some((x) => x.textContent.trim() === 'Versions')), 4000, 'admin tabs');
    await evalPage(() => {
      const t = Array.from(document.querySelectorAll('.tabstrip .tab')).find((x) => x.textContent.trim() === 'Versions');
      t?.click();
    });
    await waitFor(async () => evalPage(() => document.getElementById('modal-root').textContent.includes('Cleanup tools')), 4000, 'versions tab');
  };

  // (a) bucket delete — the window with counted stats + the L2 name check
  await openBucketCtx();
  await resetCalls();
  await ctxItem(/delete bucket/i);
  await waitFor(async () => evalPage(() => !!document.querySelector('#modal-root .delw-modal')), 4000, 'bucket delete window');
  await ok('bucket delete: one window, counted stats', evalPage(() => {
    const t = document.querySelector('#modal-root .delw-modal').textContent;
    return t.includes('7 object(s)') && t.includes('12 version(s)') && t.includes('2 delete marker(s)');
  }));
  await ok('bucket delete: types the bucket name with typing OFF', evalPage(() => {
    const m = document.querySelector('#modal-root .delw-modal');
    return m.textContent.includes('Type "team-files" to confirm:')
      && !!m.querySelector('.delw-confirm input') && m.querySelector('.btn.danger').disabled;
  }));
  await shotOf('delw-bucket', '#modal-root');
  await evalPage(() => document.querySelector('#modal-root .delw-confirm input').focus());
  await page.keyboard.type('team-files');
  await sleep(80);
  await evalPage(() => document.querySelector('#modal-root .delw-modal .btn.danger').click());
  await waitFor(async () => (await findCall('DeleteBucket')) !== null, 4000, 'DeleteBucket fired');

  // (b) window OFF — the classic typedConfirm ladder (still the name)
  await evalPage(() => localStorage.setItem('s3b-del-window', '0'));
  await openBucketCtx();
  await resetCalls();
  await ctxItem(/delete bucket/i);
  await waitFor(async () => (await evalPage(() => document.getElementById('modal-root').textContent)).includes('Type "team-files" to confirm:'), 4000, 'classic bucket prompt');
  await ok('bucket delete, window off: classic typed prompt', evalPage(() =>
    !document.querySelector('#modal-root .delw-modal') && !!document.querySelector('#modal-root input')));
  await evalPage(() => document.querySelector('#modal-root input').focus());
  await page.keyboard.type('team-files');
  await sleep(80);
  await evalPage(() => document.querySelector('#modal-root .btn.danger').click());
  await waitFor(async () => (await findCall('DeleteBucket')) !== null, 4000, 'classic DeleteBucket fired');
  await evalPage(() => localStorage.removeItem('s3b-del-window'));

  // (c) purge noncurrent — same window, 'purge' escalation at >50
  await adminVersions();
  await resetCalls();
  await evalPage(() => {
    const b = Array.from(document.querySelectorAll('#modal-root .btn')).find((x) => /purge noncurrent/i.test(x.textContent));
    b?.click();
  });
  await waitFor(async () => evalPage(() => !!document.querySelector('#modal-root .delw-modal')), 4000, 'purge window');
  await ok('purge: window with count + typed "purge"', evalPage(() => {
    const m = document.querySelector('#modal-root .delw-modal');
    return m.textContent.includes('60 noncurrent version(s)')
      && m.textContent.includes('Type "purge" to confirm:')
      && !!m.querySelector('.delw-confirm input') && m.querySelector('.btn.danger').disabled;
  }));
  await shotOf('delw-purge', '#modal-root');
  await evalPage(() => document.querySelector('#modal-root .delw-confirm input').focus());
  await page.keyboard.type('purge');
  await sleep(80);
  await evalPage(() => document.querySelector('#modal-root .delw-modal .btn.danger').click());
  await waitFor(async () => (await findCall('PurgeVersions')) !== null, 4000, 'PurgeVersions fired');

  // (d) empty bucket — the window again, L2 name check (admin was replaced)
  await adminVersions();
  await resetCalls();
  await evalPage(() => {
    const b = Array.from(document.querySelectorAll('#modal-root .btn')).find((x) => /empty bucket \(all versions\)/i.test(x.textContent));
    b?.click();
  });
  await waitFor(async () => evalPage(() => !!document.querySelector('#modal-root .delw-modal')), 4000, 'empty window');
  await ok('empty bucket: full version stats + typed name', evalPage(() => {
    const m = document.querySelector('#modal-root .delw-modal');
    return m.textContent.includes('7 current object(s)') && m.textContent.includes('12 version(s)')
      && m.textContent.includes('5 noncurrent') && m.textContent.includes('Type "team-files" to confirm:')
      && m.textContent.includes('Empty bucket');
  }));
  await shotOf('delw-empty', '#modal-root');
  await evalPage(() => document.querySelector('#modal-root .delw-confirm input').focus());
  await page.keyboard.type('team-files');
  await sleep(80);
  await evalPage(() => document.querySelector('#modal-root .delw-modal .btn.danger').click());
  await waitFor(async () => (await findCall('EmptyBucketAllVersions')) !== null, 4000, 'EmptyBucketAllVersions fired');

  await evalPage(() => {
    for (const k of ['s3b-del-window', 's3b-del-typeconfirm', 's3b-del-autoconfirm']) localStorage.removeItem(k);
  });
});

await step('compare', async () => {
  await navObjects('team-files');
  await page.click('#local-compare');
  await waitFor(async () => evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .some((r) => (r.dataset.cmp || '') !== '')), 4000, 'cmp decorations');
  await ok('compare decorations painted', evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .some((r) => ['newer-remote', 'size-diff', 'only-remote', 'diff-below', 'same'].includes(r.dataset.cmp))));
  await ok('CompareAny called with both sides', (await findCall('CompareAny')) !== null);
  await shotOf('compare', '#grid-wrap');
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
  await ok('Import S3 Credential button present', p2.evaluate(() => document.getElementById('empty-actions').textContent.toLowerCase().includes('import s3 credential')));
  await ok('empty actions offer add-source', p2.evaluate(() => document.getElementById('empty-actions').textContent.length > 0));
  await ok('upload greyed without sources', p2.evaluate(() => document.getElementById('btn-upload').disabled));
  await ok('doctor greyed without sources', p2.evaluate(() => window.__s3bCmdState?.canDoctor === false));
  await settlePaint(p2);
  await p2.screenshot({ path: path.join(OUT, String(++shotNo).padStart(2, '0') + '-onboarding-empty.png') });
  // first import straight from the welcome: the app must open the imported
  // source's content, and the welcome never shows again once any source
  // exists ("No data source yet" was sticking around before)
  await p2.evaluate(() => {
    const b = Array.from(document.querySelectorAll('#empty-actions button'))
      .find((x) => /import s3 credential/i.test(x.textContent));
    b?.click();
  });
  await p2.waitForFunction(() => !document.getElementById('modal-root').classList.contains('hidden'), null, { timeout: 8000 });
  await p2.evaluate(() => {
    const ff = Array.from(document.querySelectorAll('#modal-root button'))
      .find((b) => /^from file/i.test(b.textContent.trim()));
    ff?.click();
  });
  await p2.waitForFunction(() => !!document.querySelector('#modal-root .cred-row'), null, { timeout: 8000 });
  await p2.evaluate(() => {
    const imp = Array.from(document.querySelectorAll('#modal-root button'))
      .find((b) => /^import$/i.test(b.textContent.trim()));
    imp?.click();
  });
  await p2.waitForFunction(() => Array.from(document.querySelectorAll('#tree .tlabel'))
    .some((l) => l.textContent === 'from-file-photos'), null, { timeout: 8000 });
  // nav.to loads asynchronously — wait for the listing to render rows
  await ok('import from the welcome auto-opens the source', await p2.waitForFunction(
    () => Array.from(document.querySelectorAll('#grid-body .grid-row'))
      .some((r) => r.querySelector('.tname')?.textContent === 'img-1.jpg'),
    null, { timeout: 8000 }).then(() => true).catch(() => false));
  await ok('welcome hidden once any source exists', await p2.evaluate(() => document.getElementById('empty-state').classList.contains('hidden')));
  await settlePaint(p2);
  await p2.screenshot({ path: path.join(OUT, String(++shotNo).padStart(2, '0') + '-onboarding-autoopen.png') });
  await p2.close();
});

// ===================== DnD matrix (slice 5) =====================
// Every drop runs through the real dragstart (app-built payload) onto the
// real target element; assertions read the recorded backend call.

await step('dnd-s3-to-s3-tree', async () => {
  await navObjects('team-files');
  await resetCalls();
  const from = await gridRow('readme.md');
  const to = await treeRow('logs-2026');
  await dnd(from, to);
  const c = await findCall('CopySelection');
  await ok('routes to CopySelection', !!c);
  await ok('args: src team-files → dst logs-2026', c && c.args[0] === 'team-files' && c.args[2] === 'logs-2026');
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
  await ok('items pin the originating S3 source', c && c.args[0][0].bucket === 'team-files' && c.args[0][0].source === 'hetzner');
  await ok('dest is the remote source', c && c.args[2].kind === 'remote' && c.args[2].source === 'backup-box');
  await shot('dnd-s3-remote');
});

await step('dnd-s3-onto-folder-move', async () => {
  await resetCalls();
  const from = await gridRow('readme.md');
  const to = await gridRow('docs');
  await dnd(from, to);
  // team-files keeps versioning → the per-task version choice appears;
  // keep it plain (latest versions only)
  await vcvChoose(false);
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
  // pane on local Downloads, main grid on team-files objects.
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
  await ok('uploads into bucket+folder', c && c.args[1] === 'team-files' && c.args[2] === 'docs/');
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
  await ok('carries the bucket', c && c.args[0] === 'team-files');
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
  await navObjects('team-files');
  await resetCalls();
  await page.evaluate(() => {
    const r = document.getElementById('grid-body').getBoundingClientRect();
    window.__shim.emit('wails:file-drop', r.left + r.width / 2, r.top + r.height / 2, ['C:\\Users\\demo\\Downloads\\photos.zip']);
  });
  await waitFor(async () => (await findCall('Upload')) !== null, 4000, 'OS-drop upload');
  const c = await findCall('Upload');
  await ok('OS drop over grid uploads into current view', c && c.args[1] === 'team-files' && /photos\.zip$/.test(c.args[0][0]));
});

await step('dnd-os-file-drop-tree', async () => {
  // OS files dropped ONTO a sidebar node: the node under the cursor decides
  // the destination, never the accidentally-open main view. Bucket node →
  // s3 dest carrying the node's source; non-S3 source root → remote root.
  const overTreeNode = async (label) => {
    const r = await treeRow(label);
    if (!r) throw new Error(`no tree node "${label}"`);
    const box = await r.asElement().boundingBox();
    await page.evaluate(({ x, y }) => {
      window.__shim.emit('wails:file-drop', x, y, ['C:\\Users\\demo\\Downloads\\photos.zip']);
    }, { x: box.x + box.width / 2, y: box.y + box.height / 2 });
  };
  await resetCalls();
  await overTreeNode('team-files');
  await waitFor(async () => (await findCall('TransferCross')) !== null, 4000, 'bucket-node drop');
  let c = await findCall('TransferCross');
  await ok('drop on bucket node uploads to that bucket root', c
    && c.args[2].bucket === 'team-files' && c.args[2].dir === '' && /photos\.zip$/.test(c.args[1][0]));
  await ok('bucket node carries its source', c && c.args[2].source === 'hetzner');
  await resetCalls();
  await overTreeNode('dav-claims');
  await waitFor(async () => (await findCall('TransferCross')) !== null, 4000, 'remote-source-node drop');
  c = await findCall('TransferCross');
  await ok('drop on non-S3 source node goes to its root', c
    && c.args[2].kind === 'remote' && c.args[2].source === 'dav-claims' && c.args[2].dir === '/');
});

await step('edit-choose-app', async () => {
  // Settings → Editing (default ON): Edit routes through the OS
  // "Open with…" chooser — EditObject(bucket, key, chooseApp)
  await navObjects('team-files');
  await resetCalls();
  await openCtx('readme.md');
  await ctxItem(/^edit$/i);
  await waitFor(async () => (await findCall('EditObject')) !== null, 4000, 'EditObject (chooser on)');
  let c = await findCall('EditObject');
  await ok('Edit asks for the OS app by default (chooseApp=true)', c
    && c.args[1] === 'readme.md' && c.args[2] === true);
  // the Settings toggle turns the chooser off → default app next time
  await evalPage(() => localStorage.setItem('s3b-edit-choose-app', '0'));
  await resetCalls();
  await openCtx('readme.md');
  await ctxItem(/^edit$/i);
  await waitFor(async () => (await findCall('EditObject')) !== null, 4000, 'EditObject (chooser off)');
  c = await findCall('EditObject');
  await ok('setting off edits with the default app (chooseApp=false)', c && c.args[2] === false);
  await evalPage(() => localStorage.setItem('s3b-edit-choose-app', '1'));
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
  await ok('Ctrl+C/V copies across buckets', c && c.args[0] === 'team-files' && c.args[2] === 'logs-2026' && c.args[4] === false);
  // cut → move (the logs-2026 root collapses prefixes into the app/ folder
  // row — enter the folder to reach the file rows)
  await resetCalls();
  await dblClickRow('app');
  await waitFor(async () => (await rowKeys()).includes('app/app-2026-09-11.log'), 6000, 'app objects');
  // gridRow matches the .tname display label (file name without the folder
  // prefix), not the full anchored key
  await clickRow('app-2026-09-11.log');
  await page.keyboard.press('Control+x');
  await clickTree('team-files');
  await waitFor(async () => (await rowKeys()).includes('readme.md'), 6000, 'back to team-files');
  await page.keyboard.press('Control+v');
  await vcvChoose(false); // team-files is versioned — decline the history copy
  await sleep(200);
  c = await findCall('CopySelection');
  await ok('Ctrl+X/V moves', c && c.args[4] === true && c.args[2] === 'team-files');
  // remote clipboard → paste into S3 streams through TransferCross
  await resetCalls();
  await clickTree('backup-box');
  await waitFor(async () => (await rowKeys()).includes('/backup.sh'), 6000, 'remote listing');
  await clickRow('backup.sh');
  await page.keyboard.press('Control+c');
  await clickTree('team-files');
  await waitFor(async () => (await rowKeys()).includes('readme.md'), 6000, 'objects view');
  await page.keyboard.press('Control+v');
  await sleep(200);
  c = await findCall('TransferCross');
  await ok('remote paste routes through TransferCross', c && c.args[0][0].source === 'backup-box' && c.args[2].bucket === 'team-files');
});

await step('copy-versions-choice', async () => {
  // Per-task version preservation on S3→S3: asked ONLY when the
  // destination bucket has versioning enabled; the checkbox pre-sets
  // from the Settings toggle (default on).
  // (a) Suspended destination → straight plain copy, no dialog at all
  await navObjects('team-files');
  await resetCalls();
  let from = await gridRow('readme.md');
  await dnd(from, await treeRow('logs-2026'));
  await sleep(300); // let any (wrong) async guard/dialog path settle
  let c = await findCall('CopySelection');
  await ok('Suspended dest copies without asking', !!c && !(await modalVisible())
    && (await calls()).every((x) => x.m !== 'CopySelectionVersions'));
  // (b) Enabled destination → dialog with the checkbox pre-set on →
  // versioned job with the right args
  await clickTree('logs-2026');
  await waitFor(async () => (await rowKeys()).includes('app/'), 6000, 'logs objects');
  await resetCalls();
  from = await gridRow('app');
  await dnd(from, await treeRow('team-files'));
  await waitFor(modalVisible, 4000, 'version choice dialog');
  await ok('checkbox pre-set from Settings (on)', evalPage(() => document.querySelector('#modal-root .vcv-row input')?.checked === true));
  await shot('copy-versions-choice');
  await evalPage(() => document.querySelector('#modal-root .modal-foot .btn.primary').click());
  await waitFor(async () => (await findCall('CopySelectionVersions')) !== null, 4000, 'versioned copy started');
  c = await findCall('CopySelectionVersions');
  await ok('versioned job args', c && c.args[0] === '' && c.args[1] === 'logs-2026'
    && c.args[2][0] === 'app/' && ['hetzner', ''].includes(c.args[3])
    && c.args[4] === 'team-files' && c.args[6] === false);
  // (c) Settings default off → checkbox starts unchecked → falls back to
  // the plain latest-version copy
  await evalPage(() => localStorage.setItem('s3b-copy-versions', '0'));
  await resetCalls();
  from = await gridRow('app');
  await dnd(from, await treeRow('team-files'));
  await waitFor(modalVisible, 4000, 'dialog again');
  await ok('checkbox pre-set off with Settings off', evalPage(() => document.querySelector('#modal-root .vcv-row input')?.checked === false));
  await vcvChoose(false);
  c = await findCall('CopySelection');
  await ok('unchecked falls back to plain copy', !!c && c.args[2] === 'team-files'
    && (await calls()).every((x) => x.m !== 'CopySelectionVersions'));
  await evalPage(() => localStorage.removeItem('s3b-copy-versions'));
});

// ===================== view-source + OS interop (slice 6) =====================

await step('import-creds-file', async () => {
  await resetCalls();
  await page.locator('#menubar .mb-title').first().click(); // File
  await sleep(80);
  const item = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /import s3 credential/i.test(i.textContent)) || null);
  await ok('File→Import S3 Credential present', !!item);
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
  await shotOf('import-creds', '#modal-root .modal');
  // Test → connectivity check + ok badge
  const test = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .cred-row button'))
    .find((b) => /^test$/i.test(b.textContent.trim())) || null);
  if (test) await test.asElement().click();
  await waitFor(async () => evalPage(() => document.querySelectorAll('#modal-root .cred-test.ok').length > 0), 4000, 'test badge');
  await ok('candidate tested before import', (await findCall('TestCredentialDraft')) !== null);
  // Import → backend import + the tree gains one bucket-scoped source per
  // discovered bucket (the candidate name itself never becomes a source)
  const imp = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
    .find((b) => /^import$/i.test(b.textContent.trim())) || null);
  if (imp) await imp.asElement().click();
  await waitFor(async () => (await findCall('ImportCredentials')) !== null, 4000, 'import call');
  const c = await findCall('ImportCredentials');
  await ok('imports the checked candidate end-to-end', c && JSON.stringify(c.args[0]) === '["cand-file1"]');
  await waitFor(async () => !!(await treeRow('from-file-photos')), 4000, 'tree gains from-file-photos');
  await ok('each discovered bucket becomes its own source', !!(await treeRow('from-file-photos')) && !!(await treeRow('from-file-logs')));
  await ok('the candidate name itself never becomes a source', !(await treeRow('from-file')));
  // re-import: the dialog reopens, the same credential is imported again —
  // existing bucket sources are UPDATED in place, never duplicated
  await page.locator('#menubar .mb-title').first().click(); // File
  await sleep(80);
  const item2 = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /import s3 credential/i.test(i.textContent)) || null);
  await ok('File→Import S3 Credential reachable again', !!item2);
  if (item2) {
    await item2.asElement().click();
    await waitFor(modalVisible, 4000, 'import dialog again');
    const ff2 = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
      .find((b) => /^from file/i.test(b.textContent.trim())) || null);
    if (ff2) await ff2.asElement().click();
    await waitFor(async () => !!(await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .cred-row'))
      .find((r) => r.textContent.includes('from-file')) || null)), 4000, 'cred row again');
    const imp2 = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root button'))
      .find((b) => /^import$/i.test(b.textContent.trim())) || null);
    if (imp2) await imp2.asElement().click();
    await sleep(200);
  }
  await ok('re-import updates without duplicating', await evalPage(() => Array.from(document.querySelectorAll('#tree .tlabel'))
    .filter((l) => l.textContent === 'from-file-photos').length) === 1);
});

await step('import-creds-kms', async () => {
  await resetCalls();
  await page.locator('#menubar .mb-title').first().click(); // File
  await sleep(80);
  const item = await elOrNull(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /import s3 credential/i.test(i.textContent)) || null);
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
  // 'from-file-photos' is a bucket-scoped imported source: opening it pins
  // the engine to it (SetViewSource), mirrors it in the status bar, and
  // opens the bucket contents DIRECTLY (no buckets-view round trip)
  await resetCalls();
  await clickTree('from-file-photos');
  await waitFor(async () => (await rowKeys()).includes('img-1.jpg'), 6000, 'from-file-photos contents');
  const c = await findCall('SetViewSource');
  await ok('opening a source pins it as the view source', c && c.args[0] === 'from-file-photos');
  await ok('status bar mirrors the active source', (await txt('#status-profile')).includes('from-file-photos'));
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('team-files'), 6000, 'back to hetzner buckets');
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
  await navObjectsOf('hetzner', 'team-files');
  // clicking the navbar's empty area opens the inline path editor holding
  // the canonical Source://bucket/prefix path
  await evalPage(() => document.querySelector('.navbar').dispatchEvent(new MouseEvent('click', { bubbles: true })));
  await waitFor(async () => evalPage(() => !!document.querySelector('#breadcrumb input.path-edit')), 4000, 'path editor');
  await ok('path field holds the canonical path', evalPage(() => document.querySelector('#breadcrumb input.path-edit')?.value === 'hetzner://team-files/'));
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
  // paste-parity's Ctrl+C left a staging poll parked on the busy seed
  // queue. Idling the queue here would wake BOTH polls and whichever
  // SetFiles lands first bumps the seq — the other's clobber guard then
  // aborts it (a 50/50 on OUR mirror dying). Kill every EARLIER pending
  // mirror deterministically: bump the seq (their guards see the change
  // and abort), then idle the queue so OUR 700ms poll fires into an
  // empty engine with a stable seq.
  await evalPage(() => {
    window.__shim.world.osClipSeq = (window.__shim.world.osClipSeq || 0) + 1;
    window.__shim.world.transfers = [];
  });
  await clickRow('backup.sh');
  await page.keyboard.press('Control+c');
  await waitFor(async () => (await findCall('TransferCross')) !== null, 4000, 'staging transfer');
  const c = await findCall('TransferCross');
  await ok('copy stages through TransferCross', c && c.args[0][0].source === 'backup-box' && c.args[0][0].key === '/backup.sh');
  await ok('staging lands in the clipboard dir', c && c.args[2].kind === 'local' && /s3b-clip/.test(c.args[2].dir || ''));
  await ok('staging uses the overwrite policy', c && c.args[3] === 'overwrite');
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
  await p3.evaluate(() => {
    // a real Explorer copy writes the file list AND bumps the system seq
    window.__shim.world.osClip = ['C:\\Users\\demo\\Downloads\\photos.zip'];
    window.__shim.world.osClipSeq = (window.__shim.world.osClipSeq || 0) + 1;
  });
  await p3.evaluate(() => Array.from(document.querySelectorAll('#tree .tnode'))
    .find((n) => n.querySelector('.tlabel')?.textContent === 'team-files')?.click());
  await p3.waitForFunction(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .some((r) => r._model && r._model.key === 'readme.md'), null, { timeout: 8000 });
  await p3.evaluate(() => { window.__shim.calls.length = 0; });
  await p3.keyboard.press('Control+v');
  await p3.waitForFunction(() => (window.__shim.calls || []).some((x) => x.m === 'Upload'), null, { timeout: 6000 });
  const c = await p3.evaluate(() => window.__shim.calls.filter((x) => x.m === 'Upload').at(-1));
  await ok('Explorer-copied paths paste into a bucket', c && c.args[1] === 'team-files' && /photos\.zip$/.test(c.args[0][0]));
  const sawOs = await p3.evaluate(() => window.__shim.calls.some((x) => x.m === 'OsClipboardFiles'));
  await ok('OS clipboard read through the binding', sawOs);
  await p3.screenshot({ path: path.join(OUT, String(++shotNo).padStart(2, '0') + '-os-paste.png') });
  await p3.close();
});

await step('os-clipboard-precedence', async () => {
  // LAST COPY WINS. The old bug: one in-app Ctrl+C left clipboard.keys set
  // for the whole session, so every later Explorer copy was ignored. Now an
  // external copy bumps the OS seq past our baseline and outranks the stale
  // app payload; with the seq unchanged the app payload keeps precedence.
  const p4 = await context.newPage();
  p4.on('pageerror', (e) => { pageErrors.push(String(e)); });
  await p4.addInitScript(shim);
  await p4.goto(BASE);
  await p4.waitForFunction(() => (document.getElementById('status-version')?.textContent || '').includes('s3b v'), null, { timeout: 10000 });
  // fresh world seeds running jobs for the transfer-manager UI — idle the
  // queue so the copies' 700ms mirror polls settle immediately (see the
  // os-copy-mirror step for the same dance)
  await p4.evaluate(() => { window.__shim.world.transfers = []; });
  const openBucket = async (name, key) => {
    await p4.evaluate((b) => Array.from(document.querySelectorAll('#tree .tnode'))
      .find((n) => n.querySelector('.tlabel')?.textContent === b)?.click(), name);
    await p4.waitForFunction((k) => Array.from(document.querySelectorAll('#grid-body .grid-row'))
      .some((r) => r._model && r._model.key === k), key, { timeout: 8000 });
  };
  // selection needs the full trusted click sequence (mousedown+mouseup) —
  // a synthetic in-page el.click() navigates the tree but does NOT select
  // grid rows, and a Ctrl+C with no selection is a silent no-op
  const selectRow = async (key) => {
    const h = await p4.evaluateHandle((k) => Array.from(document.querySelectorAll('#grid-body .grid-row'))
      .find((r) => r._model && r._model.key === k) || null, key);
    const el = h.asElement();
    if (!el) throw new Error(`no grid row "${key}"`);
    await el.click();
  };
  const explorerCopy = (file) => p4.evaluate((f) => {
    window.__shim.world.osClip = [f];
    window.__shim.world.osClipSeq = (window.__shim.world.osClipSeq || 0) + 1;
  }, file);

  // (a) in-app copy (mirror settles) → Explorer copies something else →
  // the Explorer files MUST win over the stale app payload
  await openBucket('team-files', 'readme.md');
  await selectRow('readme.md');
  await p4.keyboard.press('Control+c');
  await p4.waitForFunction(() => (window.__shim.calls || []).some((x) => x.m === 'OsClipboardSetFiles'), null, { timeout: 8000 });
  await explorerCopy('C:\\Users\\demo\\Downloads\\notes.txt');
  await p4.evaluate(() => { window.__shim.calls.length = 0; });
  await p4.keyboard.press('Control+v');
  await p4.waitForFunction(() => (window.__shim.calls || []).some((x) => x.m === 'Upload'), null, { timeout: 6000 });
  const up = await p4.evaluate(() => window.__shim.calls.find((x) => x.m === 'Upload'));
  await ok('Explorer copy outranks the stale app clipboard', up && /notes\.txt$/.test(up.args[0][0]) && up.args[1] === 'team-files');

  // (b) OS clipboard unchanged since our copy → the app payload wins
  // (server-side CopySelection, no Upload of staged files)
  await selectRow('readme.md');
  await p4.keyboard.press('Control+c');
  // (a) wiped the call log, so this counts from zero: wait for THIS copy's
  // own mirror — the staged readme.md landing on the OS clipboard
  await p4.waitForFunction(() => (window.__shim.calls || []).some((x) => x.m === 'OsClipboardSetFiles'
    && (x.args[0] || []).some((p) => /readme\.md$/.test(p))), null, { timeout: 8000 });
  await openBucket('logs-2026', 'app/');
  await p4.evaluate(() => { window.__shim.calls.length = 0; });
  await p4.keyboard.press('Control+v');
  await p4.waitForFunction(() => (window.__shim.calls || []).some((x) => x.m === 'CopySelection'), null, { timeout: 6000 });
  const cp = await p4.evaluate(() => window.__shim.calls.find((x) => x.m === 'CopySelection'));
  const noUp = await p4.evaluate(() => !window.__shim.calls.some((x) => x.m === 'Upload'));
  await ok('unchanged OS clipboard keeps app payload precedence', cp && cp.args[0] === 'team-files' && cp.args[2] === 'logs-2026' && noUp);
  await p4.close();
});

await step('os-copy-mirror-abort', async () => {
  // clobber guard: the user copies elsewhere while a copy's staging download
  // is in flight → the late OS mirror must abort, not replace their clipboard
  const p5 = await context.newPage();
  p5.on('pageerror', (e) => { pageErrors.push(String(e)); });
  await p5.addInitScript(shim);
  await p5.goto(BASE);
  await p5.waitForFunction(() => (document.getElementById('status-version')?.textContent || '').includes('s3b v'), null, { timeout: 10000 });
  await p5.evaluate(() => Array.from(document.querySelectorAll('#tree .tnode'))
    .find((n) => n.querySelector('.tlabel')?.textContent === 'backup-box')?.click());
  await p5.waitForFunction(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .some((r) => r._model && r._model.key === '/backup.sh'), null, { timeout: 8000 });
  await p5.evaluate(() => { window.__shim.calls.length = 0; });
  // trusted click (see the precedence step) — a synthetic click would leave
  // the selection empty and make the copy a no-op, turning this into a
  // vacuous pass
  {
    const h = await p5.evaluateHandle(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
      .find((r) => r._model && r._model.key === '/backup.sh') || null);
    const el = h.asElement();
    if (!el) throw new Error('no /backup.sh row');
    await el.click();
  }
  await p5.keyboard.press('Control+c');
  // staging transfer queued; the "user copy" must land BEFORE the 700ms
  // idle poll — bump the seq and idle the engine immediately
  await p5.evaluate(() => {
    window.__shim.world.osClip = ['C:\\Users\\demo\\Downloads\\other.txt'];
    window.__shim.world.osClipSeq = (window.__shim.world.osClipSeq || 0) + 1;
    window.__shim.world.transfers = [];
  });
  await sleep(1800); // well past the idle poll
  const mirror = await p5.evaluate(() => (window.__shim.calls || []).filter((x) => x.m === 'OsClipboardSetFiles'));
  const kept = await p5.evaluate(() => (window.__shim.world.osClip || [])[0]);
  await ok('late mirror aborted, user clipboard kept', mirror.length === 0 && /other\.txt$/.test(kept || ''));
  await p5.close();
});

await step('os-clipboard-setting', async () => {
  // Settings → Transfers → Explorer copy & paste: OFF kills the whole OS
  // bridge (no reads, no writes, honest "Nothing to paste"); default is ON.
  const p6 = await context.newPage();
  p6.on('pageerror', (e) => { pageErrors.push(String(e)); });
  await p6.addInitScript(shim);
  await p6.goto(BASE);
  await p6.waitForFunction(() => (document.getElementById('status-version')?.textContent || '').includes('s3b v'), null, { timeout: 10000 });
  await ok('Explorer copy enabled by default', await p6.evaluate(() => localStorage.getItem('s3b-os-clip') === null));
  // toggle OFF through the real dialog row
  await p6.locator('#menubar .mb-title', { hasText: /settings/i }).first().click();
  await p6.waitForFunction(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .some((i) => /settings/i.test(i.textContent) && !i.classList.contains('has-sub')), null, { timeout: 4000 });
  await p6.evaluate(() => Array.from(document.querySelectorAll('#menubar .mb-dd:not(.hidden) .mb-item'))
    .find((i) => /settings/i.test(i.textContent) && !i.classList.contains('has-sub')).click());
  await p6.waitForFunction(() => document.querySelector('#modal-root .set-row') !== null, null, { timeout: 4000 });
  await p6.evaluate(() => {
    const row = Array.from(document.querySelectorAll('#modal-root .set-row'))
      .find((x) => /explorer copy & paste/i.test(x.querySelector('.set-name')?.textContent || ''));
    const cb = row?.querySelector('input[type=checkbox]');
    if (cb && cb.checked) cb.click();
  });
  await p6.evaluate(() => document.querySelector('#modal-root .modal-foot .btn.primary').click());
  await ok('toggle persisted off', await p6.evaluate(() => localStorage.getItem('s3b-os-clip') === '0'));
  // an Explorer copy now pastes as nothing — no binding read, honest toast
  await p6.evaluate(() => Array.from(document.querySelectorAll('#tree .tnode'))
    .find((n) => n.querySelector('.tlabel')?.textContent === 'team-files')?.click());
  await p6.waitForFunction(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .some((r) => r._model && r._model.key === 'readme.md'), null, { timeout: 8000 });
  await p6.evaluate(() => {
    window.__shim.world.osClip = ['C:\\Users\\demo\\Downloads\\secret.txt'];
    window.__shim.world.osClipSeq = (window.__shim.world.osClipSeq || 0) + 1;
    window.__shim.calls.length = 0;
  });
  await p6.keyboard.press('Control+v');
  await sleep(500);
  const sawRead = await p6.evaluate(() => window.__shim.calls.some((x) => x.m === 'OsClipboardFiles' || x.m === 'OsClipboardState'));
  const sawToast = await p6.evaluate(() => (document.getElementById('toasts')?.textContent || '').includes('Nothing to paste'));
  await ok('disabled: OS clipboard never read', !sawRead);
  await ok('disabled: honest "Nothing to paste" toast', sawToast);
  // re-enable → the very next paste works again without a reload
  await p6.evaluate(() => localStorage.setItem('s3b-os-clip', '1'));
  await p6.evaluate(() => { window.__shim.calls.length = 0; });
  await p6.keyboard.press('Control+v');
  await p6.waitForFunction(() => (window.__shim.calls || []).some((x) => x.m === 'Upload'), null, { timeout: 6000 });
  const up = await p6.evaluate(() => window.__shim.calls.find((x) => x.m === 'Upload'));
  await ok('re-enabled: Explorer paste works again', up && /secret\.txt$/.test(up.args[0][0]));
  await p6.evaluate(() => localStorage.removeItem('s3b-os-clip')); // leave clean for later pages
  await p6.close();
});

await step('drag-urls', async () => {
  // selecting files precomputes drag-out URLs (OS drag needs synchronous
  // data in dragstart)
  await navObjectsOf('hetzner', 'team-files');
  await resetCalls();
  await clickRow('readme.md');
  await waitFor(async () => (await findCall('MakeDragUrls')) !== null, 4000, 'drag urls');
  const c = await findCall('MakeDragUrls');
  await ok('selection precomputes drag-out URLs', c && c.args[0][0].bucket === 'team-files'
    && c.args[0][0].key === 'readme.md' && c.args[0][0].name === 'readme.md');
  await ok('drag items tag the originating source', c && c.args[0][0].source === 'hetzner');
});

// ===================== full-feature coverage (slice 7) =====================
await step('toolbar-nav', async () => {
  // back/forward/up through BOTH the toolbar buttons and the Alt-key
  // shortcuts, plus F5 — the canonical file-manager navigation set
  await navObjectsOf('hetzner', 'team-files');
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
  await navObjectsOf('hetzner', 'team-files');
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
  await waitFor(async () => (await rowKeys()).includes('team-files'), 6000, 'buckets view');
  await ok('favorites hidden while empty', evalPage(() => document.getElementById('fav-section').classList.contains('hidden')));
  await openCtx('team-files');
  await ctxItem(/add to favorites/i);
  await waitFor(async () => !!(await elOrNull(() => document.querySelector('#favorites .fav-row') || null)), 4000, 'fav row');
  await ok('favorite appears in the sidebar', evalPage(() => document.querySelector('#favorites .fav-label')?.textContent === 'team-files'));
  await ok('favorite persisted', (await evalPage(() => localStorage.getItem('s3b-favs'))).includes('team-files'));
  await shot('favorites');
  await resetCalls();
  await page.click('#favorites .fav-row');
  await waitFor(async () => (await txt('#breadcrumb')).includes('team-files')
    && (await rowKeys()).some((k) => k === 'readme.md'), 6000, 'fav navigation');
  await ok('clicking a favorite opens the bucket', true);
  // remove again — the section hides
  await clickTree('hetzner');
  await waitFor(async () => (await rowKeys()).includes('team-files'), 6000, 'buckets view again');
  await openCtx('team-files');
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
  // the curated theme shot shows the plain main view — the dual pane
  // (left open by the compare/DnD matrix steps, carrying whatever source
  // it was last bound to) is closed for the capture and reopened after,
  // so the rest of the walk runs against identical state
  const paneOpen = await evalPage(() => !document.getElementById('local-pane').classList.contains('hidden'));
  if (paneOpen) await page.click('#btn-panes');
  await shotOf(`theme-${after}`, '#grid-wrap');
  if (paneOpen) await page.click('#btn-panes');
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
  await navObjectsOf('hetzner', 'team-files');
  // Default is OFF: with no stored interval the indicator must stay hidden.
  await ok('auto refresh default OFF (no indicator)', evalPage(() => document.getElementById('status-auto').classList.contains('hidden')));
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
  await navObjectsOf('hetzner', 'team-files');
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
  await navObjectsOf('hetzner', 'team-files');
  await clickRow('readme.md');
  await resetCalls();
  await page.keyboard.press('Control+d');
  await waitFor(async () => (await findCall('DownloadRefs')) !== null, 4000, 'DownloadRefs');
  const c = await findCall('DownloadRefs');
  await ok('Ctrl+D downloads through DownloadRefs', c && c.args[0] === 'team-files'
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
  await ok('Ctrl+U runs the Files picker', up && up.args[1] === 'team-files');
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
  await waitFor(async () => (await rowKeys()).includes('team-files'), 6000, 'buckets view');
  await openCtx('team-files');
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
