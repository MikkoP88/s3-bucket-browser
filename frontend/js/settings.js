// App Settings (menubar Settings menu + dialog) — a VS 2026-style two-pane
// Settings surface: a left category nav, a search box that filters every
// setting across categories (with per-category match counts), and one page
// per category of label + description + control rows.
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
  [52428800, '50 MB/s'],
  [104857600, '100 MB/s'],
  [262144000, '250 MB/s'],
  [524288000, '500 MB/s'],
  [786432000, '750 MB/s'],
  [1048576000, '1000 MB/s'],
];

// Engine-tuning steps (Settings → Network / Transfers; persisted Go-side
// in appsettings.json). 0 = Default/Auto — the backend resolves it to the
// documented default and clamps anything else into range, so the exact
// numbers only need to cover the useful shelf.
const TUNE_STEPS = {
  listing: [[0, null], [10000, '10 s'], [30000, '30 s'], [60000, '1 min'], [120000, '2 min'], [300000, '5 min']],
  compare: [[0, null], [60000, '1 min'], [300000, '5 min'], [900000, '15 min'], [1800000, '30 min']],
  retries: [[0, null], [1, '1'], [2, '2'], [3, '3'], [5, '5'], [8, '8']],
  partMiB: [[0, null], [5, '5 MiB'], [8, '8 MiB'], [16, '16 MiB'], [32, '32 MiB'], [64, '64 MiB']],
  conc: [[0, null], [1, '1'], [2, '2'], [4, '4'], [5, '5'], [8, '8'], [16, '16']],
  stall: [[0, null], [5000, '5 s'], [10000, '10 s'], [30000, '30 s'], [60000, '1 min']],
};

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

// logFileRows builds the save-logs-to-file control: a select (off / app
// settings folder / custom folder) plus a Browse button that picks the
// custom location with the native folder dialog, and the three file-log
// filters — multi-select level, scope and source pickers. The source
// picker's options come from the backend (allSources: every source seen
// on a log line plus the configured data sources); all three filters
// gate ONLY what is written to the log file, the in-app log drawer keeps
// its own, independent filters. Choices STAGE into the draft
// (draft.logCfg) and reach the backend preference (logsettings.json,
// via ctx.log from main.js) once, on Save. Returns the four rows flat
// — the Settings page keeps a flat section/row structure so search
// can walk it.
function logFileRows(draft, opts, ctx, mark) {
  const cfg = draft.logCfg; // { mode, dir, levels, scopes, sources }
  const dirOpt = el('option', { value: 'custom' });
  const sel = el('select', { class: 'input set-ctl' });
  const levelsSel = multiSel(t('log.all'), ['info', 'warn', 'error'], cfg.levels || []);
  const scopesSel = multiSel(t('log.all'), opts.scopes, cfg.scopes || []);
  const sourcesSel = multiSel(t('log.sourceAll'), opts.sources, cfg.sources || []);
  const sync = () => {
    dirOpt.textContent = cfg.mode === 'custom' && cfg.dir
      ? cfg.dir
      : t('settings.logCustom');
    sel.replaceChildren(
      el('option', { value: 'off', text: t('settings.logOff') }),
      el('option', { value: 'default', text: t('settings.logDefault') }),
      dirOpt,
    );
    sel.value = cfg.mode;
  };
  sync();
  const stageFilters = () => {
    cfg.levels = [...levelsSel.sel];
    cfg.scopes = [...scopesSel.sel];
    cfg.sources = [...sourcesSel.sel];
    mark();
  };
  levelsSel.root.addEventListener('change', stageFilters);
  scopesSel.root.addEventListener('change', stageFilters);
  sourcesSel.root.addEventListener('change', stageFilters);
  // stage records a mode/dir choice into the draft. Custom with no
  // remembered folder picks one first; a canceled pick reverts the select.
  const stage = async (mode) => {
    if (mode === 'custom' && !cfg.dir) {
      const dir = await ctx.log.browse();
      if (!dir) { sync(); return; } // canceled — keep the previous choice
      cfg.dir = dir;
      cfg.mode = 'custom';
      mark();
      sync();
      return;
    }
    if (mode === 'custom') cfg.mode = 'custom'; // reuse the remembered folder
    else { cfg.mode = mode; cfg.dir = ''; }
    mark();
    sync();
  };
  const browse = async () => {
    const dir = await ctx.log.browse();
    if (!dir) return; // canceled — keep the staged choice
    cfg.dir = dir;
    cfg.mode = 'custom';
    mark();
    sync();
  };
  sel.addEventListener('change', () => stage(sel.value));
  const btn = el('button', { class: 'btn set-ctl', text: t('settings.browse'), onclick: browse });
  return [
    row(t('settings.logFile'), el('span', { class: 'set-ctl-group' }, sel, btn), t('settings.logHint')),
    row(t('settings.logLevels'), levelsSel.root, t('settings.logFilterHint')),
    row(t('settings.logScopes'), scopesSel.root),
    row(t('settings.logSources'), sourcesSel.root, t('settings.logFilterHint')),
  ];
}

