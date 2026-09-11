#!/usr/bin/env node
// gui-live.mjs — the LIVE GUI walk: real backend, real browser, real S3.
//
// Unlike gui-visual.mjs (which drives the frontend against a fake backend),
// this spins up tools/gui-live — the REAL pkg/api app behind an HTTP bridge —
// serves the real frontend/ with the Wails binding/SSE bridge injected, and
// walks the GUI in a real Chromium (Edge/Chrome channel, persistent profile)
// against a real bucket. Every check touches real S3, real local files and
// real transfers:
//
//   onboarding → add+test source → browse → guard chips → admin → doctor →
//   dual pane → New folder → DnD upload → versions + A/B diff → conflict
//   overwrite → DnD download (bytes verified) → rename → transfer manager →
//   log area → settings (theme) → profile save (Ctrl+S, encrypted file on
//   disk) → reload → close profile → open (wrong + right password) →
//   server restart (session-only sources vanish, settings survive) →
//   cleanup through the UI.
//
// Credentials NEVER live here: S3B_ACCESS_KEY / S3B_SECRET_KEY must be set
// (same envs the CLI uses). Bucket/endpoint/region via S3B_BUCKET /
// S3B_ENDPOINT / S3B_REGION (defaults: testijotain @ hel1.your-objectstorage,
// us-east-1, path-style). Native pickers (upload menus, OS file drops) cannot
// be scripted in a plain browser — DnD covers the transfer paths; the native
// dialogs remain covered by gui-visual + manual passes.
//
// Usage:  node scripts/gui-live.mjs [--headed] [--channel msedge|chrome]
// Artifacts: testartifacts/gui-live/ (screenshots, server log, fixtures,
// encrypted profile, browser profile — wiped fresh every run).

import { spawn, execFileSync } from 'node:child_process';
import { rm, mkdir, writeFile, readFile, stat } from 'node:fs/promises';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const ART = path.join(ROOT, 'testartifacts', 'gui-live');
const SHOTS = path.join(ART, 'shots');
const FIX = path.join(ART, 'fixtures');
const PROFILE = path.join(ART, 'profile.s3bprofile');
const USERDIR = path.join(ART, 'browser-profile');
const SRV_EXE = path.join(ART, 'gui-live.exe');
const SRVLOG = path.join(ART, 'server.log');

const arg = (k) => { const i = process.argv.indexOf(`--${k}`); return i >= 0 ? process.argv[i + 1] : null; };
const HEADED = process.argv.includes('--headed');

const KEY = process.env.S3B_ACCESS_KEY;
const SECRET = process.env.S3B_SECRET_KEY;
const ENDPOINT = process.env.S3B_ENDPOINT || 'https://hel1.your-objectstorage.com';
const REGION = process.env.S3B_REGION || 'us-east-1';
const BUCKET = process.env.S3B_BUCKET || 'testijotain';
const SRCNAME = 'hetzner-live';
const PASSWORD = 'live-walk-password-1';
const LOCK = '\u{1F510}'; // the profile-file chip glyph (main.js uses \u{1F510})
const PORT = 39871; // fixed port: a restart keeps the origin (and localStorage) stable
const PREFIX = 'zz-live';
const V1 = 'gui-live upload v1 — hello from the live harness\n';
const V2 = 'gui-live upload v2 — overwritten through the conflict dialog\n';
const B_TXT = 'second fixture file for the live walk\n';

