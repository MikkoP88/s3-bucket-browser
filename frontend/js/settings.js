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
import { COLUMNS } from './grid.js';

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

// colSection renders one grid's column-visibility group: a checkbox per
// column of the catalog. The identity "Name" column is always visible —
// shown locked rather than hidden. apply receives the full visible-id list
// on every change.
function colSection(labelKey, cur, apply) {
  const on = new Set(cur);
  return [
    el('div', { class: 'set-section', text: t(labelKey) }),
    ...COLUMNS.map((c) => {
      const locked = c.id === 'name';
      const cb = el('input', { type: 'checkbox', class: 'set-ctl', checked: locked || on.has(c.id), disabled: locked });
      cb.addEventListener('change', () => {
        if (cb.checked) on.add(c.id); else on.delete(c.id);
        apply(COLUMNS.filter((x) => on.has(x.id)).map((x) => x.id));
      });
      return row(t(c.labelKey), cb, locked ? t('settings.colLocked') : '');
    }),
  ];
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

// securitySection builds the Secure Storage group (docs/security.md): the
// global at-rest hardening toggle plus a live status readout. The toggle
// round-trips the backend (pkg/api/secure.go) — enabling encrypts the
// data-source store, moves the temp workspaces into the config dir and
// turns file logging off — and the returned SecureStatus re-syncs every
// row. ctx.security = { get, set } is injected by main.js.
function securitySection(ctx) {
  let cur = ctx.security?.get() || {};
  const backend = el('span', { class: 'set-val' });
  const editDir = el('span', { class: 'set-val' });
  const spoolDir = el('span', { class: 'set-val' });
  const cb = el('input', { type: 'checkbox', class: 'set-ctl' });
  const sync = () => {
    backend.textContent = cur.keyringAvailable
      ? (cur.keyringBackend || '—')
      : t('settings.secKeyringNone');
    editDir.textContent = cur.editorDir || '—';
    spoolDir.textContent = cur.spoolDir || '—';
    cb.checked = !!cur.enabled;
    cb.disabled = !cur.keyringAvailable;
  };
  sync();
  cb.addEventListener('change', async () => {
    const on = cb.checked;
    cb.disabled = true;
    try { cur = (await ctx.security.set(on)) || cur; }
    finally { sync(); } // true on-disk state, whatever happened
  });
  return el('div', {},
    row(t('settings.secure'), cb, t('settings.secureHint')),
    row(t('settings.secKeyring'), backend),
    row(t('settings.secEditor'), editDir),
    row(t('settings.secSpool'), spoolDir),
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
    row(t('settings.showVersions'), checkbox(s.showVersions?.() || false, (v) => a.showVersions?.(v)), t('settings.showVersionsHint')),
    row(t('settings.showMarkers'), checkbox(s.showMarkers?.() || false, (v) => a.showMarkers?.(v)), t('settings.showMarkersHint')),
    row(t('settings.showHidden'), checkbox(s.showHidden?.() || false, (v) => a.showHidden?.(v)), t('settings.showHiddenHint')),

    ...colSection('settings.colsMain', s.cols?.() || [], (v) => a.cols?.(v)),
    ...colSection('settings.colsSide', s.colsLocal?.() || [], (v) => a.colsLocal?.(v)),

    el('div', { class: 'set-section', text: t('settings.delSection') }),
    row(t('settings.delWindow'), checkbox(s.delWindow?.() ?? true, (v) => a.delWindow?.(v)), t('settings.delWindowHint')),
    row(t('settings.delTypeConfirm'), checkbox(s.delTypeConfirm?.() || false, (v) => a.delTypeConfirm?.(v)), t('settings.delTypeConfirmHint')),
    row(t('settings.delAutoConfirm'), checkbox(s.delAutoConfirm?.() || false, (v) => a.delAutoConfirm?.(v)), t('settings.delAutoConfirmHint')),

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

    el('div', { class: 'set-section', text: t('settings.secSection') }),
    securitySection(ctx),

    el('div', { class: 'set-section', text: t('settings.transfers') }),
    row(t('settings.conflict'), select(
      [['ask', t('settings.ask')], ['overwrite', t('settings.overwrite')], ['skip', t('settings.skip')], ['rename', t('settings.rename')]],
      s.conflict(),
      (v) => a.conflict(v),
    ), t('settings.conflictHint')),
    row(t('settings.showThrottle'), checkbox(s.showThrottle?.() || false, (v) => a.showThrottle?.(v))),
    row(t('settings.copyVersions'), checkbox(s.copyVersions?.() ?? true, (v) => a.copyVersions?.(v)), t('settings.copyVersionsHint')),
    row(t('settings.explorerClip'), checkbox(s.explorerClip?.() ?? true, (v) => a.explorerClip?.(v)), t('settings.explorerClipHint')),
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
