// S3 Bucket Browser — application shell (Explorer layout, PLAN.md §11).
import { api, onEvent } from './api.js';
import { el, fmtBytes, basename, debounce } from './util.js';
import { nav, parentOf, clipboard, view } from './state.js';
import { Grid } from './grid.js';
import { Tree } from './tree.js';
import {
  confirm, typedConfirm, prompt, properties, doctorDialog, transferManager,
  sourceEditor, helpSheet, conflictPolicy, presignDialog, toast, openModal,
  versionsDialog, adminDialog, editingDialog, findDialog, classDialog, lockDialog,
} from './dialogs.js';
import { LocalPane, aggregateCompare } from './local.js';
import { t, detectLang, setLang } from './i18n.js';
import { setCommandContext, updateCommandState, commandState } from './commands.js';
import { createMenubar } from './menubar.js';
import { createLogArea } from './logarea.js';

const $ = (id) => document.getElementById(id);

const grid = new Grid();
const localPane = new LocalPane();
const tree = new Tree({ onNavigate: (loc) => nav.to(loc), onDropTo: dropToTarget, onContext: showTreeMenu });
const logArea = createLogArea();

setCommandContext({
  selectionCount: () => grid.selectedRows().length,
  // Browsing still means S3; remote-filesystem engines land with M9.
  hasProfile: () => sources.some((s) => s.type === 's3'),
});

let sources = []; // data sources of any type (M8)
let currentEntries = []; // unfiltered rows of the active view

// Streaming listing state (M5): generation counter + active stream token.
let listSeq = 0;
let listStream = { token: null, off: null };
let pendingSelect = null; // {bucket, prefix, key} — row to select after load

// ============================ boot ============================
async function boot() {
  setLang(detectLang());
  initTheme();
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

  // Auto refresh: restore interval + refresh-on-focus from the last session.
  const ar = parseInt(localStorage.getItem('s3b-autorefresh') || '0', 10);
  if (ar > 0) setAutoRefresh(ar);
  refreshOnFocus = localStorage.getItem('s3b-refresh-focus') === '1';

  const ok = await refreshSources();
  if (ok) nav.to({ kind: 'buckets' });
  refreshPfState(); // container sessions do not survive restarts; defensive
  if (localStorage.getItem('s3b-panes') === '1') localPane.show();
  updateCommandState();
}

// toggleLogArea shows/hides the bottom log drawer (View menu, Ctrl+L,
// status-bar button) and remembers the choice.
function toggleLogArea() {
  const elx = $('logarea');
  const open = elx.classList.toggle('hidden') === false;
  localStorage.setItem('s3b-log', open ? '1' : '0');
}

// ============================ auto refresh ============================
let autoTimer = null;
let autoRefreshMs = 0;
let refreshOnFocus = false;

// autoRefreshBlocked: conditions under which a background refresh must not
// fire — a modal or context menu is open, or transfers are running (the
// transfer badge is visible exactly then; uploads also refresh views via
// s3:changed on their own).
function autoRefreshBlocked() {
  if (!$('modal-root').classList.contains('hidden')) return true;
  if (!$('ctxmenu').classList.contains('hidden')) return true;
  if (!$('transfer-badge').classList.contains('hidden')) return true;
  return false;
}

function autoTick() {
  if (document.hidden || autoRefreshBlocked()) return;
  refreshCurrent();
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

// refreshSources reloads all data sources (an open Profile file wins over
// the local store), rebuilds the s3 dropdown, and shows onboarding when
// there is nothing to browse with yet.
async function refreshSources() {
  try {
    sources = await api.ListSources();
  } catch (err) {
    toast(`Sources: ${err}`, 'error');
    sources = [];
  }
  const s3srcs = sources.filter((s) => s.type === 's3');
  const sel = $('profile-select');
  sel.replaceChildren(...s3srcs.map((s) =>
    el('option', { value: s.name }, `${s.default ? '\u2605 ' : ''}${s.name} (${providerLabel(s.s3?.endpoint)})`)));
  const def = s3srcs.find((s) => s.default) || s3srcs[0];
  if (def) sel.value = def.name;
  $('status-profile').textContent = def ? def.name : '';
  tree.setSources(sources, nav.current); // M9: sources are the tree's top level
  if (!sources.length) {
    showOnboarding();
    return false;
  }
  updateCommandState();
  return true;
}

function showOnboarding() {
  nav.replace({ kind: 'onboarding' });
  $('sidebar-head').textContent = t('buckets');
  tree.container.replaceChildren();
  renderBreadcrumb();
  showEmpty(t('noSources'), t('noSourcesSub'), [
    el('button', { class: 'btn primary', text: t('addSource'), onclick: () => sourceEditor(null, afterSourceSaved) }),
    el('button', { class: 'btn', text: t('importAws'), onclick: importAws }),
  ]);
}

async function importAws() {
  try {
    const res = await api.ImportAwsCredentials();
    const msg = res.imported.length
      ? `Imported: ${res.imported.join(', ')}${res.skipped.length ? ` — skipped: ${res.skipped.join(', ')}` : ''}`
      : res.skipped.length ? `Skipped: ${res.skipped.join(', ')}` : 'Nothing found in ~/.aws/credentials';
    toast(msg, res.imported.length ? 'ok' : '');
    if (res.imported.length) {
      await refreshSources();
      nav.to({ kind: 'buckets' });
    }
  } catch (err) {
    toast(`Import failed: ${err}`, 'error');
  }
}

function afterSourceSaved() {
  refreshSources().then((ok) => {
    if (ok && sources.some((s) => s.type === 's3')) nav.to({ kind: 'buckets' });
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
    title: `s3://${b}`,
    onclick: () => nav.to({ kind: 'objects', bucket: b, prefix: '' }),
  },
    el('span', { class: 'fav-star', text: '\u2605' }),
    el('span', { class: 'fav-label', text: b }),
  )));
}

