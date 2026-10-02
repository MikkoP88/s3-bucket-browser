// Secondary pane for dual-pane mode: a full twin of the main content
// area — its own toolbar, path bar (interactive breadcrumb + filter),
// prefixed Grid('local-') and status line — bound to the workstation
// filesystem, any remote (non-S3) source, or any S3 source (buckets +
// prefixes). Always through source-pinned APIs (the backend's single
// view source belongs to the main pane), with its own navigation
// history, directory compare decorations and cross-pane drag & drop
// (uploads land on folder rows of the remote/S3 binding, downloads on
// folder rows / the body of the local binding).
import { Grid } from './grid.js';
import { toast } from './dialogs.js';
import { el, fmtBytes, debounce, srcIconEl, parseSourcePath, parentPrefix } from './util.js';
import { api, subscribeStream } from './api.js';
import { t } from './i18n.js';
import { updateCommandState } from './commands.js';

const app = () => api;
const $ = (id) => document.getElementById(id);

// parentRowOn is the Settings -> View gate the main pane's parent row and
// climb keys share (off by default) — the pane's own ".." row follows the
// same switch, so one setting rules both content areas.
const parentRowOn = () => localStorage.getItem('s3b-parent-row') === '1';

// ARROW_SVG / arrowIconEl: the view picker's "Return to last view" glyph
// — the toolbar's back arrow, so the icon matches the mechanic.
const ARROW_SVG = '<svg viewBox="0 0 16 16" width="15" height="15" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M15 8a.5.5 0 0 0-.5-.5H2.707l3.147-3.146a.5.5 0 1 0-.708-.708l-4 4a.5.5 0 0 0 0 .708l4 4a.5.5 0 0 0 .708-.708L2.707 8.5H14.5A.5.5 0 0 0 15 8"/></svg>';
const arrowIconEl = () => {
  const s = el('span', { class: 'src-ic' });
  s.innerHTML = ARROW_SVG;
  return s;
};

// aggregateCompare folds recursive CompareDir rows into per-child statuses
// (used by both panes): direct rows keep their status, folders get
// 'diff-below' when anything beneath them differs, 'same-sub' when all
// descendants match without a direct verdict.
export function aggregateCompare(rows) {
  const agg = new Map(); // name -> { direct, subDiff }
  for (const r of rows) {
    const segs = r.key.split('/');
    const name = segs[0];
    const a = agg.get(name) || { direct: null, subDiff: false };
    if (segs.length === 1) a.direct = r.status;
    else if (r.status !== 'same') a.subDiff = true;
    agg.set(name, a);
  }
  const out = new Map();
  for (const [name, a] of agg) {
    if (a.direct && a.direct !== 'same') out.set(name, a.direct);
    else if (a.subDiff) out.set(name, 'diff-below');
    else if (a.direct === 'same') out.set(name, 'same');
    else out.set(name, 'same-sub');
  }
  return out;
}

