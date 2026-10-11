// Search-results grid: the Search window's content area wearing the main
// grid's column mechanics. grid.js itself cannot serve here — its Grid
// bakes in the checkbox column, multi-select, drag-drop rows and filter
// funnels (content the Search window must not grow), and its setColumns
// drops ids outside the catalog (the search-only Source column would
// vanish) — so this module reuses what grid.js exports (the column
// catalog, the persistence writer AND loader, the Type-column text, the
// sizing floors) and mirrors the rest of its mechanics: boundary-handle
// resize (pointer and keyboard, floor and ceiling clamped), header
// drag-to-reorder, and the right-click column picker. The rows stay the
// window's own contract — a plain streaming list, single select,
// Enter/arrows — now windowed: a run can match more rows than any DOM
// should seat at once, so the body renders a growing window of the
// model while the status bar keeps the true count.
import { el, fmtBytes, fmtDate, fileIcon, srcIconEl } from './util.js';
import { resizeDrag, resizeKeys, reorderDrag } from './coldrag.js';
import { t } from './i18n.js';
import { COLUMNS, DEFAULT_COLS, saveColState, loadColState, typeOf, MIN_COL_W, RZ_HIT_W } from './grid.js';

const STORE_KEY = 's3b-cols-sr'; // beside 's3b-cols' / 's3b-cols-local'

// A run can match far more rows than any DOM should seat at once (the
// limit field defaults to ALL matches), so the body renders a window of
// the model: SR_WINDOW rows to start, SR_GROW more per press of the
// footer’s Show more. The model keeps every hit, the status bar keeps
// the true count, and the footer speaks both — a world smaller than the
// window sees nothing at all (the strip hides, every row renders).
const SR_WINDOW = 500;
const SR_GROW = 1000;

// The search catalog: the app's global column set plus one search-only
// column. Source is the auto column — it rides runs that span origins and
// parks last (the far edge of the row); it resizes like any column but
// never reorders and never lists in the picker. Its default width fits
// the type badge plus a source/bucket pairing whole — a clipped origin
// is unreadable metadata.
const SOURCE_COL = { id: 'source', labelKey: 'col.source', w: 180 };
const CATALOG = [...COLUMNS, SOURCE_COL];
const byId = new Map(CATALOG.map((c) => [c.id, c]));

// The out-of-box visible set IS the panes' own — one default order for
// every grid the app seats (Type on by default), so the Search window
// can never drift from the main view's layout. A saved choice
// (s3b-cols-sr) always wins.
export const SR_DEFAULT_COLS = [...DEFAULT_COLS];

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
// window exists at all. Both go through grid.js's shared loader with the
// search catalog (byId) as the width set, so a persisted Source width
// survives — see loadColState there.
export function storedSearchCols() {
  const st = loadColState(STORE_KEY, byId);
  return st && st.cols.length ? [...st.cols] : [...SR_DEFAULT_COLS];
}

