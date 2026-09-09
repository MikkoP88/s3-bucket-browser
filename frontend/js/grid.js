// Virtualized, sortable, multi-select details grid.
// Renders only the visible slice (+overscan) so a 100k-object folder scrolls
// at full frame rate (PLAN.md §11 performance budget).
import { el, fmtBytes, fmtDate, fileIcon } from './util.js';

const ROW_H = 28;
const OVERSCAN = 8;

// acceptedMimes lists the drag payload types a pane accepts on folder rows:
// the remote pane takes same-pane moves plus local-pane uploads; the local
// pane takes remote downloads only (local moves are Explorer's job).
function acceptedMimes(kind) {
  return kind === 'remote'
    ? ['application/x-s3b', 'application/x-s3b-local']
    : ['application/x-s3b'];
}

const COLUMNS = [
  { id: 'name', label: 'Name', flex: true },
  { id: 'size', label: 'Size', num: true },
  { id: 'lastModified', label: 'Date modified' },
  { id: 'storageClass', label: 'Storage class' },
];

export class Grid {
  // prefix mounts the grid on <prefix>grid-head/-body/-canvas so two grids
  // can coexist (remote + local dual-pane). kind: 'remote' | 'local' decides
  // the drag payload type: drops land on folder rows of the other pane (the
  // remote pane additionally accepts same-pane moves).
  constructor(prefix = '') {
    this.head = document.getElementById(`${prefix}grid-head`);
    this.body = document.getElementById(`${prefix}grid-body`);
    this.canvas = document.getElementById(`${prefix}grid-canvas`);
    this.kind = prefix ? 'local' : 'remote';
    this.mime = prefix ? 'application/x-s3b-local' : 'application/x-s3b';

    this.all = [];          // model rows (as listed)
    this.rows = [];         // filtered + sorted
    this.sel = new Set();   // selected keys
    this.focusKey = null;
    this.anchorKey = null;
    this.sortKey = 'name';
    this.sortDir = 1;
    this.filter = '';
    this.typeBuf = '';
    this.typeTimer = null;

    this.pool = [];
    this.on = {}; // callbacks: select, activate, context, dragstart, drop
    this.renderHead();
    this.body.addEventListener('scroll', () => this.render());
  }

  // ---------- setup ----------
  renderHead() {
    this.head.replaceChildren(...COLUMNS.map((c) => {
      const ind = el('span', { class: 'sort-ind' });
      if (this.sortKey === c.id) ind.textContent = this.sortDir > 0 ? '\u25B2' : '\u25BC';
      return el('div', {
        class: `gh${c.num ? ' num' : ''}`,
        onclick: () => this.cycleSort(c.id),
      }, el('span', { text: c.label }), ind);
    }));
  }

  cycleSort(id) {
    if (this.sortKey === id) this.sortDir = -this.sortDir;
    else { this.sortKey = id; this.sortDir = 1; }
    if (id === 'name' || id === 'storageClass') this.sortDir = this.sortDir; // lexical
    this.apply();
    this.renderHead();
  }

  // ---------- data ----------
  setRows(rows) {
    this.all = rows;
    this.apply();
  }

  // appendRows extends the model with rows that arrive already in display
  // order (streaming listing, M5). Avoids re-sorting the whole array per
  // page; call apply() once when the stream ends if the sort must change.
  appendRows(rows) {
    if (!rows.length) return;
    this.all.push(...rows);
    if (!this.filter && this.sortKey === 'name' && this.sortDir === 1) {
      this.rows.push(...rows);
      this.render();
    } else {
      this.apply(); // filter or non-default sort: full recompute
    }
  }

  setFilter(f) {
    this.filter = f;
    this.apply();
  }

