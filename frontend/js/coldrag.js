// Column drag choreography shared by the two grids — grid.js's objects
// pane and srgrid.js's search-results pane. Both panes own their widths,
// templates and persistence; this module owns only the pointer and
// keyboard mechanics, which must behave identically on both sides (the
// battery's twin drag legs pin that parity). Each entry point takes a
// small adapter ({ cell, floor, ceiling, current, setWidth, finish } or
// { skip, head, cells, apply }) so the panes keep their own models.

import { el } from './util.js';

// px a header press must travel before it becomes a reorder drag — a
// plain click still sorts
const DRAG_THRESHOLD = 6;

// resizeDrag tracks a boundary-handle drag: the column width follows the
// pointer live (the adapter's setWidth restyles head + rows per move),
// clamped to the column's own floor below and the pane's ceiling above —
// whatever the drag adds or removes lands in the template's trailing
// filler first; only past that does the grid scroll. Pointerup persists
// via the adapter's finish. Callers pin the stretch column BEFORE this
// (the edge-follows-pointer contract), so the adapter's ceiling sees a
// settled template.
export const resizeDrag = (e, rz, m) => {
  if (e.button !== 0) return;
  e.preventDefault();
  e.stopPropagation();
  const startW = m.cell.getBoundingClientRect().width;
  const startX = e.clientX;
  const maxW = m.ceiling(startW);
  rz.setPointerCapture?.(e.pointerId); // keep the drag alive past the window edge
  rz.classList.add('dragging');
  document.body.classList.add('col-resize-active');
  const move = (ev) => {
    const w = Math.round(Math.min(Math.max(startW + ev.clientX - startX, m.floor), maxW));
    if (w !== m.current()) m.setWidth(w);
  };
  const done = () => {
    window.removeEventListener('pointermove', move);
    window.removeEventListener('pointerup', done);
    window.removeEventListener('pointercancel', done);
    rz.classList.remove('dragging');
    document.body.classList.remove('col-resize-active');
    m.finish();
  };
  window.addEventListener('pointermove', move);
  window.addEventListener('pointerup', done);
  window.addEventListener('pointercancel', done);
};

// resizeKeys is the keyboard path on a focused handle: ArrowLeft/Right
// nudge by 8px (Shift ×4), Home/End jump to the bounds — all clamped to
// the same floor/ceiling a pointer drag respects. Unhandled keys return
// untouched so the caller's own key handling still sees them.
export const resizeKeys = (e, m) => {
  const cur = Math.round(m.cell.getBoundingClientRect().width);
  const floor = m.floor;
  const max = m.ceiling(cur);
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
  m.setWidth(w);
  m.finish();
};

// reorderDrag arms header drag-to-reorder: a press anywhere on a header
// cell (outside the adapter's skip set — resize handles, inline filters)
// becomes a reorder once the pointer moves past DRAG_THRESHOLD. The
// dragged header dims, a 2px indicator tracks the nearest slot boundary,
// and pointerup hands (column, target slot) to the adapter's apply,
// which owns each pane's column model. The synthetic click after a drag
// would flip the sort — it is swallowed on the head (capture fires
// before the cell's listener) and removed again in cleanup so nothing
// lingers when no click follows.
export const reorderDrag = (e, c, cell, r) => {
  if (e.button !== 0 || e.target.closest(r.skip)) return;
  const startX = e.clientX;
  const startY = e.clientY;
  const head = r.head;
  let dragging = false;
  let indicator = null;
  let target = null;
  const swallow = (ev) => ev.stopPropagation();
  const boundaryX = (i) => {
    const cells = r.cells();
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
    const n = r.cells().length;
    for (let i = 0; i <= n; i++) {
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
    if (dragging && target !== null) r.apply(c, target);
  };
  window.addEventListener('pointermove', move);
  window.addEventListener('pointerup', done);
  window.addEventListener('pointercancel', done);
};
