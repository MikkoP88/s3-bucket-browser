// Virtualized, sortable, multi-select details grid.
// Renders only the visible slice (+overscan) so a 100k-object folder scrolls
// at full frame rate (performance budget). The column set is dynamic: the
// catalog below lists every possible column, setColumns() picks the visible
// ones (Settings exposes them; "name" is always visible).
import { el, fmtBytes, fmtDate, fileIcon } from './util.js';
import { t } from './i18n.js';

const ROW_H = 28;
const OVERSCAN = 8;

// acceptedMimes lists the drag payload types a pane accepts on folder rows:
// the remote pane takes same-pane moves plus local-pane uploads; the local
// pane takes remote downloads only (local moves are Explorer's job). The
// side pane overrides this per binding (this.accepts) — remote and S3
// bindings both take local-pane uploads too.
function acceptedMimes(kind) {
  return kind === 'remote'
    ? ['application/x-s3b', 'application/x-s3b-local']
    : ['application/x-s3b'];
}

// Column catalog — every column the details grid can show. flex columns
// absorb the remaining width; the rest are fixed. "name" is the identity
// column (icon + name + version/marker badges) and is always visible.
export const COLUMNS = [
  { id: 'name', labelKey: 'col.name', flex: true, minW: 200 },
  { id: 'type', labelKey: 'col.type', w: 110 },
  { id: 'size', labelKey: 'col.size', w: 110, num: true },
  { id: 'lastModified', labelKey: 'col.date', w: 160 },
  { id: 'storageClass', labelKey: 'col.class', w: 120 },
  { id: 'etag', labelKey: 'col.etag', w: 190 },
];

// DEFAULT_COLS is the out-of-box visible set (the pre-settings layout).
export const DEFAULT_COLS = ['name', 'size', 'lastModified', 'storageClass'];

// typeOf renders the Type column: "Folder" / "File" / the uppercase
// extension ("JPG", "PDF").
function typeOf(r) {
  if (r.isDir) return t('type.folder');
  const i = String(r.name || '').lastIndexOf('.');
  if (i > 0 && i < r.name.length - 1) return r.name.slice(i + 1).toUpperCase();
  return t('type.file');
}

