// Virtualized, sortable, multi-select details grid.⁠​‌‌‌​​‌‌​​‌‌​​‌‌​‌‌​​​‌​​​‌​‌‌​‌​‌‌‌​​​​​‌‌‌​​‌​​‌‌​‌‌‌‌​‌‌‌​‌‌​​‌‌​​‌​‌​‌‌​‌‌‌​​‌‌​​​​‌​‌‌​‌‌‌​​‌‌​​​‌‌​‌‌​​‌​‌​​‌​‌‌​‌​‌‌‌​‌‌​​​‌‌​​​‌​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌​​​​‌‌​‌‌​‌‌‌‌​‌‌‌​​​​​‌‌‌‌​​‌​‌‌‌​​‌​​‌‌​‌​​‌​‌‌​​‌‌‌​‌‌​‌​​​​‌‌‌​‌​​​​‌​​​​​​​‌​‌​​​​‌‌​​​‌‌​​‌​‌​​‌​​‌​​​​​​​‌‌​​‌​​​‌‌​​​​​​‌‌​​‌​​​‌‌​‌‌​​​‌​​​​​​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​​‌​​​​​​‌​‌​​​​​‌‌​​‌​‌​‌‌‌​​‌‌​‌‌​‌‌‌‌​‌‌​‌‌‌​​‌‌​​‌​‌​‌‌​‌‌‌​​​‌​​​​​​​‌​‌​​​​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​‌​‌​​​​​​‌‌‌​​​​​‌‌‌​​​​​‌​‌​​‌​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌​‌​​​​​‌‌​‌‌‌‌​‌‌​‌‌​​​‌‌‌‌​​‌​‌​​​‌‌​​‌‌​‌‌‌‌​‌‌‌​​‌​​‌‌​‌‌​‌​​‌​​​​​​‌​​‌​​‌​‌‌​‌‌‌​​‌‌‌​‌​​​‌‌​​‌​‌​‌‌‌​​‌​​‌‌​‌‌‌​​‌‌​​​​‌​‌‌​‌‌​​​​‌​​​​​​‌​‌​‌​‌​‌‌‌​​‌‌​‌‌​​‌​‌​​‌​​​​​​‌​​‌‌​​​‌‌​‌​​‌​‌‌​​​‌‌​‌‌​​‌​‌​‌‌​‌‌‌​​‌‌‌​​‌‌​‌‌​​‌​‌​​‌​​​​​​​‌‌​​​‌​​‌​‌‌‌​​​‌‌​​​​​​‌​‌‌‌​​​‌‌​​​​​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌‌​​‌‌‌​‌‌​‌​​‌​‌‌‌​‌​​​‌‌​‌​​​​‌‌‌​‌​‌​‌‌​​​‌​​​‌​‌‌‌​​‌‌​​​‌‌​‌‌​‌‌‌‌​‌‌​‌‌​‌​​‌​‌‌‌‌​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​‌​‌​​​​​​‌‌‌​​​​​‌‌‌​​​​​‌​‌‌‌‌​‌‌‌​​‌‌​​‌‌​​‌‌​​‌​‌‌​‌​‌‌​​​‌​​‌‌‌​‌​‌​‌‌​​​‌‌​‌‌​‌​‌‌​‌‌​​‌​‌​‌‌‌​‌​​​​‌​‌‌​‌​‌‌​​​‌​​‌‌‌​​‌​​‌‌​‌‌‌‌​‌‌‌​‌‌‌​‌‌‌​​‌‌​‌‌​​‌​‌​‌‌‌​​‌​⁠
// Renders only the visible slice (+overscan) so a 100k-object folder scrolls
// at full frame rate (performance budget). The column set is dynamic: the
// catalog below lists every possible column, setColumns() picks the visible
// ones (Settings exposes them; "name" is always visible). Columns resize by
// dragging their header edge — one overlay handle per boundary (renderHandles)
// that also resizes from the keyboard — and reorder by dragging the header
// itself; order and widths persist per pane (loadColState/saveColState below).
import { el, fmtBytes, fmtDate, fileIcon } from './util.js';
import { t, has } from './i18n.js';

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
// created and mode are engine-optional: created fills where the source
// carries a birth time (bucket views, WebDAV creationdate, Windows local
// files), mode where permission bits exist (local, SFTP); rows from
// engines without the attribute render the cell empty.
export const COLUMNS = [
  { id: 'name', labelKey: 'col.name', flex: true, minW: 200 },
  { id: 'type', labelKey: 'col.type', w: 150 },
  { id: 'mode', labelKey: 'col.mode', w: 130 },
  { id: 'size', labelKey: 'col.size', w: 110, num: true },
  { id: 'lastModified', labelKey: 'col.date', w: 160 },
  { id: 'created', labelKey: 'col.created', w: 160 },
  { id: 'storageClass', labelKey: 'col.class', w: 120 },
  { id: 'etag', labelKey: 'col.etag', w: 190 },
];

