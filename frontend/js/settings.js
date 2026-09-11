// App Settings (menubar Settings menu + dialog). One surface for every
// persisted shell knob: theme, language, panels/log visibility, auto
// refresh, and the transfer defaults (conflict policy + speed limit).
//
// Persistence stays in the same localStorage keys the boot code has always
// read (no migration); the apply-side actions are injected by main.js so
// this module owns only presentation.
import { el } from './util.js';
import { t, languages, LANG_NAMES } from './i18n.js';
import { openModal } from './dialogs.js';

export const AR_STEPS = [0, 5000, 10000, 30000, 60000];

export const RATE_STEPS = [
  [0, 'settings.rateNone'],
  [524288, '512 kB/s'],
  [1048576, '1 MB/s'],
  [2097152, '2 MB/s'],
  [5242880, '5 MB/s'],
  [10485760, '10 MB/s'],
];

// row helpers -----------------------------------------------------------

function row(labelText, control, hint) {
  return el('label', { class: 'set-row' },
    el('div', { class: 'set-l' },
      el('div', { class: 'set-name', text: labelText }),
      ...(hint ? [el('div', { class: 'set-hint', text: hint })] : [])),
    control);
}

function select(options, value, onchange) {
  const s = el('select', { class: 'input set-ctl', onchange: (e) => onchange(e.target.value) },
    ...options.map(([v, label]) => el('option', { value: String(v), text: label })));
  s.value = String(value);
  return s;
}

function checkbox(checked, onchange) {
  return el('input', { type: 'checkbox', class: 'set-ctl', checked: !!checked, onchange: (e) => onchange(e.target.checked) });
}

// settingsDialog ---------------------------------------------------------

// ctx = {
//   state: { theme, lang, autoRefreshMs, refreshOnFocus, panes, log,
//            conflict, throttle } — thunks read live values,
//   apply: { theme, lang, autoRefresh, refreshOnFocus, panes, log,
//            conflict, throttle } — (value) setters,
// }
export function settingsDialog(ctx) {
  const s = ctx.state;
  const a = ctx.apply;

  const langSel = select(
    [['auto', t('settings.langAuto')], ...languages().map((c) => [c, LANG_NAMES[c] || c])],
    s.lang(),
    (v) => a.lang(v),
  );

  const body = el('div', { class: 'set-body' },
    el('div', { class: 'set-section', text: t('settings.appearance') }),
    row(t('settings.theme'), select(
      [['light', t('settings.themeLight')], ['dark', t('settings.themeDark')]],
      s.theme(),
      (v) => a.theme(v),
    )),
    row(t('settings.language'), langSel, t('settings.langHint')),

    el('div', { class: 'set-section', text: t('settings.view') }),
    row(t('settings.panes'), checkbox(s.panes(), (v) => a.panes(v)), 'F9'),
    row(t('settings.log'), checkbox(s.log(), (v) => a.log(v)), 'Ctrl+L'),

    el('div', { class: 'set-section', text: t('settings.refresh') }),
    row(t('settings.autorefresh'), select(
      AR_STEPS.map((ms) => [ms, ms === 0 ? t('ar.off') : `${ms / 1000} s`]),
      s.autoRefreshMs(),
      (v) => a.autoRefresh(parseInt(v, 10)),
    )),
    row(t('settings.focus'), checkbox(s.refreshOnFocus(), (v) => a.refreshOnFocus(v))),

    el('div', { class: 'set-section', text: t('settings.transfers') }),
    row(t('settings.conflict'), select(
      [['ask', t('settings.ask')], ['overwrite', t('settings.overwrite')], ['skip', t('settings.skip')], ['rename', t('settings.rename')]],
      s.conflict(),
      (v) => a.conflict(v),
    ), t('settings.conflictHint')),
    row(t('settings.throttle'), select(
      RATE_STEPS.map(([v, label]) => [v, t(label)]),
      s.throttle(),
      (v) => a.throttle(parseInt(v, 10)),
    ), t('settings.throttleHint')),
  );

  openModal({
    title: t('settings.title'),
    body,
    wide: true,
    buttons: [{ label: 'Close', class: 'primary' }],
  });
}
