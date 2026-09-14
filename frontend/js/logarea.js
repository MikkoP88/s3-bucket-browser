// Optional bottom log drawer: renders structured `log:line` events emitted
// by the backend (pkg/api log.go). Standalone component — imports only the
// el() DOM helper and the i18n t() so it can be mounted from main.js.
//
// Filtering happens in JS against the line buffer, so every filter
// (level, scope, source, free text) also applies retroactively to lines
// that arrived before it was set. Scope and source options grow as new
// values appear in the stream.
import { el } from './util.js';
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

export function createLogArea() {
  const body = el('div', { class: 'la-body' });
  const lines = [];
  let autoscroll = true;

  // Live filter values; 'all' disables a dimension.
  const f = { level: 'all', scope: 'all', source: 'all', text: '' };

  const matches = (l) => {
    if (f.level !== 'all' && l.level !== f.level) return false;
    if (f.scope !== 'all' && (l.scope || '') !== f.scope) return false;
    if (f.source !== 'all' && (l.source || '') !== f.source) return false;
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

  // addOption grows a select when a value appears for the first time.
  function addOption(sel, value) {
    if (!value || [...sel.options].some((o) => o.value === value)) return;
    sel.appendChild(el('option', { value, text: value }));
  }

  const levelSel = el('select', {
    class: 'la-filter',
    onchange: () => { f.level = levelSel.value; render(); },
  },
    el('option', { value: 'all', text: t('log.all') }),
    ...LEVELS.map((lv) => el('option', { value: lv, text: lv })),
  );
  const scopeSel = el('select', {
    class: 'la-filter',
    onchange: () => { f.scope = scopeSel.value; render(); },
  }, el('option', { value: 'all', text: t('log.scopeAll') }));
  const sourceSel = el('select', {
    class: 'la-filter',
    onchange: () => { f.source = sourceSel.value; render(); },
  }, el('option', { value: 'all', text: t('log.sourceAll') }));
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
      levelSel,
      scopeSel,
      sourceSel,
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
    addOption(scopeSel, line.scope);
    addOption(sourceSel, line.source);
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