// DEFAULT_COLS is the out-of-box visible set (the pre-settings layout):
// Type is on by default; Storage class and ETag are opt-in via the
// header menu. A saved choice (s3b-cols in localStorage) always wins.
export const DEFAULT_COLS = ['name', 'type', 'size', 'lastModified'];

const MIN_COL_W = 48;      // resize floor for fixed-width columns
const RZ_HIT_W = 10;       // boundary-handle hit width (keep in step with .gh-resize)
const DRAG_THRESHOLD = 6;  // px before a header press becomes a reorder drag

// Column-layout persistence, one key per pane ('s3b-cols' remote,
// 's3b-cols-local' side pane). The value is JSON { cols: [ordered ids],
// widths: { id: px } }; values written before widths existed are plain CSV
// id lists and still load (order only). loadColState never throws — a
// corrupt or unknown entry reads as "no preference".
export function loadColState(key) {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return null;
    const known = new Set(COLUMNS.map((c) => c.id));
    if (!raw.startsWith('{')) {
      return { cols: raw.split(',').map((s) => s.trim()).filter((id) => known.has(id)), widths: {} };
    }
    const v = JSON.parse(raw);
    const cols = Array.isArray(v?.cols)
      ? v.cols.filter((id) => known.has(id)) : [];
    const widths = {};
    for (const [id, w] of Object.entries(v?.widths || {})) {
      if (known.has(id) && Number.isFinite(w) && w >= MIN_COL_W && w <= 4000) widths[id] = Math.round(w);
    }
    return { cols, widths };
  } catch {
    return null;
  }
}

// saveColState persists a pane's column order plus any user-set widths.
export function saveColState(key, cols, widths) {
  try {
    localStorage.setItem(key, JSON.stringify({ cols, widths: widths || {} }));
  } catch { /* storage full or blocked: the layout just won't persist */ }
}

// typeOf renders the Type column Windows-Explorer style: "Folder", the
// friendly name of a common extension ("PNG image", "Text document") in
// the UI language, or "<EXT> file" for the long tail. It derives
// everything from the name alone, so every engine — S3, local, SFTP,
// FTP, WebDAV — shows the same column for the same file name.
function typeOf(r) {
  if (r.isDir) return t('type.folder');
  const i = String(r.name || '').lastIndexOf('.');
  if (i > 0 && i < r.name.length - 1) {
    const ext = r.name.slice(i + 1).toLowerCase();
    if (has(`type.${ext}`)) return t(`type.${ext}`);
    return t('type.extFile', { ext: ext.toUpperCase() });
  }
  return t('type.file');
}

