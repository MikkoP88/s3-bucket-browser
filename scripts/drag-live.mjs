#!/usr/bin/env node
// drag-live.mjs — the LIVE native drag-out rig.
//
// The only automated test of the app's OS-level drag & drop that uses the
// REAL desktop app (Wails + WebView2), a REAL OLE drop target and REAL
// mouse input. Three actors:
//
//   1. tools/dragprobe — a Win32 window registered as an OLE drop target
//      (exactly what Explorer is) plus a SendInput synthetic mouse, driven
//      over JSON lines (built by this script).
//   2. The real GUI binary, launched portable (fresh config every run) with
//      WebView2 remote debugging enabled, driven over CDP via playwright.
//   3. This coordinator: seeds MinIO, walks the onboarding UI, calibrates
//      page CSS pixels onto the physical screen, then runs the tiers.
//
// Tiers:
//   A binding  — mouse down, DragOutFiles called directly through the
//                bridge, glide into the probe, release. Isolates the native
//                OLE machinery from everything the webview adds.
//   B gesture  — the user's exact path: real dragstart on a grid row
//                (dragOS → preventDefault → DragOutFiles), glide, release.
//   C self     — drag inside the app window and release there: the gesture
//                must end cleanly (drag:self-drop) with the app responsive.
//   D control  — the probe floats its own OLE drag of two real temp files
//                onto its own window: identical target, input and release
//                with only the source swapped. This is the tier that pinned
//                the machine finding below.
//   E info     — the same self-drag released over the app's webview (a
//                registered OLE target in another process). INFORMATIONAL,
//                not scored: on this Windows build ole32 never dispatches
//                the final IDropTarget::Drop for an injected release —
//                SendInput pure/nudged/combined, even a WM_LBUTTONUP posted
//                straight into ole32's CLIPBRDWNDCLASS tracker, all end with
//                DragLeave + effect 0 — verified against WebView2's own
//                target. A hardware drag is required to observe that last
//                dispatch; everything the app controls is observable.
//
// Payload verification: because that machine property blocks the synthetic
// Drop, tiers A/B/D score the LEAVE-TIME PULL — at DragLeave the button is
// already up and the data object still alive, so the probe makes exactly
// the GetData a real target's Drop would make, stats + MD5s every file the
// CF_HDROP names, and the hashes are compared against the seeded MinIO
// bytes. A real drop, should the machine ever deliver one, still wins. The
// app's own drag-out diagnostics (scope "drag" in the event log) are read
// back from the portable config's events.jsonl and attached to the report
// — thread id, OleInitialize hr, DoDragDrop hr, and the QueryContinueDrag
// call count that is the pulse of the OLE loop.
//
// Usage:  node scripts/drag-live.mjs [--label NAME] [--expect-fail] [--keep]
//   --expect-fail  exit 0 only if the drag-out FAILURE is reproduced (no
//                  OLE drag observed); for the pre-fix baseline run.
// Artifacts: testartifacts/gui-drag/ (report-<label>.json, screenshots).

import { spawn, execFileSync } from 'node:child_process';
import { rm, mkdir } from 'node:fs/promises';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const ART = path.join(ROOT, 'testartifacts', 'gui-drag');
const APPDIR = path.join(ART, 'app');
const APP_EXE = path.join(APPDIR, 's3b.exe');
const PROBE_EXE = path.join(ART, 'dragprobe.exe');
const PROFILE = path.join(ART, 'webview-profile');
const SEED = path.join(ART, 'seed');
const CDP_PORT = 9223;

const LABEL = (arg('label') || 'run');
const EXPECT_FAIL = flag('expect-fail');
const KEEP = flag('keep');

const WIN_TITLE = 'S3 Bucket Browser';
const BINDING = 'github.com/MikkoP88/s3-bucket-browser/pkg/api.App.DragOutFiles';
const SRC = 'dragrig';
const BUCKET = 'dragrig';
const ENDPOINT = 'http://localhost:9000';
const REGION = 'us-east-1';
const MINIO = 's3b-e2e-minio';
const AK = 'minioadmin';

