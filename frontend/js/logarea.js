// Optional bottom log drawer: renders structured `log:line` events emitted
// by the backend (pkg/api log.go). Standalone component — imports only the
// el() DOM helper and the i18n t() so it can be mounted from main.js.
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

  const copy = async () => {
    if (!lines.length) return;
    const text = lines
      .map((l) => `${fmtTime(l.time)} ${String(l.level).toUpperCase()} [${l.scope}] ${l.message}`)
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

  const filter = el('select', {
    class: 'la-filter',
    onchange: () => { body.dataset.filter = filter.value; },
  },
    el('option', { value: 'all', text: t('log.all') }),
    ...LEVELS.map((lv) => el('option', { value: lv, text: lv })),
  );

  const auto = el('input', {
    type: 'checkbox',
    checked: true,
    onchange: () => { autoscroll = auto.checked; },
  });

  const root = el('section', { class: 'logarea-inner', role: 'log', 'aria-label': t('log.title') },
    el('div', { class: 'la-head' },
      el('span', { class: 'la-title', text: t('log.title') }),
      filter,
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
    const level = LEVELS.includes(l.level) ? l.level : 'info';
    lines.push(l);
    body.appendChild(el('div', { class: `la-line ${level}` },
      el('span', { class: 'la-time mono', text: fmtTime(l.time) }),
      el('span', { class: `la-lv ${level}`, text: level }),
      el('span', { class: 'la-scope', text: l.scope || '' }),
      el('span', { class: 'la-msg', text: l.message || '' }),
    ));
    if (lines.length > MAX_LINES) {
      lines.shift();
      body.firstElementChild?.remove();
    }
    if (autoscroll) body.scrollTop = body.scrollHeight;
  }

  return { root, append };
}
