// Side pane for the dual-pane (WinSCP-style) view: a second, prefixed Grid
// that binds to the workstation filesystem, any remote (non-S3) source, or
// any S3 source (buckets + prefixes), with synchronized browsing (local
// only), directory compare decorations and cross-pane drag & drop (uploads
// land on folder rows of the remote/S3 pane, downloads on folder rows /
// the body here).
import { Grid } from './grid.js';
import { prompt } from './dialogs.js';
import { el, fmtBytes } from './util.js';
import { api, onEvent } from './api.js';

const app = () => api;
const $ = (id) => document.getElementById(id);

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

export class LocalPane {
  constructor() {
    this.grid = new Grid('local-');
    this.dir = '';        // local: '' = filesystem roots view; remote: anchored path; s3: prefix
    this.bucket = '';     // s3 binding: the listed bucket ('' = buckets view)
    this.s3Seq = 0;       // s3 listing stream generation
    this.s3Off = null;    // s3 listing page-event unsubscribe
    this.roots = [];
    this.binding = { kind: 'local', source: '' }; // what the pane is bound to
    this.sources = [];    // [{id,name,type,default}] fed by main after ListSources
    this.sync = localStorage.getItem('s3b-local-sync') === '1';
    this.syncBase = null; // {dir, prefix} captured when sync is enabled
    this.on = {};         // callbacks: dropFolder, dropBody, compare, syncBase, syncUp, openFail, activateRemoteFile, activateS3File

    this.grid.on.activate = (m) => {
      if (this.binding.kind === 's3') {
        if (m.isBucket) { this.navigateS3({ bucket: m.key, prefix: '' }); return; }
        if (m.isDir) { this.navigateS3({ bucket: this.bucket, prefix: m.key }); return; }
        this.on.activateS3File?.(this.bucket, m);
        return;
      }
      if (m.isDir) { this.navigate(this.binding.kind === 'remote' ? m.key : m.path); return; }
      if (this.binding.kind === 'remote') { this.on.activateRemoteFile?.(this.binding.source, m); return; }
      app().OpenLocal(m.path).catch((e) => this.on.openFail?.(e));
    };
    this.grid.on.select = () => this.updateStatus();
    // dropping a remote/S3 selection onto a local folder row = download into it
    this.grid.on.drop = (folder, payload, e) => this.on.dropFolder?.(folder, payload, e);

    // dropping onto the pane body = transfer into the current directory.
    // Local bindings take remote/S3 downloads only; remote and S3 bindings
    // also take local-pane uploads (the body is one big drop target).
    const body = this.grid.body;
    const bodyMimes = () => (this.binding.kind === 'local'
      ? ['application/x-s3b']
      : ['application/x-s3b', 'application/x-s3b-local']);
    body.addEventListener('dragover', (e) => {
      if (this.dir && bodyMimes().some((t) => e.dataTransfer.types.includes(t))) {
        e.preventDefault();
        body.classList.add('drop-target');
      }
    });
    body.addEventListener('dragleave', (e) => {
      if (e.target === body || e.target === this.grid.canvas) body.classList.remove('drop-target');
    });
    body.addEventListener('drop', (e) => {
      body.classList.remove('drop-target');
      if (!this.dir) return;
      let data = null;
      for (const t of bodyMimes()) {
        const d = e.dataTransfer.getData(t);
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
      if (e.key === 'Backspace') { e.preventDefault(); this.up(); return; }
      if (this.grid.keydown(e)) e.preventDefault();
    });

    $('local-up').onclick = () => this.up();
    $('local-home').onclick = () => this.home();
    $('local-refresh').onclick = () => this.refresh();
    $('local-crumb').onclick = () => this.promptPath();
    const sync = $('local-sync');
    sync.checked = this.sync;
    sync.onchange = () => this.setSync(sync.checked);
    $('local-compare').onclick = () => this.on.compare?.();
    $('local-src').onchange = () => this.rebind($('local-src').value);
  }

  // ---------- source binding (M10 panels v2, M11 S3 sources) ----------

  // renderSources fills the header dropdown: the workstation filesystem
  // plus every configured source — remote engines and S3 sources alike
  // (S3 sources browse their buckets/prefixes; transfers route through
  // the cross-source engine with the source pinned).
  renderSources() {
    const sel = $('local-src');
    const opts = [el('option', { value: 'local', text: 'Local' })];
    for (const s of this.sources) {
      opts.push(el('option', { value: s.id || s.name, text: `${s.name} (${s.type})` }));
    }
    sel.replaceChildren(...opts);
    const cur = this.binding.kind !== 'local' ? this.binding.source : 'local';
    if ([...sel.options].some((o) => o.value === cur)) sel.value = cur;
  }

  // restoreBinding re-applies the persisted binding once, after the first
  // sources load (later refreshes keep the user's current binding).
  restoreBinding() {
    if (this.bindingApplied) return;
    this.bindingApplied = true;
    const saved = localStorage.getItem('s3b-side-src');
    const ok = saved && saved !== 'local'
      && this.sources.some((s) => (s.id || s.name) === saved);
    this.rebind(ok ? saved : 'local');
  }

  // rebind switches the pane to the workstation filesystem, a remote
  // source, or an S3 source and navigates to its root.
  rebind(value) {
    if (value === 'local') {
      this.cancelS3Stream();
      this.binding = { kind: 'local', source: '' };
      localStorage.setItem('s3b-side-src', 'local');
      this.applyDragPayload();
      this.updateSyncUi();
      this.dir = '';
      this.bucket = '';
      this.start();
      return;
    }
    const src = this.sources.find((s) => (s.id || s.name) === value);
    if (!src) return;
    if (src.type === 's3') {
      this.cancelS3Stream();
      this.binding = { kind: 's3', source: src.id || src.name };
      localStorage.setItem('s3b-side-src', this.binding.source);
      this.applyDragPayload();
      this.updateSyncUi();
      this.bucket = '';
      this.dir = '';
      this.navigateS3({ bucket: '', prefix: '' });
      return;
    }
    this.binding = { kind: 'remote', source: src.id || src.name };
    localStorage.setItem('s3b-side-src', this.binding.source);
    this.applyDragPayload();
    this.updateSyncUi();
    this.dir = '/';
    this.navigateRemote('/');
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

  // updateSyncUi: synchronized browsing maps local dirs to S3 prefixes —
  // it only exists for the local binding.
  updateSyncUi() {
    const local = this.binding.kind === 'local';
    const sync = $('local-sync');
    sync.disabled = !local;
    if (!local && sync.checked) this.setSync(false);
  }

  async start() {
    if (this.binding.kind === 'remote') return this.navigateRemote(this.dir || '/');
    if (this.binding.kind === 's3') return this.navigateS3({ bucket: '', prefix: '' });
    try {
      this.roots = await app().LocalRoots();
      if (this.sync && !this.syncBase) this.syncBase = this.on.syncBase?.() || null;
      await this.navigate(await app().LocalHome());
    } catch (e) {
      this.on.openFail?.(e);
    }
  }

  home() {
    if (this.binding.kind === 'remote') return this.navigate('/');
    if (this.binding.kind === 's3') return this.navigateS3({ bucket: '', prefix: '' });
    this.navigate('');
  }

  show() {
    $('local-pane').classList.remove('hidden');
    if (!this.dir) this.start();
  }
  hide() { $('local-pane').classList.add('hidden'); }
  get visible() { return !$('local-pane').classList.contains('hidden'); }

  async navigate(dir, fromSync = false) {
    if (this.binding.kind === 'remote') return this.navigateRemote(dir, fromSync);
    if (this.binding.kind === 's3') return this.navigateS3({ bucket: this.bucket, prefix: dir || '' });
    try {
      const target = dir === '' || dir === '~' ? await app().LocalHome() : dir;
      const ents = await app().ListLocal(target);
      this.dir = target;
      this.grid.setRows(ents.map((e) => ({
        name: e.name,
        key: e.path,
        path: e.path,
        isDir: e.isDir,
        size: e.size,
        modTime: e.modTime,
      })));
      this.updateCrumb();
      this.updateStatus();
      if (this.sync && !fromSync) this.on.syncUp?.(this.dir);
    } catch (e) {
      this.on.openFail?.(e);
    }
  }

  // navigateRemote lists one directory of the bound remote source; rows
  // carry the same shape as the main grid's remote views.
  async navigateRemote(dir, fromSync = false) {
    try {
      const path = dir && dir !== '~' ? dir : '/';
      const ents = await app().RemoteList(this.binding.source, path);
      this.dir = path;
      this.grid.setRows(ents.map((e) => ({
        name: e.name,
        key: e.key,
        path: e.key,
        isDir: e.isDir,
        size: e.size,
        lastModified: e.lastModified,
      })));
      this.updateCrumb();
      this.updateStatus();
      if (this.sync && !fromSync) this.on.syncUp?.(this.dir);
    } catch (e) {
      this.on.openFail?.(e);
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
  // the main view (PLAN.md §13).
  async navigateS3({ bucket, prefix }) {
    this.cancelS3Stream();
    const seq = this.s3Seq;
    if (!bucket) {
      try {
        const buckets = await app().ListSourceBuckets(this.binding.source);
        if (seq !== this.s3Seq) return;
        this.bucket = '';
        this.dir = '';
        this.grid.setRows(buckets.map((b) => ({
          name: b.name, key: b.name, bucket: b.name, isDir: true, isBucket: true,
          lastModified: b.createdAt,
        })));
        this.updateCrumb();
        this.updateStatus();
      } catch (e) {
        this.on.openFail?.(e);
      }
      return;
    }
    let token;
    try {
      token = await app().ListSourceObjectsStream(this.binding.source, bucket, prefix || '');
    } catch (e) {
      this.on.openFail?.(e);
      return;
    }
    if (seq !== this.s3Seq) { app().CancelList(token).catch(() => {}); return; }
    this.bucket = bucket;
    this.dir = prefix || '';
    this.grid.setRows([]);
    this.updateCrumb();
    this.s3Off = onEvent('list:page', (p) => {
      if (seq !== this.s3Seq || p.token !== token) return;
      if (p.error) { this.on.openFail?.(p.error); this.cancelS3Stream(); return; }
      this.grid.appendRows((p.entries || []).map((e) => ({ ...e, bucket })));
      this.updateStatus();
      if (p.done) this.cancelS3Stream();
    });
    this.updateStatus();
  }

  async refresh() {
    if (this.dir) await this.navigate(this.dir, true);
    else await this.start();
  }

  async up() {
    if (this.binding.kind === 'remote') {
      const p = (this.dir || '/').replace(/\/+$/, '');
      if (p === '' || p === '/') return; // already at the source root
      const i = p.lastIndexOf('/');
      this.navigate(i <= 0 ? '/' : p.slice(0, i + 1));
      return;
    }
    if (this.binding.kind === 's3') {
      if (!this.bucket) return; // already at the buckets view
      const p = (this.dir || '').replace(/\/+$/, '');
      if (!p) { this.navigateS3({ bucket: '', prefix: '' }); return; }
      const i = p.lastIndexOf('/');
      this.navigateS3({ bucket: this.bucket, prefix: i <= 0 ? '' : p.slice(0, i + 1) });
      return;
    }
    if (!this.dir) return;
    const parent = await app().LocalParent(this.dir);
    if (parent === '') { this.showRoots(); return; }
    this.navigate(parent);
  }

  // showRoots lists drives / "/" when already at a filesystem root.
  showRoots() {
    this.dir = '';
    this.grid.setRows(this.roots.map((p) => ({ name: p, key: p, path: p, isDir: true })));
    this.updateCrumb();
    this.updateStatus();
  }

  async promptPath() {
    if (this.binding.kind === 'remote') {
      const p = await prompt({ title: `Folder on ${this.binding.source}`, label: 'Path', value: this.dir || '/' });
      if (p) this.navigate(p);
      return;
    }
    if (this.binding.kind === 's3') {
      const p = await prompt({ title: `Path on ${this.binding.source}`, label: 'bucket or bucket/prefix/', value: this.bucket ? `${this.bucket}/${this.dir || ''}` : '' });
      if (!p) return;
      const parts = p.replace(/^\/+|\/+$/g, '').split('/');
      const bucket = parts.shift();
      if (bucket) this.navigateS3({ bucket, prefix: parts.length ? `${parts.join('/')}/` : '' });
      return;
    }
    const p = await prompt({ title: 'Local folder', label: 'Path', value: this.dir || '' });
    if (p) this.navigate(p);
  }

  // setSync toggles synchronized browsing; the base pair (local dir + remote
  // prefix) is captured on enable and both panes follow each other from
  // there. Local binding only.
  setSync(on) {
    if (this.binding.kind !== 'local') on = false;
    this.sync = on;
    localStorage.setItem('s3b-local-sync', on ? '1' : '0');
    $('local-sync').checked = on;
    this.syncBase = on ? (this.on.syncBase?.() || null) : null;
  }

  // syncTo follows a remote prefix change (relative to the sync base).
  syncTo(prefix) {
    if (this.binding.kind !== 'local' || !this.sync || !this.syncBase) return;
    const rel = (prefix || '').slice(this.syncBase.prefix.length).replace(/^\/+/, '');
    const base = this.syncBase.dir.replace(/[\\/]+$/, '');
    const dir = rel ? `${base}/${rel}` : this.syncBase.dir;
    if (dir !== this.dir) this.navigate(dir, true);
  }

  // remotePrefixFor computes the remote prefix matching a local dir, or null
  // when the dir lies outside the sync base (remote stays put).
  remotePrefixFor(dir) {
    if (this.binding.kind !== 'local' || !this.syncBase) return null;
    const base = this.syncBase.dir.replace(/[\\/]+$/, '');
    const d = dir.replace(/[\\/]+$/, '');
    if (d === base) return this.syncBase.prefix;
    if (d.startsWith(base + '/') || d.startsWith(base + '\\')) {
      return this.syncBase.prefix + d.slice(base.length + 1).replace(/\\/g, '/') + '/';
    }
    return null;
  }

  setCompare(rows) { this.grid.setCmp(aggregateCompare(rows)); }
  clearCompare() { this.grid.setCmp(null); }

  updateCrumb() {
    if (this.binding.kind === 'remote') {
      const label = `${this.binding.source}:${this.dir || '/'}`;
      $('local-crumb').textContent = label;
      $('local-crumb').title = label;
      return;
    }
    if (this.binding.kind === 's3') {
      const label = this.bucket
        ? `${this.binding.source}:${this.bucket}/${this.dir || ''}`
        : `${this.binding.source}: (buckets)`;
      $('local-crumb').textContent = label;
      $('local-crumb').title = label;
      return;
    }
    $('local-crumb').textContent = this.dir || 'This PC (filesystem roots)';
    $('local-crumb').title = this.dir || 'Filesystem roots';
  }

  updateStatus() {
    const rows = this.grid.rows;
    const files = rows.filter((r) => !r.isDir);
    const bytes = files.reduce((s, r) => s + (r.size || 0), 0);
    const sel = this.grid.selectedRows().length;
    $('local-status').textContent = sel
      ? `${sel} selected of ${rows.length}`
      : `${rows.length - files.length} folder(s), ${files.length} file(s), ${fmtBytes(bytes)}`;
  }
}