// colText returns the text a filter matches against for one column: the
// rendered value (sizes formatted, dates localized) plus the raw bytes for
// size, so both "MB" and "1048576" hit. Folders carry no size/date/class.
function colText(r, id) {
  switch (id) {
    case 'name': return r.name || '';
    case 'type': return typeOf(r);
    case 'size': return r.isDir ? '' : `${fmtBytes(r.size)} ${r.size || 0}`;
    case 'lastModified': return r.isDir ? '' : fmtDate(r.lastModified || r.modTime);
    case 'storageClass': return r.isDir ? '' : (r.storageClass || '');
    case 'etag': return r.isDir ? '' : (r.etag || '');
    default: return '';
  }
}

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
    this.filter = '';        // global (all-columns) substring filter
    this.colFilters = {};    // column id -> substring filter (stacked)
    this.typeBuf = '';
    this.typeTimer = null;

    this.pool = [];
    this.accepts = null; // optional mime list override (side-pane bindings)
    this.showMarkers = true; // delete-marker badges on (Settings toggle)
    this.on = {}; // callbacks: select, activate, context, dragstart, drop, badgeV, badgeM
    this.setColumns(DEFAULT_COLS);
    this.body.addEventListener('scroll', () => this.render());
    // Right-click on a header cell opens the column picker (same catalog as
    // Settings); wired by the host pane via on.headerMenu.
    this.head.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      if (this.on.headerMenu) this.on.headerMenu(e);
    });
  }

  // ---------- columns ----------

  // visibleCols returns the currently shown columns in display order.
  visibleCols() { return this.cols; }

  // setColumns applies a visible-column id list ("name" is forced in).
  setColumns(ids) {
    const want = new Set(Array.isArray(ids) ? ids : []);
    want.add('name');
    this.cols = COLUMNS.filter((c) => want.has(c.id));
    if (!this.cols.some((c) => c.id === this.sortKey)) {
      this.sortKey = 'name';
      this.sortDir = 1;
    }
    // filters of now-hidden columns are dropped, not kept dormant
    for (const c of COLUMNS) {
      if (!want.has(c.id) && this.colFilters[c.id]) delete this.colFilters[c.id];
    }
    this.renderHead();
    this.rebuildPool();
    this.apply();
  }

  // gridTemplate is the CSS grid-template-columns for head + rows.
  gridTemplate() {
    return `34px ${this.cols.map((c) => (c.flex ? `minmax(${c.minW}px,3fr)` : `${c.w}px`)).join(' ')}`;
  }

  // nameCellIdx is the child index of the name cell within pooled rows.
  nameCellIdx() { return 1 + this.cols.findIndex((c) => c.id === 'name'); }

  // rebuildPool drops every pooled row (their cell layout is baked in) and
  // lets render() recreate them against the current columns.
  rebuildPool() {
    this.canvas.replaceChildren();
    this.pool = [];
  }

  // ---------- setup ----------
  renderHead() {
    // Leading checkbox column: header box = select all/none (indeterminate
    // when partial); per-row boxes toggle membership in this.sel without
    // disturbing the rest of the selection or the keyboard focus model.
    const cb = el('input', { type: 'checkbox', title: 'Select all / none' });
    cb.setAttribute('aria-label', 'Select all');
    cb.addEventListener('click', (e) => {
      e.stopPropagation();
      if (this.rows.length && this.sel.size >= this.rows.length) this.clearSelection();
      else this.selectAll();
    });
    this.headCb = cb;
    this.head.style.gridTemplateColumns = this.gridTemplate();
    this.head.replaceChildren(
      el('div', { class: 'gh check' }, cb),
      ...this.cols.map((c) => {
        const ind = el('span', { class: 'sort-ind' });
        if (this.sortKey === c.id) ind.textContent = this.sortDir > 0 ? '\u25B2' : '\u25BC';
        const active = this.colFilters[c.id];
        const label = t(c.labelKey);
        const funnel = el('button', {
          class: `gh-filter${active ? ' on' : ''}`,
          title: active ? `${label}: ${active} (Esc clears)` : `${t('col.filterBy')} ${label.toLowerCase()}`,
          'aria-label': `${t('col.filterBy')} ${label}`,
          onclick: (e) => { e.stopPropagation(); this.editColumnFilter(c.id, funnel); },
        });
        funnel.innerHTML = '<svg width="10" height="10" viewBox="0 0 16 16" aria-hidden="true"><path d="M1 2h14l-5.5 6.2V14l-3-1.6V8.2Z" fill="currentColor"/></svg>';
        return el('div', {
          class: `gh${c.num ? ' num' : ''}`,
          onclick: () => this.cycleSort(c.id),
        }, el('span', { text: label }), ind, funnel);
      }),
    );
  }

  // editColumnFilter swaps one header cell for an inline input bound to
  // that column's substring filter: typing filters live, Enter/blur keeps
  // it (the funnel stays highlighted), Esc clears it.
  editColumnFilter(id, btn) {
    const cell = btn.closest('.gh');
    if (!cell || cell.querySelector('.gh-cfilter')) return;
    cell.replaceChildren();
    const inp = el('input', { type: 'text', class: 'gh-cfilter', spellcheck: 'false' });
    inp.value = this.colFilters[id] || '';
    cell.appendChild(inp);
    inp.focus();
    inp.select();
    inp.addEventListener('click', (e) => e.stopPropagation());
    inp.addEventListener('input', () => {
      this.colFilters[id] = inp.value;
      this.apply();
    });
    inp.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); inp.blur(); }
      else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        this.colFilters[id] = '';
        this.apply();
        this.renderHead();
      }
    });
    inp.addEventListener('blur', () => this.renderHead());
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
    if (!this.hasFilters() && this.sortKey === 'name' && this.sortDir === 1) {
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

  // hasFilters: any global or per-column filter active.
  hasFilters() {
    return !!this.filter || this.cols.some((c) => this.colFilters[c.id]);
  }

  apply() {
    let rows = this.all;
    // the navbar filter is global — a hit in ANY visible column keeps the
    // row; per-column funnels stack on top, each matching its own column
    const q = (this.filter || '').toLowerCase();
    if (q) rows = rows.filter((r) => this.cols.some((c) => colText(r, c.id).toLowerCase().includes(q)));
    for (const c of this.cols) {
      const f = this.colFilters[c.id];
      if (f) {
        const cq = f.toLowerCase();
        rows = rows.filter((r) => colText(r, c.id).toLowerCase().includes(cq));
      }
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
    this.render(true); // force: re-emit select (selection may have been pruned)
  }

  selectedRows() { return this.rows.filter((r) => this.sel.has(r.key)); }

  rowByKey(key) { return this.rows.find((r) => r.key === key) || null; }

  // setCmp decorates rows with directory-compare statuses (name -> status)
  // and repaints; null clears the decorations.
  setCmp(map) {
    for (const r of this.all) r.cmp = map ? (map.get(r.name) || '') : '';
    this.render();
  }

  // setMarkers decorates rows from a PrefixVersionSummary pass (versioned
  // buckets only): map keys `${isDir?'d':'f'}:${name}` → ChildSummary
  // {versions, markers, allDeleted}. The version badge (⟲ n) shows the
  // row's version count when enabled in Settings (hidden when nothing is
  // counted); the marker badge flags delete-marked objects — ⛔ without a
  // number on files (an object carries at most one marker), ⛔ n on
  // directories, which aggregate everything beneath them. All-deleted
  // folders are dimmed like ghost rows. null clears the decorations.
  setMarkers(map) {
    for (const r of this.all) {
      const s = map ? (map.get(`${r.isDir ? 'd' : 'f'}:${r.name}`) || null) : null;
      r.vcount = s ? s.versions : 0;
      r.mcount = s ? s.markers : 0;
      r.dimmed = !!(s && s.allDeleted && r.isDir);
    }
    this.render();
  }

  selectAll() {
    this.sel = new Set(this.rows.map((r) => r.key));
    this.render(true); // force: emits select once (render's own re-emit)
  }

  clearSelection() {
    this.sel.clear();
    this.focusKey = null;
    this.render(true); // force: emits select once (render's own re-emit)
  }

  // invertSelection flips membership of every visible row (Ctrl+I, Edit
  // menu) — the complement of the current selection, Explorer-style.
  invertSelection() {
    this.sel = new Set(this.rows.map((r) => r.key).filter((k) => !this.sel.has(k)));
    if (this.focusKey && !this.sel.has(this.focusKey)) this.focusKey = null;
    this.render(true); // force: emits select once (render's own re-emit)
  }

  // ---------- rendering ----------

  // makeRow builds one pooled row against the current columns.
  makeRow() {
    const row = el('div', { class: 'grid-row', draggable: 'true', role: 'option' });
    row.style.gridTemplateColumns = this.gridTemplate();
    row.appendChild(el('div', { class: 'gc check' }, el('input', { type: 'checkbox' })));
    for (const c of this.cols) {
      if (c.id === 'name') {
        row.appendChild(el('div', { class: 'gc name' },
          el('span', { class: 'icon' }),
          el('span', { class: 'tname' }),
          el('span', { class: 'vbadge', role: 'button' }),
          el('span', { class: 'mbadge', role: 'button' })));
      } else {
        row.appendChild(el('div', { class: `gc${c.num ? ' num' : ''} ${c.id}` }));
      }
    }
    this.wireRow(row);
    return row;
  }

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
      const row = this.makeRow();
      this.canvas.appendChild(row); // pool rows live in the canvas; recycled via display/top
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
      row.classList.toggle('ghost', !!(m.ghost || m.dimmed));
      row.setAttribute('aria-selected', this.sel.has(m.key) ? 'true' : 'false');
      row.dataset.cmp = m.cmp || '';
      const cells = row.children;
      const cb = cells[0].children[0];
      cb.checked = this.sel.has(m.key);
      cb.setAttribute('aria-label', `Select ${m.name}`);
      for (let ci = 0; ci < this.cols.length; ci++) {
        const cell = cells[1 + ci];
        const c = this.cols[ci];
        if (c.id === 'name') {
          cell.children[0].textContent = fileIcon(m.name, m.isDir);
          cell.children[1].textContent = m.name;
          // version badge: count + tooltip (counts only), rendered when
          // enabled in Settings and something is counted; click opens the
          // Versions window (file) / Content Versions window (folder)
          const vb = cell.children[2];
          const vn = this.showVersions === true && m.vcount ? m.vcount : 0;
          vb.textContent = vn ? `\u27F2 ${vn}` : '';
          vb.title = vn ? t('ver.count', { n: vn }) : '';
          // marker badge: files carry at most one marker so they show the
          // bare flag; directories aggregate a count. Rendered when
          // enabled in Settings; click opens the Delete Marker window
          const mb = cell.children[3];
          const hasM = this.showMarkers === true && !!m.mcount;
          mb.textContent = hasM ? (m.isDir ? `\u26D4 ${m.mcount}` : '\u26D4') : '';
          mb.title = hasM ? (m.isDir ? t('mark.count', { n: m.mcount }) : t('mark.has')) : '';
        } else if (c.id === 'type') {
          cell.textContent = typeOf(m);
        } else if (c.id === 'etag') {
          cell.textContent = m.isDir ? '' : (m.etag || '');
          cell.title = cell.textContent;
        } else if (c.id === 'size') {
          cell.textContent = m.isDir ? '' : fmtBytes(m.size);
        } else if (c.id === 'lastModified') {
          cell.textContent = m.isDir ? '' : fmtDate(m.lastModified || m.modTime);
        } else if (c.id === 'storageClass') {
          cell.textContent = m.isDir ? '' : (m.storageClass || '');
        }
      }
    }
    // header checkbox reflects the full selection state
    if (this.headCb) {
      const n = this.rows.length;
      this.headCb.checked = n > 0 && this.sel.size >= n;
      this.headCb.indeterminate = this.sel.size > 0 && this.sel.size < n;
    }
    if (force) this.on.select?.(this.selectedRows());
  }

  wireRow(row) {
    // checkbox column: clicks toggle membership in this.sel only — no drag,
    // no dbl-activate, no plain-click selection reset.
    const cb = row.children[0].children[0];
    cb.addEventListener('mousedown', (e) => e.stopPropagation());
    cb.addEventListener('dblclick', (e) => e.stopPropagation());
    cb.addEventListener('click', (e) => {
      e.stopPropagation();
      const m = row._model;
      if (!m) return;
      if (this.sel.has(m.key)) this.sel.delete(m.key); else this.sel.add(m.key);
      this.focusKey = m.key;
      this.anchorKey = m.key;
      this.render();
      this.on.select?.(this.selectedRows());
    });
    // badges (inside the name cell): clicks open the Versions / Delete
    // Marker windows without disturbing the row selection.
    const nc = row.children[this.nameCellIdx()];
    const vb = nc.children[2];
    const mb = nc.children[3];
    for (const [badge, cb2] of [[vb, 'badgeV'], [mb, 'badgeM']]) {
      badge.addEventListener('mousedown', (e) => e.stopPropagation());
      badge.addEventListener('dblclick', (e) => e.stopPropagation());
      badge.addEventListener('click', (e) => {
        e.stopPropagation();
        const m = row._model;
        if (m) this.on[cb2]?.(m);
      });
    }
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
      if (e.target.type === 'checkbox') { e.preventDefault(); return; } // no drag from the checkbox
      const m = row._model;
      if (!this.sel.has(m.key)) {
        this.sel.clear();
        this.sel.add(m.key);
        this.render();
      }
      e.dataTransfer.setData(this.mime, JSON.stringify(this.dragPayload()));
      e.dataTransfer.effectAllowed = 'copyMove';
      // OS drag-out (rows to Explorer): the host sets DownloadURL /
      // text-uri-list from precomputed loopback URLs, if any.
      this.on.dragOS?.(e, this.selectedRows());
      this.on.dragstart?.(this.selectedRows());
    });
    row.addEventListener('dragover', (e) => {
      if (!row._model?.isDir) return;
      if (!(this.accepts || acceptedMimes(this.kind)).some((t) => e.dataTransfer.types.includes(t))) return;
      e.preventDefault();
      row.classList.add('drop-target');
    });
    row.addEventListener('dragleave', () => row.classList.remove('drop-target'));
    row.addEventListener('drop', (e) => {
      row.classList.remove('drop-target');
      if (!row._model?.isDir) return;
      let payload = null;
      for (const t of (this.accepts || acceptedMimes(this.kind))) {
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
