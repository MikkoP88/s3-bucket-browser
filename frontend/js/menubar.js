// Menu bar component (File / Edit / View / Help): horizontal title buttons,
// each opening a dropdown below it. Visual language follows .ctxmenu.
// Standalone — imports only the el() DOM helper, no app state.
import { el } from './util.js';

// defs = [{ label, items: [...] }]
// item  = { label, kbd, action, enabled } — enabled is a plain boolean OR
//         a function (re-evaluated every time the dropdown opens);
//         null renders a separator.
export function createMenubar(defs) {
  const root = el('div', { class: 'menubar' });
  const titles = [];
  const dropdowns = [];
  let openIdx = -1;

  const close = () => {
    if (openIdx < 0) return;
    openIdx = -1;
    dropdowns.forEach((d) => d.classList.add('hidden'));
    titles.forEach((t) => t.classList.remove('open'));
  };

  const open = (i) => {
    close();
    openIdx = i;
    renderItems(i);
    dropdowns[i].classList.remove('hidden');
    titles[i].classList.add('open');
  };

  // renderItems rebuilds the dropdown on every open so enabled functions
  // (commandState flags) are re-read at open time.
  function renderItems(i) {
    dropdowns[i].replaceChildren(...(defs[i].items || []).map((it) => {
      if (!it) return el('div', { class: 'mb-sep' });
      const enabled = typeof it.enabled === 'function' ? !!it.enabled() : it.enabled !== false;
      return el('div', {
        class: `mb-item${enabled ? '' : ' disabled'}`,
        onclick: () => { if (!enabled) return; close(); it.action?.(); },
      },
        el('span', { text: it.label }),
        it.kbd ? el('span', { class: 'mb-kbd', text: it.kbd }) : null,
      );
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
