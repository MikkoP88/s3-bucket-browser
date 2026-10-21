// Virtualized, sortable, multi-select details grid.⁠​‌‌‌​​‌‌​​‌‌​​‌‌​‌‌​​​‌​​​‌​‌‌​‌​‌‌‌​​​​​‌‌‌​​‌​​‌‌​‌‌‌‌​‌‌‌​‌‌​​‌‌​​‌​‌​‌‌​‌‌‌​​‌‌​​​​‌​‌‌​‌‌‌​​‌‌​​​‌‌​‌‌​​‌​‌​​‌​‌‌​‌​‌‌‌​‌‌​​​‌‌​​​‌​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌​​​​‌‌​‌‌​‌‌‌‌​‌‌‌​​​​​‌‌‌‌​​‌​‌‌‌​​‌​​‌‌​‌​​‌​‌‌​​‌‌‌​‌‌​‌​​​​‌‌‌​‌​​​​‌​​​​​​​‌​‌​​​​‌‌​​​‌‌​​‌​‌​​‌​​‌​​​​​​​‌‌​​‌​​​‌‌​​​​​​‌‌​​‌​​​‌‌​‌‌​​​‌​​​​​​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​​‌​​​​​​‌​‌​​​​​‌‌​​‌​‌​‌‌‌​​‌‌​‌‌​‌‌‌‌​‌‌​‌‌‌​​‌‌​​‌​‌​‌‌​‌‌‌​​​‌​​​​​​​‌​‌​​​​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​‌​‌​​​​​​‌‌‌​​​​​‌‌‌​​​​​‌​‌​​‌​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌​‌​​​​​‌‌​‌‌‌‌​‌‌​‌‌​​​‌‌‌‌​​‌​‌​​​‌‌​​‌‌​‌‌‌‌​‌‌‌​​‌​​‌‌​‌‌​‌​​‌​​​​​​‌​​‌​​‌​‌‌​‌‌‌​​‌‌‌​‌​​​‌‌​​‌​‌​‌‌‌​​‌​​‌‌​‌‌‌​​‌‌​​​​‌​‌‌​‌‌​​​​‌​​​​​​‌​‌​‌​‌​‌‌‌​​‌‌​‌‌​​‌​‌​​‌​​​​​​‌​​‌‌​​​‌‌​‌​​‌​‌‌​​​‌‌​‌‌​​‌​‌​‌‌​‌‌‌​​‌‌‌​​‌‌​‌‌​​‌​‌​​‌​​​​​​​‌‌​​​‌​​‌​‌‌‌​​​‌‌​​​​​​‌​‌‌‌​​​‌‌​​​​​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌‌​​‌‌‌​‌‌​‌​​‌​‌‌‌​‌​​​‌‌​‌​​​​‌‌‌​‌​‌​‌‌​​​‌​​​‌​‌‌‌​​‌‌​​​‌‌​‌‌​‌‌‌‌​‌‌​‌‌​‌​​‌​‌‌‌‌​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​‌​‌​​​​​​‌‌‌​​​​​‌‌‌​​​​​‌​‌‌‌‌​‌‌‌​​‌‌​​‌‌​​‌‌​​‌​‌‌​‌​‌‌​​​‌​​‌‌‌​‌​‌​‌‌​​​‌‌​‌‌​‌​‌‌​‌‌​​‌​‌​‌‌‌​‌​​​​‌​‌‌​‌​‌‌​​​‌​​‌‌‌​​‌​​‌‌​‌‌‌‌​‌‌‌​‌‌‌​‌‌‌​​‌‌​‌‌​​‌​‌​‌‌‌​​‌​⁠
// Renders only the visible slice (+overscan) so a 100k-object folder scrolls
// at full frame rate (performance budget). The column set is dynamic: the
// catalog below lists every possible column, setColumns() picks the visible
// ones (Settings exposes them; "name" is always visible). Columns resize by
// dragging their header edge — one overlay handle per boundary (renderHandles)
// that also resizes from the keyboard — and reorder by dragging the header
// itself; order and widths persist per pane (loadColState/saveColState below).
import { el, fmtBytes, fmtDate, fileIcon } from './util.js';
import { resizeDrag, resizeKeys, reorderDrag } from './coldrag.js';
import { t, has } from './i18n.js';

const ROW_H = 28;
const OVERSCAN = 8;

// The big-folder law: a streamed listing may offer far more rows than any
// view should materialize (a 100M-object folder is real). The grid holds a
// SLIDING WINDOW over the listing — never a cap with a door: the first walk
// streams FIRST_STEP rows, further windows load themselves as the viewport
// approaches the loaded edge (on.needRows — no continuation control of any
// kind), memory stays bounded by trimming the far edge past MAX_HELD, and
// start-after checkpoints let a scroll back above the window refill it from
// the wire. The scrollbar spans the known extent — S3 answers no total
// count, so it grows as the walk deepens, compressed past the DOM's own
// pixel ceiling (pxScale) rather than lying or breaking.
export const FIRST_STEP = 100000; // rows the initial walk streams before pausing
export const WIN_STEP = 25000;    // rows each demand window adds at the loaded edge
export const MAX_HELD = 200000;   // window ceiling — the far edge trims past it
const SCROLL_ROWS_CAP = 1000000; // rows the canvas can spell in pixels (the 33M px DOM ceiling)
const PREFETCH_ROWS = 1500;      // viewport proximity that arms a demand window
const FILL_H = 28;               // open-end liveness row height, canvas pixels
const APPLY_MS = 200;            // coalesced apply cadence while a stream runs

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
// engines without the attribute render the cell empty. Folder rows
// follow the same presence-driven contract — a folder's dates, class and
// ETag render where its engine reports them (a bucket's creation, a
// marker's metadata, a directory's real timestamps) — and size renders
// for folders too, but only once a lazy usage walk fills contentSize: a
// listing cannot know a folder's content size on its own, so an
// unfilled folder keeps an honest blank (the Properties dialog walks on
// demand the same way).
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

// Both grids share the sizing floors (srgrid imports them) — one place to
// keep in step with .gh-resize.
export const MIN_COL_W = 48; // resize floor for fixed-width columns
export const RZ_HIT_W = 10;  // boundary-handle hit width

