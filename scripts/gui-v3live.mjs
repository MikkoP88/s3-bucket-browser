#!/usr/bin/env node
// gui-v3live.mjs — the Wails v3 live walk: REAL v3 runtime + REAL bindings
// + REAL backend, driven in a real browser.
//
// The app is built with Wails' `server` build tag (go build -tags server),
// which runs the exact production stack — embedded frontend, /wails/runtime.js
// (loaded by the fixed ES-module tag in index.html), the js/bridge.js v2
// surface, HTTP-stream binding calls, WebSocket event delivery — behind a
// plain HTTP server (WAILS_SERVER_PORT). Playwright drives that stack in a
// real Chromium, so unlike gui-visual (fake backend) and the old gui-live
// (v2-era HTTP bridge) this exercises the same code paths the desktop
// webview runs, including the ESM runtime loading that once killed the app.
//
// Live data: local e2e containers (MinIO :9000 minioadmin, SFTP :2222,
// FTP :2121, WebDAV :7070 — see scripts/e2e-cross.sh) plus fixture files.
// The walk never touches the user's profile: S3B_CONFIG points at a
// throwaway directory for the whole run, and remote sections join only
// while their port answers.
//
// Coverage: boot/v3 layer checks → every menubar dropdown → onboarding →
// add+test source → browse/tree guards → filter → context menu → admin →
// doctor → help popouts (in-page: server flag gates native windows off) →
// transfer manager → dual pane → New folder → DnD upload → versions →
// conflict overwrite → A/B diff → DnD download (bytes verified) → rename →
// deep Find → remote engines (sftp/ftp/webdav round-trip + cross-engine) →
// fault lab (scripts/faultproxy.mjs: latency/throttle/reset/blackhole in
// front of the same MinIO — skeleton states, classified error vs watchdog
// timeout, Retry recovery) →
// i18n → settings/theme → log area → profile save/close/open through the
// real bindings → server restart (session model) → Delete Window cleanup
// (marker + permanent purge). A screenshot is captured from EVERY view.
//
// Usage:  node scripts/gui-v3live.mjs [--headed] [--channel msedge|chrome]
// Artifacts: testartifacts/gui-v3live/ (shots/, fixtures/, server log,
// browser profile, throwaway config) — wiped fresh every run.

import { spawn, execFileSync } from 'node:child_process';
import { rm, mkdir, writeFile, readFile, readdir } from 'node:fs/promises';
import net from 'node:net';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const ART = path.join(ROOT, 'testartifacts', 'gui-v3live');
const SHOTS = path.join(ART, 'shots');
const FIX = path.join(ART, 'fixtures');
const CFG = path.join(ART, 'config');          // S3B_CONFIG — throwaway app config
const PROFILE = path.join(ART, 'walk.s3bprofile');
const USERDIR = path.join(ART, 'browser-profile');
const SRV_EXE = path.join(ROOT, 'testartifacts', 's3b-server.exe');
const SRVLOG = path.join(ART, 'server.log');
// The exe under test (testartifacts/s3b-server.exe) is built by whoever drives
// this walk: verify.mjs stamps its own build and passes S3B_EXPECT_VERSION so
// the GetVersion round-trip asserts the REAL stamp (release runs stamp the
// tag). Standalone runs fall back to the dev-stamp default.
const VERSION = process.env.S3B_EXPECT_VERSION || 'v1.1.0-beta.14-9-wails3';

const arg = (k) => { const i = process.argv.indexOf(`--${k}`); return i >= 0 ? process.argv[i + 1] : null; };
const HEADED = process.argv.includes('--headed');

const PKG = 'github.com/MikkoP88/s3-bucket-browser/pkg/api';
const ENDPOINT = 'http://localhost:9000';
const KEY = 'minioadmin';
const SECRET = 'minioadmin';
const REGION = 'us-east-1';
const BUCKET = 'guiv3-walk';        // versioned, seeded (see the mc block in main)
const SRCNAME = 'minio-live';
const SEED = 'seed';
const PORT = 39872;                 // fixed: restart keeps the origin (localStorage) stable
const PREFIX = 'zz-v3';
const V1 = 'v3 live walk v1 — uploaded through the real v3 bridge\n';
const V2 = 'v3 live walk v2 — overwritten through the conflict dialog\n';
const B_TXT = 'second fixture file for the v3 walk\n';
const PASSWORD = 'v3-walk-password-1';
const LOCK = '\u{1F510}';

const E2E_USER = process.env.S3B_E2E_USER || 'e2e';
const E2E_PASS = process.env.S3B_E2E_PASS || 'e2epass';
const ENGINES = [
  { label: 'sftp', type: 'sftp', port: +(process.env.S3B_SFTP_PORT || 2222), root: '/upload' },
  { label: 'ftp', type: 'ftp', port: +(process.env.S3B_FTP_PORT || 2121), root: '' },
  { label: 'webdav', type: 'webdav', port: +(process.env.S3B_WEBDAV_PORT || 7070), root: '' },
];
const TREE_FILES = {
  'readme.md': 'tree readme\n',
  'root-1.txt': 'root file one\n',
  'docs/a.md': 'docs alpha\n',
  'docs/b with space.txt': 'space in the name\n',
  'docs/uni-åäö.txt': 'unicode åäö content\n',
  'docs/nested/deep-file.txt': 'deep file\n',
  'docs/empty.txt': '',
  'logs/l1.log': 'log one\n',
  'logs/l2.log': 'log two\n',
};
const RUNID = `v${Date.now().toString(36)}`;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let pass = 0, fail = 0; const failures = [];
async function step(name, fn) {
  process.stdout.write(`== ${name}\n`);
  try { const v = await fn(); pass++; return v; }
  catch (err) {
    fail++; failures.push(`${name}: ${err.message}`); console.error(`   FAIL: ${err.message}`);
    // stall triage: dump the flight recorder at the moment of failure (the
    // fault lab reloads the page later, which would wipe it)
    try {
      const obs = await evalPage(() => (window.__obs || []).slice(-150));
      if (obs.length) {
        console.log(`   FLIGHT RECORDER (last ${obs.length} events):`);
        for (const l of obs) console.log(`     ${l}`);
      }
      const logs = await evalPage(() => (window.__logs || []).slice(-40));
      if (logs.length) {
        console.log(`   BACKEND log:line (last ${logs.length}):`);
        for (const l of logs) console.log(`     ${l}`);
      }
    } catch { /* recorder optional */ }
  }
  finally { await dismissUI(); }
}
async function dismissUI() {
  if (!page) return;
  try {
    // in-page popouts first (server flag): close via their own × buttons
    await evalPage(() => Array.from(document.querySelectorAll('#popout-root .popout .modal-head .x')).forEach((x) => x.click()));
    await sleep(60);
    await page.keyboard.press('Escape');
    await sleep(60);
    await page.keyboard.press('Escape');
    await sleep(60);
    const btn = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
      .reverse().find((b) => /^(close|cancel)$/i.test(b.textContent.trim())) || null);
    if (btn) await btn.asElement().click();
    await evalPage(() => {
      document.getElementById('ctxmenu')?.classList.add('hidden');
      const r = document.getElementById('modal-root');
      if (r && !r.classList.contains('hidden') && !r.querySelector('.modal')) r.classList.add('hidden');
      document.querySelectorAll('#menubar .mb-dd').forEach((d) => d.classList.add('hidden'));
    });
  } catch { /* best effort */ }
}
async function ok(label, cond) {
  if (cond) { pass++; process.stdout.write(`   ok  ${label}\n`); }
  else { fail++; failures.push(label); console.error(`   FAIL ${label}`); }
}
async function waitFor(fn, ms = 15000, what = 'condition') {
  const t0 = Date.now();
  for (;;) {
    let v; try { v = await fn(); } catch { v = false; }
    if (v) return v;
    if (Date.now() - t0 > ms) throw new Error(`timeout waiting for ${what}`);
    await sleep(150);
  }
}
const evalPage = (fn, ...args) => page.evaluate(fn, ...args);
const elOrNull = (js, arg) => page.evaluateHandle(js, arg).then(async (h) => ((await h.asElement()) ? h : null));
const rowKeys = () => evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
  .filter((r) => r.style.display !== 'none' && r._model).map((r) => r._model.key));
const names = () => evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
  .filter((r) => r.style.display !== 'none' && r._model).map((r) => r._model.name || r._model.key));
const sideKeys = () => evalPage(() => Array.from(document.querySelectorAll('#local-grid-body .grid-row'))
  .filter((r) => r.style.display !== 'none' && r._model).map((r) => String(r._model.key).replace(/[\\/]+$/, '').split(/[\\/]/).pop()));
