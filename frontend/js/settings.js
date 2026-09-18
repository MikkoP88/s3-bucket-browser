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
// custom location with the native folder dialog, and the three file-log
// filters — multi-select level, scope and source pickers. ctx.log =
// { get, set, browse } is injected by main.js and talks to the backend
// preference (logsettings.json), so the choice survives restarts and
// `s3b log`. The source picker's options come from the backend too
// (allSources): every source seen on a log line plus the configured
// data sources. All three filters gate ONLY what is written to the log
// file; the in-app log drawer keeps its own, independent filters.
// Returns the four rows flat — the Settings page keeps a flat
// section/row structure so search can walk it.
function logFileRows(ctx) {
  let cur = ctx.log.get() || {}; // { mode, dir, levels, scopes, sources, allScopes, allSources }
  const dirOpt = el('option', { value: 'custom' });
  const sel = el('select', { class: 'input set-ctl' });
  const levelsSel = multiSel(t('log.all'), ['info', 'warn', 'error'], cur.levels || []);
  const scopesSel = multiSel(t('log.all'), cur.allScopes || [], cur.scopes || []);
  const sourcesSel = multiSel(t('log.sourceAll'), cur.allSources || [], cur.sources || []);
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
    cur = (await ctx.log.set(mode, dir, [...levelsSel.sel], [...scopesSel.sel], [...sourcesSel.sel])) || {};
    sync();
  };
  const applyFilters = async () => {
    cur = (await ctx.log.set(cur.mode || 'default', cur.dir || '', [...levelsSel.sel], [...scopesSel.sel], [...sourcesSel.sel])) || {};
    sync();
  };
  levelsSel.root.addEventListener('change', applyFilters);
  scopesSel.root.addEventListener('change', applyFilters);
  sourcesSel.root.addEventListener('change', applyFilters);
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
// row. ctx.security = { get, set } is injected by main.js. Returns the
// four rows flat (see logFileRows).
function securityRows(ctx) {
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
  return [
    row(t('settings.secure'), cb, t('settings.secureHint')),
    row(t('settings.secKeyring'), backend),
    row(t('settings.secEditor'), editDir),
    row(t('settings.secSpool'), spoolDir),
  ];
}

