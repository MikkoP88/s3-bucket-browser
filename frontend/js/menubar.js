// Menu bar component (File / Edit / View / Settings / Help): horizontal
// title buttons, each opening a dropdown below it. Visual language follows
// .ctxmenu. Standalone — imports only the el() DOM helper, no app state.
// The bar answers the keyboard the way desktop menubars do: the titles are
// a roving tab stop (Enter, Space or ArrowDown opens), the open dropdown is
// a menu surface of its own (arrows walk and skip the disabled, Enter picks
// and restores the seat to the title, Escape folds back), and ArrowLeft /
// ArrowRight stride the bar’s menus while one is open — the cascade flying
// out under ArrowRight and folding back under ArrowLeft inside it.
import { el } from './util.js';

// defs = [{ label, items: [...] }]
// item  = { label, kbd, action, enabled, checked } — enabled/checked are
//         plain booleans OR functions (re-evaluated every time the dropdown
//         opens); { label, items: [...] } renders a submenu (one level);
//         null renders a separator.
export function createMenubar(defs) {
  const root = el('div', { class: 'menubar', role: 'menubar' });
  const titles = [];
  const dropdowns = [];
  const subClosers = []; // per-dropdown "close any open submenu" fn
  let openIdx = -1;

  // the bar’s roving tab stop: the title that last held focus carries
  // tabIndex 0, the rest -1 — one seat in the Tab order, arrows inside
  const setStop = (i) => {
    titles.forEach((t, j) => { t.tabIndex = j === i ? 0 : -1; });
  };

  const close = () => {
    if (openIdx < 0) return;
    openIdx = -1;
    dropdowns.forEach((d) => d.classList.add('hidden'));
    titles.forEach((t) => { t.classList.remove('open'); t.setAttribute('aria-expanded', 'false'); });
    subClosers.forEach((c) => c?.());
  };

  // seatFirst / seatLast give the freshly opened dropdown the APG seat:
  // focus on the first (or last) enabled item, so the arrows own it from
  // the very first key — the same law the context menu’s openMenu owns
  const seatFirst = (i) => {
    const f = dropdowns[i].querySelector(':scope > .mb-item:not(.disabled)');
    f?.focus();
    return !!f;
  };
  const seatLast = (i) => {
    const a = dropdowns[i].querySelectorAll(':scope > .mb-item:not(.disabled)');
    a[a.length - 1]?.focus();
  };

  const open = (i) => {
    close();
    openIdx = i;
    renderItems(i);
    dropdowns[i].classList.remove('hidden');
    titles[i].classList.add('open');
    titles[i].setAttribute('aria-expanded', 'true');
    seatFirst(i);
  };

  // renderLeaf builds one clickable menu item (used at both levels).
  const renderLeaf = (it, closeSub) => {
    const enabled = typeof it.enabled === 'function' ? !!it.enabled() : it.enabled !== false;
    const checked = typeof it.checked === 'function' ? !!it.checked() : !!it.checked;
    const attrs = {
      class: `mb-item${enabled ? '' : ' disabled'}${checked ? ' checked' : ''}`,
      role: it.checked === undefined ? 'menuitem' : 'menuitemcheckbox',
      tabindex: '-1',
      onclick: () => { if (!enabled) return; close(); it.action?.(); },
    };
    if (!enabled) attrs['aria-disabled'] = 'true';
    if (it.checked !== undefined) attrs['aria-checked'] = String(checked);
    return el('div', attrs,
      el('span', { class: 'mb-check', text: checked ? '\u2713' : '' }),
      el('span', { text: it.label }),
      it.kbd ? el('span', { class: 'mb-kbd', text: it.kbd }) : null,
    );
  };

  // renderItems rebuilds the dropdown on every open so enabled/checked
  // functions (commandState flags) are re-read at open time.
  function renderItems(i) {
    // the open submenu rides the dropdown element itself, so the mouse
    // paths and the keyboard paths below share one closer
    const closeSub = () => {
      const s = dropdowns[i]._openSub;
      if (s) { s.classList.add('hidden'); s.parentElement?.setAttribute('aria-expanded', 'false'); dropdowns[i]._openSub = null; }
    };
    const openSubAt = (item, sub) => {
      closeSub();
      sub.classList.remove('hidden');
      item.setAttribute('aria-expanded', 'true');
      dropdowns[i]._openSub = sub;
    };
    subClosers[i] = closeSub;
    dropdowns[i]._openSubAt = openSubAt;

    dropdowns[i].replaceChildren(...(defs[i].items || []).map((it) => {
      if (!it) return el('div', { class: 'mb-sep', role: 'separator' });
      if (it.items) { // submenu (one level)
        const sub = el('div', { class: 'mb-dd sub hidden', role: 'menu' },
          ...it.items.map((s) => (s ? renderLeaf(s, closeSub) : el('div', { class: 'mb-sep', role: 'separator' }))));
        const item = el('div', { class: 'mb-item has-sub', role: 'menuitem', tabindex: '-1', 'aria-haspopup': 'menu', 'aria-expanded': 'false' },
          el('span', { class: 'mb-check' }),
          el('span', { text: it.label }),
          el('span', { class: 'mb-arrow', text: '\u25B8' }),
          sub,
        );
        item.addEventListener('mouseenter', () => {
          if (dropdowns[i]._openSub !== sub) openSubAt(item, sub);
        });
        item._sub = sub;
        return item;
      }
      const leaf = renderLeaf(it, closeSub);
      leaf.addEventListener('mouseenter', closeSub);
      return leaf;
    }));
  }

  // ddKeydown is the open dropdown’s keyboard grammar — one listener per
  // dropdown, delegated to whichever .mb-item holds focus (a submenu item
  // included: the sub lives inside the dropdown’s DOM). Every key it
  // handles is stopped here, so the grid walk on the document chain never
  // moves under an open menu.
  const ddKeydown = (e, i) => {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    if (e.key === 'Tab') { close(); return; } // focus leaves on purpose
    const item = e.target.closest('.mb-item');
    if (!item) return;
    const lvl = item.parentElement; // the dropdown or the sub owning the seat
    const inSub = !!lvl && lvl.classList.contains('sub');
    const items = lvl ? [...lvl.querySelectorAll(':scope > .mb-item:not(.disabled)')] : [];
    const k = items.indexOf(item);
    const step = (d) => { const n = items[(k + d + items.length) % items.length]; n?.focus(); };
    const stop = () => { e.preventDefault(); e.stopPropagation(); };
    const openSubSeat = () => {
      dropdowns[i]._openSubAt(item, item._sub);
      item._sub.querySelector(':scope > .mb-item:not(.disabled)')?.focus();
    };
    const foldSub = () => {
      const parent = lvl.parentElement;
      subClosers[i]?.();
      parent?.focus();
    };
    switch (e.key) {
      case 'ArrowDown': stop(); step(1); break;
      case 'ArrowUp': stop(); step(-1); break;
      case 'Home': stop(); items[0]?.focus(); break;
      case 'End': stop(); items[items.length - 1]?.focus(); break;
      case 'ArrowRight':
        stop();
        // a has-sub seat opens its cascade; a plain seat strides the bar
        // onto the next menu — the desktop menubar’s own road. Inside a
        // cascade ArrowRight stays the sub’s own: no deeper level, and
        // no fall-through to the document chain either
        if (inSub) break;
        if (item._sub) openSubSeat();
        else open((i + 1) % titles.length);
        break;
      case 'ArrowLeft':
        stop();
        if (inSub) foldSub();
        else open((i - 1 + titles.length) % titles.length);
        break;
      case 'Enter': case ' ': case 'Spacebar': {
        stop();
        if (item._sub && !inSub && item._sub.classList.contains('hidden')) { openSubSeat(); break; }
        // the picked verb restores the seat to the title BEFORE it runs,
        // so a modal the verb opens captures the bar as its previous
        // focus (the ctxmenu law)
        titles[i].focus();
        item.click();
        break;
      }
      case 'Escape':
        stop();
        if (inSub) foldSub();
        else { close(); titles[i].focus(); }
        break;
      default:
        // a menu that owns the seat owns its keys — a stray letter never
        // falls through to the listing’s type-ahead below
        if (e.key.length === 1) stop();
    }
  };

  defs.forEach((def, i) => {
    const dropdown = el('div', { class: 'mb-dd hidden', role: 'menu' });
    dropdown.addEventListener('keydown', (e) => ddKeydown(e, i));
    dropdowns.push(dropdown);
    const title = el('div', {
      class: 'mb-title',
      text: def.label,
      role: 'menuitem',
      'aria-haspopup': 'menu',
      'aria-expanded': 'false',
      onclick: () => { if (openIdx === i) close(); else open(i); },
    });
    // the title’s own keys: open on Enter / Space / ArrowDown (ArrowUp
    // opens at the bottom), stride the closed bar under the arrows, and
    // every handled key is stopped here so the listing walk on the
    // document chain never answers for the bar
    title.addEventListener('keydown', (e) => {
      if (e.altKey || e.ctrlKey || e.metaKey) return;
      const n = titles.length;
      const stop = () => { e.preventDefault(); e.stopPropagation(); };
      if (e.key === 'ArrowRight') {
        stop();
        if (openIdx >= 0) open((i + 1) % n);
        else { setStop((i + 1) % n); titles[(i + 1) % n].focus(); }
      } else if (e.key === 'ArrowLeft') {
        stop();
        if (openIdx >= 0) open((i - 1 + n) % n);
        else { setStop((i - 1 + n) % n); titles[(i - 1 + n) % n].focus(); }
      } else if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        stop();
        if (openIdx === i) seatFirst(i);
        else open(i);
      } else if (e.key === 'ArrowUp' && openIdx === i) {
        stop();
        seatLast(i);
      } else if (e.key === 'Escape' && openIdx >= 0) {
        stop();
        const ti = openIdx;
        close();
        titles[ti]?.focus();
      } else if (e.key.length === 1) {
        stop(); // the bar owns its seat’s typing — no surface below answers
      }
    });
    title.addEventListener('focus', () => setStop(i));
    title.addEventListener('mouseenter', () => { if (openIdx >= 0 && openIdx !== i) open(i); });
    titles.push(title);

    const wrap = el('div', { class: 'mb-menu' }, title, dropdown);
    root.appendChild(wrap);
  });
  setStop(0); // the first title carries the bar’s tab stop until walked

  // capture phase — grid checkboxes and V/M badges stop mousedown
  // propagation, which a bubble-phase listener never sees (same fix as
  // the shared context menu's closer in main.js)
  document.addEventListener('mousedown', (e) => {
    if (!root.contains(e.target)) close();
  }, { capture: true });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && openIdx >= 0) close();
  });

  return { root, close };
}