const gridRow = (label) => elOrNull((l) => {
  const norm = (s) => String(s || '').replace(/\/+$/, '');
  return Array.from(document.querySelectorAll('#grid-body .grid-row'))
    .find((r) => norm(r.querySelector('.tname')?.textContent) === norm(l)) || null;
}, label);
const sideRow = (label) => elOrNull((l) => {
  const norm = (s) => String(s || '').replace(/\/+$/, '');
  return Array.from(document.querySelectorAll('#local-grid-body .grid-row'))
    .find((r) => norm(r.querySelector('.tname')?.textContent) === norm(l)) || null;
}, label);
const bodyH = () => page.evaluateHandle(() => document.getElementById('grid-body'));
const sideBodyH = () => page.evaluateHandle(() => document.getElementById('local-grid-body'));
// The grids are virtualized (grid.js renders only the scrolled window +
// overscan), so a row can exist in the model yet be absent from the DOM —
// names()/rowKeys()/sideKeys() would never see it. The engine fixture roots
// accumulate one run-dir per walk, and a v-prefixed RUNID always sorts to
// the bottom, past the render window. Filtering to the target collapses
// the view onto it (the user's own find-it-fast path) — grid.apply()
// re-applies the filter to every fresh listing, so it survives refreshes.
async function filterTo(text) {
  await page.locator('#filter').fill(text);
  await sleep(250); // the app debounces the filter input 120ms
}
async function clearFilter() { await filterTo(''); }
// The side pane has no filter box; scrolling its body to the bottom pulls
// the last-sorting rows (a v-RUNID dir is always last at a fixture root)
// into the render window.
async function sideScrollBottom() {
  await evalPage(() => {
    const b = document.getElementById('local-grid-body');
    if (b) b.scrollTop = b.scrollHeight;
  });
  await sleep(200);
}
async function rowAction(label, kind) {
  const h = await waitFor(async () => {
    const r = await (kind === 'side' ? sideRow(label) : gridRow(label));
    return (await r.asElement()) ? r : false;
  }, 15000, `row "${label}"`);
  return h.asElement();
}
const clickRow = (l) => rowAction(l, 'grid').then((e) => e.click());
const dblClickRow = (l) => rowAction(l, 'grid').then((e) => e.dblclick());
const rightClickRow = (l) => rowAction(l, 'grid').then((e) => e.click({ button: 'right' }));
const txt = (sel) => evalPage((s) => document.querySelector(s)?.textContent || '', sel);
const toasts = () => evalPage(() => Array.from(document.querySelectorAll('#toasts .toast')).map((t) => t.textContent));
// binding calls straight through the bridge the app itself uses
const call = (method, ...args) => evalPage(([m, a]) => window.go['github.com/MikkoP88/s3-bucket-browser/pkg/api'].App[m](...a), [method, args]);
const verCount = () => call('ObjectVersions', BUCKET, `${PREFIX}/live-a.txt`).then((r) => (r || []).length);

