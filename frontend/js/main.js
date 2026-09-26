// S3 Bucket Browser — application shell (Explorer layout).⁠​‌‌‌​​‌‌​​‌‌​​‌‌​‌‌​​​‌​​​‌​‌‌​‌​‌‌‌​​​​​‌‌‌​​‌​​‌‌​‌‌‌‌​‌‌‌​‌‌​​‌‌​​‌​‌​‌‌​‌‌‌​​‌‌​​​​‌​‌‌​‌‌‌​​‌‌​​​‌‌​‌‌​​‌​‌​​‌​‌‌​‌​‌‌‌​‌‌​​​‌‌​​​‌​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌​​​​‌‌​‌‌​‌‌‌‌​‌‌‌​​​​​‌‌‌‌​​‌​‌‌‌​​‌​​‌‌​‌​​‌​‌‌​​‌‌‌​‌‌​‌​​​​‌‌‌​‌​​​​‌​​​​​​​‌​‌​​​​‌‌​​​‌‌​​‌​‌​​‌​​‌​​​​​​​‌‌​​‌​​​‌‌​​​​​​‌‌​​‌​​​‌‌​‌‌​​​‌​​​​​​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​​‌​​​​​​‌​‌​​​​​‌‌​​‌​‌​‌‌‌​​‌‌​‌‌​‌‌‌‌​‌‌​‌‌‌​​‌‌​​‌​‌​‌‌​‌‌‌​​​‌​​​​​​​‌​‌​​​​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​‌​‌​​​​​​‌‌‌​​​​​‌‌‌​​​​​‌​‌​​‌​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌​‌​​​​​‌‌​‌‌‌‌​‌‌​‌‌​​​‌‌‌‌​​‌​‌​​​‌‌​​‌‌​‌‌‌‌​‌‌‌​​‌​​‌‌​‌‌​‌​​‌​​​​​​‌​​‌​​‌​‌‌​‌‌‌​​‌‌‌​‌​​​‌‌​​‌​‌​‌‌‌​​‌​​‌‌​‌‌‌​​‌‌​​​​‌​‌‌​‌‌​​​​‌​​​​​​‌​‌​‌​‌​‌‌‌​​‌‌​‌‌​​‌​‌​​‌​​​​​​‌​​‌‌​​​‌‌​‌​​‌​‌‌​​​‌‌​‌‌​​‌​‌​‌‌​‌‌‌​​‌‌‌​​‌‌​‌‌​​‌​‌​​‌​​​​​​​‌‌​​​‌​​‌​‌‌‌​​​‌‌​​​​​​‌​‌‌‌​​​‌‌​​​​​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌‌​​‌‌‌​‌‌​‌​​‌​‌‌‌​‌​​​‌‌​‌​​​​‌‌‌​‌​‌​‌‌​​​‌​​​‌​‌‌‌​​‌‌​​​‌‌​‌‌​‌‌‌‌​‌‌​‌‌​‌​​‌​‌‌‌‌​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​‌​‌​​​​​​‌‌‌​​​​​‌‌‌​​​​​‌​‌‌‌‌​‌‌‌​​‌‌​​‌‌​​‌‌​​‌​‌‌​‌​‌‌​​​‌​​‌‌‌​‌​‌​‌‌​​​‌‌​‌‌​‌​‌‌​‌‌​​‌​‌​‌‌‌​‌​​​​‌​‌‌​‌​‌‌​​​‌​​‌‌‌​​‌​​‌‌​‌‌‌‌​‌‌‌​‌‌‌​‌‌‌​​‌‌​‌‌​​‌​‌​‌‌‌​​‌​⁠
import { api, onEvent, subscribeStream } from './api.js';
import { el, fmtBytes, fmtSpeed, fmtDate, basename, debounce, srcIconEl } from './util.js';
import { nav, parentOf, clipboard, clipHasItems, view } from './state.js';
import { Grid, COLUMNS, DEFAULT_COLS } from './grid.js';
import { Tree } from './tree.js';
import {
  confirm, prompt, properties, doctorDialog, transferManager, runningTasks,
  sourceEditor, helpSheet, resolveTransferOpts, presignDialog, presignListDialog, toast, openModal,
  versionsDialog, contentVersionsDialog, markersDialog, adminDialog, editingDialog, findDialog, classDialog, lockDialog,
  usageGuideDialog, sourcesInfoDialog, importCredsDialog, pill, versionChoiceDialog,
  renderPopoutView, licenseGate,
  runDeleteWindow, delTypedOn, delWindowOn, delAutoConfirm, licenseDialog, taskKindVerb, promptFile,
} from './dialogs.js';
import { LICENSE, licenseLine } from './license.js';
import { LocalPane, aggregateCompare } from './local.js';
import { t, detectLang, setLang, languages, LANG_NAMES } from './i18n.js';
import { setCommandContext, updateCommandState, commandState } from './commands.js';
import { createMenubar } from './menubar.js';
import { createLogArea } from './logarea.js';
import { settingsDialog } from './settings.js';

const $ = (id) => document.getElementById(id);

const grid = new Grid();
const localPane = new LocalPane();
const tree = new Tree({
  onNavigate: (loc) => nav.to(loc),
  onDropTo: dropToTarget,
  onContext: showTreeMenu,
  guardOf: (bucket, source) => guardCache.get(guardKey(source, bucket)) || null,
  onGuardClick: (bucket, source) => navThen(
    { kind: 'objects', source, bucket, prefix: '' },
    () => adminDialog(bucket, refreshCurrent),
  ),
  onBuckets: (source, names) => ensureGuards(source, names),
});
const logArea = createLogArea();

setCommandContext({
  selectionCount: () => grid.selectedRows().length,
  hasProfile: () => sources.some((s) => s.type === 's3'),
  localSelectionCount: () => (localPane.visible ? localPane.grid.selectedRows().length : 0),
  localPaneOpen: () => localPane.visible,
  osClipFiles: () => osClipFilesReady, // Explorer files wait on the OS clipboard
});

let sources = []; // data sources of any type (M8)
let currentEntries = []; // unfiltered rows of the active view

// The view source: the S3 source the engine-native APIs (bucket/object
// admin, versions, presign, rename/delete, upload/download) address. It
// follows navigation — opening a source's buckets/objects sets it Go-side.
let viewSource = '';
let dragUrls = []; // loopback URLs for the current selection (OS drag-out)
// Desktop drag-out goes native instead: WebView2 ignores DownloadURL, so a
// plain drag of a files-only selection hands the rows to Go, which stages
// them and floats a real OLE drag — Explorer drops actual files, and a
// release over the app itself comes back as drag:self-drop and routes
// through the internal move/copy logic. Resolved once like the popout
// flag — a binding round-trip cannot gate a synchronous dragstart.
let osDragNative = false;
// true while a native drag initiated here is (or just was) floating: the
// drag:self-drop event must only ever route one of OUR gestures.
let nativeDragOut = false;
if (window.wails?.Call?.ByName) {
  api.IsDesktopShell().then((v) => { osDragNative = !!v; }).catch(() => {});
}

// Streaming listing state (M5): generation counter + active stream token.
let listSeq = 0;
let listStream = { token: null, off: null };
let pendingSelect = null; // {bucket, prefix, key} — row to select after load

// View-load generation: guards the non-streaming listing paths (buckets,
// remote folders) against the navigate-away race — a response that lands
// after a newer navigation started is dropped instead of clobbering the
// view the user is looking at.
let viewSeq = 0;
// Consecutive failed background (silent) refreshes. The first failure of
// a streak tells the user the rows on screen are stale; recovery (any
// completed refresh) clears the streak silently.
let silentFailStreak = 0;
function noteSilentFail() {
  if (silentFailStreak === 0) toast(t('staleRefresh'), 'error');
  silentFailStreak++;
}

// applyColumnPrefs restores the persisted visible-column sets (Settings →
// View) and the delete-marker badge toggle for both grids. "name" is the
// identity column — the grid forces it in; an unknown/empty stored value
// falls back to the default layout.
function applyColumnPrefs() {
  const parse = (v) => {
    const ids = (v || '').split(',').map((s) => s.trim()).filter((id) => COLUMNS.some((c) => c.id === id));
    return ids.length ? ids : DEFAULT_COLS;
  };
  grid.setColumns(parse(localStorage.getItem('s3b-cols')));
  localPane.grid.setColumns(parse(localStorage.getItem('s3b-cols-local')));
  // version/marker badges are opt-in (Settings → View); both default off
  grid.showMarkers = localStorage.getItem('s3b-show-markers') === '1';
  grid.showVersions = localStorage.getItem('s3b-show-versions') === '1';
}

// ============================ boot ============================
async function boot() {
  setLang(detectLang());
  initTheme();
  // A native popout window (Wails v3 multi-window): the main window opened
  // this page with ?popout=<kind> to float exactly one view as a real OS
  // window — render just that view, none of the app chrome.
  const popoutQS = new URLSearchParams(location.search);
  if (popoutQS.get('popout')) {
    document.body.classList.add('popout-win');
    renderPopoutView(popoutQS.get('popout'), popoutQS);
    return;
  }
  // Setup phase: a fresh install accepts the license before any chrome
  // renders. Popout windows skip it — they only ever open from an
  // already-accepted session.
  await licenseGate();
  // Popout geometry is session state: a window reopens at its last
  // position/size until the app closes, and the next launch starts
  // fresh — wipe the leftovers a previous run persisted (the placement
  // choice is a setting and survives). Popout windows never get here,
  // so a floating view can't erase the session it lives in.
  for (const k of Object.keys(localStorage)) {
    if (k.startsWith('s3b-popout-') && k !== 's3b-popout-center') localStorage.removeItem(k);
  }
  applyColumnPrefs();
  $('status-version').textContent = `s3b v${await api.GetVersion()}`;
  wireToolbar();
  mountMenubar();
  wireGrid();
  wireLocalPane();
  wireKeys();
  wireDrop();
  wireEvents();

  // Log drawer: mount (subscription is wired in wireEvents) and restore
  // visibility from the last session.
  $('logarea').replaceChildren(logArea.root);
  if (localStorage.getItem('s3b-log') === '1') $('logarea').classList.remove('hidden');

  // Auto refresh: OFF by default — a deliberate opt-in (View menu / Settings)
  // rather than a background poller the user never asked for; restore the
  // interval + refresh-on-focus from the last session.
  const ar = parseInt(localStorage.getItem('s3b-autorefresh') || '0', 10);
  if (ar > 0) setAutoRefresh(ar);
  refreshOnFocus = localStorage.getItem('s3b-refresh-focus') === '1';

  const ok = await refreshSources();
  if (ok) nav.to(sourceHomeLoc());
  initSidebarResize();
  refreshPfState(); // container sessions do not survive restarts; defensive
  if (localStorage.getItem('s3b-panes') === '1') localPane.show();
  osClipAdopt(); // baseline the OS clipboard seq so later Explorer copies are detected
  updateCommandState();
}

// toggleLogArea shows/hides the bottom log drawer (View menu, Ctrl+L,
// status-bar button) and remembers the choice.
function toggleLogArea() { setLogArea($('logarea').classList.contains('hidden')); }

function setLogArea(open) {
  $('logarea').classList.toggle('hidden', !open);
  localStorage.setItem('s3b-log', open ? '1' : '0');
}

// ============================ auto refresh ============================
let autoTimer = null;
let autoRefreshMs = 0;
let refreshOnFocus = false;

// autoRefreshBlocked: conditions under which a background refresh must not
// fire — no data source is configured (nothing to poll), a modal or context
// menu is open, or transfers are running (the status-bar jobs indicator is
// visible exactly then; uploads also refresh views via s3:changed on their
// own).
function autoRefreshBlocked() {
  if (sources.length === 0) return true; // fully off until a source exists
  if (!$('modal-root').classList.contains('hidden')) return true;
  if (!$('ctxmenu').classList.contains('hidden')) return true;
  if (!$('status-jobs').classList.contains('hidden')) return true;
  return false;
}

function autoTick() {
  if (document.hidden || autoRefreshBlocked()) return;
  refreshCurrent(true); // silent: no flicker, selection kept
}

// setAutoRefresh (re)starts the interval timer; 0 disables it. The choice
// is persisted and mirrored in the status bar.
function setAutoRefresh(ms) {
  autoRefreshMs = ms;
  localStorage.setItem('s3b-autorefresh', String(ms));
  if (autoTimer) { clearInterval(autoTimer); autoTimer = null; }
  if (ms > 0) autoTimer = setInterval(autoTick, ms);
  const sb = $('status-auto');
  sb.classList.toggle('hidden', ms === 0);
  sb.textContent = `\u21BB ${ms / 1000}s`;
}

function setRefreshOnFocus(on) {
  refreshOnFocus = on;
  localStorage.setItem('s3b-refresh-focus', on ? '1' : '0');
}

// Theme preference: 'light', 'dark', or 'auto'. 'auto' follows the OS
// light/dark setting live (the VS 2026 "use system setting" pattern) — the
// media listener stays registered until an explicit theme replaces it.
const themeMq = matchMedia('(prefers-color-scheme: dark)');
let themeAuto = false;
const applyAutoTheme = () => {
  document.documentElement.dataset.theme = themeMq.matches ? 'dark' : 'light';
};
const setThemeAuto = (on) => {
  if (on === themeAuto) return;
  themeAuto = on;
  if (on) themeMq.addEventListener('change', applyAutoTheme);
  else themeMq.removeEventListener('change', applyAutoTheme);
};

function initTheme() {
  const saved = localStorage.getItem('s3b-theme');
  if (saved === 'auto' || !saved) {
    // Unsaved means "never chose" — the boot default follows the system,
    // so the Settings select honestly shows Auto (system).
    setThemeAuto(true);
    applyAutoTheme();
    return;
  }
  document.documentElement.dataset.theme = saved;
}

// setThemePref is the Settings apply hook: persists the choice and applies
// it immediately ('auto' re-registers the system listener).
function setThemePref(v) {
  localStorage.setItem('s3b-theme', v);
  if (v === 'auto') { setThemeAuto(true); applyAutoTheme(); return; }
  setThemeAuto(false);
  document.documentElement.dataset.theme = v;
}

