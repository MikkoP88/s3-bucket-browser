// S3 Bucket Browser — application shell (Explorer layout, PLAN.md §11).
import { api, onEvent } from './api.js';
import { el, fmtBytes, basename, debounce } from './util.js';
import { nav, parentOf, clipboard, view } from './state.js';
import { Grid } from './grid.js';
import { Tree } from './tree.js';
import {
  confirm, typedConfirm, prompt, properties, doctorDialog, transferManager,
  profileEditor, helpSheet, conflictPolicy, presignDialog, toast, openModal,
  versionsDialog, adminDialog, editingDialog, findDialog, classDialog, lockDialog,
} from './dialogs.js';
import { LocalPane, aggregateCompare } from './local.js';
import { t, detectLang, setLang } from './i18n.js';
import { setCommandContext, updateCommandState } from './commands.js';

const $ = (id) => document.getElementById(id);

const grid = new Grid();
const localPane = new LocalPane();
const tree = new Tree({ onNavigate: (loc) => nav.to(loc), onDropTo: dropToTarget });

setCommandContext({
  selectionCount: () => grid.selectedRows().length,
  hasProfile: () => profiles.length > 0,
});

let profiles = [];
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
  wireGrid();
  wireLocalPane();
  wireKeys();
  wireDrop();
  wireEvents();

  const ok = await refreshProfiles();
  if (ok) nav.to({ kind: 'buckets' });
  if (localStorage.getItem('s3b-panes') === '1') localPane.show();
  updateCommandState();
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

