// Search-results grid: the Search window's content area wearing the main
// grid's column mechanics. grid.js itself cannot serve here — its Grid
// bakes in the checkbox column, multi-select, drag-drop rows and filter
// funnels (content the Search window must not grow), and its setColumns
// drops ids outside the catalog (the search-only Source column would
// vanish) — so this module reuses what grid.js exports (the column
// catalog, the per-key persistence writer) and mirrors the rest of its
// mechanics: boundary-handle resize (pointer and keyboard, floor and
// ceiling clamped), header drag-to-reorder, and the right-click column
// picker. The rows stay the window's own contract — a plain streaming
// list, single select, Enter/arrows — untouched.
import { el, fmtBytes, fmtDate, fileIcon } from './util.js';
import { t, has } from './i18n.js';
import { COLUMNS, saveColState } from './grid.js';

const MIN_COL_W = 48;           // resize floor for fixed columns (grid.js's)
const RZ_HIT_W = 10;            // handle hit width — keep in step with .gh-resize
const DRAG_THRESHOLD = 6;       // px before a header press becomes a reorder drag
const STORE_KEY = 's3b-cols-sr'; // beside 's3b-cols' / 's3b-cols-local'

// The search catalog: the app's global column set plus one search-only
// column. Source is the auto column — it rides runs that span origins and
// parks last (the far edge of the row); it resizes like any column but
// never reorders and never lists in the picker.
const SOURCE_COL = { id: 'source', labelKey: 'col.source', w: 150 };
const CATALOG = [...COLUMNS, SOURCE_COL];
const byId = new Map(CATALOG.map((c) => [c.id, c]));

// The out-of-box visible set — exactly the columns the window always
// showed. A saved choice (s3b-cols-sr) always wins.
export const SR_DEFAULT_COLS = ['name', 'size', 'lastModified'];

// loadColState mirrors grid.js's loader for the search store: same shape,
// same tolerance (a corrupt or unknown read is simply "no preference"),
// but the known-id set includes Source — the shared loader filters against
// the main catalog only and would drop a persisted Source width. The cols
// list stays catalog-only: Source's presence is the run's shape, never a
// saved choice.
function loadColState(key) {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return null;
    if (!raw.startsWith('{')) {
      return { cols: raw.split(',').map((s) => s.trim()).filter((id) => COLUMNS.some((c) => c.id === id)), widths: {} };
    }
    const v = JSON.parse(raw);
    const cols = Array.isArray(v?.cols)
      ? v.cols.filter((id) => COLUMNS.some((c) => c.id === id)) : [];
    const widths = {};
    for (const [id, w] of Object.entries(v?.widths || {})) {
      if (byId.has(id) && Number.isFinite(w) && w >= MIN_COL_W && w <= 4000) widths[id] = Math.round(w);
    }
    return { cols, widths };
  } catch {
    return null;
  }
}

// cleanCols normalizes an incoming id list the way the live list applies
// it: catalog-only, deduped, name always present — the Settings apply
// path writes the store without an instance at hand, so the rule lives
// once here.
function cleanCols(ids) {
  const seen = new Set();
  const out = [];
  for (const id of Array.isArray(ids) ? ids : []) {
    if (COLUMNS.some((c) => c.id === id) && !seen.has(id)) { seen.add(id); out.push(id); }
  }
  if (!seen.has('name')) out.unshift('name');
  return out;
}

// storedSearchCols reads the persisted set (or the out-of-box one) for
// the Settings draft; applyStoredCols writes a new set, keeping any
// saved widths — the dialog's Save path, which may run while no search
// window exists at all.
export function storedSearchCols() {
  const st = loadColState(STORE_KEY);
  return st && st.cols.length ? [...st.cols] : [...SR_DEFAULT_COLS];
}

export function applyStoredCols(ids) {
  const st = loadColState(STORE_KEY);
  saveColState(STORE_KEY, cleanCols(ids), (st && st.widths) || {});
}