// seed files: name → content (sizes stay tiny so staging is instant)
const betaBin = Buffer.alloc(4096);
for (let i = 0; i < betaBin.length; i++) betaBin[i] = (i * 7 + 13) & 0xff;
const SEED_FILES = {
  'alpha-drag.txt': Buffer.from('drag-rig alpha\n'),
  'beta-drag.bin': betaBin,
  'gamma-drag.txt': Buffer.from('drag-rig gamma\n'),
};
const MD5 = Object.fromEntries(Object.entries(SEED_FILES)
  .map(([n, b]) => [n, crypto.createHash('md5').update(b).digest('hex')]));

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function arg(name) {
  const i = process.argv.indexOf(`--${name}`);
  return i >= 0 ? process.argv[i + 1] : undefined;
}
function flag(name) { return process.argv.includes(`--${name}`); }

function log(msg) { console.log(`[drag-live] ${msg}`); }

async function waitFor(fn, ms, what) {
  const t0 = Date.now();
  for (;;) {
    const v = await (async () => { try { return await fn(); } catch { return false; } })();
    if (v) return v;
    if (Date.now() - t0 > ms) throw new Error(`timeout waiting for ${what}`);
    await sleep(250);
  }
}

function killTree(proc) {
  if (!proc || proc.exitCode !== null) return;
  try { execFileSync('taskkill', ['/PID', String(proc.pid), '/T', '/F'], { stdio: 'ignore' }); } catch { /* already gone */ }
}

// ---------- dragprobe driver ----------

class Probe {
  constructor(proc) {
    this.proc = proc;
    this.events = [];       // every stdout line parsed
    this.waiters = [];      // {match, resolve}
    this.acks = [];         // {op, resolve}
    this.proc.stdout.setEncoding('utf8');
    let buf = '';
    this.proc.stdout.on('data', (d) => {
      buf += d;
      let i;
      while ((i = buf.indexOf('\n')) >= 0) {
        const line = buf.slice(0, i).trim();
        buf = buf.slice(i + 1);
        if (!line) continue;
        let ev; try { ev = JSON.parse(line); } catch { continue; }
        this.events.push(ev);
        this.waiters = this.waiters.filter((w) => { if (w.match(ev)) { w.resolve(ev); return false; } return true; });
        if (ev.ev === 'ack') this.acks = this.acks.filter((w) => { if (w.op === ev.op) { w.resolve(ev); return false; } return true; });
      }
    });
  }

  static async start(x, y, w, h) {
    const errLog = fs.openSync(path.join(ART, `probe-stderr-${LABEL}.log`), 'a');
    const proc = spawn(PROBE_EXE, ['-x', String(x), '-y', String(y), '-w', String(w), '-h', String(h)], { stdio: ['pipe', 'pipe', errLog] });
    const p = new Probe(proc);
    const ready = await p.wait((ev) => ev.ev === 'ready', 10000);
    p.rect = { x, y, w, h, cx: ready.cx, cy: ready.cy };
    return p;
  }

  wait(match, ms) {
    const existing = this.events.find(match);
    if (existing) return Promise.resolve(existing);
    return new Promise((resolve, reject) => {
      const w = { match, resolve };
      this.waiters.push(w);
      setTimeout(() => {
        const i = this.waiters.indexOf(w);
        if (i >= 0) { this.waiters.splice(i, 1); resolve(null); } // timeout → null, caller decides
      }, ms).unref?.();
    });
  }

  send(o) { this.proc.stdin.write(`${JSON.stringify(o)}\n`); }

  async ackOp(o, ms = 8000) {
    const before = this.events.length;
    this.send(o);
    // ack for this op must arrive after our send — match by position:
    // the next 'ack' with this op name appended after `before`
    const t0 = Date.now();
    while (Date.now() - t0 < ms) {
      const ev = this.events.slice(before).find((e) => e.ev === 'ack' && e.op === o.op);
      if (ev) return ev;
      await sleep(50);
    }
    throw new Error(`probe did not ack ${o.op}`);
  }

  async request(o, evName, ms = 8000) {
    this.send(o);
    return this.wait((e) => e.ev === evName, ms);
  }

  async stop() {
    this.send({ op: 'quit' });
    const exited = await new Promise((res) => {
      this.proc.once('exit', () => res(true));
      setTimeout(() => res(false), 2000);
    });
    if (!exited) killTree(this.proc);
  }
}

// ---------- app ----------

async function cdpReady() {
  for (;;) {
    try {
      const r = await fetch(`http://127.0.0.1:${CDP_PORT}/json/version`, { signal: AbortSignal.timeout(1500) });
      if (r.ok) return true;
    } catch { /* not up yet */ }
    await sleep(300);
  }
}