  apply() {
    let rows = this.all;
    if (this.filter) {
      const q = this.filter.toLowerCase();
      rows = rows.filter((r) => r.name.toLowerCase().includes(q));
    }
    const dir = this.sortDir;
    const key = this.sortKey;
    rows = [...rows].sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1; // folders first, always
      let va = a[key], vb = b[key];
      if (key === 'size' || key === 'lastModified') {
        va = va || 0; vb = vb || 0;
        return (va < vb ? -1 : va > vb ? 1 : 0) * dir;
      }
      va = String(va || '').toLowerCase(); vb = String(vb || '').toLowerCase();
      return (va < vb ? -1 : va > vb ? 1 : 0) * dir;
    });
    this.rows = rows;
    // prune selection to existing rows
    const live = new Set(rows.map((r) => r.key));
    for (const k of this.sel) if (!live.has(k)) this.sel.delete(k);
    this.render(true);
    this.on.select?.(this.selectedRows());
  }

  selectedRows() { return this.rows.filter((r) => this.sel.has(r.key)); }

  rowByKey(key) { return this.rows.find((r) => r.key === key) || null; }

  // setCmp decorates rows with directory-compare statuses (name -> status)
  // and repaints; null clears the decorations.
  setCmp(map) {
    for (const r of this.all) r.cmp = map ? (map.get(r.name) || '') : '';
    this.render();
  }

  selectAll() {
    this.sel = new Set(this.rows.map((r) => r.key));
    this.render(true);
    this.on.select?.(this.selectedRows());
  }

  clearSelection() {
    this.sel.clear();
    this.focusKey = null;
    this.render(true);
    this.on.select?.([]);
  }

  // ---------- rendering ----------
  render(force = false) {
    const total = this.rows.length;
    this.canvas.style.height = `${total * ROW_H}px`;
    const scrollTop = this.body.scrollTop;
    const h = this.body.clientHeight;
    const first = Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN);
    const last = Math.min(total - 1, Math.ceil((scrollTop + h) / ROW_H) + OVERSCAN);

    // recycle pool
    const need = Math.max(0, last - first + 1);
    while (this.pool.length < need) {
      const row = el('div', { class: 'grid-row', draggable: 'true', role: 'option' });
      const nameCell = el('div', { class: 'gc name' }, el('span', { class: 'icon' }), el('span', { class: 'tname' }));
      row.appendChild(nameCell);
      row.appendChild(el('div', { class: 'gc num size' }));
      row.appendChild(el('div', { class: 'gc lastModified' }));
      row.appendChild(el('div', { class: 'gc storageClass' }));
      this.wireRow(row);
      this.pool.push(row);
    }
    for (let i = 0; i < this.pool.length; i++) {
      const row = this.pool[i];
      const idx = first + i;
      if (idx > last) { row.style.display = 'none'; continue; }
      const m = this.rows[idx];
      row.style.display = '';
      row.style.top = `${idx * ROW_H}px`;
      row._model = m;
      row.classList.toggle('sel', this.sel.has(m.key));
      row.classList.toggle('focus', m.key === this.focusKey);
      row.setAttribute('aria-selected', this.sel.has(m.key) ? 'true' : 'false');
      row.dataset.cmp = m.cmp || '';
      const cells = row.children;
      cells[0].children[0].textContent = fileIcon(m.name, m.isDir);
      cells[0].children[1].textContent = m.name;
      cells[1].textContent = m.isDir ? '' : fmtBytes(m.size);
      cells[2].textContent = m.isDir ? '' : fmtDate(m.lastModified || m.modTime);
      cells[3].textContent = m.isDir ? '' : (m.storageClass || '');
    }
    if (force) this.on.select?.(this.selectedRows());
  }

  wireRow(row) {
    row.addEventListener('mousedown', (e) => {
      if (e.button !== 0) return;
      const m = row._model;
      if (e.ctrlKey || e.metaKey) {
        this.sel.has(m.key) ? this.sel.delete(m.key) : this.sel.add(m.key);
      } else if (e.shiftKey && this.anchorKey) {
        const a = this.rows.findIndex((r) => r.key === this.anchorKey);
        const b = this.rows.findIndex((r) => r.key === m.key);
        if (a >= 0 && b >= 0) {
          this.sel.clear();
          for (let i = Math.min(a, b); i <= Math.max(a, b); i++) this.sel.add(this.rows[i].key);
        }
      } else if (!this.sel.has(m.key)) {
        this.sel.clear();
        this.sel.add(m.key);
      }
      this.focusKey = m.key;
      this.anchorKey = m.key;
      this.render();
      this.on.select?.(this.selectedRows());
    });
    row.addEventListener('dblclick', () => this.on.activate?.(row._model));
    row.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      const m = row._model;
      if (!this.sel.has(m.key)) {
        this.sel.clear();
        this.sel.add(m.key);
        this.focusKey = m.key;
        this.render();
        this.on.select?.(this.selectedRows());
      }
      this.on.context?.(e, this.selectedRows());
    });
    row.addEventListener('dragstart', (e) => {
      const m = row._model;
      if (!this.sel.has(m.key)) {
        this.sel.clear();
        this.sel.add(m.key);
        this.render();
      }
      e.dataTransfer.setData(this.mime, JSON.stringify(this.dragPayload()));
      e.dataTransfer.effectAllowed = 'copyMove';
      this.on.dragstart?.(this.selectedRows());
    });
    row.addEventListener('dragover', (e) => {
      if (!row._model?.isDir) return;
      if (!acceptedMimes(this.kind).some((t) => e.dataTransfer.types.includes(t))) return;
      e.preventDefault();
      row.classList.add('drop-target');
    });
    row.addEventListener('dragleave', () => row.classList.remove('drop-target'));
    row.addEventListener('drop', (e) => {
      row.classList.remove('drop-target');
      if (!row._model?.isDir) return;
      let payload = null;
      for (const t of acceptedMimes(this.kind)) {
        const d = e.dataTransfer.getData(t);
        if (d) { payload = JSON.parse(d); break; }
      }
      if (!payload) return;
      e.preventDefault();
      e.stopPropagation();
      this.on.drop?.(row._model, payload, e);
    });
  }

  dragPayload() {
    const rows = this.selectedRows();
    if (this.kind === 'local') return { paths: rows.map((r) => r.path) };
    return { // bucket added by main
      keys: rows.map((r) => r.key),
      entries: rows.map((r) => ({ key: r.key, size: r.size || 0, isDir: !!r.isDir })),
    };
  }

  // ---------- keyboard ----------
  keydown(e) {
    const n = this.rows.length;
    if (!n) return false;
    const idx = Math.max(0, this.rows.findIndex((r) => r.key === this.focusKey));
    let handled = true;
    switch (e.key) {
      case 'ArrowDown': this.moveSel(idx + 1, e); break;
      case 'ArrowUp': this.moveSel(idx - 1, e); break;
      case 'PageDown': this.moveSel(Math.min(n - 1, idx + 20), e); break;
      case 'PageUp': this.moveSel(Math.max(0, idx - 20), e); break;
      case 'Home': this.moveSel(0, e); break;
      case 'End': this.moveSel(n - 1, e); break;
      case 'Enter': {
        const m = this.rowByKey(this.focusKey);
        if (m) this.on.activate?.(m);
        break;
      }
      default:
        if (e.key.length === 1 && !e.ctrlKey && !e.altKey && !e.metaKey) {
          this.typeJump(e.key);
          break;
        }
        handled = false;
    }
    return handled;
  }

  moveSel(idx, e) {
    idx = Math.max(0, Math.min(this.rows.length - 1, idx));
    const m = this.rows[idx];
    if (e.shiftKey && this.anchorKey) {
      const a = this.rows.findIndex((r) => r.key === this.anchorKey);
      if (a >= 0) {
        this.sel.clear();
        for (let i = Math.min(a, idx); i <= Math.max(a, idx); i++) this.sel.add(this.rows[i].key);
      }
    } else if (!e.ctrlKey && !e.metaKey) {
      this.sel.clear();
      this.sel.add(m.key);
      this.anchorKey = m.key;
    }
    this.focusKey = m.key;
    this.scrollTo(idx);
    this.render();
    this.on.select?.(this.selectedRows());
  }

  scrollTo(idx) {
    const top = idx * ROW_H;
    if (top < this.body.scrollTop) this.body.scrollTop = top;
    else if (top + ROW_H > this.body.scrollTop + this.body.clientHeight)
      this.body.scrollTop = top + ROW_H - this.body.clientHeight;
  }

  typeJump(ch) {
    this.typeBuf += ch.toLowerCase();
    clearTimeout(this.typeTimer);
    this.typeTimer = setTimeout(() => { this.typeBuf = ''; }, 600);
    const i = this.rows.findIndex((r) => r.name.toLowerCase().startsWith(this.typeBuf));
    if (i >= 0) {
      this.sel.clear();
      this.sel.add(this.rows[i].key);
      this.focusKey = this.rows[i].key;
      this.anchorKey = this.rows[i].key;
      this.scrollTo(i);
      this.render();
      this.on.select?.(this.selectedRows());
    }
  }
}