function toggleTheme() {
  // The quick toggle always lands on an explicit theme (and switches an
  // 'auto' session to the opposite of what is on screen).
  setThemePref(document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark');
}

// ============================ data sources (M8) ============================
// providerLabel mirrors pkg/provider.Detect for the dropdown/dialog labels
// (the Source JSON does not carry the derived provider key).
function providerLabel(endpoint) {
  const e = (endpoint || '').toLowerCase();
  if (!e) return 'aws';
  const has = (s) => e.includes(s);
  switch (true) {
    case has('amazonaws.com'): return 'aws';
    case has('wasabisys.com'): return 'wasabi';
    case has('backblazeb2.com'): return 'b2';
    case has('objectstorage.cloud.ibm.com'): return 'ibm';
    case has('digitaloceanspaces.com'): return 'do';
    case has('minio') || has('aistor'): return 'minio';
    case has('cloudflare') || has('r2'): return 'cloudflare';
    case has('hetzner'): return 'hetzner';
    case has('ceph') || has('rgw'): return 'ceph';
    case has('dell') || has('ecs'): return 'dell';
    case has('netapp') || has('storagegrid'): return 'netapp';
    case /^(localhost|127\.|\[?::1)/.test(e): return 'minio'; // local labs
    default: return 'custom';
  }
}

// refreshSources reloads the workspace's data sources (open Profile file,
// else the session-only registry), refreshes the tree/side pane and probes
// every source's connectivity for the status balls.
async function refreshSources() {
  try {
    sources = await api.ListSources();
  } catch (err) {
    toast(`Sources: ${err}`, 'error');
    sources = [];
  }
  $('status-profile').textContent = viewSource;
  renderSidebarHead();
  tree.setSources(sources, nav.current); // sources are the tree's top level
  // The side pane's source dropdown follows the source set
  localPane.sources = sources.map((s) => ({ id: s.id, name: s.name, type: s.type }));
  localPane.renderSources();
  if (!localPane.bindingApplied) localPane.restoreBinding();
  else if (localPane.binding.kind !== 'local'
    && !sources.some((s) => (s.id || s.name) === localPane.binding.source)) {
    localPane.rebind('local'); // bound source was removed
  }
  if (!sources.length) {
    showOnboarding();
    return false;
  }
  probeSources();
  updateCommandState();
  return true;
}

// sourceHomeLoc is the landing view of a source (or the first one): S3
// sources open their bucket's contents (legacy account-wide ones their
// bucket list), everything else its root directory.
function sourceHomeLoc(src) {
  const s = src || sources[0];
  if (!s) return { kind: 'onboarding' };
  if (s.type === 's3') {
    return s.bucket
      ? { kind: 'objects', source: s.name, bucket: s.bucket, prefix: '' }
      : { kind: 'buckets', source: s.name };
  }
  return { kind: 'remote', source: s.name, path: '' };
}

// navThen navigates and runs fn once the view source has settled on the
// target's source — engine-native dialogs (admin/doctor/versions/stat)
// address the view source, so a bucket of another source must be opened
// in the main view first. nav.to is fire-and-forget; this polls the module
// state with a timeout instead of chaining into loadView.
function navThen(loc, fn) {
  nav.to(loc);
  const want = loc.source || '';
  const t0 = Date.now();
  const tick = () => {
    if (viewSource === want || Date.now() - t0 > 4000) fn();
    else setTimeout(tick, 25);
  };
  tick();
}

// probeSource tests one source's connectivity: S3 sources get the backend
// probe (a HEAD on the scoped bucket, or a bucket list for legacy
// account-wide sources), everything else lists its root directory.
async function probeSource(s) {
  try {
    if (s.type === 's3') {
      const res = await api.TestSource(s.id || s.name);
      return !!res.ok;
    }
    await api.RemoteList(s.id || s.name, '/');
    return true;
  } catch {
    return false;
  }
}

// probeSources refreshes every source's status ball (busy while probing,
// ok/error after). Runs in the background after each sources reload.
let probing = false;
async function probeSources() {
  if (probing || !sources.length) return;
  probing = true;
  const map = {};
  for (const s of sources) map[s.name] = 'busy';
  tree.setStatus(map);
  await Promise.all(sources.map(async (s) => {
    map[s.name] = await probeSource(s) ? 'ok' : 'error';
    tree.setStatus({ ...map });
  }));
  probing = false;
}

// initSidebarResize wires the splitter between sidebar and main area: drag
// to resize (140px..half the window), persisted in localStorage.
function initSidebarResize() {
  const split = $('side-split');
  const aside = $('sidebar');
  if (!split || !aside) return;
  const apply = (w) => {
    document.documentElement.style.setProperty('--sidebar-w', `${w}px`);
  };
  const saved = parseInt(localStorage.getItem('s3b-sidebar-w') || '', 10);
  if (saved >= 140) apply(saved);
  split.addEventListener('mousedown', (e) => {
    e.preventDefault();
    document.body.classList.add('col-resizing');
    const onMove = (ev) => {
      const w = Math.min(Math.max(140, ev.clientX), Math.ceil(window.innerWidth / 2));
      apply(w);
      localStorage.setItem('s3b-sidebar-w', String(w));
    };
    const onUp = () => {
      document.body.classList.remove('col-resizing');
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  });
  split.addEventListener('dblclick', () => {
    localStorage.removeItem('s3b-sidebar-w');
    document.documentElement.style.removeProperty('--sidebar-w');
  });
}

// renderSidebarHead: the sidebar header is a static "Data sources" label
// with a persistent "+" add button — it no longer mirrors the active
// source/bucket (the tree's highlight already marks where you are).
function renderSidebarHead() {
  const head = $('sidebar-head');
  head.replaceChildren(
    document.createTextNode(t('sourcesTitle')),
    el('button', {
      class: 'side-add', text: '+', title: t('addSource'),
      onclick: () => sourceEditor(null, afterSourceSaved),
    }),
  );
}

function showOnboarding() {
  nav.replace({ kind: 'onboarding' });
  renderSidebarHead();
  // The sidebar stays empty (the header's + adds sources); the main-area
  // empty state carries the call to action.
  renderBreadcrumb();
  showEmpty(t('noSources'), t('noSourcesSub'), [
    el('button', { class: 'btn primary', text: `+ ${t('addSource')}`, onclick: () => sourceEditor(null, afterSourceSaved) }),
    el('button', { class: 'btn', text: 'Import S3 Credential\u2026', onclick: importCredsUi }),
  ]);
}

// importCredsUi opens the Import S3 Credential dialog: candidates from local
// credential files (AWS INI/rclone/JSON/.env/.s3bprofile) or a KMS service
// (Vault, AWS SM, Azure, GCP, custom HTTP), each testable before import.
async function importCredsUi() {
  await importCredsDialog(async (res) => {
    if (res?.imported?.length) {
      await refreshSources();
      refreshPfState();
      // The first import happens on the welcome screen ("No data sources
      // yet") — open the imported source's content instead of leaving the
      // stale call to action on screen.
      if (nav.current?.kind === 'onboarding') {
        const first = sources.find((s) => s.name === res.imported[0]);
        if (first) nav.to(sourceHomeLoc(first));
      }
    }
  });
}

function afterSourceSaved(saved) {
  refreshSources().then((ok) => {
    // ListSources is name-sorted, so "the last entry" is NOT reliably the
    // source just saved — navigate to the saved one (falling back for
    // callers without a source object).
    if (ok) nav.to(sourceHomeLoc(saved || sources[sources.length - 1]));
  });
  refreshPfState(); // a container edit flips the dirty flag
  toast('Source saved', 'ok');
}

// ============================ navigation ============================
nav.onNavigate((loc) => loadView(loc));

// ============================ favorites (M5) ============================
function favorites() {
  try { return JSON.parse(localStorage.getItem('s3b-favs') || '[]'); } catch { return []; }
}
function isFavorite(bucket) { return favorites().includes(bucket); }
function toggleFavorite(bucket) {
  const favs = favorites();
  const i = favs.indexOf(bucket);
  if (i >= 0) favs.splice(i, 1); else favs.push(bucket);
  localStorage.setItem('s3b-favs', JSON.stringify(favs));
  renderFavorites();
}
function renderFavorites() {
  const favs = favorites();
  $('fav-section').classList.toggle('hidden', favs.length === 0);
  $('favorites').replaceChildren(...favs.map((b) => el('div', {
    class: 'fav-row',
    role: 'listitem',
    title: `${viewSource}://${b}`,
    onclick: () => nav.to({ kind: 'objects', source: viewSource, bucket: b, prefix: '' }),
  },
    el('span', { class: 'fav-star', text: '\u2605' }),
    el('span', { class: 'fav-label', text: b }),
  )));
}

// setViewSourceFor resolves a location's source to its canonical NAME,
// pushes it Go-side (engine-native S3 APIs address it) and mirrors it in
// the status bar. Returns the resolved name ('' when no S3 source exists).
async function setViewSourceFor(loc) {
  let name = loc.source || '';
  if (!name) {
    const s = sources.find((x) => x.type === 's3');
    name = s ? s.name : '';
  } else {
    const s = sources.find((x) => x.name === name || x.id === name);
    name = s ? s.name : name;
  }
  loc.source = name;
  if (name && name !== viewSource) {
    try {
      await api.SetViewSource(name);
    } catch (err) {
      toast(`View source: ${err}`, 'error');
    }
    viewSource = name;
    $('status-profile').textContent = name;
    $('status-profile').title = `Active data source: ${name}`;
  }
  return name;
}

async function loadView(loc, { silent = false } = {}) {
  // silent (background auto refresh): keep the current rows, selection and
  // breadcrumb on screen while the new listing is fetched — no flicker. New
  // data swaps in once, in a single frame.
  // viewSeq: every navigation bumps it; a response that resolves after a
  // NEWER navigation started is dropped, so a slow directory can never
  // clobber the one the user actually opened (the classic A→B race).
  const seq = ++viewSeq;
  if (!silent) grid.clearSelection();
  updateNavButtons();
  if (!silent) renderBreadcrumb();
  tree.markCurrent(loc);
  // Non-silent navigation SHOWS work: skeleton rows + spinner while the
  // listing is in flight — a blank panel reads as "empty folder" on a
  // slow source, which is a lie until the response lands.
  if (silent) hideEmpty();
  else showLoading();
  // A silent refresh that completes clears any stale-data notice streak.
  let landed = false;

  try {
    if (loc.kind === 'buckets') {
      const src = await setViewSourceFor(loc);
      const buckets = await api.ListBuckets();
      if (seq !== viewSeq) return; // superseded by a newer navigation
      landed = true;
      currentEntries = buckets.map((b) => ({
        key: b.name, name: b.name, isDir: true, size: 0,
        lastModified: b.createdAt, bucketCreated: true,
      }));
      hideEmpty();
      grid.setRows(currentEntries);
      renderFavorites();
      if (!currentEntries.length) {
        showEmpty(t('noBuckets'), t('noBucketsSub'), [
          el('button', { class: 'btn primary', text: t('createBucket'), onclick: createBucket }),
        ]);
      }
      // always feed the tree — the legacy bucket level tracks the live
      // bucket set (created/deleted), even down to zero
      tree.refresh(src, buckets, loc);
      $('btn-up').disabled = true;
    } else if (loc.kind === 'objects') {
      await setViewSourceFor(loc);
      if (seq !== viewSeq) return;
      await loadObjectsStream(loc, silent);
      tree.reveal(loc).catch(() => {});
      localPane.syncTo(loc.prefix || '');
      $('btn-up').disabled = false;
    } else if (loc.kind === 'remote') {
      // sftp/scp/ftp/ftps/webdav/local source browsed through its remotefs
      // engine; rows carry the same shape as S3 listings
      const entries = await api.RemoteList(loc.source, loc.path || '/');
      if (seq !== viewSeq) return; // superseded — drop the stale rows
      landed = true;
      currentEntries = entries;
      hideEmpty();
      grid.setRows(entries);
      if (!currentEntries.length) {
        showEmpty(t('emptyFolder'), t('emptyFolderSub'), []);
      }
      tree.reveal(loc).catch(() => {});
      tree.updateRemoteDir(loc.source, loc.path || '/', entries);
      $('btn-up').disabled = !parentOf(loc);
    }
    if (landed) silentFailStreak = 0;
  } catch (err) {
    // A silent refresh keeps the current view on transient errors — but the
    // FIRST failure of a streak tells the user the rows are now stale.
    if (silent) {
      if (seq === viewSeq) noteSilentFail();
      return;
    }
    if (seq !== viewSeq) return;
    currentEntries = [];
    grid.setRows([]);
    showListError(err);
  }
  updateStatus();
}

// loadObjectsStream fills the grid incrementally from the streaming
// listing API: the first page renders immediately, Go-side memory stays
// at one page for million-object folders, and stale streams are
// canceled on navigation (M5).
async function loadObjectsStream(loc, silent = false) {
  const seq = ++listSeq;
  cancelListStream();
  if (pendingSelect && (pendingSelect.bucket !== loc.bucket || pendingSelect.prefix !== (loc.prefix || ''))) {
    pendingSelect = null; // user navigated elsewhere
  }
  if (!silent) {
    currentEntries = [];
    grid.setRows([]);
  }
  grid.setFilter(view.filter);
  // silent: pages buffer off-screen; the grid swaps once, when done.
  const buffered = [];
  const stream = subscribeStream(
    () => api.ListObjectsStream(loc.bucket, loc.prefix || ''),
    (p) => {
      if (seq !== listSeq) return; // superseded while pages still arrived
      if (p.error) {
        if (!silent) showListError(p.error);
        else noteSilentFail();
        return;
      }
      if (silent) {
        buffered.push(...p.entries);
        currentEntries = buffered;
      } else {
        // First data landed: the skeleton/loading overlay has done its
        // job — rows now tell the story (idempotent, so every page can
        // call it without layout cost once hidden).
        hideEmpty();
        currentEntries.push(...p.entries);
        grid.appendRows(p.entries);
      }
      updateStatus();
      if (p.done) {
        listStream.token = null;
        listStream.off?.();
        listStream.off = null;
        if (silent) {
          // merge the folder's ghost rows in BEFORE this single paint: the
          // fresh listing can't contain delete-marked children, and without
          // the carry they'd flash out here and only return once the
          // version-summary pass re-appends them (auto-refresh)
          grid.setRows([...buffered, ...ghostCarryFor(loc, buffered)]);
        }
        grid.apply(); // canonical folders-first ordering + active sort/filter
        decorateVersionMarkers(loc, seq);
        // Keep the sidebar tree in step with what the grid now shows —
        // the S3 twin of the remote path's updateRemoteDir call: folders
        // created, deleted or renamed anywhere appear in the tree without
        // a manual re-expand. currentEntries is complete only at done
        // (pages before it are partial). loc.source is unset on the
        // local-pane sync-up path; the listing went through the pinned
        // viewSource there, which is also the tree node's source.
        tree.updateObjectsDir(loc.source || viewSource, loc.bucket, loc.prefix || '',
          currentEntries.filter((e) => e.isDir));
        if (!currentEntries.length && !view.filter) {
          showEmpty(t('emptyFolder'), t('dropToUpload'), [
            el('button', { class: 'btn primary', text: '\u2191 Upload', onclick: (ev) => openMenu(ev.currentTarget, uploadChoices(uploadFiles, uploadFolder)) }),
          ]);
        }
        consumePendingSelect();
        updateStatus();
        if (silent) silentFailStreak = 0; // a completed refresh = healthy
      }
    },
  );
  listStream.off = stream.off;
  let token;
  try {
    token = await stream.begin;
  } catch (err) {
    if (listStream.off === stream.off) listStream.off = null;
    if (seq !== listSeq || silent) return;
    showListError(err);
    return;
  }
  if (seq !== listSeq) { api.CancelList(token).catch(() => {}); return; }
  listStream.token = token;
  stream.flush();
}

function cancelListStream() {
  if (listStream.token) { api.CancelList(listStream.token).catch(() => {}); listStream.token = null; }
  listStream.off?.();
  listStream.off = null;
}

// vmarkRowCap bounds delete-marker decoration: beyond it the extra
// ListObjectVersions pass costs more than the badges are worth.
const vmarkRowCap = 500;

// lastGhosts remembers one folder's ghost rows (delete-marked children,
// invisible to any plain listing) across refreshes. A silent refresh's
// setRows swaps in the fresh listing — which by definition has no ghosts —
// so without carrying them across, ghost rows would vanish at that paint
// and only reappear once the version-summary pass re-appends them: the
// flash only hidden rows suffer on auto-refresh. Normal rows never leave
// the grid, so they never flash.
let lastGhosts = { key: '', rows: [] };

function ghostsKey(loc) {
  return `${loc.source || ''}|${loc.bucket}|${loc.prefix || ''}`;
}

// ghostCarryFor returns the folder's remembered ghost rows that the fresh
// listing still doesn't contain, so the silent swap can merge them in
// before its single paint. A ghost whose name now exists as a real row
// (restored or re-uploaded) is not carried — the summary pass drops it.
function ghostCarryFor(loc, freshRows) {
  if (localStorage.getItem('s3b-show-hidden') !== '1') return [];
  if (lastGhosts.key !== ghostsKey(loc)) return [];
  const have = new Set(freshRows.map((r) => `${r.isDir ? 'd' : 'f'}:${r.name}`));
  return lastGhosts.rows.filter((r) => !have.has(`${r.isDir ? 'd' : 'f'}:${r.name}`));
}

// decorateVersionMarkers badges the folder view with version + delete-marker
// counts (versioned buckets only): the ⟲ badge carries each row's version
// count, the ⛔ badge its marker count (directories aggregate everything
// beneath), and all-deleted folders are dimmed. With "show hidden" on
// (Settings → View, default off) it also appends ghost rows — children whose
// newest state is a delete marker, which a plain listing never returns.
// One extra ListObjectVersions pass, best-effort — a failure simply leaves
// the view undecorated. seq guards against painting a stale folder.
async function decorateVersionMarkers(loc, seq) {
  try {
    const g = await ensureGuard(loc.source, loc.bucket);
    if (!g || g.versioning !== 'Enabled') {
      lastGhosts = { key: '', rows: [] }; // no ghosts can exist here
      return;
    }
    if (seq !== listSeq || grid.all.length > vmarkRowCap) return;
    const kids = await api.PrefixVersionSummary(loc.bucket, loc.prefix || '');
    if (seq !== listSeq) return; // navigated away while listing versions
    if (localStorage.getItem('s3b-show-hidden') === '1') {
      const prefix = loc.prefix || '';
      // reconcile against the fresh summary: ghosts that are no longer
      // all-deleted (restored, or purged out of existence) leave; new
      // delete markers join. The carried rows from ghostCarryFor are
      // re-confirmed here, so a state change costs one summary latency —
      // never a flash.
      const deleted = new Set(kids
        .filter((c) => c.allDeleted)
        .map((c) => `${c.isDir ? 'd' : 'f'}:${c.name}`));
      const stale = grid.all
        .filter((r) => r.ghost && !deleted.has(`${r.isDir ? 'd' : 'f'}:${r.name}`))
        .map((r) => r.key);
      if (stale.length) grid.removeRows(stale);
      const have = new Set(grid.all.map((r) => `${r.isDir ? 'd' : 'f'}:${r.name}`));
      const ghosts = kids
        .filter((c) => c.allDeleted && !have.has(`${c.isDir ? 'd' : 'f'}:${c.name}`))
        .map((c) => ({
          name: c.name,
          key: prefix + c.name + (c.isDir ? '/' : ''),
          isDir: c.isDir,
          size: 0,
          ghost: true,
        }));
      if (ghosts.length) {
        grid.appendRows(ghosts);
        grid.apply(); // canonical folders-first ordering for the appended ghosts
      }
      lastGhosts = { key: ghostsKey(loc), rows: grid.all.filter((r) => r.ghost) };
    } else {
      lastGhosts = { key: '', rows: [] }; // setting off: nothing to carry
    }
    grid.setMarkers(new Map(kids.map((c) => [`${c.isDir ? 'd' : 'f'}:${c.name}`, c])));
  } catch { /* decoration is best-effort */ }
}

// consumePendingSelect focuses a row requested by an earlier action
// (deep-search "open location") once its listing finished.
function consumePendingSelect() {
  if (!pendingSelect) return;
  const key = pendingSelect.key;
  pendingSelect = null;
  const idx = grid.rows.findIndex((r) => r.key === key);
  if (idx < 0) return;
  grid.sel = new Set([key]);
  grid.focusKey = key;
  grid.anchorKey = key;
  grid.scrollTo(idx);
  grid.render(true);
}

// openSearchResult navigates to a deep-search result's folder and marks
// the object row for selection.
function openSearchResult(loc) {
  pendingSelect = loc;
  nav.to({ kind: 'objects', source: viewSource, bucket: loc.bucket, prefix: loc.prefix });
}

function refreshCurrent(silent = false) {
  if (nav.current) loadView({ ...nav.current }, { silent });
}

function updateNavButtons() {
  $('btn-back').disabled = !nav.canBack();
  $('btn-forward').disabled = !nav.canForward();
}

function renderBreadcrumb() {
  const bc = $('breadcrumb');
  bc.replaceChildren();
  const loc = nav.current || { kind: 'buckets', source: viewSource };
  // Nothing to anchor the path on (onboarding: no sources configured, or
  // a fresh session where none is selected yet): the path bar starts
  // empty — no orphan root icon for a source that isn't there.
  if (loc.kind === 'onboarding') return;
  if (!nav.current && !sources.some((x) => x.name === viewSource)) return;
  if (loc.kind === 'remote') {
    const atRoot = !loc.path || loc.path === '/';
    const s = sources.find((x) => x.name === loc.source);
    const root = el('span', { class: `crumb${atRoot ? ' current' : ''}`, title: `${loc.source}://${loc.path || '/'}` },
      srcIconEl(s?.type, s?.color), loc.source);
    root.onclick = () => nav.to({ kind: 'remote', source: loc.source, path: '' });
    bc.appendChild(root);
    if (loc.path && loc.path !== '/') {
      let acc = '';
      for (const part of loc.path.split('/')) {
        if (!part) continue;
        acc += part + '/';
        bc.appendChild(el('span', { class: 'crumb-sep', text: '\u203A' }));
        const c = el('span', { class: 'crumb', text: part });
        const target = acc;
        c.onclick = () => nav.to({ kind: 'remote', source: loc.source, path: target });
        bc.appendChild(c);
      }
      bc.lastChild?.classList.add('current');
    }
    return;
  }
  const srcName = loc.source || viewSource;
  const s = sources.find((x) => x.name === srcName);
  const root = el('span', { class: `crumb${loc.kind === 'buckets' ? ' current' : ''}`, title: `${srcName}://` },
    srcIconEl(s?.type || 's3', s?.color), srcName);
  root.onclick = () => nav.to({ kind: 'buckets', source: srcName });
  bc.appendChild(root);
  if (loc.kind !== 'objects') return;
  bc.appendChild(el('span', { class: 'crumb-sep', text: '\u203A' }));
  const b = el('span', { class: 'crumb', text: loc.bucket });
  b.onclick = () => nav.to({ kind: 'objects', source: srcName, bucket: loc.bucket, prefix: '' });
  bc.appendChild(b);
  let acc = '';
  for (const part of (loc.prefix || '').split('/')) {
    if (!part) continue;
    acc += part + '/';
    bc.appendChild(el('span', { class: 'crumb-sep', text: '\u203A' }));
    const c = el('span', { class: 'crumb', text: part });
    const target = acc;
    c.onclick = () => nav.to({ kind: 'objects', source: srcName, bucket: loc.bucket, prefix: target });
    bc.appendChild(c);
  }
  bc.lastChild?.classList.add('current');
}

// ====================== canonical path bar ======================
// Every location has ONE path format across all source kinds:
// "NAME://bucket/prefix/" for S3 sources, "NAME:///dir/" for the remote
// engines — the source's display name is the scheme. Clicking the navbar's
// empty area swaps the breadcrumb for an editable field with that string,
// so a path can be copied out or pasted in from anywhere and navigated
// with Enter.
function canonicalPath(loc) {
  if (!loc) return '';
  if (loc.kind === 'buckets') return `${loc.source || viewSource}://`;
  if (loc.kind === 'objects') return `${loc.source || viewSource}://${loc.bucket}/${loc.prefix || ''}`;
  if (loc.kind === 'remote') return `${loc.source}://${loc.path || '/'}`;
  return '';
}

// parsePath maps a pasted path back to a location: the scheme must match a
// source's name or id (the source name IS the scheme). Returns { loc } or
// null when nothing matches.
function parsePath(str) {
  const m = String(str || '').trim().match(/^([A-Za-z0-9._-]+):\/\/(.*)$/);
  if (!m) return null;
  const scheme = m[1].toLowerCase();
  const rest = m[2].replace(/\\/g, '/').replace(/^\/+/, '');
  const src = sources.find((s) => s.name.toLowerCase() === scheme
    || String(s.id || '').toLowerCase() === scheme);
  if (!src) return null;
  if (src.type === 's3') {
    if (!rest) return { loc: { kind: 'buckets', source: src.name } };
    const parts = rest.replace(/\/+$/g, '').split('/');
    const bucket = parts.shift();
    if (!bucket) return null;
    const prefix = parts.length ? `${parts.join('/')}/` : '';
    return { loc: { kind: 'objects', source: src.name, bucket, prefix } };
  }
  const path = rest ? `/${rest.replace(/\/+$/g, '')}/` : '/';
  return { loc: { kind: 'remote', source: src.name, path } };
}

// editPath swaps the breadcrumb for a one-line editable field holding the
// canonical path: copy out, paste in, Enter navigates, Esc cancels.
function editPath() {
  const bc = $('breadcrumb');
  const cur = canonicalPath(nav.current);
  if (!cur || bc.querySelector('input.path-edit')) return;
  const restore = () => renderBreadcrumb();
  const inp = el('input', { type: 'text', class: 'filter path-edit', spellcheck: 'false' });
  inp.value = cur;
  bc.replaceChildren(inp);
  inp.focus();
  inp.select();
  inp.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      const parsed = parsePath(inp.value);
      if (parsed) {
        nav.to(parsed.loc);
      } else {
        toast(t('pathInvalid', { p: inp.value.trim() || '?' }), 'error');
        inp.focus();
      }
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      restore();
    }
  });
  inp.addEventListener('blur', restore);
}

// ====================== bucket guard state (M10.4) ======================
// guardCache memoizes one guard state per source+bucket per session. The
// state shows as small icons after the bucket's name in the Data Sources
// tree (🔄 versioning, 🔒 object lock) — clicking an icon opens the admin
// panel — and feeds the bucket properties dialogs as Enabled/Disabled pills.
const guardCache = new Map();
const guardKey = (source, bucket) => `${source || viewSource || ''}/${bucket}`;

// ensureGuards fetches the guard state of one source's buckets the tree
// just drew (skips cached ones), then re-renders the tree once so the
// icons appear. SourceGetBucketGuard pins the source — the buckets may
// belong to any configured source, not just the view source. Fetch
// failures are NOT cached — a transient error must not permanently hide
// the versioning/lock icons or gate the delete-mode dialog — but retried
// a few times with backoff, then left cold (later calls still retry).
const guardTries = new Map();
const GUARD_MAX_TRIES = 4;
function ensureGuards(source, names) {
  const missing = names.filter((b) => !guardCache.has(guardKey(source, b)));
  if (!missing.length) return;
  let pending = missing.length;
  for (const b of missing) {
    const k = guardKey(source, b);
    api.SourceGetBucketGuard(source, b)
      .then((g) => { guardCache.set(k, g); guardTries.delete(k); })
      .catch(() => {
        const tries = (guardTries.get(k) || 0) + 1;
        guardTries.set(k, tries);
        if (tries < GUARD_MAX_TRIES) {
          setTimeout(() => { if (!guardCache.has(k)) ensureGuards(source, [b]); }, 4000 * tries);
        }
      })
      .finally(() => {
        pending -= 1;
        if (pending === 0 && missing.some((b) => guardCache.has(guardKey(source, b)))) tree.render();
      });
  }
}

// ensureGuard returns one bucket's guard state, fetching it once when the
// cache is cold (properties dialogs use it; the tree batch-fetches via
// ensureGuards on its own). A failed fetch returns the zero guard without
// caching, so the next call retries.
async function ensureGuard(source, bucket) {
  const k = guardKey(source, bucket);
  if (!guardCache.has(k)) {
    try {
      guardCache.set(k, await api.SourceGetBucketGuard(source || viewSource, bucket));
    } catch {
      return { versioning: '', lockEnabled: false };
    }
  }
  return guardCache.get(k);
}

// guardRows renders the versioning / object-lock rows shared by the bucket
// and folder properties dialogs (a folder inherits its bucket's guard) as
// the same Enabled/Disabled pills everywhere.
function guardRows(g) {
  return [
    ['Versioning', pill(g.versioning === 'Enabled')],
    ['Object Lock', pill(!!g.lockEnabled)],
  ];
}

// ============================ grid wiring ============================
// refreshDragUrls precomputes loopback URLs for the current selection so a
// drag out to the OS (Explorer, browsers) can attach DownloadURL /
// text-uri-list data synchronously in dragstart (dragstart cannot await).
function refreshDragUrls() {
  dragUrls = [];
  if (osDragNative) return; // desktop drags float natively (grid.on.dragOS)
  const loc = nav.current;
  const rows = grid.selectedRows();
  if (!rows.length || rows.some((r) => r.isDir)) return; // files only (folders would need zips)
  if (loc?.kind !== 'objects' && loc?.kind !== 'remote') return;
  const items = loc.kind === 'objects'
    ? rows.map((r) => ({ source: loc.source || viewSource, bucket: loc.bucket, key: r.key, name: r.name, size: r.size || 0 }))
    : rows.map((r) => ({ source: loc.source, key: r.key, name: r.name, size: r.size || 0 }));
  api.MakeDragUrls(items)
    .then((urls) => { if (urls?.length === rows.length) dragUrls = urls; })
    .catch(() => {});
}

function wireGrid() {
  grid.on.select = () => { updateStatus(); refreshDragUrls(); };
  grid.on.dragOS = (e, rows) => {
    // OS drag-out. Desktop: a plain drag of a files-only selection floats
    // a native OLE drag — Go stages the selection and Explorer drops real
    // files; releasing the gesture over the app itself returns as
    // drag:self-drop and routes through the same move/copy logic as an
    // internal drop. The DOM drag is cancelled because one mouse cannot
    // serve two drags; folder-bearing selections keep the in-app D&D.
    // Browsers/server: rows leave as downloadable loopback URLs (the
    // DownloadURL trick is a browser feature WebView2 lacks).
    const loc = nav.current;
    if (osDragNative && rows.length && !rows.some((r) => r.isDir)
      && (loc?.kind === 'objects' || loc?.kind === 'remote')) {
      const items = loc.kind === 'objects'
        ? rows.map((r) => ({ source: loc.source || viewSource, bucket: loc.bucket, key: r.key, name: r.name, size: r.size || 0 }))
        : rows.map((r) => ({ source: loc.source, key: r.key, name: r.name, size: r.size || 0 }));
      e.preventDefault();
      nativeDragOut = true;
      api.DragOutFiles(items)
        .catch((err) => toast(`Drag-out: ${err}`, 'error'))
        // Go emits drag:self-drop before this promise resolves, but event
        // delivery and promise settlement race inside the webview — hold
        // the gesture flag briefly past the end.
        .finally(() => { setTimeout(() => { nativeDragOut = false; }, 250); });
      return;
    }
    if (!dragUrls.length || dragUrls.length !== rows.length) return;
    if (rows.length === 1) {
      e.dataTransfer.setData('DownloadURL', `application/octet-stream:${rows[0].name}:${dragUrls[0]}`);
    }
    e.dataTransfer.setData('text/uri-list', dragUrls.join('\r\n'));
  };
  grid.on.activate = (row) => {
    const loc = nav.current;
    if (loc.kind === 'remote') {
      if (row.isDir) nav.to({ kind: 'remote', source: loc.source, path: row.key });
      else downloadSelection([{ key: row.key, size: row.size, name: row.name }]);
      return;
    }
    if (loc.kind === 'buckets') {
      nav.to({ kind: 'objects', source: loc.source, bucket: row.key, prefix: '' });
    } else if (row.isDir) {
      nav.to({ kind: 'objects', source: loc.source, bucket: loc.bucket, prefix: row.key });
    } else {
      downloadSelection([{ key: row.key, size: row.size, name: row.name }]);
    }
  };
  grid.on.context = (e, rows) => showContextMenu(e, rows);
  // Right-click on a header cell: the column picker (Settings' catalog one
  // click closer — Explorer's header menu pattern).
  grid.on.headerMenu = (e) => columnMenu(e, grid, 's3b-cols');
  // name-cell badges: ⟲ opens the Versions window (files) or the Content
  // Versions window (folders — the object-timeline dialog would paginate the
  // whole subtree); ⛔ opens the Delete Marker window for either.
  grid.on.badgeV = (row) => {
    const loc = nav.current;
    if (!loc || loc.kind !== 'objects') return;
    if (row.isDir) contentVersionsDialog(loc.bucket, row.key, refreshCurrent);
    else versionsDialog(loc.bucket, row.key, refreshCurrent);
  };
  grid.on.badgeM = (row) => {
    const loc = nav.current;
    if (!loc || loc.kind !== 'objects') return;
    markersDialog(loc.bucket, [{ key: row.key, isDir: row.isDir }], loc.prefix || '', refreshCurrent);
  };
  // The Delete Marker window's inline opt-in flips the same localStorage
  // knob boot reads — keep the grid's ⛔ badges in step the moment it
  // changes (identical effect to the View-menu item).
  window.addEventListener('s3b-markers-changed', () => {
    grid.showMarkers = localStorage.getItem('s3b-show-markers') === '1';
    grid.render();
  });
  grid.on.drop = (targetRow, data, e) => {
    const loc = nav.current;
    if (loc.kind === 'objects') dropToTarget({ kind: 's3', source: loc.source || viewSource, bucket: loc.bucket, dir: targetRow.key }, data, e);
    else if (loc.kind === 'remote') dropToTarget({ kind: 'remote', source: loc.source, dir: targetRow.key }, data, e);
  };
  // Body-level drop target: the empty area below the rows accepts the same
  // payloads as folder rows and transfers into the current directory.
  const gridBodyMimes = ['application/x-s3b', 'application/x-s3b-local'];
  grid.body.addEventListener('dragover', (e) => {
    if (e.target.closest('.grid-row')) return; // row handlers own it
    // external OS file drags highlight too (their upload arrives via the
    // wails:file-drop event, not the DOM drop handler below)
    const types = e.dataTransfer.types;
    if (xferDestOf(nav.current) && (gridBodyMimes.some((t) => types.includes(t)) || types.includes('Files'))) {
      e.preventDefault();
      grid.body.classList.add('drop-target');
    }
  });
  grid.body.addEventListener('dragleave', (e) => {
    if (e.target === grid.body || e.target === grid.canvas) grid.body.classList.remove('drop-target');
  });
  grid.body.addEventListener('drop', (e) => {
    grid.body.classList.remove('drop-target');
    if (e.target.closest('.grid-row')) return;
    const dest = xferDestOf(nav.current);
    if (!dest) return;
    let data = null;
    for (const t of gridBodyMimes) {
      const d = e.dataTransfer.getData(t);
      if (d) { data = JSON.parse(d); break; }
    }
    if (!data) return;
    e.preventDefault();
    dropToTarget(dest, data, e);
  });
  grid.body.addEventListener('mousedown', (e) => {
    if (e.target === grid.body || e.target === grid.canvas) {
      grid.clearSelection();
      startMarquee(e);
    }
  });
  // empty-area right-click: folder/bucket-level menu
  grid.body.addEventListener('contextmenu', (e) => {
    if (e.target.closest('.grid-row')) return; // row menu handles it
    e.preventDefault();
    showEmptyAreaMenu(e);
  });
  // the empty-state overlay covers the grid body in empty folders/bucket
  // lists — right-clicks land on it, so it must open the same menu
  $('empty-state').addEventListener('contextmenu', (e) => {
    e.preventDefault();
    showEmptyAreaMenu(e);
  });
  // sidebar background right-click: profile + tree management
  $('tree').addEventListener('contextmenu', (e) => {
    if (e.target.closest('.tnode')) return; // node menu lands with sidebar parity (M7.9)
    e.preventDefault();
    openMenu(e, [
      [t('addSource'), '', () => sourceEditor(null, afterSourceSaved)],
      null,
      ['Import S3 Credential\u2026', '', () => importCredsUi()],
      null,
      [t('pf.new'), '', newProfileFileUi],
      [t('pf.open'), '', openProfileFileUi],
      null,
      ['Collapse all', '', () => tree.collapseAll()],
      ['Refresh', 'F5', () => refreshCurrent()],
    ]);
  });
}