async function readDragLog() {
  const f = path.join(APPDIR, 'config', 'events.jsonl');
  if (!fs.existsSync(f)) return ['(no events.jsonl)'];
  const lines = fs.readFileSync(f, 'utf8').split('\n').filter(Boolean)
    .map((l) => { try { return JSON.parse(l); } catch { return null; } })
    .filter((j) => j && j.scope === 'drag')
    .map((j) => j.message);
  return lines;
}

// ---------- coordinate calibration ----------

// Returns {originX, originY, scale} mapping page CSS px → physical screen
// px for the webview client area, validated by a real hover probe.
async function calibrate(probe, page, appwin) {
  const dpr = await page.evaluate(() => window.devicePixelRatio);
  const physW = Math.round(await page.evaluate(() => screen.width) * dpr);
  const scr = await probe.request({ op: 'screen' }, 'screen');
  if (!scr) throw new Error('probe did not answer screen size');
  if (Math.abs(scr.w - physW) > 4) {
    log(`WARNING: screen mismatch probe=${scr.w}x${scr.h} page-physical=${physW} — mapping may be off`);
  }

  // candidate client-area windows: Chromium children first, largest first
  // (childInfo marshals Class as "class", coords capitalized)
  const kids = (appwin.children || [])
    .map((c) => ({ ...c, area: (c.Right - c.Left) * (c.Bottom - c.Top), cls: c.class || c.Class || '' }))
    .sort((a, b) => b.area - a.area);
  const cands = [
    ...kids.filter((c) => c.cls.startsWith('Chrome')),
    ...kids,
    { Left: appwin.left, Top: appwin.top, cls: '(appwin)' },
  ];
  if (!cands.length) throw new Error('findwin returned no children');

  const rowCount = await page.evaluate(() => document.querySelectorAll('#grid-body .grid-row').length);
  if (!rowCount) throw new Error('no grid rows to calibrate against');

  for (const c of cands) {
    const map = (x, y) => ({ sx: Math.round(c.Left + x * dpr), sy: Math.round(c.Top + y * dpr) });
    const rect = await page.evaluate(() => {
      const r = document.querySelector('#grid-body .grid-row').getBoundingClientRect();
      return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
    });
    const { sx, sy } = map(rect.x, rect.y);
    await probe.ackOp({ op: 'move', x: sx, y: sy });
    await sleep(200);
    const hovering = await page.evaluate(() => !!document.querySelector('#grid-body .grid-row:hover'));
    if (hovering) {
      log(`calibrated on child "${c.cls}" @${c.Left},${c.Top} dpr=${dpr}`);
      return { originX: c.Left, originY: c.Top, scale: dpr };
    }
  }
  throw new Error('could not calibrate: no child rect puts the cursor over a grid row');
}

async function rowScreenPt(page, cal, name) {
  const rect = await page.evaluate((nm) => {
    const rows = Array.from(document.querySelectorAll('#grid-body .grid-row'));
    const r = rows.find((row) => row._model && (row._model.name === nm || row._model.key === nm));
    if (!r) return null;
    const b = r.getBoundingClientRect();
    return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
  }, name);
  if (!rect) throw new Error(`row ${name} not found`);
  return {
    sx: Math.round(cal.originX + rect.x * cal.scale),
    sy: Math.round(cal.originY + rect.y * cal.scale),
  };
}

// fire-and-forget binding call; markers readable later from the page
async function fireDragOut(page, items) {
  await page.evaluate(([fq, its]) => {
    window.__dragRes = undefined;
    window.__dragErr = null;
    window.wails.Call.ByName(fq, its)
      .then((r) => { window.__dragRes = r === null || r === undefined ? 'ok' : String(r); })
      .catch((e) => { window.__dragErr = String(e); });
  }, [BINDING, items]);
}

const dragPromiseState = (page) => page.evaluate(() =>
  ({ res: window.__dragRes, err: window.__dragErr }));

// ---------- tiers ----------