// ============================ profiles ============================
async function refreshProfiles(selectAfter = true) {
  try {
    profiles = await api.ListProfiles();
  } catch (err) {
    toast(`Profiles: ${err}`, 'error');
    profiles = [];
  }
  const sel = $('profile-select');
  sel.replaceChildren(...profiles.map((p) =>
    el('option', { value: p.name }, `${p.default ? '\u2605 ' : ''}${p.name} (${p.provider})`)));
  const def = profiles.find((p) => p.default) || profiles[0];
  if (def) sel.value = def.name;
  $('status-profile').textContent = def ? def.name : '';
  if (!profiles.length) {
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
  showEmpty(t('noProfiles'), t('noProfilesSub'), [
    el('button', { class: 'btn primary', text: t('addProfile'), onclick: () => profileEditor(null, afterProfileSaved) }),
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
      await refreshProfiles();
      nav.to({ kind: 'buckets' });
    }
  } catch (err) {
    toast(`Import failed: ${err}`, 'error');
  }
}

function afterProfileSaved() {
  refreshProfiles().then((ok) => { if (ok) nav.to({ kind: 'buckets' }); });
  toast('Profile saved', 'ok');
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
          el('button', { class: 'btn primary', text: '\u2191 Upload files', onclick: uploadFiles }),
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
}

function wireLocalPane() {
  localPane.on.openFail = (e) => toast(`Local: ${e}`, 'error');
  localPane.on.dropFolder = (folder, payload) => downloadRefs(payload.entries, folder.path);
  localPane.on.dropBody = (payload) => downloadRefs(payload.entries, localPane.dir);
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

// ============================ context menu ============================
function showContextMenu(e, rows) {
  const menu = $('ctxmenu');
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

  menu.replaceChildren(...items.map((it) => {
    if (!it) return el('div', { class: 'sep' });
    const [label, kbd, fn, disabled] = it;
    return el('div', {
      class: `item${disabled ? ' disabled' : ''}`,
      onclick: () => { hideContextMenu(); fn(); },
    }, el('span', { text: label }), kbd ? el('span', { class: 'kbd', text: kbd }) : null);
  }));
  menu.classList.remove('hidden');
  const x = Math.min(e.clientX, innerWidth - 220);
  const y = Math.min(e.clientY, innerHeight - menu.offsetHeight - 10);
  menu.style.left = `${x}px`;
  menu.style.top = `${y}px`;
}

function hideContextMenu() { $('ctxmenu').classList.add('hidden'); }
document.addEventListener('mousedown', (e) => {
  if (!e.target.closest('#ctxmenu')) hideContextMenu();
});
window.addEventListener('blur', hideContextMenu);

// ============================ actions ============================
async function uploadPaths(paths, prefixOverride) {
  const loc = nav.current;
  if (loc.kind !== 'objects') { toast('Open a bucket first'); return; }
  const prefix = prefixOverride !== undefined ? prefixOverride : (loc.prefix || '');
  const res = await conflictPolicy('upload', `s3://${loc.bucket}/${prefix || ''}`);
  if (!res) return;
  try {
    await api.Upload(paths, loc.bucket, prefix, res.policy, res.maxBps);
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
async function downloadRefs(entries, dest) {
  const loc = nav.current;
  if (!entries?.length) return;
  const res = await conflictPolicy('download', dest);
  if (!res) return;
  try {
    await api.DownloadRefs(loc.bucket, entries, dest, res.policy, res.maxBps);
    toast(`Downloading ${entries.length} item(s)\u2026`);
    showTransfersBadge();
  } catch (err) {
    toast(`Download failed: ${err}`, 'error');
  }
}

async function deleteSelection() {
  const loc = nav.current;
  if (loc.kind === 'buckets') {
    const row = grid.selectedRows()[0];
    if (row) deleteBucket(row.key);
    return;
  }
  const rows = grid.selectedRows();
  if (!rows.length) return;
  const keys = rows.map((r) => r.key);
  try {
    const p = await api.PreviewDelete(loc.bucket, keys);
    const desc = `${p.count} object(s)${p.bytes ? ` (${fmtBytes(p.bytes)})` : ''}${p.folders ? ` in ${p.folders} folder(s)` : ''}`;
    let ok;
    if (p.requiresL2) {
      ok = await typedConfirm({
        title: `Delete from s3://${loc.bucket}`,
        message: `You are about to delete ${desc}.\nThis cannot be undone.`,
        typeWord: 'delete',
      });
    } else {
      ok = await confirm({
        title: `Delete from s3://${loc.bucket}`,
        message: `Delete ${desc}? This cannot be undone.`,
        okLabel: 'Delete',
        danger: true,
      });
    }
    if (!ok) return;
    const res = await api.DeleteSelection(loc.bucket, keys, p.requiresL2);
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
    await api.RenameObject(loc.bucket, row.key, name);
    toast('Renamed', 'ok');
    refreshCurrent();
  } catch (err) {
    toast(`Rename failed: ${err}`, 'error');
  }
}

async function newFolder() {
  const loc = nav.current;
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

async function paste() {
  const loc = nav.current;
  if (loc.kind !== 'objects' || !clipboard.keys.length) return;
  const move = clipboard.mode === 'cut';
  try {
    const res = await api.CopySelection(clipboard.bucket, clipboard.keys, loc.bucket, loc.prefix || '', move);
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

async function runDoctor(bucket) {
  try {
    const rep = await api.RunDoctor(bucket || '');
    doctorDialog(rep);
  } catch (err) {
    toast(`Doctor failed: ${err}`, 'error');
  }
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
  $('btn-upload').onclick = uploadFiles;
  $('btn-upload-dir').onclick = uploadFolder;
  $('btn-download').onclick = () => downloadSelection();
  $('btn-panes').onclick = togglePanes;
  $('btn-find').onclick = findFromHere;
  $('btn-newfolder').onclick = newFolder;
  $('btn-doctor').onclick = () => runDoctor(nav.current?.kind === 'objects' ? nav.current.bucket : '');
  $('btn-transfers').onclick = () => transferManager();
  $('btn-profiles').onclick = () => profilesDialog();
  $('btn-theme').onclick = toggleTheme;
  $('btn-help').onclick = helpSheet;
  $('profile-select').onchange = async () => {
    await api.SetDefaultProfile($('profile-select').value);
    await refreshProfiles();
    nav.to({ kind: 'buckets' });
    toast('Profile switched', 'ok');
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

function profilesDialog() {
  const list = el('div', {});
  const draw = () => {
    list.replaceChildren(...profiles.map((p) => el('div', { class: 'tr-job' },
      el('div', { class: 'tr-top' },
        el('span', { class: 'tr-name' },
          el('span', { style: `color:${p.color || 'var(--accent)'};margin-right:6px`, text: '\u25CF' }),
          `${p.name} ${p.default ? '(\u2605 default)' : ''}`),
        el('span', { class: 'tr-status', text: `${p.provider}${p.endpoint ? ` — ${p.endpoint}` : ''}` }),
        el('button', { class: 'btn', text: 'Edit', onclick: () => { profileEditor(p, async () => { await refreshProfiles(); draw(); }); } }),
        el('button', { class: 'btn', text: 'Default', onclick: async () => { await api.SetDefaultProfile(p.name); await refreshProfiles(); draw(); } }),
        el('button', { class: 'btn', text: 'Remove', onclick: async () => {
          if (await confirm({ title: `Remove profile ${p.name}?`, message: 'Stored credentials will be deleted.', danger: true, okLabel: 'Remove' })) {
            await api.RemoveProfile(p.name);
            await refreshProfiles();
            draw();
          }
        } }),
      ),
    )));
    list.appendChild(el('div', {}, '\u00A0'));
  };
  draw();
  openModal({
    title: 'Connection profiles',
    body: list,
    wide: true,
    buttons: [
      { label: t('importAws'), onclick: async (c) => { c(); importAws(); } },
      { label: t('addProfile'), class: 'primary', onclick: (c) => { c(); profileEditor(null, afterProfileSaved); } },
      { label: 'Close' },
    ],
  });
}

// ============================ keyboard ============================
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
    if (ctrl && e.key.toLowerCase() === 'c') { const r = grid.selectedRows(); if (r.length) { clipboard.mode = 'copy'; clipboard.bucket = nav.current?.bucket; clipboard.keys = r.map((x) => x.key); toast(`Copied ${r.length} item(s)`); updateCommandState(); } return; }
    if (ctrl && e.key.toLowerCase() === 'x') { const r = grid.selectedRows(); if (r.length) { clipboard.mode = 'cut'; clipboard.bucket = nav.current?.bucket; clipboard.keys = r.map((x) => x.key); toast(`Cut ${r.length} item(s)`); updateCommandState(); } return; }
    if (ctrl && e.key.toLowerCase() === 'v') { e.preventDefault(); paste(); return; }
    if (ctrl && e.shiftKey && e.key.toLowerCase() === 'f') { e.preventDefault(); findFromHere(); return; }
    if (ctrl && e.key.toLowerCase() === 'f') { e.preventDefault(); $('filter').focus(); $('filter').select(); return; }
    if (ctrl && e.key.toLowerCase() === 'u') { e.preventDefault(); uploadFiles(); return; }
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
  $('status-editing').onclick = () => editingDialog(updateEditingStatus);
  window.addEventListener('focus', updateEditingStatus);
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
