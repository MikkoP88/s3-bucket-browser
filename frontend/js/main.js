// S3 Bucket Browser — application shell (Explorer layout).
import { api, onEvent, subscribeStream } from './api.js';
import { el, fmtBytes, fmtDate, basename, debounce } from './util.js';
import { nav, parentOf, clipboard, clipHasItems, view } from './state.js';
import { Grid, COLUMNS, DEFAULT_COLS } from './grid.js';
import { Tree } from './tree.js';
import {
  confirm, typedConfirm, prompt, properties, doctorDialog, transferManager,
  sourceEditor, helpSheet, resolveTransferOpts, presignDialog, presignListDialog, toast, openModal,
  versionsDialog, dirVersionsDialog, markersDialog, adminDialog, editingDialog, findDialog, classDialog, lockDialog,
  usageGuideDialog, sourcesInfoDialog, importCredsDialog, pill, versionChoiceDialog,
  deleteWindow,
} from './dialogs.js';
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
});

let sources = []; // data sources of any type (M8)
let currentEntries = []; // unfiltered rows of the active view

// The view source: the S3 source the engine-native APIs (bucket/object
// admin, versions, presign, rename/delete, upload/download) address. It
// follows navigation — opening a source's buckets/objects sets it Go-side.
let viewSource = '';
let dragUrls = []; // loopback URLs for the current selection (OS drag-out)

// Streaming listing state (M5): generation counter + active stream token.
let listSeq = 0;
let listStream = { token: null, off: null };
let pendingSelect = null; // {bucket, prefix, key} — row to select after load

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
  grid.showMarkers = localStorage.getItem('s3b-show-markers') !== '0';
}