// typeOf mirrors grid.js's Type-column text (not exported there): Folder,
// the friendly name of a common extension, or "<EXT> file" — taken from
// the basename so keys ("docs/notes.md") read like names.
function typeOf(name, isDir) {
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

// makeSearchGrid builds the Search window's results area — head inside
// its clipping band, scrolling body — and owns the column state behind
// both. opts.onActivate(hit) fires on Enter/double-click. Every listener
// is element-scoped: a second window-open call while the popout floats
// builds a discarded subtree here, and document/window-level bindings
// would outlive it (drag listeners are transient, added on press and
// removed on release, exactly like grid.js).
export function makeSearchGrid(opts = {}) {
  const head = el('div', { class: 'grid-head', role: 'row' });
  const headClip = el('div', { class: 'grid-headclip' }, head);
  const body = el('div', { class: 'grid-body sr-list', tabindex: '0', role: 'listbox' });
  const area = el('div', { class: 'sr-results' }, headClip, body);

  let userCols = [...SR_DEFAULT_COLS]; // ordered catalog columns (never Source)
  let widths = {};                  // column id -> user-set pixel width
  let headCells = [];               // the .gh elements, in display order
  let showSource = false;           // multi-origin run only (the old rule)
  let sortKey = '';                 // '' = arrival order, the streaming default
  let sortDir = 1;
  let selKey = null;                // the one selected hit (click / arrows)
  const hits = [];                  // streamed results, arrival order

  const cols = () => [...userCols.map((id) => byId.get(id)), ...(showSource ? [SOURCE_COL] : [])];
  // originOf reads a hit's origin for the Source column and its sort: a
  // source named after its bucket (the credential-import flow's own
  // naming) reads once — the bucket drops out instead of doubling the name
  const originOf = (r) => (r.bucket && r.bucket !== r.source ? `${r.source || ''}/${r.bucket}` : (r.source || ''));
  const isDirOf = (r) => !!(r.isDir || String(r.key).endsWith('/'));
  const hitType = (r) => {
    const k = String(r.key || '');
    const cut = k.lastIndexOf('/');
    return typeOf(cut >= 0 ? k.slice(cut + 1) : k, isDirOf(r));
  };
  const persist = () => saveColState(STORE_KEY, userCols, widths);

  // gridTemplate is the columns' shared track list. It rides ONE custom
  // property on the results area (--sr-tpl): search rows are a plain
  // streaming list, not grid.js's pooled canvas, so restyling every row
  // from JS per pointer move would be O(rows) — the var is one write and
  // native inheritance does the rest. The flex column (name) absorbs the
  // remaining width until sized; a sized flex column turns fixed with a
  // trailing filler (grid.js freezeStretch explains the trap).
  const gridTemplate = () => {
    const cs = cols();
    const parts = cs.map((c) => {
      if (c.flex && widths[c.id] === undefined) return `minmax(${c.minW}px,1fr)`;
      return `${Math.round(widths[c.id] || c.w)}px`;
    });
    if (cs.some((c) => c.flex && widths[c.id] !== undefined)) parts.push('minmax(0,1fr)');
    return parts.join(' ');
  };

  // the head follows the list: width-locked to its client width and slid
  // by its horizontal scroll, so headers stay over their columns in a
  // window narrower than the column set
  const syncHeadWidth = () => { head.style.width = `${body.clientWidth}px`; };
  const syncHeadScroll = () => { head.style.transform = `translateX(${-body.scrollLeft}px)`; };

  // freezeStretch pins the stretch column's current width before any other
  // column resizes: with two elastic tracks an edge drag would feed both
  // and the pointer would lose the column it grabbed.
  const freezeStretch = () => {
    const flex = cols().find((c) => c.flex && widths[c.id] === undefined);
    if (!flex) return;
    const cell = headCells[cols().indexOf(flex)];
    if (cell) widths[flex.id] = Math.min(4000, Math.round(cell.getBoundingClientRect().width));
  };
  const resizeFloor = (c) => (c.flex ? c.minW : MIN_COL_W);
  const resizeCeiling = (curW) => Math.max(curW, Math.round(body.clientWidth));

  const applyTemplate = () => {
    area.style.setProperty('--sr-tpl', gridTemplate());
    syncHeadWidth();
    syncHeadScroll();
    positionHandles();
    updateHandleAria();
  };

  const headerCell = (c) => {
    const ind = el('span', { class: 'sort-ind' });
    if (sortKey === c.id) ind.textContent = sortDir > 0 ? '▲' : '▼';
    const cell = el('div', {
      class: `gh${c.num ? ' num' : ''}`,
      'data-col': c.id,
      role: 'columnheader',
      onclick: () => {
        if (sortKey === c.id) sortDir = -sortDir;
        else { sortKey = c.id; sortDir = 1; }
        buildHead();
        renderRows();
      },
    }, el('span', { text: t(c.labelKey) }), ind);
    // Source never reorders — it owns the row's far edge — so it carries
    // no drag affordance; the catalog columns all do.
    if (c.id !== 'source') {
      cell.title = t('col.dragTip');
      cell.addEventListener('pointerdown', (e) => startColDrag(e, c, cell));
    }
    return cell;
  };

  // handlesLayer is the boundary-resize overlay: first child of the head
  // so the last header keeps its :last-child border, one handle per
  // column boundary, above the cells (grid.js's layout, reused).
  const handlesLayer = () => {
    const layer = el('div', { class: 'gh-handles' });
    layer.replaceChildren(...cols().map((c) => {
      const rz = el('div', {
        class: 'gh-resize', role: 'separator', 'aria-orientation': 'vertical',
        tabindex: '0', 'data-col': c.id,
        'aria-label': `${t(c.labelKey)}: ${t('col.resizeTip')}`, title: t('col.resizeTip'),
      });
      rz.addEventListener('pointerdown', (e) => startResize(e, c, rz));
      rz.addEventListener('dblclick', (e) => { e.stopPropagation(); resetWidth(c); });
      rz.addEventListener('keydown', (e) => keyResize(e, c));
      return rz;
    }));
    return layer;
  };

  // positionHandles seats every boundary handle on its column's right
  // edge; a boundary flush with the pane's right edge pulls its handle
  // fully inside so every edge stays grabbable (grid.js's rule).
  const positionHandles = () => {
    const layer = head.querySelector('.gh-handles');
    if (!layer) return;
    const hl = head.getBoundingClientRect().left;
    const paneW = headClip.clientWidth;
    const sl = body.scrollLeft || 0;
    headCells.forEach((cell, i) => {
      const h = layer.children[i];
      if (!h) return;
      const v = cell.getBoundingClientRect().right - hl - sl;
      const seated = v <= paneW ? Math.min(v, paneW - RZ_HIT_W / 2) : v;
      h.style.left = `${seated - RZ_HIT_W / 2}px`;
    });
  };

  const updateHandleAria = () => {
    const layer = head.querySelector('.gh-handles');
    if (!layer) return;
    cols().forEach((c, i) => {
      const h = layer.children[i];
      if (!h || !headCells[i]) return;
      const w = Math.round(headCells[i].getBoundingClientRect().width);
      h.setAttribute('aria-valuemin', String(resizeFloor(c)));
      h.setAttribute('aria-valuemax', String(resizeCeiling(w)));
      h.setAttribute('aria-valuenow', String(w));
      h.setAttribute('aria-valuetext', `${w}px`);
    });
  };

  const buildHead = () => {
    headCells = cols().map(headerCell);
    head.replaceChildren(handlesLayer(), ...headCells);
    applyTemplate();
  };

  const sortVal = (r) => (sortKey === 'size' ? (isDirOf(r) ? -1 : (r.size || 0))
    : sortKey === 'lastModified' ? (r.lastModified ? new Date(r.lastModified).getTime() : 0)
      : sortKey === 'created' ? (r.created ? new Date(r.created).getTime() : 0)
        : sortKey === 'source' ? originOf(r)
          : sortKey === 'type' ? hitType(r)
            : sortKey === 'mode' ? (r.mode || '')
              : sortKey === 'etag' ? (r.etag || '')
                : sortKey === 'storageClass' ? (r.storageClass || '')
                  : r.key);
  const sorted = () => (sortKey
    ? [...hits].sort((a, b) => {
      const va = sortVal(a); const vb = sortVal(b);
      return (va < vb ? -1 : va > vb ? 1 : 0) * sortDir;
    })
    : hits);

  // cell text per column — engine-optional fields render empty (the Entry
  // contract); dirs blank size, dates, class and ETag, as the main grid's
  // rows do
  const cellText = (c, r) => {
    switch (c.id) {
      case 'type': return hitType(r);
      case 'mode': return r.mode || '';
      case 'size': return isDirOf(r) ? '' : fmtBytes(r.size || 0);
      case 'lastModified': return isDirOf(r) ? '' : (r.lastModified ? fmtDate(r.lastModified) : '');
      case 'created': return r.created ? fmtDate(r.created) : '';
      case 'storageClass': return isDirOf(r) ? '' : (r.storageClass || '');
      case 'etag': return isDirOf(r) ? '' : (r.etag || '');
      case 'source': return originOf(r);
      default: return '';
    }
  };

  // main-grid parity: a click picks the row, a double-click (or Enter)
  // opens the hit — a stray single click never navigates. Opt-in columns
  // (beyond the window's own default set) wear the same quiet 'extra'
  // hook the main grid's opt-ins carry.
  function rowEl(r) {
    const row = el('div', {
      class: `grid-row${r.key === selKey ? ' sel' : ''}`,
      role: 'option',
      'aria-selected': r.key === selKey ? 'true' : 'false',
    });
    row.appendChild(el('div', { class: 'gc name' },
      el('span', { class: 'icon', text: fileIcon(r.key, isDirOf(r)) }),
      el('span', { class: 'tname', text: r.key })));
    for (const c of cols()) {
      if (c.id === 'name') continue;
      const extra = SR_DEFAULT_COLS.includes(c.id) || c.id === 'source' ? '' : ' extra';
      const cell = el('div', { class: `gc${c.num ? ' num' : ''} ${c.id}${extra}` });
      cell.textContent = cellText(c, r);
      if (c.id === 'etag') cell.title = cell.textContent;
      row.appendChild(cell);
    }
    row.addEventListener('click', () => {
      if (selKey === r.key) return;
      selKey = r.key;
      for (const x of body.querySelectorAll('.grid-row')) {
        const on = x === row;
        x.classList.toggle('sel', on);
        x.setAttribute('aria-selected', on ? 'true' : 'false');
      }
    });
    row.addEventListener('dblclick', () => opts.onActivate?.(r));
    return row;
  }

  const renderRows = () => { body.replaceChildren(...sorted().map(rowEl)); };

  const selectRow = (r) => {
    selKey = r.key;
    renderRows();
    body.querySelector('.grid-row.sel')?.scrollIntoView({ block: 'nearest' });
  };

  // Enter opens the selected hit, the arrows walk the list — the body is
  // focusable (tabindex 0) like the main grid
  body.addEventListener('keydown', (e) => {
    const list = sorted();
    if (e.key === 'Enter') {
      e.preventDefault();
      const r = hits.find((x) => x.key === selKey);
      if (r) opts.onActivate?.(r);
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!list.length) return;
      let i = list.findIndex((x) => x.key === selKey);
      if (i < 0) i = e.key === 'ArrowDown' ? 0 : list.length - 1;
      else i = e.key === 'ArrowDown' ? Math.min(list.length - 1, i + 1) : Math.max(0, i - 1);
      selectRow(list[i]);
    }
  });

  const startResize = (e, c, rz) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    if (!c.flex) freezeStretch(); // pin the stretch column: the edge must follow the pointer
    const cs = cols();
    const cell = headCells[cs.indexOf(c)];
    if (!cell) return;
    const startW = cell.getBoundingClientRect().width;
    const startX = e.clientX;
    const floor = resizeFloor(c);
    const maxW = resizeCeiling(startW);
    rz.setPointerCapture?.(e.pointerId);
    rz.classList.add('dragging');
    document.body.classList.add('col-resize-active');
    const move = (ev) => {
      const w = Math.round(Math.min(Math.max(startW + ev.clientX - startX, floor), maxW));
      if (w !== widths[c.id]) {
        widths[c.id] = w;
        applyTemplate();
      }
    };
    const done = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', done);
      window.removeEventListener('pointercancel', done);
      rz.classList.remove('dragging');
      document.body.classList.remove('col-resize-active');
      persist();
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', done);
    window.addEventListener('pointercancel', done);
  };

  // the keyboard ladder mirrors the pointer one: arrows nudge by 8
  // (Shift: 32), Home/End snap to floor/ceiling — every handle is a
  // focusable separator, so column widths never need a mouse
  const keyResize = (e, c) => {
    if (!c.flex) freezeStretch();
    const cs = cols();
    const cell = headCells[cs.indexOf(c)];
    if (!cell) return;
    const cur = Math.round(cell.getBoundingClientRect().width);
    const floor = resizeFloor(c);
    const max = resizeCeiling(cur);
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
    widths[c.id] = w;
    applyTemplate();
    persist();
  };

  // a double-click on a handle gives the column its catalog width back
  const resetWidth = (c) => {
    if (widths[c.id] === undefined) return;
    delete widths[c.id];
    applyTemplate();
    persist();
  };

  const startColDrag = (e, c, cell) => {
    if (e.button !== 0 || e.target.closest('.gh-resize')) return;
    const startX = e.clientX;
    const startY = e.clientY;
    let dragging = false;
    let indicator = null;
    let target = null;
    // the synthetic click after a drag would flip the sort — swallow it
    // on the head (capture fires before the cell's own listener)
    const swallow = (ev) => ev.stopPropagation();
    const boundaryX = (i) => {
      if (i >= headCells.length) return headCells[headCells.length - 1].getBoundingClientRect().right;
      return headCells[i].getBoundingClientRect().left;
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
      for (let i = 0; i <= headCells.length; i++) {
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
        const from = userCols.indexOf(c.id);
        // dropping straight back where it started is a no-op; Source is
        // never a drop target — it is not in userCols and stays last
        if (target !== from && target !== from + 1) {
          const ids = [...userCols];
          ids.splice(from, 1);
          ids.splice(target > from ? target - 1 : target, 0, c.id);
          setColumns(ids);
          persist();
        }
      }
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', done);
    window.addEventListener('pointercancel', done);
  };

  // setColumns applies a visible-column id list (order IS display order;
  // cleanCols normalizes). Source never enters — its presence is the
  // run's shape, not a choice. Hiding the sorted column returns the list
  // to arrival order, the streaming neutral (grid.js falls back to name;
  // a stream has none).
  const setColumns = (ids) => {
    userCols = cleanCols(ids);
    if (!cols().some((c) => c.id === sortKey)) sortKey = '';
    buildHead();
    renderRows();
  };

  const resetCols = () => {
    widths = {};
    setColumns(SR_DEFAULT_COLS);
  };

  // columnPicker is the header's right-click menu — the same check-list
  // against the catalog the main grid's header menu shows ('name' locked
  // on, Reset columns last). It renders into the shared #ctxmenu element
  // (it lives outside #app, so native popout windows carry it too); the
  // app's global outside-click/blur close in main.js covers dismissing.
  const columnPicker = (e) => {
    const menu = document.getElementById('ctxmenu');
    if (!menu) return;
    const hide = () => menu.classList.add('hidden');
    const cur = new Set(userCols);
    const item = (label, fn, disabled = false) => {
      const node = el('div', { class: `item${disabled ? ' disabled' : ''}` }, el('span', { text: label }));
      node.addEventListener('click', () => { if (disabled) return; hide(); fn(); });
      return node;
    };
    menu.replaceChildren(
      ...COLUMNS.map((c) => item(`${cur.has(c.id) ? '✓ ' : ''}${t(c.labelKey)}`, () => {
        const next = new Set(cur);
        if (cur.has(c.id)) next.delete(c.id); else next.add(c.id);
        setColumns([...next]);
        persist();
      }, c.id === 'name')),
      el('div', { class: 'sep' }),
      item(t('col.reset'), () => { resetCols(); persist(); }),
    );
    menu.classList.remove('hidden');
    menu.style.left = `${Math.max(0, Math.min(e.clientX, innerWidth - menu.offsetWidth - 8))}px`;
    menu.style.top = `${Math.max(0, Math.min(e.clientY, innerHeight - menu.offsetHeight - 10))}px`;
  };
  head.addEventListener('contextmenu', (e) => { e.preventDefault(); e.stopPropagation(); columnPicker(e); });

  body.addEventListener('scroll', () => { syncHeadScroll(); positionHandles(); });
  new ResizeObserver(() => { syncHeadWidth(); positionHandles(); updateHandleAria(); }).observe(headClip);

  // restore the persisted layout (or the window's default) and build
  const stored = loadColState(STORE_KEY);
  widths = (stored && stored.widths) || {};
  setColumns(stored && stored.cols.length ? stored.cols : SR_DEFAULT_COLS);

  return {
    area, headClip, body,
    // the Settings Save path re-seats a live window through this (see
    // applySearchCols in dialogs.js); applyStoredCols covers the no-
    // instance case with the same normalization
    applyCols(ids) { setColumns(ids); persist(); },
    setShowSource(v) { if (v === showSource) return; showSource = v; buildHead(); renderRows(); },
    addPage(entries) { hits.push(...entries); renderRows(); },
    clearHits() { hits.length = 0; selKey = null; renderRows(); },
    count: () => hits.length,
    reset() { hits.length = 0; selKey = null; sortKey = ''; showSource = false; buildHead(); renderRows(); },
  };
}