// colText returns the text a filter matches against for one column: the
// rendered value (sizes formatted, dates localized) plus the raw bytes for
// size, so both "MB" and "1048576" hit. Folders carry no size/date/class;
// created and mode match only where the engine supplied a value.
function colText(r, id) {
  switch (id) {
    case 'name': return r.name || '';
    case 'type': return typeOf(r);
    case 'mode': return r.mode || '';
    case 'size': return r.isDir ? '' : `${fmtBytes(r.size)} ${r.size || 0}`;
    case 'lastModified': return r.isDir ? '' : fmtDate(r.lastModified || r.modTime);
    case 'created': return r.created ? fmtDate(r.created) : '';
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
    // The head scrolls horizontally WITH the rows: it is width-locked to the
    // rows' layout width and translated by -scrollLeft inside a clipping
    // wrapper. Without the lock a pane narrower than the column minimums
    // clips the head forever — off-clip headers (and their resize handles)
    // could never be reached, and after a horizontal scroll the headers
    // would sit over the wrong columns' data.
    this.headClip = el('div', { class: 'grid-headclip' });
    this.head.replaceWith(this.headClip);
    this.headClip.appendChild(this.head);
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
    this.widths = {};        // column id -> user-set pixel width (catalog w otherwise)
    this.typeBuf = '';
    this.typeTimer = null;

    this.pool = [];
    this.accepts = null; // optional mime list override (side-pane bindings)
    this.showMarkers = true; // delete-marker badges on (Settings toggle)
    this.on = {}; // callbacks: select, activate, context, dragstart, drop, badgeV, badgeM
    this.setColumns(DEFAULT_COLS);
    this.body.addEventListener('scroll', () => { this.syncHeadScroll(); this.render(); });
    // pane width changes (split drag, window resize) and the scrollbar
    // appearing/disappearing (row count changes) both re-lock the head width
    // and re-seat the boundary handles
    this.ro = new ResizeObserver(() => { this.syncHeadWidth(); this.positionHandles(); });
    this.ro.observe(this.headClip);
    this.ro.observe(this.canvas);
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

  // setColumns applies a visible-column id list. The list's order IS the
  // display order (drag-to-reorder persists through here); unknown ids and
  // duplicates are dropped, and "name" — the identity column — is forced in
  // (and leads) when absent.
  setColumns(ids) {
    const byId = new Map(COLUMNS.map((c) => [c.id, c]));
    const seen = new Set();
    this.cols = [];
    for (const id of Array.isArray(ids) ? ids : []) {
      const c = byId.get(id);
      if (c && !seen.has(id)) { seen.add(id); this.cols.push(c); }
    }
    if (!seen.has('name')) this.cols.unshift(byId.get('name'));
    if (!this.cols.some((c) => c.id === this.sortKey)) {
      this.sortKey = 'name';
      this.sortDir = 1;
    }
    // filters of now-hidden columns are dropped, not kept dormant
    const want = new Set(this.cols.map((c) => c.id));
    for (const c of COLUMNS) {
      if (!want.has(c.id) && this.colFilters[c.id]) delete this.colFilters[c.id];
    }
    this.renderHead();
    this.rebuildPool();
    this.apply();
  }

  // restoreCols applies a persisted column state (order + widths) or the
  // default layout when nothing valid is stored.
  restoreCols(key) {
    const st = loadColState(key);
    this.widths = (st && st.widths) || {};
    this.setColumns(st && st.cols.length ? st.cols : DEFAULT_COLS);
  }

  // gridTemplate is the CSS grid-template-columns for head + rows. The flex
  // column (name) absorbs the remaining width until the user sizes it; a
  // user-sized flex column becomes capped (minmax up to that width) and a
  // trailing filler track takes the rest, so the last column never stretches.
  gridTemplate() {
    const parts = this.cols.map((c) => {
      if (c.flex && !this.widths[c.id]) return `minmax(${c.minW}px,3fr)`;
      const w = Math.round(this.widths[c.id] || c.w);
      return c.flex ? `minmax(${c.minW}px,${w}px)` : `${w}px`;
    });
    if (this.cols.some((c) => c.flex && this.widths[c.id])) parts.push('minmax(0,1fr)');
    return `34px ${parts.join(' ')}`;
  }

  // colWidth is a column's current effective width (user-set or catalog).
  colWidth(c) { return this.widths[c.id] || c.w; }

  // colFloor is the width a column must keep while another one is being
  // resized: fixed columns their current width, the flex one its minimum.
  colFloor(c) { return c.flex ? c.minW : this.colWidth(c); }

  // resizeFloor is the least width a drag may give a column: the flex
  // column bottoms out at its template minimum, fixed columns at
  // MIN_COL_W. Clamping the drag here (not at MIN_COL_W alone) keeps the
  // pointer tracking at the floor — minmax(minW, w < minW) silently
  // resolves to minW, so the last stretch of a narrowing drag would look
  // dead while the pointer keeps moving.
  resizeFloor(c) { return c.flex ? c.minW : MIN_COL_W; }

  // resizeCeiling is the most a column may take in this pane: everything
  // the grid has (checkbox track included) once the other columns keep
  // their floors. The rows area is the truth — it loses the scrollbar
  // width. Floored at the column's current width so a rightward drag in a
  // layout already overflowing its minimums stays inert instead of
  // snapping the column down to the ceiling — and shifting every column
  // after it — on the first move.
  resizeCeiling(c, curW) {
    const bound = Math.min(this.headClip.clientWidth, this.body.clientWidth);
    return Math.max(curW, bound - 34
      - this.cols.reduce((sum, x) => (x === c ? sum : sum + this.colFloor(x)), 0));
  }

  // syncHeadWidth locks the head to the rows' layout width: the fr/flex
  // track then resolves against exactly the space the rows see, so head and
  // row cell boundaries agree pixel-for-pixel, vertical scrollbar included.
  syncHeadWidth() {
    this.head.style.width = `${this.body.clientWidth}px`;
  }

  // syncHeadScroll slides the head by the body's horizontal scroll offset.
  syncHeadScroll() {
    this.head.style.transform = `translateX(${-this.body.scrollLeft}px)`;
  }

  // applyTemplate pushes the current column template onto the head and
  // every pooled row — called live from a resize drag. The head follows
  // width-locked and translated, the boundary handles re-seat on their
  // (possibly just-moved) edges, and the separators re-expose their size.
  applyTemplate() {
    const tpl = this.gridTemplate();
    this.head.style.gridTemplateColumns = tpl;
    for (const row of this.pool) row.style.gridTemplateColumns = tpl;
    this.syncHeadWidth();
    this.syncHeadScroll();
    this.positionHandles();
    this.updateHandleAria();
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
    this.headCols = this.cols.map((c) => this.headerCell(c));
    // the handles layer goes FIRST so the last header cell stays
    // :last-child (its right border is dropped there)
    this.head.replaceChildren(this.handlesLayer(), el('div', { class: 'gh check' }, cb), ...this.headCols);
    this.updateHandleAria();
    this.positionHandles();
  }

  // handlesLayer builds the overlay that carries one resize handle per
  // column boundary. The handles live above the cells — not inside them:
  // a cell's overflow:hidden (the label ellipsis) clips a child handle at
  // the padding box, killing exactly the boundary-side half of the grab
  // zone, and a grab there lands on the NEXT cell instead — a sort click
  // or an accidental column reorder. The layer itself never blocks a cell
  // (pointer-events none); only the handles take pointers.
  handlesLayer() {
    const layer = el('div', { class: 'gh-handles' });
    layer.replaceChildren(...this.cols.map((c) => {
      const label = t(c.labelKey);
      const rz = el('div', {
        class: 'gh-resize', role: 'separator', 'aria-orientation': 'vertical',
        tabindex: '0', 'data-col': c.id,
        'aria-label': `${label}: ${t('col.resizeTip')}`, title: t('col.resizeTip'),
      });
      rz.addEventListener('pointerdown', (e) => this.startResize(e, c, rz));
      rz.addEventListener('dblclick', (e) => { e.stopPropagation(); this.resetWidth(c); });
      rz.addEventListener('keydown', (e) => this.keyResize(e, c));
      return rz;
    }));
    return layer;
  }

  // positionHandles seats every boundary handle on its column's right edge
  // (cell rects minus the head's own rect, so a translated head needs no
  // special casing). Runs after any template change — including each live
  // drag move, so a handle follows its boundary while the column resizes.
  positionHandles() {
    const layer = this.head.querySelector('.gh-handles');
    if (!layer) return;
    const hl = this.head.getBoundingClientRect().left;
    this.headCols.forEach((cell, i) => {
      const h = layer.children[i];
      if (h) h.style.left = `${cell.getBoundingClientRect().right - hl - RZ_HIT_W / 2}px`;
    });
  }

  // updateHandleAria exposes each separator's live size and the same
  // floor/ceiling a pointer drag respects, so assistive tech reads the
  // real bounds, not just "separator".
  updateHandleAria() {
    const layer = this.head.querySelector('.gh-handles');
    if (!layer) return;
    this.cols.forEach((c, i) => {
      const h = layer.children[i];
      const w = Math.round(this.headCols[i].getBoundingClientRect().width);
      h.setAttribute('aria-valuemin', String(this.resizeFloor(c)));
      h.setAttribute('aria-valuemax', String(this.resizeCeiling(c, w)));
      h.setAttribute('aria-valuenow', String(w));
      h.setAttribute('aria-valuetext', `${w}px`);
    });
  }

  // headerCell builds one column header: label + sort indicator + filter
  // funnel. Click cycles the sort; dragging the cell (6px+) reorders the
  // column. The resize handle is NOT here — see handlesLayer.
  headerCell(c) {
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
    const cell = el('div', {
      class: `gh${c.num ? ' num' : ''}`,
      'data-col': c.id,
      title: t('col.dragTip'),
      onclick: () => this.cycleSort(c.id),
    }, el('span', { text: label }), ind, funnel);
    cell.addEventListener('pointerdown', (e) => this.startColDrag(e, c, cell));
    return cell;
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
    this.apply();
    this.renderHead();
  }

  // ---------- column drag: resize + reorder ----------

  // startResize tracks a boundary-handle drag: the column width follows the
  // pointer live (head + pooled rows restyle per move — a pool is ~40 rows,
  // one inline style each), clamped to the column's own floor below and to
  // the width the other columns must keep above. Pointerup persists via
  // on.colsChanged.
  startResize(e, c, rz) {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    const startW = this.headCols[this.cols.indexOf(c)].getBoundingClientRect().width;
    const startX = e.clientX;
    const floor = this.resizeFloor(c);
    const maxW = this.resizeCeiling(c, startW);
    rz.setPointerCapture?.(e.pointerId); // keep the drag alive past the window edge
    rz.classList.add('dragging');
    document.body.classList.add('col-resize-active');
    const move = (ev) => {
      const w = Math.round(Math.min(Math.max(startW + ev.clientX - startX, floor), maxW));
      if (w !== this.widths[c.id]) {
        this.widths[c.id] = w;
        this.applyTemplate();
      }
    };
    const done = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', done);
      window.removeEventListener('pointercancel', done);
      rz.classList.remove('dragging');
      document.body.classList.remove('col-resize-active');
      this.on.colsChanged?.();
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', done);
    window.addEventListener('pointercancel', done);
  }

  // keyResize is the keyboard path on a focused handle: ArrowLeft/Right
  // nudge by 8px (Shift ×4), Home/End jump to the bounds — all clamped to
  // the same floor/ceiling a pointer drag respects.
  keyResize(e, c) {
    const cell = this.headCols[this.cols.indexOf(c)];
    const cur = Math.round(cell.getBoundingClientRect().width);
    const floor = this.resizeFloor(c);
    const max = this.resizeCeiling(c, cur);
    let w;
    if (e.key === 'ArrowLeft') w = cur - (e.shiftKey ? 32 : 8);
    else if (e.key === 'ArrowRight') w = cur + (e.shiftKey ? 32 : 8);
    else if (e.key === 'Home') w = floor;
    else if (e.key === 'End') w = max;
    else return;
    e.preventDefault();
    e.stopPropagation();
    w = Math.round(Math.min(Math.max(w, floor), max));
    if (w === cur) return;
    this.widths[c.id] = w;
    this.applyTemplate();
    this.on.colsChanged?.();
  }

  // resetWidth clears a user-set width — double-click on the handle. The
  // column returns to its catalog width (the flex column goes back to
  // absorbing all remaining width).
  resetWidth(c) {
    if (this.widths[c.id] === undefined) return;
    delete this.widths[c.id];
    this.applyTemplate();
    this.on.colsChanged?.();
  }

  // startColDrag arms header drag-to-reorder: a press anywhere on a header
  // cell (outside the funnel, the inline filter input and the resize
  // handle) becomes a reorder once the pointer moves past DRAG_THRESHOLD —
  // a plain click still sorts. The dragged header dims, a 2px indicator
  // tracks the nearest slot boundary, and pointerup applies the move.
  startColDrag(e, c, cell) {
    if (e.button !== 0 || e.target.closest('.gh-resize, .gh-filter, .gh-cfilter')) return;
    const startX = e.clientX;
    const startY = e.clientY;
    const head = this.head;
    let dragging = false;
    let indicator = null;
    let target = null;
    // the synthetic click after a drag would flip the sort — swallow it on
    // the head (capture fires before the cell's listener); removed again
    // in cleanup so nothing lingers when no click follows
    const swallow = (ev) => ev.stopPropagation();
    const boundaryX = (i) => {
      const cells = this.headCols;
      if (i >= cells.length) return cells[cells.length - 1].getBoundingClientRect().right;
      return cells[i].getBoundingClientRect().left;
    };
    const move = (ev) => {
      if (!dragging) {
        if (Math.hypot(ev.clientX - startX, ev.clientY - startY) < DRAG_THRESHOLD) return;
        dragging = true;
        cell.classList.add('drag-src');
        indicator = el('div', { class: 'gh-insert' });
        head.appendChild(indicator);
        head.addEventListener('click', swallow, { capture: true });
        document.body.classList.add('col-dragging');
      }
      let best = 0;
      let bd = Infinity;
      for (let i = 0; i <= this.cols.length; i++) {
        const d = Math.abs(ev.clientX - boundaryX(i));
        if (d < bd) { bd = d; best = i; }
      }
      target = best;
      indicator.style.left = `${boundaryX(best) - head.getBoundingClientRect().left}px`;
    };
    const done = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', done);
      window.removeEventListener('pointercancel', done);
      head.removeEventListener('click', swallow, { capture: true });
      document.body.classList.remove('col-dragging');
      cell.classList.remove('drag-src');
      if (indicator) indicator.remove();
      if (dragging && target !== null) {
        const from = this.cols.indexOf(c);
        // dropping immediately before/after the source cell is a no-op
        if (target !== from && target !== from + 1) {
          const ids = this.cols.map((x) => x.id);
          ids.splice(from, 1);
          ids.splice(target > from ? target - 1 : target, 0, c.id);
          this.setColumns(ids); // rebuilds head + pool; widths ride along
          this.on.colsChanged?.();
        }
      }
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', done);
    window.addEventListener('pointercancel', done);
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

  // removeRows drops rows by key — the inverse of appendRows, for rows a
  // plain listing can never return (ghost reconciliation on refresh).
  removeRows(keys) {
    if (!keys.length) return;
    const drop = new Set(keys);
    this.all = this.all.filter((r) => !drop.has(r.key));
    this.apply();
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
      if (key === 'type') { va = typeOf(a); vb = typeOf(b); }
      else if (key === 'size' || key === 'lastModified' || key === 'created') {
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
        } else if (c.id === 'mode') {
          cell.textContent = m.mode || '';
        } else if (c.id === 'etag') {
          cell.textContent = m.isDir ? '' : (m.etag || '');
          cell.title = cell.textContent;
        } else if (c.id === 'size') {
          cell.textContent = m.isDir ? '' : fmtBytes(m.size);
        } else if (c.id === 'lastModified') {
          cell.textContent = m.isDir ? '' : fmtDate(m.lastModified || m.modTime);
        } else if (c.id === 'created') {
          cell.textContent = m.created ? fmtDate(m.created) : '';
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