if (!KEY || !SECRET) {
  console.error('refusing to run without credentials: set S3B_ACCESS_KEY and S3B_SECRET_KEY (same envs as the CLI)');
  process.exit(2);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let pass = 0, fail = 0; const failures = [];
async function step(name, fn) {
  process.stdout.write(`== ${name}\n`);
  try { await fn(); pass++; }
  catch (err) { fail++; failures.push(`${name}: ${err.message}`); console.error(`   FAIL: ${err.message}`); }
  finally { await dismissUI(); }
}
// a stray modal/ctx menu/dropdown would intercept every later pointer event
// (#modal-root is a full-screen overlay) — close whatever is left over
async function dismissUI() {
  if (!page) return;
  try {
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
// visible display names — inside a prefix, keys carry the full zz-live/…
// prefix while the row shows the basename; appearance checks use names
const names = () => evalPage(() => Array.from(document.querySelectorAll('#grid-body .grid-row'))
  .filter((r) => r.style.display !== 'none' && r._model).map((r) => r._model.name || r._model.key));
// local rows carry full paths as keys (S3/remote side bindings use their own)
const sideKeys = () => evalPage(() => Array.from(document.querySelectorAll('#local-grid-body .grid-row'))
  .filter((r) => r.style.display !== 'none' && r._model).map((r) => String(r._model.key).split(/[\\/]/).pop()));
// folder rows render .tname WITHOUT the trailing slash their keys carry
// (inlined per helper: evaluate callbacks run in the browser, no closures)
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
// race-free row actions: wait for the row to exist before acting on it
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
// version count straight through the bridge (pollable, no dialog round-trip)
const verCount = () => evalPage(async ([b, k]) => {
  const r = await fetch('/__live/call', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ m: 'ObjectVersions', args: [b, k] }) });
  const j = await r.json();
  return (j.result || []).length;
}, [BUCKET, `${PREFIX}/live-a.txt`]);

async function shot(name) {
  try { await page.screenshot({ path: path.join(SHOTS, `${name}.png`) }); } catch { /* non-fatal */ }
}

// modal helpers (the app renders exactly one modal at a time in #modal-root)
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
  // close through the app's own button so onClose handlers run (a forced
  // hide would strand the app's modal bookkeeping)
  const btn = await elOrNull(() => Array.from(document.querySelectorAll('#modal-root .modal-foot .btn'))
    .reverse().find((b) => /^(close|cancel)$/i.test(b.textContent.trim())) || null);
  if (btn) { await btn.asElement().click(); await sleep(100); return; }
  await page.keyboard.press('Escape');
  await sleep(100);
  await evalPage(() => document.getElementById('modal-root')?.classList.add('hidden')); // last resort
}
async function modalVisible() {
  return evalPage(() => !!document.querySelector('#modal-root .modal') && !document.getElementById('modal-root').classList.contains('hidden'));
}
async function modalText() {
  return evalPage(() => document.querySelector('#modal-root .modal')?.textContent || '');
}
// confirm / typedConfirm accept: type the word when asked, then the ok button
async function confirmDanger(word = 'delete') {
  const hasInput = await page.locator('#modal-root .modal input.input').count();
  if (hasInput) await page.locator('#modal-root .modal input.input').last().fill(word);
  await clickFooter(/^(delete|discard & close|destroy)$/i);
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

// synthetic HTML5 DnD with a real DataTransfer — the app's own dragstart
// fills the payload, the target's dragover/drop handlers consume it
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
// context menu on the grid's empty area (dispatched on the body itself so
// the row filter in the handler lets it through)
async function bodyCtx() {
  await page.evaluate((el) => {
    const r = el.getBoundingClientRect();
    el.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: r.right - 40, clientY: r.bottom - 30 }));
  }, await bodyH());
  await sleep(80);
}