function wireLocalPane() {
  localPane.on.openFail = (e) => toast(`${localPane.binding.kind === 'remote' ? localPane.binding.source : 'Local'}: ${e}`, 'error');
  // Drops dispatch by pane binding: a remote-bound pane is a transfer target
  // like any other remote dir (move/copy rules of dropToTarget apply); the
  // local binding keeps the download-into-folder path.
  localPane.on.dropFolder = (folder, payload, e) => {
    if (localPane.binding.kind === 'remote') {
      dropToTarget({ kind: 'remote', source: localPane.binding.source, dir: folder.key || localPane.dir || '/' }, payload, e);
      return;
    }
    if (localPane.binding.kind === 's3') {
      // bucket rows accept drops as "into the bucket root"; folder rows
      // inside a bucket as "into that prefix".
      if (folder.isBucket) {
        dropToTarget({ kind: 's3', source: localPane.binding.source, bucket: folder.key, dir: '' }, payload, e);
        return;
      }
      if (localPane.bucket) {
        dropToTarget({ kind: 's3', source: localPane.binding.source, bucket: localPane.bucket, dir: folder.key || localPane.dir || '' }, payload, e);
      }
      return;
    }
    dropToLocal(payload, folder.path);
  };
  localPane.on.dropBody = (payload, e) => {
    if (localPane.binding.kind === 'remote') {
      dropToTarget({ kind: 'remote', source: localPane.binding.source, dir: localPane.dir || '/' }, payload, e);
      return;
    }
    if (localPane.binding.kind === 's3' && localPane.bucket) {
      dropToTarget({ kind: 's3', source: localPane.binding.source, bucket: localPane.bucket, dir: localPane.dir || '' }, payload, e);
      return;
    }
    dropToLocal(payload, localPane.dir);
  };
  // Enter on a file of a remote/S3-bound pane downloads it (WinSCP-style).
  localPane.on.activateRemoteFile = (_source, row) => downloadSideRows([row]);
  localPane.on.activateS3File = (_bucket, row) => downloadSideRows([row]);
  localPane.grid.on.context = (e, rows) => showLocalRowMenu(e, rows);
  localPane.grid.on.headerMenu = (e) => columnMenu(e, localPane.grid, 's3b-cols-local');
  localPane.on.contextEmpty = (e, dir) => {
    if (localPane.binding.kind === 'remote') { sideRemoteEmptyMenu(e); return; }
    if (localPane.binding.kind === 's3') { sideS3EmptyMenu(e); return; }
    const st = commandState();
    openMenu(e, [
      ['Paste', 'Ctrl+V', () => paste(), !st.canPaste],
      null,
      ['Select all', 'Ctrl+A', () => localPane.grid.selectAll()],
      ['Refresh', '', () => localPane.refresh()],
      null,
      ['Open terminal here\u2026', '', async () => {
        if (!dir) { toast('Open a folder first'); return; }
        try { await api.OpenTerminal(dir); } catch (err) { toast(`Terminal failed: ${err}`, 'error'); }
      }, !dir],
    ]);
  };
  localPane.on.compare = compareDirs;
  localPane.on.syncBase = () => {
    const loc = nav.current;
    return loc?.kind === 'objects' ? { dir: localPane.dir, prefix: loc.prefix || '' } : null;
  };
  localPane.on.syncUp = (dir) => {
    const loc = nav.current;
    if (loc?.kind !== 'objects') return;
    const prefix = localPane.remotePrefixFor(dir);
    if (prefix !== null && prefix !== (loc.prefix || '')) {
      nav.to({ kind: 'objects', bucket: loc.bucket, prefix });
    }
  };
}

// openMenu renders items in the shared #ctxmenu popup. An item is
// [label, kbd, fn, disabled?, cls?, sub?] or null for a separator; fn=null
// makes an inert row, and sub (an array of items in the same tuple shape)
// turns the row into an Explorer-style cascading flyout parent — hover or
// click reveals it, hovering (or opening) a sibling removes it. The anchor
// is a mouse event (menu at the pointer) or an element (menu below its
// rect).
function openMenu(anchor, items) {
  const menu = $('ctxmenu');
  menu.replaceChildren(...menuItems(menu, items));
  menu.classList.remove('hidden');
  const r = anchor?.getBoundingClientRect?.();
  const x = r ? r.left : anchor.clientX;
  const y = r ? r.bottom + 4 : anchor.clientY;
  // clamp by the menu's REAL width — a guessed 220px let wide menus
  // (long labels + kbd hints) overflow the right edge anyway
  menu.style.left = `${Math.max(0, Math.min(x, innerWidth - menu.offsetWidth - 8))}px`;
  menu.style.top = `${Math.max(0, Math.min(y, innerHeight - menu.offsetHeight - 10))}px`;
}

// menuItems builds the item rows of one menu (or flyout) level; scope is
// the element whose direct children these rows become, so closing a
// flyout only ever touches the levels below it.
function menuItems(scope, items) {
  const closeSubs = () => scope.querySelectorAll('.submenu').forEach((s) => s.remove());
  return items.map((it) => {
    if (!it) return el('div', { class: 'sep' });
    const [label, kbd, fn, disabled, cls, sub] = it;
    const node = el('div', {
      class: `item${disabled ? ' disabled' : ''}${cls ? ` ${cls}` : ''}${sub?.length ? ' has-sub' : ''}`,
      onclick: () => { if (disabled || !fn) return; hideContextMenu(); fn(); },
    }, el('span', { text: label }), kbd ? el('span', { class: 'kbd', text: kbd }) : null);
    if (sub?.length && !disabled) {
      const open = () => {
        closeSubs();
        const fly = el('div', { class: 'submenu' });
        fly.replaceChildren(...menuItems(fly, sub));
        node.appendChild(fly);
        if (fly.getBoundingClientRect().right > innerWidth - 8) fly.classList.add('flip');
      };
      node.addEventListener('mouseenter', open);
      node.addEventListener('click', (e) => { e.stopPropagation(); if (!node.querySelector(':scope > .submenu')) open(); });
    } else {
      node.addEventListener('mouseenter', closeSubs);
    }
    return node;
  });
}

// ============================ context menu ============================
// columnMenu: right-click menu on a grid header — the column picker as a
// check list against the COLUMNS catalog ('name' is locked on), the same
// control Settings exposes, one click closer. lsKey is the pane's
// localStorage persistence key.
function columnMenu(e, g, lsKey) {
  const cur = new Set(g.visibleCols().map((c) => c.id));
  openMenu(e, COLUMNS.map((c) => {
    const on = cur.has(c.id);
    return [
      `${on ? '\u2713 ' : ''}${t(c.labelKey)}`,
      '',
      () => {
        const next = new Set(cur);
        if (on) next.delete(c.id);
        else next.add(c.id);
        g.setColumns([...next]);
        localStorage.setItem(lsKey, g.visibleCols().map((x) => x.id).join(','));
      },
      c.id === 'name', // the identity column is always visible
    ];
  }));
}

function showContextMenu(e, rows) {
  const loc = nav.current;
  const items = [];
  const inObjects = loc.kind === 'objects';
  const sel = rows.length;

  if (loc.kind === 'buckets') {
    const b = rows[0];
    items.push(['Open', 'Enter', () => nav.to({ kind: 'objects', source: loc.source, bucket: b.key, prefix: '' })]);
    items.push([isFavorite(b.key) ? '\u2605 Remove from favorites' : '\u2606 Add to favorites', '', () => toggleFavorite(b.key)]);
    items.push(['Find in bucket\u2026', '', () => findDialog(b.key, '', openSearchResult)]);
    items.push(null);
    items.push(['Copy name', '', () => copyAsText(rows, 'name')]);
    items.push(['Copy S3 URI', '', () => copyAsText(rows, 'uri')]);
    items.push(null);
    items.push(['Admin panel\u2026', '', () => adminDialog(b.key, refreshCurrent)]);
    items.push(['Doctor\u2026', '', async () => runDoctor(b.key)]);
    items.push(['Properties', '', () => bucketProperties(b.key)]);
    items.push(null);
    items.push(['Delete bucket\u2026', '', () => deleteBucket(b.key)]);
  } else if (loc.kind === 'remote') {
    // Remote sources: engine-native operations plus the cross-source
    // transfer matrix (copy/cut/paste/download stream through TransferCross).
    if (sel === 1 && rows[0].isDir) items.push(['Open', 'Enter', () => grid.on.activate(rows[0])]);
    items.push([`Download${sel ? ` (${sel})` : ''}\u2026`, 'Ctrl+D', () => downloadSelection(), !sel]);
    items.push(null);
    items.push(['Copy', 'Ctrl+C', () => copySelection(), !sel]);
    items.push(['Cut', 'Ctrl+X', () => cutSelection(), !sel]);
    if (sel === 1 && rows[0].isDir) {
      items.push(['Paste into folder', 'Ctrl+V', () => paste(rows[0].key), !pasteReady()]);
    }
    items.push(['Copy name', '', () => copyAsText(rows, 'name'), !sel]);
    items.push(['Copy path', '', () => copyAsText(rows, 'path'), !sel]);
    // scheme://user@host[:port]/server-path, built backend-side
    items.push(['Copy URL', '', () => copyAsText(rows, 'url'), !sel]);
    items.push(null);
    items.push(['Rename', 'F2', () => renameSelection(), sel !== 1]);
    items.push(['Delete\u2026', 'Del', () => deleteSelection(), !sel]);
    items.push(null);
    items.push(['New folder', 'Ctrl+Shift+N', () => newFolder()]);
    items.push(['New file\u2026', 'Shift+F4', () => newFile()]);
    items.push(null);
    items.push(['Refresh', 'F5', () => refreshCurrent()]);
    items.push(['Properties', 'Alt+Enter', () => selectionProperties(), !sel]);
  } else {
    if (sel === 1 && rows[0].isDir) items.push(['Open', 'Enter', () => grid.on.activate(rows[0])]);
    items.push([`Download${sel ? ` (${sel})` : ''}`, 'Ctrl+D', () => downloadSelection()]);
    items.push(null);
    items.push(['Cut', 'Ctrl+X', () => cutSelection()]);
    items.push(['Copy', 'Ctrl+C', () => copySelection()]);
    items.push(['Paste', 'Ctrl+V', () => paste(), !pasteReady() || !inObjects]);
    items.push(['Copy name', '', () => copyAsText(rows, 'name'), !sel]);
    items.push(['Copy path', '', () => copyAsText(rows, 'path'), !sel]);
    items.push(['Copy S3 URI', '', () => copyAsText(rows, 'uri'), !sel]);
    // the real address (WinSCP's Copy URI): the endpoint-resolved URL of
    // the browsing source; the s3:// URI above stays as the CLI form
    items.push(['Copy URL', '', () => copyAsText(rows, 'url'), !sel]);
    items.push(null);
    items.push(['Rename', 'F2', () => renameSelection(), sel !== 1]);
    items.push(['Delete\u2026', 'Del', () => deleteSelection(), !sel]);
    items.push(null);
    items.push(['New folder', 'Ctrl+Shift+N', () => newFolder()]);
    items.push(['New file\u2026', 'Shift+F4', () => newFile()]);
    if (sel === 1 && !rows[0].isDir) {
      items.push(['Edit', '', () => editObject(rows[0])]);
    }
    if (sel && !rows.some((r) => r.isDir)) items.push(['Pre-sign URL\u2026', '', () => presign(rows)]);
    // Version-grade entries appear only where the bucket supports them:
    // guardCache carries the bucket's versioning/lock state (populated on
    // view load), so unversioned buckets show no Versions entry and
    // lock-less buckets no Object lock entry.
    const g = guardCache.get(guardKey(loc.source, loc.bucket));
    if (sel === 1 && g?.versioning === 'Enabled') {
      // Files open the object timeline; folders the Content Versions
      // window (bounded stats + per-child aggregates and controls — the
      // timeline dialog would page the whole subtree and hang on
      // "Loading…").
      items.push(['Versions\u2026', '', () => (rows[0].isDir
        ? contentVersionsDialog(loc.bucket, rows[0].key, refreshCurrent)
        : versionsDialog(loc.bucket, rows[0].key, refreshCurrent))]);
    }
    // "Delete marker(s)…" — offered while any selected row carries
    // marker(s); one row uses the singular for a file, and a multi
    // selection (objects and/or directories) opens the merged window.
    if (g?.versioning === 'Enabled' && rows.some((r) => r.mcount > 0)) {
      items.push([`${sel === 1 ? t(rows[0].isDir ? 'markw.title' : 'markw.titleOne') : t('markw.multiTitle', { n: sel })}\u2026`, '',
        () => markersDialog(loc.bucket, rows.map((r) => ({ key: r.key, isDir: r.isDir })), loc.prefix || '', refreshCurrent)]);
    }
    if (sel) items.push(['Storage class\u2026', '', () => classDialog(loc.bucket, rows, refreshCurrent)]);
    if (sel && !rows.some((r) => r.isDir) && g?.lockEnabled) items.push(['Object lock\u2026', '', () => lockDialog(loc.bucket, rows, refreshCurrent)]);
    // Find targets a folder: the single selected one (its own subtree), or
    // via the empty-area menu the folder already open. Files never offer it.
    if (sel === 1 && rows[0].isDir) items.push(['Find in this folder\u2026', 'Ctrl+Shift+F', () => findDialog(loc.bucket, rows[0].key, openSearchResult)]);
    items.push(['Properties', 'Alt+Enter', () => selectionProperties()]);
  }
  openMenu(e, items);
}

// showEmptyAreaMenu covers right-clicks on grid background (no row under the
// pointer): the folder/bucket-level commands instead of selection commands.
function showEmptyAreaMenu(e) {
  const loc = nav.current;
  const st = commandState();
  if (loc.kind === 'buckets') {
    openMenu(e, [
      ['New bucket\u2026', '', () => createBucket(), !st.canCreateBucket],
      null,
      ['Import S3 Credential\u2026', '', () => importCredsUi()],
      null,
      ['Refresh', 'F5', () => refreshCurrent()],
    ]);
    return;
  }
  if (loc.kind === 'remote') {
    openMenu(e, [
      ['Paste', 'Ctrl+V', () => paste(), !st.canPaste],
      null,
      ...uploadMenu(uploadFiles, uploadFolder),
      ['New folder', 'Ctrl+Shift+N', () => newFolder(), !st.canNewFolder],
    ['New file\u2026', 'Shift+F4', () => newFile(), !st.canNewFolder],
      null,
      ['Download all\u2026', '', () => downloadSelection(grid.rows), !grid.rows.length],
      ['Select all', 'Ctrl+A', () => grid.selectAll()],
      ['Refresh', 'F5', () => refreshCurrent()],
      ['Properties', '', () => folderProperties()],
    ]);
    return;
  }
  openMenu(e, [
    ['Paste', 'Ctrl+V', () => paste(), !st.canPaste],
    null,
    ...uploadMenu(uploadFiles, uploadFolder, !st.canUpload),
    ['New folder', 'Ctrl+Shift+N', () => newFolder(), !st.canNewFolder],
    ['New file\u2026', 'Shift+F4', () => newFile(), !st.canNewFolder],
    null,
    ['Download all\u2026', '', () => downloadSelection(grid.rows), !grid.rows.length],
    ['Find in this folder\u2026', 'Ctrl+Shift+F', () => findDialog(loc.bucket, loc.prefix || '', openSearchResult), !st.canFind],
    null,
    ['Refresh', 'F5', () => refreshCurrent()],
    ['Properties', '', () => folderProperties()],
  ]);
}

// folderProperties summarizes the current folder view (from the live grid).
async function folderProperties() {
  const loc = nav.current;
  if (loc?.kind === 'remote') {
    const rows = grid.rows;
    const files = rows.filter((r) => !r.isDir);
    const bytes = files.reduce((s, r) => s + (r.size || 0), 0);
    properties(`Properties — ${loc.source}:${loc.path || '/'}`, [
      ['Source type', sources.find((s) => s.name === loc.source)?.type || '?'],
      ['Folders', rows.length - files.length],
      ['Files', files.length],
      ['Total size', fmtBytes(bytes)],
      ...(view.filter ? [['Name filter', view.filter]] : []),
    ]);
    return;
  }
  if (loc?.kind !== 'objects') return;
  const rows = grid.rows;
  const files = rows.filter((r) => !r.isDir);
  const bytes = files.reduce((s, r) => s + (r.size || 0), 0);
  const g = await ensureGuard(loc.source, loc.bucket);
  properties(`Properties — ${loc.source || viewSource}://${loc.bucket}/${loc.prefix || ''}`, [
    ['Folders', rows.length - files.length],
    ['Files', files.length],
    ['Total size', fmtBytes(bytes)],
    ...guardRows(g),
    ...(view.filter ? [['Name filter', view.filter]] : []),
  ]);
}