// securityRows builds the Secure Storage group (docs/security.md): the
// global at-rest hardening toggle plus a live status readout. The toggle
// round-trips the backend (pkg/api/secure.go) — enabling encrypts the
// data-source store, moves the temp workspaces into the config dir and
// turns file logging off — and the returned SecureStatus re-syncs every
// row. This group is deliberately LIVE (the one exception to the draft
// model): enabling secure storage is an operation with immediate,
// on-disk consequences, so it must not wait behind Save. ctx.security =
// { get, set } is injected by main.js; `after` runs once the round-trip
// is done so the sheet can re-sync the log draft (file logging turned
// off). Returns the four rows flat (see logFileRows).
function securityRows(ctx, after) {
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
    finally { // true on-disk state, whatever happened
      sync();
      after?.();
    }
  });
  return [
    row(t('settings.secure'), cb, t('settings.secureHint')),
    row(t('settings.secKeyring'), backend),
    row(t('settings.secEditor'), editDir),
    row(t('settings.secSpool'), spoolDir),
  ];
}

// msLabel renders an off-step millisecond value ('45 s', '90 min') — the
// rare hand-edited appsettings.json can land between the curated steps, and
// the select must still show the real stored value.
function msLabel(ms) {
  return ms % 60000 === 0 ? `${ms / 60000} min` : `${Math.round(ms / 1000)} s`;
}

// tuningRows builds the six engine-tuning selects: the Settings → Network
// page (timeouts + retries) and the Transfer-engine group under Settings →
// File transfers (multipart shape + stall flag). Values STAGE into the
// draft (draft.tuning via getTun/setTun — the same block is shared by
// both pages) and reach the backend preference (appsettings.json, via
// ctx.engine from main.js) as ONE SetTuning with the whole snapshot on
// Save — no per-select round-trip. 0 = Default/Auto: SetTuning
// resolves it to the documented default and clamps everything else. Rows
// are rebuilt from the draft on every render, so Reset restages them
// like any other control.
function tuningRows(getTun, setTun) {
  const mk = (field, steps, autoKey, fmt) => {
    const tun = getTun();
    const val = tun[field] || 0;
    const opts = steps.map(([v, label]) => [v, label ?? t(autoKey)]);
    if (val && !steps.some(([v]) => v === val)) opts.push([val, fmt(val)]);
    return select(opts, val, (v) => setTun({ ...getTun(), [field]: parseInt(v, 10) }));
  };
  return {
    networkRows: () => [
      row(t('settings.listTimeout'), mk('listingTimeoutMs', TUNE_STEPS.listing, 'settings.defOpt', msLabel), t('settings.listTimeoutHint')),
      row(t('settings.compareTimeout'), mk('compareTimeoutMs', TUNE_STEPS.compare, 'settings.defOpt', msLabel), t('settings.compareTimeoutHint')),
      row(t('settings.retryAttempts'), mk('retryAttempts', TUNE_STEPS.retries, 'settings.defOpt', String), t('settings.retryAttemptsHint')),
    ],
    engineRows: () => [
      row(t('settings.partSize'), mk('partSizeMiB', TUNE_STEPS.partMiB, 'settings.auto', (v) => `${v} MiB`), t('settings.partSizeHint')),
      row(t('settings.partsInFlight'), mk('partConcurrency', TUNE_STEPS.conc, 'settings.auto', String), t('settings.partsInFlightHint')),
      row(t('settings.stallAfter'), mk('stallAfterMs', TUNE_STEPS.stall, 'settings.defOpt', msLabel), t('settings.stallAfterHint')),
    ],
  };
}