// ---------- backend + browser lifecycle ----------
function startServer() {
  return new Promise((resolve, reject) => {
    const proc = spawn(SRV_EXE, ['-frontend', 'frontend', '-addr', `127.0.0.1:${PORT}`, '-open-profile', PROFILE, '-save-profile', PROFILE], { cwd: ROOT });
    let url = '';
    proc.stdout.on('data', (d) => {
      const m = d.toString().match(/^gui-live (http:\/\/\S+)/m);
      if (m && !url) { url = m[1]; resolve({ proc, url }); }
    });
    proc.stderr.on('data', (d) => fs.appendFileSync(SRVLOG, d));
    proc.on('exit', (code) => { if (!url) reject(new Error(`gui-live exited early (code ${code}) — see testartifacts/gui-live/server.log`)); });
    setTimeout(() => { if (!url) reject(new Error('gui-live did not announce its URL in 60s')); }, 60000);
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

async function main() {
  await rm(ART, { recursive: true, force: true });
  await mkdir(SHOTS, { recursive: true });
  await mkdir(FIX, { recursive: true });
  fs.writeFileSync(SRVLOG, '');
  await writeFile(path.join(FIX, 'live-a.txt'), V1);
  await writeFile(path.join(FIX, 'live-b.txt'), B_TXT);

  console.log('building tools/gui-live …');
  execFileSync('go', ['build', '-o', SRV_EXE, './tools/gui-live'], { cwd: ROOT, stdio: 'inherit' });

  srv = await startServer();
  console.log(`gui-live at ${srv.url}`);

  await launch();
  page = context.pages()[0] || await context.newPage();
  page.on('console', (m) => { consoleTail.push(`[${m.type()}] ${m.text()}`); consoleTail = consoleTail.slice(-40); });
  page.on('pageerror', (e) => { consoleTail.push(`[pageerror] ${e.message}`); consoleTail = consoleTail.slice(-40); });
  page.setDefaultTimeout(20000);

  await walk();

  await shot('final');
  console.log(`\nlive walk: ${pass} check(s) passed, ${fail} failed`);
  if (fail) {
    console.log('\nlast browser console lines:');
    for (const l of consoleTail) console.log(`  ${l}`);
    process.exitCode = 1;
  }
}

// ---------- the walk ----------
let base = 0; // version count at baseline (relative assertions survive reruns)
async function walk() {
  await step('boot + onboarding', async () => {
    await page.goto(srv.url);
    // pin English (host locale could be anything) and reload
    await evalPage(() => localStorage.setItem('s3b-lang', 'en'));
    await page.reload();
    await waitFor(() => txt('#status-version').then((s) => s.includes('0.0.0-gui-live')), 15000, 'version in status bar');
    await ok('bridge binding live', true);
    await waitFor(() => evalPage(() => Array.from(document.querySelectorAll('#empty-actions .btn')).length > 0), 10000, 'onboarding actions');
    await ok('onboarding empty state shown', await modalVisible() === false);
    await shot('01-onboarding');
  });

  await step('add + test Hetzner source', async () => {
    await page.locator('#empty-actions .btn.primary').first().click();
    await waitFor(() => page.locator('#modal-root .modal input.input').count().then((n) => n >= 5), 5000, 'source editor fields');
    const inputs = page.locator('#modal-root .modal input.input');
    await inputs.nth(0).fill(SRCNAME);      // name
    await inputs.nth(1).fill(ENDPOINT);     // endpoint
    await inputs.nth(2).fill(REGION);       // region
    await inputs.nth(3).fill(KEY);          // access key
    await inputs.nth(4).fill(SECRET);       // secret key
    // path-style checkbox (first checkbox in the modal)
    await page.locator('#modal-root .modal input[type="checkbox"]').first().check();
    await clickFooter(/^test$/i);
    await waitFor(async () => /✅|❌/.test(await modalText()), 30000, 'Test result');
    await ok('source test passed against real S3', (await modalText()).includes('✅'));
    await shot('02-source-test');
    await clickFooter(/^save$/i);
    await waitFor(async () => (await rowKeys()).includes(BUCKET), 20000, 'buckets list');
    await ok('buckets listed', true);
  });

  await step('session-only status chip', async () => {
    await waitFor(() => txt('#status-pfile').then((s) => /unsaved source/i.test(s)), 5000, 'unsaved-source chip');
    await ok(`status bar: "${(await txt('#status-pfile')).trim()}"`, true);
  });

  await step('browse bucket + guard chips', async () => {
    await dblClickRow(BUCKET);
    await waitFor(async () => (await rowKeys()).some((k) => k.startsWith('NetApp_koulutus')), 20000, 'bucket contents');
    await ok('existing data visible (NetApp_koulutus/)', true);
    const chips = await evalPage(() => Array.from(document.querySelectorAll('#guard-chips .guard-chip')).map((c) => c.textContent));
    await ok(`guard chips: [${chips.join(', ')}]`, chips.length > 0);
    await shot('03-objects');
  });

  await step('admin dialog (guard chips → real bucket info)', async () => {
    await page.locator('#guard-chips .guard-chip').first().click();
    await waitFor(async () => (await modalText()).length > 20, 10000, 'admin dialog');
    const tabs = await evalPage(() => Array.from(document.querySelectorAll('#modal-root .tab')).map((t) => t.textContent));
    await ok(`admin dialog opens (${tabs.length} tab(s))`, await modalVisible());
    await shot('04-admin');
    await closeModal();
  });

  await step('doctor (real checks)', async () => {
    await page.locator('#btn-doctor').click();
    await waitFor(async () => /run all/i.test(await modalText()), 10000, 'doctor dialog');
    await clickFooter(/^run all$/i); // rows open as "notrun" — the walk must start them
    await waitFor(async () => /pass/i.test(await evalPage(() => document.querySelector('#modal-root .doc-summary')?.textContent || '')), 60000, 'doctor summary');
    const summary = await evalPage(() => document.querySelector('#modal-root .doc-summary')?.textContent || '');
    await ok(`doctor summary: "${summary.trim()}"`, /pass/i.test(summary));
    await shot('05-doctor');
    await closeModal();
  });

  await step('dual pane: bind local side to fixtures', async () => {
    await page.keyboard.press('F9');
    await waitFor(() => evalPage(() => !document.getElementById('local-pane').classList.contains('hidden')), 5000, 'local pane');
    await page.locator('#local-crumb').click();
    await answerPrompt(FIX);
    await waitFor(async () => (await sideKeys()).includes('live-a.txt'), 10000, 'fixture rows');
    await ok('local rows visible', true);
    await shot('06-dualpane');
  });

  await step('New folder via empty-area context menu', async () => {
    await bodyCtx();
    await ctxItem(/new folder/i);
    await answerPrompt(PREFIX);
    await waitFor(async () => (await rowKeys()).includes(`${PREFIX}/`), 20000, 'zz-live folder');
    await ok(`folder ${PREFIX}/ created`, true);
    await dblClickRow(`${PREFIX}/`);
    await waitFor(async () => (await rowKeys()).length === 0, 10000, 'empty folder view');
  });

  await step('DnD upload (local row → grid body)', async () => {
    // default conflict preset is "ask": the dialog opens for EVERY upload —
    // an unanswered (canceled) dialog silently cancels the whole upload
    await dnd(await sideRow('live-a.txt'), await bodyH());
    await waitFor(async () => /conflicting files/i.test(await modalText()), 15000, 'conflict dialog');
    await clickFooter(/^start$/i); // default radio = Overwrite
    await waitFor(async () => (await names()).includes('live-a.txt'), 60000, 'uploaded row');
    await ok('live-a.txt uploaded via drag & drop', true);
    await dnd(await sideRow('live-b.txt'), await bodyH());
    await waitFor(async () => /conflicting files/i.test(await modalText()), 15000, 'conflict dialog (b)');
    await clickFooter(/^start$/i);
    await waitFor(async () => (await names()).includes('live-b.txt'), 60000, 'uploaded row b');
    await ok('live-b.txt uploaded via drag & drop', true);
    await shot('07-uploaded');
  });

  await step('versions baseline', async () => {
    base = await verCount();
    await ok(`baseline: ${base} version(s) of ${PREFIX}/live-a.txt`, base >= 1);
    await clickRow('live-a.txt');
    await rightClickRow('live-a.txt');
    await ctxItem(/previous versions/i);
    await waitFor(() => page.locator('#modal-root .ver-row').count().then((n) => n >= base), 15000, 'version rows');
    await ok(`versions dialog shows ${await page.locator('#modal-root .ver-row').count()} row(s)`, true);
    await closeModal();
  });

  await step('overwrite via conflict dialog → 2 versions → A/B diff', async () => {
    await writeFile(path.join(FIX, 'live-a.txt'), V2);
    await dnd(await sideRow('live-a.txt'), await bodyH());
    await waitFor(async () => /conflicting files/i.test(await modalText()), 15000, 'conflict dialog');
    await ok('conflict policy dialog shown', true);
    await shot('08-conflict');
    await clickFooter(/^start$/i); // default radio = Overwrite
    await waitFor(async () => (await verCount()) === base + 1, 60000, 'second version landed');
    await ok('overwrite created a new version', true);
    await clickRow('live-a.txt');
    await rightClickRow('live-a.txt');
    await ctxItem(/previous versions/i);
    await waitFor(async () => page.locator('#modal-root .ver-row').count().then((n) => n >= base + 1), 20000, 'two versions');
    await ok(`now ${await page.locator('#modal-root .ver-row').count()} versions`, true);
    await shot('09-versions');
    // pick A = newest, B = oldest, compare
    await page.locator('#modal-root .ver-row').first().locator('.ver-pick', { hasText: 'A' }).click();
    await sleep(80);
    await page.locator('#modal-root .ver-row').last().locator('.ver-pick', { hasText: 'B' }).click();
    await sleep(120);
    await page.locator('#modal-root button', { hasText: 'Compare A' }).click();
    // the metadata table renders at once; the text diff fills in async
    await waitFor(async () => {
      const d = await modalText();
      return d.includes('v1') && d.includes('v2');
    }, 20000, 'A/B diff contents');
    await ok('A/B diff shows both contents', true);
    await shot('10-versiondiff');
    await closeModal();
  });

  await step('DnD download (S3 row → local pane, bytes verified)', async () => {
    // bind the side pane to a fresh empty dir — the file's arrival proves the
    // download (the local pane deliberately has no delete; Explorer's job)
    const dst = path.join(FIX, 'downloads');
    await mkdir(dst, { recursive: true });
    await page.locator('#local-crumb').click();
    await answerPrompt(dst);
    await waitFor(async () => (await sideKeys()).length === 0, 10000, 'empty downloads dir');
    await dnd(await rowAction('live-b.txt', 'grid'), await sideBodyH());
    await waitFor(async () => /conflicting files/i.test(await modalText()), 15000, 'download conflict dialog');
    await clickFooter(/^start$/i);
    await waitFor(async () => (await sideKeys()).includes('live-b.txt'), 60000, 'downloaded row');
    const bytes = await readFile(path.join(dst, 'live-b.txt'), 'utf8');
    await ok('downloaded bytes identical', bytes === B_TXT);
    await shot('11-downloaded');
  });

  await step('rename (F2)', async () => {
    await clickRow('live-b.txt');
    await page.keyboard.press('F2');
    await answerPrompt('live-renamed.txt');
    await waitFor(async () => (await names()).includes('live-renamed.txt'), 20000, 'renamed row');
    await ok('renamed to live-renamed.txt', true);
  });

  await step('transfer manager', async () => {
    await page.locator('#btn-transfers').click();
    await waitFor(() => modalVisible(), 5000, 'transfer manager');
    await ok('transfer manager opens', true);
    await shot('12-transfers');
    await closeModal();
  });

  await step('log area (Ctrl+L)', async () => {
    await page.keyboard.press('Control+L');
    await waitFor(() => evalPage(() => !document.getElementById('logarea').classList.contains('hidden')), 5000, 'logarea');
    const lines = await evalPage(() => Array.from(document.querySelectorAll('#logarea .log-line, #logarea .line, #logarea div')).slice(-30).map((l) => l.textContent));
    await ok(`log shows ${lines.length} line(s)`, lines.length > 0);
    await shot('13-log');
    await page.keyboard.press('Control+L');
  });

  await step('settings: theme → dark (applies + persists)', async () => {
    await menuClick(/^settings$/i, /settings/i);
    await waitFor(() => page.locator('#modal-root .set-body select').count().then((n) => n > 0), 5000, 'settings dialog');
    await page.locator('#modal-root .set-body select').first().selectOption('dark');
    await sleep(150);
    await ok('dark theme applied', await evalPage(() => document.documentElement.dataset.theme === 'dark'));
    await shot('14-settings-dark');
    await closeModal();
  });

  await step('profile save (Ctrl+S → encrypted file on disk)', async () => {
    await page.keyboard.press('Control+S');
    await waitFor(() => page.locator('#modal-root .modal input[type="password"]').count().then((n) => n > 0), 5000, 'password prompt');
    await answerPrompt(PASSWORD);
    await waitFor(() => txt('#status-pfile').then((s) => s.includes(LOCK)), 15000, `profile chip (got "${await txt('#status-pfile')}")`);
    const st = await stat(PROFILE);
    await ok(`profile file written (${st.size} bytes, mode 0${(st.mode & 0o777).toString(8)})`, st.size > 0);
    await shot('15-profile-saved');
  });

  await step('page reload keeps the open profile + dark theme', async () => {
    await page.reload();
    await waitFor(async () => (await rowKeys()).length > 0 || (await sideKeys()).length > 0, 20000, 'view after reload');
    await waitFor(() => txt('#status-pfile').then((s) => s.includes(LOCK)), 10000, 'profile chip after reload');
    await ok('profile still open after reload', true);
    await ok('theme still dark after reload', await evalPage(() => document.documentElement.dataset.theme === 'dark'));
  });

  await step('close profile → onboarding returns', async () => {
    await menuClick(/^file$/i, /close profile file/i);
    await sleep(300);
    if (await modalVisible()) await confirmDanger('discard'); // only if it claims unsaved changes
    await waitFor(() => evalPage(() => Array.from(document.querySelectorAll('#empty-actions .btn')).length > 0), 10000, 'onboarding back');
    await ok('sources gone, onboarding shown', true);
    await ok('status chip hidden', await evalPage(() => document.getElementById('status-pfile').classList.contains('hidden')));
    await shot('16-closed');
  });

  await step('open profile: wrong password rejected', async () => {
    await menuClick(/^file$/i, /open profile file/i);
    await waitFor(() => page.locator('#modal-root .modal input[type="password"]').count().then((n) => n > 0), 5000, 'password prompt');
    await answerPrompt('definitely-wrong');
    await waitFor(async () => (await toasts()).some((t) => /decrypt|password|corrupt/i.test(t)), 10000, 'error toast');
    await ok('wrong password → error toast', true);
    await ok('still onboarding', await evalPage(() => Array.from(document.querySelectorAll('#empty-actions .btn')).length > 0));
  });

  await step('open profile: correct password restores sources', async () => {
    await menuClick(/^file$/i, /open profile file/i);
    await waitFor(() => page.locator('#modal-root .modal input[type="password"]').count().then((n) => n > 0), 5000, 'password prompt');
    await answerPrompt(PASSWORD);
    await waitFor(async () => (await rowKeys()).includes(BUCKET), 20000, 'buckets after reopen');
    await ok('sources restored from encrypted file', true);
    await ok('profile chip back', (await txt('#status-pfile')).includes(LOCK));
    await shot('17-reopened');
  });

  await step('server restart: session sources vanish, settings survive', async () => {
    await stopServer(srv);
    srv = await startServer();
    await page.goto(srv.url);
    await waitFor(() => evalPage(() => Array.from(document.querySelectorAll('#empty-actions .btn')).length > 0), 15000, 'onboarding after restart');
    await ok('fresh backend → no sources (strict session model)', true);
    await ok('theme persisted (dark)', await evalPage(() => document.documentElement.dataset.theme === 'dark'));
    await shot('18-after-restart');
  });

  await step('reopen + clean up through the UI', async () => {
    await menuClick(/^file$/i, /open profile file/i);
    await waitFor(() => page.locator('#modal-root .modal input[type="password"]').count().then((n) => n > 0), 5000, 'password prompt');
    await answerPrompt(PASSWORD);
    await waitFor(async () => (await rowKeys()).includes(BUCKET), 20000, 'buckets');
    await dblClickRow(BUCKET);
    await dblClickRow(`${PREFIX}/`);
    await waitFor(async () => (await rowKeys()).length >= 2, 20000, 'zz-live contents');
    await page.keyboard.press('Control+A');
    await page.keyboard.press('Delete');
    await waitFor(() => modalVisible(), 5000, 'delete confirm');
    await confirmDanger('delete');
    await waitFor(async () => (await rowKeys()).length === 0, 60000, 'folder emptied');
    await ok('zz-live objects deleted', true);
    await page.keyboard.press('Backspace'); // up to bucket root
    await waitFor(async () => (await rowKeys()).includes(`${PREFIX}/`), 20000, 'bucket root');
    await clickRow(`${PREFIX}/`);
    await page.keyboard.press('Delete');
    await waitFor(() => modalVisible(), 5000, 'folder delete confirm');
    await confirmDanger('delete');
    await waitFor(async () => !(await rowKeys()).includes(`${PREFIX}/`), 60000, 'folder gone');
    await ok('zz-live folder deleted', true);
    await waitFor(async () => (await rowKeys()).some((k) => k.startsWith('NetApp')), 20000, 'bucket root relisted');
    const left = await rowKeys();
    await ok(`bucket root back to pre-existing data [${left.join(', ')}]`, left.every((k) => !k.startsWith(PREFIX)) && left.some((k) => k.startsWith('NetApp_koulutus')));
    await shot('19-cleaned');
  });
}

// ---------- shutdown ----------
process.on('SIGINT', async () => { await stopServer(srv); process.exit(130); });
main()
  .catch(async (err) => {
    console.error(`\nlive walk crashed: ${err.message}`);
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