async function tierA(probe, page, cal, report) {
  const out = { name: 'A: binding-driven drag-out' };
  report.tiers.push(out);
  // Capture the event index at TIER start: the release's leave + leavepull
  // can land while the `up` op is still acking (the op sleeps 20ms), so an
  // index taken after the ack would disqualify this tier's own events.
  const idx = probe.events.length;
  const isNew = (e) => probe.events.indexOf(e) >= idx;
  const items = ['alpha-drag.txt', 'gamma-drag.txt'].map((k) => ({
    source: SRC, bucket: BUCKET, key: k, name: k, size: SEED_FILES[k].length,
  }));
  const start = await rowScreenPt(page, cal, 'alpha-drag.txt');
  await probe.ackOp({ op: 'move', x: start.sx, y: start.sy });
  await probe.ackOp({ op: 'down' });
  await sleep(100);
  await fireDragOut(page, items);
  await sleep(150);
  await probe.ackOp({ op: 'glide', x: probe.rect.cx, y: probe.rect.cy, steps: 25, ms: 450 });
  await sleep(150);
  out.winat = await probe.request({ op: 'winat', x: probe.rect.cx, y: probe.rect.cy }, 'winat');
  await probe.ackOp({ op: 'up' });

  // This machine's ole32 never dispatches IDropTarget::Drop for a synthetic
  // release (tier E documents it) — so the payload checkpoint is the
  // leave-time GetData pull: DragLeave fires after the button came up,
  // while the data object is still alive, and makes exactly the GetData a
  // real Drop would have made. A real drop, should one ever arrive, still
  // wins.
  const drop = await probe.wait((e) => e.ev === 'drop' && isNew(e), 4000);
  const pull = drop || await probe.wait((e) => e.ev === 'leavepull' && isNew(e), 11000);
  const enter = probe.events.find((e) => e.ev === 'enter');
  out.sawEnter = !!enter;
  out.via = drop ? 'drop' : 'leavepull';
  if (!pull) {
    out.pass = false;
    out.reason = 'neither a drop nor a leave-time payload pull arrived';
    out.promise = await dragPromiseState(page);
    return out;
  }
  out.paths = pull.paths;
  out.missing = pull.missing;
  out.md5 = pull.md5;
  out.getDataErr = pull.error;
  out.promise = await waitFor(() => dragPromiseState(page).then((s) => (s.res !== undefined || s.err) ? s : false), 8000, 'drag promise');
  const want = ['alpha-drag.txt', 'gamma-drag.txt'];
  out.pass = !pull.error && pull.missing?.length === 0
    && want.every((n) => pull.md5?.[n] === MD5[n]);
  if (!out.pass) out.reason = `payload mismatch (err=${pull.error} missing=${JSON.stringify(pull.missing)} md5=${JSON.stringify(pull.md5)} want=${JSON.stringify(MD5)})`;
  return out;
}

async function tierB(probe, page, cal, report) {
  const out = { name: 'B: real row-dragstart gesture' };
  report.tiers.push(out);
  const idx = probe.events.length;
  const isNew = (e) => probe.events.indexOf(e) >= idx;
  const start = await rowScreenPt(page, cal, 'beta-drag.bin');
  await probe.ackOp({ op: 'move', x: start.sx, y: start.sy });
  await probe.ackOp({ op: 'down' });
  await sleep(60);
  // small real movement first — that is what makes Chromium raise dragstart
  await probe.ackOp({ op: 'glide', x: start.sx + 14, y: start.sy + 10, steps: 6, ms: 180 });
  await sleep(350); // dragstart → preventDefault → DragOutFiles → OLE float
  await probe.ackOp({ op: 'glide', x: probe.rect.cx, y: probe.rect.cy, steps: 25, ms: 450 });
  await sleep(150);
  await probe.ackOp({ op: 'up' });

  // Same payload checkpoint as tier A: prefer a real drop, fall back to the
  // leave-time GetData pull (the machine-blocked synthetic release means
  // DragLeave is where the payload can still be proven).
  const drop = await probe.wait((e) => e.ev === 'drop' && isNew(e), 4000);
  const pull = drop || await probe.wait((e) => e.ev === 'leavepull' && isNew(e), 11000);
  if (!pull) {
    out.pass = false;
    out.reason = 'neither a drop nor a leave-time payload pull arrived';
    out.promise = await dragPromiseState(page);
    out.toasts = await page.evaluate(() => Array.from(document.querySelectorAll('#toasts .toast')).map((t) => t.textContent));
    return out;
  }
  out.via = drop ? 'drop' : 'leavepull';
  out.paths = pull.paths;
  out.missing = pull.missing;
  out.md5 = pull.md5;
  out.getDataErr = pull.error;
  out.pass = !pull.error && pull.missing?.length === 0 && pull.md5?.['beta-drag.bin'] === MD5['beta-drag.bin'];
  if (!out.pass) out.reason = `payload mismatch (err=${pull.error} md5=${JSON.stringify(pull.md5)})`;
  return out;
}