async function shot(name) {
  try { await page.screenshot({ path: path.join(SHOTS, `${name}.png`) }); } catch { /* non-fatal */ }
}
// modal helpers
async function answerPrompt(value) {
  const input = page.locator('#modal-root .modal input.input').last();
  await waitFor(() => input.count().then((n) => n > 0), 5000, 'prompt input');
  await input.fill(value);
  await input.press('Enter');
  await sleep(120);
}
async function clickFooter(re) {
  const h = await elOrNull((src) => {
    const btns = Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'));
    return btns.find((b) => new RegExp(src, 'i').test(b.textContent.trim())) || null;
  }, re.source);
  if (!h) throw new Error(`no footer button /${re.source}/`);
  await h.asElement().click();
  await sleep(120);
}
async function closeModal() {
  const btn = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
    .reverse().find((b) => /^(close|cancel)$/i.test(b.textContent.trim())) || null);
  if (btn) { await btn.asElement().click(); await sleep(100); return; }
  await page.keyboard.press('Escape');
  await sleep(100);
  await evalPage(() => document.getElementById('modal-root')?.classList.add('hidden'));
}
async function modalVisible() {
  return evalPage(() => !!document.querySelector('#modal-root .modal') && !document.getElementById('modal-root').classList.contains('hidden'));
}
async function modalText() {
  return evalPage(() => document.querySelector('#modal-root .modal')?.textContent || '');
}
// Flight recorder for stall triage: every breadcrumb swap, grid row-set
// change and loading-overlay toggle, plus the backend log stream — all
// timestamped in-page. Re-armed at boot and at the engines step (page
// reloads wipe it); dumped from main() when the walk ends with failures.
async function installRecorder() {
  await evalPage(() => {
    window.__recorderOff?.(); // disarm any previous generation first
    window.__logs = [];
    const offLog = window.runtime.EventsOn('log:line', (l) => window.__logs
      .push(`${l.level || '?'}|${l.scope || ''}|${l.source || ''}|${l.message || ''}`));
    window.__obs = [];
    const t0 = performance.now();
    const rec = (what, detail) => window.__obs
      .push(`${Math.round(performance.now() - t0)}ms ${what} ${detail}`);
    const watches = [];
    const watch = (node, opts, fn) => {
      const mo = new MutationObserver(fn);
      mo.observe(node, opts);
      watches.push(mo);
    };
    const bc = document.getElementById('breadcrumb');
    if (bc) watch(bc, { childList: true, subtree: true, characterData: true },
      () => rec('CRUMB', JSON.stringify((bc.textContent || '').trim().slice(0, 70))));
    const gb = document.getElementById('grid-body');
    if (gb) watch(gb, { childList: true },
      (m) => rec('GRID', `mut=${m.length} rows=${gb.querySelectorAll('.grid-row').length}`));
    const sk = document.getElementById('load-skel');
    if (sk) watch(sk, { attributes: true, attributeFilter: ['class'] },
      () => rec('SKEL', sk.classList.contains('hidden') ? 'hidden' : 'SHOWN'));
    window.__recorderOff = () => {
      watches.forEach((mo) => mo.disconnect());
      try { offLog?.(); } catch { /* runtime may be re-initializing */ }
    };
  });
}
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
    const menu = Array.from(document.querySelectorAll('#menubar .mb-title')).find((t) => new RegExp(m, 'i').test(t.textContent));
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
      return (h && await h.asElement()) ? true : false;
    }, timeoutMs, 'transfer dialog');
    await clickFooter(/^start$/i);
    return 'dialog';
  } catch { return 'silent'; }
}
async function bodyCtx() {
  await page.evaluate((el) => {
    const r = el.getBoundingClientRect();
    el.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: r.right - 40, clientY: r.bottom - 30 }));
  }, await bodyH());
  await sleep(80);
}
const portOpen = (port) => new Promise((res) => {
  const s = net.connect({ host: '127.0.0.1', port, timeout: 1500 });
  s.once('connect', () => { s.destroy(); res(true); });
  s.once('error', () => res(false));
  s.once('timeout', () => { s.destroy(); res(false); });
});
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
async function addRemoteSource({ label, type, port, root }) {
  await page.locator('#sidebar-head .side-add').click();
  await waitFor(() => page.locator('#modal-root .modal select').count().then((n) => n > 0), 5000, 'source editor');
  await page.locator('#modal-root .modal select').selectOption(type);
  const inputs = page.locator('#modal-root .modal input.input');
  await waitFor(() => inputs.count().then((n) => n >= 6), 5000, 'remote fields');
  await inputs.nth(0).fill(`live-${label}`);
  await inputs.nth(1).fill('127.0.0.1');
  await inputs.nth(2).fill(String(port));
  await inputs.nth(3).fill(E2E_USER);
  await inputs.nth(4).fill(E2E_PASS);
  await inputs.nth(5).fill(root);
  await clickFooter(/^test$/i);
  await waitFor(async () => /✅|❌/.test(await modalText()), 20000, `test ${label}`);
  await ok(`${label}: source test passed`, (await modalText()).includes('✅'));
  await shot(`r0-${label}-test`);
  await clickFooter(/^save$/i);
  await waitFor(async () => txt('#breadcrumb').then((s) => s.includes(`live-${label}`)), 15000, `${label} root`);
}
async function runDeleteWindow(mode = '') {
  await waitFor(async () => (await evalPage(() => document.querySelectorAll('#modal-root input[name="delmode"]').length)) >= 1, 5000, 'delete window');
  const nModes = await evalPage(() => document.querySelectorAll('#modal-root input[name="delmode"]').length);
  if (nModes > 1) {
    await evalPage((m) => {
      document.querySelector(`#modal-root input[name="delmode"][value="${m}"]`)?.click();
    }, mode);
  }
  const armed = await evalPage(() => {
    const i = document.querySelector('#modal-root .delw-confirm input');
    return !!i && !i.disabled;
  });
  if (armed) await page.locator('#modal-root .delw-confirm input').fill('delete');
  await shot(mode ? `delete-window-${mode}` : 'delete-window-marker');
  await clickFooter(/^delete$/i);
}
async function crumbRoot() {
  await evalPage(() => document.querySelector('#breadcrumb .crumb')?.click());
  await sleep(120);
}
// bucketRoot clicks the BUCKET crumb (the one labeled with the bucket name)
// — crumbRoot clicks the SOURCE crumb, which lands on the bucket LIST
// (ListBuckets), not the bucket's object root. The fault lab asserts on
// bucket-root rows, so it must navigate here.
async function bucketRoot() {
  await evalPage((b) => {
    const c = Array.from(document.querySelectorAll('#breadcrumb .crumb'))
      .find((x) => x.textContent.trim() === b);
    c?.click();
  }, BUCKET);
  await sleep(120);
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

// ---------- server lifecycle ----------
function startServer() {
  return new Promise((resolve, reject) => {
    const proc = spawn(SRV_EXE, [], {
      cwd: ROOT,
      env: {
        ...process.env,
        WAILS_SERVER_HOST: '127.0.0.1',
        WAILS_SERVER_PORT: String(PORT),
        S3B_CONFIG: CFG,
      },
    });
    proc.stdout.on('data', (d) => fs.appendFileSync(SRVLOG, d));
    proc.stderr.on('data', (d) => fs.appendFileSync(SRVLOG, d));
    const t0 = Date.now();
    (function poll() {
      fetch(`http://127.0.0.1:${PORT}/health`, { signal: AbortSignal.timeout(1500) })
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
async function stopServer(srv) {
  if (!srv || srv.proc.exitCode !== null) return;
  const exited = new Promise((r) => srv.proc.on('exit', r));
  srv.proc.kill();
  await Promise.race([exited, sleep(3000)]);
  if (srv.proc.exitCode === null) srv.proc.kill();
}

let page, context, srv;
let consoleTail = [];
let pageErrors = [];

async function launch() {
  const channels = [...new Set([arg('channel'), process.env.S3B_BROWSER_CHANNEL, 'msedge', 'chrome'].filter(Boolean))];
  let lastErr;
  for (const ch of channels) {
    try {
      context = await chromium.launchPersistentContext(USERDIR, { channel: ch, headless: !HEADED });
      return;
    } catch (err) { lastErr = err; }
  }
  throw new Error(`no usable browser (tried channels: ${channels.join(', ')}): ${lastErr?.message}`);
}

import fs from 'node:fs';

async function main() {
  await rm(ART, { recursive: true, force: true });
  await mkdir(SHOTS, { recursive: true });
  await mkdir(FIX, { recursive: true });
  await mkdir(CFG, { recursive: true });
  fs.writeFileSync(SRVLOG, '');
  await writeFile(path.join(FIX, 'live-a.txt'), V1);
  await writeFile(path.join(FIX, 'live-b.txt'), B_TXT);
  for (const [rel, content] of Object.entries(TREE_FILES)) {
    const p = path.join(FIX, 'tree', rel);
    await mkdir(path.dirname(p), { recursive: true });
    await writeFile(p, content);
  }

  srv = await startServer();
  console.log(`s3b v3 server at http://127.0.0.1:${PORT} (config: ${CFG})`);

  // Hermetic bucket root: earlier suites and manual runs leave random-named
  // debris prefixes (x-vm*) and stray files in the walk bucket. The grid
  // virtualizes — with dozens of stale folders the walk's own rows (zz-*
  // sort last, files sort after every folder) land below the fold where
  // the row helpers can no longer see them, and every later step cascades.
  // Purge everything except the seed/ baseline (all versions, no markers).
  {
    const shq = (c) => execFileSync('docker', ['exec', 's3b-e2e-minio', 'sh', '-c', c], { stdio: 'pipe' }).toString();
    shq('mc alias set local http://localhost:9000 minioadmin minioadmin >/dev/null 2>&1 || true');
    for (const line of shq(`mc ls local/${BUCKET}/`).split('\n')) {
      const name = line.trim().split(/\s+/).pop(); // last token (root debris has no spaces)
      if (!name || name === `${SEED}/`) continue;
      shq(`mc rm --recursive --force --versions "local/${BUCKET}/${name}" >/dev/null 2>&1 || true`);
    }
  }

  await launch();
  page = context.pages()[0] || await context.newPage();
  page.on('console', (m) => { consoleTail.push(`[${m.type()}] ${m.text()}`); consoleTail = consoleTail.slice(-60); });
  page.on('pageerror', (e) => { pageErrors.push(e.message); consoleTail.push(`[pageerror] ${e.message}`); consoleTail = consoleTail.slice(-60); });
  page.setDefaultTimeout(20000);

  await walk();

  await shot('final');
  console.log(`\nv3 live walk: ${pass} check(s) passed, ${fail} failed`);
  if (pageErrors.length) {
    console.log(`\nPAGE ERRORS (${pageErrors.length}):`);
    for (const e of pageErrors) console.log(`  ${e}`);
  } else {
    console.log('no page errors across the whole walk');
  }
  if (fail || pageErrors.length) process.exitCode = 1;
}

// ---------- the walk ----------
async function walk() {
  await step('boot: v3 runtime, bridge, bindings, events', async () => {
    await page.goto(`http://127.0.0.1:${PORT}/`);
    await evalPage(() => {
      localStorage.setItem('s3b-lang', 'en');
      localStorage.setItem('s3b-show-markers', '1');
    });
    await page.reload();
    // the setup phase: a fresh rig config walks the first-launch license
    // gate — accept it (a no-op once the record is on file, and the wait
    // doubles as the boot-past-gate barrier for the assertions below)
    await waitFor(async () => await evalPage(() => {
      const b = document.querySelector('.licgate .btn.primary');
      if (b) { b.click(); return false; }
      return !!(document.getElementById('status-version')?.textContent || '').trim();
    }), 15000, 'boot past the license gate');
    // THE layer the desktop webview runs: v3 runtime as ES module, bridge
    // surface, binding call round-trip, backend→frontend event delivery.
    await waitFor(() => evalPage(() => !!window.wails?.Call?.ByName && !!window.wails?.Events?.On), 15000, 'window.wails');
    await ok('v3 runtime loaded (window.wails)', true);
    await waitFor(() => evalPage(() => !!window.go && !!window.runtime), 15000, 'bridge surface');
    await ok('bridge installed window.go + window.runtime', true);
    const ver = await call('GetVersion');
    await ok(`binding round-trip GetVersion → "${ver}"`, ver === VERSION);
    // events: subscribe in the page, emit from the backend through a real
    // transfer-free path — the log:line stream the app itself listens to
    const got = await evalPage(async () => {
      let hit = false;
      window.runtime.EventsOn('v3walk:ping', (v) => { window.__v3ping = v; });
      // backend emits via a binding that logs (any API call logs log:line);
      // for a pure event check use the wails runtime itself
      await window.wails.Events.Emit('v3walk:ping', { n: 42 });
      await new Promise((r) => setTimeout(r, 300));
      return window.__v3ping;
    });
    await ok(`runtime event round-trip (emit→on): ${JSON.stringify(got)}`, got && got.n === 42);
    await waitFor(() => txt('#status-version').then((s) => s.includes(VERSION)), 15000, 'version in status bar');
    await ok('status bar shows the build version', true);
    await installRecorder();
    await waitFor(() => evalPage(() => Array.from(document.querySelectorAll('#empty-actions .btn')).length > 0), 10000, 'onboarding actions');
    await ok('onboarding empty state shown', true);
    await ok('menubar rendered', await evalPage(() => document.querySelectorAll('#menubar .mb-title').length >= 4));
    await shot('01-boot-onboarding');
  });

  await step('menubar: every dropdown opens (screenshot each)', async () => {
    const titles = await evalPage(() => Array.from(document.querySelectorAll('#menubar .mb-title')).map((t) => t.textContent.trim()));
    process.stdout.write(`   menus: [${titles.join(', ')}]\n`);
    for (const t of titles) {
      await page.locator('#menubar .mb-title', { hasText: new RegExp(`^${t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`, 'i') }).first().click();
      await sleep(120);
      const open = await evalPage(() => Array.from(document.querySelectorAll('#menubar .mb-dd')).some((d) => !d.classList.contains('hidden')));
      await ok(`menu "${t}" opens`, open);
      await shot(`02-menu-${t.toLowerCase().replace(/\s+/g, '-')}`);
      await page.keyboard.press('Escape');
      await sleep(80);
    }
  });

  await step('add + test S3 source (MinIO, real dial)', async () => {
    await page.locator('#empty-actions .btn.primary').first().click();
    await waitFor(() => page.locator('#modal-root .modal input.input').count().then((n) => n >= 6), 5000, 'source editor fields');
    const inputs = page.locator('#modal-root .modal input.input');
    await inputs.nth(0).fill(SRCNAME);
    await inputs.nth(1).fill(BUCKET);
    await inputs.nth(2).fill(ENDPOINT);
    await inputs.nth(3).fill(REGION);
    await inputs.nth(4).fill(KEY);
    await inputs.nth(5).fill(SECRET);
    await page.locator('#modal-root .modal input[type="checkbox"]').first().check(); // path-style
    await shot('03-source-editor');
    await clickFooter(/^test$/i);
    await waitFor(async () => /✅|❌/.test(await modalText()), 30000, 'Test result');
    await ok('source test passed against MinIO', (await modalText()).includes('✅'));
    await shot('04-source-test');
    await clickFooter(/^save$/i);
    await waitFor(async () => (await rowKeys()).some((k) => k.startsWith(SEED)), 20000, 'bucket root listing');
    await ok('bucket root listed through the v3 bridge', true);
  });

  // The dialog-width regression guard: a raw backend error is one long line
  // of unbreakable tokens (URLs, host ids). The source editor is width-pinned
  // (.srcw-modal) and its status strip wraps (.dlg-status overflow-wrap) —
  // before those fixes the first error shoved the dialog out to 720px.
  await step('add-source dialog: error keeps pinned width + wraps', async () => {
    await page.locator('#sidebar-head .side-add').click();
    await waitFor(() => page.locator('#modal-root .modal.srcw-modal input.input').count().then((n) => n >= 6), 5000, 'source editor fields');
    await sleep(250); // let the modal-in entry animation finish — getBoundingClientRect
    //                 includes its transform, so measuring mid-animation reads short
    const before = await evalPage(() => document.querySelector('#modal-root .modal.srcw-modal').getBoundingClientRect().width);
    const inputs = page.locator('#modal-root .modal.srcw-modal input.input');
    await inputs.nth(0).fill('dead-endpoint');
    await inputs.nth(1).fill(BUCKET);
    await inputs.nth(2).fill('http://127.0.0.1:9'); // connection refused, fast
    await inputs.nth(3).fill(REGION);
    await inputs.nth(4).fill(KEY);
    await inputs.nth(5).fill(SECRET);
    await clickFooter(/^test$/i);
    await waitFor(async () => /❌/.test(await modalText()), 20000, 'dead-endpoint error on the status strip');
    const after = await evalPage(() => {
      const m = document.querySelector('#modal-root .modal.srcw-modal');
      const s = m.querySelector('.dlg-status');
      return { width: m.getBoundingClientRect().width, cw: s.clientWidth, sw: s.scrollWidth };
    });
    await ok(`width pinned at ${Math.round(after.width)}px before AND after the error (${Math.round(before)}px)`,
      Math.abs(after.width - before) <= 1 && Math.abs(after.width - 560) <= 1);
    await ok('error text wraps inside the status strip (no horizontal overflow)', after.sw <= after.cw + 1);
    await shot('04b-source-error-width');
    await closeModal();
  });

  // Backdrop click closes: the handler used to sit on the dialog box while
  // testing for the backdrop as target — an event that can never bubble
  // that way, so it silently never fired.
  await step('modal: backdrop mousedown closes', async () => {
    await page.locator('#sidebar-head .side-add').click();
    await waitFor(() => evalPage(() => !!document.querySelector('#modal-root .modal.srcw-modal')), 5000, 'modal open');
    const pt = await evalPage(() => {
      const r = document.querySelector('#modal-root .modal').getBoundingClientRect();
      return { x: Math.round(r.left / 2), y: Math.round(r.top + 10) }; // on the backdrop, beside the dialog
    });
    await page.mouse.move(pt.x, pt.y);
    await page.mouse.down();
    await page.mouse.up();
    await sleep(150);
    await ok('backdrop press closed the modal', await evalPage(() => !document.querySelector('#modal-root .modal')));
  });

  await step('session-only status chip', async () => {
    await waitFor(() => txt('#status-pfile').then((s) => /unsaved source/i.test(s)), 5000, 'unsaved-source chip');
    await ok(`status bar: "${(await txt('#status-pfile')).trim()}"`, true);
  });

  await step('browse: rows, tree guards, breadcrumb', async () => {
    await waitFor(async () => (await rowKeys()).some((k) => k.startsWith(SEED)), 20000, 'bucket contents');
    await ok(`existing data visible (${SEED}/)`, true);
    await waitFor(async () => (await evalPage(() => document.querySelectorAll('#tree .tguard').length)) > 0, 10000, 'tree guard icons');
    const icons = await evalPage(() => Array.from(document.querySelectorAll('#tree .tguard')).map((c) => c.title));
    await ok(`tree guard icons: [${icons.join(' | ')}]`, icons.length > 0);
    await shot('05-objects');
  });

  await step('filter box filters rows live', async () => {
    await page.locator('#filter').fill('see'); // matches seed/
    await sleep(250);
    const filtered = await evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
      .filter((r) => r.style.display !== 'none').length);
    await ok(`filter narrowed the view (${filtered} row(s))`, filtered >= 1);
    await shot('06-filter');
    await page.locator('#filter').fill('');
    await sleep(150);
  });

  await step('context menu on a row (all items)', async () => {
    await clickRow(SEED);
    await rightClickRow(SEED);
    await sleep(120);
    const items = await evalPage(() => Array.from(document.querySelectorAll('#ctxmenu:not(.hidden) .item')).map((i) => i.textContent.trim()));
    process.stdout.write(`   items: [${items.join(' | ')}]\n`);
    await ok('row context menu has items', items.length >= 5);
    await shot('07-ctxmenu');
    await evalPage(() => document.getElementById('ctxmenu')?.classList.add('hidden'));
    // copy-path goes through the real ClipSetText binding — a no-op impl in
    // server builds, so assert the menu worked, not the OS clipboard
    await rightClickRow(SEED);
    await sleep(80);
    await ctxItem(/^copy path$/i);
    await sleep(300);
    process.stdout.write(`   copy-path toasts: ${JSON.stringify(await toasts()).slice(0, 160)}\n`);
    await ok('copy path clicked without killing the page', true);
  });

  await step('admin dialog (real bucket info + versioning status)', async () => {
    await page.locator('#tree .tguard').first().click();
    await waitFor(async () => /versioning/i.test(await modalText()), 10000, 'admin overview');
    const tabs = await evalPage(() => Array.from(document.querySelectorAll('#modal-root .tab')).map((t) => t.textContent));
    await ok(`admin dialog opens (${tabs.length} tab(s): ${tabs.slice(0, 4).join(',')}…)`, await modalVisible());
    await ok('admin dialog titled Admin panel', (await modalText()).includes('Admin panel'));
    await ok(`versioning reported: ${(await modalText().then((t) => t.match(/Versioning\s*(Enabled|Suspended|off[^]*)?/i) || [])[0] || '')}`, /enabled/i.test(await modalText()));
    // screenshot a second tab too (Security) for coverage
    const tab = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .tab')).find((t) => /security/i.test(t.textContent)) || null);
    if (tab) { await tab.asElement().click(); await sleep(400); await shot('08b-admin-security'); }
    await shot('08-admin');
    await closeModal();
  });

  await step('doctor (real checks over the bridge)', async () => {
    await menuClick(/help/i, /doctor/i);
    // Help → Doctor opens the source picker first (S3 sources only;
    // right-click Doctor… on a bucket still bypasses it)
    await waitFor(() => evalPage(() => document.querySelectorAll('#modal-root .picker-row').length > 0), 5000, 'doctor picker');
    const names = await evalPage(() => Array.from(document.querySelectorAll('#modal-root .picker-row .picker-name')).map((n) => n.textContent));
    await ok(`picker lists S3 sources (${names.join(', ')})`, names.includes(SRCNAME));
    await shot('09-doctor-picker');
    const row = await elOrNull((want) => Array.from(document.querySelectorAll('#modal-root .picker-row'))
      .find((r) => r.textContent.includes(want)) || null, SRCNAME);
    await row.asElement().click();
    // doctor floats as an in-page popout (#popout-root), not a modal
    await waitFor(() => evalPage(() => {
      const p = document.querySelector('#popout-root .popout');
      return p ? p.textContent : '';
    }).then((s) => /run all/i.test(s)), 10000, 'doctor popout');
    await shot('09-doctor-initial');
    const runAllBtn = await elOrNull(() => Array.from(document.querySelectorAll('#popout-root .popout .modal-foot .btn'))
      .find((b) => /run all/i.test(b.textContent.trim())) || null);
    await runAllBtn.asElement().click();
    await waitFor(async () => /pass/i.test(await evalPage(() => document.querySelector('#popout-root .doc-summary')?.textContent || '')), 60000, 'doctor summary');
    const summary = await evalPage(() => document.querySelector('#popout-root .doc-summary')?.textContent || '');
    await ok(`doctor summary: "${summary.trim()}"`, /pass/i.test(summary));
    // the run itself registered as a tracked task: Running tasks carries
    // the finished doctor row (the everything-monitor sees doctor runs)
    const doc = (await call('RunningTasks')).find((t) => t.kind === 'doctor');
    await ok('doctor run registered as a Running-tasks row', !!doc && doc.status === 'done' && /all checks/.test(doc.label));
    await shot('10-doctor-run');
    await dismissUI();
  });

  await step('help popouts render in-page (server flag gates native off)', async () => {
    // F1 keyboard shortcuts
    await page.keyboard.press('F1');
    await waitFor(() => evalPage(() => document.querySelectorAll('#popout-root .popout').length > 0), 5000, 'keyboard popout');
    await ok('keyboard shortcuts popout (in-page)', true);
    await shot('11-keys-popout');
    await evalPage(() => Array.from(document.querySelectorAll('#popout-root .popout .modal-head .x')).forEach((x) => x.click()));
    // user guide
    await menuClick(/help/i, /guide|usage/i);
    await waitFor(() => evalPage(() => document.querySelectorAll('#popout-root .popout').length > 0), 5000, 'guide popout');
    await ok('user guide popout (in-page)', true);
    await shot('12-guide-popout');
    await evalPage(() => Array.from(document.querySelectorAll('#popout-root .popout .modal-head .x')).forEach((x) => x.click()));
    // supported data sources
    await menuClick(/help/i, /sources|data sources/i);
    await waitFor(() => evalPage(() => document.querySelectorAll('#popout-root .popout').length > 0), 5000, 'sources popout');
    await ok('supported data sources popout (in-page)', true);
    await shot('13-sources-popout');
    await evalPage(() => Array.from(document.querySelectorAll('#popout-root .popout .modal-head .x')).forEach((x) => x.click()));
    // license window (single-source identity from license.js)
    await menuClick(/help/i, /license/i);
    await waitFor(() => evalPage(() => document.querySelectorAll('#popout-root .popout').length > 0), 5000, 'license popout');
    await ok('license popout shows the PolyForm identity', (await evalPage(() => document.querySelector('#popout-root .popout').textContent)).includes('PolyForm Internal Use License'));
    await shot('13b-license-popout');
    await evalPage(() => Array.from(document.querySelectorAll('#popout-root .popout .modal-head .x')).forEach((x) => x.click()));
  });

  await step('transfer manager (in-page popout)', async () => {
    await menuClick(/view/i, /transfers/i);
    await waitFor(() => evalPage(() => document.querySelectorAll('#popout-root .popout').length > 0), 5000, 'transfer popout');
    await ok('transfer manager opens as a popout', true);
    await shot('14-transfers');
    await evalPage(() => Array.from(document.querySelectorAll('#popout-root .popout .modal-head .x')).forEach((x) => x.click()));
  });

  await step('dual pane: bind local side to fixtures', async () => {
    await page.keyboard.press('F9');
    await waitFor(() => evalPage(() => !document.getElementById('local-pane').classList.contains('hidden')), 5000, 'local pane');
    await page.locator('#local-crumb').click();
    await answerPrompt(FIX);
    await waitFor(async () => (await sideKeys()).includes('live-a.txt'), 10000, 'fixture rows');
    await ok('local rows visible', true);
    await shot('15-dualpane');
  });

  await step('New folder via empty-area context menu', async () => {
    await bodyCtx();
    await ctxItem(/new folder/i);
    await shot('16-newfolder-prompt');
    await answerPrompt(PREFIX);
    await waitFor(async () => (await rowKeys()).includes(`${PREFIX}/`), 20000, 'zz-v3 folder');
    await ok(`folder ${PREFIX}/ created`, true);
    await dblClickRow(`${PREFIX}/`);
    await waitFor(async () => (await rowKeys()).length === 0, 10000, 'empty folder view');
  });

  await step('New file: dialog + real CreateFile (no editor side effects)', async () => {
    // The dialog UI is exercised with Cancel only: submitting would run the
    // real EditObject handoff and pop the OS "Open with" picker on the test
    // machine. The creation path itself goes through the real bridge.
    await page.keyboard.press('Shift+F4');
    await waitFor(() => evalPage(() => !!document.querySelector('#modal-root input.input')), 4000, 'new-file prompt');
    await shot('16b-newfile-prompt');
    await ok('prompt defaults + live preview', evalPage(() => {
      const m = document.getElementById('modal-root');
      const input = m.querySelector('input.input');
      const sel = m.querySelector('select');
      return input.value === 'new-file' && sel.value === 'txt'
        && /Creates:\s*new-file\.txt/.test(m.textContent);
    }));
    await evalPage(() => {
      const b = Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
        .find((x) => /cancel/i.test(x.textContent));
      b?.click();
    });
    await sleep(150);
    await ok('dialog cancel creates nothing', await evalPage(() => !document.querySelector('#modal-root .modal')));
    // direct binding: the real PutObject path, name+ext composed server-side
    const key = await call('CreateFile', BUCKET, `${PREFIX}/`, 'live-note', 'md');
    await ok(`CreateFile returned the composed key (${key})`, key === `${PREFIX}/live-note.md`);
    const st1 = await call('StatObject', BUCKET, key);
    await ok(`object exists on the server, size 0 (etag ${st1?.etag})`, !!st1 && st1.size === 0);
    // dedup: a name already carrying the extension composes to the same key
    const key2 = await call('CreateFile', BUCKET, `${PREFIX}/`, 'live-note.md', 'md');
    await ok('extension dedup composes the same key', key2 === key);
    // the refresh lists the created file
    await page.keyboard.press('F5');
    await waitFor(async () => (await rowKeys()).includes(key), 20000, 'new file row');
    await ok('created file lists in the grid', true);
    await shot('16c-newfile-grid');
  });

  await step('DnD upload through the bridge (local row → grid body)', async () => {
    const upload = async (file) => {
      await dnd(await sideRow(file), await bodyH());
      const how = await startIfAsked(8000);
      await waitFor(async () => (await names()).includes(file), 60000, `uploaded row ${file}`);
      return how;
    };
    const a = await upload('live-a.txt');
    await ok(`live-a.txt uploaded (${a})`, true);
    const b = await upload('live-b.txt');
    await ok(`live-b.txt uploaded (${b})`, true);
    await shot('17-uploaded');
  });

  await step('versions baseline + versions dialog', async () => {
    const base = await verCount();
    await ok(`baseline: ${base} version(s) of ${PREFIX}/live-a.txt`, base >= 1);
    await clickRow('live-a.txt');
    await rightClickRow('live-a.txt');
    await ctxItem(/^versions/i);
    await waitFor(() => page.locator('#modal-root .ver-row').count().then((n) => n >= base), 15000, 'version rows');
    await ok(`versions dialog shows ${await page.locator('#modal-root .ver-row').count()} row(s)`, true);
    await shot('18-versions');
    await closeModal();
    return base;
  }).then((base) => { walk.base = base; });

  await step('overwrite via conflict dialog → 2 versions → A/B diff', async () => {
    const base = walk.base;
    await writeFile(path.join(FIX, 'live-a.txt'), V2);
    await dnd(await sideRow('live-a.txt'), await bodyH());
    await waitFor(async () => /already exist/i.test(await modalText()), 15000, 'conflict dialog');
    await ok('conflict policy dialog shown', true);
    await shot('19-conflict');
    await clickFooter(/^start$/i);
    await waitFor(async () => (await verCount()) === base + 1, 60000, 'second version landed');
    await ok('overwrite created a new version', true);
    await clickRow('live-a.txt');
    await rightClickRow('live-a.txt');
    await ctxItem(/^versions/i);
    await waitFor(async () => page.locator('#modal-root .ver-row').count().then((n) => n >= base + 1), 20000, 'two versions');
    await ok(`now ${await page.locator('#modal-root .ver-row').count()} versions`, true);
    await shot('20-versions-two');
    await page.locator('#modal-root .ver-row').first().locator('.ver-pick', { hasText: 'A' }).click();
    await sleep(80);
    await page.locator('#modal-root .ver-row').last().locator('.ver-pick', { hasText: 'B' }).click();
    await sleep(120);
    await page.locator('#modal-root button', { hasText: 'Compare A' }).click();
    await waitFor(async () => {
      const d = await modalText();
      return d.includes('v1') && d.includes('v2');
    }, 20000, 'A/B diff contents');
    await ok('A/B diff shows both contents', true);
    await shot('21-versiondiff');
    await closeModal();
  });

  await step('DnD download (S3 row → local pane, bytes verified)', async () => {
    const dst = path.join(FIX, 'downloads');
    await mkdir(dst, { recursive: true });
    await page.locator('#local-crumb').click();
    await answerPrompt(dst);
    await waitFor(async () => (await sideKeys()).length === 0, 10000, 'empty downloads dir');
    await dnd(await rowAction('live-b.txt', 'grid'), await sideBodyH());
    await startIfAsked(8000);
    await waitFor(async () => (await sideKeys()).includes('live-b.txt'), 60000, 'downloaded row');
    const bytes = await readFile(path.join(dst, 'live-b.txt'), 'utf8');
    await ok('downloaded bytes identical', bytes === B_TXT);
    await shot('22-downloaded');
  });

  await step('rename (F2)', async () => {
    await page.locator('#local-crumb').click();
    await answerPrompt(FIX);
    await waitFor(async () => (await sideKeys()).includes('tree'), 10000, 'local side reset');
    await clickRow('live-b.txt');
    await page.keyboard.press('F2');
    await shot('23-rename-prompt');
    await answerPrompt('live-renamed.txt');
    await waitFor(async () => (await names()).includes('live-renamed.txt'), 20000, 'renamed row');
    await ok('renamed to live-renamed.txt', true);
  });

  await step('deep Find (Ctrl+Shift+F)', async () => {
    await page.keyboard.press('Control+Shift+F');
    await waitFor(() => modalVisible(), 5000, 'find dialog');
    await ok('find dialog opens', true);
    await shot('24-find');
    const input = page.locator('#modal-root .modal input.input').first();
    if (await input.count()) {
      await input.fill('live-a');
      const btn = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
        .find((b) => /^(find|search|start)$/i.test(b.textContent.trim())) || null);
      if (btn) { await btn.asElement().click(); await sleep(1500); }
      await shot('25-find-results');
    }
  });

  await step('remote engines: add + test + structure-fidelity round-trip', async () => {
    const engines = [];
    for (const e of ENGINES) if (await portOpen(e.port)) engines.push(e);
    if (!engines.length) {
      console.log('   (no local e2e containers reachable — remote engine checks skipped)');
      await ok('remote engine section skipped (no containers up)', true);
      return;
    }
    await installRecorder(); // fresh trace: this step's waits are where stalls show up
    walk.engines = engines;
    for (const e of engines) {
      await treeOpen(SRCNAME);
      await waitFor(async () => (await rowKeys()).some((k) => k.startsWith(SEED)), 15000, 'back on s3 source');
      await addRemoteSource(e);
      await shot(`r1-${e.label}-root`);
      await bodyCtx();
      await ctxItem(/new folder/i);
      await answerPrompt(RUNID);
      await filterTo(RUNID); // virtualized grid: filter renders the fresh dir
      await waitFor(async () => (await names()).includes(RUNID), 20000, `${e.label}: ${RUNID} dir`);
      await dblClickRow(RUNID); // still filtered — the single row is rendered
      await waitFor(async () => (await rowKeys()).length === 0, 10000, `${e.label}: empty run dir`);
      await clearFilter();
      await page.locator('#local-crumb').click();
      await answerPrompt(FIX);
      await waitFor(async () => (await sideKeys()).includes('tree'), 10000, 'fixture tree on the local side');
      await dnd(await sideRow('tree'), await bodyH());
      await startIfAsked(8000);
      await waitFor(async () => (await names()).includes('tree'), 90000, `${e.label}: tree uploaded`);
      await ok(`${e.label}: nested tree uploaded via folder-row DnD`, true);
      await dblClickRow('tree');
      await waitFor(async () => {
        const n = await names();
        return ['docs', 'logs', 'readme.md', 'root-1.txt'].every((x) => n.includes(x));
      }, 20000, `${e.label}: tree root`);
      await ok(`${e.label}: root files + dirs intact`, true);
      await shot(`r2-${e.label}-tree`);
      await dblClickRow('docs');
      await waitFor(async () => {
        const n = await names();
        return ['a.md', 'b with space.txt', 'uni-åäö.txt', 'nested', 'empty.txt'].every((x) => n.includes(x));
      }, 20000, `${e.label}: docs/`);
      await ok(`${e.label}: space, unicode, empty file and nested dir intact`, true);
      await crumbRoot();
      await filterTo(RUNID);
      await waitFor(async () => (await names()).includes(RUNID), 10000, `${e.label}: back at root`);
      await dblClickRow(RUNID);
      await clearFilter();
      await waitFor(async () => (await names()).includes('tree'), 10000, `${e.label}: run dir`);
      const dst = path.join(FIX, `rt-${e.label}`);
      await mkdir(dst, { recursive: true });
      await page.locator('#local-crumb').click();
      await answerPrompt(dst);
      await waitFor(async () => (await sideKeys()).length === 0, 10000, `${e.label}: empty local dst`);
      await dnd(await rowAction('tree', 'grid'), await sideBodyH());
      await startIfAsked(8000);
      await waitFor(async () => (await sideKeys()).includes('tree'), 90000, `${e.label}: tree downloaded`);
      const a = await treeOf(path.join(dst, 'tree'));
      const b = await treeOf(path.join(FIX, 'tree'));
      await ok(`${e.label}: round-trip byte-identical (${a.length} files)`, JSON.stringify(a) === JSON.stringify(b));
      await shot(`r3-${e.label}-roundtrip`);
      await crumbRoot();
      await filterTo(RUNID);
      await waitFor(async () => (await names()).includes(RUNID), 10000, `${e.label}: root again`);
      await clearFilter();
    }
    if (engines.length >= 2) {
      const from = engines[0];
      await treeOpen(SRCNAME);
      await bodyCtx();
      await ctxItem(/new folder/i);
      await answerPrompt(`x-${RUNID}`);
      await filterTo(`x-${RUNID}`);
      await waitFor(async () => (await names()).includes(`x-${RUNID}`), 20000, 'cross-engine dir');
      await dblClickRow(`x-${RUNID}`);
      await waitFor(async () => (await rowKeys()).length === 0, 10000, 'empty cross dir');
      await clearFilter();
      await page.locator('#local-src').selectOption({ label: `live-${from.label} (${from.type})` });
      // virtualized side pane: RUNID sorts last at the fixture root, and the
      // listing may still be in flight — keep scrolling to the bottom inside
      // the wait so the row renders once the real canvas height exists
      await waitFor(async () => {
        await sideScrollBottom();
        return (await sideKeys()).includes(RUNID);
      }, 15000, `${from.label} on the side`);
      const runDir = await rowAction(RUNID, 'side');
      await runDir.dblclick();
      await waitFor(async () => (await sideKeys()).includes('tree'), 15000, `${from.label}: run dir on side`);
      await dnd(await sideRow('tree'), await bodyH());
      await startIfAsked(8000);
      await waitFor(async () => (await names()).includes('tree'), 90000, 'cross-engine tree landed');
      await dblClickRow('tree');
      await waitFor(async () => (await names()).includes('docs'), 20000, 'cross-engine docs/');
      await ok(`${from.label} → last engine: cross-engine DnD keeps the tree`, true);
      await shot('r4-cross-engine');
      await page.locator('#local-src').selectOption('local');
      await page.locator('#local-crumb').click();
      await answerPrompt(FIX);
      await waitFor(async () => (await sideKeys()).includes('tree'), 10000, 'local side restored');
    }
  });

  await step('i18n: switch language, verify UI strings swap', async () => {
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.startsWith(SEED)), 15000, 'back on s3');
    await menuClick(/^settings$/i, /settings/i);
    await waitFor(() => page.locator('#modal-root .set-page select').count().then((n) => n > 0), 5000, 'settings dialog');
    // The language select carries language-code options; the theme select
    // next to it only has light/dark — target by the 'fi' option value.
    const langSel = page.locator('#modal-root .set-page select').filter({ has: page.locator('option[value="fi"]') });
    await waitFor(() => langSel.count().then((n) => n === 1), 5000, 'language select');
    const opts = await langSel.first().evaluate((s) => Array.from(s.options).map((o) => o.value));
    process.stdout.write(`   language options: [${opts.join(', ')}]\n`);
    await ok(`language picker present (${opts.length} options incl. auto)`, opts.includes('auto') && opts.includes('fi'));
    // onchange persists the choice and reloads (setLanguage); the reload
    // can race the select action itself, so a throw here is still success.
    await langSel.first().selectOption('fi').catch(() => {});
    await waitFor(() => evalPage(() => localStorage.getItem('s3b-lang') === 'fi'
      && Array.from(document.querySelectorAll('#menubar .mb-title')).some((t) => /Asetukset/.test(t.textContent))), 20000, 'Finnish UI after reload');
    await ok('language switched to fi (menubar shows Asetukset)', true);
    await shot('26-lang-fi');
    // restore English through the same persisted choice the app writes
    await evalPage(() => localStorage.setItem('s3b-lang', 'en'));
    await page.reload();
    await waitFor(() => evalPage(() => Array.from(document.querySelectorAll('#menubar .mb-title')).some((t) => /^Settings$/.test(t.textContent.trim()))), 20000, 'English UI after reload');
    await ok('switched back to English', true);
  });

  await step('settings: theme dark/light applies + persists', async () => {
    // settings dialog may still be open from the language step
    if (!(await modalVisible())) {
      await menuClick(/^settings$/i, /settings/i);
      await waitFor(() => page.locator('#modal-root .set-page select').count().then((n) => n > 0), 5000, 'settings dialog');
    }
    // find the theme select across all pages: it holds 'dark'
    const themeSel = page.locator('#modal-root .set-page select').filter({ has: page.locator('option[value="dark"]') }).first();
    const isTheme = await themeSel.evaluate((s) => Array.from(s.options).some((o) => o.value === 'dark'));
    const rows = await evalPage(() => document.querySelectorAll('#modal-root .set-row').length);
    await ok(`settings dialog renders (${rows} rows)`, rows >= 3);
    if (isTheme) {
      await themeSel.selectOption('dark');
      await sleep(150);
      await ok('dark theme applied', await evalPage(() => document.documentElement.dataset.theme === 'dark'));
      await shot('27-settings-dark');
      await themeSel.selectOption('light');
      await sleep(150);
      await ok('light theme applied', await evalPage(() => (document.documentElement.dataset.theme || 'light') === 'light'));
    }
    // engine tuning: Network page → listing timeout 10 s; File transfers →
    // Transfer-engine group → part size 8 MiB. Both ride SetTuning and the
    // stored truth comes back — and the blackhole step later proves the
    // 10 s watchdog this sets is honored live.
    await page.locator('#modal-root .set-nav-item').filter({ hasText: 'Network' }).click();
    await page.locator('#modal-root .set-row').filter({ hasText: 'Listing timeout' }).locator('select').selectOption('10000');
    await waitFor(async () => (await call('GetTuning')).listingTimeoutMs === 10000, 5000, 'GetTuning 10 s');
    await ok('listing timeout set to 10 s (round-trips the binding)', true);
    await page.locator('#modal-root .set-nav-item').filter({ hasText: 'File transfers' }).click();
    await page.locator('#modal-root .set-row').filter({ hasText: 'Multipart part size' }).locator('select').selectOption('8');
    await waitFor(async () => (await call('GetTuning')).partSizeMiB === 8, 5000, 'GetTuning 8 MiB');
    await ok('multipart part size set to 8 MiB (engine group round-trips)', true);
    await shot('28-settings');
    await closeModal();
  });

  await step('log area (Ctrl+L) shows real backend lines', async () => {
    await page.keyboard.press('Control+L');
    await waitFor(() => evalPage(() => !document.getElementById('logarea').classList.contains('hidden')), 5000, 'logarea');
    const lines = await evalPage(() => Array.from(document.querySelectorAll('#logarea .log-line, #logarea .line, #logarea div')).slice(-30).map((l) => l.textContent));
    await ok(`log shows ${lines.length} line(s)`, lines.length > 0);
    // the SetTuning calls from the settings step each wrote a
    // scope="settings" line
    await ok('engine tuning change logged', lines.some((l) => /engine tuning/i.test(l)));
    await shot('29-log');
    await page.keyboard.press('Control+L');
  });

  await step('profile round-trip through the real bindings', async () => {
    // server builds have no OS dialogs; drive the same bindings the UI's
    // save/close/open flows call. The UI chip/tree update only in those UI
    // flows (no event for them), so assert backend truth via
    // GetProfileFileState and the re-booted UI after page.reload()s.
    await call('SaveProfileFileAs', PROFILE, PASSWORD);
    const st = await call('GetProfileFileState');
    // the profile NAME is the filename minus the .s3bprofile extension
    await ok(`saved: open=${st.open} name="${st.name}" sources=${st.sourceCount}`,
      !!st.open && String(st.name) === 'walk' && st.sourceCount >= 1);
    const bytes = (await readFile(PROFILE)).length;
    await ok(`profile file written (${bytes} bytes)`, bytes > 0);
    await page.reload();
    await waitFor(() => txt('#status-pfile').then((s) => s.includes(LOCK)), 15000, 'profile chip after reload');
    await ok('chip shows the profile lock after reload', true);
    await shot('30-profile-saved');
    await call('CloseProfileFile', true);
    await page.reload();
    await waitFor(() => evalPage(() => Array.from(document.querySelectorAll('#empty-actions .btn')).length > 0), 15000, 'onboarding after close');
    await ok('profile closed → sources gone → onboarding', true);
    await shot('31-profile-closed');
    // a failed open must REJECT through the bridge (the UI shows the toast)
    let err = '';
    try { await call('OpenProfileFile', PROFILE, 'definitely-wrong'); } catch (e) { err = String(e && e.message || e); }
    await ok(`wrong password rejected (${err})`, /password|decrypt|corrupt/i.test(err));
    await call('OpenProfileFile', PROFILE, PASSWORD);
    await page.reload();
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.startsWith(SEED)), 20000, 'bucket root after reopen');
    await ok('profile reopened, sources restored', true);
    await shot('32-profile-reopened');
  });

  await step('server restart: session sources vanish, settings survive', async () => {
    await stopServer(srv);
    srv = await startServer();
    await page.goto(`http://127.0.0.1:${PORT}/`);
    await waitFor(() => evalPage(() => Array.from(document.querySelectorAll('#empty-actions .btn')).length > 0), 15000, 'onboarding after restart');
    await ok('fresh backend → no sources (strict session model)', true);
    // appsettings.json survives the process restart — the tuning set in
    // the settings step is still the stored truth
    const tun = await call('GetTuning');
    await ok(`engine tuning survived the restart (listing ${tun.listingTimeoutMs} ms, parts ${tun.partSizeMiB} MiB)`,
      tun.listingTimeoutMs === 10000 && tun.partSizeMiB === 8);
    await shot('33-after-restart');
  });

  await step('reopen + Delete Window cleanup (marker → badge → permanent)', async () => {
    await call('OpenProfileFile', PROFILE, PASSWORD);
    await page.reload();
    await treeOpen(SRCNAME);
    await waitFor(async () => (await rowKeys()).some((k) => k.startsWith(SEED)), 20000, 'bucket root');
    await dblClickRow(`${PREFIX}/`);
    await waitFor(async () => (await rowKeys()).length >= 2, 20000, 'zz-v3 contents');
    await page.keyboard.press('Control+A');
    await page.keyboard.press('Delete');
    await runDeleteWindow('');
    await ok('versioned Delete Window: marker default, one click through', true);
    await waitFor(async () => (await rowKeys()).length === 0, 60000, 'folder emptied');
    await ok('zz-v3 objects marker-deleted', true);
    await shot('34-deleted');
    await page.keyboard.press('Backspace');
    await waitFor(async () => (await rowKeys()).includes(`${PREFIX}/`), 20000, 'bucket root');
    const badge = await waitFor(() => evalPage((k) => {
      const r = Array.from(document.querySelectorAll('#grid-body .grid-row'))
        .find((x) => x._model && x._model.key === k);
      return r ? (r.querySelector('.mbadge')?.textContent || '').trim() : '';
    }, `${PREFIX}/`), 15000, 'marker-count badge');
    await ok(`folder badge shows "${badge}"`, /⛔/.test(badge));
    await shot('35-marker-badge');
    await clickRow(`${PREFIX}/`);
    await page.keyboard.press('Delete');
    await runDeleteWindow('permanent');
    await waitFor(async () => !(await rowKeys()).includes(`${PREFIX}/`), 60000, 'folder row gone for good');
    await ok('permanent purge removed the folder row entirely', true);
    await waitFor(async () => (await rowKeys()).some((k) => k.startsWith(SEED)), 20000, 'bucket root relisted');
    const left = await rowKeys();
    await ok(`bucket root back to pre-existing data [${left.join(', ')}]`, left.some((k) => k.startsWith(SEED)) && !left.some((k) => k.startsWith(PREFIX) && !k.endsWith('/')));
    await shot('36-cleaned');
  });

  // ---------- slow & broken connections (scripts/faultproxy.mjs) ----------
  // A second S3 source points at a TCP fault-injection proxy in front of the
  // SAME MinIO. The proxy's HTTP control plane lets one long-lived process
  // serve the whole scenario list (direct → latency → throttle → reset →
  // blackhole) while the SDK's keep-alive pool stays connected to it — the
  // real-world "the connection went bad mid-session" case, not a fresh dial
  // per scenario. zz-slowwalk/ is seeded with 120 objects so throttled
  // listings take visible, assertable seconds.
  {
    const FPORT = 19000, FCTL = 19001;
    const FSRC = 'minio-fault', SLOW = 'zz-slowwalk', SLOWN = 120;
    const sh = (c) => execFileSync('docker', ['exec', 's3b-e2e-minio', 'sh', '-c', c], { stdio: 'pipe' }).toString();
    const ctl = (p, opts) => fetch(`http://127.0.0.1:${FCTL}${p}`, { signal: AbortSignal.timeout(3000), ...opts })
      .then((r) => r.json());
    const setMode = (patch) => ctl('/mode', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(patch),
    });
    // the file panel's in-flight contract: empty-state visible, flagged
    // is-loading, skeleton rows underneath — never a blank panel
    const inFlight = () => evalPage(() => {
      const e = document.getElementById('empty-state');
      return !!e && !e.classList.contains('hidden') && e.classList.contains('is-loading')
        && !document.getElementById('load-skel').classList.contains('hidden');
    });
    // the failure contract: settled error page (not loading) with a
    // classified title, the raw message and a Retry action
    const errState = () => evalPage(() => {
      const e = document.getElementById('empty-state');
      if (!e || e.classList.contains('hidden') || e.classList.contains('is-loading')) return null;
      return {
        title: document.getElementById('empty-title').textContent,
        sub: document.getElementById('empty-sub').textContent,
        retry: Array.from(document.querySelectorAll('#empty-actions .btn'))
          .some((b) => b.textContent.includes('Retry')),
      };
    });
    const clickRetry = () => evalPage(() => {
      Array.from(document.querySelectorAll('#empty-actions .btn'))
        .find((b) => b.textContent.includes('Retry'))?.click();
    });
    // logical item count from the status bar — the grid virtualizes (only
    // the visible slice + overscan is in the DOM), so a 120-row folder can
    // never show 115 DOM rows however complete the listing is
    const totalItems = () => evalPage(() => {
      const m = (document.getElementById('status-selection')?.textContent || '').match(/^(\d+)/);
      return m ? Number(m[1]) : 0;
    });
    // a crashed earlier run can leave a stale proxy owning the ports (still
    // stuck in its last fault mode) — clear them before listening
    const clearFaultPorts = () => {
      try {
        execFileSync('bash', [path.join(ROOT, 'scripts', 'kill-faultports.sh'), String(FPORT), String(FCTL)], { stdio: 'ignore' });
      } catch { /* best effort */ }
    };
    clearFaultPorts();
    const fx = spawn(process.execPath, [path.join(ROOT, 'scripts', 'faultproxy.mjs'),
      '--listen', String(FPORT), '--control', String(FCTL), '--target', '127.0.0.1:9000']);
    fx.stdout.on('data', (d) => fs.appendFileSync(SRVLOG, `[faultproxy] ${d}`));
    fx.stderr.on('data', (d) => fs.appendFileSync(SRVLOG, `[faultproxy!] ${d}`));
    try {
      await waitFor(async () => (await ctl('/state')).mode === 'direct', 10000, 'faultproxy control plane');

      await step('fault lab: 120-object prefix + source behind the proxy (direct control)', async () => {
        sh('mc alias set local http://localhost:9000 minioadmin minioadmin >/dev/null 2>&1 || true');
        sh(`rm -rf /tmp/slowseed; mkdir -p /tmp/slowseed; i=0; while [ "$i" -lt ${SLOWN} ]; do echo "slowwalk payload $i" > /tmp/slowseed/f-$(printf '%03d' "$i").txt; i=$((i+1)); done`);
        sh(`mc rm --recursive --force local/${BUCKET}/${SLOW}/ >/dev/null 2>&1 || true`);
        sh(`mc cp --recursive /tmp/slowseed/ local/${BUCKET}/${SLOW}/ >/dev/null`);
        const seeded = sh(`mc ls --recursive local/${BUCKET}/${SLOW}/ | wc -l`).trim();
        await ok(`seeded ${BUCKET}/${SLOW}/ with ${seeded} objects`, Number(seeded) === SLOWN);
        await call('SaveSource', {
          name: FSRC, type: 's3', bucket: BUCKET,
          s3: { name: FSRC, endpoint: `http://127.0.0.1:${FPORT}`, region: REGION, accessKeyId: KEY, secretKey: SECRET, pathStyle: true },
        });
        await page.reload();
        await treeOpen(FSRC);
        await waitFor(async () => (await rowKeys()).some((k) => k.startsWith(SEED)), 20000, 'listing through the proxy');
        await ok('control: bucket root lists through the proxy (direct mode)', true);
        const st = await ctl('/state');
        await ok(`proxy carried the listing (connections=${st.connections})`, st.connections >= 1);
        await shot('37-fault-direct');
      });

      await step('latency +250ms per chunk: skeleton during listing, rows land whole', async () => {
        // delay per TCP chunk × ~25 response chunks must stay comfortably
        // under the 30s stream watchdog — at 900ms the 112KB listing hit
        // ~31s and was (correctly) killed as a dead-slow source.
        await setMode({ mode: 'latency', delayMs: 250 });
        await dblClickRow(`${SLOW}/`);
        await waitFor(inFlight, 6000, 'in-flight skeleton');
        await ok('spinner + skeleton rows while chunks crawl in', true);
        await shot('38-fault-latency-skeleton');
        await waitFor(async () => (await totalItems()) >= SLOWN - 5, 20000, 'slowwalk rows under latency');
        await ok(`latency: listing completed anyway (${await totalItems()} items)`, true);
      });

      await step('throttle 8KB/s: seconds-long listing stays live and completes', async () => {
        // the 120-object listing is ~32KB of XML: at 8Kbps that is ~4s of
        // paced transfer (assert ≥3s below) while chunks keep resetting
        // the 30s no-progress watchdog — alive, just slow.
        await setMode({ mode: 'throttle', bps: 8192 });
        await bucketRoot();
        await waitFor(async () => (await rowKeys()).includes(`${SLOW}/`), 30000, 'throttled root listing');
        const t0 = Date.now();
        await dblClickRow(`${SLOW}/`);
        await waitFor(inFlight, 6000, 'in-flight skeleton');
        await ok('skeleton shows while the listing trickles in', true);
        await shot('39-fault-throttle-skeleton');
        await waitFor(async () => (await totalItems()) >= SLOWN - 5, 60000, 'slowwalk rows under throttle');
        const secs = ((Date.now() - t0) / 1000).toFixed(1);
        await ok(`throttle: full listing in ${secs}s (paced, not stalled)`, Date.now() - t0 >= 3000);
      });

      await step('reset (RST mid-session): classified error + Retry recovers', async () => {
        await setMode({ mode: 'reset' });
        await bucketRoot();
        const st = await waitFor(errState, 30000, 'reset error state');
        await ok(`reset → "${st.title}" + Retry (${st.sub.slice(0, 60)}…)`,
          /could not load/i.test(st.title) && st.retry);
        await shot('40-fault-reset-error');
        await setMode({ mode: 'direct' });
        await clickRetry();
        await waitFor(async () => (await rowKeys()).some((k) => k.startsWith(SEED)), 20000, 'rows after retry');
        await ok('Retry after reset re-listed the bucket root', true);
      });

      await step('blackhole: dead endpoint → stream watchdog → timeout state + Retry recovers', async () => {
        await setMode({ mode: 'blackhole' });
        await dblClickRow(`${SLOW}/`);
        await waitFor(inFlight, 6000, 'skeleton while the source is silent');
        await ok('skeleton holds while the endpoint never answers', true);
        // the watchdog fires at the 10 s listing timeout set through the
        // Settings UI (the whole point: the setting is honored live, not
        // just stored); budget generously for CI jitter
        const st = await waitFor(errState, 50000, 'watchdog timeout state');
        await ok(`watchdog → "${st.title}" (${st.sub.slice(0, 60)}…)`, /took too long/i.test(st.title)
          && /10s/.test(st.sub));
        await shot('41-fault-blackhole-timeout');
        await setMode({ mode: 'direct' });
        await clickRetry();
        await waitFor(async () => (await totalItems()) >= SLOWN - 5, 30000, 'rows after retry');
        await ok('Retry after blackhole recovered the listing', true);
        // restore the documented defaults (0 = default per field)
        await call('SetTuning', 0, 0, 0, 0, 0, 0);
        const tun = await call('GetTuning');
        await ok(`tuning restored to defaults (listing ${tun.listingTimeoutMs} ms, parts ${tun.partSizeMiB} MiB)`,
          tun.listingTimeoutMs === 30000 && tun.partSizeMiB === 0);
      });

      await step('fault lab teardown: fault source + seeded prefix removed', async () => {
        await call('RemoveSource', FSRC);
        await page.reload();
        await treeOpen(SRCNAME);
        const nodes = await evalPage(() => Array.from(document.querySelectorAll('#tree .tnode'))
          .map((n) => (n.querySelector('.tlabel')?.textContent || '').trim()));
        await ok(`fault source gone from the sidebar [${nodes.join(', ')}]`, !nodes.includes(FSRC));
        sh(`mc rm --recursive --force local/${BUCKET}/${SLOW}/ >/dev/null 2>&1 || true`);
        const left = sh(`mc ls --recursive local/${BUCKET}/${SLOW}/ 2>&1 | wc -l`).trim();
        await ok(`seeded prefix purged (${left} objects left)`, Number(left) === 0);
        await shot('42-fault-teardown');
      });
    } finally {
      try { await setMode({ mode: 'direct' }); } catch { /* proxy may already be gone */ }
      fx.kill();
      clearFaultPorts(); // fx.kill covers the happy path; ports are the source of truth
    }
  }
}

// ---------- shutdown ----------
process.on('SIGINT', async () => { await stopServer(srv); process.exit(130); });
main()
  .catch(async (err) => {
    console.error(`\nv3 live walk crashed: ${err.message}`);
    if (page) await shot('crash').catch(() => {});
    if (consoleTail.length) {
      console.log('last browser console lines:');
      for (const l of consoleTail) console.log(`  ${l}`);
    }
    await stopServer(srv);
    process.exit(1);
  })
  .finally(async () => {
    await stopServer(srv);
    if (context) await context.close().catch(() => {});
  });
