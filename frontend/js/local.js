// Side pane for the dual-pane (WinSCP-style) view: a second, prefixed Grid
// that binds either to the workstation filesystem or to any remote
// (non-S3) source, with synchronized browsing (local only), directory
// compare decorations and cross-pane drag & drop (uploads land on folder
// rows of the remote pane, downloads on folder rows / the body here).
import { Grid } from './grid.js';
import { prompt } from './dialogs.js';
import { el, fmtBytes } from './util.js';
import { api } from './api.js';

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
    this.dir = '';        // local: '' = filesystem roots view; remote: anchored path
    this.roots = [];
    this.binding = { kind: 'local', source: '' }; // what the pane is bound to
    this.sources = [];    // [{id,name,type}] fed by main after ListSources
    this.sync = localStorage.getItem('s3b-local-sync') === '1';
    this.syncBase = null; // {dir, prefix} captured when sync is enabled
    this.on = {};         // callbacks: dropFolder, dropBody, compare, syncBase, syncUp, openFail, activateRemoteFile

    this.grid.on.activate = (m) => {
      if (m.isDir) { this.navigate(this.binding.kind === 'remote' ? m.key : m.path); return; }
      if (this.binding.kind === 'remote') { this.on.activateRemoteFile?.(this.binding.source, m); return; }
      app().OpenLocal(m.path).catch((e) => this.on.openFail?.(e));
    };
    this.grid.on.select = () => this.updateStatus();
    // dropping a remote selection onto a local folder row = download into it
    this.grid.on.drop = (folder, payload, e) => this.on.dropFolder?.(folder, payload, e);

    // dropping onto the pane body = download into the current directory
    const body = this.grid.body;
    body.addEventListener('dragover', (e) => {
      if (this.dir && e.dataTransfer.types.includes('application/x-s3b')) e.preventDefault();
    });
    body.addEventListener('drop', (e) => {
      if (!this.dir) return;
      const data = e.dataTransfer.getData('application/x-s3b');
      if (!data) return;
      e.preventDefault();
      this.on.dropBody?.(JSON.parse(data), e);
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

  // ---------- source binding (M10 panels v2) ----------

  // renderSources fills the header dropdown: the workstation filesystem
  // plus every remote source (S3 sources browse in the main view — their
  // object operations route through the default profile).
  renderSources() {
    const sel = $('local-src');
    const opts = [el('option', { value: 'local', text: 'Local' })];
    for (const s of this.sources) {
      if (s.type === 's3') continue;
      opts.push(el('option', { value: s.id || s.name, text: `${s.name} (${s.type})` }));
    }
    sel.replaceChildren(...opts);
    const cur = this.binding.kind === 'remote' ? this.binding.source : 'local';
    if ([...sel.options].some((o) => o.value === cur)) sel.value = cur;
  }

  // restoreBinding re-applies the persisted binding once, after the first
  // sources load (later refreshes keep the user's current binding).
  restoreBinding() {
    if (this.bindingApplied) return;
    this.bindingApplied = true;
    const saved = localStorage.getItem('s3b-side-src');
    const ok = saved && saved !== 'local'
      && this.sources.some((s) => (s.id || s.name) === saved && s.type !== 's3');
    this.rebind(ok ? saved : 'local');
  }

  // rebind switches the pane to the workstation filesystem or a remote
  // source and navigates to its root.
  rebind(value) {
    if (value === 'local') {
      this.binding = { kind: 'local', source: '' };
      localStorage.setItem('s3b-side-src', 'local');
      this.applyDragPayload();
      this.updateSyncUi();
      this.dir = '';
      this.start();
      return;
    }
    const src = this.sources.find((s) => (s.id || s.name) === value && s.type !== 's3');
    if (!src) return;
    this.binding = { kind: 'remote', source: src.id || src.name };
    localStorage.setItem('s3b-side-src', this.binding.source);
    this.applyDragPayload();
    this.updateSyncUi();
    this.dir = '/';
    this.navigateRemote('/');
  }

  // applyDragPayload: a remote-bound pane drags remote-origin payloads
  // (source + dir + keys/entries) so every drop target of the transfer
  // matrix accepts them; the local binding keeps the prototype payload
  // ({paths}).
  applyDragPayload() {
    if (this.binding.kind === 'remote') {
      const b = this.binding;
      this.grid.dragPayload = () => {
        const rows = this.grid.selectedRows();
        return {
          source: b.source, dir: this.dir || '/',
          keys: rows.map((r) => r.key),
          entries: rows.map((r) => ({ key: r.key, size: r.size || 0, isDir: !!r.isDir })),
        };
      };
    } else {
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
    try {
      this.roots = await app().LocalRoots();
      if (this.sync && !this.syncBase) this.syncBase = this.on.syncBase?.() || null;
      await this.navigate(await app().LocalHome());
    } catch (e) {
      this.on.openFail?.(e);
    }
  }

  home() {
    this.navigate(this.binding.kind === 'remote' ? '/' : '');
  }

  show() {
    $('local-pane').classList.remove('hidden');
    if (!this.dir) this.start();
  }
  hide() { $('local-pane').classList.add('hidden'); }
  get visible() { return !$('local-pane').classList.contains('hidden'); }

  async navigate(dir, fromSync = false) {
    if (this.binding.kind === 'remote') return this.navigateRemote(dir, fromSync);
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