async function tierC(probe, page, cal, report) {
  const out = { name: 'C: self-drop ends cleanly' };
  report.tiers.push(out);
  const start = await rowScreenPt(page, cal, 'gamma-drag.txt');
  // release over the app's own status bar — drag:self-drop, no transfer
  const statusBar = await page.evaluate(() => {
    const b = document.querySelector('footer.statusbar')?.getBoundingClientRect();
    return b ? { x: b.x + b.width / 2, y: b.y + b.height / 2 } : null;
  });
  if (!statusBar) { out.pass = false; out.reason = 'no statusbar element'; return out; }
  await probe.ackOp({ op: 'move', x: start.sx, y: start.sy });
  await probe.ackOp({ op: 'down' });
  await sleep(60);
  await probe.ackOp({ op: 'glide', x: start.sx + 14, y: start.sy + 10, steps: 6, ms: 180 });
  await sleep(350);
  const over = {
    sx: Math.round(cal.originX + statusBar.x * cal.scale),
    sy: Math.round(cal.originY + statusBar.y * cal.scale),
  };
  await probe.ackOp({ op: 'glide', x: over.sx, y: over.sy, steps: 20, ms: 350 });
  await sleep(120);
  await probe.ackOp({ op: 'up' });

  const s = await waitFor(() => dragPromiseState(page).then((r) => (r.res !== undefined || r.err) ? r : false), 15000, 'self-drop gesture end').catch(() => null);
  out.promise = s;
  out.appResponsive = await page.evaluate(() => 1 + 1).then((v) => v === 2).catch(() => false);
  const dropAfterIdx = probe.events.length; // nothing may land on the probe
  await sleep(400);
  out.probeSawDrop = probe.events.slice(dropAfterIdx).some((e) => e.ev === 'drop');
  out.pass = !!s && out.appResponsive && !out.probeSawDrop;
  if (!out.pass && !s) out.reason = 'gesture never ended (drag promise pending)';
  return out;
}

// The control experiment: the probe floats its own OLE drag of two REAL
// temp files and drops it onto its own window. Target, synthetic input and
// button-up routing are identical to the app tiers — only the source
// differs. It validated the machine finding (even an in-process drop of
// real files ends DragLeave + effect 0 under synthetic release) and now
// proves the leave-time GetData pull that scores the app tiers.
async function tierD(probe, report) {
  const out = { name: 'D: probe self-drag control' };
  report.tiers.push(out);
  const idx = probe.events.length;
  const c = { x: probe.rect.cx, y: probe.rect.cy };
  await probe.ackOp({ op: 'move', x: c.x, y: c.y });
  await probe.ackOp({ op: 'down' });
  await sleep(80);
  await probe.ackOp({ op: 'dragout' });
  await sleep(300); // let DoDragDrop float before moving
  await probe.ackOp({ op: 'glide', x: c.x + 40, y: c.y + 26, steps: 10, ms: 250 });
  await sleep(150);
  // Release into the drag-tracker window itself (WM_LBUTTONUP posted to the
  // capture window) — injected SendInput button-ups end the gesture with
  // DragLeave + effect 0 even from a perfectly tracked hover.
  await probe.ackOp({ op: 'cup' });

  const isNew = (e) => probe.events.indexOf(e) >= idx;
  const drop = await probe.wait((e) => e.ev === 'drop' && isNew(e), 4000);
  const pull = drop || await probe.wait((e) => e.ev === 'leavepull' && isNew(e), 5000);
  const end = await probe.wait((e) => e.ev === 'dragout-end' && isNew(e), 5000);
  out.end = end;
  out.via = drop ? 'drop' : 'leavepull';
  if (!pull) {
    out.pass = false;
    out.reason = 'self-drag delivered no payload at all (neither drop nor leave pull)';
    return out;
  }
  out.paths = pull.paths;
  out.missing = pull.missing;
  out.md5 = pull.md5;
  out.getDataErr = pull.error;
  // effect=1 requires the machine to dispatch Drop for a synthetic release;
  // on this box it never does (tier E documents it), so the payload pull +
  // the S_DROP return carry the verdict.
  out.pass = !pull.error && pull.paths?.length === 2
    && pull.missing?.length === 0
    && Object.keys(pull.md5 || {}).length === 2
    && end?.hr === '0x00040101';
  if (!out.pass) out.reason = `payload or hr mismatch (${JSON.stringify({ pull, end })})`;
  return out;
}

