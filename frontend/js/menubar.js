// Menu bar component (File / Edit / View / Help): horizontal title buttons,
// each opening a dropdown below it. Visual language follows .ctxmenu.
// Standalone — imports only the el() DOM helper, no app state.
import { el } from './util.js';

// defs = [{ label, items: [...] }]
// item  = { label, kbd, action, enabled, checked } — enabled/checked are
//         plain booleans OR functions (re-evaluated every time the dropdown
//         opens); { label, items: [...] } renders a submenu (one level);
//         null renders a separator.
export function createMenubar(defs) {
  const root = el('div', { class: 'menubar' });
  const titles = [];
  const dropdowns = [];
  const subClosers = []; // per-dropdown "close any open submenu" fn
  let openIdx = -1;

  const close = () => {
    if (openIdx < 0) return;
    openIdx = -1;
    dropdowns.forEach((d) => d.classList.add('hidden'));
    titles.forEach((t) => t.classList.remove('open'));
    subClosers.forEach((c) => c?.());
  };

  const open = (i) => {
    close();
    openIdx = i;
    renderItems(i);
    dropdowns[i].classList.remove('hidden');
    titles[i].classList.add('open');
  };

  // renderLeaf builds one clickable menu item (used at both levels).
  const renderLeaf = (it, closeSub) => {
    const enabled = typeof it.enabled === 'function' ? !!it.enabled() : it.enabled !== false;
    const checked = typeof it.checked === 'function' ? !!it.checked() : !!it.checked;
    return el('div', {
      class: `mb-item${enabled ? '' : ' disabled'}${checked ? ' checked' : ''}`,
      onclick: () => { if (!enabled) return; close(); it.action?.(); },
    },
      el('span', { class: 'mb-check', text: checked ? '\u2713' : '' }),
      el('span', { text: it.label }),
      it.kbd ? el('span', { class: 'mb-kbd', text: it.kbd }) : null,
    );
  };

  // renderItems rebuilds the dropdown on every open so enabled/checked
  // functions (commandState flags) are re-read at open time.
  function renderItems(i) {
    let openSub = null;
    const closeSub = () => { if (openSub) { openSub.classList.add('hidden'); openSub = null; } };
    subClosers[i] = closeSub;

    dropdowns[i].replaceChildren(...(defs[i].items || []).map((it) => {
      if (!it) return el('div', { class: 'mb-sep' });
      if (it.items) { // submenu (one level)
        const sub = el('div', { class: 'mb-dd sub hidden' },
          ...it.items.map((s) => (s ? renderLeaf(s, closeSub) : el('div', { class: 'mb-sep' }))));
        const item = el('div', { class: 'mb-item has-sub' },
          el('span', { class: 'mb-check' }),
          el('span', { text: it.label }),
          el('span', { class: 'mb-arrow', text: '\u25B8' }),
          sub,
        );
        item.addEventListener('mouseenter', () => {
          if (openSub !== sub) { closeSub(); sub.classList.remove('hidden'); openSub = sub; }
        });
        return item;
      }
      const leaf = renderLeaf(it, closeSub);
      leaf.addEventListener('mouseenter', closeSub);
      return leaf;
    }));
  }

  defs.forEach((def, i) => {
    const dropdown = el('div', { class: 'mb-dd hidden' });
    dropdowns.push(dropdown);
    const title = el('div', {
      class: 'mb-title',
      text: def.label,
      onclick: () => { if (openIdx === i) close(); else open(i); },
    });
    title.addEventListener('mouseenter', () => { if (openIdx >= 0 && openIdx !== i) open(i); });
    titles.push(title);

    const wrap = el('div', { class: 'mb-menu' }, title, dropdown);
    root.appendChild(wrap);
  });

  document.addEventListener('mousedown', (e) => {
    if (!root.contains(e.target)) close();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && openIdx >= 0) close();
  });

  return { root, close };
}