// settingsDialog ---------------------------------------------------------
//
// ctx = {
//   state:    { theme, lang, autoRefreshMs, refreshOnFocus, panes, log,
//               conflict, throttle, ... } — thunks reading the live
//               values, snapshotted ONCE into the draft at open,
//   apply:    { <same keys> } — (value) setters, called for the
//               changed keys on Save (the same setters the menus use),
//   log:      { get, set, browse } — backend file-log preference,
//   security: { get, set } — live secure-storage toggle (never deferred),
//   engine:   { get, set } — backend engine tuning (one SetTuning on Save),
//   defaults: staged-reset target, mirroring the boot fallbacks.
// }
//
// Nothing applies until Save: controls edit `draft`, Save applies the
// diff and closes, Reset stages the defaults into the draft (applied on
// Save), and a dirty draft gates Escape/X/backdrop behind a confirm.
//
// VS 2026-style layout: nav + search on the left, the active category's
// page on the right. Searching flattens the book — every page un-hides,
// non-matching rows (and emptied section headers) hide, and the nav shows
// per-category match counts. Clearing the search restores the active page.
export function settingsDialog(ctx) {
  const s = ctx.state;
  const a = ctx.apply;

  // ---- draft model ------------------------------------------------------
  const lg0 = ctx.log?.get() || {};
  const logOpts = { scopes: lg0.allScopes || [], sources: lg0.allSources || [] };
  const logCfgOf = (lg) => ({
    mode: lg?.mode || 'off',
    dir: lg?.dir || '',
    levels: [...(lg?.levels || [])],
    scopes: [...(lg?.scopes || [])],
    sources: [...(lg?.sources || [])],
  });
  // base = what the dialog opened against; draft = what Save would apply.
  // Key order is fixed by snap(), so a plain stringify compare is a deep
  // compare (the arrays here are deterministic: catalogs and option lists).
  const snap = () => ({
    theme: s.theme(),
    lang: s.lang(),
    autoRefreshMs: s.autoRefreshMs(),
    refreshOnFocus: !!s.refreshOnFocus(),
    panes: !!s.panes(),
    log: !!s.log(),
    conflict: s.conflict(),
    throttle: parseInt(s.throttle(), 10) || 0,
    showThrottle: !!(s.showThrottle?.()),
    editChooseApp: s.editChooseApp?.() ?? true,
    copyVersions: s.copyVersions?.() ?? true,
    cols: [...(s.cols?.() || [])],
    colsLocal: [...(s.colsLocal?.() || [])],
    showHidden: !!(s.showHidden?.()),
    showMarkers: !!(s.showMarkers?.()),
    showVersions: !!(s.showVersions?.()),
    delWindow: s.delWindow?.() ?? true,
    delTypeConfirm: !!(s.delTypeConfirm?.()),
    delAutoConfirm: !!(s.delAutoConfirm?.()),
    explorerClip: s.explorerClip?.() ?? true,
    xferWin: s.xferWin?.() ?? true,
    popoutCenter: s.popoutCenter(),
    popoutPersist: s.popoutPersist?.() ?? true,
    localSync: !!(s.localSync?.()),
    tuning: { ...(ctx.engine?.get() || {}) },
    logCfg: logCfgOf(ctx.log?.get()),
  });
  let base = snap();
  let draft = structuredClone(base);
  const same = () => JSON.stringify(draft) === JSON.stringify(base);
  let saveBtn = null;
  const syncSave = () => { if (saveBtn) saveBtn.disabled = same(); };
  const set = (key, value) => { draft[key] = value; syncSave(); };

  // ---- shell: nav + search (left) ----------------------------------------
  const search = el('input', { type: 'search', class: 'input set-search',
    placeholder: t('settings.search'), 'aria-label': t('settings.search') });
  const PAGES = [
    { id: 'appearance', label: t('settings.appearance') },
    { id: 'view', label: t('settings.view') },
    { id: 'refresh', label: t('settings.refresh') },
    { id: 'network', label: t('settings.network') },
    { id: 'editing', label: t('settings.editing') },
    { id: 'deleting', label: t('settings.delSection') },
    { id: 'logging', label: t('settings.logging') },
    { id: 'security', label: t('settings.secSection') },
    { id: 'transfers', label: t('settings.transfers') },
  ];
  let activeId = PAGES[0].id;
  const navItems = PAGES.map((p) => {
    const badge = el('span', { class: 'set-nav-badge', hidden: '' });
    const btn = el('button', { type: 'button', class: 'set-nav-item' },
      el('span', { class: 'set-nav-label', text: p.label }),
      badge);
    btn.addEventListener('click', () => selectPage(p.id));
    return { btn, badge };
  });
  const nav = el('nav', { class: 'set-nav', 'aria-label': t('settings.title') },
    ...navItems.map((n) => n.btn));

  // ---- pages (right) ------------------------------------------------------
  // Each page's children are only .set-section headers and .set-row rows
  // (logFileRows/securityRows/colSection all return flat lists), which is
  // what the search walk relies on.
  let pageEls = [];
  const noMatch = el('div', { class: 'set-search-none', hidden: '' });
  const main = el('div', { class: 'set-main' }, noMatch);

  // filter is the single visibility pass: with a query it flattens every
  // page into a results list (hiding non-matching rows and emptied
  // headers, counting matches per category for the nav badges); without
  // one it shows just the active page.
  const filter = (raw) => {
    const q = (raw || '').trim().toLowerCase();
    let total = 0;
    for (const [i, n] of navItems.entries()) {
      const pageEl = pageEls[i];
      let count = 0;
      let pageVisible = false;
      let sectionEl = null;
      let sectionVisible = false;
      const flush = () => { if (sectionEl) sectionEl.hidden = !sectionVisible; };
      for (const child of pageEl.children) {
        if (child.classList.contains('set-section')) {
          flush();
          sectionEl = child;
          sectionVisible = false;
        } else if (child._q !== undefined) {
          const hit = !q || child._q.includes(q);
          child.hidden = !hit;
          if (hit) { sectionVisible = true; pageVisible = true; count++; }
        }
      }
      flush();
      pageEl.hidden = q ? !pageVisible : PAGES[i].id !== activeId;
      n.badge.textContent = String(count);
      n.badge.hidden = !q || count === 0;
      n.btn.classList.toggle('miss', !!q && count === 0);
      total += count;
    }
    noMatch.hidden = !q || total > 0;
    if (q) noMatch.textContent = t('settings.searchNone', { q: raw.trim() });
  };

  function selectPage(id) {
    activeId = id;
    navItems.forEach((n, i) => n.btn.classList.toggle('active', PAGES[i].id === id));
    if (search.value) { search.value = ''; }
    filter('');
  }
  search.addEventListener('input', () => filter(search.value));

  // Security is live: after the round-trip the file-log truth may have
  // changed (enabling turns file logging off), so fold the fresh settings
  // into base AND draft — the flip must never read as dirty — and
  // rebuild the book from the merged state.
  const resyncLog = () => {
    const lg = ctx.log?.get() || {};
    logOpts.scopes = lg.allScopes || logOpts.scopes;
    logOpts.sources = lg.allSources || logOpts.sources;
    const fresh = logCfgOf(lg);
    base.logCfg = structuredClone(fresh);
    draft.logCfg = structuredClone(fresh);
    render();
    syncSave();
  };

  // render (re)builds every page from the current draft: once at open,
  // again after Reset (defaults staged) and after a security flip.
  // Security stays live; everything below reads the draft.
  const render = () => {
    const d = draft;
    const tuning = tuningRows(() => d.tuning, (next) => { d.tuning = next; syncSave(); });
    const build = {
      appearance: () => [
        row(t('settings.theme'), select(
          [
            ['auto', t('settings.themeAuto')],
            ['light', t('settings.themeLight')],
            ['dark', t('settings.themeDark')],
          ],
          d.theme,
          (v) => set('theme', v),
        )),
        row(t('settings.language'), select(
          [['auto', t('settings.langAuto')], ...languages().map((c) => [c, LANG_NAMES[c] || c])],
          d.lang,
          (v) => set('lang', v),
        ), t('settings.langHint')),
      ],
      view: () => [
        row(t('settings.panes'), checkbox(d.panes, (v) => set('panes', v)), 'F9'),
        row(t('settings.log'), checkbox(d.log, (v) => set('log', v)), 'Ctrl+L'),
        row(t('settings.showVersions'), checkbox(d.showVersions, (v) => set('showVersions', v)), t('settings.showVersionsHint')),
        row(t('settings.showMarkers'), checkbox(d.showMarkers, (v) => set('showMarkers', v)), t('settings.showMarkersHint')),
        row(t('settings.showHidden'), checkbox(d.showHidden, (v) => set('showHidden', v)), t('settings.showHiddenHint')),
        row(t('settings.localSync'), checkbox(d.localSync, (v) => set('localSync', v)), t('settings.localSyncHint')),
        // Native popout windows open centered on the display the app is on
        // (multi-monitor aware); "app" pins them to the app window's center.
        // In-page popouts are bounded by the app window either way.
        row(t('settings.popoutCenter'), select(
          [['display', t('settings.popoutCenterDisplay')], ['app', t('settings.popoutCenterApp')]],
          d.popoutCenter,
          (v) => set('popoutCenter', v),
        ), t('settings.popoutCenterHint')),
        row(t('settings.popoutPersist'), checkbox(d.popoutPersist, (v) => set('popoutPersist', v)), t('settings.popoutPersistHint')),
        ...colSection('settings.colsMain', d.cols, (v) => set('cols', v)),
        ...colSection('settings.colsSide', d.colsLocal, (v) => set('colsLocal', v)),
      ],
      refresh: () => [
        row(t('settings.autorefresh'), select(
          AR_STEPS.map((ms) => [ms, ms === 0 ? t('ar.off') : `${ms / 1000} s`]),
          d.autoRefreshMs,
          (v) => set('autoRefreshMs', parseInt(v, 10)),
        )),
        row(t('settings.focus'), checkbox(d.refreshOnFocus, (v) => set('refreshOnFocus', v))),
      ],
      // Network: the engine's time and retry budgets — every row is honored
      // Go-side (listing watchdog, quick-op contexts, compare walk, SDK
      // retryer) and persisted in appsettings.json, not localStorage.
      network: () => tuning.networkRows(),
      editing: () => [
        row(t('settings.editChooseApp'), checkbox(d.editChooseApp, (v) => set('editChooseApp', v)), t('settings.editChooseAppHint')),
      ],
      deleting: () => [
        row(t('settings.delWindow'), checkbox(d.delWindow, (v) => set('delWindow', v)), t('settings.delWindowHint')),
        row(t('settings.delTypeConfirm'), checkbox(d.delTypeConfirm, (v) => set('delTypeConfirm', v)), t('settings.delTypeConfirmHint')),
        row(t('settings.delAutoConfirm'), checkbox(d.delAutoConfirm, (v) => set('delAutoConfirm', v)), t('settings.delAutoConfirmHint')),
      ],
      logging: () => logFileRows(draft, logOpts, ctx, syncSave),
      security: () => securityRows(ctx, resyncLog),
      transfers: () => [
        row(t('settings.conflict'), select(
          [['ask', t('settings.ask')], ['overwrite', t('settings.overwrite')], ['skip', t('settings.skip')], ['rename', t('settings.rename')]],
          d.conflict,
          (v) => set('conflict', v),
        ), t('settings.conflictHint')),
        row(t('settings.showThrottle'), checkbox(d.showThrottle, (v) => set('showThrottle', v))),
        row(t('settings.copyVersions'), checkbox(d.copyVersions, (v) => set('copyVersions', v)), t('settings.copyVersionsHint')),
        row(t('settings.explorerClip'), checkbox(d.explorerClip, (v) => set('explorerClip', v)), t('settings.explorerClipHint')),
        // The transfers window opens itself when a transfer starts and
        // closes itself on a clean, all-done batch — hand-opened windows
        // never close on their own.
        row(t('settings.xferWin'), checkbox(d.xferWin, (v) => set('xferWin', v)), t('settings.xferWinHint')),
        row(t('settings.throttle'), select(
          RATE_STEPS.map(([v, label]) => [v, t(label)]),
          d.throttle,
          (v) => set('throttle', parseInt(v, 10)),
        ), t('settings.throttleHint')),
        // Transfer engine: multipart shape and the Stalled flag threshold —
        // honored by every transfer construction site (S3 up/down, editor,
        // cross-engine) and captured per job at start.
        el('div', { class: 'set-section', text: t('settings.engineSection') }),
        ...tuning.engineRows(),
      ],
    };
    pageEls = PAGES.map((p) => el('div', { class: 'set-page', 'data-cat': p.id },
      el('div', { class: 'set-section', text: p.label }),
      ...build[p.id]()));
    // every row indexes its own searchable text: label, description, and the
    // section + category it lives under (searching "columns" finds the
    // column groups; searching a category name finds its whole page)
    for (const [pi, pageEl] of pageEls.entries()) {
      let section = '';
      for (const child of pageEl.children) {
        if (child.classList.contains('set-section')) section = child.textContent;
        else child._q = `${child.textContent} ${section} ${PAGES[pi].label}`.toLowerCase();
      }
    }
    main.replaceChildren(...pageEls, noMatch);
    filter(search.value);
  };

  // boot state: first page active
  navItems[0].btn.classList.add('active');
  render();

  const body = el('div', { class: 'set-shell' },
    el('div', { class: 'set-side' }, search, nav),
    main,
  );

  // Save applies the staged diff: one pass over the changed localStorage
  // knobs, then at most one backend batch per store (tuning, file log),
  // then close. Backend failures toast from main.js; the localStorage
  // parts still hold. Language goes LAST: setLanguage reloads the window
  // (strings render once at construction), so everything else must be on
  // disk before it fires.
  const save = async (close) => {
    const d = draft;
    const changed = Object.keys(d).filter((k) => JSON.stringify(d[k]) !== JSON.stringify(base[k]));
    if (!changed.length) { close(); return; }
    if (saveBtn) saveBtn.disabled = true;
    // clean first: Escape/backdrop during the backend awaits must not
    // prompt, and the guard must not see the applied state as dirty
    base = structuredClone(d);
    try {
      for (const k of changed) {
        if (k === 'lang' || k === 'tuning' || k === 'logCfg') continue;
        a[k]?.(d[k]);
      }
      if (changed.includes('tuning')) await ctx.engine?.set(d.tuning);
      if (changed.includes('logCfg')) {
        await ctx.log?.set(d.logCfg.mode || 'off', d.logCfg.dir || '',
          [...(d.logCfg.levels || [])], [...(d.logCfg.scopes || [])], [...(d.logCfg.sources || [])]);
      }
    } finally { close(); }
    if (changed.includes('lang')) a.lang?.(d.lang); // the reload closes the rest
  };

  const m = openModal({
    title: t('settings.title'),
    body,
    cls: 'settings-modal',
    // A dirty draft gates every dismiss path: confirm discards (and
    // restores base), cancel keeps the sheet open with the draft intact.
    onBeforeClose: async () => {
      if (same()) return true;
      if (!await confirm({
        title: t('settings.unsavedTitle'),
        message: t('settings.unsavedMsg'),
        okLabel: t('settings.discard'),
      })) return false;
      draft = structuredClone(base);
      return true;
    },
    buttons: [
      {
        // Stages the defaults into the draft — nothing is applied and
        // nothing is lost until Save (or a discard) says so.
        label: t('settings.resetDefaults'),
        onclick: () => {
          const def = ctx.defaults || {};
          const d = structuredClone(base); // keeps the draft's key order
          for (const k of Object.keys(d)) if (k in def) d[k] = structuredClone(def[k]);
          draft = d;
          render();
          syncSave();
        },
      },
      { label: t('settings.save'), class: 'primary', disabled: true, onclick: save },
    ],
  });
  saveBtn = m.btns[m.btns.length - 1];
  syncSave();
  search.focus();
}