export function applyStoredCols(ids) {
  const st = loadColState(STORE_KEY, byId);
  saveColState(STORE_KEY, cleanCols(ids), (st && st.widths) || {});
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
  // the windowed tail: when the model outgrows the rendered window this
  // strip speaks the whole truth — how many rows are shown, how many
  // exist, and the verb that grows the window. It sits OUTSIDE the
  // listbox (a real button is not an option), pinned under the list.
  const moreText = el('span', { class: 'sr-window-text' });
  const moreBtn = el('button', { class: 'btn', type: 'button' });
  moreBtn.addEventListener('click', () => { cap += SR_GROW; renderRows(); });
  const moreStrip = el('div', { class: 'sr-window hidden' }, moreText, moreBtn);
  const area = el('div', { class: 'sr-results' }, headClip, body, moreStrip);

  let userCols = [...SR_DEFAULT_COLS]; // ordered catalog columns (never Source)
  let widths = {};                  // column id -> user-set pixel width
  let headCells = [];               // the .gh elements, in display order
  let showSource = false;           // multi-origin run only (the old rule)
  let sortKey = '';                 // '' = arrival order, the streaming default
  let sortDir = 1;
  let selKey = null;                // the one selected hit (click / arrows)
  let cap = SR_WINDOW;               // the rendered window (Show more grows it)
  const hits = [];                  // streamed results, arrival order
  // sourceOf resolves a hit's source name to its {type, color} so the
  // Source column can wear the type badge; the window injects it from the
  // sources list it opened with (null here = names stay plain text)
  const sourceOf = opts.sourceOf || null;

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
    // the search head already speaks columnheader, so the sort state is
    // aria-sort here — and the verb still rides a real button (the same
    // roving grammar the main headers own: one tab stop, arrows, Enter)
    const seatLabel = () => {
      const l = t(c.labelKey);
      if (sortKey !== c.id) return `${l}, activate to sort ascending`;
      return `${l}, sorted ${sortDir > 0 ? 'ascending' : 'descending'}, activate to reverse`;
    };
    const seat = el('button', {
      class: 'gh-sort', type: 'button', 'data-col': c.id, 'aria-label': seatLabel(),
    }, el('span', { text: t(c.labelKey) }), ind);
    seat.addEventListener('keydown', seatKeys);
    seat.addEventListener('focus', () => seatSort(c.id));
    const cell = el('div', {
      class: `gh${c.num ? ' num' : ''}`,
      'data-col': c.id,
      role: 'columnheader',
      'aria-sort': sortKey === c.id ? (sortDir > 0 ? 'ascending' : 'descending') : 'none',
      onclick: () => {
        const held = document.activeElement && document.activeElement.classList.contains('gh-sort')
          && head.contains(document.activeElement) ? document.activeElement.dataset.col : null;
        if (sortKey === c.id) sortDir = -sortDir;
        else { sortKey = c.id; sortDir = 1; }
        buildHead();
        renderRows();
        if (held) head.querySelector(`.gh-sort[data-col="${held}"]`)?.focus();
      },
    }, seat);
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

  // the seat grammar, closure-shaped: one roving tab stop over the
  // head's sort buttons (the sorted column by default), arrows with
  // wraparound, Home and End at the ends — every owned key stopped, so
  // nothing else in the popout hears it.
  const seatKeys = (e) => {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    const seats = Array.from(head.querySelectorAll('.gh-sort'));
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
  };
  const seatSort = (id) => {
    headCells.forEach((cell) => {
      const b = cell.querySelector('.gh-sort');
      if (b) b.tabIndex = cell.dataset.col === id ? 0 : -1;
    });
  };
  const updateSeats = () => {
    seatSort(sortKey && headCells.some((cell) => cell.dataset.col === sortKey) ? sortKey : headCells[0]?.dataset.col);
  };

  const buildHead = () => {
    headCells = cols().map(headerCell);
    head.replaceChildren(handlesLayer(), ...headCells);
    applyTemplate();
    updateSeats();
  };

  const sortVal = (r) => (sortKey === 'size' ? (isDirOf(r) ? (r.contentSize == null ? -1 : r.contentSize) : (r.size || 0))
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
  // contract); folder rows carry their own dates, class and ETag where the
  // source reports them, and size fills from the window's lazy usage walk
  // (contentSize), exactly like the main grid's folder rows — an unfilled
  // folder stays an honest blank
  const sizeOf = (r) => (isDirOf(r) ? (r.contentSize == null ? null : r.contentSize) : (r.size || 0));
  const cellText = (c, r) => {
    switch (c.id) {
      case 'type': return hitType(r);
      case 'mode': return r.mode || '';
      case 'size': {
        const s = sizeOf(r);
        return s == null ? '' : fmtBytes(s);
      }
      case 'lastModified': return r.lastModified ? fmtDate(r.lastModified) : '';
      case 'created': return r.created ? fmtDate(r.created) : '';
      case 'storageClass': return r.storageClass || '';
      case 'etag': return r.etag || '';
      case 'source': return originOf(r);
      default: return '';
    }
  };

  // main-grid parity: a click picks the row, a double-click (or Enter)
  // opens the hit — a stray single click never navigates. Opt-in columns
  // (beyond the window's own default set) wear the same quiet 'extra'
  // hook the main grid's opt-ins carry.
  // paintSourceCell seats the type badge before the Source column's
  // origin text — the identity format the breadcrumb roots, the sidebar
  // rows and the pickers all wear — whenever the name resolves through
  // the window's sourceOf; an unresolved name keeps its plain text,
  // honest rather than a wrong badge.
  function paintSourceCell(cell, r) {
    const s = r.source && sourceOf ? sourceOf(r.source) : null;
    if (!s) return;
    cell.replaceChildren(srcIconEl(s.type, s.color), document.createTextNode(originOf(r)));
  }
  function rowEl(r, i) {
    const row = el('div', {
      class: `grid-row${r.key === selKey ? ' sel' : ''}`,
      role: 'option',
      'aria-selected': r.key === selKey ? 'true' : 'false',
      id: `sr-row-${i}`, // the body’s activedescendant names the selected hit
    });
    row.dataset.key = r.key; // the in-place walk finds its seat by key
    row.appendChild(el('div', { class: 'gc name' },
      el('span', { class: 'icon', text: fileIcon(r.key, isDirOf(r)) }),
      el('span', { class: 'tname', text: r.key })));
    for (const c of cols()) {
      if (c.id === 'name') continue;
      const extra = SR_DEFAULT_COLS.includes(c.id) || c.id === 'source' ? '' : ' extra';
      const cell = el('div', { class: `gc${c.num ? ' num' : ''} ${c.id}${extra}` });
      cell.textContent = cellText(c, r);
      if (c.id === 'etag') cell.title = cell.textContent;
      if (c.id === 'source') paintSourceCell(cell, r);
      row.appendChild(cell);
    }
    row.addEventListener('click', () => selectRow(r));
    row.addEventListener('dblclick', () => opts.onActivate?.(r));
    return row;
  }

  // every seat change patches in place (the walk and the click share one
  // law) — and each must keep the body naming its selected row
  const syncActive = () => {
    const sel = body.querySelector('.grid-row.sel');
    if (sel) body.setAttribute('aria-activedescendant', sel.id);
    else body.removeAttribute('aria-activedescendant');
  };
  // the rendered window: the model may hold far more than the DOM seats.
  // The strip hides while everything fits — small worlds render whole,
  // exactly the old law
  const renderRows = () => {
    const list = sorted();
    const shown = Math.min(list.length, cap);
    body.replaceChildren(...list.slice(0, shown).map(rowEl));
    syncActive();
    if (shown < list.length) {
      moreText.textContent = t('findShown', { shown, total: list.length });
      moreBtn.textContent = t('findMore');
      moreStrip.classList.remove('hidden');
    } else moreStrip.classList.add('hidden');
  };

  // the walk and the click patch in place — a full rebuild per keypress
  // would re-seat every row under the cursor at small scales and freeze
  // outright at large ones. The seat scrolls into view and the body
  // names it for assistive tech.
  const selectRow = (r) => {
    selKey = r.key;
    let seat = null;
    for (const x of body.querySelectorAll('.grid-row')) {
      const on = x.dataset.key === r.key;
      x.classList.toggle('sel', on);
      x.setAttribute('aria-selected', on ? 'true' : 'false');
      if (on) seat = x;
    }
    if (seat) {
      seat.scrollIntoView({ block: 'nearest' });
      body.setAttribute('aria-activedescendant', seat.id);
    } else body.removeAttribute('aria-activedescendant');
  };

  // Enter opens the selected hit, the arrows walk the list — the body is
  // focusable (tabindex 0) like the main grid
  body.addEventListener('keydown', (e) => {
    // the walk speaks the rendered window — the footer’s verb is how the
    // window grows, and a walk past the edge would name a row no reader
    // could see
    const list = sorted().slice(0, cap);
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

  // resizeAdapter hands the shared drag/keyboard choreography (coldrag.js)
  // this pane's width model — the same shape grid.js builds for its own.
  const resizeAdapter = (c) => {
    const cell = headCells[cols().indexOf(c)];
    if (!cell) return null;
    return {
      cell,
      floor: resizeFloor(c),
      ceiling: (w) => resizeCeiling(w),
      current: () => widths[c.id],
      setWidth: (w) => { widths[c.id] = w; applyTemplate(); },
      finish: persist,
    };
  };

  // pin the stretch column (the edge must follow the pointer), then hand
  // the boundary-handle drag to the shared choreography
  const startResize = (e, c, rz) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    if (!c.flex) freezeStretch(); // pin the stretch column: the edge must follow the pointer
    const m = resizeAdapter(c);
    if (m) resizeDrag(e, rz, m);
  };

  // the keyboard ladder mirrors the pointer one (the shared coldrag.js
  // ladder over this pane's adapter): arrows nudge by 8 (Shift: 32),
  // Home/End snap to floor/ceiling — every handle is a focusable
  // separator, so column widths never need a mouse
  const keyResize = (e, c) => {
    if (!c.flex) freezeStretch();
    const m = resizeAdapter(c);
    if (m) resizeKeys(e, m);
  };

  // a double-click on a handle gives the column its catalog width back
  const resetWidth = (c) => {
    if (widths[c.id] === undefined) return;
    delete widths[c.id];
    applyTemplate();
    persist();
  };

  // header drag-to-reorder over the shared choreography — a plain click
  // still sorts
  const startColDrag = (e, c, cell) => {
    reorderDrag(e, c, cell, {
      skip: '.gh-resize',
      head,
      cells: () => headCells,
      apply: (col, target) => {
        const from = userCols.indexOf(col.id);
        // dropping straight back where it started is a no-op; Source is
        // never a drop target — it is not in userCols and stays last
        if (target !== from && target !== from + 1) {
          const ids = [...userCols];
          ids.splice(from, 1);
          ids.splice(target > from ? target - 1 : target, 0, col.id);
          setColumns(ids);
          persist();
        }
      },
    });
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
  const stored = loadColState(STORE_KEY, byId);
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
    clearHits() { hits.length = 0; selKey = null; cap = SR_WINDOW; renderRows(); },
    count: () => hits.length,
    reset() { hits.length = 0; selKey = null; sortKey = ''; showSource = false; cap = SR_WINDOW; buildHead(); renderRows(); },
    // sizeDirs lists the folder hits whose recursive size no walk has
    // answered yet — the window's lazy fill walks those and hands the
    // answers back through setSizes
    sizeDirs() { return hits.filter((r) => isDirOf(r) && r.contentSize == null); },
    // setSizes applies walked folder sizes ({source, bucket, key, size})
    // and repaints without re-sorting: the list keeps its arrival order or
    // the user's own sort choice, never jumps under the cursor mid-run
    setSizes(list) {
      if (!Array.isArray(list) || !list.length) return;
      const m = new Map(list.map((s) => [`${s.source || ''}|${s.bucket || ''}|${s.key}`, s.size]));
      let touched = false;
      for (const r of hits) {
        if (!isDirOf(r) || r.contentSize != null) continue;
        const v = m.get(`${r.source || ''}|${r.bucket || ''}|${r.key}`);
        if (v == null) continue;
        r.contentSize = v;
        touched = true;
      }
      if (touched) renderRows();
    },
  };
}