async function loadView(loc) {
  grid.clearSelection();
  updateNavButtons();
  renderBreadcrumb();
  tree.markCurrent(loc);
  hideEmpty();

  try {
    if (loc.kind === 'buckets') {
      $('sidebar-head').textContent = t('buckets');
      const buckets = await api.ListBuckets();
      currentEntries = buckets.map((b) => ({
        key: b.name, name: b.name, isDir: true, size: 0,
        lastModified: b.createdAt, bucketCreated: true,
      }));
      grid.setRows(currentEntries);
      renderFavorites();
      if (!currentEntries.length) showEmpty(t('noBuckets'), t('noBucketsSub'), [
        el('button', { class: 'btn primary', text: t('createBucket'), onclick: createBucket }),
      ]);
      else tree.refresh(buckets, loc);
      $('btn-up').disabled = true;
    } else if (loc.kind === 'objects') {
      $('sidebar-head').textContent = loc.bucket;
      await loadObjectsStream(loc);
      tree.reveal(loc).catch(() => {});
      localPane.syncTo(loc.prefix || '');
      $('btn-up').disabled = false;
    } else if (loc.kind === 'srcroot') {
      // non-default S3 source: read-only bucket listing (its object
      // operations route through the default profile until multi-source
      // transfers land)
      $('sidebar-head').textContent = loc.source;
      const buckets = await api.ListSourceBuckets(loc.source);
      currentEntries = buckets.map((b) => ({
        key: b.name, name: b.name, isDir: true, size: 0,
        lastModified: b.createdAt, bucketCreated: true,
      }));
      grid.setRows(currentEntries);
      if (!currentEntries.length) {
        const src = sources.find((s) => s.name === loc.source);
        showEmpty('No buckets', `${src?.s3?.endpoint || 'This endpoint'} has no buckets yet`, []);
      }
      $('btn-up').disabled = true;
    } else if (loc.kind === 'remote') {
      // sftp/scp/ftp/ftps/local source browsed through its remotefs
      // engine; rows carry the same shape as S3 listings
      $('sidebar-head').textContent = loc.source;
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
    currentEntries = [];
    grid.setRows([]);
    showEmpty('Could not list', String(err), []);
  }
  updateStatus();
}

// loadObjectsStream fills the grid incrementally from the streaming
// listing API: the first page renders immediately, Go-side memory stays
// at one page for million-object folders, and stale streams are
// canceled on navigation (PLAN.md §13, M5).
async function loadObjectsStream(loc) {
  const seq = ++listSeq;
  cancelListStream();
  if (pendingSelect && (pendingSelect.bucket !== loc.bucket || pendingSelect.prefix !== (loc.prefix || ''))) {
    pendingSelect = null; // user navigated elsewhere
  }
  currentEntries = [];
  grid.setRows([]);
  grid.setFilter(view.filter);
  let token;
  try {
    token = await api.ListObjectsStream(loc.bucket, loc.prefix || '');
  } catch (err) {
    if (seq !== listSeq) return;
    showEmpty('Could not list', String(err), []);
    return;
  }
  if (seq !== listSeq) { api.CancelList(token).catch(() => {}); return; }
  listStream.token = token;
  listStream.off = onEvent('list:page', (p) => {
    if (seq !== listSeq || p.token !== token) return;
    if (p.error) {
      showEmpty('Could not list', p.error, []);
      return;
    }
    currentEntries.push(...p.entries);
    grid.appendRows(p.entries);
    updateStatus();
    if (p.done) {
      listStream.token = null;
      listStream.off?.();
      listStream.off = null;
      grid.apply(); // canonical folders-first ordering + active sort/filter
      if (!currentEntries.length && !view.filter) {
        showEmpty(t('emptyFolder'), t('dropToUpload'), [
          el('button', { class: 'btn primary', text: '\u2191 Upload', onclick: showUploadMenu }),
        ]);
      }
      consumePendingSelect();
      updateStatus();
    }
  });
}

function cancelListStream() {
  if (listStream.token) { api.CancelList(listStream.token).catch(() => {}); listStream.token = null; }
  listStream.off?.();
  listStream.off = null;
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
  nav.to({ kind: 'objects', bucket: loc.bucket, prefix: loc.prefix });
}

function refreshCurrent() {
  if (nav.current) loadView({ ...nav.current });
}

function updateNavButtons() {
  $('btn-back').disabled = !nav.canBack();
  $('btn-forward').disabled = !nav.canForward();
}

function renderBreadcrumb() {
  const bc = $('breadcrumb');
  bc.replaceChildren();
  const loc = nav.current || { kind: 'buckets' };
  if (loc.kind === 'srcroot' || loc.kind === 'remote') {
    const icon = loc.kind === 'srcroot' ? '\u{1F5C2}' : '\u{1F5DD}';
    const atRoot = loc.kind === 'srcroot' || !loc.path;
    const root = el('span', { class: `crumb${atRoot ? ' current' : ''}`, text: `${icon} ${loc.source}` });
    root.onclick = () => nav.to(loc.kind === 'srcroot'
      ? { kind: 'srcroot', source: loc.source }
      : { kind: 'remote', source: loc.source, path: '' });
    bc.appendChild(root);
    if (loc.kind === 'remote' && loc.path) {
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
  const root = el('span', { class: `crumb${loc.kind === 'buckets' ? ' current' : ''}`, text: '\u{1F5C2} S3' });
  root.onclick = () => nav.to({ kind: 'buckets' });
  bc.appendChild(root);
  if (loc.kind !== 'objects') return;
  bc.appendChild(el('span', { class: 'crumb-sep', text: '\u203A' }));
  const b = el('span', { class: 'crumb', text: loc.bucket });
  b.onclick = () => nav.to({ kind: 'objects', bucket: loc.bucket, prefix: '' });
  bc.appendChild(b);
  let acc = '';
  for (const part of (loc.prefix || '').split('/')) {
    if (!part) continue;
    acc += part + '/';
    bc.appendChild(el('span', { class: 'crumb-sep', text: '\u203A' }));
    const c = el('span', { class: 'crumb', text: part });
    const target = acc;
    c.onclick = () => nav.to({ kind: 'objects', bucket: loc.bucket, prefix: target });
    bc.appendChild(c);
  }
  bc.lastChild?.classList.add('current');
}

// ============================ grid wiring ============================
function wireGrid() {
  grid.on.select = updateStatus;
  grid.on.activate = (row) => {
    const loc = nav.current;
    if (loc.kind === 'srcroot') {
      toast('Set this source as default (dropdown or "Use") to manage its objects — per-source transfers ship next');
      return;
    }
    if (loc.kind === 'remote') {
      if (row.isDir) nav.to({ kind: 'remote', source: loc.source, path: row.key });
      else toast('File operations on remote sources arrive with cross-source transfers');
      return;
    }
    if (loc.kind === 'buckets') {
      nav.to({ kind: 'objects', bucket: row.key, prefix: '' });
    } else if (row.isDir) {
      nav.to({ kind: 'objects', bucket: loc.bucket, prefix: row.key });
    } else {
      downloadSelection([{ key: row.key, size: row.size, name: row.name }]);
    }
  };
  grid.on.context = (e, rows) => showContextMenu(e, rows);
  grid.on.drop = (targetRow, data, e) => {
    const loc = nav.current;
    if (loc.kind !== 'objects') return;
    dropToTarget({ bucket: loc.bucket, prefix: targetRow.key }, data, e);
  };
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
  // sidebar background right-click: profile + tree management
  $('tree').addEventListener('contextmenu', (e) => {
    if (e.target.closest('.tnode')) return; // node menu lands with sidebar parity (M7.9)
    e.preventDefault();
    openMenu(e, [
      [t('addSource'), '', () => sourceEditor(null, afterSourceSaved)],
      null,
      [t('importAws'), '', () => importAws()],
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
  localPane.on.openFail = (e) => toast(`Local: ${e}`, 'error');
  localPane.on.dropFolder = (folder, payload) => downloadRefs(payload.entries, folder.path);
  localPane.on.dropBody = (payload) => downloadRefs(payload.entries, localPane.dir);
  localPane.on.contextEmpty = (e, dir) => {
    openMenu(e, [
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
// [label, kbd, fn, disabled?] or null for a separator. The anchor is a mouse
// event (menu at the pointer) or an element (menu below its rect).
function openMenu(anchor, items) {
  const menu = $('ctxmenu');
  menu.replaceChildren(...items.map((it) => {
    if (!it) return el('div', { class: 'sep' });
    const [label, kbd, fn, disabled] = it;
    return el('div', {
      class: `item${disabled ? ' disabled' : ''}`,
      onclick: () => { hideContextMenu(); fn(); },
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
    items.push(['Open', 'Enter', () => nav.to({ kind: 'objects', bucket: b.key, prefix: '' })]);
    items.push([isFavorite(b.key) ? '\u2605 Remove from favorites' : '\u2606 Add to favorites', '', () => toggleFavorite(b.key)]);
    items.push(['Find in bucket\u2026', '', () => findDialog(b.key, '', openSearchResult)]);
    items.push(['Admin panel\u2026', '', () => adminDialog(b.key, refreshCurrent)]);
    items.push(['Doctor\u2026', '', async () => runDoctor(b.key)]);
    items.push(['Properties', '', () => bucketProperties(b.key)]);
    items.push(null);
    items.push(['Delete bucket\u2026', '', () => deleteBucket(b.key)]);
  } else if (loc.kind === 'remote') {
    // Remote sources: the engine-native operations (transfers arrive with
    // the cross-source matrix).
    if (sel === 1 && rows[0].isDir) items.push(['Open', 'Enter', () => grid.on.activate(rows[0])]);
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
    items.push(['Cut', 'Ctrl+X', () => { clipboard.mode = 'cut'; clipboard.bucket = loc.bucket; clipboard.keys = rows.map((r) => r.key); toast(`Cut ${rows.length} item(s)`); updateCommandState(); }]);
    items.push(['Copy', 'Ctrl+C', () => { clipboard.mode = 'copy'; clipboard.bucket = loc.bucket; clipboard.keys = rows.map((r) => r.key); toast(`Copied ${rows.length} item(s)`); updateCommandState(); }]);
    items.push(['Paste', 'Ctrl+V', () => paste(), !clipboard.keys.length || !inObjects]);
    items.push(null);
    items.push(['Rename', 'F2', () => renameSelection(), sel !== 1]);
    items.push(['Delete\u2026', 'Del', () => deleteSelection(), !sel]);
    items.push(null);
    items.push(['New folder', 'Ctrl+Shift+N', () => newFolder()]);
    if (sel === 1 && !rows[0].isDir) {
      items.push(['Edit', '', () => editObject(rows[0])]);
      items.push(['Pre-sign URL\u2026', '', () => presign(rows[0])]);
    }
    if (sel === 1) items.push(['Previous versions\u2026', '', () => versionsDialog(loc.bucket, rows[0].key, refreshCurrent)]);
    if (sel) items.push(['Storage class\u2026', '', () => classDialog(loc.bucket, rows, refreshCurrent)]);
    if (sel === 1 && !rows[0].isDir) items.push(['Object lock\u2026', '', () => lockDialog(loc.bucket, rows[0].key, refreshCurrent)]);
    items.push(['Find in this folder\u2026', 'Ctrl+Shift+F', () => findDialog(loc.bucket, loc.prefix || '', openSearchResult)]);
    items.push(['Delete permanently\u2026', 'Shift+Del', () => deletePermanentSelection(), !sel]);
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
      ['Import ~/.aws/credentials\u2026', '', () => importAws()],
      null,
      ['Refresh', 'F5', () => refreshCurrent()],
    ]);
    return;
  }
  if (loc.kind === 'remote') {
    openMenu(e, [
      ['New folder', 'Ctrl+Shift+N', () => newFolder(), !st.canNewFolder],
      null,
      ['Select all', 'Ctrl+A', () => grid.selectAll()],
      ['Refresh', 'F5', () => refreshCurrent()],
      ['Properties', '', () => folderProperties()],
    ]);
    return;
  }
  openMenu(e, [
    ['Paste', 'Ctrl+V', () => paste(), !st.canPaste],
    null,
    ['Upload files\u2026', 'Ctrl+U', () => uploadFiles(), !st.canUpload],
    ['Upload folder\u2026', '', () => uploadFolder(), !st.canUpload],
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
function folderProperties() {
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
  properties(`Properties — s3://${loc.bucket}/${loc.prefix || ''}`, [
    ['Folders', rows.length - files.length],
    ['Files', files.length],
    ['Total size', fmtBytes(bytes)],
    ...(view.filter ? [['Name filter', view.filter]] : []),
  ]);
}

// showTreeMenu gives sidebar nodes (buckets and folders) context-menu parity
// with grid rows. node = {bucket, prefix, label} from the Tree, or a
// {kind:'rdir', source, path, label} remote directory node.
function showTreeMenu(e, node) {
  const st = commandState();
  if (node.kind === 'rdir') {
    openMenu(e, [
      ['Open', '', () => nav.to({ kind: 'remote', source: node.source, path: node.path })],
      null,
      ['New folder here\u2026', 'Ctrl+Shift+N', () => newRemoteFolderIn(node.source, node.path)],
      null,
      ['Rename\u2026', 'F2', () => renameRemoteTreeFolder(node)],
      ['Delete\u2026', 'Del', () => deleteRemoteSelection(node.source, [node.path])],
    ]);
    return;
  }
  const go = () => nav.to({ kind: 'objects', bucket: node.bucket, prefix: node.prefix });
  if (node.prefix === '') {
    openMenu(e, [
      ['Open', '', go],
      [isFavorite(node.bucket) ? '\u2605 Remove from favorites' : '\u2606 Add to favorites', '', () => toggleFavorite(node.bucket), !st.hasProfile],
      null,
      ['Upload files here\u2026', 'Ctrl+U', () => uploadFilesTo(node.bucket, ''), !st.hasProfile],
      ['Upload folder here\u2026', '', () => uploadFolderTo(node.bucket, ''), !st.hasProfile],
      ['Paste here', 'Ctrl+V', () => paste('', node.bucket), !(st.hasProfile && clipboard.keys.length)],
      null,
      ['Find in bucket\u2026', 'Ctrl+Shift+F', () => findDialog(node.bucket, '', openSearchResult), !st.hasProfile],
      ['Admin panel\u2026', '', () => adminDialog(node.bucket, refreshCurrent), !st.hasProfile],
      ['Doctor\u2026', '', () => runDoctor(node.bucket), !st.hasProfile],
      ['Properties', '', () => bucketProperties(node.bucket), !st.hasProfile],
      null,
      ['Delete bucket\u2026', '', () => deleteBucket(node.bucket), !st.hasProfile],
    ]);
    return;
  }
  const clipCopy = () => { clipboard.mode = 'copy'; clipboard.bucket = node.bucket; clipboard.keys = [node.prefix]; toast(`Copied ${node.label}`); updateCommandState(); };
  const clipCut = () => { clipboard.mode = 'cut'; clipboard.bucket = node.bucket; clipboard.keys = [node.prefix]; toast(`Cut ${node.label}`); updateCommandState(); };
  openMenu(e, [
    ['Open', '', go],
    null,
    ['Upload files here\u2026', 'Ctrl+U', () => uploadFilesTo(node.bucket, node.prefix), !st.hasProfile],
    ['Upload folder here\u2026', '', () => uploadFolderTo(node.bucket, node.prefix), !st.hasProfile],
    ['Download\u2026', '', () => downloadTreeEntry(node), !st.hasProfile],
    null,
    ['Copy', 'Ctrl+C', clipCopy, !st.hasProfile],
    ['Cut', 'Ctrl+X', clipCut, !st.hasProfile],
    ['Paste into folder', 'Ctrl+V', () => paste(node.prefix, node.bucket), !(st.hasProfile && clipboard.keys.length)],
    null,
    ['Rename\u2026', 'F2', () => renameTreeFolder(node), !st.hasProfile],
    ['Delete\u2026', 'Del', () => deleteSelection(node.bucket, [node.prefix]), !st.hasProfile],
    null,
    ['Find here\u2026', 'Ctrl+Shift+F', () => findDialog(node.bucket, node.prefix, openSearchResult), !st.hasProfile],
    ['Properties', '', () => treeProperties(node), !st.hasProfile],
  ]);
}

async function uploadFilesTo(bucket, prefix) {
  const paths = await api.PickUploadFiles();
  if (paths?.length) uploadPaths(paths, prefix, bucket);
}

async function uploadFolderTo(bucket, prefix) {
  const dir = await api.PickFolder('Choose a folder to upload');
  if (dir) uploadPaths([dir], prefix, bucket);
}

async function downloadTreeEntry(node) {
  const dest = await api.PickFolder('Choose download folder');
  if (!dest) return;
  await downloadRefs([{ key: node.prefix, size: 0, isDir: true }], dest, node.bucket);
}

async function renameTreeFolder(node) {
  const name = await prompt({ title: 'Rename', label: 'New name', value: node.label });
  if (!name || name === node.label) return;
  try {
    await api.RenameObject(node.bucket, node.prefix, name);
    toast('Renamed', 'ok'); // s3:changed refreshes the view + tree
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
    const st = await api.StatObject(node.bucket, node.prefix);
    properties(`Properties — ${node.label}`, [
      ['Type', 'Folder'],
      ['Objects', st.usage.objectCount],
      ['Total size', fmtBytes(st.usage.totalBytes)],
      ['s3:// URI', `s3://${node.bucket}/${node.prefix}`],
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
async function uploadPaths(paths, prefixOverride, bucketOverride) {
  const loc = nav.current;
  const bucket = bucketOverride || loc?.bucket;
  if (!bucket || (!bucketOverride && loc.kind !== 'objects')) { toast('Open a bucket first'); return; }
  const prefix = prefixOverride !== undefined ? prefixOverride : (loc.prefix || '');
  const res = await conflictPolicy('upload', `s3://${bucket}/${prefix || ''}`);
  if (!res) return;
  try {
    await api.Upload(paths, bucket, prefix, res.policy, res.maxBps);
    toast(`Uploading ${paths.length} item(s)\u2026`);
    showTransfersBadge();
  } catch (err) {
    toast(`Upload failed: ${err}`, 'error');
  }
}

async function uploadFiles() {
  const paths = await api.PickUploadFiles();
  if (paths?.length) uploadPaths(paths);
}

async function uploadFolder() {
  const dir = await api.PickFolder('Choose a folder to upload');
  if (dir) uploadPaths([dir]);
}

// showUploadMenu is the single Upload command: a small menu under the button
// offering the native file picker (Ctrl+U) and the folder picker. The backend
// walks directories either way; drag & drop needs no picker at all.
function showUploadMenu(e) {
  openMenu(e.currentTarget || e, [
    ['Files\u2026', 'Ctrl+U', uploadFiles],
    ['Folder\u2026', '', uploadFolder],
  ]);
}

async function downloadSelection(overrideRows) {
  const loc = nav.current;
  if (loc.kind !== 'objects') return;
  const rows = overrideRows || grid.selectedRows();
  if (!rows.length) { toast('Select objects to download'); return; }
  const refs = rows.map((r) => ({ key: r.key, size: r.size || 0, isDir: !!r.isDir }));
  const dest = await api.PickFolder('Choose download folder');
  if (!dest) return;
  await downloadRefs(refs, dest);
}

// downloadRefs downloads mixed file/folder refs into dest — folders expand
// recursively on the backend (dual-pane drops, Ctrl+D with folders selected).
async function downloadRefs(entries, dest, bucketOverride) {
  const loc = nav.current;
  const bucket = bucketOverride || loc.bucket;
  if (!entries?.length || !bucket) return;
  const res = await conflictPolicy('download', dest);
  if (!res) return;
  try {
    await api.DownloadRefs(bucket, entries, dest, res.policy, res.maxBps);
    toast(`Downloading ${entries.length} item(s)\u2026`);
    showTransfersBadge();
  } catch (err) {
    toast(`Download failed: ${err}`, 'error');
  }
}

async function deleteSelection(bucketOverride, keysOverride) {
  const loc = nav.current;
  if (!bucketOverride && loc.kind === 'remote') {
    deleteRemoteSelection();
    return;
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
  try {
    const p = await api.PreviewDelete(bucket, keys);
    const desc = `${p.count} object(s)${p.bytes ? ` (${fmtBytes(p.bytes)})` : ''}${p.folders ? ` in ${p.folders} folder(s)` : ''}`;
    let ok;
    if (p.requiresL2) {
      ok = await typedConfirm({
        title: `Delete from s3://${bucket}`,
        message: `You are about to delete ${desc}.\nThis cannot be undone.`,
        typeWord: 'delete',
      });
    } else {
      ok = await confirm({
        title: `Delete from s3://${bucket}`,
        message: `Delete ${desc}? This cannot be undone.`,
        okLabel: 'Delete',
        danger: true,
      });
    }
    if (!ok) return;
    const res = await api.DeleteSelection(bucket, keys, p.requiresL2);
    if (res.errors?.length) toast(`${res.deleted} deleted, errors: ${res.errors.slice(0, 3).join('; ')}`, 'error');
    else toast(`Deleted ${res.deleted} object(s)`, 'ok');
    refreshCurrent();
  } catch (err) {
    toast(`Delete failed: ${err}`, 'error');
  }
}

async function editObject(row) {
  const loc = nav.current;
  try {
    await api.EditObject(loc.bucket, row.key);
    toast(`Opening ${row.name} — saves upload automatically`, 'ok');
    updateEditingStatus();
  } catch (err) {
    toast(`Edit failed: ${err}`, 'error');
  }
}

// Shift+Del: destroy the selection including all versions and delete markers
// (safety ladder L3, PLAN.md §9).
async function deletePermanentSelection() {
  const loc = nav.current;
  if (loc.kind !== 'objects') return;
  const rows = grid.selectedRows();
  if (!rows.length) return;
  const ok = await typedConfirm({
    title: 'Delete permanently',
    message: `Every version of ${rows.length} object(s) will be destroyed.\nUnlike Delete, this erases version history too — it cannot be undone.`,
    typeWord: 'permanent',
  });
  if (!ok) return;
  try {
    let failed = 0;
    for (const r of rows) {
      try {
        await api.DeleteObjectPermanently(loc.bucket, r.key);
      } catch (e) {
        failed++;
        toast(`${r.name}: ${e}`, 'error');
      }
    }
    if (!failed) toast(`Destroyed ${rows.length} object(s) including all versions`, 'ok');
    refreshCurrent();
  } catch (err) {
    toast(`Permanent delete failed: ${err}`, 'error');
  }
}

// compareDirs compares the local pane folder against the remote prefix and
// decorates both grids (WinSCP-style keep-in-sync).
async function compareDirs() {
  const loc = nav.current;
  if (loc?.kind !== 'objects') { toast('Open a bucket folder to compare against'); return; }
  if (!localPane.dir) { toast('Local pane is at filesystem roots — open a folder first'); return; }
  try {
    const rows = await api.CompareDir(localPane.dir, loc.bucket, loc.prefix || '');
    localPane.setCompare(rows);
    grid.setCmp(aggregateCompare(rows));
    const n = (s) => rows.filter((r) => r.status === s).length;
    properties(`Compare — ${localPane.dir} \u2194 s3://${loc.bucket}/${loc.prefix || ''}`, [
      ['Identical', n('same')],
      ['Only local (to upload)', n('only-local')],
      ['Only remote (to download)', n('only-remote')],
      ['Newer local', n('newer-local')],
      ['Newer remote', n('newer-remote')],
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

function togglePanes() {
  const on = !localPane.visible;
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

// deleteRemoteSelection: count-then-act delete on a remote source. Remote
// filesystems have no trash and no versions — the typed confirm says so.
async function deleteRemoteSelection(overrideSource, overrideKeys) {
  const loc = nav.current;
  const source = overrideSource || loc?.source;
  if (!source) return;
  const keys = overrideKeys || grid.selectedRows().map((r) => r.key);
  if (!keys.length) return;
  try {
    const p = await api.RemoteDeletePreview(source, keys);
    const desc = `${p.files} file(s), ${p.folders} folder(s)${p.bytes ? ` (${fmtBytes(p.bytes)})` : ''}`;
    if (p.errors?.length) toast(`Warning: ${p.errors.slice(0, 2).join('; ')}`, 'error');
    const ok = await typedConfirm({
      title: `Delete from ${source}`,
      message: `You are about to delete ${desc}.\nRemote sources have no trash or versions — this cannot be undone.`,
      typeWord: 'delete',
      okLabel: 'Delete',
    });
    if (!ok) return;
    const res = await api.RemoteRemove(source, keys);
    if (res.errors?.length) toast(`${res.deleted} deleted, errors: ${res.errors.slice(0, 3).join('; ')}`, 'error');
    else toast(`Deleted ${res.deleted} item(s)`, 'ok');
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
    nav.to({ kind: 'buckets' });
  } catch (err) {
    toast(`Delete bucket failed: ${err}`, 'error');
  }
}

async function paste(prefixOverride, bucketOverride) {
  const loc = nav.current;
  const bucket = bucketOverride || loc.bucket;
  if (!bucket || (!bucketOverride && loc.kind !== 'objects') || !clipboard.keys.length) return;
  const prefix = prefixOverride !== undefined ? prefixOverride : (loc.prefix || '');
  const move = clipboard.mode === 'cut';
  try {
    const res = await api.CopySelection(clipboard.bucket, clipboard.keys, bucket, prefix, move);
    if (res.errors?.length) toast(`Errors: ${res.errors.slice(0, 3).join('; ')}`, 'error');
    else toast(`${move ? 'Moved' : 'Copied'} ${res.copied} item(s)`, 'ok');
    if (move) clipboard.keys = [];
    updateCommandState();
    refreshCurrent();
  } catch (err) {
    toast(`Paste failed: ${err}`, 'error');
  }
}

async function presign(row) {
  const loc = nav.current;
  try {
    const url = await api.PresignObject(loc.bucket, row.key, 3600);
    presignDialog(url);
  } catch (err) {
    toast(`Presign failed: ${err}`, 'error');
  }
}

async function selectionProperties() {
  const loc = nav.current;
  const row = grid.selectedRows()[0];
  if (!row) return;
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
      ['s3:// URI', `s3://${loc.bucket}/${row.key}`],
    ];
    properties(`Properties — ${st.name}`, rows);
  } catch (err) {
    toast(`Properties failed: ${err}`, 'error');
  }
}

async function bucketProperties(bucket) {
  try {
    const st = await api.StatBucket(bucket);
    properties(`Properties — ${bucket}`, [['Name', bucket], ['Region', st.region], ['s3:// URI', `s3://${bucket}`]]);
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
    if (paths.length) uploadPaths(paths);
  });
}

async function dropToTarget(target, data, e) {
  if (data.paths?.length) { // dragged in from the local pane → upload
    uploadPaths(data.paths, target.prefix);
    return;
  }
  const srcBucket = data.bucket;
  if (!srcBucket || !data.keys?.length) return;
  const move = srcBucket === target.bucket
    ? !e.ctrlKey || e.shiftKey   // same bucket: move (Shift forces)
    : e.shiftKey;                // cross bucket: copy (Shift forces move)
  try {
    const res = await api.CopySelection(srcBucket, data.keys, target.bucket, target.prefix || '', move);
    if (res.errors?.length) toast(`Errors: ${res.errors.slice(0, 3).join('; ')}`, 'error');
    else toast(`${move ? 'Moved' : 'Copied'} ${res.copied} object(s)`, 'ok');
    refreshCurrent();
  } catch (err) {
    toast(`Drag & drop ${move ? 'move' : 'copy'} failed: ${err}`, 'error');
  }
}

// Patch grid drag payload to include the source bucket.
const origDragPayload = grid.dragPayload.bind(grid);
grid.dragPayload = () => {
  const loc = nav.current;
  return { ...origDragPayload(), bucket: loc.kind === 'objects' ? loc.bucket : null };
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
  $('btn-upload').onclick = showUploadMenu;
  $('btn-download').onclick = () => downloadSelection();
  $('btn-panes').onclick = togglePanes;
  $('btn-find').onclick = findFromHere;
  $('btn-newfolder').onclick = newFolder;
  $('btn-doctor').onclick = () => runDoctor(nav.current?.kind === 'objects' ? nav.current.bucket : '');
  $('btn-transfers').onclick = () => transferManager();
  $('btn-profiles').onclick = () => sourcesDialog();
  $('btn-theme').onclick = toggleTheme;
  $('btn-help').onclick = helpSheet;
  // Switching the dropdown makes another s3 source the default. The source
  // flag travels through SaveSource (store and Profile-file mode alike); in
  // store mode the legacy mirror is re-flagged too so browsing and the CLI
  // agree. Masked secrets ride along and are re-attached server-side by ID.
  $('profile-select').onchange = async () => {
    const name = $('profile-select').value;
    try {
      await makeDefaultSource(name);
      await refreshSources();
      nav.to({ kind: 'buckets' });
      toast('Source switched', 'ok');
    } catch (err) {
      toast(`Switch failed: ${err}`, 'error');
      await refreshSources();
    }
  };
  $('filter').addEventListener('input', debounce(() => {
    view.filter = $('filter').value;
    grid.setFilter(view.filter);
  }, 120));
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

// sourceDetail is the secondary line in the sources dialog.
function sourceDetail(s) {
  if (s.type === 's3') return `${providerLabel(s.s3?.endpoint)}${s.s3?.endpoint ? ` — ${s.s3.endpoint}` : ''}`;
  if (s.type === 'local') return `local — ${s.localRoot || ''}`;
  return `${s.type} — ${s.host || ''}${s.port ? `:${s.port}` : ''}`;
}

function sourcesDialog() {
  const list = el('div', {});
  const draw = () => {
    list.replaceChildren(...sources.map((s) => el('div', { class: 'tr-job' },
      el('div', { class: 'tr-top' },
        el('span', { class: 'tr-name' },
          el('span', { style: `color:${s.color || 'var(--accent)'};margin-right:6px`, text: '\u25CF' }),
          `${s.name} ${s.default ? '(\u2605 default)' : ''}`),
        el('span', { class: 'tr-status', text: sourceDetail(s) }),
        el('button', { class: 'btn', text: 'Edit', onclick: () => { sourceEditor(s, async () => { await refreshSources(); draw(); }); } }),
        ...(s.type === 's3' ? [el('button', { class: 'btn', text: 'Default', onclick: async () => {
          try {
            await makeDefaultSource(s.name);
            await refreshSources();
            draw();
          } catch (err) { toast(`Default failed: ${err}`, 'error'); }
        } })] : []),
        el('button', { class: 'btn', text: 'Remove', onclick: async () => {
          if (await confirm({ title: `Remove source ${s.name}?`, message: 'Stored credentials will be deleted.', danger: true, okLabel: 'Remove' })) {
            try {
              await api.RemoveSource(s.id || s.name);
              await refreshSources();
              refreshPfState();
              draw();
            } catch (err) { toast(`Remove failed: ${err}`, 'error'); }
          }
        } }),
      ),
    )));
    list.appendChild(el('div', {}, '\u00A0'));
  };
  draw();
  openModal({
    title: t('sourcesTitle'),
    body: list,
    wide: true,
    buttons: [
      { label: t('importAws'), onclick: async (c) => { c(); importAws(); } },
      { label: t('addSource'), class: 'primary', onclick: (c) => { c(); sourceEditor(null, afterSourceSaved); } },
      { label: 'Close' },
    ],
  });
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
  } else {
    sp.classList.add('hidden');
  }
}

// makeDefaultSource flips the default flag in one place: the source record
// via SaveSource (works in store and Profile-file mode) and, in store mode,
// the legacy mirror via SetDefaultProfile so browsing and the CLI agree.
async function makeDefaultSource(name) {
  await refreshPfState();
  const src = sources.find((s) => s.type === 's3' && s.name === name);
  if (!src) return;
  await api.SaveSource({ ...src, default: true, s3: { ...src.s3, default: true } });
  if (!pfState.open) await api.SetDefaultProfile(name);
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
    if (sources.some((s) => s.type === 's3')) nav.to({ kind: 'buckets' });
  } catch (err) {
    toast(`${err}`, 'error'); // wrong password and corruption look identical by design
  }
}

async function saveProfileFileUi() {
  await refreshPfState();
  if (!pfState.open) return; // Ctrl+S is global; silently ignore without a session
  if (!pfState.path) return saveAsProfileFileUi(); // never saved yet
  try {
    await api.SaveProfileFile();
    toast(t('pf.saved'), 'ok');
    await refreshPfState();
  } catch (err) { toast(`${err}`, 'error'); }
}

async function saveAsProfileFileUi() {
  await refreshPfState();
  if (!pfState.open) return;
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
  if (!pfState.open) return;
  try {
    await api.CloseProfileFile(false);
  } catch (err) {
    // Dirty: offer a forced close that discards the container edits.
    const ok = await confirm({
      title: t('pf.close'),
      message: 'The profile file has unsaved changes.\nClose anyway and discard them?',
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
  if (sources.some((s) => s.type === 's3')) nav.to({ kind: 'buckets' });
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
        { label: t('menu.uploadFiles'), kbd: 'Ctrl+U', action: uploadFiles, enabled: () => st().canUpload },
        { label: t('menu.uploadFolder'), action: uploadFolder, enabled: () => st().canUpload },
        null,
        { label: t('menu.importAws'), action: importAws },
        null,
        { label: t('pf.new'), action: newProfileFileUi },
        { label: t('pf.open'), action: openProfileFileUi },
        { label: t('pf.save'), kbd: 'Ctrl+S', action: saveProfileFileUi, enabled: () => pfState.open },
        { label: t('pf.saveAs'), action: saveAsProfileFileUi, enabled: () => pfState.open },
        { label: t('pf.close'), action: closeProfileFileUi, enabled: () => pfState.open },
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
      label: t('menu.help'),
      items: [
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
      el('div', { class: 'v', text: 'MIT — Copyright (c) MikkoP88' }),
      el('div', { class: 'k', text: t('menu.aboutUrl') }),
      el('div', { class: 'v mono', text: 'https://github.com/MikkoP88/s3-bucket-browser' }),
    );
  };
  draw('');
  api.GetVersion().then(draw).catch(() => {});
  openModal({ title: t('menu.aboutTitle'), body, buttons: [{ label: 'Close' }] });
}

// ============================ keyboard ============================
// copySelection/cutSelection: shared by Ctrl+C/X and the Edit menu.
function copySelection() {
  const r = grid.selectedRows();
  if (!r.length) return;
  clipboard.mode = 'copy';
  clipboard.bucket = nav.current?.bucket;
  clipboard.keys = r.map((x) => x.key);
  toast(`Copied ${r.length} item(s)`);
  updateCommandState();
}

function cutSelection() {
  const r = grid.selectedRows();
  if (!r.length) return;
  clipboard.mode = 'cut';
  clipboard.bucket = nav.current?.bucket;
  clipboard.keys = r.map((x) => x.key);
  toast(`Cut ${r.length} item(s)`);
  updateCommandState();
}

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
    if (loc.kind === 'buckets' || data?.bucket === loc.bucket) refreshCurrent();
  });
  onEvent('transfer:update', (j) => {
    if (j.status === 'running') showTransfersBadge();
    else showTransfersBadge();
  });
  onEvent('editor:saved', (d) => {
    toast(`Uploaded ${d?.key ? basename(d.key) : 'edited file'}`, 'ok');
    updateEditingStatus();
  });
  onEvent('log:line', (l) => logArea.append(l));
  $('status-editing').onclick = () => editingDialog(updateEditingStatus);
  $('status-log').onclick = toggleLogArea;
  window.addEventListener('focus', updateEditingStatus);
  window.addEventListener('focus', () => {
    if (refreshOnFocus && !autoRefreshBlocked()) refreshCurrent();
  });
}

function showTransfersBadge() {
  api.ActiveTransfers().then((jobs) => {
    const running = jobs.filter((j) => j.status === 'running');
    const badge = $('transfer-badge');
    badge.classList.toggle('hidden', running.length === 0);
    badge.textContent = running.length;
    const sb = $('status-jobs');
    if (running.length) {
      sb.classList.remove('hidden');
      const j = running[0];
      sb.textContent = `\u21C5 ${j.doneFiles}/${j.totalFiles} ${j.currentFile ? basename(j.currentFile) : ''} ${fmtBytes(j.sentBytes)}${j.totalBytes ? '/' + fmtBytes(j.totalBytes) : ''}`;
    } else {
      sb.classList.add('hidden');
    }
  });
}

function updateStatus() {
  const sel = grid.selectedRows().length;
  const total = grid.rows.length;
  $('status-selection').textContent = sel
    ? `${sel} of ${total} ${t('items')} ${t('selected')}`
    : `${total} ${total === 1 ? t('item') : t('items')}`;
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