// showTreeMenu gives sidebar nodes (sources, buckets and folders)
// context-menu parity with grid rows. node = {kind:'source',…} source root,
// {kind:'source', bucket, …} bucket-scoped S3 source (the node IS the
// bucket), {bucket, prefix, label} bucket/folder (legacy account-wide
// sources), or {kind:'rdir', source, path, label} remote directory — from
// the Tree.
function showTreeMenu(e, node) {
  const st = commandState();
  if (node.kind === 'source' && node.bucket === undefined) {
    const src = sources.find((s) => s.name === node.source);
    if (!src) return;
    openMenu(e, [
      [node.stype === 's3' ? 'Open buckets' : 'Open root', '', () => {
        if (node.stype !== 's3') nav.to({ kind: 'remote', source: node.source, path: '' });
        else nav.to({ kind: 'buckets', source: node.source });
      }],
      null,
      ['Refresh', 'F5', () => tree.reload(node.id)],
      ['Reconnect', '', async () => {
        // A no-op save round-trip: masked secrets inherit stored values,
        // and SaveSource drops cached engines/clients (re-dial on next use).
        try {
          await api.SaveSource(src);
          tree.reload(node.id);
          tree.setStatus({ [node.source]: 'busy' });
          probeSource(src).then((ok) => tree.setStatus({ [node.source]: ok ? 'ok' : 'error' }));
          toast(`Reconnected ${node.source}`, 'ok');
        } catch (err) { toast(`${err}`, 'error'); }
      }],
      ['Test connection\u2026', '', async () => {
        // S3 sources have a dedicated probe; remote/local engines are
        // probed by listing their root through the live engine.
        if (node.stype === 's3') {
          const res = await api.TestProfile(node.source);
          toast(res.ok ? `\u2705 ${res.message}` : `\u274C ${res.message}`, res.ok ? 'ok' : 'error');
        } else {
          try {
            await api.RemoteList(node.source, '/');
            toast(`\u2705 ${node.source} reachable`, 'ok');
          } catch (err) { toast(`\u274C ${err}`, 'error'); }
        }
      }],
      null,
      ['Edit source\u2026', '', () => sourceEditor(src, afterSourceSaved)],
      ['Remove source\u2026', '', async () => {
        if (await confirm({
          title: `Remove source ${node.source}?`,
          message: 'The connection is removed from the workspace.\nStored credentials will be deleted.',
          danger: true, okLabel: 'Remove',
        })) {
          try {
            await api.RemoveSource(src.id || src.name);
            await refreshSources();
            refreshPfState();
          } catch (err) { toast(`Remove failed: ${err}`, 'error'); }
        }
      }],
    ]);
    return;
  }
  if (node.kind === 'source') {
    // Bucket-scoped S3 source (the node IS the bucket): it inherits the
    // full bucket-grade feature set (open contents, favorites, upload/
    // paste, find, admin, doctor, properties, delete bucket) on top of
    // source management.
    const src = sources.find((s) => s.name === node.source);
    if (!src) return;
    const go = () => nav.to({ kind: 'objects', source: node.source, bucket: node.bucket, prefix: '' });
    const goThen = (fn) => () => navThen(
      { kind: 'objects', source: node.source, bucket: node.bucket, prefix: '' },
      fn,
    );
    openMenu(e, [
      ['Open', '', go],
      [isFavorite(node.bucket) ? '\u2605 Remove from favorites' : '\u2606 Add to favorites', '', () => toggleFavorite(node.bucket), !st.hasProfile],
      null,
      ...uploadMenu(() => uploadTo('', node.bucket, node.source), () => uploadFolderTo('', node.bucket, node.source), !st.hasProfile),
      ['Paste here', 'Ctrl+V', () => paste('', node.bucket, { kind: 's3', source: node.source, bucket: node.bucket, dir: '' }), !(st.hasProfile && pasteReady())],
      null,
      ['Find in bucket\u2026', 'Ctrl+Shift+F', goThen(() => findDialog(node.bucket, '', openSearchResult)), !st.hasProfile],
      ['Admin panel\u2026', '', goThen(() => adminDialog(node.bucket, refreshCurrent)), !st.hasProfile],
      ['Doctor\u2026', '', goThen(() => runDoctor(node.bucket)), !st.hasProfile],
      ['Properties', '', goThen(() => bucketProperties(node.bucket)), !st.hasProfile],
      null,
      ['Delete bucket\u2026', '', goThen(() => deleteBucket(node.bucket)), !st.hasProfile],
      null,
      ['Refresh', 'F5', () => tree.reload(node.id)],
      ['Reconnect', '', async () => {
        try {
          await api.SaveSource(src);
          tree.reload(node.id);
          tree.setStatus({ [node.source]: 'busy' });
          probeSource(src).then((ok) => tree.setStatus({ [node.source]: ok ? 'ok' : 'error' }));
          toast(`Reconnected ${node.source}`, 'ok');
        } catch (err) { toast(`${err}`, 'error'); }
      }],
      ['Test connection\u2026', '', async () => {
        // bucket-scoped: the backend probes THIS bucket, not the account
        const res = await api.TestSource(src.id || node.source);
        toast(res.ok ? `\u2705 ${res.message}` : `\u274C ${res.message}`, res.ok ? 'ok' : 'error');
      }],
      null,
      ['Edit source\u2026', '', () => sourceEditor(src, afterSourceSaved)],
      ['Remove source\u2026', '', async () => {
        if (await confirm({
          title: `Remove source ${node.source}?`,
          message: 'The connection is removed from the workspace.\nStored credentials will be deleted.',
          danger: true, okLabel: 'Remove',
        })) {
          try {
            await api.RemoveSource(src.id || src.name);
            await refreshSources();
            refreshPfState();
          } catch (err) { toast(`Remove failed: ${err}`, 'error'); }
        }
      }],
    ]);
    return;
  }
  if (node.kind === 'rdir') {
    openMenu(e, [
      ['Open', '', () => nav.to({ kind: 'remote', source: node.source, path: node.path })],
      null,
      ...uploadMenu(
        async () => { const paths = await api.PickUploadFiles(); if (paths?.length) uploadToRemote(paths, node.source, node.path); },
        async () => { const dir = await api.PickFolder('Choose a folder to upload'); if (dir) uploadToRemote([dir], node.source, node.path); },
      ),
      ['Download\u2026', '', async () => {
        const dest = await api.PickFolder('Choose download folder');
        if (dest) startTransfer({
          items: [{ source: node.source, key: node.path, isDir: true }],
          dest: { kind: 'local', dir: dest }, label: dest,
        });
      }],
      null,
      ['Copy', 'Ctrl+C', () => {
        Object.assign(clipboard, { mode: 'copy', kind: 'remote', bucket: null, source: node.source, dir: parentRemoteDir(node.path), keys: [node.path], paths: [] });
        toast(`Copied ${node.label}`); updateCommandState();
      }],
      ['Cut', 'Ctrl+X', () => {
        Object.assign(clipboard, { mode: 'cut', kind: 'remote', bucket: null, source: node.source, dir: parentRemoteDir(node.path), keys: [node.path], paths: [] });
        toast(`Cut ${node.label}`); updateCommandState();
      }],
      ['Paste into folder', 'Ctrl+V', () => paste(node.path), !pasteReady()],
      null,
      ['New folder here\u2026', 'Ctrl+Shift+N', () => newRemoteFolderIn(node.source, node.path)],
      null,
      ['Rename\u2026', 'F2', () => renameRemoteTreeFolder(node)],
      ['Delete\u2026', 'Del', async () => { if (await deleteRemoteSelection(node.source, [node.path])) tree.reloadParentOf(node); }],
    ]);
    return;
  }
  // S3 bucket/folder nodes: engine-native dialogs address the view source,
  // so acting on another source's bucket opens it in the main view first
  // (navThen waits for the view source to settle).
  const go = () => nav.to({ kind: 'objects', source: node.source, bucket: node.bucket, prefix: node.prefix });
  const goThen = (fn) => () => navThen(
    { kind: 'objects', source: node.source, bucket: node.bucket, prefix: node.prefix },
    fn,
  );
  if (node.prefix === '') {
    openMenu(e, [
      ['Open', '', go],
      [isFavorite(node.bucket) ? '\u2605 Remove from favorites' : '\u2606 Add to favorites', '', () => toggleFavorite(node.bucket), !st.hasProfile],
      null,
      ...uploadMenu(() => uploadTo('', node.bucket, node.source), () => uploadFolderTo('', node.bucket, node.source), !st.hasProfile),
      ['Paste here', 'Ctrl+V', () => paste('', node.bucket, { kind: 's3', source: node.source, bucket: node.bucket, dir: '' }), !(st.hasProfile && pasteReady())],
      null,
      ['Find in bucket\u2026', 'Ctrl+Shift+F', goThen(() => findDialog(node.bucket, '', openSearchResult)), !st.hasProfile],
      ['Admin panel\u2026', '', goThen(() => adminDialog(node.bucket, refreshCurrent)), !st.hasProfile],
      ['Doctor\u2026', '', goThen(() => runDoctor(node.bucket)), !st.hasProfile],
      ['Properties', '', goThen(() => bucketProperties(node.bucket)), !st.hasProfile],
      null,
      ['Delete bucket\u2026', '', goThen(() => deleteBucket(node.bucket)), !st.hasProfile],
    ]);
    return;
  }
  const clipCopy = () => { Object.assign(clipboard, { mode: 'copy', kind: 's3', bucket: node.bucket, source: node.source, dir: node.prefix.replace(/\/+$/, ''), keys: [node.prefix], paths: [] }); toast(`Copied ${node.label}`); updateCommandState(); };
  const clipCut = () => { Object.assign(clipboard, { mode: 'cut', kind: 's3', bucket: node.bucket, source: node.source, dir: node.prefix.replace(/\/+$/, ''), keys: [node.prefix], paths: [] }); toast(`Cut ${node.label}`); updateCommandState(); };
  openMenu(e, [
    ['Open', '', go],
    null,
    ...uploadMenu(() => uploadTo(node.prefix, node.bucket, node.source), () => uploadFolderTo(node.prefix, node.bucket, node.source), !st.hasProfile),
    ['Download\u2026', '', () => downloadTreeEntry(node), !st.hasProfile],
    null,
    ['Copy', 'Ctrl+C', clipCopy, !st.hasProfile],
    ['Cut', 'Ctrl+X', clipCut, !st.hasProfile],
    ['Paste into folder', 'Ctrl+V', () => paste(node.prefix, node.bucket, { kind: 's3', source: node.source, bucket: node.bucket, dir: node.prefix }), !(st.hasProfile && pasteReady())],
    null,
    ['Rename\u2026', 'F2', goThen(() => renameTreeFolder(node)), !st.hasProfile],
    // Delete is source-pinned (no view-source dialog), so no goThen nav:
    // refresh the deleted folder's PARENT in the tree — the sidebar only
    // self-updates when the main view lists a folder, and navigating into
    // the deleted folder (the old goThen target) left the parent's stale
    // row behind (and the view inside a deleted prefix).
    ['Delete\u2026', 'Del', async () => { if (await deleteSelection(node.bucket, [node.prefix], node.source)) tree.reloadParentOf(node); }, !st.hasProfile],
    null,
    ['Find here\u2026', 'Ctrl+Shift+F', goThen(() => findDialog(node.bucket, node.prefix, openSearchResult)), !st.hasProfile],
    ['Properties', '', goThen(() => treeProperties(node)), !st.hasProfile],
  ]);
}

// uploadTo/uploadFolderTo push picked files or one directory into a
// bucket/prefix of any S3 source (tree context menus' Upload menu).
async function uploadTo(prefix, bucket, source) {
  const paths = await api.PickUploadFiles();
  if (paths?.length) uploadPaths(paths, prefix, bucket, source);
}
async function uploadFolderTo(prefix, bucket, source) {
  const dir = await api.PickFolder('Choose a folder to upload');
  if (dir) uploadPaths([dir], prefix, bucket, source);
}

async function downloadTreeEntry(node) {
  const dest = await api.PickFolder('Choose download folder');
  if (!dest) return;
  // DownloadRefs addresses the view source — open the bucket first when
  // it belongs to another source.
  const run = () => downloadRefs([{ key: node.prefix, size: 0, isDir: true }], dest, node.bucket);
  if (node.source && node.source !== viewSource) {
    navThen({ kind: 'objects', source: node.source, bucket: node.bucket, prefix: node.prefix }, run);
  } else run();
}

async function renameTreeFolder(node) {
  const name = await prompt({ title: 'Rename', label: 'New name', value: node.label });
  if (!name || name === node.label) return;
  try {
    await api.SourceRenameObject(node.source, node.bucket, node.prefix, name);
    toast('Renamed', 'ok');
    tree.reload(node.id);
  } catch (err) {
    toast(`Rename failed: ${err}`, 'error');
  }
}

// Remote-source tree nodes: the same native ops as the grid's remote rows.
async function newRemoteFolderIn(source, path) {
  const name = await prompt({ title: 'New folder', label: 'Folder name', value: 'new-folder' });
  if (!name) return;
  try {
    await api.RemoteMkdir(source, remoteChildPath(path, name));
    toast('Folder created', 'ok');
    if (nav.current?.kind === 'remote' && nav.current.source === source) refreshCurrent();
  } catch (err) {
    toast(`Create folder failed: ${err}`, 'error');
  }
}

async function renameRemoteTreeFolder(node) {
  const name = await prompt({ title: 'Rename', label: 'New name', value: node.label });
  if (!name || name === node.label) return;
  try {
    await api.RemoteRename(node.source, node.path, name);
    toast('Renamed', 'ok');
    if (nav.current?.kind === 'remote' && nav.current.source === node.source) refreshCurrent();
  } catch (err) {
    toast(`Rename failed: ${err}`, 'error');
  }
}

async function treeProperties(node) {
  try {
    const [st, g] = await Promise.all([
      api.SourceStatObject(node.source, node.bucket, node.prefix),
      ensureGuard(node.source, node.bucket),
    ]);
    properties(`Properties — ${node.label}`, [
      ['Type', 'Folder'],
      ['Objects', st.usage.objectCount],
      ['Total size', fmtBytes(st.usage.totalBytes)],
      ...guardRows(g),
      ['Path', `${node.source}://${node.bucket}/${node.prefix}`],
    ]);
  } catch (err) {
    toast(`Properties failed: ${err}`, 'error');
  }
}

function hideContextMenu() { $('ctxmenu').classList.add('hidden'); }
// capture phase: grid checkboxes and the V/M badges stop mousedown
// propagation, and a bubble-phase document listener never saw those —
// the menu stayed open when the user clicked exactly them. Capture runs
// before any component handler, so no stopper can defeat the close.
document.addEventListener('mousedown', (e) => {
  if (!e.target.closest('#ctxmenu')) hideContextMenu();
}, { capture: true });
window.addEventListener('blur', hideContextMenu);

// ============================ actions ============================
// uploadPaths pushes local paths into an S3 bucket/prefix. The view source
// keeps the native Upload fast path; any other source streams through
// TransferCross (source-pinned).
async function uploadPaths(paths, prefixOverride, bucketOverride, sourceOverride) {
  const loc = nav.current;
  const bucket = bucketOverride || loc?.bucket;
  const source = sourceOverride || loc?.source || viewSource;
  if (!bucket || (!bucketOverride && loc.kind !== 'objects')) { toast('Open a bucket first'); return; }
  const prefix = prefixOverride !== undefined ? prefixOverride : (loc.prefix || '');
  if (source && source !== viewSource) {
    await startTransfer({ localPaths: paths, dest: { kind: 's3', source, bucket, dir: prefix } });
    return;
  }
  const res = await resolveTransferOpts('upload', `${source || 's3'}://${bucket}/${prefix || ''}`,
    () => api.CheckConflicts(null, paths, { kind: 's3', source: source || '', bucket, dir: prefix }));
  if (!res) return;
  try {
    await api.Upload(paths, bucket, prefix, res.policy, res.maxBps, res.decisions || null);
    toast(`Uploading ${paths.length} item(s)\u2026`);
    showTransfersBadge();
  } catch (err) {
    toast(`Upload failed: ${err}`, 'error');
  }
}

// uploadFiles and uploadFolder are the two leaves of the Upload menu —
// the proven v1.0.0 pair of native pickers (multi-select files, one
// directory), restored after the beta.3 one-dialog mixed picker proved
// unable to select files on Windows. Destination follows the active pane:
// a remote view uploads into its directory, an objects view into the
// bucket prefix; the backend walks directories either way.
async function uploadFiles() {
  const loc = nav.current;
  const paths = await api.PickUploadFiles();
  if (!paths?.length) return;
  if (loc?.kind === 'remote') return uploadToRemote(paths, loc.source, loc.path || '/');
  if (loc?.kind === 'objects') return uploadPaths(paths, loc.prefix || '', loc.bucket, loc.source);
  toast('Open a bucket or folder first');
}

async function uploadFolder() {
  const loc = nav.current;
  const dir = await api.PickFolder('Choose a folder to upload');
  if (!dir) return;
  if (loc?.kind === 'remote') return uploadToRemote([dir], loc.source, loc.path || '/');
  if (loc?.kind === 'objects') return uploadPaths([dir], loc.prefix || '', loc.bucket, loc.source);
  toast('Open a bucket or folder first');
}

// uploadChoices are the two upload leaves — Files… (Ctrl+U) and Folder… —
// shared by every surface that offers uploads. The toolbar and the
// empty-folder card open them directly; context menus embed them under one
// cascading "Upload ▸" row via uploadMenu. disabled applies to both leaves.
const uploadChoices = (filesFn, folderFn, disabled = false) => ([
  ['Files\u2026', 'Ctrl+U', filesFn, disabled],
  ['Folder\u2026', '', folderFn, disabled],
]);
const uploadMenu = (filesFn, folderFn, disabled = false) => ([
  ['Upload', '', null, false, '', disabled ? null : uploadChoices(filesFn, folderFn)],
]);

async function downloadSelection(overrideRows) {
  const loc = nav.current;
  if (loc.kind !== 'objects' && loc.kind !== 'remote') return;
  const rows = overrideRows || grid.selectedRows();
  if (!rows.length) { toast('Select items to download'); return; }
  const dest = await api.PickFolder('Choose download folder');
  if (!dest) return;
  if (loc.kind === 'remote') {
    await startTransfer({
      items: rows.map((r) => ({ source: loc.source, key: r.key, size: r.size || 0, isDir: !!r.isDir })),
      dest: { kind: 'local', dir: dest },
      label: dest,
    });
    return;
  }
  const refs = rows.map((r) => ({ key: r.key, size: r.size || 0, isDir: !!r.isDir }));
  await downloadRefs(refs, dest);
}

// downloadRefs downloads mixed file/folder refs into dest — folders expand
// recursively on the backend (dual-pane drops, Ctrl+D with folders selected).
async function downloadRefs(entries, dest, bucketOverride) {
  const loc = nav.current;
  const bucket = bucketOverride || loc.bucket;
  if (!entries?.length || !bucket) return;
  const res = await resolveTransferOpts('download', dest,
    () => api.CheckConflicts(entries.map((e) => ({ source: '', bucket, key: e.key, size: e.size || 0, isDir: !!e.isDir })), null, { kind: 'local', dir: dest }));
  if (!res) return;
  try {
    await api.DownloadRefs(bucket, entries, dest, res.policy, res.maxBps, res.decisions || null);
    toast(`Downloading ${entries.length} item(s)\u2026`);
    showTransfersBadge();
  } catch (err) {
    toast(`Download failed: ${err}`, 'error');
  }
}

// ====================== cross-source transfers ======================
// The TransferCross matrix: any read side (S3, a remote source, local-pane
// paths) into any write side (S3 bucket/prefix, remote dir, local folder).
// S3→S3 keeps the synchronous CopySelection path (server-side copies); every
// other combination goes through the background streaming job.

// xferDestOf derives the destination of the current view, or null.
function xferDestOf(loc) {
  if (loc?.kind === 'objects') return { kind: 's3', source: loc.source || viewSource, bucket: loc.bucket, dir: loc.prefix || '' };
  if (loc?.kind === 'remote') return { kind: 'remote', source: loc.source, dir: loc.path || '/' };
  return null;
}

function xferDestLabel(dest) {
  if (dest.kind === 's3') return `${dest.source || viewSource}://${dest.bucket}/${dest.dir || ''}`;
  if (dest.kind === 'remote') return `${dest.source}://${dest.dir || '/'}`;
  return dest.dir;
}

// sidePaneDest is the side pane as a transfer destination (remote-bound →
// its source dir; S3-bound → its bucket prefix; local binding → its folder;
// null at filesystem roots / buckets view / hidden pane).
function sidePaneDest() {
  if (!localPane.visible) return null;
  if (localPane.binding.kind === 'remote') return { kind: 'remote', source: localPane.binding.source, dir: localPane.dir || '/' };
  if (localPane.binding.kind === 's3') {
    return localPane.bucket
      ? { kind: 's3', source: localPane.binding.source, bucket: localPane.bucket, dir: localPane.dir || '' }
      : null;
  }
  return localPane.dir ? { kind: 'local', dir: localPane.dir } : null;
}

// startTransfer runs one background TransferCross job; resolves false when
// the user canceled the conflict dialog or the job failed to start.
async function startTransfer({ items = [], localPaths = [], dest, move = false, label } = {}) {
  const res = await resolveTransferOpts('transfer', label || xferDestLabel(dest),
    () => api.CheckConflicts(items, localPaths, dest));
  if (!res) return false;
  try {
    await api.TransferCross(items, localPaths, dest, res.policy, res.maxBps, move, res.decisions || null, false);
    toast(`${move ? 'Moving' : 'Copying'} ${items.length + localPaths.length} item(s)\u2026`);
    showTransfersBadge();
    return true;
  } catch (err) {
    toast(`Transfer failed: ${err}`, 'error');
    return false;
  }
}

// clipboardToXfer builds { items, localPaths } from the current clipboard.
// Sizes are unknown at paste time (only keys are kept); the planner re-reads
// them — size feeds just the progress totals.
function clipboardToXfer() {
  if (clipboard.kind === 'local') return { items: [], localPaths: clipboard.paths };
  if (clipboard.kind === 'remote') {
    return {
      items: clipboard.keys.map((k) => ({ source: clipboard.source, key: k, isDir: k.endsWith('/') })),
      localPaths: [],
    };
  }
  return {
    items: clipboard.keys.map((k) => ({
      source: clipboard.source || '', // "" = the view source
      bucket: clipboard.bucket,
      key: k,
      isDir: k.endsWith('/'),
    })),
    localPaths: [],
  };
}

// uploadToRemote pushes picked local files/folders into a remote source.
async function uploadToRemote(paths, source, dir) {
  await startTransfer({ localPaths: paths, dest: { kind: 'remote', source, dir }, label: `${source}:${dir}` });
}

// parentRemoteDir: the anchored parent of a remote path ("/a/b" → "/a",
// "/b" → "/") — the origin dir recorded when tree nodes feed the clipboard.
function parentRemoteDir(p) {
  const t = (p || '/').replace(/\/+$/, '');
  const i = t.lastIndexOf('/');
  return i <= 0 ? '/' : t.slice(0, i + 1);
}

// ---- unified Delete Window (all sources) ----
// The confirmation itself lives in dialogs.js now: deleteWindow is the
// base template every destructive confirmation is built on, and
// runDeleteWindow routes every delete through it while honoring the
// Settings → Delete preferences (window / typed word / auto-confirm —
// see the deleteWindow doc comment there).

// S3_DEL_MODES lists a versioned bucket's delete types (the window's radio
// list). The marker delete is the safe default; keep-current and permanent
// destroy history (ladder L3) — the window's explicit choice and summary
// are their guard.
const S3_DEL_MODES = [
  { id: '', label: 'delm.marker', hint: 'delm.markerHint' },
  { id: 'keepcurrent', label: 'delm.keep', hint: 'delm.keepHint' },
  { id: 'permanent', label: 'delm.perm', hint: 'delm.permHint' },
];

// reportDeleteResult toasts a DeleteResult ({deleted, errors}).
function reportDeleteResult(res, okMsg) {
  if (!res) return;
  if (res.errors?.length) toast(`${res.deleted} deleted, errors: ${res.errors.slice(0, 3).join('; ')}`, 'error');
  else toast(okMsg(res.deleted ?? 0), 'ok');
}

// deleteS3Keys runs the full S3 delete flow — preview, the Delete Window
// with the bucket's delete-type list, then the matching API — for one
// bucket. source '' addresses the view source; after() runs on success
// (the main view refreshes, the side pane re-lists). preset forces the
// window open at that mode (Shift+Del → 'permanent').
async function deleteS3Keys(source, bucket, keys, preset, target, after) {
  const p = source
    ? await api.SourcePreviewDelete(source, bucket, keys)
    : await api.PreviewDelete(bucket, keys);
  // Versioned buckets offer every delete type in the window; elsewhere a
  // marker would just be noise without a timeline to hold it.
  const g = await ensureGuard(source, bucket).catch(() => null);
  const versioned = g?.versioning === 'Enabled';
  const modes = versioned ? S3_DEL_MODES : [];
  const desc = `${p.count} object(s)${p.bytes ? ` (${fmtBytes(p.bytes)})` : ''}${p.folders ? ` in ${p.folders} folder(s)` : ''}`;
  const undoNote = versioned
    ? 'Objects stay in version history and can be restored.'
    : 'This cannot be undone.';
  const mode = await runDeleteWindow({
    target,
    summary: { objects: p.objects, folders: p.folders, bytes: p.bytes, requiresL2: p.requiresL2 },
    modes,
    mode: preset,
    classicMsg: `You are about to delete ${desc}.\n${undoNote}`,
  });
  if (mode === null) return false;
  // force contract: the plain mode's backend gate re-counts the same
  // objects this preview counted, so requiresL2 maps 1:1 onto it. The
  // versioned destructive modes (keepcurrent/permanent) destroy VERSIONS —
  // hundreds can hide behind a handful of current objects, which the
  // object-count preview can never see — so requiresL2 would leave them
  // under the backend's version threshold and the delete would ALWAYS fail
  // with "N version(s) selected — typed confirmation (force) required"
  // (typed word included). Their window never auto-confirms and never
  // skips the explicit destructive-mode choice (plus the typed word
  // whenever the Require-typing setting is on): the confirmed window IS
  // the force contract, same as the purge/empty-bucket flows.
  const force = mode !== '' || p.requiresL2;
  let res;
  if (mode === 'permanent') {
    res = source
      ? await api.SourceDeleteSelectionPermanent(source, bucket, keys, force)
      : await api.DeleteSelectionPermanent(bucket, keys, force);
    reportDeleteResult(res, (n) => `Destroyed ${n} version(s)/marker(s) permanently`);
  } else if (mode === 'keepcurrent') {
    res = source
      ? await api.SourceDeleteSelectionKeepCurrent(source, bucket, keys, force)
      : await api.DeleteSelectionKeepCurrent(bucket, keys, force);
    reportDeleteResult(res, (n) => `Deleted the history of ${n} item(s) — current versions kept`);
  } else {
    res = source
      ? await api.SourceDeleteSelection(source, bucket, keys, force)
      : await api.DeleteSelection(bucket, keys, force);
    reportDeleteResult(res, (n) => `Deleted ${n} object(s)${versioned ? ' — restorable from version history' : ''}`);
  }
  after?.();
  return true;
}

async function deleteSelection(bucketOverride, keysOverride, sourceOverride, presetMode = '') {
  const loc = nav.current;
  if (!bucketOverride && loc.kind === 'remote') {
    return deleteRemoteSelection(null, null, presetMode);
  }
  if (!bucketOverride && loc.kind === 'buckets') {
    const row = grid.selectedRows()[0];
    if (row) deleteBucket(row.key);
    return;
  }
  const bucket = bucketOverride || loc.bucket;
  if (!bucket || (!bucketOverride && loc.kind !== 'objects')) return;
  const keys = keysOverride || grid.selectedRows().map((r) => r.key);
  if (!keys.length) return;
  const source = sourceOverride ?? (loc.kind === 'objects' ? loc.source : '');
  const scheme = source ? `${source}://` : 's3://';
  const target = keys.length === 1
    ? `s3://${bucket}/${keys[0]}`
    : `${scheme}${bucket}/${loc?.prefix || ''}`;
  try {
    // resolves true when the deletion ran (false = canceled window) so
    // sidebar callers know to refresh the tree's parent node
    return await deleteS3Keys(source, bucket, keys, presetMode, target, refreshCurrent);
  } catch (err) {
    toast(`Delete failed: ${err}`, 'error');
    return false;
  }
}

async function editObject(row) {
  const loc = nav.current;
  try {
    // Settings → Editing: ask which app edits the file via the OS
    // "Open with" chooser (default on; off = OS default app).
    const chooseApp = localStorage.getItem('s3b-edit-choose-app') !== '0';
    await api.EditObject(loc.bucket, row.key, chooseApp);
    toast(`Opening ${row.name} — saves upload automatically`, 'ok');
    updateEditingStatus();
  } catch (err) {
    toast(`Edit failed: ${err}`, 'error');
  }
}

// Shift+Del: destroy the selection including all versions and delete markers
// (safety ladder L3) — the Delete Window opens preset to "permanent", whose
// typed partition is always forced, so directory rows purge EVERYTHING
// beneath them, not just the folder marker key.
async function deletePermanentSelection() {
  const loc = nav.current;
  if (loc.kind !== 'objects') return;
  const keys = grid.selectedRows().map((r) => r.key);
  if (!keys.length) return;
  await deleteSelection(loc.bucket, keys, loc.source, 'permanent');
}

