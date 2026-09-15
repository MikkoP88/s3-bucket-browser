// App Settings (menubar Settings menu + dialog). One surface for every
// persisted shell knob: theme, language, panels/log visibility, auto
// refresh, and the transfer defaults (conflict policy + speed limit).
//
// Persistence stays in the same localStorage keys the boot code has always
// read (no migration); the apply-side actions are injected by main.js so
// this module owns only presentation.
import { el, multiSel } from './util.js';
import { t, languages, LANG_NAMES } from './i18n.js';
import { openModal, confirm } from './dialogs.js';

const AR_STEPS = [0, 5000, 10000, 30000, 60000];

const RATE_STEPS = [
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

// logFileRow builds the save-logs-to-file control: a select (off / app
// settings folder / custom folder) plus a Browse button that picks the
// custom location with the native folder dialog, and the two file-log
// filters — multi-select level and scope pickers. ctx.log = { get, set,
// browse } is injected by main.js and talks to the backend preference
// (logsettings.json), so the choice survives restarts and `s3b log`.
// The filters gate ONLY what is written to the log file; the in-app log
// drawer keeps its own, independent filters.
function logFileRow(ctx) {
  let cur = ctx.log.get() || {}; // { mode, dir, levels, scopes, allScopes }
  const dirOpt = el('option', { value: 'custom' });
  const sel = el('select', { class: 'input set-ctl' });
  const levelsSel = multiSel(t('log.all'), ['info', 'warn', 'error'], cur.levels || []);
  const scopesSel = multiSel(t('log.all'), cur.allScopes || [], cur.scopes || []);
  const sync = () => {
    dirOpt.textContent = cur.mode === 'custom' && cur.dir
      ? cur.dir
      : t('settings.logCustom');
    sel.replaceChildren(
      el('option', { value: 'off', text: t('settings.logOff') }),
      el('option', { value: 'default', text: t('settings.logDefault') }),
      dirOpt,
    );
    sel.value = cur.mode;
  };
  sync();
  const apply = async (mode, dir) => {
    cur = (await ctx.log.set(mode, dir, [...levelsSel.sel], [...scopesSel.sel])) || {};
    sync();
  };
  const applyFilters = async () => {
    cur = (await ctx.log.set(cur.mode || 'default', cur.dir || '', [...levelsSel.sel], [...scopesSel.sel])) || {};
    sync();
  };
  levelsSel.root.addEventListener('change', applyFilters);
  scopesSel.root.addEventListener('change', applyFilters);
  const browse = async () => {
    const dir = await ctx.log.browse();
    if (dir) await apply('custom', dir);
  };
  sel.addEventListener('change', async () => {
    if (sel.value === 'custom') {
      if (!cur.dir) {
        const dir = await ctx.log.browse();
        if (!dir) { sync(); return; } // canceled — revert to the previous mode
        await apply('custom', dir);
        return;
      }
      await apply('custom', cur.dir); // reuse the remembered folder
      return;
    }
    await apply(sel.value, '');
  });
  const btn = el('button', { class: 'btn set-ctl', text: t('settings.browse'), onclick: browse });
  return el('div', {},
    row(t('settings.logFile'), el('span', { class: 'set-ctl-group' }, sel, btn), t('settings.logHint')),
    row(t('settings.logLevels'), levelsSel.root, t('settings.logFilterHint')),
    row(t('settings.logScopes'), scopesSel.root),
  );
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

    el('div', { class: 'set-section', text: t('settings.editing') }),
    row(t('settings.editChooseApp'), checkbox(s.editChooseApp?.() ?? true, (v) => a.editChooseApp?.(v)), t('settings.editChooseAppHint')),

    el('div', { class: 'set-section', text: t('settings.refresh') }),
    row(t('settings.autorefresh'), select(
      AR_STEPS.map((ms) => [ms, ms === 0 ? t('ar.off') : `${ms / 1000} s`]),
      s.autoRefreshMs(),
      (v) => a.autoRefresh(parseInt(v, 10)),
    )),
    row(t('settings.focus'), checkbox(s.refreshOnFocus(), (v) => a.refreshOnFocus(v))),

    el('div', { class: 'set-section', text: t('settings.logging') }),
    logFileRow(ctx),

    el('div', { class: 'set-section', text: t('settings.transfers') }),
    row(t('settings.conflict'), select(
      [['ask', t('settings.ask')], ['overwrite', t('settings.overwrite')], ['skip', t('settings.skip')], ['rename', t('settings.rename')]],
      s.conflict(),
      (v) => a.conflict(v),
    ), t('settings.conflictHint')),
    row(t('settings.showThrottle'), checkbox(s.showThrottle?.() || false, (v) => a.showThrottle?.(v))),
    row(t('settings.copyVersions'), checkbox(s.copyVersions?.() ?? true, (v) => a.copyVersions?.(v)), t('settings.copyVersionsHint')),
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
    buttons: [
      {
        // Reset asks for confirmation first — it wipes every persisted
        // shell knob (main.js ctx.reset) and reloads the UI.
        label: t('settings.resetDefaults'),
        onclick: async (close) => {
          close();
          if (await confirm({
            title: t('settings.title'),
            message: t('settings.resetConfirm'),
            okLabel: t('settings.resetDefaults'),
            danger: true,
          })) ctx.reset?.();
        },
      },
      { label: 'Close', class: 'primary' },
    ],
  });
}