// ============================ boot ============================
async function boot() {
  setLang(detectLang());
  initTheme();
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

function initTheme() {
  const saved = localStorage.getItem('s3b-theme');
  const theme = saved || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  document.documentElement.dataset.theme = theme;
}

function toggleTheme() {
  const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = next;
  localStorage.setItem('s3b-theme', next);
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
  if (!silent) grid.clearSelection();
  updateNavButtons();
  if (!silent) renderBreadcrumb();
  tree.markCurrent(loc);
  hideEmpty();

  try {
    if (loc.kind === 'buckets') {
      const src = await setViewSourceFor(loc);
      const buckets = await api.ListBuckets();
      currentEntries = buckets.map((b) => ({
        key: b.name, name: b.name, isDir: true, size: 0,
        lastModified: b.createdAt, bucketCreated: true,
      }));
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
      await loadObjectsStream(loc, silent);
      tree.reveal(loc).catch(() => {});
      localPane.syncTo(loc.prefix || '');
      $('btn-up').disabled = false;
    } else if (loc.kind === 'remote') {
      // sftp/scp/ftp/ftps/webdav/local source browsed through its remotefs
      // engine; rows carry the same shape as S3 listings
      const entries = await api.RemoteList(loc.source, loc.path || '/');
      currentEntries = entries;
      grid.setRows(entries);
      if (!currentEntries.length) {
        showEmpty(t('emptyFolder'), 'This folder is empty', []);
      }
      tree.reveal(loc).catch(() => {});
      tree.updateRemoteDir(loc.source, loc.path || '/', entries);
      $('btn-up').disabled = !parentOf(loc);
    }
  } catch (err) {
    // A silent refresh keeps the current view on transient errors — only an
    // explicit navigation shows the error state.
    if (silent) return;
    currentEntries = [];
    grid.setRows([]);
    showEmpty('Could not list', String(err), []);
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
        if (!silent) showEmpty('Could not list', p.error, []);
        return;
      }
      if (silent) {
        buffered.push(...p.entries);
        currentEntries = buffered;
      } else {
        currentEntries.push(...p.entries);
        grid.appendRows(p.entries);
      }
      updateStatus();
      if (p.done) {
        listStream.token = null;
        listStream.off?.();
        listStream.off = null;
        if (silent) grid.setRows(buffered);
        grid.apply(); // canonical folders-first ordering + active sort/filter
        decorateVersionMarkers(loc, seq);
        if (!currentEntries.length && !view.filter) {
          showEmpty(t('emptyFolder'), t('dropToUpload'), [
            el('button', { class: 'btn primary', text: '\u2191 Upload', onclick: (ev) => openMenu(ev.currentTarget, uploadMenu(uploadFiles, uploadFolder)) }),
          ]);
        }
        consumePendingSelect();
        updateStatus();
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
    showEmpty('Could not list', String(err), []);
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
    if (!g || g.versioning !== 'Enabled') return;
    if (seq !== listSeq || grid.all.length > vmarkRowCap) return;
    const kids = await api.PrefixVersionSummary(loc.bucket, loc.prefix || '');
    if (seq !== listSeq) return; // navigated away while listing versions
    if (localStorage.getItem('s3b-show-hidden') === '1') {
      const prefix = loc.prefix || '';
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
  if (loc.kind === 'remote') {
    const atRoot = !loc.path || loc.path === '/';
    const root = el('span', { class: `crumb${atRoot ? ' current' : ''}`, text: `\u{1F5DD} ${loc.source}` });
    root.title = `${loc.source}://${loc.path || '/'}`;
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
  const root = el('span', { class: `crumb${loc.kind === 'buckets' ? ' current' : ''}`, text: `\u{1F5C2} ${srcName}` });
  root.title = `${srcName}://`;
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
    // OS drag-out: rows leave the app as downloadable loopback URLs.
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
  // name-cell badges: ⟲ opens the Versions window (files) or the Directory
  // Versions window (folders — the object-timeline dialog would paginate the
  // whole subtree); ⛔ opens the Delete Marker window for either.
  grid.on.badgeV = (row) => {
    const loc = nav.current;
    if (!loc || loc.kind !== 'objects') return;
    if (row.isDir) dirVersionsDialog(loc.bucket, row.key, refreshCurrent);
    else versionsDialog(loc.bucket, row.key, refreshCurrent);
  };
  grid.on.badgeM = (row) => {
    const loc = nav.current;
    if (!loc || loc.kind !== 'objects') return;
    markersDialog(loc.bucket, row.key, row.isDir, refreshCurrent);
  };
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
    if (xferDestOf(nav.current) && gridBodyMimes.some((t) => e.dataTransfer.types.includes(t))) {
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
// [label, kbd, fn, disabled?, cls?] or null for a separator; fn=null makes
// an inert row (menu headers like Upload). The anchor is a mouse event
// (menu at the pointer) or an element (menu below its rect).
function openMenu(anchor, items) {
  const menu = $('ctxmenu');
  menu.replaceChildren(...items.map((it) => {
    if (!it) return el('div', { class: 'sep' });
    const [label, kbd, fn, disabled, cls] = it;
    return el('div', {
      class: `item${disabled ? ' disabled' : ''}${cls ? ` ${cls}` : ''}`,
      onclick: () => { if (disabled || !fn) return; hideContextMenu(); fn(); },
    }, el('span', { text: label }), kbd ? el('span', { class: 'kbd', text: kbd }) : null);
  }));
  menu.classList.remove('hidden');
  const r = anchor?.getBoundingClientRect?.();
  const x = r ? r.left : anchor.clientX;
  const y = r ? r.bottom + 4 : anchor.clientY;
  menu.style.left = `${Math.min(x, innerWidth - 220)}px`;
  menu.style.top = `${Math.min(y, innerHeight - menu.offsetHeight - 10)}px`;
}

// ============================ context menu ============================
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
      items.push(['Paste into folder', 'Ctrl+V', () => paste(rows[0].key), !clipHasItems()]);
    }
    items.push(null);
    items.push(['Rename', 'F2', () => renameSelection(), sel !== 1]);
    items.push(['Delete\u2026', 'Del', () => deleteSelection(), !sel]);
    items.push(null);
    items.push(['New folder', 'Ctrl+Shift+N', () => newFolder()]);
    items.push(null);
    items.push(['Refresh', 'F5', () => refreshCurrent()]);
    items.push(['Properties', 'Alt+Enter', () => selectionProperties(), !sel]);
  } else {
    if (sel === 1 && rows[0].isDir) items.push(['Open', 'Enter', () => grid.on.activate(rows[0])]);
    items.push([`Download${sel ? ` (${sel})` : ''}`, 'Ctrl+D', () => downloadSelection()]);
    items.push(null);
    items.push(['Cut', 'Ctrl+X', () => cutSelection()]);
    items.push(['Copy', 'Ctrl+C', () => copySelection()]);
    items.push(['Paste', 'Ctrl+V', () => paste(), !clipHasItems() || !inObjects]);
    items.push(null);
    items.push(['Rename', 'F2', () => renameSelection(), sel !== 1]);
    items.push(['Delete\u2026', 'Del', () => deleteSelection(), !sel]);
    items.push(null);
    items.push(['New folder', 'Ctrl+Shift+N', () => newFolder()]);
    if (sel === 1 && !rows[0].isDir) {
      items.push(['Edit', '', () => editObject(rows[0])]);
    }
    if (sel && !rows.some((r) => r.isDir)) items.push(['Pre-sign URL\u2026', '', () => presign(rows)]);
    if (sel === 1) {
      // Files open the object timeline; folders the Directory Versions
      // window (bounded stats + per-child aggregates — the timeline dialog
      // would page the whole subtree and hang on "Loading…").
      items.push(['Versions\u2026', '', () => (rows[0].isDir
        ? dirVersionsDialog(loc.bucket, rows[0].key, refreshCurrent)
        : versionsDialog(loc.bucket, rows[0].key, refreshCurrent))]);
      const g = guardCache.get(guardKey(loc.source, loc.bucket));
      if (g?.versioning === 'Enabled') {
        items.push(['Delete markers\u2026', '', () => markersDialog(loc.bucket, rows[0].key, rows[0].isDir, refreshCurrent)]);
      }
    }
    if (sel) items.push(['Storage class\u2026', '', () => classDialog(loc.bucket, rows, refreshCurrent)]);
    if (sel && !rows.some((r) => r.isDir)) items.push(['Object lock\u2026', '', () => lockDialog(loc.bucket, rows, refreshCurrent)]);
    items.push(['Find in this folder\u2026', 'Ctrl+Shift+F', () => findDialog(loc.bucket, loc.prefix || '', openSearchResult)]);
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
      ['Paste here', 'Ctrl+V', () => paste('', node.bucket, { kind: 's3', source: node.source, bucket: node.bucket, dir: '' }), !(st.hasProfile && clipboard.keys.length)],
      null,
      ['Find in bucket\u2026', 'Ctrl+Shift+F', goThen(() => findDialog(node.bucket, '', openSearchResult)), !st.hasProfile],
      ['Admin panel\u2026', '', goThen(() => adminDialog(node.bucket, refreshCurrent)), !st.hasProfile],
      ['Doctor\u2026', '', goThen(() => runDoctor(node.bucket)), !st.hasProfile],
      ['Properties', '', goThen(() => bucketProperties(node.bucket)), !st.hasProfile],
      null,
      ['Delete bucket\u2026', '', goThen(() => deleteBucket(node.bucket)), !st.hasProfile],
      null,
      ['Open buckets view', '', () => nav.to({ kind: 'buckets', source: node.source })],
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
      ['Paste into folder', 'Ctrl+V', () => paste(node.path), !clipHasItems()],
      null,
      ['New folder here\u2026', 'Ctrl+Shift+N', () => newRemoteFolderIn(node.source, node.path)],
      null,
      ['Rename\u2026', 'F2', () => renameRemoteTreeFolder(node)],
      ['Delete\u2026', 'Del', () => deleteRemoteSelection(node.source, [node.path])],
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
      ['Paste here', 'Ctrl+V', () => paste('', node.bucket, { kind: 's3', source: node.source, bucket: node.bucket, dir: '' }), !(st.hasProfile && clipboard.keys.length)],
      null,
      ['Find in bucket\u2026', 'Ctrl+Shift+F', goThen(() => findDialog(node.bucket, '', openSearchResult)), !st.hasProfile],
      ['Admin panel\u2026', '', goThen(() => adminDialog(node.bucket, refreshCurrent)), !st.hasProfile],
      ['Doctor\u2026', '', goThen(() => runDoctor(node.bucket)), !st.hasProfile],
      ['Properties', '', goThen(() => bucketProperties(node.bucket)), !st.hasProfile],
      null,
      ['Delete bucket\u2026', '', goThen(() => deleteBucket(node.bucket)), !st.hasProfile],
      null,
      ['Open buckets view', '', () => nav.to({ kind: 'buckets', source: node.source })],
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
    ['Paste into folder', 'Ctrl+V', () => paste(node.prefix, node.bucket, { kind: 's3', source: node.source, bucket: node.bucket, dir: node.prefix }), !(st.hasProfile && clipboard.keys.length)],
    null,
    ['Rename\u2026', 'F2', goThen(() => renameTreeFolder(node)), !st.hasProfile],
    ['Delete\u2026', 'Del', goThen(() => deleteSelection(node.bucket, [node.prefix], node.source)), !st.hasProfile],
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
document.addEventListener('mousedown', (e) => {
  if (!e.target.closest('#ctxmenu')) hideContextMenu();
});
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

// uploadMenu builds the one Upload hierarchy shown everywhere (toolbar,
// context menus, empty states): a header with Files (Ctrl+U) and Folder
// beneath it. disabled applies to both leaves.
const uploadMenu = (filesFn, folderFn, disabled = false) => ([
  ['Upload', '', null, false, 'hdr'],
  ['Files\u2026', 'Ctrl+U', filesFn, disabled, 'sub'],
  ['Folder\u2026', '', folderFn, disabled, 'sub'],
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
    await api.TransferCross(items, localPaths, dest, res.policy, res.maxBps, move, res.decisions || null);
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
// Delete preferences (Settings → Delete): the window itself (default on —
// it shows exactly what would be removed), the typed-"delete" partition
// (default off; the destructive modes force it regardless), and
// auto-confirm (default off — single-type deletes then run unprompted).
const delWindowOn = () => localStorage.getItem('s3b-del-window') !== '0';
const delTypedOn = () => localStorage.getItem('s3b-del-typeconfirm') === '1';
const delAutoConfirm = () => localStorage.getItem('s3b-del-autoconfirm') === '1';

// S3_DEL_MODES lists a versioned bucket's delete types (the window's radio
// list). The marker delete is the safe default; keep-current and permanent
// destroy history (ladder L3) and always force the typed partition.
const S3_DEL_MODES = [
  { id: '', label: 'delm.marker', hint: 'delm.markerHint' },
  { id: 'keepcurrent', label: 'delm.keep', hint: 'delm.keepHint' },
  { id: 'permanent', label: 'delm.perm', hint: 'delm.permHint' },
];

// runDeleteWindow owns the confirmation every delete passes through.
// Multi-type sources and preset (Shift+Del) deletes always open the window —
// the user must pick, or re-verify, the destructive type there. Single-type
// deletes follow Settings: auto-confirm skips prompts, the window is the
// default, and only with both off does the classic ladder run (the typed
// word at L2 or on sources with no undo; a plain confirm otherwise).
// Resolves the confirmed mode ('' = plain) or null (canceled).
async function runDeleteWindow({ target, summary, modes = [], mode = '', classicTyped = false, classicMsg = '' }) {
  const multi = modes.length > 1;
  if (!multi && !mode && delAutoConfirm()) return '';
  if (multi || mode || delWindowOn()) {
    return deleteWindow({ target, summary, modes, mode, typedOn: delTypedOn() });
  }
  const ok = (classicTyped || summary.requiresL2)
    ? await typedConfirm({ title: target, message: classicMsg, typeWord: 'delete', okLabel: t('delw.go') })
    : await confirm({ title: target, message: classicMsg, okLabel: t('delw.go'), danger: true });
  return ok ? '' : null;
}

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
  if (mode === null) return;
  const force = p.requiresL2; // every path above already confirmed
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
    await deleteS3Keys(source, bucket, keys, presetMode, target, refreshCurrent);
  } catch (err) {
    toast(`Delete failed: ${err}`, 'error');
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
      classicTyped: true, // remote deletes have no undo: always the typed word
      classicMsg: `You are about to delete ${desc}.\nRemote sources have no trash or versions — this cannot be undone.`,
    });
    if (mode === null) return;
    const res = await api.RemoteRemove(source, keys);
    reportDeleteResult(res, (n) => `Deleted ${n} item(s)`);
    refreshCurrent();
  } catch (err) {
    toast(`Delete failed: ${err}`, 'error');
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
    const ok = await typedConfirm({
      title: `Delete bucket s3://${bucket}`,
      message: p.requiresL2
        ? `The bucket holds ${p.objectCount} object(s). Deleting removes the bucket AND all of its contents.`
        : 'The bucket is empty and will be removed.',
      typeWord: bucket,
      okLabel: 'Delete bucket',
    });
    if (!ok) return;
    const res = await api.DeleteBucket(bucket, true);
    toast(`Bucket removed (${res.deleted} object(s) emptied)`, 'ok');
    nav.to({ kind: 'buckets', source: viewSource });
  } catch (err) {
    toast(`Delete bucket failed: ${err}`, 'error');
  }
}

// paste drops the app clipboard into a destination; with the app clipboard
// empty it falls back to the OS clipboard (Ctrl+C in Explorer) — files land
// wherever the active view points, exactly like an in-app paste.
async function paste(prefixOverride, bucketOverride, destOverride) {
  const loc = nav.current;
  let dest;
  if (destOverride) dest = destOverride; // explicit target (side-pane/tree menus)
  else if (bucketOverride) dest = { kind: 's3', source: viewSource, bucket: bucketOverride, dir: prefixOverride !== undefined ? prefixOverride : '' };
  else if (prefixOverride !== undefined && loc?.kind === 'remote') dest = { kind: 'remote', source: loc.source, dir: prefixOverride };
  else dest = xferDestOf(loc) || sidePaneDest();

  if (!clipHasItems()) {
    // OS clipboard fallback: paths copied in Explorer (or any app).
    let osPaths = null;
    try { osPaths = await api.OsClipboardFiles(); } catch { /* binding missing */ }
    if (!osPaths?.length || !dest) return;
    if (dest.kind === 's3') uploadPaths(osPaths, dest.dir, dest.bucket, dest.source);
    else if (dest.kind === 'remote') uploadToRemote(osPaths, dest.source, dest.dir);
    else startTransfer({ localPaths: osPaths, dest, label: dest.dir });
    return;
  }
  if (!dest) return;

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

// ============================ drag & drop ============================
function wireDrop() {
  onEvent('wails:file-drop', (data) => {
    const paths = data?.paths || [];
    if (!paths.length) return;
    // OS-level drop: hit-test which pane sits under the cursor. The side
    // pane accepts when visible and inside a directory (its binding decides
    // the destination); otherwise the main view takes the drop.
    const hit = Number.isFinite(data?.x) && Number.isFinite(data?.y)
      ? document.elementFromPoint(data.x, data.y)
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
        toast(`${move ? 'Moving' : 'Copying'} version history — see Transfers`, 'ok');
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
      classicTyped: true, // permanent, no trash: always the typed word
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
    ...(sel === 1 && rows[0].isDir
      ? [['Paste into folder', 'Ctrl+V', () => paste(null, null, { kind: 'remote', source: b.source, dir: rows[0].key }), !clipHasItems()]]
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
    ['Paste', 'Ctrl+V', () => paste(null, null, { kind: 'remote', source: b.source, dir }), !clipHasItems()],
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
      ? [['Paste into folder', 'Ctrl+V', () => paste(null, null, { kind: 's3', source: b.source, bucket: localPane.bucket, dir: rows[0].key }), !clipHasItems()]]
      : []),
    null,
    ['Copy', 'Ctrl+C', () => copySelection(), hasBucketRow],
    ['Cut', 'Ctrl+X', () => cutSelection(), hasBucketRow],
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
    ['Paste', 'Ctrl+V', () => paste(null, null, dest), !clipHasItems() || !inBucket],
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
  $('btn-refresh').onclick = refreshCurrent;
  $('btn-upload').onclick = () => openMenu($('btn-upload'), uploadMenu(uploadFiles, uploadFolder));
  $('btn-download').onclick = () => downloadSelection();
  $('btn-panes').onclick = togglePanes;
  $('btn-find').onclick = findFromHere;
  $('btn-newfolder').onclick = newFolder;
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
  settingsDialog({
    state: {
      theme: () => (document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'),
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
      showMarkers: () => localStorage.getItem('s3b-show-markers') !== '0',
      delWindow: delWindowOn,
      delTypeConfirm: delTypedOn,
      delAutoConfirm: delAutoConfirm,
    },
    apply: {
      theme: (v) => { document.documentElement.dataset.theme = v; localStorage.setItem('s3b-theme', v); },
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
      delWindow: (v) => localStorage.setItem('s3b-del-window', v ? '1' : '0'),
      delTypeConfirm: (v) => localStorage.setItem('s3b-del-typeconfirm', v ? '1' : '0'),
      delAutoConfirm: (v) => localStorage.setItem('s3b-del-autoconfirm', v ? '1' : '0'),
    },
    log: {
      get: () => logSet,
      set: async (mode, dir, levels, scopes) => {
        try { logSet = await api.SetLogSettings(mode, dir, levels || [], scopes || []); }
        catch (e) { toast(String(e), 'error'); }
        return logSet;
      },
      browse: async () => {
        try { return await api.PickFolder('Choose the log folder'); } catch { return ''; }
      },
    },
    // Reset to defaults: wipe every persisted shell knob and reload.
    // Favorites are data, not settings — they survive. The file-log
    // preference (logsettings.json, Go-side) resets through its binding.
    reset: () => {
      const favs = localStorage.getItem('s3b-favs');
      localStorage.clear();
      if (favs !== null) localStorage.setItem('s3b-favs', favs);
      try { api.SetLogSettings('default', '', [], []); } catch { /* best effort */ }
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
        { label: t('menu.paste'), kbd: 'Ctrl+V', action: () => paste(), enabled: () => st().canPaste },
        { label: t('menu.selectAll'), kbd: 'Ctrl+A', action: () => grid.selectAll(), enabled: inObjects },
        null,
        { label: t('menu.rename'), kbd: 'F2', action: () => renameSelection(), enabled: () => st().canRename },
        { label: t('menu.delete'), kbd: 'Del', action: () => deleteSelection(), enabled: () => st().canDelete },
      ],
    },
    {
      label: t('menu.view'),
      items: [
        { label: t('menu.refresh'), kbd: 'F5', action: refreshCurrent },
        null,
        { label: t('menu.theme'), action: toggleTheme },
        { label: t('menu.panes'), kbd: 'F9', action: togglePanes },
        { label: t('menu.log'), kbd: 'Ctrl+L', action: toggleLogArea },
        { label: t('menu.transfers'), action: () => transferManager() },
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
        { label: t('menu.doctor'), action: () => runDoctor(nav.current?.kind === 'objects' ? nav.current.bucket : ''), enabled: () => st().canDoctor },
        { label: t('menu.about'), action: aboutDialog },
      ],
    },
  ];

  const mb = createMenubar(defs);
  $('menubar').replaceChildren(mb.root);
}

// aboutDialog: minimal About box — name, version, publisher, license, URL.
function aboutDialog() {
  const body = el('div', { class: 'kv' });
  const draw = (v) => {
    body.replaceChildren(
      el('div', { class: 'k', text: 's3b' }),
      el('div', { class: 'v mono', text: `v${v || '?'}` }),
      el('div', { class: 'k', text: t('menu.aboutPublisher') }),
      el('div', { class: 'v', text: 'MikkoP88' }),
      el('div', { class: 'k', text: t('menu.aboutLicense') }),
      el('div', { class: 'v', text: 'PolyForm Internal Use 1.0.0 — Copyright (c) MikkoP88' }),
      el('div', { class: 'k', text: t('menu.aboutUrl') }),
      el('div', { class: 'v mono', text: 'https://github.com/MikkoP88/s3-bucket-browser' }),
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
    // Mirror the copy onto the OS clipboard so Ctrl+V works in Explorer
    // too (cut never mirrors — an Explorer paste would move/delete).
    if (mode === 'copy') {
      osCopyRemote(rows.map((r) => (loc.kind === 'remote'
        ? { source: src, key: r.key, size: r.size || 0, isDir: !!r.isDir }
        : { source: src, bucket: loc.bucket, key: r.key, size: r.size || 0, isDir: !!r.isDir })));
    }
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
      if (mode === 'copy') api.OsClipboardSetFiles(clipboard.paths).catch(() => {});
    }
    toast(`${mode === 'cut' ? 'Cut' : 'Copied'} ${lrows.length} item(s)`);
    updateCommandState();
  }
}

// osCopyRemote mirrors a remote/S3 copy onto the OS clipboard: the selection
// is staged (downloaded) into a scratch dir via the transfer engine and the
// staged paths are pushed to CF_HDROP once the engine goes idle. Explorer
// paste then works like any local copy. Large selections skip the mirror
// silently — the app-internal clipboard still works everywhere.
async function osCopyRemote(items) {
  const MAX_BYTES = 256 * 1024 * 1024;
  const MAX_ITEMS = 500;
  if (!items.length) return;
  const total = items.reduce((s, it) => s + (it.size || 0), 0);
  if (items.length > MAX_ITEMS || (total > 0 && total > MAX_BYTES)) return;
  let dir;
  try {
    dir = await api.StageClipboardDir();
  } catch {
    return;
  }
  toast('Preparing OS clipboard — staging download\u2026');
  try {
    await api.TransferCross(items, [], { kind: 'local', dir }, 'overwrite', 0, false, null);
    showTransfersBadge();
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
      await api.OsClipboardSetFiles(staged);
      toast('Ready to paste in Explorer', 'ok');
    } catch {
      // best-effort only
    }
  }, 700);
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
    if (e.key === 'Escape') { grid.clearSelection(); return; }

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
  $('status-editing').onclick = () => editingDialog(updateEditingStatus);
  $('status-log').onclick = toggleLogArea;
  // The status-bar jobs indicator opens the transfer manager (the toolbar
  // Transfers button is gone; View menu carries it too).
  $('status-jobs').title = 'Open the transfer manager';
  $('status-jobs').onclick = () => transferManager();
  window.addEventListener('focus', updateEditingStatus);
  window.addEventListener('focus', () => {
    if (refreshOnFocus && !autoRefreshBlocked()) refreshCurrent(true);
  });
}

function showTransfersBadge() {
  api.ActiveTransfers().then((jobs) => {
    const running = jobs.filter((j) => j.status === 'running');
    const sb = $('status-jobs');
    if (running.length) {
      sb.classList.remove('hidden');
      const j = running[0];
      sb.textContent = `\u21C5 ${running.length > 1 ? `${running.length} jobs — ` : ''}${j.doneFiles}/${j.totalFiles} ${j.currentFile ? basename(j.currentFile) : ''} ${fmtBytes(j.sentBytes)}${j.totalBytes ? '/' + fmtBytes(j.totalBytes) : ''}`;
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
  $('empty-state').classList.remove('hidden');
}
function hideEmpty() { $('empty-state').classList.add('hidden'); }

boot();