// sidePaneRef is the side pane's compare reference: a remote-bound pane
// compares its source dir, an S3-bound pane its bucket prefix, the local
// binding its folder (null at roots / buckets view).
function sidePaneRef() {
  if (localPane.binding.kind === 'remote') return { kind: 'remote', source: localPane.binding.source, dir: localPane.dir || '/' };
  if (localPane.binding.kind === 's3') {
    return localPane.bucket
      ? { kind: 's3', source: localPane.binding.source, bucket: localPane.bucket, prefix: localPane.dir || '' }
      : null;
  }
  return localPane.dir ? { kind: 'local', dir: localPane.dir } : null;
}

// mainCompareRef is the main view's compare reference (bucket folder or
// remote dir), null elsewhere.
function mainCompareRef() {
  const loc = nav.current;
  if (loc?.kind === 'objects') return { kind: 's3', bucket: loc.bucket, prefix: loc.prefix || '' };
  if (loc?.kind === 'remote') return { kind: 'remote', source: loc.source, dir: loc.path || '/' };
  return null;
}

function cmpRefLabel(ref) {
  if (ref.kind === 's3') return `s3://${ref.bucket}/${ref.prefix || ''}`;
  if (ref.kind === 'remote') return `${ref.source}:${ref.dir}`;
  return ref.dir;
}

// compareDirs compares the side pane against the main view (local ↔ S3,
// remote ↔ remote, local ↔ remote, …) and decorates both grids
// (WinSCP-style keep-in-sync).
async function compareDirs() {
  const x = sidePaneRef();
  const y = mainCompareRef();
  if (!x) {
    toast(localPane.binding.kind === 'local'
      ? 'Side pane is at filesystem roots — open a folder first'
      : 'Open a folder (or bucket folder) on the source first');
    return;
  }
  if (!y) { toast('Open a bucket folder or remote directory to compare against'); return; }
  try {
    const rows = await api.CompareAny(x, y);
    localPane.setCompare(rows);
    grid.setCmp(aggregateCompare(rows));
    const n = (s) => rows.filter((r) => r.status === s).length;
    const xl = cmpRefLabel(x), yl = cmpRefLabel(y);
    properties(`Compare — ${xl} \u2194 ${yl}`, [
      ['Identical', n('same')],
      [`Only on left (${xl})`, n('only-local')],
      [`Only on right (${yl})`, n('only-remote')],
      ['Newer on left', n('newer-local')],
      ['Newer on right', n('newer-remote')],
      ['Different size', n('size-diff')],
    ]);
  } catch (err) {
    toast(`Compare failed: ${err}`, 'error');
  }
}

// updateEditingStatus refreshes the status-bar editor indicator.
function updateEditingStatus() {
  api.EditingFiles().then((files) => {
    const sp = $('status-editing');
    sp.classList.toggle('hidden', !files.length);
    sp.textContent = `\u270E ${files.length} in editor`;
  }).catch(() => {});
}

function togglePanes() { setPanes(!localPane.visible); }

function setPanes(on) {
  localStorage.setItem('s3b-panes', on ? '1' : '0');
  if (on) localPane.show();
  else localPane.hide();
}

async function renameSelection() {
  const loc = nav.current;
  const row = grid.selectedRows()[0];
  if (!row) return;
  const name = await prompt({ title: 'Rename', label: 'New name', value: row.name });
  if (!name || name === row.name) return;
  try {
    if (loc.kind === 'remote') await api.RemoteRename(loc.source, row.key, name);
    else await api.RenameObject(loc.bucket, row.key, name);
    toast('Renamed', 'ok');
    refreshCurrent();
  } catch (err) {
    toast(`Rename failed: ${err}`, 'error');
  }
}

// remoteChildPath builds the anchored path of a new child in dir (dir is
// the location path: '' or '/sub/', both with/without trailing content).
function remoteChildPath(dir, name) {
  return `${dir && dir.endsWith('/') ? dir : `${dir}/`}${name}`;
}

async function newFolder() {
  const loc = nav.current;
  if (loc.kind === 'remote') {
    const name = await prompt({ title: 'New folder', label: 'Folder name', value: 'new-folder' });
    if (!name) return;
    try {
      await api.RemoteMkdir(loc.source, remoteChildPath(loc.path || '', name));
      toast('Folder created', 'ok');
      refreshCurrent();
    } catch (err) {
      toast(`Create folder failed: ${err}`, 'error');
    }
    return;
  }
  if (loc.kind !== 'objects') { toast('Open a bucket first'); return; }
  const name = await prompt({ title: 'New folder', label: 'Folder name', value: 'new-folder' });
  if (!name) return;
  try {
    await api.CreateFolder(loc.bucket, loc.prefix || '', name);
    toast('Folder created', 'ok');
    refreshCurrent();
  } catch (err) {
    toast(`Create folder failed: ${err}`, 'error');
  }
}

// newFile is the WinSCP-style New file: name + type dialog, the empty
// object is created FIRST, then the editor handoff is best-effort —
// cancelling the app picker (or having no app) must still leave the
// created empty file behind.
async function newFile() {
  const loc = nav.current;
  if (loc.kind === 'remote') {
    const r = await promptFile({ title: 'New file', dir: loc.path || '' });
    if (!r) return;
    try {
      const p = await api.RemoteCreateFile(loc.source, loc.path || '', r.name, r.ext);
      toast(`Created ${p}`, 'ok');
      refreshCurrent();
    } catch (err) {
      toast(`Create file failed: ${err}`, 'error');
    }
    return;
  }
  if (loc.kind !== 'objects') { toast('Open a bucket first'); return; }
  const r = await promptFile({ title: 'New file', dir: loc.prefix || '' });
  if (!r) return;
  let key = '';
  try {
    key = await api.CreateFile(loc.bucket, loc.prefix || '', r.name, r.ext);
    toast('File created', 'ok');
    refreshCurrent();
  } catch (err) {
    toast(`Create file failed: ${err}`, 'error');
    return;
  }
  try {
    const chooseApp = localStorage.getItem('s3b-edit-choose-app') !== '0';
    await api.EditObject(loc.bucket, key, chooseApp);
    toast(`Opening ${key.split('/').pop()} — saves upload automatically`, 'ok');
    updateEditingStatus();
  } catch { /* cancelled picker / no app: the empty file stays */ }
}

// deleteRemoteSelection: count-then-act delete on a remote source through
// the unified Delete Window. Remote filesystems have no trash and no
// versions — the window's warning (and the classic fallback's typed word)
// says so.
async function deleteRemoteSelection(overrideSource, overrideKeys, presetMode = '') {
  const loc = nav.current;
  const source = overrideSource || loc?.source;
  if (!source) return;
  const keys = overrideKeys || grid.selectedRows().map((r) => r.key);
  if (!keys.length) return;
  try {
    const p = await api.RemoteDeletePreview(source, keys);
    if (p.errors?.length) toast(`Warning: ${p.errors.slice(0, 2).join('; ')}`, 'error');
    const desc = `${p.files} file(s), ${p.folders} folder(s)${p.bytes ? ` (${fmtBytes(p.bytes)})` : ''}`;
    const mode = await runDeleteWindow({
      target: keys.length === 1 ? `${source}:${keys[0]}` : `${source}:${loc?.path || '/'}`,
      summary: { files: p.files, folders: p.folders, bytes: p.bytes },
      mode: presetMode,
      classicTyped: true, // no undo on remotes: classic mode types the word when the setting is on
      classicMsg: `You are about to delete ${desc}.\nRemote sources have no trash or versions — this cannot be undone.`,
    });
    if (mode === null) return false;
    const res = await api.RemoteRemove(source, keys);
    reportDeleteResult(res, (n) => `Deleted ${n} item(s)`);
    refreshCurrent();
    return true;
  } catch (err) {
    toast(`Delete failed: ${err}`, 'error');
    return false;
  }
}

async function createBucket() {
  const name = await prompt({ title: t('createBucket'), label: 'Bucket name (globally unique, DNS-safe)' });
  if (!name) return;
  try {
    await api.CreateBucket(name, '');
    toast(`Bucket ${name} created`, 'ok');
    refreshCurrent();
  } catch (err) {
    toast(`Create bucket failed: ${err}`, 'error');
  }
}

async function deleteBucket(bucket) {
  try {
    const p = await api.PreviewBucketDelete(bucket);
    // The unified Delete Window with the L2 identity check: typing the
    // bucket's own name applies regardless of the Require-typing setting.
    // force: a whole-bucket removal never auto-confirms.
    const go = await runDeleteWindow({
      target: `s3://${bucket}`,
      summary: p.requiresL2 ? { stats: [
        `${p.objectCount} object(s)`,
        ...(p.versioned ? [`${p.versionCount} version(s)`, `${p.deleteMarkers} delete marker(s)`] : []),
      ] } : { stats: ['empty bucket'] },
      warn: p.requiresL2
        ? 'Removes the bucket AND all of its contents — permanently.'
        : 'The bucket is empty; only the bucket itself is removed.',
      typedAlways: true, typedWord: bucket,
      classicMsg: p.requiresL2
        ? `The bucket holds ${p.objectCount} object(s). Deleting removes the bucket AND all of its contents.`
        : 'The bucket is empty and will be removed.',
      confirmLabel: 'Delete bucket', title: `Delete bucket s3://${bucket}`,
      force: true,
    });
    if (go === null) return;
    const res = await api.DeleteBucket(bucket, true);
    toast(`Bucket removed (${res.deleted} object(s) emptied)`, 'ok');
    nav.to({ kind: 'buckets', source: viewSource });
  } catch (err) {
    toast(`Delete bucket failed: ${err}`, 'error');
  }
}

// ====================== OS file clipboard (Explorer) ======================
// The Explorer bridge: Ctrl+C in Windows Explorer puts a CF_HDROP file list
// on the OS clipboard. Paste follows Explorer's own rule — LAST COPY WINS:
// every clipboard write by any process bumps the system sequence number
// (api.OsClipboardState), so
//   - an external copy bumps seq past our baseline → the OS payload
//     outranks the app clipboard (which never expires on its own and would
//     otherwise shadow Explorer copies for the rest of the session);
//   - every in-app copy/cut re-adopts the current seq as the baseline, and
//     our own mirrors (OsClipboardSetFiles) re-adopt after writing, so the
//     app's payload keeps precedence until someone copies elsewhere.
// Settings → File transfers can disable the whole bridge (locked-down machines).
const explorerClipOn = () => localStorage.getItem('s3b-os-clip') !== '0';
let osClipBaseline = -1; // seq as of our last own clipboard interaction
let osClipFilesReady = false; // OS clipboard holds files right now (menus)

// pasteReady: whether a paste has anything to act on — the app clipboard
// or Explorer files waiting on the OS clipboard (context-menu gating).
const pasteReady = () => clipHasItems() || osClipFilesReady;

// osClipAdopt re-reads the state and adopts the current seq as the
// baseline ("ours from here on") — at boot and around every own copy/write.
async function osClipAdopt() {
  if (!explorerClipOn()) return;
  try {
    const st = await api.OsClipboardState();
    osClipBaseline = st?.seq ?? osClipBaseline;
    osClipFilesReady = !!st?.files;
  } catch { /* binding missing — keep the previous baseline */ }
}

// refreshOsClip re-polls the files flag (window focus) WITHOUT adopting a
// possibly-external new seq — adopting here would mask the very change the
// paste precedence needs to see.
async function refreshOsClip() {
  if (!explorerClipOn()) { osClipFilesReady = false; return; }
  try {
    const st = await api.OsClipboardState();
    osClipFilesReady = !!st?.files;
    updateCommandState();
  } catch { /* leave as-is */ }
}

// osClipPayload resolves the OS clipboard's file list for a paste, or null
// when it must not win: disabled by setting, no file payload, or an
// unchanged clipboard while the app clipboard still holds a newer copy.
async function osClipPayload() {
  if (!explorerClipOn()) return null;
  let st = null;
  try { st = await api.OsClipboardState(); } catch { return null; }
  if (!st?.files) return null;
  if (clipHasItems() && osClipBaseline >= 0 && st.seq === osClipBaseline) return null;
  let paths = null;
  try { paths = await api.OsClipboardFiles(); } catch { return null; }
  return paths?.length ? paths : null;
}

// paste drops the app clipboard into a destination; with the app clipboard
// empty — or outranked by a newer Explorer copy — the OS clipboard's files
// land wherever the active view points, exactly like an in-app paste.
async function paste(prefixOverride, bucketOverride, destOverride) {
  const loc = nav.current;
  let dest;
  if (destOverride) dest = destOverride; // explicit target (side-pane/tree menus)
  else if (bucketOverride) dest = { kind: 's3', source: viewSource, bucket: bucketOverride, dir: prefixOverride !== undefined ? prefixOverride : '' };
  else if (prefixOverride !== undefined && loc?.kind === 'remote') dest = { kind: 'remote', source: loc.source, dir: prefixOverride };
  else dest = xferDestOf(loc) || sidePaneDest();

  // A winning Explorer payload is a copy and supersedes the app clipboard
  // (from here on the Explorer copy IS the clipboard, Explorer-style).
  const osPaths = await osClipPayload();
  if (osPaths) {
    if (!dest) { toast('Open a bucket or folder first'); return; }
    clipboard.keys = []; clipboard.paths = []; clipboard.mode = null;
    updateCommandState();
    if (dest.kind === 's3') uploadPaths(osPaths, dest.dir, dest.bucket, dest.source);
    else if (dest.kind === 'remote') uploadToRemote(osPaths, dest.source, dest.dir);
    else startTransfer({ localPaths: osPaths, dest, label: dest.dir });
    return;
  }
  if (!clipHasItems()) { toast('Nothing to paste'); return; }
  if (!dest) { toast('Open a bucket or folder first'); return; }

  // Same-dir paste is a no-op (would spam "(1)" renames), and pasting a
  // folder into itself / its own subtree is refused (Explorer rules).
  const normP = (s) => (s || '').replace(/[\/\\]+$/, '');
  const sameOrigin = (clipboard.kind === 's3' && dest.kind === 's3' && (clipboard.source || '') === (dest.source || '') && clipboard.bucket === dest.bucket)
    || (clipboard.kind === 'remote' && dest.kind === 'remote' && clipboard.source === dest.source)
    || (clipboard.kind === 'local' && dest.kind === 'local');
  if (sameOrigin && normP(clipboard.dir) === normP(dest.dir)) { toast('Source and destination are the same'); return; }
  if (sameOrigin && intoItself(dest.dir, clipboard.kind === 'local' ? clipboard.paths : clipboard.keys)) {
    toast('A folder cannot be moved or copied into itself', 'error');
    return;
  }

  const move = clipboard.mode === 'cut';
  // S3 → S3 within the view source keeps the synchronous server-side copy
  // path; any other source pairing streams through TransferCross (which
  // still copies server-side when both sides share a client). Both S3→S3
  // pairings offer the per-task version choice first.
  if (clipboard.kind === 's3' && dest.kind === 's3'
    && clipboard.source === dest.source && dest.source === viewSource) {
    const prefix = prefixOverride !== undefined ? prefixOverride : (loc?.prefix || '');
    const status = await copyS3Selection(
      { source: '', bucket: clipboard.bucket, keys: clipboard.keys },
      { kind: 's3', source: '', bucket: dest.bucket, dir: prefix },
      move,
      async () => {
        try {
          const res = await api.CopySelection(clipboard.bucket, clipboard.keys, dest.bucket, prefix, move);
          if (res.errors?.length) toast(`Errors: ${res.errors.slice(0, 3).join('; ')}`, 'error');
          else toast(`${move ? 'Moved' : 'Copied'} ${res.copied} item(s)`, 'ok');
          if (move) clipboard.keys = [];
          updateCommandState();
          refreshCurrent();
        } catch (err) {
          toast(`Paste failed: ${err}`, 'error');
        }
      },
    );
    // A versioned move skips the plain callback — clear the clipboard here.
    if (status === 'versioned' && move) {
      clipboard.keys = [];
      clipboard.mode = null;
      updateCommandState();
      refreshCurrent();
    }
    return;
  }
  const runXfer = async () => {
    const { items, localPaths } = clipboardToXfer();
    const started = await startTransfer({ items, localPaths, dest, move });
    if (started && move) { clipboard.keys = []; clipboard.paths = []; clipboard.mode = null; }
    updateCommandState();
  };
  if (clipboard.kind === 's3' && dest.kind === 's3') {
    const status = await copyS3Selection(
      { source: clipboard.source || '', bucket: clipboard.bucket, keys: clipboard.keys },
      dest, move, runXfer,
    );
    // A versioned move skips runXfer — clear the clipboard here.
    if (status === 'versioned' && move) {
      clipboard.keys = [];
      clipboard.paths = [];
      clipboard.mode = null;
      updateCommandState();
    }
    return;
  }
  await runXfer();
}

async function presign(rowsOverride) {
  const loc = nav.current;
  const rows = rowsOverride || grid.selectedRows();
  if (!rows.length) return;
  try {
    if (rows.length === 1) {
      const url = await api.PresignObject(loc.bucket, rows[0].key, 3600);
      presignDialog(url);
      return;
    }
    const list = [];
    for (const r of rows) list.push({ name: r.name, url: await api.PresignObject(loc.bucket, r.key, 3600) });
    presignListDialog(list);
  } catch (err) {
    toast(`Presign failed: ${err}`, 'error');
  }
}

async function selectionProperties() {
  const loc = nav.current;
  const rows = grid.selectedRows();
  if (!rows.length) return;
  if (rows.length > 1) { multiProperties(loc, rows); return; }
  const row = rows[0];
  if (loc.kind === 'remote') {
    try {
      const st = await api.RemoteStat(loc.source, row.key);
      properties(`Properties — ${row.name}`, [
        ['Name', row.name],
        ['Type', row.isDir ? 'Folder' : 'File'],
        ...(!row.isDir ? [
          ['Size', fmtBytes(st.size)],
          ['Last modified', fmtDate(st.lastModified)],
        ] : []),
        ['Source', `${loc.source} (${sources.find((s) => s.name === loc.source)?.type || '?'})`],
        ['Path', row.key],
      ]);
    } catch (err) {
      toast(`Properties failed: ${err}`, 'error');
    }
    return;
  }
  try {
    const st = await api.StatObject(loc.bucket, row.key);
    const rows = [
      ['Name', st.name],
      ['Type', st.isDir ? 'Folder' : 'Object'],
      ...(st.isDir ? [['Objects', st.usage.objectCount], ['Total size', fmtBytes(st.usage.totalBytes)]] : [
        ['Size', fmtBytes(st.size)],
        ['Last modified', fmtDate(st.lastModified)],
        ['ETag', st.etag],
        ['Storage class', st.storageClass],
        ['Content type', st.contentType],
        ['SSE', st.sse || 'none'],
      ]),
      ['Path', `${loc.source || viewSource}://${loc.bucket}/${row.key}`],
    ];
    // Object-lock state of the selected object (M10.4): one extra call,
    // tolerating buckets without a lock config (rows simply stay away).
    if (!row.isDir) {
      try {
        const lock = await api.GetObjectLock(loc.bucket, row.key, '');
        rows.push(
          ['Retention', lock.mode ? `${lock.mode} until ${lock.retainUntil ? fmtDate(lock.retainUntil) : '?'}` : 'none'],
          ['Legal hold', lock.legalHold || 'off'],
        );
      } catch { /* no lock config or no permission — skip */ }
    }
    properties(`Properties — ${st.name}`, rows);
  } catch (err) {
    toast(`Properties failed: ${err}`, 'error');
  }
}

// multiProperties summarizes a multi-selection: composition, total size,
// modification range and the deepest common prefix (M10.3 multi-run
// Properties — no per-item round trips).
function multiProperties(loc, rows) {
  const files = rows.filter((r) => !r.isDir);
  const folders = rows.length - files.length;
  const bytes = files.reduce((s, r) => s + (r.size || 0), 0);
  const parts = [];
  if (folders) parts.push(`${folders} folder(s)`);
  if (files.length) parts.push(`${files.length} file(s)`);

  const props = [['Items', `${rows.length} (${parts.join(', ')})`]];
  if (files.length) props.push(['Total size', fmtBytes(bytes)]);

  const ms = (v) => (typeof v === 'string' ? Date.parse(v) : v);
  const times = rows.map((r) => ms(r.lastModified || r.modTime)).filter((v) => Number.isFinite(v) && v > 0);
  if (times.length) {
    props.push(['Modified range', `${fmtDate(Math.min(...times))} — ${fmtDate(Math.max(...times))}`]);
  }

  if (loc?.kind === 'objects' || loc?.kind === 'remote') {
    const common = rows.reduce((p, r) => {
      const k = r.key || '';
      let i = 0;
      while (i < p.length && i < k.length && p[i] === k[i]) i++;
      const cut = p.slice(0, i).lastIndexOf('/');
      return cut < 0 ? '' : p.slice(0, cut + 1);
    }, rows[0].key || '');
    props.push(['Common prefix', common || '(none)']);
  }
  if (loc?.kind === 'remote') props.push(['Source', loc.source]);
  else if (loc?.kind === 'objects') props.push(['Bucket', loc.bucket]);
  properties(`Properties — ${rows.length} item(s)`, props);
}

// bucketProperties is the S3 bucket Properties dialog (Data Sources tree +
// buckets view): identity, versioning / object-lock state and a per-section
// summary of the admin panel in one view. One GetBucketAdmin round trip;
// every section degrades on its own error.
async function bucketProperties(bucket) {
  try {
    const [st, panel] = await Promise.all([
      api.StatBucket(bucket).catch(() => null),
      api.GetBucketAdmin(bucket).catch(() => null),
    ]);
    const def = sources.find((s) => s.name === viewSource)
      || sources.find((s) => s.type === 's3');
    const gk = guardKey(viewSource, bucket);
    // keep the tree icons in sync with what the panel just told us
    if (panel) guardCache.set(gk, {
      versioning: panel.versions || '',
      lockEnabled: !!panel.lock?.enabled,
      lockMode: panel.lock?.mode || '',
      lockDays: panel.lock?.days || 0,
    });
    const g = guardCache.get(gk) || {};
    const pabOn = panel?.pab
      ? ['blockPublicAcls', 'ignorePublicAcls', 'blockPublicPolicy', 'restrictPublicBuckets']
        .filter((k) => panel.pab[k]).length
      : null;
    properties(`Properties — ${bucket}`, [
      ['Name', bucket],
      ['Provider', def ? providerLabel(def.s3?.endpoint) : '—'],
      ['Region', st?.region || panel?.region || '—'],
      ['Path', `${viewSource}://${bucket}`],
      ...guardRows(g),
      ...(panel ? [
        ['Default encryption', panel.encryption?.algorithm
          ? `${panel.encryption.algorithm}${panel.encryption.kmsKeyId ? ` (${panel.encryption.kmsKeyId})` : ''}`
          : 'none set'],
        ['Public access', panel.publicWarning ? '\u26A0 allowed (see Admin panel)' : 'no public access'],
        ['Bucket policy', panel.policyErr ? 'unavailable'
          : panel.policy?.summary ? `${panel.policy.summary.statementCount} statement(s)` : 'none'],
        ['Public access block', panel.pabErr ? 'unavailable'
          : pabOn === 4 ? 'all on (recommended)' : `${pabOn} of 4 on`],
        ['CORS rules', panel.corsErr ? 'unavailable' : String(panel.cors?.length || 0)],
        ['Lifecycle rules', panel.lifecycleErr ? 'unavailable' : String(panel.lifecycle?.length || 0)],
        ['Bucket tags', panel.tagsErr ? 'unavailable' : String(panel.tags?.length || 0)],
        ['Static website', panel.websiteErr ? 'unavailable'
          : panel.website?.indexSuffix || panel.website?.redirectHost ? 'enabled' : 'not configured'],
      ] : []),
    ]);
  } catch (err) {
    toast(`Properties failed: ${err}`, 'error');
  }
}

function runDoctor(bucket) {
  doctorDialog(bucket || '');
}

