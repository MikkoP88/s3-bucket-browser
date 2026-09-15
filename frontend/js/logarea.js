// Optional bottom log drawer: renders structured `log:line` events emitted
// by the backend (pkg/api log.go). Standalone component — imports only DOM
// helpers and the i18n t() so it can be mounted from main.js.
//
// Filtering happens in JS against the line buffer, so every filter
// (level, scope, source, free text) also applies retroactively to lines
// that arrived before it was set. Scope and source options grow as new
// values appear in the stream. Each dimension is a multi-select: any
// combination of levels/scopes/sources can be shown at once.
import { el, multiSel } from './util.js';
import { t } from './i18n.js';

// Buffer cap: the drawer keeps the newest MAX_LINES entries; older ones are
// dropped from both the model and the DOM so long sessions stay flat.
const MAX_LINES = 2000;
const LEVELS = ['info', 'warn', 'error'];

const fmtTime = (iso) => {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (x) => String(x).padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};

// pass: an empty selection disables the dimension (everything passes).
const pass = (sel, v) => sel.size === 0 || sel.has(v || '');

export function createLogArea() {
  const body = el('div', { class: 'la-body' });
  const lines = [];
  let autoscroll = true;

  const f = { text: '' };

  const matches = (l) => {
    if (!pass(levelSel.sel, l.level)) return false;
    if (!pass(scopeSel.sel, l.scope)) return false;
    if (!pass(sourceSel.sel, l.source)) return false;
    if (f.text && !String(l.message || '').toLowerCase().includes(f.text)
      && !String(l.scope || '').toLowerCase().includes(f.text)) return false;
    return true;
  };

  const rowOf = (l) => el('div', { class: `la-line ${l.level}` },
    el('span', { class: 'la-time mono', text: fmtTime(l.time) }),
    el('span', { class: `la-lv ${l.level}`, text: l.level }),
    el('span', { class: 'la-scope', text: l.scope || '' }),
    l.source ? el('span', { class: 'la-src', text: l.source }) : null,
    el('span', { class: 'la-msg', text: l.message || '' }),
  );

  function render() {
    body.replaceChildren(...lines.filter(matches).map(rowOf));
    if (autoscroll) body.scrollTop = body.scrollHeight;
  }

  const levelSel = multiSel(t('log.all'), LEVELS, [], render);
  const scopeSel = multiSel(t('log.scopeAll'), [], [], render);
  const sourceSel = multiSel(t('log.sourceAll'), [], [], render);
  const search = el('input', {
    class: 'input la-search',
    type: 'search',
    placeholder: t('log.search'),
    oninput: () => { f.text = search.value.trim().toLowerCase(); render(); },
  });

  const copy = async () => {
    const rows = lines.filter(matches);
    if (!rows.length) return;
    const text = rows
      .map((l) => `${fmtTime(l.time)} ${String(l.level).toUpperCase()} [${l.scope || ''}]${l.source ? ` (${l.source})` : ''} ${l.message}`)
      .join('\n');
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // Clipboard API unavailable (permissions/older WebView2): fallback.
      const ta = el('textarea', { class: 'hidden' });
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      ta.remove();
    }
  };

  const auto = el('input', {
    type: 'checkbox',
    checked: true,
    onchange: () => { autoscroll = auto.checked; },
  });

  const root = el('section', { class: 'logarea-inner', role: 'log', 'aria-label': t('log.title') },
    el('div', { class: 'la-head' },
      el('span', { class: 'la-title', text: t('log.title') }),
      levelSel.root,
      scopeSel.root,
      sourceSel.root,
      search,
      el('label', { class: 'la-auto' }, auto, ` ${t('log.autoscroll')}`),
      el('button', { class: 'btn', text: t('log.copy'), onclick: copy }),
      el('button', {
        class: 'btn',
        text: t('log.clear'),
        onclick: () => { lines.length = 0; body.replaceChildren(); },
      }),
    ),
    body,
  );

  function append(l) {
    if (!l) return;
    const line = LEVELS.includes(l.level) ? l : { ...l, level: 'info' };
    lines.push(line);
    scopeSel.add(line.scope);
    sourceSel.add(line.source);
    let trimmed = false;
    if (lines.length > MAX_LINES) { lines.shift(); trimmed = true; }
    if (trimmed) { render(); return; } // re-sync: the dropped line may be anywhere
    if (matches(line)) {
      body.appendChild(rowOf(line));
      if (autoscroll) body.scrollTop = body.scrollHeight;
    }
  }

  return { root, append };
}