// The mechanism discriminator: the probe floats the SAME self-drag (real
// temp files, tracker in the probe process, cup release) but lets it land
// on the APP's webview — a real, WebView2-registered OLE target owned by
// another process. If wails:file-drop fires, a synthetic release CAN
// complete a genuine OLE drop and the probe's own window is the anomaly;
// if not, no OLE target on this box completes a synthetic release.
async function tierE(probe, page, cal, report) {
  const out = { name: 'E: self-drag onto the app window', info: true };
  report.tiers.push(out);
  const idx = probe.events.length;
  const over = await rowScreenPt(page, cal, 'alpha-drag.txt');
  await page.evaluate(() => {
    window.__osdrop = null;
    window.wails?.Events?.On?.('wails:file-drop', (ev) => {
      window.__osdrop = JSON.stringify(ev);
    });
  });
  const c = { x: probe.rect.cx, y: probe.rect.cy };
  await probe.ackOp({ op: 'move', x: c.x, y: c.y });
  await probe.ackOp({ op: 'down' });
  await sleep(80);
  await probe.ackOp({ op: 'dragout' });
  await sleep(300); // let DoDragDrop float before moving
  await probe.ackOp({ op: 'glide', x: over.sx, y: over.sy, steps: 12, ms: 350 });
  await sleep(200);
  await probe.ackOp({ op: 'cup' });

  const isNew = (e) => probe.events.indexOf(e) >= idx;
  const end = await probe.wait((e) => e.ev === 'dragout-end' && isNew(e), 8000);
  out.end = end;
  const dropEv = await waitFor(() => page.evaluate(() => window.__osdrop), 15000).catch(() => null);
  out.osdrop = dropEv;
  await page.keyboard.press('Escape').catch(() => {}); // close any upload dialog
  out.pass = !!dropEv && /probe-one\.txt/.test(String(dropEv))
    && end?.hr === '0x00040101' && end?.effect === 1;
  if (!out.pass) {
    out.reason = 'informational: synthetic release did not complete an OLE drop on this machine (ole32 dispatches no Drop under injected input)';
  }
  return out;
}

// ---------- setup ----------

async function buildBinaries() {
  log('building dragprobe + GUI binary …');
  execFileSync('go', ['build', '-o', PROBE_EXE, './tools/dragprobe'], { cwd: ROOT, stdio: 'inherit' });
  execFileSync('go', ['build', '-o', APP_EXE, './cmd/s3b'], { cwd: ROOT, stdio: 'inherit' });
}

async function seedMinio() {
  await rm(SEED, { recursive: true, force: true });
  await mkdir(SEED, { recursive: true });
  for (const [n, b] of Object.entries(SEED_FILES)) fs.writeFileSync(path.join(SEED, n), b);
  const sh = (c) => execFileSync('docker', ['exec', MINIO, 'sh', '-c', c], { stdio: 'pipe' }).toString();
  sh('mc alias set local http://localhost:9000 minioadmin minioadmin >/dev/null');
  sh('mc rb --force local/dragrig >/dev/null 2>&1 || true');
  sh('mc mb --ignore-existing local/dragrig >/dev/null');
  execFileSync('docker', ['cp', `${SEED}/.`, `${MINIO}:/tmp/seed/`], { stdio: 'inherit' });
  sh('mc cp /tmp/seed/alpha-drag.txt /tmp/seed/beta-drag.bin /tmp/seed/gamma-drag.txt local/dragrig/ >/dev/null');
  const ls = sh('mc ls local/dragrig');
  const n = ls.split('\n').filter((l) => l.trim()).length;
  if (n !== 3) throw new Error(`seed failed: mc ls shows ${n} objects`);
  log('MinIO seeded: 3 objects in dragrig/');
}

// ---------- main ----------