// doctorPicker is the Help menu's front door to the Doctor: a small window
// listing every S3 source, so the user picks what to analyze. Picking a
// source switches the engine's view source to it (the doctor addresses the
// source the main view is browsing) and opens the regular doctor window —
// bucket-scoped sources carry their bucket, account-wide ones diagnose the
// endpoint itself. The right-click "Doctor…" path on a source/bucket node
// stays exactly as it was; this picker only replaces the menu entry.
function doctorPicker() {
  const s3s = sources.filter((s) => s.type === 's3');
  if (!s3s.length) {
    toast(t('doctor.pickNone'), 'error');
    return;
  }
  const rows = s3s.map((s) => el('div', {
    class: 'picker-row',
    role: 'button',
    tabindex: '0',
    onclick: () => pick(s),
    onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') pick(s); },
  },
  srcIconEl('s3', s.color),
  el('div', { class: 'picker-main' },
    el('div', { class: 'picker-name', text: s.name }),
    el('div', { class: 'picker-sub', text: s.bucket ? `s3://${s.bucket}` : 'account-wide — all buckets of this key' }),
  ),
  ));
  let modal;
  const pick = async (s) => {
    modal?.close();
    await setViewSourceFor({ source: s.name });
    runDoctor(s.bucket || '');
  };
  modal = openModal({
    title: t('doctor.pickTitle'),
    body: el('div', {},
      el('div', { class: 'picker-hint', text: t('doctor.pickHint') }),
      el('div', { class: 'picker-list' }, rows),
    ),
    buttons: [{ label: t('dlg.cancel') }],
  });
}

// ============================ drag & drop ============================
function wireDrop() {
  // OS-level drop: hit-test which pane sits under the cursor. The side
  // pane accepts when visible and inside a directory (its binding decides
  // the destination); otherwise the main view takes the drop.
  const handleOSDrop = (x, y, paths) => {
    if (!paths?.length) return;
    const hit = Number.isFinite(x) && Number.isFinite(y)
      ? document.elementFromPoint(x, y)
      : null;
    if (hit?.closest('#local-pane') && localPane.visible) {
      const b = localPane.binding;
      if (b.kind === 'remote' && localPane.dir) { uploadToRemote(paths, b.source, localPane.dir); return; }
      if (b.kind === 's3' && localPane.bucket) {
        startTransfer({ localPaths: paths, dest: { kind: 's3', source: b.source, bucket: localPane.bucket, dir: localPane.dir || '' } });
        return;
      }
      if (b.kind === 'local' && localPane.dir) {
        startTransfer({ localPaths: paths, dest: { kind: 'local', dir: localPane.dir }, label: localPane.dir });
        return;
      }
    }
    // Sidebar tree nodes are full drop targets: the node under the cursor
    // decides the destination (a bucket root or folder, a remote
    // directory, a non-S3 source's root) — never the accidentally-open
    // main view. Account-wide S3 roots still need a bucket picked.
    const tnode = hit?.closest('#tree .tnode');
    if (tnode) {
      const d = tnode.dataset;
      if (d.rdir !== undefined) { uploadToRemote(paths, d.source, d.rdir); return; }
      if (d.bucket !== undefined) {
        startTransfer({ localPaths: paths, dest: { kind: 's3', source: d.source || '', bucket: d.bucket, dir: d.prefix || '' } });
        return;
      }
      if (d.stype && d.stype !== 's3') { uploadToRemote(paths, d.source, '/'); return; }
      toast('Open a bucket first — this source is account-wide');
      return;
    }
    const loc = nav.current;
    if (loc?.kind === 'remote') { uploadToRemote(paths, loc.source, loc.path || '/'); return; }
    uploadPaths(paths);
  };
  // The drop arrives as three positional arguments (x, y, paths). The
  // desktop app (Wails v3) arms its own drop listeners when the runtime
  // script loads — they preventDefault external file drags so the
  // webview never navigates to the dropped file — and Go re-emits the
  // resolved paths as the wails:file-drop event, which js/bridge.js
  // spreads back into positional arguments. The test shims keep the
  // v2-style runtime.OnFileDrop surface and route it to the same
  // handler; nothing here relies on drop-target CSS (the app does its
  // own hit-testing).
  if (typeof window.runtime?.OnFileDrop === 'function') {
    window.runtime.OnFileDrop(handleOSDrop, false);
  } else {
    onEvent('wails:file-drop', (x, y, paths) => handleOSDrop(x, y, paths));
  }
  // drag:self-drop — a native drag-out released back over the app routes
  // exactly like the internal HTML5 drop it replaces: same payload (the
  // current selection, origin-tagged), same dropToTarget/dropToLocal
  // funnel, same Shift/Ctrl modifier semantics captured at release.
  onEvent('drag:self-drop', (x, y, shift, ctrl) => {
    if (!nativeDragOut) return;
    const hit = Number.isFinite(x) && Number.isFinite(y) ? document.elementFromPoint(x, y) : null;
    if (!hit) return;
    const rows = grid.selectedRows();
    if (!rows.length) return;
    const payload = grid.dragPayload();
    const ev = { shiftKey: !!shift, ctrlKey: !!ctrl };
    if (hit.closest('#local-pane') && localPane.visible) {
      const rowEl = hit.closest('.grid-row');
      const m = rowEl?._model;
      if (m && (m.isDir || m.isBucket)) { localPane.on.dropFolder?.(m, payload, ev); return; }
      localPane.on.dropBody?.(payload, ev);
      return;
    }
    const tnode = hit.closest('#tree .tnode');
    if (tnode) {
      const d = tnode.dataset;
      if (d.rdir !== undefined) { dropToTarget({ kind: 'remote', source: d.source, dir: d.rdir }, payload, ev); return; }
      if (d.bucket !== undefined) { dropToTarget({ kind: 's3', source: d.source || '', bucket: d.bucket, dir: d.prefix || '' }, payload, ev); return; }
      if (d.stype && d.stype !== 's3') { dropToTarget({ kind: 'remote', source: d.source, dir: '/' }, payload, ev); return; }
      return; // account-wide root: no bucket picked, same refusal as a DOM drop
    }
    const rowEl = hit.closest('#grid-body .grid-row');
    if (rowEl && rowEl._model?.isDir) { grid.on.drop?.(rowEl._model, payload, ev); return; }
    if (hit.closest('#grid-body')) {
      const dest = xferDestOf(nav.current);
      if (dest) dropToTarget(dest, payload, ev);
    }
  });
}

// copyS3Selection runs an S3→S3 copy/move with the per-task version
// choice. When the destination bucket has versioning enabled the user
// picks "preserve the full history" (checkbox pre-set from the Settings
// toggle) or a plain latest-version copy; non-versioned destinations skip
// the question — the timeline would collapse anyway. plain() runs the
// copy exactly the old way. Returns 'versioned' | 'plain' | 'canceled'.
async function copyS3Selection(origin, dest, move, plain) {
  let guard = null;
  try { guard = await ensureGuard(dest.source || viewSource, dest.bucket); } catch { /* unknown guard → plain */ }
  if (guard?.versioning === 'Enabled') {
    const preserve = await versionChoiceDialog({
      move,
      defaultOn: localStorage.getItem('s3b-copy-versions') !== '0',
    });
    if (preserve === null) return 'canceled'; // user closed the dialog
    if (preserve) {
      try {
        await api.CopySelectionVersions(origin.source || '', origin.bucket, origin.keys,
          dest.source || '', dest.bucket, dest.dir || '', move);
        toast(`${move ? 'Moving' : 'Copying'} version history — see File transfers`, 'ok');
        return 'versioned';
      } catch (err) {
        toast(`Versioned ${move ? 'move' : 'copy'} failed: ${err}`, 'error');
        return 'canceled';
      }
    }
  }
  await plain();
  return 'plain';
}

// dropToTarget is the single drop dispatcher: target is
// {kind:'s3',bucket,dir[,source]} | {kind:'remote',source,dir} (legacy
// {bucket,prefix} call sites are normalized; a set `source` addresses a named
// S3 source, "" or missing = the default). Modifier rules: copy by default,
// Shift forces move; within the same S3 bucket (of the same source) or the
// same remote source, move is the default (Ctrl keeps a copy), matching
// Explorer.
async function dropToTarget(target, data, e) {
  const dest = target.kind ? target : { kind: 's3', bucket: target.bucket, dir: target.prefix || '' };
  if (data.paths?.length) { // dragged from the local pane
    if (dest.kind === 's3') {
      // a destination on another source (side pane / tree) streams through
      // TransferCross; the view source keeps the plain upload path
      if (dest.source && dest.source !== viewSource) await startTransfer({ localPaths: data.paths, dest, move: e.shiftKey });
      else uploadPaths(data.paths, dest.dir, dest.bucket, dest.source);
      return;
    }
    if (dest.kind === 'remote') { await startTransfer({ localPaths: data.paths, dest, move: e.shiftKey }); return; }
    return;
  }
  if (data.source && !data.bucket) { // dragged from a remote source view
    const items = (data.entries || []).map((en) => ({ source: data.source, key: en.key, size: en.size || 0, isDir: !!en.isDir }));
    if (!items.length) return;
    const sameSource = dest.kind === 'remote' && dest.source === data.source;
    if (sameSource && destDirOf(dest) === data.dir) return; // onto itself
    if (sameSource && intoItself(destDirOf(dest), data.keys)) {
      toast('A folder cannot be moved or copied into itself', 'error');
      return;
    }
    const move = sameSource ? (!e.ctrlKey || e.shiftKey) : e.shiftKey;
    await startTransfer({ items, dest, move });
    return;
  }
  const srcBucket = data.bucket;
  if (!srcBucket || !data.keys?.length) return;
  const srcTag = data.source || viewSource; // origin source (main-grid drags carry it)
  const sameS3 = dest.kind === 's3' && srcBucket === dest.bucket && srcTag === (dest.source || viewSource);
  if (sameS3 && destDirOf(dest) === data.dir) return; // onto itself
  if (sameS3 && intoItself(destDirOf(dest), data.keys)) {
    toast('A folder cannot be moved or copied into itself', 'error');
    return;
  }
  if (dest.kind === 's3' && srcTag === viewSource && (dest.source || viewSource) === viewSource) {
    // S3 → S3 within the view source keeps the synchronous server-side copy
    // path (behind the per-task version choice when the destination keeps
    // versioning)
    const move = sameS3
      ? !e.ctrlKey || e.shiftKey         // same bucket: move (Shift forces)
      : e.shiftKey;                      // cross bucket: copy (Shift forces move)
    await copyS3Selection(
      { source: '', bucket: srcBucket, keys: data.keys },
      { kind: 's3', source: '', bucket: dest.bucket, dir: dest.dir || '' },
      move,
      async () => {
        try {
          const res = await api.CopySelection(srcBucket, data.keys, dest.bucket, dest.dir || '', move);
          if (res.errors?.length) toast(`Errors: ${res.errors.slice(0, 3).join('; ')}`, 'error');
          else toast(`${move ? 'Moved' : 'Copied'} ${res.copied} object(s)`, 'ok');
          refreshCurrent();
        } catch (err) {
          toast(`Drag & drop ${move ? 'move' : 'copy'} failed: ${err}`, 'error');
        }
      },
    );
    return;
  }
  // S3 origin on a named source (side pane), or S3 → remote/local: stream
  // through TransferCross (same-client S3→S3 still copies server-side there).
  const items = (data.entries || []).map((en) => ({ source: srcTag, bucket: srcBucket, key: en.key, size: en.size || 0, isDir: !!en.isDir }));
  if (!items.length) return;
  const move = sameS3 ? (!e.ctrlKey || e.shiftKey) : e.shiftKey;
  if (dest.kind === 's3') {
    // cross-source S3→S3 still deserves the version choice
    await copyS3Selection(
      { source: srcTag, bucket: srcBucket, keys: data.keys },
      dest, move,
      async () => { await startTransfer({ items, dest, move }); },
    );
    return;
  }
  await startTransfer({ items, dest, move });
}

// destDirOf normalizes a destination dir for comparison.
function destDirOf(dest) {
  if (dest.kind === 'remote') return (dest.dir || '/').replace(/\/+$/, '') || '/';
  if (dest.kind === 's3') return dest.dir || '';
  return dest.dir;
}

// intoItself reports whether destDir is one of the listed folder items or
// lives beneath one (same-source moves/copies only) — Explorer refuses
// that transfer, and so do we.
function intoItself(destDir, keys) {
  const d = String(destDir || '').replace(/\\/g, '/').replace(/\/+$/, '');
  for (const k of keys || []) {
    let p = String(k || '').replace(/\\/g, '/').replace(/\/+$/, '');
    if (!String(k || '').endsWith('/') && !String(k || '').endsWith('\\')) continue; // files contain nothing
    if (d === p || (d + '/').startsWith(p + '/')) return true;
  }
  return false;
}

// dropToLocal handles drops onto the local pane (folder rows and body):
// an S3 origin on the active view source keeps the DownloadRefs fast path;
// any other named source or a remote origin streams down through
// TransferCross.
async function dropToLocal(payload, dir) {
  if (!dir || payload.paths?.length) return; // local→local is Explorer's job
  if (payload.source && !payload.bucket) { // remote origin
    const items = (payload.entries || []).map((en) => ({ source: payload.source, key: en.key, size: en.size || 0, isDir: !!en.isDir }));
    if (items.length) await startTransfer({ items, dest: { kind: 'local', dir }, label: dir });
    return;
  }
  if (payload.entries?.length) {
    if (payload.source && payload.source !== viewSource) { // named S3 source (side-pane origin)
      const items = payload.entries.map((en) => ({ source: payload.source, bucket: payload.bucket, key: en.key, size: en.size || 0, isDir: !!en.isDir }));
      await startTransfer({ items, dest: { kind: 'local', dir }, label: dir });
    } else {
      downloadRefs(payload.entries, dir, payload.bucket);
    }
  }
}

// showLocalRowMenu: the side pane's per-row menu (open/copy/cut feed the
// cross-source clipboard). Delete runs the same Delete Window as every
// other source — local deletion is permanent (no trash), so the window's
// counts are the safety net.
function showLocalRowMenu(e, rows) {
  if (localPane.binding.kind === 'remote') return showSideRemoteRowMenu(e, rows);
  if (localPane.binding.kind === 's3') return showSideS3RowMenu(e, rows);
  const sel = rows.length;
  if (!sel) return;
  openMenu(e, [
    [rows.length === 1 && rows[0].isDir ? 'Open' : 'Open / Preview', 'Enter', () => localPane.grid.on.activate(rows[0])],
    null,
    ['Copy', 'Ctrl+C', () => copySelection()],
    ['Cut', 'Ctrl+X', () => cutSelection()],
    ['Copy name', '', () => copyAsText(rows, 'name', { kind: 'local' }), !sel],
    ['Copy path', '', () => copyAsText(rows, 'path', { kind: 'local' }), !sel],
    ['Copy URL', '', () => copyAsText(rows, 'url', { kind: 'local' }), !sel],
    null,
    ['Delete\u2026', 'Del', () => deleteLocalSelection(rows.map((r) => r.path)), !sel],
    null,
    ['Properties', 'Alt+Enter', () => {
      const r = rows[0];
      properties(`Properties — ${r.name}`, [
        ['Type', r.isDir ? 'Folder' : 'File'],
        ...(!r.isDir ? [['Size', fmtBytes(r.size || 0)]] : []),
        ['Path', r.path],
      ]);
    }, sel !== 1],
  ]);
}

// deleteLocalSelection deletes local files/folders through the unified
// Delete Window: LocalDeletePreview expands directory trees into counts and
// bytes first (count-then-act), roots are refused Go-side, and deletion is
// permanent — the OS trash is not involved.
async function deleteLocalSelection(paths) {
  if (!paths?.length) return;
  try {
    const p = await api.LocalDeletePreview(paths);
    const desc = `${p.objects} file(s)${p.folders ? ` in ${p.folders} folder(s)` : ''}${p.bytes ? ` (${fmtBytes(p.bytes)})` : ''}`;
    const mode = await runDeleteWindow({
      target: paths.length === 1 ? paths[0] : `${paths.length} item(s)`,
      summary: { objects: p.objects, folders: p.folders, bytes: p.bytes, requiresL2: p.requiresL2 },
      classicTyped: true, // permanent, no trash: classic mode types the word when the setting is on
      classicMsg: `You are about to delete ${desc}.\nLocal deletion is permanent — the Recycle Bin is not used.`,
    });
    if (mode === null) return;
    const res = await api.LocalRemove(paths);
    reportDeleteResult(res, (n) => `Deleted ${n} item(s)`);
    localPane.refresh();
  } catch (err) {
    toast(`Delete failed: ${err}`, 'error');
  }
}

// showSideRemoteRowMenu: per-row menu for a remote-bound side pane — the
// same engine-native operations as the main remote view.
function showSideRemoteRowMenu(e, rows) {
  const sel = rows.length;
  if (!sel) return;
  const b = localPane.binding;
  openMenu(e, [
    ...(sel === 1 && rows[0].isDir
      ? [['Open', 'Enter', () => localPane.grid.on.activate(rows[0])]]
      : [[`Download${sel ? ` (${sel})` : ''}\u2026`, 'Ctrl+D', () => downloadSideRows(rows)]]),
    null,
    ['Copy', 'Ctrl+C', () => copySelection(), !sel],
    ['Cut', 'Ctrl+X', () => cutSelection(), !sel],
    ['Copy name', '', () => copyAsText(rows, 'name', { kind: 'remote' }), !sel],
    ['Copy path', '', () => copyAsText(rows, 'path', { kind: 'remote' }), !sel],
    ['Copy URL', '', () => copyAsText(rows, 'url', { kind: 'remote', source: b.source }), !sel],
    ...(sel === 1 && rows[0].isDir
      ? [['Paste into folder', 'Ctrl+V', () => paste(null, null, { kind: 'remote', source: b.source, dir: rows[0].key }), !pasteReady()]]
      : []),
    null,
    ['Rename', 'F2', async () => {
      const row = rows[0];
      const name = await prompt({ title: 'Rename', label: 'New name', value: row.name });
      if (!name || name === row.name) return;
      try {
        await api.RemoteRename(b.source, row.key, name);
        toast('Renamed', 'ok');
        localPane.refresh();
      } catch (err) {
        toast(`Rename failed: ${err}`, 'error');
      }
    }, sel !== 1],
    ['Delete\u2026', 'Del', () => deleteRemoteSelection(b.source, rows.map((r) => r.key)).then(() => localPane.refresh()), !sel],
    null,
    ['Properties', 'Alt+Enter', () => sideRemoteProperties(rows[0]), sel !== 1],
  ]);
}

// sideRemoteEmptyMenu: empty-area menu of a remote-bound side pane —
// paste/uploads land in the pane's current dir.
function sideRemoteEmptyMenu(e) {
  const b = localPane.binding;
  const dir = localPane.dir || '/';
  openMenu(e, [
    ['Paste', 'Ctrl+V', () => paste(null, null, { kind: 'remote', source: b.source, dir }), !pasteReady()],
    null,
    ...uploadMenu(
      async () => { const paths = await api.PickUploadFiles(); if (paths?.length) uploadToRemote(paths, b.source, dir); },
      async () => { const d = await api.PickFolder('Choose a folder to upload'); if (d) uploadToRemote([d], b.source, dir); },
    ),
    ['New folder', 'Ctrl+Shift+N', async () => {
      const name = await prompt({ title: 'New folder', label: 'Folder name', value: 'new-folder' });
      if (!name) return;
      try {
        await api.RemoteMkdir(b.source, remoteChildPath(localPane.dir || '', name));
        toast('Folder created', 'ok');
        localPane.refresh();
      } catch (err) {
        toast(`Create folder failed: ${err}`, 'error');
      }
    }],
    null,
    [`Download all\u2026`, '', () => downloadSideRows(localPane.grid.rows), !localPane.grid.rows.length],
    ['Select all', 'Ctrl+A', () => localPane.grid.selectAll()],
    ['Refresh', 'F5', () => localPane.refresh()],
  ]);
}

// downloadSideRows downloads rows from a remote- or S3-bound side pane into
// a picked local folder.
async function downloadSideRows(rows) {
  const b = localPane.binding;
  if (!rows?.length) return;
  const dest = await api.PickFolder('Choose download folder');
  if (!dest) return;
  const items = b.kind === 's3'
    ? rows.map((r) => ({ source: b.source, bucket: localPane.bucket, key: r.key, size: r.size || 0, isDir: !!r.isDir }))
    : rows.map((r) => ({ source: b.source, key: r.key, size: r.size || 0, isDir: !!r.isDir }));
  await startTransfer({
    items,
    dest: { kind: 'local', dir: dest },
    label: dest,
  });
}

// sideRemoteProperties shows one row's engine-side metadata.
async function sideRemoteProperties(row) {
  const b = localPane.binding;
  try {
    const st = await api.RemoteStat(b.source, row.key);
    properties(`Properties — ${row.name}`, [
      ['Name', row.name],
      ['Type', row.isDir ? 'Folder' : 'File'],
      ...(!row.isDir ? [
        ['Size', fmtBytes(st.size)],
        ['Last modified', fmtDate(st.lastModified)],
      ] : []),
      ['Source', b.source],
      ['Path', row.key],
    ]);
  } catch (err) {
    toast(`Properties failed: ${err}`, 'error');
  }
}

// showSideS3RowMenu: per-row menu for an S3-bound side pane. Copy/cut feed
// the cross-source clipboard (origin source + bucket); rename/delete/
// properties run through the source-pinned Source* APIs, so every S3
// source is fully manageable from the pane (bucket rows never).
function showSideS3RowMenu(e, rows) {
  const b = localPane.binding;
  const sel = rows.length;
  if (!sel) return;
  const hasBucketRow = rows.some((r) => r.isBucket);
  const inBucket = !!localPane.bucket;
  openMenu(e, [
    ...(sel === 1 && rows[0].isDir
      ? [['Open', 'Enter', () => localPane.grid.on.activate(rows[0])]]
      : [['Download\u2026', 'Ctrl+D', () => downloadSideRows(rows.filter((r) => !r.isBucket)), hasBucketRow]]),
    ...(sel === 1 && rows[0].isDir && !rows[0].isBucket
      ? [['Paste into folder', 'Ctrl+V', () => paste(null, null, { kind: 's3', source: b.source, bucket: localPane.bucket, dir: rows[0].key }), !pasteReady()]]
      : []),
    null,
    ['Copy', 'Ctrl+C', () => copySelection(), hasBucketRow],
    ['Cut', 'Ctrl+X', () => cutSelection(), hasBucketRow],
    ['Copy name', '', () => copyAsText(rows, 'name', { kind: 'objects', bucket: localPane.bucket }), hasBucketRow],
    ['Copy path', '', () => copyAsText(rows, 'path', { kind: 'objects', bucket: localPane.bucket }), hasBucketRow],
    ['Copy S3 URI', '', () => copyAsText(rows, 'uri', { kind: 'objects', bucket: localPane.bucket }), hasBucketRow],
    ['Copy URL', '', () => copyAsText(rows, 'url', { kind: 'objects', bucket: localPane.bucket, source: b.source }), hasBucketRow],
    null,
    ['Rename', 'F2', async () => {
      const row = rows[0];
      const name = await prompt({ title: 'Rename', label: 'New name', value: row.name });
      if (!name || name === row.name) return;
      try {
        await api.SourceRenameObject(b.source, localPane.bucket, row.key, name);
        toast('Renamed', 'ok');
        localPane.refresh();
      } catch (err) {
        toast(`Rename failed: ${err}`, 'error');
      }
    }, !inBucket || sel !== 1 || rows[0].isBucket],
    ['Delete\u2026', 'Del', () => deleteSideS3Selection(b.source, localPane.bucket, rows.filter((r) => !r.isBucket).map((r) => r.key)), !inBucket || hasBucketRow],
    null,
    ['Properties', 'Alt+Enter', () => sideS3Properties(rows[0]), !inBucket || sel !== 1],
  ]);
}

// deleteSideS3Selection deletes keys in a bucket of a named S3 source —
// the same Delete Window and type list as the main view, through the
// source-pinned Source* APIs.
async function deleteSideS3Selection(source, bucket, keys) {
  if (!keys.length) return;
  const target = keys.length === 1
    ? `${source}://${bucket}/${keys[0]}`
    : `${source}://${bucket}/${localPane.dir || ''}`;
  try {
    await deleteS3Keys(source, bucket, keys, '', target, () => localPane.refresh());
  } catch (err) {
    toast(`Delete failed: ${err}`, 'error');
  }
}