// settingsDialog ---------------------------------------------------------
//
// ctx = {
//   state: { theme, lang, autoRefreshMs, refreshOnFocus, panes, log,
//            conflict, throttle } — thunks read live values,
//   apply: { theme, lang, autoRefresh, refreshOnFocus, panes, log,
//            conflict, throttle } — (value) setters,
// }
//
// VS 2026-style layout: nav + search on the left, the active category's
// page on the right. Searching flattens the book — every page un-hides,
// non-matching rows (and emptied section headers) hide, and the nav shows
// per-category match counts. Clearing the search restores the active page.
export function settingsDialog(ctx) {
  const s = ctx.state;
  const a = ctx.apply;

  const langSel = select(
    [['auto', t('settings.langAuto')], ...languages().map((c) => [c, LANG_NAMES[c] || c])],
    s.lang(),
    (v) => a.lang(v),
  );

  // pages, in nav order; every page leads with its own category header
  const pages = [
    { id: 'appearance', label: t('settings.appearance'), build: () => [
      row(t('settings.theme'), select(
        [
          ['auto', t('settings.themeAuto')],
          ['light', t('settings.themeLight')],
          ['dark', t('settings.themeDark')],
        ],
        s.theme(),
        (v) => a.theme(v),
      )),
      row(t('settings.language'), langSel, t('settings.langHint')),
    ] },
    { id: 'view', label: t('settings.view'), build: () => [
      row(t('settings.panes'), checkbox(s.panes(), (v) => a.panes(v)), 'F9'),
      row(t('settings.log'), checkbox(s.log(), (v) => a.log(v)), 'Ctrl+L'),
      row(t('settings.showVersions'), checkbox(s.showVersions?.() || false, (v) => a.showVersions?.(v)), t('settings.showVersionsHint')),
      row(t('settings.showMarkers'), checkbox(s.showMarkers?.() || false, (v) => a.showMarkers?.(v)), t('settings.showMarkersHint')),
      row(t('settings.showHidden'), checkbox(s.showHidden?.() || false, (v) => a.showHidden?.(v)), t('settings.showHiddenHint')),
      row(t('settings.localSync'), checkbox(s.localSync?.() || false, (v) => a.localSync?.(v)), t('settings.localSyncHint')),
      // Native popout windows open centered on the display the app is on
      // (multi-monitor aware); "app" pins them to the app window's center.
      // In-page popouts are bounded by the app window either way.
      row(t('settings.popoutCenter'), select(
        [['display', t('settings.popoutCenterDisplay')], ['app', t('settings.popoutCenterApp')]],
        s.popoutCenter(),
        (v) => a.popoutCenter(v),
      ), t('settings.popoutCenterHint')),
      row(t('settings.popoutPersist'), checkbox(s.popoutPersist?.() ?? true, (v) => a.popoutPersist?.(v)), t('settings.popoutPersistHint')),
      ...colSection('settings.colsMain', s.cols?.() || [], (v) => a.cols?.(v)),
      ...colSection('settings.colsSide', s.colsLocal?.() || [], (v) => a.colsLocal?.(v)),
    ] },
    { id: 'refresh', label: t('settings.refresh'), build: () => [
      row(t('settings.autorefresh'), select(
        AR_STEPS.map((ms) => [ms, ms === 0 ? t('ar.off') : `${ms / 1000} s`]),
        s.autoRefreshMs(),
        (v) => a.autoRefresh(parseInt(v, 10)),
      )),
      row(t('settings.focus'), checkbox(s.refreshOnFocus(), (v) => a.refreshOnFocus(v))),
    ] },
    { id: 'editing', label: t('settings.editing'), build: () => [
      row(t('settings.editChooseApp'), checkbox(s.editChooseApp?.() ?? true, (v) => a.editChooseApp?.(v)), t('settings.editChooseAppHint')),
    ] },
    { id: 'deleting', label: t('settings.delSection'), build: () => [
      row(t('settings.delWindow'), checkbox(s.delWindow?.() ?? true, (v) => a.delWindow?.(v)), t('settings.delWindowHint')),
      row(t('settings.delTypeConfirm'), checkbox(s.delTypeConfirm?.() || false, (v) => a.delTypeConfirm?.(v)), t('settings.delTypeConfirmHint')),
      row(t('settings.delAutoConfirm'), checkbox(s.delAutoConfirm?.() || false, (v) => a.delAutoConfirm?.(v)), t('settings.delAutoConfirmHint')),
    ] },
    { id: 'logging', label: t('settings.logging'), build: () => logFileRows(ctx) },
    { id: 'security', label: t('settings.secSection'), build: () => securityRows(ctx) },
    { id: 'transfers', label: t('settings.transfers'), build: () => [
      row(t('settings.conflict'), select(
        [['ask', t('settings.ask')], ['overwrite', t('settings.overwrite')], ['skip', t('settings.skip')], ['rename', t('settings.rename')]],
        s.conflict(),
        (v) => a.conflict(v),
      ), t('settings.conflictHint')),
      row(t('settings.showThrottle'), checkbox(s.showThrottle?.() || false, (v) => a.showThrottle?.(v))),
      row(t('settings.copyVersions'), checkbox(s.copyVersions?.() ?? true, (v) => a.copyVersions?.(v)), t('settings.copyVersionsHint')),
      row(t('settings.explorerClip'), checkbox(s.explorerClip?.() ?? true, (v) => a.explorerClip?.(v)), t('settings.explorerClipHint')),
      // The transfers window opens itself when a transfer starts and
      // closes itself on a clean, all-done batch — hand-opened windows
      // never close on their own.
      row(t('settings.xferWin'), checkbox(s.xferWin?.() ?? true, (v) => a.xferWin?.(v)), t('settings.xferWinHint')),
      row(t('settings.throttle'), select(
        RATE_STEPS.map(([v, label]) => [v, t(label)]),
        s.throttle(),
        (v) => a.throttle(parseInt(v, 10)),
      ), t('settings.throttleHint')),
    ] },
  ];

  // nav + search (left) --------------------------------------------------
  const search = el('input', { type: 'search', class: 'input set-search',
    placeholder: t('settings.search'), 'aria-label': t('settings.search') });
  const navItems = pages.map((p) => {
    const badge = el('span', { class: 'set-nav-badge', hidden: '' });
    const btn = el('button', { type: 'button', class: 'set-nav-item' },
      el('span', { class: 'set-nav-label', text: p.label }),
      badge);
    btn.addEventListener('click', () => selectPage(p));
    return { page: p, btn, badge };
  });
  const nav = el('nav', { class: 'set-nav', 'aria-label': t('settings.title') },
    ...navItems.map((n) => n.btn));

  // pages (right) ----------------------------------------------------------
  // Each page's children are only .set-section headers and .set-row rows
  // (logFileRows/securityRows/colSection all return flat lists), which is
  // what the search walk relies on.
  const pageEls = pages.map((p) => el('div', { class: 'set-page', 'data-cat': p.id },
    el('div', { class: 'set-section', text: p.label }),
    ...p.build()));
  // every row indexes its own searchable text: label, description, and the
  // section + category it lives under (searching "columns" finds the
  // column groups; searching a category name finds its whole page)
  for (const [pi, pageEl] of pageEls.entries()) {
    let section = '';
    for (const child of pageEl.children) {
      if (child.classList.contains('set-section')) section = child.textContent;
      else child._q = `${child.textContent} ${section} ${pages[pi].label}`.toLowerCase();
    }
  }
  const noMatch = el('div', { class: 'set-search-none', hidden: '' });

  const main = el('div', { class: 'set-main' }, ...pageEls, noMatch);

  // search + navigation -----------------------------------------------------
  let active = pages[0];

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
      pageEl.hidden = q ? !pageVisible : pageEls[i] !== pageEls[pages.indexOf(active)];
      n.badge.textContent = String(count);
      n.badge.hidden = !q || count === 0;
      n.btn.classList.toggle('miss', !!q && count === 0);
      total += count;
    }
    noMatch.hidden = !q || total > 0;
    if (q) noMatch.textContent = t('settings.searchNone', { q: raw.trim() });
  };

  function selectPage(p) {
    active = p;
    navItems.forEach((n) => n.btn.classList.toggle('active', n.page === p));
    if (search.value) { search.value = ''; }
    filter('');
  }
  search.addEventListener('input', () => filter(search.value));

  // boot state: first page active
  navItems[0].btn.classList.add('active');
  filter('');

  const body = el('div', { class: 'set-shell' },
    el('div', { class: 'set-side' }, search, nav),
    main,
  );

  openModal({
    title: t('settings.title'),
    body,
    cls: 'settings-modal',
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
  search.focus();
}