let probe; let app; let browser;
let cleaned = false;
const report = { label: LABEL, startedAt: new Date().toISOString(), tiers: [] };

async function main() {
  await rm(path.join(ART, 'webview-profile'), { recursive: true, force: true });
  await rm(path.join(APPDIR, 'config'), { recursive: true, force: true });
  await mkdir(APPDIR, { recursive: true });
  fs.writeFileSync(path.join(APPDIR, 's3b-portable'), ''); // portable: config beside exe

  await buildBinaries();
  await seedMinio();

  // app first: its window rect decides where the probe goes
  log('launching app (WebView2 CDP on :9223) …');
  const appLog = fs.openSync(path.join(ART, `app-${LABEL}.log`), 'w');
  app = spawn(APP_EXE, [], {
    cwd: APPDIR,
    env: {
      ...process.env,
      // Wails' Go WebView2 loader ignores the WEBVIEW2_* env vars; gui.go
      // wires these S3B_RIG_* vars into the real application options.
      S3B_RIG_CDP_PORT: String(CDP_PORT),
      S3B_RIG_WEBVIEW_PROFILE: PROFILE,
    },
    stdio: ['ignore', appLog, appLog],
  });
  await waitFor(cdpReady, 60000, 'WebView2 CDP endpoint');
  browser = await chromium.connectOverCDP(`http://127.0.0.1:${CDP_PORT}`);
  const ctx = browser.contexts()[0];
  // WebView2's real app page is the wails.localhost target; pages()[0] can
  // be a leftover about:blank context from the connect handshake.
  const page = await waitFor(() => {
    const pages = ctx.pages();
    return pages.find((p) => p.url().includes('wails.localhost')) || false;
  }, 20000, 'wails.localhost page');
  page.on('pageerror', (e) => log(`pageerror: ${e.message}`));

  // pin English, then let the app boot to onboarding
  await page.waitForLoadState('domcontentloaded').catch(() => {});
  for (let i = 0; i < 10; i++) {
    try { await page.evaluate(() => localStorage.setItem('s3b-lang', 'en')); break; }
    catch { await sleep(400); } // mid-navigation context churn — retry
  }
  await page.reload();
  await waitFor(() => page.evaluate(() =>
    document.querySelectorAll('#empty-actions .btn').length > 0
    || document.querySelectorAll('#grid-body .grid-row').length > 0), 20000, 'onboarding or grid');

  // add + test + save the source (same flow gui-live.mjs walks)
  if (await page.evaluate(() => document.querySelectorAll('#empty-actions .btn').length > 0)) {
    await page.locator('#empty-actions .btn.primary').first().click();
    await waitFor(() => page.locator('#modal-root .modal input.input').count().then((n) => n >= 6), 5000, 'source editor fields');
    const inputs = page.locator('#modal-root .modal input.input');
    await inputs.nth(0).fill(SRC);
    await inputs.nth(1).fill(BUCKET);
    await inputs.nth(2).fill(ENDPOINT);
    await inputs.nth(3).fill(REGION);
    await inputs.nth(4).fill(AK);
    await inputs.nth(5).fill(AK);
    await page.locator('#modal-root .modal input[type="checkbox"]').first().check();
    const foot = (re) => page.locator('#modal-root .modal-foot .btn', { hasText: re }).first();
    await foot(/test/i).click();
    await waitFor(() => page.locator('#modal-root .modal').textContent().then((t) => /✅|❌/.test(t)), 30000, 'Test result');
    const tested = await page.locator('#modal-root .modal').textContent();
    if (!tested.includes('✅')) throw new Error(`source test failed: ${tested}`);
    await foot(/save/i).click();
  }
  await waitFor(() => page.evaluate(() => document.querySelectorAll('#grid-body .grid-row').length >= 3), 20000, '3 seeded rows');
  log('source added, bucket listed');

  // place the probe beside the app window (never overlapping)
  probe = await Probe.start(0, 0, 560, 420); // throwaway position; moved next
  let appwin;
  for (let attempt = 1; attempt <= 3; attempt++) {
    appwin = (await probe.request({ op: 'findwin', title: WIN_TITLE }, 'appwin', 10000))?.data;
    if (appwin && !appwin.error) break;
    log(`findwin attempt ${attempt}: ${JSON.stringify(appwin)}`);
    appwin = undefined;
    await sleep(500);
  }
  if (!appwin) throw new Error(`findwin: no answer after 3 attempts (probe events: ${JSON.stringify(probe.events.slice(0, 5))})`);
  report.appwin = appwin;
  log(`app window: class=${appwin.class} @${appwin.left},${appwin.top}-${appwin.right},${appwin.bottom} (${(appwin.matches || []).length} title matches)`);
  const dpr = await page.evaluate(() => window.devicePixelRatio);
  const physW = Math.round(await page.evaluate(() => screen.width) * dpr);
  const PW = 560;
  let px = appwin.right + 24;
  if (px + PW > physW - 8) px = Math.max(8, appwin.left - PW - 24);
  await probe.stop();
  probe = await Probe.start(px, appwin.top, PW, 420);
  log(`probe window at ${px},${appwin.top} (drop target ${probe.rect.cx},${probe.rect.cy})`);

  const cal = await calibrate(probe, page, appwin);
  report.calibration = cal;

  // a pre-gesture control: a plain glide across the probe must NOT raise
  // OLE events (proves the probe only speaks during real gestures)
  const controlIdx = probe.events.length;
  await probe.ackOp({ op: 'glide', x: probe.rect.cx, y: probe.rect.cy, steps: 5, ms: 150 });
  await sleep(300);
  report.controlClean = !probe.events.slice(controlIdx)
    .some((e) => e.ev === 'enter' || e.ev === 'over' || e.ev === 'drop');

  await tierA(probe, page, cal, report);
  await tierB(probe, page, cal, report);
  await tierC(probe, page, cal, report);
  await tierD(probe, report);
  await tierE(probe, page, cal, report);

  await page.screenshot({ path: path.join(ART, `final-${LABEL}.png`) }).catch(() => {});
  report.dragLog = await readDragLog();
  report.probeEvents = probe.events;
}