// sideS3EmptyMenu: empty-area menu of an S3-bound side pane. Writes land in
// the pane's bucket/prefix — uploads through TransferCross, folder creation
// through the source-pinned API; both need a bucket first.
function sideS3EmptyMenu(e) {
  const b = localPane.binding;
  const dir = localPane.dir || '';
  const dest = { kind: 's3', source: b.source, bucket: localPane.bucket, dir };
  const inBucket = !!localPane.bucket;
  openMenu(e, [
    ['Paste', 'Ctrl+V', () => paste(null, null, dest), !pasteReady() || !inBucket],
    null,
    ...uploadMenu(
      async () => { const paths = await api.PickUploadFiles(); if (paths?.length) startTransfer({ localPaths: paths, dest }); },
      async () => { const d = await api.PickFolder('Choose a folder to upload'); if (d) startTransfer({ localPaths: [d], dest }); },
      !inBucket,
    ),
    ['New folder', 'Ctrl+Shift+N', async () => {
      const name = await prompt({ title: 'New folder', label: 'Folder name', value: 'new-folder' });
      if (!name) return;
      try {
        await api.SourceCreateFolder(b.source, localPane.bucket, dir, name);
        toast('Folder created', 'ok');
        localPane.refresh();
      } catch (err) {
        toast(`Create folder failed: ${err}`, 'error');
      }
    }, !inBucket],
    null,
    ['Download all\u2026', '', () => downloadSideRows(localPane.grid.rows.filter((r) => !r.isBucket)), !localPane.grid.rows.length],
    ['Select all', 'Ctrl+A', () => localPane.grid.selectAll()],
    ['Refresh', 'F5', () => localPane.refresh()],
  ]);
}

// sideS3Properties shows one object's metadata via the source-pinned
// SourceStatObject.
async function sideS3Properties(row) {
  const b = localPane.binding;
  try {
    const st = await api.SourceStatObject(b.source, localPane.bucket, row.key);
    properties(`Properties — ${row.name}`, [
      ['Name', row.name],
      ['Type', row.isDir ? 'Folder' : 'Object'],
      ...(row.isDir
        ? [['Objects', st.usage?.objectCount], ['Total size', fmtBytes(st.usage?.totalBytes || 0)]]
        : [
          ['Size', fmtBytes(st.size || 0)],
          ['Last modified', fmtDate(st.lastModified)],
          ['Storage class', st.storageClass || ''],
        ]),
      ['Source', b.source],
      ['Path', `${b.source}://${localPane.bucket}/${row.key}`],
    ]);
  } catch (err) {
    toast(`Properties failed: ${err}`, 'error');
  }
}

// Patch grid drag payload to carry the origin (source + bucket or remote
// source + dir) so drops build the right TransferCross items.
const origDragPayload = grid.dragPayload.bind(grid);
grid.dragPayload = () => {
  const loc = nav.current;
  const base = origDragPayload();
  if (loc?.kind === 'objects') return { ...base, source: loc.source || viewSource, bucket: loc.bucket, dir: loc.prefix || '' };
  if (loc?.kind === 'remote') return { ...base, source: loc.source, dir: loc.path || '/' };
  return base;
};

// ============================ marquee ============================
function startMarquee(e) {
  const body = grid.body;
  const startX = e.clientX, startY = e.clientY;
  const mq = $('marquee');
  let active = false;

  const onMove = (ev) => {
    if (!active && Math.hypot(ev.clientX - startX, ev.clientY - startY) < 5) return;
    active = true;
    mq.classList.remove('hidden');
    const x = Math.min(ev.clientX, startX), y = Math.min(ev.clientY, startY);
    const w = Math.abs(ev.clientX - startX), h = Math.abs(ev.clientY - startY);
    Object.assign(mq.style, { left: `${x}px`, top: `${y}px`, width: `${w}px`, height: `${h}px` });
    const rect = body.getBoundingClientRect();
    const first = Math.max(0, Math.floor((y - rect.top + body.scrollTop) / 28));
    const last = Math.floor((y + h - rect.top + body.scrollTop) / 28);
    grid.sel.clear();
    for (let i = first; i <= Math.min(last, grid.rows.length - 1); i++) grid.sel.add(grid.rows[i].key);
    grid.render();
    updateStatus();
  };
  const onUp = () => {
    mq.classList.add('hidden');
    document.removeEventListener('mousemove', onMove);
    document.removeEventListener('mouseup', onUp);
  };
  document.addEventListener('mousemove', onMove);
  document.addEventListener('mouseup', onUp);
}

// ============================ toolbar ============================
function wireToolbar() {
  $('btn-back').onclick = () => { if (nav.canBack()) nav.back(); };
  $('btn-forward').onclick = () => { if (nav.canForward()) nav.forwardGo(); };
  $('btn-up').onclick = () => { const p = parentOf(nav.current); if (p) nav.to(p); };
  // () => : a bare `onclick = refreshCurrent` would pass the MouseEvent in
  // as `silent` (truthy) — the button refresh would silently skip the
  // loading state and bury errors as stale-row toasts. Manual refresh is
  // explicit: show the in-flight state, show errors.
  $('btn-refresh').onclick = () => refreshCurrent();
  $('btn-upload').onclick = () => openMenu($('btn-upload'), uploadChoices(uploadFiles, uploadFolder));
  $('btn-download').onclick = () => downloadSelection();
  $('btn-panes').onclick = togglePanes;
  $('btn-find').onclick = findFromHere;
  $('btn-newfolder').onclick = newFolder;
  $('btn-newfile').onclick = newFile;
  $('btn-theme').onclick = toggleTheme;
  $('btn-help').onclick = helpSheet;
  $('filter').addEventListener('input', debounce(() => {
    view.filter = $('filter').value;
    grid.setFilter(view.filter);
  }, 120));

  // Path bar: clicking the navbar's empty area (not a crumb or the filter
  // box) opens the inline path editor for copy/paste navigation.
  const navbar = document.querySelector('.navbar');
  navbar.title = t('pathClickHint');
  navbar.addEventListener('click', (e) => {
    if (e.target.closest('.crumb, .crumb-sep, input, button')) return;
    editPath();
  });
}

// findFromHere opens the deep-search dialog for the current folder, the
// selected bucket (buckets view), or the current bucket.
function findFromHere() {
  const loc = nav.current;
  if (!loc) return;
  if (loc.kind === 'objects') {
    findDialog(loc.bucket, loc.prefix || '', openSearchResult);
    return;
  }
  const row = grid.selectedRows()[0];
  if (row) findDialog(row.key, '', openSearchResult);
  else toast('Open a bucket first — or select one');
}

// ============================ profile file session (M8) ============================
// pfState mirrors GetProfileFileState() (open container + dirty flag). The
// File menu reads it for enablement; the status bar shows the session.
let pfState = { open: false };

async function refreshPfState() {
  try {
    pfState = await api.GetProfileFileState();
  } catch {
    pfState = { open: false };
  }
  const sp = $('status-pfile');
  if (pfState.open) {
    sp.classList.remove('hidden');
    sp.textContent = `\u{1F510} ${pfState.name || 'profile file'}${pfState.dirty ? ' \u25CF' : ''}`;
    sp.title = pfState.path
      ? `${pfState.path}${pfState.dirty ? ' — unsaved changes (Ctrl+S)' : ''}`
      : 'unsaved profile file — use File \u2192 Save profile file as\u2026';
  } else if (pfState.sourceCount > 0) {
    // Session-only sources: live in memory, gone on close (strict model).
    sp.classList.remove('hidden');
    sp.textContent = `\u25CF ${pfState.sourceCount} unsaved source${pfState.sourceCount === 1 ? '' : 's'}`;
    sp.title = 'These sources live in memory only and vanish on close.\nUse File \u2192 Save profile file as\u2026 to store them in an encrypted profile file.';
  } else {
    sp.classList.add('hidden');
  }
}

async function newProfileFileUi() {
  const name = await prompt({ title: t('pf.new'), label: t('pf.nameLabel'), value: 'work' });
  if (name === null) return;
  const pw = await prompt({ title: t('pf.new'), label: t('pf.pwLabel'), okLabel: 'Create', password: true });
  if (!pw) return; // empty resolves null too — a password is required anyway
  const pw2 = await prompt({ title: t('pf.new'), label: t('pf.pwRepeat'), okLabel: 'Create', password: true });
  if (pw2 !== pw) { toast('Passwords do not match', 'error'); return; }
  try {
    await api.NewProfileFile(name.trim(), pw);
    toast(t('pf.created'), 'ok');
    await refreshSources();
    await refreshPfState();
  } catch (err) { toast(`${err}`, 'error'); }
}

async function openProfileFileUi() {
  let path;
  try {
    path = await api.PickOpenProfileFile();
  } catch (err) { toast(`${err}`, 'error'); return; }
  if (!path) return;
  const pw = await prompt({ title: t('pf.open'), label: t('pf.pwFor', { name: basename(path) }), okLabel: 'Open', password: true });
  if (!pw) return;
  try {
    await api.OpenProfileFile(path, pw);
    toast(`${t('pf.opened')}: ${basename(path)}`, 'ok');
    await refreshSources();
    await refreshPfState();
    nav.to(sourceHomeLoc());
  } catch (err) {
    toast(`${err}`, 'error'); // wrong password and corruption look identical by design
  }
}

async function saveProfileFileUi() {
  await refreshPfState();
  if (!pfState.open) {
    // Session-only sources: Ctrl+S runs Save As so they become a Profile
    // file (the fix for "cannot save without creating New Profile first").
    if (pfState.sourceCount > 0) return saveAsProfileFileUi();
    return; // Ctrl+S is global; silently ignore with nothing to save
  }
  if (!pfState.path) return saveAsProfileFileUi(); // never saved yet
  try {
    await api.SaveProfileFile();
    toast(t('pf.saved'), 'ok');
    await refreshPfState();
  } catch (err) { toast(`${err}`, 'error'); }
}

async function saveAsProfileFileUi() {
  await refreshPfState();
  if (!pfState.open && !pfState.sourceCount) return;
  let path;
  try {
    path = await api.PickSaveProfileFile(pfState.name || 'profile');
  } catch (err) { toast(`${err}`, 'error'); return; }
  if (!path) return;
  // The password prompt resolves null for cancel AND empty input; the label
  // says empty keeps the current password, so both proceed with pw || ''.
  const pw = await prompt({ title: t('pf.saveAs'), label: t('pf.newPw'), okLabel: 'Save', password: true });
  try {
    await api.SaveProfileFileAs(path, pw || '');
    toast(`${t('pf.saved')}: ${basename(path)}`, 'ok');
    await refreshPfState();
  } catch (err) { toast(`${err}`, 'error'); }
}