// entryKey/entrySame/entryAncestorOf compare pane history entries with the
// road signs' own spelling: crumb slices, tree nodes and pasted paths name
// the same place differently — separators differ, and Windows paths are
// case-insensitive while remote and S3 keys are not — so the comparison
// normalizes instead of comparing bytes.
const paneNormDir = (d, sep) => (sep === '\\'
  ? String(d || '').replace(/\//g, '\\').replace(/\\+$/, '').toUpperCase()
  : String(d || '').replace(/^\/+/, '').replace(/\/+$/, ''));

const entryKey = (e) => {
  if (e.kind === 'local') return 'local|' + paneNormDir(e.dir, '\\');
  if (e.kind === 'remote') return 'remote|' + (e.source || '') + '|' + paneNormDir(e.dir, '/');
  return 's3|' + (e.source || '') + '|' + (e.bucket || '') + '|' + paneNormDir(e.dir, '/');
};

const entrySame = (a, b) => !!a && !!b && a.kind === b.kind && entryKey(a) === entryKey(b);

// An ancestor sits strictly above in the same binding: the roots view
// above any drive, the buckets view above any bucket, a folder above its
// subtree.
const entryAncestorOf = (up, cur) => {
  if (!up || !cur || up.kind !== cur.kind) return false;
  if (up.kind !== 'local' && (up.source || '') !== (cur.source || '')) return false;
  if (up.kind === 'local' && !String(up.dir || '')) return true;
  if (up.kind === 's3' && !up.bucket) return true;
  if (up.kind === 's3' && (up.bucket || '') !== (cur.bucket || '')) return false;
  const sep = up.kind === 'local' ? '\\' : '/';
  const u = paneNormDir(up.dir, sep);
  const c = paneNormDir(cur.dir, sep);
  return u !== c && (u === '' || c.startsWith(u + sep));
};

export class SidePane {
  constructor() {
    this.grid = new Grid('local-');
    this.dir = '';        // local: '' = filesystem roots view; remote: anchored path; s3: prefix
    this.bucket = '';     // s3 binding: the listed bucket ('' = buckets view)
    this.s3Seq = 0;       // s3 listing stream generation
    this.s3Off = null;    // s3 listing page-event unsubscribe
    this.navSeq = 0;      // local/remote listing generation (stale-response guard)
    this.roots = [];
    this.binding = { kind: 'local', source: '' }; // what the pane is bound to
    this.bound = false;   // a binding has been applied (until then: onboarding)
    this.hasListed = false; // some listing has landed for the current binding
    this.painted = false; // streaming leg: first page arrived
    this.sources = [];    // [{id,name,type}] fed by main after ListSources
    this.hist = [];       // back stack of location entries
    this.futr = [];       // forward stack of location entries
    this.upWanted = false; // the current listing's parent-row verdict
    this.on = {};         // callbacks: dropFolder, dropBody, openFail, activateRemoteFile, activateS3File, contextEmpty
    this.openedLoc = null; // where the pane stood when it was opened — the view picker's "return" target
    this.pendingSelect = null; // search pick waiting for its listing to land (gotoHit)

    this.grid.on.activate = (m) => {
      if (this.binding.kind === 's3') {
        if (m.isBucket) { this.go({ kind: 's3', source: this.binding.source, bucket: m.key, dir: '' }); return; }
        if (m.isDir) { this.go({ kind: 's3', source: this.binding.source, bucket: this.bucket, dir: m.key }); return; }
        this.on.activateS3File?.(this.bucket, m);
        return;
      }
      if (m.isDir) {
        this.go(this.binding.kind === 'remote'
          ? { kind: 'remote', source: this.binding.source, dir: m.key }
          : { kind: 'local', dir: m.path });
        return;
      }
      if (this.binding.kind === 'remote') { this.on.activateRemoteFile?.(this.binding.source, m); return; }
      app().OpenLocal(m.path).catch((e) => this.on.openFail?.(e));
    };
    this.grid.on.select = () => this.updateStatus();
    // dropping a remote/S3 selection onto a local folder row = download into it
    this.grid.on.drop = (folder, payload, e) => this.on.dropFolder?.(folder, payload, e);

    // dropping onto the pane body = transfer into the current directory.
    // Local bindings take remote/S3 downloads only (a directory must be
    // open — the roots view is not one); remote bindings always, S3
    // bindings whenever a bucket is open (its root included).
    const body = this.grid.body;
    const bodyMimes = () => (this.binding.kind === 'local'
      ? ['application/x-s3b']
      : ['application/x-s3b', 'application/x-s3b-local']);
    const droppable = () => (this.binding.kind === 'local'
      ? !!this.dir
      : this.binding.kind === 's3' ? !!this.bucket : true);
    body.addEventListener('dragover', (e) => {
      // external OS file drags highlight too (their upload arrives via the
      // wails:file-drop event, not the DOM drop handler below)
      const types = e.dataTransfer.types;
      if (droppable() && (bodyMimes().some((tm) => types.includes(tm)) || types.includes('Files'))) {
        e.preventDefault();
        body.classList.add('drop-target');
      }
    });
    body.addEventListener('dragleave', (e) => {
      if (e.target === body || e.target === this.grid.canvas) body.classList.remove('drop-target');
    });
    body.addEventListener('drop', (e) => {
      body.classList.remove('drop-target');
      if (!droppable()) return;
      let data = null;
      for (const tm of bodyMimes()) {
        const d = e.dataTransfer.getData(tm);
        if (d) { data = JSON.parse(d); break; }
      }
      if (!data) return;
      e.preventDefault();
      this.on.dropBody?.(data, e);
    });

    body.addEventListener('mousedown', (e) => {
      if (e.target === body || e.target === this.grid.canvas) this.grid.clearSelection();
    });
    // empty-area right-click (per-row menus are wired by main through
    // grid.on.context; main wires contextEmpty here)
    body.addEventListener('contextmenu', (e) => {
      if (e.target.closest('.grid-row')) return;
      e.preventDefault();
      this.on.contextEmpty?.(e, this.dir);
    });
    body.addEventListener('keydown', (e) => {
      // the climb key rests while the parent row is hidden (Settings -> View)
      if (e.key === 'Backspace' && parentRowOn()) { e.preventDefault(); this.up(); return; }
      if (this.grid.keydown(e)) e.preventDefault();
    });

    // pane-owned chrome: navigation and the path bar act on the pane's own
    // state; the upload/download/new/find buttons are wired by main (they
    // need its transfer and dialog engines — find scopes to this pane)
    $('local-btn-back').onclick = () => this.back();
    $('local-btn-forward').onclick = () => this.forward();
    $('local-btn-refresh').onclick = () => this.refresh();
    // onboarding picker: choosing a source binds the pane and leaves the
    // empty state behind
    $('local-src').onchange = () => this.rebind($('local-src').value);
    // the pane's filter narrows its own grid only; the value is read at
    // fire time, never captured at keystroke time
    const filter = $('local-filter');
    filter.addEventListener('input', debounce(() => this.grid.setFilter(filter.value), 120));
    // the parent row climbs on click; its keydown rests inside the button
    // (the body's Backspace handler must not see it)
    $('local-upbar').onclick = () => this.up();
    $('local-upbar').addEventListener('keydown', (e) => e.stopPropagation());
    // the breadcrumb's segments navigate; a click on the navbar's empty
    // area (not a segment, the filter or a button) opens the inline
    // path editor — the same affordance the main pane's navbar carries
    const navbar = document.querySelector('#local-pane .navbar');
    navbar.title = t('pathClickHint');
    navbar.addEventListener('click', (e) => {
      if (e.target.closest('.crumb, .crumb-sep, input, button')) return;
      this.editPath();
    });
    // the view picker's lifecycle: any click outside it (or its trigger
    // button in the global bar) and any Escape close it — the pane itself
    // stays open. The picker's own keys (Escape / arrows) rest inside it.
    document.addEventListener('click', (e) => {
      if (e.target.closest('#pane-dest, #btn-panes')) return;
      this.hideDestPop();
    }, true);
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') this.hideDestPop();
    });
    const dest = $('pane-dest');
    dest.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); this.hideDestPop(); return; }
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
      e.preventDefault();
      const opts = Array.from(dest.querySelectorAll('.dest-opt:not(:disabled)'));
      if (opts.length < 2) return;
      const i = opts.indexOf(document.activeElement);
      opts[(i + (e.key === 'ArrowDown' ? 1 : opts.length - 1)) % opts.length].focus();
    });
    // the Name-column seat stays live against the pane grid's own template
    // (mirrors main's observer on #grid-head)
    new MutationObserver(() => this.syncUpbarLayout()).observe($('local-grid-head'),
      { attributes: true, attributeFilter: ['style'], childList: true, subtree: true });
  }

  // ---------- source binding ----------

  // renderSources fills the onboarding picker: the workstation filesystem
  // plus every configured source — remote engines and S3 sources alike
  // (S3 sources browse their buckets/prefixes; transfers route through
  // the cross-source engine with the source pinned).
  renderSources() {
    const sel = $('local-src');
    const opts = [el('option', { value: 'local', text: 'Local' })];
    for (const s of this.sources) {
      opts.push(el('option', { value: s.id || s.name, text: s.name + ' (' + s.type + ')' }));
    }
    sel.replaceChildren(...opts);
    const cur = this.binding.kind !== 'local' ? this.binding.source : 'local';
    if ([...sel.options].some((o) => o.value === cur)) sel.value = cur;
  }

  // bindTo switches the binding without navigating (history is the
  // caller's business): null = the workstation filesystem.
  bindTo(src) {
    this.cancelS3Stream();
    if (!src) {
      this.binding = { kind: 'local', source: '' };
      this.bucket = '';
      this.dir = '';
    } else if (src.type === 's3') {
      // source stays the id (backend resolves id-or-name, localStorage too);
      // name is what the user sees in the crumb and prompts. A
      // bucket-scoped source opens its bucket's contents directly; legacy
      // account-wide sources open the buckets view.
      this.binding = { kind: 's3', source: src.id || src.name, name: src.name, bucket: src.bucket || '' };
      this.bucket = this.binding.bucket;
      this.dir = '';
    } else {
      this.binding = { kind: 'remote', source: src.id || src.name, name: src.name };
      this.dir = '/';
    }
    localStorage.setItem('s3b-side-src', this.binding.kind === 'local' ? 'local' : this.binding.source);
    this.bound = true;
    this.hasListed = false;
    this.applyDragPayload();
  }

  // rebind switches the pane to the workstation filesystem, a remote
  // source, or an S3 source and navigates to its root — a fresh start
  // (history cut, remembered location cleared).
  rebind(value) {
    const src = value === 'local' ? null : this.sources.find((s) => s.id === value || s.name === value);
    if (value !== 'local' && !src) return;
    this.bindTo(src);
    this.hist.length = 0;
    this.futr.length = 0;
    localStorage.removeItem('s3b-side-loc');
    this.start();
  }

  // reset drops any binding (the remembered source disappeared) and stands
  // the onboarding empty state back up.
  reset() {
    this.cancelS3Stream();
    this.bound = false;
    this.hasListed = false;
    this.hist.length = 0;
    this.futr.length = 0;
    this.grid.setRows([]);
    this.grid.setCmp(null);
    localStorage.removeItem('s3b-side-src');
    localStorage.removeItem('s3b-side-loc');
    this.showOnboarding();
  }

  // restore re-applies the remembered state once, after the first sources
  // load (later refreshes keep the user's current binding): the exact last
  // location when it is still reachable, else the remembered source's
  // opening view, else the workstation's home folder — a fresh pane opens
  // on the local directory (the old Panes panel's default), never an empty
  // stop; the onboarding picker stands only for a vanished remembered
  // source.
  restore() {
    if (this.bound) return;
    const savedSrc = localStorage.getItem('s3b-side-src');
    let loc = null;
    try { loc = JSON.parse(localStorage.getItem('s3b-side-loc') || 'null'); } catch { loc = null; }
    if (savedSrc && savedSrc !== 'local') {
      const src = this.sources.find((s) => s.id === savedSrc || s.name === savedSrc);
      if (!src) { this.showOnboarding(); return; } // remembered source is gone
      const kind = src.type === 's3' ? 's3' : 'remote';
      this.bindTo(src);
      if (loc && loc.source === this.binding.source) {
        this.navEntry({ kind, source: this.binding.source, bucket: loc.bucket || '', dir: loc.dir || '' });
        return;
      }
      this.navEntry({ kind, source: this.binding.source, bucket: src.bucket || '', dir: kind === 's3' ? '' : '/' });
      return;
    }
    if (savedSrc === 'local') {
      this.bindTo(null);
      if (loc && loc.kind === 'local' && loc.dir) { this.navigate(loc.dir); return; }
      this.start();
      return;
    }
    // nothing remembered: the workstation's home folder, the old Panes
    // panel's default — a fresh pane opens on the local directory, never
    // on an empty stop
    this.bindTo(null);
    this.start();
  }

  // applyDragPayload: remote and S3 bindings drag origin-tagged payloads
  // (source[/bucket] + dir + keys/entries) so every drop target of the
  // transfer matrix accepts them; the local binding keeps the prototype
  // payload ({paths}).
  applyDragPayload() {
    if (this.binding.kind === 'remote' || this.binding.kind === 's3') {
      const b = this.binding;
      const s3 = b.kind === 's3';
      this.grid.accepts = ['application/x-s3b', 'application/x-s3b-local'];
      this.grid.dragPayload = () => {
        // bucket rows in the S3 buckets view are navigation only — a
        // dragged bucket would mean "transfer the entire bucket".
        const rows = this.grid.selectedRows().filter((r) => !r.isBucket);
        const base = s3
          ? { source: b.source, bucket: this.bucket, dir: this.dir || '' }
          : { source: b.source, dir: this.dir || '/' };
        return {
          ...base,
          keys: rows.map((r) => r.key),
          entries: rows.map((r) => ({ key: r.key, size: r.size || 0, isDir: !!r.isDir })),
        };
      };
    } else {
      this.grid.accepts = null;
      delete this.grid.dragPayload;
    }
  }

  // ---------- history ----------

  // snapshot captures the current listing as a history/location entry.
  snapshot() {
    return {
      kind: this.binding.kind,
      source: this.binding.kind === 'local' ? '' : this.binding.source,
      bucket: this.bucket,
      dir: this.dir,
    };
  }
  canBack() { return this.hist.length > 0; }
  canForward() { return this.futr.length > 0; }

  // go navigates somewhere new: the current listing is pushed onto the
  // back stack, and the forward stack lives by the same rule the main
  // view's history learned — re-listing the very same place (clicking the
  // current crumb or tree node) moves nothing, a climb to an ancestor
  // (crumb segment, parent row, tree node, pasted path) keeps the deeper
  // view one Forward away instead of cutting it, and anything else —
  // deeper, sideways, another binding — cuts it (the standard rule).
  go(entry) {
    if (!entry) return;
    const from = this.bound && this.hasListed ? this.snapshot() : null;
    if (!from) this.futr.length = 0;
    else if (!entrySame(from, entry)) {
      this.hist.push(from);
      if (entryAncestorOf(entry, from)) this.futr.push(from);
      else this.futr.length = 0;
    }
    this.navEntry(entry);
  }
  back() {
    if (!this.hist.length) return;
    this.futr.push(this.snapshot());
    this.navEntry(this.hist.pop());
  }
  forward() {
    if (!this.futr.length) return;
    this.hist.push(this.snapshot());
    this.navEntry(this.futr.pop());
  }

  // entryOf normalizes an openAt target (the tree's kind/path/prefix
  // vocabulary) into a history entry.
  entryOf(target) {
    if (!target) return null;
    if (target.kind === 'remote') return { kind: 'remote', source: target.source, dir: target.path || '/' };
    if (target.kind === 's3') return { kind: 's3', source: target.source, bucket: target.bucket || '', dir: target.prefix || '' };
    return null;
  }

  // navEntry lists one entry directly — history bookkeeping is the
  // caller's business. Switches the source binding when the entry's
  // source differs from the current one.
  navEntry(e) {
    if (!e) return;
    if (e.kind === 'remote') {
      if (e.source && e.source !== this.binding.source) {
        const src = this.sources.find((s) => s.id === e.source || s.name === e.source);
        if (!src) return;
        this.bindTo(src);
      }
      if (this.binding.kind === 'remote') this.navigateRemote(e.dir || '/');
      return;
    }
    if (e.kind === 's3') {
      if (e.source && e.source !== this.binding.source) {
        const src = this.sources.find((s) => s.id === e.source || s.name === e.source);
        if (!src) return;
        this.bindTo(src);
      }
      if (this.binding.kind === 's3') this.navigateS3({ bucket: e.bucket || '', prefix: e.dir || '' });
      return;
    }
    if (e.kind === 'local') {
      // a local entry rebinds to the workstation — history can cross
      // bindings (Back from a remote source to a local folder works)
      if (this.binding.kind !== 'local') this.bindTo(null);
      // dir '' is the roots view — the climb's destination past a drive
      // root and the "This PC" breadcrumb segment's target
      if (e.dir === '') { this.showRoots(); return; }
      this.navigate(e.dir);
    }
  }

  // openAt is the tree's "Open on secondary pane": ensure the pane is
  // visible, then list the target through the pane's history. A fresh
  // pane binds straight to the target's source and starts its history
  // there.
  openAt(target) {
    const entry = this.entryOf(target);
    this.show();
    if (!entry) return;
    if (!this.bound) {
      const src = this.sources.find((s) => s.id === entry.source || s.name === entry.source);
      if (!src) return;
      this.bindTo(src);
      this.hist.length = 0;
      this.futr.length = 0;
    }
    this.go(entry);
  }

  async start() {
    if (this.binding.kind === 'remote') return this.navigateRemote(this.dir || '/');
    if (this.binding.kind === 's3') return this.navigateS3({ bucket: this.binding.bucket || '', prefix: '' });
    try {
      this.roots = await app().LocalRoots();
      await this.navigate(await app().LocalHome());
    } catch (e) {
      this.listFail(e);
    }
  }

  show() {
    const wasHidden = !this.visible;
    $('local-pane').classList.remove('hidden');
    $('pane-split').classList.remove('hidden');
    // where the pane stood when it was opened: persistLoc overwrites the
    // remembered spot with every landing, so this one capture is the
    // view picker's "return to last view" target
    if (wasHidden) this.openedLoc = this.rememberedLoc();
    if (!this.bound) this.restore();
    else if (!this.hasListed) this.start();
  }
  hide() {
    this.hideDestPop();
    $('local-pane').classList.add('hidden');
    $('pane-split').classList.add('hidden');
  }
  get visible() { return !$('local-pane').classList.contains('hidden'); }

  // ---------- Dual-pane button's view picker ----------

  // rememberedLoc reads the persisted pair (s3b-side-src + s3b-side-loc)
  // into a navEntry — the pane's last view. restore() navigates here on
  // reopen; the view picker returns to it on demand.
  rememberedLoc() {
    const savedSrc = localStorage.getItem('s3b-side-src');
    if (!savedSrc) return null;
    let loc = null;
    try { loc = JSON.parse(localStorage.getItem('s3b-side-loc') || 'null'); } catch { loc = null; }
    if (savedSrc === 'local') return loc && loc.kind === 'local' ? { kind: 'local', dir: loc.dir || '' } : null;
    if (loc && loc.kind !== 'local' && loc.source) {
      return { kind: loc.kind, source: loc.source, bucket: loc.bucket || '', dir: loc.dir || '' };
    }
    // no exact location survived: the remembered source's opening view
    const src = this.sources.find((s) => s.id === savedSrc || s.name === savedSrc);
    if (!src) return null;
    return src.type === 's3'
      ? { kind: 's3', source: src.id || src.name, bucket: src.bucket || '', dir: '' }
      : { kind: 'remote', source: src.id || src.name, dir: '/' };
  }

  // locCanonical renders a navEntry in the user-facing NAME:// form (the
  // picker's "last view" subtitle) — the same shapes paneCanonical builds
  // for the live binding.
  locCanonical(e) {
    const src = this.sources.find((s) => s.id === e.source || s.name === e.source);
    const name = (src && src.name) || e.source;
    if (e.kind === 'remote') return `${name}://${e.dir || '/'}`;
    if (e.kind === 's3') {
      if (src && src.bucket) return `${name}://${e.dir || ''}`;
      return e.bucket ? `${name}://${e.bucket}/${e.dir || ''}` : `${name}://`;
    }
    return e.dir || '';
  }

  // goHome is the picker's "Home view": a fresh start on the workstation's
  // home folder — the same reset the onboarding picker's choice performs.
  goHome() { this.rebind('local'); }

  // goLast is the picker's "Return to last view": back to where the pane
  // stood when it was opened, through history (Back undoes the jump).
  goLast() {
    if (this.openedLoc) this.go(this.openedLoc);
  }

  // destPop is the Dual-pane button's second act: with the pane already
  // open the button no longer closes it — it offers where the pane should
  // point, anchored under the button. F9 and the pane's × remain the
  // honest closers.
  destPop(btn) {
    const pop = $('pane-dest');
    if (!pop.classList.contains('hidden')) { this.hideDestPop(); btn.focus(); return; }
    this.renderDestPop();
    btn.setAttribute('aria-expanded', 'true');
    pop.style.left = '0px';
    pop.style.top = '0px';
    pop.classList.remove('hidden');
    // anchor below the button, clamped into the window; flip above when
    // the picker would not fit
    const r = btn.getBoundingClientRect();
    const w = pop.offsetWidth, h = pop.offsetHeight;
    const left = Math.min(Math.max(8, r.left), Math.max(8, innerWidth - w - 8));
    let top = r.bottom + 6;
    if (top + h > innerHeight - 8) top = Math.max(8, r.top - h - 6);
    pop.style.left = `${Math.round(left)}px`;
    pop.style.top = `${Math.round(top)}px`;
    requestAnimationFrame(() => pop.classList.add('open'));
    pop.querySelector('.dest-opt:not(:disabled)')?.focus();
  }

  // renderDestPop rebuilds the picker from the pane's state: the Home view
  // always; the last view when one was captured (else it rests disabled).
  renderDestPop() {
    const last = this.openedLoc;
    const pop = $('pane-dest');
    pop.setAttribute('aria-label', t('pane.emptyTitle'));
    pop.replaceChildren(
      this.destOpt(srcIconEl('local'), t('pane.popHome'), t('pane.popHomeSub'), () => this.goHome()),
      this.destOpt(arrowIconEl(), t('pane.popLast'),
        last ? t('pane.popLastSub', { p: this.locCanonical(last) }) : t('pane.popLastNone'),
        () => this.goLast(), !last),
    );
  }

  // destOpt builds one picker row: icon tile, title, subtitle.
  destOpt(icon, title, sub, act, disabled = false) {
    const b = el('button', { type: 'button', class: 'dest-opt', role: 'menuitem' });
    if (disabled) b.disabled = true;
    b.append(el('span', { class: 'dest-ic', 'aria-hidden': 'true' }, icon),
      el('span', { class: 'dest-tx' },
        el('span', { class: 'dest-title', text: title }),
        el('span', { class: 'dest-sub', text: sub })));
    b.onclick = () => { this.hideDestPop(); act(); };
    return b;
  }

  hideDestPop() {
    const pop = $('pane-dest');
    if (pop.classList.contains('hidden')) return;
    pop.classList.add('hidden');
    pop.classList.remove('open');
    pop.replaceChildren();
    for (const id of ['btn-panes', 'local-btn-panes']) $(id)?.removeAttribute('aria-expanded');
  }

  // ---------- empty / loading / error states ----------

  // showEmpty paints the pane's information panel (title/sub/actions) —
  // the twin of main's showEmpty; the onboarding picker hides whenever a
  // plain message owns the area.
  showEmpty(title, sub, actions = []) {
    const empty = $('local-empty');
    this.panelIdle();
    $('local-empty-picker').classList.add('hidden');
    $('local-empty-title').textContent = title;
    $('local-empty-sub').textContent = sub || '';
    $('local-empty-actions').replaceChildren(...actions);
    empty.classList.remove('hidden');
  }
  hideEmpty() {
    $('local-empty').classList.add('hidden');
    this.panelIdle();
  }

  // panelIdle clears the in-flight decorations (skeleton rows, dimming).
  panelIdle() {
    const empty = $('local-empty');
    empty.classList.remove('is-loading');
    $('local-load-skel').classList.add('hidden');
  }

  // showOnboarding stands the unbound pane's empty state up: what the pane
  // is for, and the source picker that binds it.
  showOnboarding() {
    this.bound = false;
    this.upWanted = false;
    this.grid.setRows([]);
    this.showEmpty(t('pane.emptyTitle'), t('pane.emptySub'), []);
    $('local-empty-picker').classList.remove('hidden');
    this.renderSources();
    this.updateCrumb();
    this.updateStatus();
    this.updateNav();
    this.applyUpbar();
  }
  hideOnboarding() {
    $('local-empty').classList.add('hidden');
    $('local-empty-picker').classList.add('hidden');
  }

  // setLoading marks the pane mid-listing: skeleton rows own the area and
  // the previous rows dim (honest about being stale, never blank).
  setLoading(on) {
    $('local-pane').classList.toggle('is-loading', on);
    if (!on) { this.panelIdle(); return; }
    $('local-status').textContent = '\u2026'; // ellipsis until the real totals land
    $('local-empty-picker').classList.add('hidden');
    $('local-empty-title').textContent = t('loading');
    $('local-empty-sub').textContent = '';
    $('local-empty-actions').replaceChildren();
    $('local-load-skel').classList.remove('hidden');
    const empty = $('local-empty');
    empty.classList.add('is-loading');
    empty.classList.remove('hidden');
  }

  // listFail is the shared failure exit of every listing: with rows on
  // screen the failure stays a toast (main wires on.openFail); on an empty
  // pane it becomes the error panel with a retry.
  listFail(err) {
    this.setLoading(false);
    if (this.grid.rows.length) { this.on.openFail?.(err); return; }
    this.showEmpty(String(err?.message || err), '', [
      el('button', { class: 'btn primary', text: t('retry'), onclick: () => this.refresh() }),
    ]);
  }

  // settled is the shared landing of every complete listing: rows are
  // already in the grid — the crumb, status, nav buttons, parent row and
  // the remembered location follow.
  settled() {
    this.setLoading(false);
    this.hideOnboarding();
    this.hasListed = true;
    if (!this.grid.rows.length) this.showEmpty(t('emptyFolder'), t('emptyFolderSub'), []);
    else this.hideEmpty();
    this.updateCrumb();
    this.updateStatus();
    this.updateNav();
    this.landUpbar();
    this.consumePendingSelect();
  }

  // ---------- listings ----------

  async navigate(dir) {
    if (this.binding.kind === 'remote') return this.navigateRemote(dir);
    if (this.binding.kind === 's3') return this.navigateS3({ bucket: this.bucket, prefix: dir || '' });
    const seq = ++this.navSeq;
    this.setLoading(true);
    try {
      const target = dir === '' || dir === '~' ? await app().LocalHome() : dir;
      const ents = await app().ListLocal(target);
      if (seq !== this.navSeq) return; // superseded — user navigated on
      this.dir = target;
      this.grid.setRows(ents.map((e) => ({
        name: e.name,
        key: e.path,
        path: e.path,
        isDir: e.isDir,
        size: e.size,
        modTime: e.modTime,
        created: e.created || null,
        mode: e.mode || null,
      })));
      this.settled();
    } catch (e) {
      if (seq === this.navSeq) this.listFail(e);
    }
  }

  // navigateRemote lists one directory of the bound remote source; rows
  // carry the same shape as the main grid's remote views.
  async navigateRemote(dir) {
    const seq = ++this.navSeq;
    this.setLoading(true);
    try {
      const path = dir && dir !== '~' ? dir : '/';
      const ents = await app().RemoteList(this.binding.source, path);
      if (seq !== this.navSeq) return; // superseded — user navigated on
      this.dir = path;
      this.grid.setRows(ents.map((e) => ({
        name: e.name,
        key: e.key,
        path: e.key,
        isDir: e.isDir,
        size: e.size,
        lastModified: e.lastModified,
        created: e.created || null,
        mode: e.mode || null,
      })));
      this.settled();
    } catch (e) {
      if (seq === this.navSeq) this.listFail(e);
    }
  }

  // cancelS3Stream invalidates the running S3 listing (navigation moved on).
  cancelS3Stream() {
    this.s3Seq++;
    this.s3Off?.();
    this.s3Off = null;
  }

  // navigateS3 lists the bound S3 source: the buckets view at the root,
  // one streamed prefix inside a bucket. Same one-page memory contract as
  // the main view.
  async navigateS3({ bucket, prefix }) {
    this.cancelS3Stream();
    const seq = this.s3Seq;
    this.setLoading(true);
    if (!bucket) {
      try {
        const buckets = await app().ListSourceBuckets(this.binding.source);
        if (seq !== this.s3Seq) return;
        this.bucket = '';
        this.dir = '';
        this.grid.setRows(buckets.map((b) => ({
          name: b.name, key: b.name, bucket: b.name, isDir: true, isBucket: true,
          lastModified: b.createdAt,
          created: b.createdAt,
        })));
        this.settled();
      } catch (e) {
        if (seq === this.s3Seq) this.listFail(e);
      }
      return;
    }
    let token;
    const stream = subscribeStream(
      () => app().ListSourceObjectsStream(this.binding.source, bucket, prefix || ''),
      (p) => {
        if (seq !== this.s3Seq) return; // superseded while pages still arrived
        if (p.error) { this.listFail(p.error); this.cancelS3Stream(); return; }
        this.setLoading(false);
        this.hideOnboarding();
        this.hasListed = true;
        this.grid.appendRows((p.entries || []).map((e) => ({ ...e, bucket })));
        if (!this.painted) { this.painted = true; this.hideEmpty(); this.landUpbar(); this.updateNav(); }
        this.updateStatus();
        if (p.done) {
          this.cancelS3Stream();
          if (!this.grid.rows.length) this.showEmpty(t('emptyFolder'), t('emptyFolderSub'), []);
          this.updateCrumb();
          this.consumePendingSelect();
        }
      },
    );
    this.s3Off = stream.off;
    try {
      token = await stream.begin;
    } catch (e) {
      if (this.s3Off === stream.off) this.s3Off = null;
      if (seq === this.s3Seq) this.listFail(e);
      return;
    }
    if (seq !== this.s3Seq) { app().CancelList(token).catch(() => {}); return; }
    this.bucket = bucket;
    this.dir = prefix || '';
    this.painted = false;
    this.grid.setRows([]);
    this.updateCrumb();
    this.updateStatus();
    stream.flush();
  }

  async refresh() {
    if (!this.bound) return;
    if (this.binding.kind === 's3') return this.navigateS3({ bucket: this.bucket, prefix: this.dir || '' });
    if (this.binding.kind === 'remote') return this.navigateRemote(this.dir || '/');
    if (!this.dir) {
      // the roots view: re-read the drives, then re-paint
      try { this.roots = await app().LocalRoots(); } catch (e) { this.listFail(e); return; }
      this.showRoots();
      return;
    }
    await this.navigate(this.dir);
  }

  // ---------- search: this pane's own scope ----------

  // gotoHit lands a Search-window pick on THIS pane (its find button opens
  // the window scoped to the pane's binding): folders open themselves,
  // files open their parent with the row selected — the twin of main's
  // gotoSearchHit, driving the pane's own bindings and history.
  async gotoHit(r) {
    if (!r?.key || !this.bound) return;
    const key = String(r.key);
    const isDir = r.isDir || key.endsWith('/');
    const b = this.binding;
    if (b.kind === 'local') {
      // local hits carry absolute paths — the pane's own row keys — so the
      // backend's canonical parent calc seats the file's folder
      if (isDir) { this.navigate(key); return; }
      const parent = await app().LocalParent(key);
      this.pendingSelect = { key };
      this.navigate(parent || key);
      return;
    }
    if (b.kind === 'remote') {
      if (isDir) { this.navigateRemote(key); return; }
      const s = key.replace(/\/+$/, '');
      const i = s.lastIndexOf('/');
      this.pendingSelect = { key };
      this.navigateRemote(i <= 0 ? '/' : s.slice(0, i + 1));
      return;
    }
    this.pendingSelect = isDir ? null : { key };
    this.navigateS3({ bucket: r.bucket || b.bucket, prefix: isDir ? key : parentPrefix(key) });
  }

  // consumePendingSelect focuses a row requested by gotoHit once its
  // listing landed (the pane's twin of main's consumePendingSelect) —
  // settled() for local/remote, the stream's done page for S3.
  consumePendingSelect() {
    if (!this.pendingSelect) return;
    const key = this.pendingSelect.key;
    this.pendingSelect = null;
    const idx = this.grid.rows.findIndex((row) => row.key === key);
    if (idx < 0) return;
    this.grid.sel = new Set([key]);
    this.grid.focusKey = key;
    this.grid.anchorKey = key;
    this.grid.scrollTo(idx);
    this.grid.render(true);
  }

  async up() {
    if (this.binding.kind === 'remote') {
      const p = (this.dir || '/').replace(/\/+$/, '');
      if (p === '' || p === '/') return; // already at the source root
      const i = p.lastIndexOf('/');
      this.go({ kind: 'remote', source: this.binding.source, dir: i <= 0 ? '/' : p.slice(0, i + 1) });
      return;
    }
    if (this.binding.kind === 's3') {
      if (!this.bucket) return; // already at the buckets view
      const p = (this.dir || '').replace(/\/+$/, '');
      if (!p) {
        // a bucket-scoped source's root has nothing above it
        if (!this.binding.bucket) this.go({ kind: 's3', source: this.binding.source, bucket: '', dir: '' });
        return;
      }
      const i = p.lastIndexOf('/');
      this.go({ kind: 's3', source: this.binding.source, bucket: this.bucket, dir: i <= 0 ? '' : p.slice(0, i + 1) });
      return;
    }
    if (!this.dir) return;
    const parent = await app().LocalParent(this.dir);
    if (parent === '') { this.go({ kind: 'local', dir: '' }); return; }
    this.go({ kind: 'local', dir: parent });
  }

  // showRoots lists drives / "/" when already at a filesystem root.
  showRoots() {
    this.dir = '';
    this.grid.setRows(this.roots.map((p) => ({ name: p, key: p, path: p, isDir: true })));
    this.settled();
  }

  // paneCanonical is the pane's twin of main's canonicalPath: the
  // one-line path the path editor holds. Source bindings speak
  // NAME://content ('' while unbound); the local binding's canonical
  // form is the raw directory itself ('' at the filesystem-roots view),
  // so the editor always hands back what it was given.
  paneCanonical() {
    if (!this.bound) return '';
    const name = this.binding.name || this.binding.source || '';
    if (this.binding.kind === 'remote') return `${name}://${this.dir || '/'}`;
    if (this.binding.kind === 's3') {
      if (this.binding.bucket) return `${name}://${this.dir || ''}`;
      return this.bucket ? `${name}://${this.bucket}/${this.dir || ''}` : `${name}://`;
    }
    return this.dir || '';
  }

  // editPath swaps the breadcrumb for a one-line editable field holding
  // the canonical path — the twin of the main pane's editor: copy out,
  // paste in, Enter navigates (any source's NAME:// path rebinds the
  // pane; a bare directory navigates the local binding), Esc cancels.
  // An unbound pane opens the editor empty — a pasted path is one more
  // way past the onboarding picker.
  editPath() {
    const bc = $('local-crumb');
    if (bc.querySelector('input.path-edit')) return;
    const restore = () => this.updateCrumb();
    const inp = el('input', { type: 'text', class: 'filter path-edit', spellcheck: 'false' });
    inp.value = this.paneCanonical();
    bc.replaceChildren(inp);
    inp.focus();
    inp.select();
    inp.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        const entry = this.parsePanePath(inp.value);
        if (entry) {
          if (entry.kind === 'local' && !this.bound) this.bindTo(null);
          this.go(entry);
        } else {
          toast(t('pathInvalid', { p: inp.value.trim() || '?' }), 'error');
          inp.focus();
        }
      } else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        restore();
      }
    });
    inp.addEventListener('blur', restore);
  }

  // parsePanePath maps an edited line to a pane history entry: a
  // NAME:// path matches any configured source through parseSourcePath
  // (rebinding the pane when the scheme names a different one), and a
  // bare directory navigates the local binding. Returns null when
  // nothing matches.
  parsePanePath(str) {
    const parsed = parseSourcePath(str, this.sources);
    if (parsed) {
      const { loc } = parsed;
      if (loc.kind === 'remote') return { kind: 'remote', source: loc.source, dir: loc.path || '/' };
      return { kind: 's3', source: loc.source, bucket: loc.bucket || '', dir: loc.prefix || '' };
    }
    const v = String(str || '').trim();
    if (v && this.binding.kind === 'local') return { kind: 'local', dir: v };
    return null;
  }

  setCompare(rows) { this.grid.setCmp(aggregateCompare(rows)); }
  clearCompare() { this.grid.setCmp(null); }

  // ---------- parent row ----------

  // parentPossible is the current listing's parent-row verdict — the same
  // rules the main pane applies per kind: nothing above a source's top
  // level (the buckets view, a bucket-scoped source's root, the local
  // roots view).
  parentPossible() {
    if (!this.bound) return false;
    if (this.binding.kind === 'remote') return !!(this.dir && this.dir !== '/');
    if (this.binding.kind === 's3') {
      if (!this.bucket) return false;
      if (!this.dir) return !this.binding.bucket;
      return true;
    }
    return !!this.dir;
  }
  landUpbar() {
    this.upWanted = this.parentPossible();
    this.applyUpbar();
  }
  // applyUpbar re-seats the row against the shared setting — called after
  // every landing and from main's settings apply.
  applyUpbar() {
    const on = this.upWanted && parentRowOn();
    $('local-upbar').classList.toggle('hidden', !on);
    if (on) this.syncUpbarLayout();
  }
  reseatUpbar() { this.applyUpbar(); }

  // syncUpbarLayout mirrors the pane grid's live column template onto its
  // parent row (the twin of main's syncUpbarLayout): same grid columns,
  // the name cell seated in whatever column "name" currently occupies.
  syncUpbarLayout() {
    const head = $('local-grid-head');
    const tpl = head.style.gridTemplateColumns;
    if (!tpl) return;
    const up = $('local-upbar');
    up.style.gridTemplateColumns = tpl;
    const cols = Array.from(head.querySelectorAll('.gh[data-col]'));
    const i = cols.findIndex((c) => c.dataset.col === 'name');
    const cell = up.querySelector('.gc.name');
    if (i >= 0 && cell) cell.style.gridColumn = String(i + 2);
  }

  // ---------- status / crumb / commands ----------

  // cmdAdapter is the per-pane view commands.js greys the pane toolbar
  // from — the same contract the main view's location satisfies there.
  cmdAdapter() {
    const pane = this;
    return {
      kind: pane.binding.kind,
      bound: pane.bound,
      dir: pane.dir,
      bucket: pane.bucket,
      selCount: () => pane.grid.selectedRows().length,
      canBack: () => pane.canBack(),
      canForward: () => pane.canForward(),
    };
  }

  // updateCrumb renders the pane's interactive breadcrumb (the twin of
  // main's renderBreadcrumb): the source root segment (icon + name, click
  // = the source's opening view), then bucket and path segments, each
  // click navigating through the pane's history; the current tail wears
  // .current. The local binding gets a "This PC" root + path segments.
  updateCrumb() {
    const bc = $('local-crumb');
    bc.replaceChildren();
    bc.title = '';
    if (!this.bound) return;
    const name = this.binding.name || this.binding.source || '';
    const s = this.sources.find((x) => x.id === this.binding.source || x.name === this.binding.source);
    if (this.binding.kind === 'remote') {
      bc.title = name + '://' + (this.dir || '/');
      const root = el('span', { class: 'crumb' + ((!this.dir || this.dir === '/') ? ' current' : ''), title: name + '://' },
        srcIconEl(s?.type, s?.color), name);
      root.onclick = () => this.go({ kind: 'remote', source: this.binding.source, dir: '/' });
      bc.appendChild(root);
      let acc = '';
      for (const part of String(this.dir || '').split('/')) {
        if (!part) continue;
        acc += part + '/';
        bc.appendChild(el('span', { class: 'crumb-sep', text: '\u203A' }));
        const c = el('span', { class: 'crumb', text: part });
        const target = acc;
        c.onclick = () => this.go({ kind: 'remote', source: this.binding.source, dir: target });
        bc.appendChild(c);
      }
      bc.lastChild?.classList.add('current');
      this.persistLoc();
      return;
    }
    if (this.binding.kind === 's3') {
      const scoped = !!this.binding.bucket;
      bc.title = scoped
        ? name + '://' + (this.dir || '')
        : (this.bucket ? name + '://' + this.bucket + '/' + (this.dir || '') : name + '://');
      const root = el('span', { class: 'crumb' + ((scoped && !this.dir) ? ' current' : ''), title: name + '://' },
        srcIconEl(s?.type || 's3', s?.color), name);
      root.onclick = () => this.go(scoped
        ? { kind: 's3', source: this.binding.source, bucket: this.binding.bucket, dir: '' }
        : { kind: 's3', source: this.binding.source, bucket: '', dir: '' });
      bc.appendChild(root);
      if (this.bucket && !scoped) {
        bc.appendChild(el('span', { class: 'crumb-sep', text: '\u203A' }));
        const b = el('span', { class: 'crumb' + (!this.dir ? ' current' : ''), text: this.bucket });
        b.onclick = () => this.go({ kind: 's3', source: this.binding.source, bucket: this.bucket, dir: '' });
        bc.appendChild(b);
      }
      let acc = '';
      for (const part of String(this.dir || '').split('/')) {
        if (!part) continue;
        acc += part + '/';
        bc.appendChild(el('span', { class: 'crumb-sep', text: '\u203A' }));
        const c = el('span', { class: 'crumb', text: part });
        const target = acc;
        c.onclick = () => this.go({ kind: 's3', source: this.binding.source, bucket: this.bucket, dir: target });
        bc.appendChild(c);
      }
      if (scoped || this.bucket) bc.lastChild?.classList.add('current');
      this.persistLoc();
      return;
    }
    // local: "This PC" root + the path's own segments
    bc.title = this.dir || 'Filesystem roots';
    const root = el('span', { class: 'crumb' + (!this.dir ? ' current' : ''), text: 'This PC' });
    root.onclick = () => this.go({ kind: 'local', dir: '' });
    bc.appendChild(root);
    const dir = String(this.dir || '');
    const marks = [];
    for (let i = 0; i < dir.length; i++) {
      if (dir[i] === '/' || dir[i] === '\\') marks.push(i);
    }
    let prev = 0;
    for (const m of marks) {
      const part = dir.slice(prev, m);
      prev = m + 1;
      if (!part) continue;
      bc.appendChild(el('span', { class: 'crumb-sep', text: '\u203A' }));
      const c = el('span', { class: 'crumb', text: part });
      const target = dir.slice(0, m + 1);
      c.onclick = () => this.go({ kind: 'local', dir: target });
      bc.appendChild(c);
    }
    const last = dir.slice(prev);
    if (last) {
      bc.appendChild(el('span', { class: 'crumb-sep', text: '\u203A' }));
      bc.appendChild(el('span', { class: 'crumb current', text: last }));
    } else {
      bc.lastChild?.classList.add('current');
    }
    this.persistLoc();
  }

  // persistLoc remembers the pane's current location (the binding key
  // alone remembers only the source) — s3b-side-loc, restored on reopen.
  persistLoc() {
    if (!this.bound) return;
    try { localStorage.setItem('s3b-side-loc', JSON.stringify(this.snapshot())); } catch { /* quota — the source key still holds */ }
  }

  // updateNav greys the pane's back/forward buttons from its own history.
  updateNav() {
    $('local-btn-back').disabled = !this.canBack();
    $('local-btn-forward').disabled = !this.canForward();
  }

  updateStatus() {
    const rows = this.grid.rows;
    const files = rows.filter((r) => !r.isDir);
    const bytes = files.reduce((s, r) => s + (r.size || 0), 0);
    const sel = this.grid.selectedRows().length;
    $('local-status').textContent = sel
      ? sel + ' selected of ' + rows.length
      : (rows.length - files.length) + ' folder(s), ' + files.length + ' file(s), ' + fmtBytes(bytes);
    const tag = $('local-source');
    if (this.bound) {
      tag.textContent = this.binding.kind === 'local' ? 'This PC' : (this.binding.name || this.binding.source);
      tag.classList.remove('hidden');
    } else {
      tag.classList.add('hidden');
    }
    updateCommandState();
  }
}