// Column-layout persistence, one key per pane ('s3b-cols' remote,
// 's3b-cols-local' side pane, 's3b-cols-sr' search window). The value is
// JSON { cols: [ordered ids], widths: { id: px } }; values written before
// widths existed are plain CSV id lists and still load (order only).
// loadColState never throws — a corrupt or unknown entry reads as "no
// preference". widthKnown optionally widens the accepted WIDTH ids (the
// search pane passes its catalog so a persisted Source width survives);
// the cols list always stays catalog-only — Source's presence is the
// run's shape, never a saved choice.
export function loadColState(key, widthKnown) {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return null;
    const known = new Set(COLUMNS.map((c) => c.id));
    const wKnown = widthKnown || known;
    if (!raw.startsWith('{')) {
      return { cols: raw.split(',').map((s) => s.trim()).filter((id) => known.has(id)), widths: {} };
    }
    const v = JSON.parse(raw);
    const cols = Array.isArray(v?.cols)
      ? v.cols.filter((id) => known.has(id)) : [];
    const widths = {};
    for (const [id, w] of Object.entries(v?.widths || {})) {
      if (wKnown.has(id) && Number.isFinite(w) && w >= MIN_COL_W && w <= 4000) widths[id] = Math.round(w);
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
// FTP, WebDAV — shows the same column for the same file name; the
// search pane feeds it key basenames the same way.
export function typeOf(name, isDir) {
  if (isDir) return t('type.folder');
  const s = String(name || '');
  const i = s.lastIndexOf('.');
  if (i > 0 && i < s.length - 1) {
    const ext = s.slice(i + 1).toLowerCase();
    if (has(`type.${ext}`)) return t(`type.${ext}`);
    return t('type.extFile', { ext: ext.toUpperCase() });
  }
  return t('type.file');
}

// sizeOf is a row's Size answer: a file's own bytes, or a folder's lazy
// recursive content size once a usage walk fills it. null while the walk
// has not answered — an unfilled folder is unknown, never a false zero.
const sizeOf = (r) => (r.isDir ? (r.contentSize == null ? null : r.contentSize) : (r.size || 0));

// colText returns the text a filter matches against for one column: the
// rendered value (sizes formatted, dates localized) plus the raw bytes for
// size, so both "MB" and "1048576" hit. Engine-optional fields match only
// where the engine supplied a value — folder rows included, so a dated
// folder answers a date filter, and a size-filled folder answers a size
// filter while an unfilled one matches nothing.
function colText(r, id) {
  switch (id) {
    case 'name': return r.name || '';
    case 'type': return typeOf(r.name, r.isDir);
    case 'mode': return r.mode || '';
    case 'size': {
      const s = sizeOf(r);
      return s == null ? '' : `${fmtBytes(s)} ${s}`;
    }
    case 'lastModified': return r.lastModified || r.modTime ? fmtDate(r.lastModified || r.modTime) : '';
    case 'created': return r.created ? fmtDate(r.created) : '';
    case 'storageClass': return r.storageClass || '';
    case 'etag': return r.etag || '';
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

    // streaming session state (the big-folder law): see beginStream
    this.streaming = false;
    this.streamDone = false; // the listing finished (or no session runs)
    this.tune = null;        // rigs' seat: window constants at a seedable scale
    this.winTarget = 0;      // absolute listing index the current fetch stops at
    this.baseIndex = 0;      // listing rows trimmed above the held window
    this.tops = [];          // start-after checkpoints [{after, index}] (never empty while open)
    this.needDown = false;   // a downward fetch is in flight
    this.needUp = false;     // a backward (upward) fetch is in flight
    this.upBuf = [];         // backfilled rows awaiting one settleUp paint
    this.upDone = false;     // the walk reached the listing's head — nothing above
    this.upHit = false;      // a backfill met the held window (its early stop)
    this.upStart = 0;        // absolute listing index of upBuf's first row (the fill's target)
    this.upScanned = 0;      // rows the current up demand passed over (the chase's budget)
    this.winGrew = true;     // last window added display rows (filter-chain gate)
    this.winFrom = 0;        // display rows before the current window fetch
    this.universe = null;    // exact total once a full walk landed (null = open)
    this.applyTimer = null;  // coalesced apply while streaming
    this.fillEl = null;      // open-end liveness row (lazily built in renderFill)
    this.pxScale = 1;        // rows per pool slot when the extent outgrows the pixel ceiling
    this.downErrAt = 0;      // last fetch failure (epoch ms) — the demand retry's backoff

    this.pool = [];
    this.accepts = null; // optional mime list override (side-pane bindings)
    this.showMarkers = true; // delete-marker badges on (Settings toggle)
    this.on = {}; // callbacks: select, activate, context, dragstart, drop, badgeV, badgeM
    this.setColumns(DEFAULT_COLS);
    this.body.addEventListener('scroll', () => { this.syncHeadScroll(); this.positionHandles(); this.render(); });
    // pane width changes (split drag, window resize) and the scrollbar
    // appearing/disappearing (row count changes) both re-lock the head width
    // and re-seat the boundary handles. A re-render rides the same observer
    // now, and the body joins the watched set: the body's box changes with
    // the window height AND with text zoom (root zoom scales the layout
    // viewport itself), and a taller window — or a zoom step that fits more
    // rows — must extend the pool exactly the way a scroll always did
    this.ro = new ResizeObserver(() => { this.syncHeadWidth(); this.positionHandles(); this.render(); });
    this.ro.observe(this.headClip);
    this.ro.observe(this.canvas);
    this.ro.observe(this.body);
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

  // resetCols restores the out-of-box layout — the default visible set,
  // in the default order, at catalog widths with the stretch column
  // absorbing the leftover width again (the header picker's
  // "Reset columns": one click recovers a scrambled pane).
  resetCols() {
    this.widths = {};
    this.setColumns(DEFAULT_COLS);
  }

  // gridTemplate is the CSS grid-template-columns for head + rows. The flex
  // column (name) absorbs the remaining width until it is sized — by the
  // user, or by freezeStretch pinning it at the first neighbor resize. A
  // sized flex column becomes a FIXED track (a minmax(minW, w) cap would
  // still absorb shrinkage from growing neighbors, sliding boundaries
  // instead of overflowing honestly) and a trailing filler track takes the
  // rest, so the last column never stretches.
  gridTemplate() {
    const parts = this.cols.map((c) => {
      if (c.flex && !this.widths[c.id]) return `minmax(${c.minW}px,3fr)`;
      return `${Math.round(this.widths[c.id] || c.w)}px`;
    });
    if (this.cols.some((c) => c.flex && this.widths[c.id])) parts.push('minmax(0,1fr)');
    return `34px ${parts.join(' ')}`;
  }

  // colWidth is a column's current effective width (user-set or catalog).
  colWidth(c) { return this.widths[c.id] || c.w; }

  // freezeStretch pins the stretch column at its current rendered width
  // the first time any other column is resized. While the flex track is
  // live mid-template, growing a neighbor steals from it — the dragged
  // edge stands still and every boundary between the two slides instead
  // (the classic "resize moves all the columns" trap). Pinned, the
  // template ends with a filler track: the dragged edge follows the
  // pointer, growth eats the filler first, and only real overflow
  // scrolls. Double-clicking the flex column's own handle thaws it
  // again (resetWidth) so it resumes absorbing the leftover width.
  freezeStretch() {
    const flex = this.cols.find((c) => c.flex && this.widths[c.id] === undefined);
    if (!flex) return;
    const i = this.cols.indexOf(flex);
    this.widths[flex.id] = Math.min(4000,
      Math.round(this.headCols[i].getBoundingClientRect().width));
  }

  // resizeFloor is the least width a drag may give a column: the flex
  // column bottoms out at its template minimum, fixed columns at
  // MIN_COL_W. Clamping the drag here (not at MIN_COL_W alone) keeps the
  // pointer tracking at the floor — minmax(minW, w < minW) silently
  // resolves to minW, so the last stretch of a narrowing drag would look
  // dead while the pointer keeps moving.
  resizeFloor(c) { return c.flex ? c.minW : MIN_COL_W; }

  // resizeCeiling caps a column at the rows pane's own width — growth
  // past the remaining space is honest overflow (the grid scrolls
  // sideways, the head riding along in lockstep), never width stolen
  // from the other columns. Floored at the column's current width so a
  // rightward drag in an already-overflowing layout tracks the pointer
  // instead of snapping the column down on its first move.
  resizeCeiling(curW) {
    return Math.max(curW, Math.round(this.body.clientWidth));
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
    this.fillEl = null; // wiped with the canvas; renderFill rebuilds on demand
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
    this.updateSortSeats();
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
  // A boundary flush with the pane's right edge (grid exactly full, or
  // scrolled to the end) would clip the handle's outer half out of the
  // window — half the grip gone, and with it the guarantee that every
  // edge is grabbable. Such a handle is pulled fully inside; a boundary
  // scrolled past the edge keeps its handle with it, out of sight.
  positionHandles() {
    const layer = this.head.querySelector('.gh-handles');
    if (!layer) return;
    const hl = this.head.getBoundingClientRect().left;
    const paneW = this.headClip.clientWidth;
    const sl = this.body.scrollLeft || 0;
    this.headCols.forEach((cell, i) => {
      const h = layer.children[i];
      if (!h) return;
      const b = cell.getBoundingClientRect().right - hl; // head-local content x
      const v = b - sl; // where the boundary shows in the pane right now
      const seated = v <= paneW ? Math.min(v, paneW - RZ_HIT_W / 2) : v;
      h.style.left = `${seated - RZ_HIT_W / 2}px`;
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
      h.setAttribute('aria-valuemax', String(this.resizeCeiling(w)));
      h.setAttribute('aria-valuenow', String(w));
      h.setAttribute('aria-valuetext', `${w}px`);
    });
  }

  // sortSeatLabel speaks the column's live sort state as the seat's
  // accessible name — the rows are options (a listbox grammar), so the
  // state rides the name rather than aria-sort.
  sortSeatLabel(c) {
    const label = t(c.labelKey);
    if (this.sortKey !== c.id) return `${label}, activate to sort ascending`;
    return `${label}, sorted ${this.sortDir > 0 ? 'ascending' : 'descending'}, activate to reverse`;
  }

  // seatSort walks the roving tab stop onto one column's seat — one
  // stop for the whole header row, the menubar's law. Focus itself
  // reseats: reaching a seat by pointer moves the stop with it.
  seatSort(id) {
    this.headCols.forEach((cell, i) => {
      const b = cell.querySelector('.gh-sort');
      if (b) b.tabIndex = this.cols[i] && this.cols[i].id === id ? 0 : -1;
    });
  }

  // updateSortSeats seats the roving stop at the sorted column (the
  // first column when nothing is sorted) — renderHead's tail.
  updateSortSeats() {
    const id = this.cols.some((c) => c.id === this.sortKey) ? this.sortKey : this.cols[0]?.id;
    this.seatSort(id);
  }

  // sortSeatKeys walks the seats under the arrows (wraparound, Home and
  // End at the ends) and stops every key it owns — the listing's
  // document-level walk never moves under a seated header. Modified
  // keys pass through untouched.
  sortSeatKeys(e) {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    const seats = Array.from(this.head.querySelectorAll('.gh-sort'));
    const i = seats.indexOf(e.currentTarget);
    let j = null;
    if (e.key === 'ArrowRight') j = (i + 1) % seats.length;
    else if (e.key === 'ArrowLeft') j = (i - 1 + seats.length) % seats.length;
    else if (e.key === 'Home') j = 0;
    else if (e.key === 'End') j = seats.length - 1;
    else return;
    e.preventDefault();
    e.stopPropagation();
    if (seats[j]) seats[j].focus();
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
    // The sort verb rides a real button so the keyboard owns it: a
    // click bubbles to the cell's own handler, Enter/Space fire the same
    // click natively, and the roving seat (one tab stop for the whole
    // row — updateSortSeats) walks between columns under the arrows. The
    // accessible name carries the live sort state because the listing
    // speaks a listbox grammar (role=option rows), not columnheaders —
    // the state belongs in the name, not aria-sort.
    const seat = el('button', {
      class: 'gh-sort', type: 'button', 'data-col': c.id,
      'aria-label': this.sortSeatLabel(c),
    }, el('span', { text: label }), ind);
    seat.addEventListener('keydown', (e) => this.sortSeatKeys(e));
    seat.addEventListener('focus', () => this.seatSort(c.id));
    const cell = el('div', {
      class: `gh${c.num ? ' num' : ''}`,
      'data-col': c.id,
      title: t('col.dragTip'),
      onclick: () => this.cycleSort(c.id),
    }, seat, funnel);
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
      this.winGrew = true; // a new needle re-arms the demand chain
      this.apply();
    });
    inp.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); inp.blur(); }
      else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        this.colFilters[id] = '';
        this.winGrew = true;
        this.apply();
        this.renderHead();
      }
    });
    inp.addEventListener('blur', () => this.renderHead());
  }

  cycleSort(id) {
    // a seated keyboard sort must survive its own head rebuild — the
    // render below replaces the focused button, so the seat is captured
    // and re-focused (pointer sorts never steal focus this way: the
    // capture only fires when the seat itself already held it)
    const held = document.activeElement && document.activeElement.classList.contains('gh-sort')
      && this.head.contains(document.activeElement) ? document.activeElement.dataset.col : null;
    if (this.sortKey === id) this.sortDir = -this.sortDir;
    else { this.sortKey = id; this.sortDir = 1; }
    this.apply();
    this.renderHead();
    if (held) this.head.querySelector(`.gh-sort[data-col="${held}"]`)?.focus();
  }

  // ---------- column drag: resize + reorder ----------

  // resizeAdapter hands the shared drag/keyboard choreography (coldrag.js)
  // this grid's width model: the dragged column's head cell, its
  // floor/ceiling, the user-width store behind current/setWidth (head +
  // pooled rows restyle per move — a pool is ~40 rows, one inline style
  // each), and the on.colsChanged persist.
  resizeAdapter(c) {
    return {
      cell: this.headCols[this.cols.indexOf(c)],
      floor: this.resizeFloor(c),
      ceiling: (w) => this.resizeCeiling(w),
      current: () => this.widths[c.id],
      setWidth: (w) => { this.widths[c.id] = w; this.applyTemplate(); },
      finish: () => this.on.colsChanged?.(),
    };
  }

  // startResize: pin the stretch column (the edge must follow the
  // pointer), then hand the boundary-handle drag to the shared
  // choreography.
  startResize(e, c, rz) {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    if (!c.flex) this.freezeStretch(); // pin the stretch column: the edge must follow the pointer
    resizeDrag(e, rz, this.resizeAdapter(c));
  }

  // keyResize: the shared keyboard ladder over the same floor/ceiling a
  // pointer drag respects — ArrowLeft/Right nudge by 8px (Shift ×4),
  // Home/End jump to the bounds.
  keyResize(e, c) {
    if (!c.flex) this.freezeStretch(); // same edge-follows-pointer contract as a drag
    resizeKeys(e, this.resizeAdapter(c));
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

  // startColDrag arms header drag-to-reorder over the shared
  // choreography — a plain click still sorts; the apply step rebuilds
  // this grid's column order (setColumns rebuilds head + pool; widths
  // ride along).
  startColDrag(e, c, cell) {
    reorderDrag(e, c, cell, {
      skip: '.gh-resize, .gh-filter, .gh-cfilter',
      head: this.head,
      cells: () => this.headCols,
      apply: (col, target) => {
        const from = this.cols.indexOf(col);
        // dropping immediately before/after the source cell is a no-op
        if (target !== from && target !== from + 1) {
          const ids = this.cols.map((x) => x.id);
          ids.splice(from, 1);
          ids.splice(target > from ? target - 1 : target, 0, col.id);
          this.setColumns(ids);
          this.on.colsChanged?.();
        }
      },
    });
  }

  // ---------- data ----------
  setRows(rows) {
    // a wholesale swap ends any streaming session: window bookkeeping and
    // the deferred prune all belong to the model that just left. The
    // swap's model IS the universe — nothing sits outside it.
    this.endSession();
    this.universe = rows.length;
    this.all = rows;
    this.apply();
  }

  // appendRows extends the model with rows that arrive already in display
  // order (streaming listing, M5). Avoids re-sorting the whole array per
  // page; call apply() once when the stream ends if the sort must change.
  appendRows(rows) {
    if (!rows.length) return;
    // a trailing tombstone band materializes first: the down wire walks
    // from lastKey, and the band's slots are exactly the listing rows
    // that follow it — those land in their slots, only the remainder
    // pushes past the old edge
    let band = 0;
    while (band < rows.length && this.all.length - 1 - band >= 0
      && this.all[this.all.length - 1 - band] === null) band++;
    if (band) this.all.splice(this.all.length - band, band, ...rows.slice(0, band));
    this.all.push(...rows.slice(band));
    if (!this.hasFilters() && this.sortKey === 'name' && this.sortDir === 1) {
      this.rows.push(...rows);
      this.render();
      if (this.on.model) this.on.model(); // a streamed page is a model change too — the tiles follow
    } else if (this.streaming) {
      this.applySoon(); // filter or non-default sort mid-stream: coalesce
    } else {
      this.apply(); // filter or non-default sort: full recompute
    }
  }

  // removeRows drops rows by key — the inverse of appendRows, for rows a
  // plain listing can never return (ghost reconciliation on refresh).
  removeRows(keys) {
    if (!keys.length) return;
    const drop = new Set(keys);
    this.all = this.all.filter((r) => r && !drop.has(r.key)); // tombstones survive row drops — geometry, not rows
    this.apply();
  }

  // ---------- big-folder streaming (the 100M-object law) ----------
  // A streamed listing may hold far more rows than any view should
  // materialize. The grid answers with a SLIDING WINDOW over the
  // listing, never a cap with a door: the first walk streams tuned
  // ('first') rows, further windows load themselves as the viewport
  // approaches the loaded edge (checkNeed -> on.needRows — no
  // continuation control exists), the far edge trims past tuned('max')
  // so memory stays bounded however deep the user walks, and start-after
  // checkpoints (tops) refill the window when the viewport climbs back
  // above it. The scrollbar spans the known extent — S3 answers no total
  // count — compressed past the DOM's own pixel ceiling (pxScale)
  // rather than lying or breaking.

  // tuned reads a window constant through the rigs' seat.
  tuned(k) {
    const v = this.tune ? this.tune[k] : 0;
    if (v) return v;
    return { first: FIRST_STEP, win: WIN_STEP, max: MAX_HELD, prefetch: PREFETCH_ROWS, cap: SCROLL_ROWS_CAP }[k] || 0;
  }

  // setStreamTune retunes the window constants — the rigs' seat, so
  // batteries can walk the window law at a seedable scale while
  // production rides the shipped constants. A falsy tune clears the
  // seat again (the harness restores the shipped constants at its tail).
  setStreamTune(t) {
    this.tune = t ? {
      first: Math.max(1, Math.round(t.first || 0)),
      win: Math.max(1, Math.round(t.win || 0)),
      max: Math.max(1, Math.round(t.max || 0)),
      prefetch: Math.max(1, Math.round(t.prefetch || 0)),
      cap: Math.max(1, Math.round(t.cap || 0)),
    } : null;
  }

  // beginStream opens a streaming session: the window resets, the
  // universe count reopens, and filtered applies coalesce (applySoon) so
  // a page never pays a full re-sort on top of the wire. The initial
  // walk IS the first downward fetch — needDown stays armed until the
  // walk's settle, so the demand engine cannot double-fire under it.
  beginStream() {
    this.streaming = true;
    this.streamDone = false;
    this.winTarget = this.baseIndex + this.all.length + this.tuned('first');
    this.winFrom = 0;
    this.winGrew = true;
    this.baseIndex = 0;
    this.tops = [{ after: '', index: 0 }];
    this.needDown = true;
    this.needUp = false;
    this.upHit = false;
    this.upBuf = [];
    this.upDone = false;
    this.upStart = 0;
    this.upScanned = 0;
    this.universe = null;
    this.renderFill(); // the walk's liveness wash seats with the session
  }

  // windowFull is the pause oracle: the current downward fetch stops
  // once the walk's absolute edge holds its target of listing rows —
  // ABSOLUTE, because the trim inside appendStreamRows can hand back a
  // held count below the target the same fetch just reached (a window
  // fetched to absolute 2,000 trims to 1,500 held; a held-count oracle
  // would never pause, the stream would run to the folder's end, and
  // its done page would settle a universe the window never accepted).
  windowFull() { return this.baseIndex + this.all.length >= this.winTarget; }

  // pushTop seats a start-after checkpoint: `after` is the last key
  // known contiguous at listing index `index` (rows from that index
  // refetch from that key — the listing's ascending order makes every
  // observed key/index pair a true binding, whichever direction the
  // walk observed it in). The index-0 entry never drops; the ring stays
  // bounded so a very long walk cannot grow it without end — and the
  // eviction keeps a SPREAD, thinning the older half's odd steps, so a
  // climb at any depth always has an anchor within reach: a linear tail
  // would strand a 100M-row scrollback miles from every checkpoint.
  pushTop(after, index) {
    const last = this.tops[this.tops.length - 1];
    if (last && last.index === index) { last.after = String(after || ''); return; }
    this.tops.push({ after: String(after || ''), index });
    if (this.tops.length > 192) {
      const half = this.tops.length >> 1;
      for (let i = half - 1; i >= 1; i -= 2) this.tops.splice(i, 1);
      if (this.tops.length > 192) this.tops.splice(1, this.tops.length - 192);
    }
  }

  // topBelow picks the tightest checkpoint at or below a listing index —
  // the resume point a backward fill wants.
  topBelow(index) {
    if (!this.tops.length) return { after: '', index: 0 };
    let best = this.tops[0];
    for (const cp of this.tops) if (cp.index <= index && cp.index >= best.index) best = cp;
    return best;
  }

  // appendStreamRows is the window-aware append: rows land until the
  // fetch's target is met, the accepted slice comes back so the caller's
  // own bookkeeping (tree dirs, ghost merge) holds exactly what the grid
  // holds, and every page boundary seats a checkpoint for the backward
  // fill that may someday scroll back to it.
  appendStreamRows(rows) {
    if (!rows.length) return rows;
    const room = this.winTarget - (this.baseIndex + this.all.length);
    let acc = rows;
    if (room <= 0) acc = [];
    else if (rows.length > room) acc = rows.slice(0, room);
    if (acc.length) {
      this.appendRows(acc);
      this.pushTop(this.lastKey(), this.baseIndex + this.all.length);
      this.trimTop();
    }
    return acc;
  }

  // settleDown closes the current downward fetch — every exit of the
  // host's stream loop lands here, error paths included (a dangling
  // needDown would starve every later demand). `done` marks the listing
  // exhausted: the universe settles but the session NEVER folds — a walk
  // completed deep in a huge folder holds rows trimmed above it, and the
  // climb back must still refill them, so the windowed geometry (canvas,
  // checkpoints, stride) stands past done. Only the downward side dies
  // with streamDone; a wholesale setRows is the one true fold. Windowed,
  // the listing's own order IS the settled order — folders-first is
  // display space's law and takes over the moment a sort or filter
  // leaves native order.
  settleDown(info) {
    // a settle with no open session and no dangling fetch is a stray —
    // folding hands already settled must not re-apply the window grammar
    if (!this.streaming && !this.needDown && !this.needUp) return;
    this.needDown = false;
    this.downErrAt = info && info.error ? Date.now() : 0;
    this.winGrew = !this.hasFilters() || this.rows.length > this.winFrom;
    if (info && info.done) {
      this.streamDone = true;
      this.universe = this.baseIndex + this.all.length;
      this.applyWindow();
      this.renderFill(); // retires the wash — the open end is now closed
      return;
    }
    this.applyWindow();
    this.renderFill();
  }

  // endSession folds an open session: every window field retires and
  // the final apply runs the full folders-first grammar — the listing
  // order the window preserved yields to the display order every
  // settled view carries.
  endSession() {
    this.streaming = false;
    this.streamDone = true;
    this.winTarget = 0;
    this.baseIndex = 0;
    this.tops = [];
    this.needDown = false;
    this.needUp = false;
    this.upBuf = [];
    this.upDone = false;
    this.upStart = 0;
    this.upScanned = 0;
    this.winGrew = true;
    this.winFrom = 0;
    if (this.applyTimer) { clearTimeout(this.applyTimer); this.applyTimer = null; }
    this.all = this.all.filter(Boolean); // the fold drops tombstone slots — display space holds only rows
    this.apply();
    this.renderFill();
  }

  // appendBackRows is the backward-fill append: pages arrive ascending
  // from the checkpoint, and every row's TRUE slot is the checkpoint's
  // own binding — cp.index + k, whatever the walk met on its way. So
  // acceptance is slot-based, never key-based: a row lands only where
  // the model is missing (above the window's head, or inside a gapped
  // fill's tombstone band), rows the model already holds pass over
  // silently toward the gap, and the first held ground met ABOVE the
  // new rows ends the walk (upHit — the early stop). Acceptance stops
  // at one window's worth so a single demand stays bounded; the pass-
  // over has its own budget (one first window), and every examined page
  // seats its boundary checkpoint — a long chase through held ground
  // never walks the same page twice.
  appendBackRows(rows, done, cp) {
    if (done) this.upDone = true;
    const cpIndex = (cp && cp.index) || 0;
    const win = this.tuned('win');
    const acc = [];
    for (let k = 0; k < rows.length; k++) {
      if (this.upBuf.length + acc.length >= win) break; // one window's worth per demand
      const rel = cpIndex + k - this.baseIndex;
      const held = rel >= 0 && rel < this.all.length && !!this.all[rel];
      if (held) {
        if (acc.length) { this.upHit = true; break; } // the fill met held ground above it
        this.upScanned++;
        if (this.upScanned >= this.tuned('first')) { this.upHit = true; break; }
        continue;
      }
      if (rel >= this.all.length) { this.upHit = true; break; } // below the model: the down wire's ground
      if (!this.upBuf.length && !acc.length) this.upStart = cpIndex + k;
      acc.push(rows[k]);
    }
    this.upBuf = this.upBuf.concat(acc);
    if (rows.length) this.pushTop(String(rows[rows.length - 1].key), cpIndex + rows.length);
    return acc.length;
  }

  // backfillDone is the backward fetch's stop oracle: the head of the
  // listing, the held window met, or the buffer full all end the walk.
  backfillDone() { return this.upDone || this.upHit || this.upBuf.length >= this.tuned('win'); }

  // settleUp closes the backward fetch: the buffered rows land in ONE
  // paint at their TRUE slots. A fill that reached or passed the model's
  // head prepends (the unbridged span between the fill's end and the old
  // head seats as tombstones — honest blanks for rows not yet held,
  // never a silent gap the contiguous arithmetic would misplace); a fill
  // inside the held span splices straight into its tombstone band. The
  // canvas never shifts — blanks become rows exactly where the viewport
  // already sat, so no scroll compensation exists (the old compensated
  // prepend jumped the view a full window down on every climb); the tail
  // trims past the ceiling and the fill's boundary seats as a checkpoint.
  // Returns the count for the host's own mirror.
  settleUp() {
    // the stray-settle law of settleDown, mirrored: nothing in flight and
    // nothing buffered means there is nothing to settle
    if (!this.needUp && !this.upBuf.length) return 0;
    const n = this.upBuf.length;
    if (n) {
      const lastKeyAbove = this.upBuf[n - 1].key;
      const lo = this.upStart;
      const hi = lo + n;
      const rel = lo - this.baseIndex;
      if (rel <= 0) {
        // at/above the model's head: prepend, tombstoning the unbridged
        // span. A negative gap is the crossing fill — its tail overlaps
        // the old head's own leading tombstones (acceptance guarantees
        // every covered held slot was one), so the overlap drops out
        const gap = this.baseIndex - hi;
        const ts = gap > 0 ? new Array(gap).fill(null) : [];
        const overlap = gap < 0 ? -gap : 0;
        this.all = this.upBuf.concat(ts, this.all.slice(overlap));
        this.baseIndex = lo;
      } else {
        // inside the held span: the tombstone band materializes in place
        this.all = this.all.slice(0, rel).concat(this.upBuf, this.all.slice(rel + n));
      }
      this.upBuf = [];
      this.pushTop(lastKeyAbove, hi);
      this.trimBottom();
      this.applyWindow();
      this.render();
    }
    this.needUp = false;
    this.upHit = false;
    this.upScanned = 0;
    this.renderFill();
    return n;
  }

  // trimTop drops the oldest held rows past the ceiling — never into
  // the viewport (a screen of headroom stays), compensating the scroll
  // so the view does not jump, seating the boundary checkpoint, and
  // telling the host (on.trim) so its own mirror stays aligned.
  trimTop() {
    const max = this.tuned('max');
    if (this.all.length <= max) return;
    const over = this.all.length - max;
    const win = this.isWindowed();
    let safe;
    if (win) {
      const absTop = (this.body.scrollTop / ROW_H) * this.pxScale;
      const screen = (this.body.clientHeight / ROW_H) * this.pxScale;
      safe = Math.max(0, Math.floor(absTop - screen) - this.baseIndex);
    } else {
      safe = Math.max(0, Math.floor(this.body.scrollTop / ROW_H) - 2);
    }
    const cut = Math.min(over, safe);
    if (cut <= 0) return;
    // the boundary checkpoint seats at the cut span's last REAL row — a
    // tombstone has no key to bind (the ring's older entries already
    // cover territory the cut leaves keyless)
    let real = -1;
    for (let i = cut - 1; i >= 0; i--) if (this.all[i]) { real = i; break; }
    if (real >= 0) this.pushTop(this.all[real].key, this.baseIndex + real + 1);
    const cutRows = this.all.slice(0, cut);
    const cutSet = new Set(cutRows);
    this.all = this.all.slice(cut);
    this.baseIndex += cut;
    let cutReal = 0; // the mirror counts rows, never tombstone slots
    for (const r of cutRows) {
      if (!r) continue;
      cutReal++;
      this.sel.delete(r.key);
      if (this.focusKey === r.key) this.focusKey = null;
      if (this.anchorKey === r.key) this.anchorKey = null;
    }
    this.rows = this.rows.filter((r) => !cutSet.has(r));
    if (win) this.body.scrollTop = Math.max(0, this.body.scrollTop - (cut / this.pxScale) * ROW_H);
    else {
      let kept = 0;
      for (const r of cutRows) if (r && this.passesFilters(r)) kept++;
      this.body.scrollTop = Math.max(0, this.body.scrollTop - kept * ROW_H);
    }
    this.on.trim?.(cutReal);
  }

  // trimBottom is the backfill's mirror: the tail trims past the
  // ceiling once the viewport keeps a screen of room below it. No
  // compensation is needed — only content below the view leaves.
  trimBottom() {
    const max = this.tuned('max');
    if (this.all.length <= max) return;
    const over = this.all.length - max;
    const absBot = ((this.body.scrollTop + this.body.clientHeight) / ROW_H) * this.pxScale - this.baseIndex;
    const screen = (this.body.clientHeight / ROW_H) * this.pxScale;
    const safe = Math.max(0, Math.floor(this.all.length - absBot - screen));
    const cut = Math.min(over, safe);
    if (cut <= 0) return;
    const dropSet = new Set(this.all.slice(this.all.length - cut));
    this.all = this.all.slice(0, this.all.length - cut);
    this.rows = this.rows.filter((r) => !dropSet.has(r));
    let cutReal = 0; // the mirror counts rows, never tombstone slots
    for (const r of dropSet) {
      if (!r) continue;
      cutReal++;
      this.sel.delete(r.key);
      if (this.focusKey === r.key) this.focusKey = null;
      if (this.anchorKey === r.key) this.anchorKey = null;
    }
    this.on.trimEnd?.(cutReal);
  }

  // checkNeed arms the demand engine at every render's end: in windowed
  // mode the viewport's edges speak in listing space (through the
  // stride), upward when the window's head sits within reach (a
  // backward fill from the tightest checkpoint), downward at the loaded
  // edge; in display space (filter or off-native sort on an open
  // listing) the loaded edge is the filtered bottom. One fetch at a
  // time — the view owns a single stream slot — and the winGrew gate
  // parks a filter's chain after a window that added nothing (never
  // walk 100M rows for an absent needle; the next filter change
  // re-arms it).
  checkNeed() {
    if (!this.streaming) return;
    if (this.needDown || this.needUp) return;
    // a failed fetch backs off five seconds — a dead provider must not
    // turn the render loop into a retry storm (the up-branch stays live:
    // its failure is a courtesy repair, not an error surface)
    if (this.downErrAt && Date.now() - this.downErrAt < 5000) return;
    const pre = this.tuned('prefetch');
    if (this.isWindowed()) {
      const absTop = (this.body.scrollTop / ROW_H) * this.pxScale;
      const absBot = absTop + (this.body.clientHeight / ROW_H) * this.pxScale;
      // the up-branch outlives done: a completed walk still holds its
      // trimmed-above rows, and the climb back refills them — the arm
      // fires on a head within reach OR any tombstone band the viewport
      // can see (a gap's blanks demand their rows exactly the way a
      // trimmed-away head does)
      const relTop = Math.floor(Math.max(0, absTop - pre)) - this.baseIndex;
      const relBot = Math.floor(absBot) - this.baseIndex;
      let miss = relTop < 0;
      if (!miss) {
        const stop = Math.min(this.all.length - 1, relBot);
        for (let i = Math.max(0, relTop); i <= stop; i++) {
          if (this.all[i] === null) { miss = true; break; }
        }
      }
      if (miss) {
        this.needUp = true;
        this.upScanned = 0;
        this.on.needRows?.('up', this.topBelow(Math.max(0, Math.floor(absTop))));
        return;
      }
      // ...but the down-branch dies at the END, never at done: a done
      // walk still holds rows below a climbed window (the climb's
      // trimBottom parked them), and the dive back re-arms the same
      // window fetch — the known universe is the empty-fetch guard
      const heldBot = this.baseIndex + this.all.length;
      if (!(this.streamDone && heldBot >= this.universe)
        && absBot + pre >= heldBot && (this.winGrew || !this.hasFilters())) this.fireDown();
    } else {
      const bottom = this.rows.length - (this.body.scrollTop + this.body.clientHeight) / ROW_H;
      if (!(this.streamDone && this.rows.length >= this.universe)
        && bottom < pre && (this.winGrew || !this.hasFilters())) this.fireDown();
    }
  }

  // fireDown opens the next downward window at the loaded edge. The
  // target is the ABSOLUTE edge the fetch walks to (baseIndex plus
  // held) — trims move baseIndex, never the absolute truth.
  fireDown() {
    if (this.needDown || this.needUp) return;
    this.needDown = true;
    this.winTarget = this.baseIndex + this.all.length + this.tuned('win');
    this.winFrom = this.rows.length;
    this.renderFill(); // the fetch's liveness wash seats with the demand
    this.on.needRows?.('down', null);
  }

  // isWindowed: the geometry mode where the canvas spells listing
  // space — an open, native-order, unfiltered session. Done does not
  // end it (the climb back still refills); only a fold (setRows) or a
  // departure into display space (sort or filter) does.
  isWindowed() {
    return !!(this.streaming && this.sortKey === 'name' && this.sortDir === 1 && !this.hasFilters());
  }

  // extent: the listing rows the canvas spans. Loaded rows — S3
  // answers no total count, so the scrollbar grows as the walk
  // deepens; pxScale keeps it honest past the pixel ceiling.
  extent() { return this.baseIndex + this.all.length; }

  // calcScale seats the stride: one pool slot per row until the extent
  // would outgrow the DOM's own pixel ceiling, then rows compress into
  // slots — the display reads as a stride, an honest proportional
  // scrollbar past ~33.5M pixels, never a broken canvas. A stride change
  // re-anchors the scroll in listing space so the viewport keeps its
  // rows instead of jumping.
  calcScale() {
    const prev = this.pxScale;
    this.pxScale = this.isWindowed() ? Math.max(1, Math.ceil(this.extent() / this.tuned('cap'))) : 1;
    if (this.pxScale !== prev && this.body.scrollTop > 0) {
      this.body.scrollTop = ((this.body.scrollTop / ROW_H) * prev / this.pxScale) * ROW_H;
    }
  }

  // modelInfo reports the honest counts a status bar owes: what is
  // shown, what the universe holds when that is known exactly, and —
  // for an open window — how many listing rows sit held, how many sit
  // above the window, and the stride when the canvas compresses.
  modelInfo() {
    let held = 0; // real rows only — tombstone slots are owed rows, not held ones
    for (const r of this.all) if (r) held++;
    return { shown: this.rows.length, universe: this.universe, held, above: this.baseIndex, scale: this.pxScale };
  }

  // lastKey is the highest key held — the StartAfter anchor for the
  // next window fetch. Arrival order interleaves folders and files (a
  // page's common prefixes come before its objects), so "last" means
  // max, not final.
  lastKey() {
    let k = '';
    for (const r of this.all) if (r && r.key > k) k = r.key;
    return k;
  }

  // applySoon coalesces apply() while a stream runs: the first dirty call
  // schedules one timer and every later call rides it, so a filter or a
  // non-default sort recomputes at most every APPLY_MS however fast the
  // pages land.
  applySoon() {
    if (this.applyTimer) return;
    this.applyTimer = setTimeout(() => {
      this.applyTimer = null;
      this.apply();
    }, APPLY_MS);
  }

  // renderFill seats or retires the open end's liveness row — a passive
  // animated wash, never a control: the window loads itself, so there
  // is nothing to click. It rides while a downward fetch is in flight —
  // a done listing's re-dive is as live as the first walk.
  renderFill() {
    const want = !!(this.streaming && this.needDown);
    if (!want && this.fillEl) { this.fillEl.remove(); this.fillEl = null; }
    else if (want && !this.fillEl) {
      this.fillEl = el('div', { class: 'grid-fill' });
      this.fillEl.setAttribute('aria-hidden', 'true');
      this.canvas.appendChild(this.fillEl);
    }
    this.render();
  }

  setFilter(f) {
    this.filter = f;
    this.winGrew = true; // a new needle re-arms the demand chain
    this.apply();
  }

  // hasFilters: any global or per-column filter active.
  hasFilters() {
    return !!this.filter || this.cols.some((c) => this.colFilters[c.id]);
  }

  // passesFilters: the filter grammar in one place — the navbar filter
  // is global (a hit in ANY visible column keeps the row); per-column
  // funnels stack on top, each matching its own column.
  passesFilters(r) {
    const q = (this.filter || '').toLowerCase();
    if (q && !this.cols.some((c) => colText(r, c.id).toLowerCase().includes(q))) return false;
    for (const c of this.cols) {
      const f = this.colFilters[c.id];
      if (f && !colText(r, c.id).toLowerCase().includes(f.toLowerCase())) return false;
    }
    return true;
  }

  apply() {
    // an open native-order session keeps the listing's own order — the
    // window geometry (placeholders, checkpoints, stride) depends on it;
    // folders-first and every other sort settle at the session's fold or
    // once the view leaves native order (filter or sort: display space)
    if (this.isWindowed()) return this.applyWindow();
    let rows = this.all.filter(Boolean); // tombstones never enter display space
    if (this.hasFilters()) rows = rows.filter((r) => this.passesFilters(r));
    const dir = this.sortDir;
    const key = this.sortKey;
    rows = [...rows].sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1; // folders first, always
      let va = a[key], vb = b[key];
      if (key === 'type') { va = typeOf(a.name, a.isDir); vb = typeOf(b.name, b.isDir); }
      else if (key === 'size') {
        // an unfilled folder sorts below a known-zero one: unknown, not empty
        va = sizeOf(a) ?? -1; vb = sizeOf(b) ?? -1;
        return (va < vb ? -1 : va > vb ? 1 : 0) * dir;
      }
      else if (key === 'lastModified' || key === 'created') {
        va = va || 0; vb = vb || 0;
        return (va < vb ? -1 : va > vb ? 1 : 0) * dir;
      }
      va = String(va || '').toLowerCase(); vb = String(vb || '').toLowerCase();
      return (va < vb ? -1 : va > vb ? 1 : 0) * dir;
    });
    this.rows = rows;
    // prune selection to existing rows — deferred while a stream runs (the
    // model only grows mid-fetch; the fold's final apply prunes once)
    if (!this.streaming) {
      const live = new Set(rows.map((r) => r.key));
      for (const k of this.sel) if (!live.has(k)) this.sel.delete(k);
    }
    this.render(true); // force: re-emit select (selection may have been pruned)
    if (this.on.model) this.on.model(); // the gallery re-seats its tiles on every model change
  }

  // applyWindow is the open session's apply: filter without sorting —
  // the display keeps the provider's own listing order so the window
  // geometry holds. It never aliases this.all into this.rows (trims and
  // backfills swap the held array), tombstones never enter display rows
  // (a blank slot is geometry, not a row), and it never prunes the
  // selection (the model only grows mid-fetch; trims prune their own
  // cuts).
  applyWindow() {
    let rows = this.all.filter(Boolean);
    if (this.hasFilters()) rows = rows.filter((r) => this.passesFilters(r));
    this.rows = rows;
    this.render(true);
    if (this.on.model) this.on.model();
  }

  selectedRows() { return this.rows.filter((r) => this.sel.has(r.key)); }

  rowByKey(key) { return this.rows.find((r) => r.key === key) || null; }

  // setCmp decorates rows with directory-compare statuses (name -> status)
  // and repaints; null clears the decorations.
  setCmp(map) {
    for (const r of this.all) if (r) r.cmp = map ? (map.get(r.name) || '') : '';
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
      if (!r) continue; // a tombstone slot carries nothing to decorate
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
    // a stable pool id lets the body’s aria-activedescendant name the
    // focused row — the pool index never changes, only the model under it
    row.id = `${this.body.id}-r${this.pool.length}`;
    row.style.gridTemplateColumns = this.gridTemplate();
    // the row checkbox is pointer-only by law: a listbox option owns no
// focusable descendants, the walk’s Space owns keyboard selection,
// and the head’s select-all stays the one checkbox in the Tab order
row.appendChild(el('div', { class: 'gc check' }, el('input', { type: 'checkbox', tabindex: '-1' })));
    for (const c of this.cols) {
      if (c.id === 'name') {
        // the badges are pointer shortcuts, not buttons — the verb lives
        // in the row context menu and the counts read as plain text; a
        // button role with no focus path would be a lie
        row.appendChild(el('div', { class: 'gc name' },
          el('span', { class: 'icon' }),
          el('span', { class: 'tname' }),
          el('span', { class: 'vbadge' }),
          el('span', { class: 'mbadge' })));
      } else {
        // columns beyond the default four carry their values on a quiet
        // gray wash so opt-in columns read as overlay data at a glance
        const extra = DEFAULT_COLS.includes(c.id) ? '' : ' extra';
        row.appendChild(el('div', { class: `gc${c.num ? ' num' : ''} ${c.id}${extra}` }));
      }
    }
    this.wireRow(row);
    return row;
  }

  render(force = false) {
    const win = this.isWindowed();
    if (win) this.calcScale();
    const slots = win ? Math.ceil(this.extent() / this.pxScale) : this.rows.length;
    // the open end's liveness row (while a fetch runs) extends the canvas
    // and sits right past the last slot
    this.canvas.style.height = `${slots * ROW_H + (this.fillEl ? FILL_H : 0)}px`;
    if (this.fillEl) this.fillEl.style.top = `${slots * ROW_H}px`;
    const scrollTop = this.body.scrollTop;
    const h = this.body.clientHeight;
    const first = Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN);
    const last = Math.min(slots - 1, Math.ceil((scrollTop + h) / ROW_H) + OVERSCAN);
    // slot -> model: windowed slots spell listing space (through the
    // stride when the extent outgrows the pixel ceiling); a slot outside
    // the held window renders as a blank placeholder — the honest
    // not-loaded region above a trimmed window
    const p = this.pxScale;
    const bi = this.baseIndex;
    const al = this.all.length;
    const rs = this.rows;
    const rowAt = (i) => {
      if (!win) return rs[i] || null;
      const rel = Math.floor(i * p) - bi;
      return rel >= 0 && rel < al ? this.all[rel] : null;
    };

    // recycle pool
    let focusRowId = null; // the pool row holding focusKey, for activedescendant
    const need = Math.max(0, last - first + 1);
    while (this.pool.length < need) {
      const row = this.makeRow();
      this.canvas.appendChild(row); // pool rows live in the canvas; recycled via display/top
      this.pool.push(row);
    }
    for (let i = 0; i < this.pool.length; i++) {
      const row = this.pool[i];
      const idx = first + i;
      if (idx > last) {
        // a hidden pool row keeps no ghost state: rows re-enter range
        // with every class recomputed, so the classes a hidden row
        // carries are stale — a computed-style read (or a bug) can trip
        // on a selection the viewport cannot even show
        row.style.display = 'none';
        row.classList.remove('sel', 'focus', 'ghost');
        row.setAttribute('aria-selected', 'false');
        continue;
      }
      const m = rowAt(idx);
      row.style.display = '';
      row.style.top = `${idx * ROW_H}px`;
      row._model = m;
      if (!m) {
        // placeholder: nothing held at this slot — a quiet blank, inert
        row.classList.remove('sel', 'focus', 'ghost');
        row.classList.add('placeholder');
        row.setAttribute('aria-selected', 'false');
        row.dataset.cmp = '';
        const cells = row.children;
        const pcb = cells[0].children[0];
        pcb.checked = false;
        pcb.setAttribute('aria-label', '');
        for (let ci = 0; ci < this.cols.length; ci++) {
          const cell = cells[1 + ci];
          const c = this.cols[ci];
          if (c.id === 'name') {
            cell.children[0].textContent = '';
            cell.children[1].textContent = '';
            cell.children[2].textContent = '';
            cell.children[2].title = '';
            cell.children[3].textContent = '';
            cell.children[3].title = '';
          } else {
            cell.textContent = '';
            cell.title = '';
          }
        }
        continue;
      }
      row.classList.remove('placeholder');
      row.classList.toggle('sel', this.sel.has(m.key));
      row.classList.toggle('focus', m.key === this.focusKey);
      row.classList.toggle('ghost', !!(m.ghost || m.dimmed));
      row.setAttribute('aria-selected', this.sel.has(m.key) ? 'true' : 'false');
      if (m.key === this.focusKey) focusRowId = row.id;
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
          vb.textContent = vn ? `⟲ ${vn}` : '';
          vb.title = vn ? t('ver.count', { n: vn }) : '';
          // marker badge: files carry at most one marker so they show the
          // bare flag; directories aggregate a count. Rendered when
          // enabled in Settings; click opens the Delete Marker window
          const mb = cell.children[3];
          const hasM = this.showMarkers === true && !!m.mcount;
          mb.textContent = hasM ? (m.isDir ? `⛔ ${m.mcount}` : '⛔') : '';
          mb.title = hasM ? (m.isDir ? t('mark.count', { n: m.mcount }) : t('mark.has')) : '';
        } else if (c.id === 'type') {
          cell.textContent = typeOf(m.name, m.isDir);
        } else if (c.id === 'mode') {
          cell.textContent = m.mode || '';
        } else if (c.id === 'etag') {
          cell.textContent = m.etag || '';
          cell.title = cell.textContent;
        } else if (c.id === 'size') {
          const s = sizeOf(m);
          cell.textContent = s == null ? '' : fmtBytes(s);
        } else if (c.id === 'lastModified') {
          cell.textContent = m.lastModified || m.modTime ? fmtDate(m.lastModified || m.modTime) : '';
        } else if (c.id === 'created') {
          cell.textContent = m.created ? fmtDate(m.created) : '';
        } else if (c.id === 'storageClass') {
          cell.textContent = m.storageClass || '';
        }
      }
    }
    // aria-activedescendant names the focused row so assistive tech speaks
    // the seat without per-row DOM focus — the walk, the click, the
    // type-ahead jump and the drag all render, so this one write covers
    // every path that can move focus
    if (focusRowId) this.body.setAttribute('aria-activedescendant', focusRowId);
    else this.body.removeAttribute('aria-activedescendant');
    // header checkbox reflects the full selection state
    if (this.headCb) {
      const n = this.rows.length;
      this.headCb.checked = n > 0 && this.sel.size >= n;
      this.headCb.indeterminate = this.sel.size > 0 && this.sel.size < n;
    }
    if (force) this.on.select?.(this.selectedRows());
    // the demand engine rides every render: a viewport that approached a
    // loaded edge arms the next window fetch
    this.checkNeed();
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
      if (!m) return; // a placeholder slot holds nothing to select
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
    row.addEventListener('dblclick', () => {
      if (!row._model) return;
      this.on.activate?.(row._model);
    });
    row.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      const m = row._model;
      if (!m) return;
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
      if (!m) { e.preventDefault(); return; } // a placeholder drags nothing
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
      if (!this.acceptedTypes().some((t) => e.dataTransfer.types.includes(t))) return;
      e.preventDefault();
      row.classList.add('drop-target');
    });
    row.addEventListener('dragleave', () => row.classList.remove('drop-target'));
    row.addEventListener('drop', (e) => {
      row.classList.remove('drop-target');
      if (!row._model?.isDir) return;
      let payload = null;
      for (const t of this.acceptedTypes()) {
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

  // acceptedTypes names the payload MIME types this grid receives on folder
  // seats — the one grammar both faces (rows and tiles) speak on dragover.
  acceptedTypes() {
    return this.accepts || acceptedMimes(this.kind);
  }

  // ---------- keyboard ----------
  keydown(e) {
    const n = this.rows.length;
    if (!n) return false;
    const idx = Math.max(0, this.rows.findIndex((r) => r.key === this.focusKey));
    let handled = true;
    // the Menu key (Shift+F10 its laptop spelling) opens the walked
    // row’s verb menu the way a right-click does — the seat, the
    // selection law and the on.context seam are all the mouse event’s
    // own; only the anchor differs: the row element stands in for the
    // pointer, so the menu lands beside the row (openMenu anchors an
    // element by its rect)
    if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
      this.menuSeat();
      return true;
    }
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

  // menuSeat opens the walked row’s context menu from the keyboard:
  // the focus row is seated into the selection exactly the way the
  // row’s own contextmenu listener seats it, and the anchor handed to
  // on.context is the pool row carrying the model — the menu opens
  // beside the row because there is no pointer to open it at.
  menuSeat() {
    const m = this.rowByKey(this.focusKey);
    if (!m) return;
    if (!this.sel.has(m.key)) {
      this.sel.clear();
      this.sel.add(m.key);
      this.anchorKey = m.key;
      this.render();
      this.on.select?.(this.selectedRows());
    }
    const row = this.pool.find((r) => r._model && r._model.key === m.key);
    if (row) this.on.context?.(row, this.selectedRows());
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
    // display rows speak listing space in windowed mode (through the
    // stride) — the one scroll write beside render's own geometry
    const top = this.isWindowed() ? ((this.baseIndex + idx) / this.pxScale) * ROW_H : idx * ROW_H;
    if (top < this.body.scrollTop) this.body.scrollTop = top;
    else if (top + ROW_H > this.body.scrollTop + this.body.clientHeight)
      this.body.scrollTop = top + ROW_H - this.body.clientHeight;
  }

  typeJump(ch) {
    const i = this.typeFeed(ch);
    if (i >= 0) this.moveSel(i, {});
  }

  // typeFeed folds one typed character into the type-ahead buffer and
  // names the first row whose label carries it (prefix, case-folded) —
  // the commit is the caller's, because the two faces seat the jump
  // differently (rows scroll their body, tiles scroll their element).
  // The buffer lives 600ms past the last key and says so through
  // on.typeahead ('' when it expires) — the hook the buffer chip rides.
  typeFeed(ch) {
    this.typeBuf += ch.toLowerCase();
    clearTimeout(this.typeTimer);
    this.typeTimer = setTimeout(() => {
      this.typeBuf = '';
      this.on.typeahead?.('');
    }, 600);
    this.on.typeahead?.(this.typeBuf);
    return this.rows.findIndex((r) => r.name.toLowerCase().startsWith(this.typeBuf));
  }
}