async function closeProfileFileUi() {
  await refreshPfState();
  if (!pfState.open && !pfState.sourceCount) return;
  try {
    await api.CloseProfileFile(false);
  } catch (err) {
    // Dirty container or unsaved session sources: offer a discard.
    const ok = await confirm({
      title: t('pf.close'),
      message: pfState.open
        ? 'The profile file has unsaved changes.\nClose anyway and discard them?'
        : `There are ${pfState.sourceCount} unsaved session source(s).\nClose anyway and discard them?`,
      okLabel: 'Discard & close',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.CloseProfileFile(true);
    } catch (err2) { toast(`${err2}`, 'error'); return; }
  }
  toast(t('pf.closed'), 'ok');
  await refreshSources();
  await refreshPfState();
  nav.to(sourceHomeLoc());
}

// ============================ settings ============================
// savedLang: the persisted language choice; 'en' is the default (no
// stored choice = English), 'auto' follows the browser.
const savedLang = () => localStorage.getItem('s3b-lang') || 'en';

// setLanguage persists the choice and reloads — strings render once at
// construction, and a reload is the honest way to re-render them all.
function setLanguage(v) {
  localStorage.setItem('s3b-lang', v);
  window.location.reload();
}

// openSettings mounts the Settings dialog over the persisted knobs; rows
// apply immediately through the same setters the menus use. The log-file
// preference lives Go-side (logsettings.json) so the writer honors it —
// fetched before the dialog opens and pushed back on change.
async function openSettings() {
  let logSet = { mode: 'default', dir: '' };
  try { logSet = await api.GetLogSettings(); } catch { /* binding missing pre-Startup */ }
  let secSet = { enabled: false, keyringAvailable: false, keyringBackend: '', editorDir: '', spoolDir: '' };
  try { secSet = await api.GetSecureStorage(); } catch { /* binding missing pre-Startup */ }
  let tunSet = null;
  try { tunSet = await api.GetTuning(); } catch { /* binding missing pre-Startup */ }
  settingsDialog({
    state: {
      theme: () => localStorage.getItem('s3b-theme') || 'auto',
      lang: savedLang,
      autoRefreshMs: () => autoRefreshMs,
      refreshOnFocus: () => refreshOnFocus,
      panes: () => localPane.visible,
      log: () => !$('logarea').classList.contains('hidden'),
      conflict: () => localStorage.getItem('s3b-conflict') || 'ask',
      throttle: () => localStorage.getItem('s3b-throttle') || '0',
      showThrottle: () => localStorage.getItem('s3b-show-throttle') === '1',
      editChooseApp: () => localStorage.getItem('s3b-edit-choose-app') !== '0',
      copyVersions: () => localStorage.getItem('s3b-copy-versions') !== '0',
      cols: () => grid.visibleCols().map((c) => c.id),
      colsLocal: () => localPane.grid.visibleCols().map((c) => c.id),
      showHidden: () => localStorage.getItem('s3b-show-hidden') === '1',
      showMarkers: () => localStorage.getItem('s3b-show-markers') === '1',
      showVersions: () => localStorage.getItem('s3b-show-versions') === '1',
      delWindow: delWindowOn,
      delTypeConfirm: delTypedOn,
      delAutoConfirm: delAutoConfirm,
      explorerClip: () => localStorage.getItem('s3b-os-clip') !== '0',
      xferWin: () => localStorage.getItem('s3b-xfer-window') !== '0',
      popoutCenter: () => (localStorage.getItem('s3b-popout-center') === 'app' ? 'app' : 'display'),
      // Popout geometry persistence (dialogs.js reads this fresh on every
      // open/save) — off = windows always open centered, nothing stored.
      popoutPersist: () => localStorage.getItem('s3b-popouts-persist') !== '0',
      // Synced local/remote browsing (local.js setSync mirrors the pane
      // header checkbox and captures the base pair on enable).
      localSync: () => localPane.sync,
    },
    apply: {
      theme: setThemePref,
      lang: setLanguage,
      autoRefresh: setAutoRefresh,
      refreshOnFocus: setRefreshOnFocus,
      panes: setPanes,
      log: setLogArea,
      conflict: (v) => localStorage.setItem('s3b-conflict', v),
      throttle: (v) => localStorage.setItem('s3b-throttle', String(v)),
      showThrottle: (v) => localStorage.setItem('s3b-show-throttle', v ? '1' : '0'),
      editChooseApp: (v) => localStorage.setItem('s3b-edit-choose-app', v ? '1' : '0'),
      copyVersions: (v) => localStorage.setItem('s3b-copy-versions', v ? '1' : '0'),
      cols: (v) => { grid.setColumns(v); localStorage.setItem('s3b-cols', v.join(',')); },
      colsLocal: (v) => { localPane.grid.setColumns(v); localStorage.setItem('s3b-cols-local', v.join(',')); },
      showHidden: (v) => { localStorage.setItem('s3b-show-hidden', v ? '1' : '0'); refreshCurrent(); },
      showMarkers: (v) => { localStorage.setItem('s3b-show-markers', v ? '1' : '0'); grid.showMarkers = v; grid.render(); },
      showVersions: (v) => { localStorage.setItem('s3b-show-versions', v ? '1' : '0'); grid.showVersions = v; grid.render(); },
      delWindow: (v) => localStorage.setItem('s3b-del-window', v ? '1' : '0'),
      delTypeConfirm: (v) => localStorage.setItem('s3b-del-typeconfirm', v ? '1' : '0'),
      delAutoConfirm: (v) => localStorage.setItem('s3b-del-autoconfirm', v ? '1' : '0'),
      explorerClip: (v) => {
        localStorage.setItem('s3b-os-clip', v ? '1' : '0');
        // Enable must NOT adopt the seq: a copy made while sharing was off
        // (or in Explorer before enabling) is still the newest write — last
        // copy wins. Refresh only re-syncs osClipFilesReady for canPaste.
        if (v) refreshOsClip(); else { osClipFilesReady = false; }
        updateCommandState();
      },
      // Read fresh on every transfer-start decision (dialogs.js) — no
      // other consumer needs notifying.
      xferWin: (v) => localStorage.setItem('s3b-xfer-window', v ? '1' : '0'),
      // Read fresh on every native popout open (dialogs.js) — no other
      // consumer needs notifying.
      popoutCenter: (v) => localStorage.setItem('s3b-popout-center', v === 'app' ? 'app' : 'display'),
      popoutPersist: (v) => localStorage.setItem('s3b-popouts-persist', v ? '1' : '0'),
      localSync: (v) => localPane.setSync(v),
    },
    log: {
      get: () => logSet,
      set: async (mode, dir, levels, scopes, sources) => {
        try { logSet = await api.SetLogSettings(mode, dir, levels || [], scopes || [], sources || []); }
        catch (e) { toast(String(e), 'error'); }
        return logSet;
      },
      browse: async () => {
        try { return await api.PickFolder('Choose the log folder'); } catch { return ''; }
      },
    },
    // Secure Storage (pkg/api/secure.go): the toggle is honored Go-side,
    // so round-trip the binding and re-sync from the on-disk truth. A
    // failed switch re-fetches the status; a success refreshes the cached
    // file-log settings too (enabling turns file logging off).
    security: {
      get: () => secSet,
      set: async (on) => {
        try {
          secSet = await api.SetSecureStorage(on);
          try { logSet = await api.GetLogSettings(); } catch { /* keep last */ }
        } catch (e) {
          toast(String(e), 'error');
          try { secSet = await api.GetSecureStorage(); } catch { /* keep last */ }
        }
        return secSet;
      },
    },
    // Engine tuning (pkg/api/tuning.go): every value is honored Go-side —
    // the listing watchdog and quick-op budgets, the SDK retryer, the
    // multipart shape at every transfer site, the Stalled flag threshold —
    // and persisted in appsettings.json, so it survives restarts. set
    // pushes the whole snapshot; the stored (clamped) truth comes back and
    // re-syncs the selects.
    engine: {
      get: () => tunSet,
      set: async (v) => {
        try {
          tunSet = await api.SetTuning(
            v.listingTimeoutMs || 0, v.compareTimeoutMs || 0, v.retryAttempts || 0,
            v.partSizeMiB || 0, v.partConcurrency || 0, v.stallAfterMs || 0,
          );
        } catch (e) { toast(String(e), 'error'); }
        return tunSet;
      },
    },
    // Reset to defaults: wipe every persisted shell knob and reload.
    // Favorites are data, not settings — they survive. The Go-side
    // preferences (logsettings.json, appsettings.json) reset through
    // their bindings — 0 means "default" per field.
    reset: () => {
      const favs = localStorage.getItem('s3b-favs');
      localStorage.clear();
      if (favs !== null) localStorage.setItem('s3b-favs', favs);
      try { api.SetLogSettings('default', '', [], [], []); } catch { /* best effort */ }
      try { api.SetTuning(0, 0, 0, 0, 0, 0); } catch { /* best effort */ }
      window.location.reload();
    },
  });
}

// ============================ menu bar ============================
// enabled flags are re-evaluated on every dropdown open (menubar.js
// re-renders), reading live state through commandState().
function mountMenubar() {
  const st = () => commandState();
  const inObjects = () => nav.current?.kind === 'objects';

  const defs = [
    {
      label: t('menu.file'),
      items: [
        // One Upload entry with a Files/Folder submenu — the same selector
        // every other Upload surface (toolbar, context menus, empty states)
        // opens.
        {
          label: 'Upload',
          items: [
            { label: 'Files\u2026', kbd: 'Ctrl+U', action: uploadFiles, enabled: () => st().canUpload },
            { label: 'Folder\u2026', action: uploadFolder, enabled: () => st().canUpload },
          ],
        },
        null,
        { label: 'Import S3 Credential\u2026', action: importCredsUi },
        null,
        { label: t('pf.new'), action: newProfileFileUi },
        { label: t('pf.open'), action: openProfileFileUi },
        { label: t('pf.save'), kbd: 'Ctrl+S', action: saveProfileFileUi, enabled: () => pfState.open || pfState.sourceCount > 0 },
        { label: t('pf.saveAs'), action: saveAsProfileFileUi, enabled: () => pfState.open || pfState.sourceCount > 0 },
        { label: t('pf.close'), action: closeProfileFileUi, enabled: () => pfState.open || pfState.sourceCount > 0 },
        null,
        { label: t('menu.exit'), action: () => api.ExitApp() },
      ],
    },
    {
      label: t('menu.edit'),
      items: [
        { label: t('menu.cut'), kbd: 'Ctrl+X', action: cutSelection, enabled: () => st().canCut },
        { label: t('menu.copy'), kbd: 'Ctrl+C', action: copySelection, enabled: () => st().canCopy },
        {
          label: t('menu.copyAs'),
          items: [
            { label: t('menu.copyName'), action: () => { const c = copyAsFromMenu(); copyAsText(c.rows, 'name', c.ctx); }, enabled: () => st().canCopy },
            { label: t('menu.copyPath'), action: () => { const c = copyAsFromMenu(); copyAsText(c.rows, 'path', c.ctx); }, enabled: () => st().canCopy },
            { label: t('menu.copyUri'), action: () => { const c = copyAsFromMenu(); copyAsText(c.rows, 'uri', c.ctx); }, enabled: () => st().canCopy && canCopyUriFromMenu() },
            { label: t('menu.copyUrl'), action: () => { const c = copyAsFromMenu(); copyAsText(c.rows, 'url', c.ctx); }, enabled: () => st().canCopy && canCopyUrlFromMenu() },
          ],
        },
        { label: t('menu.paste'), kbd: 'Ctrl+V', action: () => paste(), enabled: () => st().canPaste },
        { label: t('menu.selectAll'), kbd: 'Ctrl+A', action: () => grid.selectAll(), enabled: inObjects },
        { label: t('menu.invertSel'), kbd: 'Ctrl+I', action: () => grid.invertSelection(), enabled: inObjects },
        null,
        { label: t('menu.rename'), kbd: 'F2', action: () => renameSelection(), enabled: () => st().canRename },
        { label: t('menu.delete'), kbd: 'Del', action: () => deleteSelection(), enabled: () => st().canDelete },
      ],
    },
    {
      label: t('menu.view'),
      items: [
        { label: t('menu.refresh'), kbd: 'F5', action: () => refreshCurrent() },
        null,
        { label: t('menu.theme'), action: toggleTheme },
        { label: t('menu.panes'), kbd: 'F9', action: togglePanes },
        { label: t('menu.log'), kbd: 'Ctrl+L', action: toggleLogArea },
        { label: t('menu.transfers'), action: () => transferManager() },
        { label: t('menu.tasks'), action: () => runningTasks() },
        { label: t('menu.filter'), kbd: 'Ctrl+F', action: () => { $('filter').focus(); $('filter').select(); } },
        null,
        {
          label: t('menu.autorefresh'),
          items: [0, 5000, 10000, 30000, 60000].map((ms) => ({
            label: ms === 0 ? t('ar.off') : `${ms / 1000} s`,
            checked: () => autoRefreshMs === ms,
            action: () => setAutoRefresh(ms),
          })),
        },
        { label: t('ar.focus'), checked: () => refreshOnFocus, action: () => setRefreshOnFocus(!refreshOnFocus) },
        null,
        // The three versioning/visibility toggles Settings also carries —
        // one click closer (Explorer's View menu pattern).
        { label: t('settings.showVersions'), checked: () => localStorage.getItem('s3b-show-versions') === '1', action: () => { const v = localStorage.getItem('s3b-show-versions') !== '1'; localStorage.setItem('s3b-show-versions', v ? '1' : '0'); grid.showVersions = v; grid.render(); } },
        { label: t('settings.showMarkers'), checked: () => localStorage.getItem('s3b-show-markers') === '1', action: () => { const v = localStorage.getItem('s3b-show-markers') !== '1'; localStorage.setItem('s3b-show-markers', v ? '1' : '0'); grid.showMarkers = v; grid.render(); } },
        { label: t('settings.showHidden'), checked: () => localStorage.getItem('s3b-show-hidden') === '1', action: () => { const v = localStorage.getItem('s3b-show-hidden') !== '1'; localStorage.setItem('s3b-show-hidden', v ? '1' : '0'); refreshCurrent(); } },
      ],
    },
    {
      label: t('menu.settings'),
      items: [
        { label: t('settings.open'), action: openSettings },
        null,
        {
          label: t('settings.language'),
          items: [
            { label: t('settings.langAuto'), checked: () => savedLang() === 'auto', action: () => setLanguage('auto') },
            ...languages().map((code) => ({
              label: LANG_NAMES[code] || code,
              checked: () => savedLang() === code,
              action: () => setLanguage(code),
            })),
          ],
        },
        { label: t('menu.panes'), kbd: 'F9', checked: () => localPane.visible, action: togglePanes },
        { label: t('menu.log'), kbd: 'Ctrl+L', checked: () => !$('logarea').classList.contains('hidden'), action: toggleLogArea },
        { label: t('menu.theme'), action: toggleTheme },
      ],
    },
    {
      label: t('menu.help'),
      items: [
        { label: t('menu.guide'), action: usageGuideDialog },
        { label: t('menu.sources'), action: sourcesInfoDialog },
        { label: t('menu.keys'), kbd: 'F1', action: helpSheet },
        null,
        { label: t('menu.doctor'), action: doctorPicker, enabled: () => st().canDoctor },
        null,
        { label: t('menu.license'), action: licenseDialog },
        { label: t('menu.about'), action: aboutDialog },
      ],
    },
  ];

  const mb = createMenubar(defs);
  $('menubar').replaceChildren(mb.root);
}

// aboutDialog: minimal About box — brand mark, name, version, publisher,
// license, URL. Same about-panel treatment as the License dialog’s About tab.
function aboutDialog() {
  const body = el('div', { class: 'about-panel' });
  const draw = (v) => {
    body.replaceChildren(
      el('img', { class: 'about-logo', src: 'assets/logo.svg', alt: 's3b', draggable: 'false' }),
      el('div', { class: 'kv' },
      el('div', { class: 'k', text: 's3b' }),
      // strip a tag’s leading v so every injection style renders "v1.2.3"
      el('div', { class: 'v mono', text: 'v' + String(v || '?').replace(/^v/, '') }),
      el('div', { class: 'k', text: t('menu.aboutPublisher') }),
      el('div', { class: 'v', text: 'MikkoP88' }),
      el('div', { class: 'k', text: t('menu.aboutLicense') }),
      el('div', { class: 'v', text: licenseLine() }),
      el('div', { class: 'k', text: t('menu.aboutUrl') }),
      el('div', { class: 'v mono', text: LICENSE.repo }),
      ),
    );
  };
  draw('');
  api.GetVersion().then(draw).catch(() => {});
  openModal({ title: t('menu.aboutTitle'), body, buttons: [{ label: 'Close' }] });
}

// ============================ keyboard ============================
// setClip records the current selection (main grid by location kind, else the
// local pane's selection) as the clipboard payload.
function setClip(mode) {
  const loc = nav.current;
  const rows = grid.selectedRows();
  if (rows.length && (loc?.kind === 'objects' || loc?.kind === 'remote')) {
    const src = loc.kind === 'remote' ? loc.source : (loc.source || viewSource);
    Object.assign(clipboard, {
      mode,
      kind: loc.kind === 'remote' ? 'remote' : 's3',
      bucket: loc.kind === 'objects' ? loc.bucket : null,
      source: src,
      dir: loc.kind === 'remote' ? (loc.path || '/') : (loc.prefix || ''),
      keys: rows.map((x) => x.key),
      paths: [],
    });
    // In-app pastes stage references only — nothing downloads until a
    // paste runs the transfer. For Explorer's sake a copy also mirrors
    // onto the OS clipboard: the staging download runs as a HIDDEN job
    // (never in File transfers/Running tasks/badges, never an auto-open)
    // and only for small selections; cut never mirrors — an Explorer
    // paste of a cut would move/delete.
    if (mode === 'copy' && explorerClipOn()) {
      osCopyRemote(rows.map((r) => (loc.kind === 'remote'
        ? { source: src, key: r.key, size: r.size || 0, isDir: !!r.isDir }
        : { source: src, bucket: loc.bucket, key: r.key, size: r.size || 0, isDir: !!r.isDir })));
    }
    osClipAdopt(); // in-app copy is now the newest clipboard (last copy wins)
    toast(`${mode === 'cut' ? 'Cut' : 'Copied'} ${rows.length} item(s)`);
    updateCommandState();
    return;
  }
  if (localPane.visible) {
    const lrows = localPane.grid.selectedRows();
    if (!lrows.length) return;
    // A remote-bound pane contributes a remote clipboard payload (keys +
    // origin source/dir), the local binding the workstation paths.
    if (localPane.binding.kind === 'remote') {
      Object.assign(clipboard, {
        mode,
        kind: 'remote',
        bucket: null,
        source: localPane.binding.source,
        dir: localPane.dir || '/',
        keys: lrows.map((x) => x.key),
        paths: [],
      });
      if (mode === 'copy') osCopyRemote(lrows.map((r) => ({ source: localPane.binding.source, key: r.key, size: r.size || 0, isDir: !!r.isDir })));
    } else if (localPane.binding.kind === 's3') {
      if (!localPane.bucket) return; // bucket rows are navigation only
      Object.assign(clipboard, {
        mode,
        kind: 's3',
        bucket: localPane.bucket,
        source: localPane.binding.source,
        dir: localPane.dir || '',
        keys: lrows.filter((x) => !x.isBucket).map((x) => x.key),
        paths: [],
      });
      if (mode === 'copy') osCopyRemote(lrows.filter((x) => !x.isBucket).map((r) => ({ source: localPane.binding.source, bucket: localPane.bucket, key: r.key, size: r.size || 0, isDir: !!r.isDir })));
    } else {
      Object.assign(clipboard, {
        mode,
        kind: 'local',
        bucket: null,
        source: null,
        dir: localPane.dir,
        keys: [],
        paths: lrows.map((x) => x.path),
      });
      // Local files are real OS files — straight onto the OS clipboard.
      if (mode === 'copy' && explorerClipOn()) {
        api.OsClipboardSetFiles(clipboard.paths).then(() => osClipAdopt(), () => {});
      }
    }
    osClipAdopt(); // in-app copy is now the newest clipboard (last copy wins)
    toast(`${mode === 'cut' ? 'Cut' : 'Copied'} ${lrows.length} item(s)`);
    updateCommandState();
  }
}

// osCopyRemote mirrors a remote/S3 copy onto the OS clipboard so Ctrl+V in
// Explorer works: the selection is staged (downloaded) into a scratch dir
// and the staged paths are pushed to CF_HDROP once the engine goes idle.
// Explorer owns its paste — there is no second step we control — so this
// one direction must materialize at copy time. The staging runs as a
// HIDDEN job: it never appears in File transfers, Running tasks, the
// status-bar badges, and never auto-opens a window (in-app pastes are
// unaffected — they use the reference clipboard and run the real action
// at paste). Large selections skip the mirror silently; the app-internal
// clipboard still works everywhere.
async function osCopyRemote(items) {
  const MAX_BYTES = 256 * 1024 * 1024;
  const MAX_ITEMS = 500;
  if (!explorerClipOn() || !items.length) return;
  const total = items.reduce((s, it) => s + (it.size || 0), 0);
  if (items.length > MAX_ITEMS || (total > 0 && total > MAX_BYTES)) return;
  let dir;
  try {
    dir = await api.StageClipboardDir();
  } catch {
    return;
  }
  // Baseline for the clobber guard: if anything writes the clipboard while
  // the staging download runs, the user copied elsewhere — the late mirror
  // must abort instead of replacing their clipboard (bug: Explorer copies
  // silently overwritten by a finished staging).
  let startSeq = null;
  try { startSeq = (await api.OsClipboardState())?.seq ?? null; } catch { /* proceed */ }
  toast('Preparing OS clipboard — staging download\u2026');
  try {
    await api.TransferCross(items, [], { kind: 'local', dir }, 'overwrite', 0, false, null, true);
  } catch (err) {
    toast(`OS clipboard staging failed: ${err}`, 'error');
    return;
  }
  // Wait for the transfer engine to finish, then push the staged paths.
  // The staged layout mirrors the transfer planner: dest/<leaf-of-key>.
  const staged = items.map((it) => `${dir}\\${(it.key || '').replace(/\/+$/, '').split('/').pop()}`);
  const t0 = Date.now();
  setTimeout(async function poll() {
    let busy = true;
    try {
      busy = (await api.ActiveTransfers()).some((j) => j.status === 'running');
    } catch {
      busy = false;
    }
    if (busy && Date.now() - t0 < 120000) { setTimeout(poll, 500); return; }
    try {
      if (startSeq != null) {
        const st = await api.OsClipboardState();
        if (st?.seq !== startSeq) return; // copied elsewhere meanwhile — theirs wins
      }
      await api.OsClipboardSetFiles(staged);
      osClipAdopt(); // our own write — re-adopt so it does not look external
      toast('Ready to paste in Explorer', 'ok');
    } catch {
      // best-effort only
    }
  }, 700);
}

// copyAsText puts rows on the OS clipboard as plain text (api.ClipboardSetText)
// — the Explorer-style "copy name / copy path / copy S3 URI / copy URL"
// actions. Ctrl+C mirrors the selection as files through hidden staging (see
// osCopyRemote); these explicit actions copy text for editors, tickets and
// terminals. what: 'name' | 'path' | 'uri' | 'url'. ctx: {kind, bucket,
// source} — 'objects' rows format bucket/key and s3://bucket/key, 'buckets'
// rows the bare bucket name (and s3://bucket), 'remote' rows the source
// path, 'local' rows the absolute path. 'uri' is the S3 CLI-interop form;
// 'url' is the REAL address (WinSCP's Copy URI purpose): the
// endpoint-resolved https URL for S3 objects, scheme://user@host[:port]/
// server-path for remote sources, file:/// for local rows.
async function copyAsText(rows, what, ctx = {}) {
  if (!rows.length) return;
  const kind = ctx.kind || nav.current?.kind || 'objects';
  const bucket = ctx.bucket ?? nav.current?.bucket;
  if (what === 'url' && (kind === 'objects' || kind === 'remote')) {
    // real addresses come from the backend: the source's endpoint,
    // addressing style and root are backend-side facts, and one call
    // carries the whole selection
    const source = ctx.source ?? nav.current?.source ?? '';
    const paths = rows.map((r) => r.key);
    try {
      const urls = kind === 'objects'
        ? await api.SourceObjectUrls(source, bucket, paths)
        : await api.RemoteUrls(source, paths);
      await api.ClipboardSetText(urls.join('\n'));
      toast(`Copied ${urls.length} URL${urls.length === 1 ? '' : 's'}`, 'ok');
    } catch (err) {
      toast(`Copy failed: ${err}`, 'error');
    }
    return;
  }
  let fmt;
  if (what === 'name') fmt = (r) => r.name;
  else if (what === 'uri') {
    if (kind === 'objects') fmt = (r) => `s3://${bucket}/${r.key}`;
    else if (kind === 'buckets') fmt = (r) => `s3://${r.key}`;
    else return;
  } else if (what === 'url') {
    // local rows: the file URL is pure path reshaping (UNC paths keep
    // the //server/share authority form)
    fmt = (r) => {
      const p = encodeURI(String(r.path).replace(/\\/g, '/'));
      return p.startsWith('//') ? `file:${p}` : `file:///${p.replace(/^\/+/, '')}`;
    };
  } else if (kind === 'objects') fmt = (r) => `${bucket}/${r.key}`;
  else if (kind === 'local') fmt = (r) => r.path;
  else fmt = (r) => r.key; // buckets: name; remote: source path
  const label = what === 'uri' ? 'URI' : what;
  try {
    await api.ClipboardSetText(rows.map(fmt).join('\n'));
    toast(`Copied ${rows.length} ${label}${rows.length === 1 ? '' : 's'}`, 'ok');
  } catch (err) {
    toast(`Copy failed: ${err}`, 'error');
  }
}

// copyAsFromMenu gathers what an Edit-menu copy-as action addresses: the
// main grid's selection, else the local pane's (mirrors setClip's
// gathering) — with the ctx copyAsText needs.
function copyAsFromMenu() {
  const loc = nav.current;
  const rows = grid.selectedRows();
  if (rows.length) return { rows, ctx: { kind: loc?.kind, bucket: loc?.bucket, source: loc?.source } };
  if (localPane.visible) {
    const b = localPane.binding;
    const lrows = localPane.grid.selectedRows();
    if (b.kind === 'local' && lrows.length) return { rows: lrows, ctx: { kind: 'local' } };
    if (b.kind === 'remote' && lrows.length) return { rows: lrows, ctx: { kind: 'remote', source: b.source } };
    if (b.kind === 's3' && localPane.bucket) {
      return { rows: lrows.filter((r) => !r.isBucket), ctx: { kind: 'objects', bucket: localPane.bucket, source: b.source } };
    }
  }
  return { rows: [], ctx: {} };
}

// canCopyUriFromMenu: the s3:// form exists only for S3-addressed selections.
function canCopyUriFromMenu() {
  const loc = nav.current;
  if (grid.selectedRows().length) return loc?.kind === 'objects' || loc?.kind === 'buckets';
  return localPane.visible && localPane.binding.kind === 's3' && !!localPane.bucket;
}

// canCopyUrlFromMenu: every pane kind has a real-address form (S3 object,
// remote path, local file) — unlike the s3:// URI, which is S3-only.
function canCopyUrlFromMenu() {
  if (grid.selectedRows().length) {
    const k = nav.current?.kind;
    return k === 'objects' || k === 'remote';
  }
  if (!localPane.visible) return false;
  const b = localPane.binding;
  const lrows = localPane.grid.selectedRows();
  if (b.kind === 's3') return !!localPane.bucket && lrows.some((r) => !r.isBucket);
  return lrows.length > 0;
}

// copySelection/cutSelection: shared by Ctrl+C/X and the Edit menu.
function copySelection() { setClip('copy'); }

function cutSelection() { setClip('cut'); }

function wireKeys() {
  document.addEventListener('keydown', (e) => {
    // modal-open keys still work (Escape handled in dialogs)
    const inInput = e.target.tagName === 'INPUT' || e.target.tagName === 'SELECT' || e.target.tagName === 'TEXTAREA';
    if (e.key === 'F1') { e.preventDefault(); helpSheet(); return; }
    if (inInput && e.key !== 'F5') return;

    const ctrl = e.ctrlKey || e.metaKey;
    if (e.altKey && e.key === 'ArrowLeft') { e.preventDefault(); if (nav.canBack()) nav.back(); return; }
    if (e.altKey && e.key === 'ArrowRight') { e.preventDefault(); if (nav.canForward()) nav.forwardGo(); return; }
    if (e.altKey && (e.key === 'ArrowUp' || e.key === 'Up')) { e.preventDefault(); const p = parentOf(nav.current); if (p) nav.to(p); return; }
    if (e.key === 'Backspace') { e.preventDefault(); const p = parentOf(nav.current); if (p) nav.to(p); return; }
    if (e.key === 'F5') { e.preventDefault(); refreshCurrent(); return; }
    if (e.key === 'F2') { e.preventDefault(); renameSelection(); return; }
    if (e.key === 'Delete') { e.preventDefault(); if (e.shiftKey) deletePermanentSelection(); else deleteSelection(); return; }
    if (e.key === 'F9') { e.preventDefault(); togglePanes(); return; }
    if (ctrl && e.key.toLowerCase() === 'a') { e.preventDefault(); grid.selectAll(); return; }
    if (ctrl && e.key.toLowerCase() === 'i') { e.preventDefault(); grid.invertSelection(); return; }
    if (ctrl && e.key.toLowerCase() === 'c') { copySelection(); return; }
    if (ctrl && e.key.toLowerCase() === 'x') { cutSelection(); return; }
    if (ctrl && e.key.toLowerCase() === 'v') { e.preventDefault(); paste(); return; }
    if (ctrl && e.shiftKey && e.key.toLowerCase() === 'f') { e.preventDefault(); findFromHere(); return; }
    if (ctrl && e.key.toLowerCase() === 'f') { e.preventDefault(); $('filter').focus(); $('filter').select(); return; }
    if (ctrl && e.key.toLowerCase() === 'l') { e.preventDefault(); toggleLogArea(); return; }
    if (ctrl && e.key.toLowerCase() === 'u') { e.preventDefault(); uploadFiles(); return; }
    if (ctrl && e.key.toLowerCase() === 's') { e.preventDefault(); saveProfileFileUi(); return; }
    if (ctrl && e.key.toLowerCase() === 'd') { e.preventDefault(); downloadSelection(); return; }
    if (ctrl && e.shiftKey && e.key.toLowerCase() === 'n') { e.preventDefault(); newFolder(); return; }
    if (e.shiftKey && e.key === 'F4') { e.preventDefault(); newFile(); return; }
    if (e.key === 'Escape') {
      // an open context menu owns Escape first: close it and keep the
      // selection (the menubar dropdown closes on Escape the same way)
      if (!$('ctxmenu').classList.contains('hidden')) { hideContextMenu(); return; }
      grid.clearSelection(); return;
    }

    // grid navigation keys (arrows, Enter, type-to-jump)
    if (grid.keydown(e)) e.preventDefault();
  });
}

// ============================ events + status ============================
function wireEvents() {
  onEvent('s3:changed', (data) => {
    const loc = nav.current;
    if (!loc) return;
    if (loc.kind === 'buckets' || data?.bucket === loc.bucket) refreshCurrent(true);
  });
  onEvent('transfer:update', (j) => {
    showTransfersBadge();
    // A finished job may have mutated the open views. Cross-source jobs
    // touch both sides; downloads write the local pane — s3:changed only
    // covers S3, so nothing else would refresh it; uploads arrive via
    // s3:changed.
    if (j?.status && j.status !== 'running') {
      const op = String(j.op || j.id || '');
      if (op.startsWith('transfer')) {
        refreshCurrent(true);
        if (localPane.visible) localPane.refresh();
      } else if (op.startsWith('download') && localPane.visible) {
        localPane.refresh();
      }
    }
  });
  onEvent('editor:saved', (d) => {
    toast(`Uploaded ${d?.key ? basename(d.key) : 'edited file'}`, 'ok');
    updateEditingStatus();
  });
  onEvent('log:line', (l) => logArea.append(l));
  // Guarded exit: the backend refused an exit that would lose work (the X
  // button or File → Exit while transfers run / the profile is dirty) and
  // asks here. "Exit anyway" force-quits through ConfirmExit.
  onEvent('exit:confirm', async (d) => {
    const okExit = await confirm({
      title: 'Exit s3b',
      message: `${d?.reason || 'Work is still in progress.'}\nExit anyway?`,
      okLabel: 'Exit anyway',
      danger: true,
    });
    if (okExit) api.ConfirmExit();
  });
  $('status-editing').onclick = () => editingDialog(updateEditingStatus);
  $('status-log').onclick = toggleLogArea;
  // The status-bar jobs indicator opens the transfer manager (the toolbar
  // transfers button is gone; the View menu carries it too); the tasks
  // indicator beside it opens the everything-monitor.
  $('status-jobs').title = 'Open the transfer manager';
  $('status-jobs').onclick = () => transferManager();
  $('status-tasks').title = 'Open the running tasks window';
  $('status-tasks').onclick = () => runningTasks();
  onEvent('tasks:update', showTasksBadge);
  window.addEventListener('focus', updateEditingStatus);
  window.addEventListener('focus', () => {
    if (refreshOnFocus && !autoRefreshBlocked()) refreshCurrent(true);
  });
  // Explorer copies arrive while the app is unfocused; refresh the
  // files-waiting flag (NOT the seq baseline — see osClipPayload) so the
  // Paste affordances light up when the window regains focus.
  window.addEventListener('focus', () => refreshOsClip());
}

function showTransfersBadge() {
  api.ActiveTransfers().then((jobs) => {
    const running = jobs.filter((j) => j.status === 'running' && !j.hidden);
    const sb = $('status-jobs');
    if (running.length) {
      sb.classList.remove('hidden');
      const j = running[0];
      const name = j.name || (j.currentFile ? basename(j.currentFile) : '') || j.id;
      let pct = 0;
      if (j.totalBytes > 0) pct = Math.floor((j.sentBytes / j.totalBytes) * 100);
      else if (j.totalFiles > 0) pct = Math.floor(((j.doneFiles + j.failedFiles + j.skippedFiles) / j.totalFiles) * 100);
      let text;
      if (running.length > 1) {
        text = `\u21C5 ${running.length} \u2014 ${name} ${pct}%`;
      } else {
        const verb = j.op === 'upload' ? t('transfer.verbUploading')
          : j.op === 'download' ? t('transfer.verbDownloading')
            : t(j.move ? 'transfer.verbMoving' : 'transfer.verbCopying');
        text = `\u21C5 ${verb} ${name} \u2014 ${pct}%`;
        if (j.speedBps > 1) text += ` @ ${fmtSpeed(j.speedBps)}`;
      }
      sb.textContent = text;
    } else {
      sb.classList.add('hidden');
    }
  });
}

// showTasksBadge mirrors the jobs indicator for the everything-monitor:
// non-transfer tasks only (transfers have their own badge, directory
// listings are transient navigation noise), click opens Running tasks;
// the active-task count is always shown.
function showTasksBadge() {
  api.RunningTasks().then((tasks) => {
    const live = tasks.filter((x) => (x.status === 'running' || x.status === 'queued') && !x.hidden
      && !['upload', 'download', 'transfer', 'list'].includes(x.kind));
    const sb = $('status-tasks');
    if (live.length) {
      sb.classList.remove('hidden');
      const x = live[0];
      // Live % on the badge: the first running task's progress at a
      // glance, without opening the window.
      let counts = '';
      if (x.totalUnits > 0) {
        const pct = Math.min(100, (x.doneUnits / x.totalUnits) * 100);
        counts = ` ${x.doneUnits}/${x.totalUnits} (${Math.floor(pct)}%)`;
      }
      sb.textContent = `\u2699 ${live.length} ${live.length === 1 ? 'task' : 'tasks'} — ${(taskKindVerb(x)?.label) || x.kind}${counts}`;
    } else {
      sb.classList.add('hidden');
    }
  });
}

function updateStatus() {
  const selRows = grid.selectedRows();
  const sel = selRows.length;
  const total = grid.rows.length;
  let text;
  if (sel) {
    // Selection summary bar (M10.3): count, folders/files, total size.
    const folders = selRows.filter((r) => r.isDir).length;
    const files = sel - folders;
    const bytes = selRows.reduce((s, r) => s + (!r.isDir ? (r.size || 0) : 0), 0);
    const parts = [];
    if (folders) parts.push(`${folders} folder(s)`);
    if (files) parts.push(`${files} file(s)`);
    if (bytes > 0) parts.push(fmtBytes(bytes));
    text = `${sel} of ${total} ${t('items')} ${t('selected')}${parts.length ? ` \u2014 ${parts.join(', ')}` : ''}`;
  } else {
    text = `${total} ${total === 1 ? t('item') : t('items')}`;
  }
  $('status-selection').textContent = text;
  updateCommandState();
}

function showEmpty(title, sub, actions = []) {
  $('empty-title').textContent = title;
  $('empty-sub').textContent = sub;
  $('empty-actions').replaceChildren(...actions);
  // An incoming empty/error state always displaces the loading one.
  $('empty-state').classList.remove('is-loading');
  $('load-skel').classList.add('hidden');
  $('empty-state').classList.remove('hidden');
}
function hideEmpty() {
  $('empty-state').classList.add('hidden');
  $('empty-state').classList.remove('is-loading');
  $('load-skel').classList.add('hidden');
}
// showLoading puts the panel in its in-flight state: spinner + skeleton
// rows while a listing is on the wire. Called on every non-silent
// navigation — the user must never see a blank panel (which reads as
// "empty folder") while data is still arriving.
function showLoading() {
  $('empty-title').textContent = t('loading');
  $('empty-sub').textContent = '';
  $('empty-actions').replaceChildren();
  $('load-skel').classList.remove('hidden');
  $('empty-state').classList.add('is-loading');
  $('empty-state').classList.remove('hidden');
}
// showListError renders a failed view load: the raw message, a title that
// classifies timeouts (the single most action-relevant failure on a bad
// connection) and a one-click Retry of the same navigation.
function showListError(err) {
  const msg = String(err?.message ?? err);
  const timedOut = /timed?\s?[\s-]?out|timeout|deadline exceeded|context deadline/i.test(msg);
  showEmpty(timedOut ? t('loadTimeout') : t('loadFailed'), msg, [
    el('button', { class: 'btn primary', text: `\u21BB ${t('retry')}`, onclick: () => refreshCurrent() }),
  ]);
}

boot();