try {
  const watchdog = setTimeout(() => { log('WATCHDOG: rig stuck 4min — aborting'); cleanup(1); }, 4 * 60 * 1000);
  watchdog.unref?.();
  await main();
  cleanup(0);
} catch (e) {
  report.fatal = String(e?.message || e);
  console.error(`[drag-live] FATAL: ${e?.stack || e}`);
  cleanup(1);
}

function cleanup(code) {
  if (cleaned) return;
  cleaned = true;
  const done = () => {
    report.finishedAt = new Date().toISOString();
    const scored = report.tiers.filter((t) => !t.info);
    const pass = scored.filter((t) => t.pass).length;
    const total = scored.length;
    let verdict;
    if (EXPECT_FAIL) {
      const a = report.tiers[0];
      verdict = a && !a.pass && !a.sawEnter ? 'FAILURE-REPRODUCED' : 'UNEXPECTED';
      report.verdict = verdict;
      console.log(`\n[drag-live] verdict: ${verdict} (${pass}/${total} tiers passed${verdict === 'FAILURE-REPRODUCED' ? '' : ' — expected the drag-out failure!'})`);
    } else {
      verdict = total > 0 && pass === total ? 'PASS' : 'FAIL';
      report.verdict = verdict;
      console.log(`\n[drag-live] verdict: ${verdict} (${pass}/${total} tiers passed)`);
    }
    for (const t of report.tiers) {
      const mark = t.info ? (t.pass ? '✔' : '–') : (t.pass ? '✔' : '✘');
      console.log(`  ${mark} ${t.name}${t.reason ? ` — ${t.reason}` : ''}`);
    }
    if (report.dragLog) for (const l of report.dragLog) console.log(`  [drag] ${l}`);
    try {
      fs.writeFileSync(path.join(ART, `report-${LABEL}.json`), JSON.stringify(report, null, 2));
    } catch { /* best effort */ }
    console.log(`[drag-live] report: ${path.join(ART, `report-${LABEL}.json`)}`);
    process.exit(
      (EXPECT_FAIL ? verdict === 'FAILURE-REPRODUCED' : verdict === 'PASS') ? 0 : 1);
  };
  Promise.allSettled([
    probe?.stop(),
    browser?.close().catch(() => {}),
    new Promise((res) => {
      if (!app || app.exitCode !== null) return res();
      killTree(app);
      app.once('exit', () => res());
      setTimeout(res, 3000);
    }),
  ]).then(done, done);
}
