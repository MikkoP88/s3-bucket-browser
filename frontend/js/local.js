// Local pane for the dual-pane (WinSCP-style) view: a second, prefixed Grid
// over the workstation filesystem with synchronized browsing, directory
// compare decorations and cross-pane drag & drop (uploads land on folder
// rows of the remote pane, downloads on folder rows / the body here).
import { Grid } from './grid.js';
import { prompt } from './dialogs.js';
import { fmtBytes } from './util.js';
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
    this.dir = '';        // '' = filesystem roots view
    this.roots = [];
    this.sync = localStorage.getItem('s3b-local-sync') === '1';
    this.syncBase = null; // {dir, prefix} captured when sync is enabled
    this.on = {};         // callbacks: dropFolder, dropBody, compare, syncBase, syncUp, openFail

    this.grid.on.activate = (m) => {
      if (m.isDir) { this.navigate(m.path); return; }
      app().OpenLocal(m.path).catch((e) => this.on.openFail?.(e));
    };
    this.grid.on.select = () => this.updateStatus();
    // dropping a remote selection onto a local folder row = download into it
    this.grid.on.drop = (folder, payload) => this.on.dropFolder?.(folder, payload);

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
      this.on.dropBody?.(JSON.parse(data));
    });

    body.addEventListener('mousedown', (e) => {
      if (e.target === body || e.target === this.grid.canvas) this.grid.clearSelection();
    });
    // empty-area right-click (rows keep the grid's row menu off — the local
    // pane has no per-row menu yet; main wires contextEmpty)
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
    $('local-home').onclick = () => this.navigate('');
    $('local-refresh').onclick = () => this.refresh();
    $('local-crumb').onclick = () => this.promptPath();
    const sync = $('local-sync');
    sync.checked = this.sync;
    sync.onchange = () => this.setSync(sync.checked);
    $('local-compare').onclick = () => this.on.compare?.();
  }

  async start() {
    try {
      this.roots = await app().LocalRoots();
      if (this.sync && !this.syncBase) this.syncBase = this.on.syncBase?.() || null;
      await this.navigate(await app().LocalHome());
    } catch (e) {
      this.on.openFail?.(e);
    }
  }

  show() {
    $('local-pane').classList.remove('hidden');
    if (!this.dir) this.start();
  }
  hide() { $('local-pane').classList.add('hidden'); }
  get visible() { return !$('local-pane').classList.contains('hidden'); }

  async navigate(dir, fromSync = false) {
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

  async refresh() {
    if (this.dir) await this.navigate(this.dir, true);
    else await this.start();
  }

  async up() {
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
    const p = await prompt({ title: 'Local folder', label: 'Path', value: this.dir || '' });
    if (p) this.navigate(p);
  }

  // setSync toggles synchronized browsing; the base pair (local dir + remote
  // prefix) is captured on enable and both panes follow each other from there.
  setSync(on) {
    this.sync = on;
    localStorage.setItem('s3b-local-sync', on ? '1' : '0');
    this.syncBase = on ? (this.on.syncBase?.() || null) : null;
  }

  // syncTo follows a remote prefix change (relative to the sync base).
  syncTo(prefix) {
    if (!this.sync || !this.syncBase) return;
    const rel = (prefix || '').slice(this.syncBase.prefix.length).replace(/^\/+/, '');
    const base = this.syncBase.dir.replace(/[\\/]+$/, '');
    const dir = rel ? `${base}/${rel}` : this.syncBase.dir;
    if (dir !== this.dir) this.navigate(dir, true);
  }

  // remotePrefixFor computes the remote prefix matching a local dir, or null
  // when the dir lies outside the sync base (remote stays put).
  remotePrefixFor(dir) {
    if (!this.syncBase) return null;
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
