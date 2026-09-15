// Modal framework + every dialog: confirmations (L1/L2 ladder), prompts,
// properties, doctor, profile editor, transfer manager, help sheet.
import { api, onEvent, subscribeStream } from './api.js';
import { el, fmtBytes, fmtSpeed, fmtDate, parseSizeStr, parseDurStr, parentPrefix } from './util.js';
import { t } from './i18n.js';

const root = () => document.getElementById('modal-root');

export function openModal({ title, body, buttons = [], wide = false, cls = '', onClose }) {
  const r = root();
  const prevFocus = document.activeElement;
  r.classList.remove('hidden');

  const close = (result) => {
    r.classList.add('hidden');
    r.replaceChildren();
    document.removeEventListener('keydown', esc, true);
    document.removeEventListener('keydown', trap, true);
    prevFocus?.focus?.();
    onClose?.(result);
  };
  const esc = (e) => { if (e.key === 'Escape') close(null); };
  // Focus trap: Tab (and Shift+Tab) cycle inside the modal (a11y).
  const trap = (e) => {
    if (e.key !== 'Tab') return;
    const focusables = box.querySelectorAll('button, input, select, textarea, [tabindex]:not([tabindex="-1"])');
    if (!focusables.length) return;
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    else if (!r.contains(document.activeElement)) { e.preventDefault(); first.focus(); }
  };
  document.addEventListener('keydown', esc, true);
  document.addEventListener('keydown', trap, true);

  const foot = el('div', { class: 'modal-foot' },
    buttons.map((b) => el('button', {
      class: `btn ${b.class || ''}`,
      text: b.label,
      onclick: () => b.onclick ? b.onclick(close) : close(null),
    })),
  );

  const box = el('div', {
    class: `modal${wide ? ' wide' : ''}${cls ? ' ' + cls : ''}`,
    role: 'dialog',
    'aria-modal': 'true',
    'aria-label': title,
  },
    el('div', { class: 'modal-head' },
      el('span', { text: title }),
      el('span', { class: 'x', text: '\u00D7', role: 'button', 'aria-label': 'Close', onclick: () => close(null) }),
    ),
    el('div', { class: 'modal-body' }, body),
    foot,
  );
  box.addEventListener('mousedown', (e) => { if (e.target === r) close(null); });
  r.replaceChildren(box);
  // initial focus: first form control, else first button
  const firstCtl = box.querySelector('input, select, textarea') || box.querySelector('.modal-foot .btn');
  firstCtl?.focus?.();
  return { close, body: box.querySelector('.modal-body') };
}

// ---------- confirmations (safety ladder) ----------
export function confirm({ title, message, okLabel = 'OK', danger = false }) {
  let settled = false;
  return new Promise((resolve) => {
    // settle() must run BEFORE close(): close() fires onClose synchronously
    // and a plain resolve would otherwise always lose the race to it.
    const done = (v) => { if (!settled) { settled = true; resolve(v); } };
    openModal({
      title,
      body: el('div', {}, message),
      buttons: [
        { label: 'Cancel', onclick: (close) => { done(false); close(); } },
        { label: okLabel, class: danger ? 'danger' : 'primary', onclick: (close) => { done(true); close(); } },
      ],
      onClose: () => done(false),
    });
  });
}

// versionChoiceDialog is the per-task S3→S3 question: preserve the full
// version timeline (CopySelectionVersions background job) or copy latest
// versions only. Resolves true = preserve, false = plain copy, null =
// canceled. defaultOn seeds the checkbox from the Settings toggle.
export function versionChoiceDialog({ move = false, defaultOn = true } = {}) {
  let settled = false;
  return new Promise((resolve) => {
    const done = (v) => { if (!settled) { settled = true; resolve(v); } };
    const chk = el('input', { type: 'checkbox', checked: !!defaultOn });
    openModal({
      title: t('cv.title'),
      body: el('div', {},
        el('label', { class: 'vcv-row' }, chk, ` ${t('cv.preserve')}`),
        el('div', { class: 'set-hint', text: t('cv.preserveHint') }),
        move ? el('div', { class: 'set-hint', text: t('cv.moveWarn') }) : null,
      ),
      buttons: [
        { label: 'Cancel', onclick: (c) => { done(null); c(); } },
        {
          label: move ? t('cv.move') : t('cv.copy'),
          class: 'primary',
          onclick: (c) => { done(!!chk.checked); c(); },
        },
      ],
      onClose: () => done(null),
    });
  });
}

// deleteChoiceDialog asks how to delete from a VERSIONED bucket: the safe
// marker path (default — objects stay restorable in version history) or the
// permanent one (L3 — destroys every version and delete marker). Resolves
// 'marker' | 'permanent' | null (canceled). Non-versioned buckets never
// reach this dialog.
export function deleteChoiceDialog({ desc = '' } = {}) {
  let settled = false;
  return new Promise((resolve) => {
    const done = (v) => { if (!settled) { settled = true; resolve(v); } };
    const mk = el('input', { type: 'radio', name: 'delmode', value: 'marker' });
    const pm = el('input', { type: 'radio', name: 'delmode', value: 'permanent' });
    mk.checked = true;
    openModal({
      title: t('del.title'),
      body: el('div', {},
        desc ? el('div', { text: desc }) : null,
        el('label', { class: 'vcv-row' }, mk, ` ${t('del.marker')}`),
        el('div', { class: 'set-hint', text: t('del.markerHint') }),
        el('label', { class: 'vcv-row' }, pm, ` ${t('del.perm')}`),
        el('div', { class: 'set-hint', text: t('del.permHint') }),
      ),
      buttons: [
        { label: 'Cancel', onclick: (c) => { done(null); c(); } },
        {
          label: t('del.go'),
          class: 'danger',
          onclick: (c) => { done(pm.checked ? 'permanent' : 'marker'); c(); },
        },
      ],
      onClose: () => done(null),
    });
  });
}

export function typedConfirm({ title, message, typeWord, okLabel = 'Delete', danger = true }) {
  let settled = false;
  return new Promise((resolve) => {
    const done = (v) => { if (!settled) { settled = true; resolve(v); } };
    const input = el('input', { class: 'input', autocomplete: 'off' });
    const body = el('div', {},
      el('div', {}, message),
      el('label', { class: 'field', text: `Type "${typeWord}" to confirm:` }),
      input,
    );
    openModal({
      title,
      body,
      buttons: [
        { label: 'Cancel', onclick: (c) => { done(false); c(); } },
        {
          label: okLabel,
          class: danger ? 'danger' : 'primary',
          onclick: (c) => { if (input.value.trim() === typeWord) { done(true); c(); } },
        },
      ],
      onClose: () => done(false),
    });
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && input.value.trim() === typeWord) { done(true); document.querySelector('#modal-root .modal-head .x')?.click(); }
    });
    input.focus();
  });
}

export function prompt({ title, label, value = '', okLabel = 'OK', password = false }) {
  let settled = false;
  return new Promise((resolve) => {
    const done = (v) => { if (!settled) { settled = true; resolve(v); } };
    const input = el('input', { class: 'input', value, spellcheck: 'false' });
    if (password) {
      input.type = 'password';
      input.autocomplete = 'new-password';
    }
    const submit = (close) => { done(password ? (input.value || null) : (input.value.trim() || null)); close(); };
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') submit(closeRef.close); });
    const closeRef = openModal({
      title,
      body: el('div', {}, el('label', { class: 'field', text: label }), input),
      buttons: [
        { label: 'Cancel', onclick: (c) => { done(null); c(); } },
        { label: okLabel, class: 'primary', onclick: submit },
      ],
      onClose: () => done(null),
    });
    input.focus();
    input.select();
  });
}

// ---------- properties ----------
// rows values are strings or DOM nodes (pills etc.).
export function properties(title, rows) {
  const body = el('div', { class: 'kv' });
  for (const [k, v] of rows) {
    body.appendChild(el('div', { class: 'k', text: k }));
    if (v && v.nodeType === 1) body.appendChild(el('div', { class: 'v' }, v));
    else body.appendChild(el('div', { class: 'v mono', text: String(v ?? '') }));
  }
  openModal({ title, body, buttons: [{ label: 'Close' }] });
}

// pill builds an Enabled/Disabled status pill (shared by guard rows and
// bucket properties).
export function pill(on) {
  return el('span', { class: `pill ${on ? 'on' : 'off'}`, text: on ? 'Enabled' : 'Disabled' });
}

// ---------- doctor (v2: per-check rows, run-all, per-check re-run) ----------
const docTime = (iso) => {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (x) => String(x).padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};

export function doctorDialog(bucket) {
  const summary = el('div', { class: 'doc-summary' });
  const list = el('div', { class: 'doc-list' });
  const warnBox = el('div', {});
  const meta = el('div', { class: 'kv' });

  const fmtDur = (c) => (c && typeof c.durationMs === 'number' ? `${c.durationMs} ms` : '');

  // detail body for one check: advice, error, info (each hidden if absent).
  // Rows start unchecked (c === null) and are re-rendered by update(c), so
  // the null shape must render instead of throwing.
  const detailOf = (c) => {
    if (!c) return el('div', { class: 'doc-detail' });
    return el('div', { class: 'doc-detail' },
      c.advice ? el('div', { class: 'banner warn' },
        el('div', { text: `${t('doctor.advice')}: ${c.advice.suggestion || c.advice.cause || c.advice.code}` }),
        ...(c.advice.commands || []).map((cmd) => el('div', { class: 'mono doc-cmd', text: cmd })),
      ) : null,
      c.error ? el('div', { class: 'doc-detail-sec' },
        el('div', { class: 'doc-detail-k', text: t('doctor.error') }),
        el('div', { class: 'doc-detail-v', text: c.error }),
      ) : null,
      c.info ? el('div', { class: 'doc-detail-sec' },
        el('div', { class: 'doc-detail-k', text: t('doctor.info') }),
        el('pre', { class: 'mono doc-pre', text: JSON.stringify(c.info, null, 2) }),
      ) : null,
      c.detail && !c.error ? el('div', { class: 'doc-detail-sec' },
        el('div', { class: 'doc-detail-k', text: t('doctor.info') }),
        el('div', { class: 'doc-detail-v', text: c.detail }),
      ) : null,
    );
  };

  function makeRow(name) {
    let result = null;
    let expanded = false;
    let detail = detailOf(null);
    detail.classList.add('hidden');
    const twist = el('span', { class: 'doc-twist', text: '\u25B8' });
    const pill = el('span', { class: 'doc-pill notrun', text: t('doctor.notRun') });
    const times = el('span', { class: 'doc-times mono' });
    const dur = el('span', { class: 'doc-dt', text: '' });
    const rerunBtn = el('button', {
      class: 'btn doc-rerun', text: '\u25B6', title: t('doctor.rerun'),
      onclick: () => rerun(name),
    });
    const row = el('div', { class: 'doc-row' },
      el('div', { class: 'doc-row-top' },
        twist,
        el('span', { class: 'doc-name', text: name }),
        pill, times, dur, rerunBtn,
      ),
      detail,
    );
    row.addEventListener('click', (e) => {
      if (e.target.closest('.doc-rerun')) return;
      expanded = !expanded;
      twist.textContent = expanded ? '\u25BE' : '\u25B8';
      detail.classList.toggle('hidden', !expanded);
    });
    return {
      row,
      setRunning(on) {
        pill.className = `doc-pill ${on ? 'running' : (result ? result.status : 'notrun')}`;
        pill.textContent = on ? '\u21BB' : (result ? t(`doctor.${result.status}`) : t('doctor.notRun'));
        pill.classList.toggle('spin', on);
        rerunBtn.disabled = on;
      },
      update(c) {
        result = c;
        pill.className = `doc-pill ${c.status}`;
        pill.textContent = t(`doctor.${c.status}`);
        times.textContent = `${docTime(c.started_at) || '—'} \u2192 ${docTime(c.finished_at) || '—'}`;
        dur.textContent = fmtDur(c);
        const nd = detailOf(c);
        nd.classList.toggle('hidden', !expanded);
        detail.replaceWith(nd);
        detail = nd;
      },
    };
  }

  const rows = new Map();
  const setSummary = (s) => {
    summary.textContent = s
      ? t('doctor.summary', { pass: s.pass, warn: s.warn, fail: s.fail, skip: s.skip })
      : '';
  };

  async function rerun(name) {
    const r = rows.get(name);
    if (!r) return;
    r.setRunning(true);
    try {
      const c = await api.RunDoctorCheck(bucket, name);
      r.update(c);
    } catch (err) {
      r.update({ check: name, status: 'fail', error: String(err), durationMs: 0, started_at: '', finished_at: '' });
      toast(`${name}: ${err}`, 'error');
    }
    r.setRunning(false);
  }

  async function runAll() {
    for (const r of rows.values()) r.setRunning(true);
    try {
      const rep = await api.RunDoctor(bucket);
      meta.replaceChildren(
        el('div', { class: 'k', text: 'Endpoint' }), el('div', { class: 'v mono', text: rep.endpoint }),
        el('div', { class: 'k', text: 'Provider' }), el('div', { class: 'v', text: rep.provider }),
        el('div', { class: 'k', text: 'Bucket' }), el('div', { class: 'v mono', text: rep.bucket || '—' }),
      );
      for (const c of rep.checks || []) rows.get(c.check)?.update(c);
      warnBox.replaceChildren(...(rep.warnings || []).map((w) =>
        el('div', { class: 'banner warn', text: w })));
      setSummary(rep.summary);
    } catch (err) {
      toast(`Doctor failed: ${err}`, 'error');
    }
    for (const r of rows.values()) r.setRunning(false);
  }

  openModal({
    title: `${t('doctor.title')}${bucket ? ` — s3://${bucket}` : ''}`,
    body: el('div', {}, summary, meta, el('div', { style: 'height:10px' }), list, warnBox),
    wide: true,
    buttons: [
      { label: t('doctor.runAll'), class: 'primary', onclick: () => runAll() },
      { label: 'Close' },
    ],
  });

  api.DoctorChecks().then((names) => {
    for (const name of names) {
      const r = makeRow(name);
      rows.set(name, r);
      list.appendChild(r.row);
    }
  }).catch((err) => toast(`Doctor: ${err}`, 'error'));
}

// ---------- transfer manager ----------
export function transferManager(onClose) {
  const list = el('div', {});
  const { close } = openModal({
    title: t('transfer.managerTitle'),
    body: list,
    wide: true,
    buttons: [
      { label: t('transfer.clearFinished'), onclick: async (c) => { await api.ClearFinishedTransfers(); draw(); } },
      { label: t('dlg.close'), onclick: (c) => { c(); onClose?.(); } },
    ],
    onClose: () => off(),
  });

  async function draw() {
    const jobs = await api.ActiveTransfers();
    // Running jobs stay on top; the rest keep their order.
    const rank = { running: 0, queued: 1, done: 2, canceled: 3, failed: 4 };
    jobs.sort((x, y) => (rank[x.status] ?? 9) - (rank[y.status] ?? 9));
    list.replaceChildren(...jobs.map(renderJob));
    if (!jobs.length) list.appendChild(el('div', { class: 'tm-empty', text: t('transfer.noTransfers') }));
  }

  function renderJob(j) {
    const pct = j.totalBytes > 0 ? Math.min(100, (j.sentBytes / j.totalBytes) * 100) : 0;
    const bar = el('div', { class: 'tr-bar' }, el('div', { style: `width:${pct}%` }));
    const counts = t('transfer.filesCount', { d: j.doneFiles, t: j.totalFiles })
      + (j.failedFiles ? ` · ${t('transfer.failedCount', { n: j.failedFiles })}` : '')
      + (j.skippedFiles ? ` · ${t('transfer.skippedCount', { n: j.skippedFiles })}` : '');
    let bytes = `${fmtBytes(j.sentBytes)}${j.totalBytes ? ` / ${fmtBytes(j.totalBytes)}` : ''}`;
    if (j.speedBps > 1 && j.status === 'running') {
      bytes += ` @ ${fmtSpeed(j.speedBps)}`;
      const remain = j.totalBytes > j.sentBytes ? (j.totalBytes - j.sentBytes) / j.speedBps : 0;
      if (remain > 1) bytes += ` · ${fmtEta(remain)}`;
    }
    const job = el('div', { class: `tr-job ${j.status}` },
      el('div', { class: 'tr-top' },
        el('span', { class: 'tr-name', text: `${j.op === 'upload' ? '\u2191' : j.op === 'transfer' ? '\u21C4' : '\u2193'} ${j.currentFile || j.id}` }),
        el('span', { class: 'tr-status', text: `${j.status} — ${counts}, ${bytes}` }),
        el('span', { class: 'tr-pct mono', text: `${Math.floor(pct)}%` }),
        j.status === 'running' ? el('button', { class: 'btn', text: t('transfer.cancelJob'), onclick: async () => { await api.CancelTransfer(j.id); } }) : null,
      ),
      bar,
      j.error ? el('div', { class: 'tr-sub', text: j.error }) : null,
    );
    job.dataset.id = j.id;
    return job;
  }

  draw();
  const off = onEvent('transfer:update', () => draw());
  const origClose = close;
  return { close: () => origClose() };
}

// fmtEta renders a remaining-seconds estimate for running jobs.
function fmtEta(secs) {
  if (secs >= 3600) return `~${Math.round(secs / 3600)}h`;
  if (secs >= 60) return `~${Math.round(secs / 60)}m`;
  return `~${Math.round(secs)}s`;
}

// ---------- data source editor (M8: any connection type) ----------
const SOURCE_COLORS = ['#0b63ce', '#1b7f3b', '#b3261e', '#9a6700', '#7c3aed', '#0e7490', '#be185d', '#57606a'];

const SOURCE_TYPES = [
  ['s3', 'Amazon S3 / S3-compatible'],
  ['sftp', 'SFTP'],
  ['scp', 'SCP (SFTP engine)'],
  ['ftp', 'FTP'],
  ['ftps', 'FTPS'],
  ['webdav', 'WebDAV'],
  ['webdavs', 'WebDAVS (HTTPS)'],
  ['local', 'Local folder'],
];

// sourceEditor edits one data source of any type. existing is a (masked)
// Source from ListSources or null. Secret fields arrive empty with the
// stored mask as placeholder; the backend re-attaches stored values.
// The name starts auto-filled from the connection details (the S3 bucket,
// endpoint host, local folder name, remote host/start dir) and stays
// editable — once the user types a name it is never overwritten.
export function sourceEditor(existing, onSaved) {
  const f = {
    name: el('input', { class: 'input', value: existing?.name || '', spellcheck: 'false' }),
    type: el('select', { class: 'input' }, SOURCE_TYPES.map(([v, l]) => el('option', { value: v }, l))),
    // s3 (legacy profile fields)
    bucket: el('input', { class: 'input mono', value: existing?.bucket || '', placeholder: 'the bucket this source opens (required)', spellcheck: 'false', list: 'src-bucket-list', autocomplete: 'off' }),
    endpoint: el('input', { class: 'input mono', value: existing?.s3?.endpoint || '', placeholder: 'https://s3.amazonaws.com (empty = AWS)' }),
    region: el('input', { class: 'input', value: existing?.s3?.region || '', placeholder: 'us-east-1' }),
    accessKey: el('input', { class: 'input mono', value: existing?.s3?.accessKeyId || '', autocomplete: 'off' }),
    secretKey: el('input', { class: 'input mono', type: 'password', placeholder: existing?.s3?.secretKey || 'secret access key', autocomplete: 'new-password' }),
    token: el('input', { class: 'input mono', type: 'password', placeholder: 'optional (temporary credentials)', autocomplete: 'new-password' }),
    pathStyle: el('input', { type: 'checkbox' }),
    insecure: el('input', { type: 'checkbox' }),
    // remote filesystems
    host: el('input', { class: 'input mono', value: existing?.host || '', placeholder: 'server.example.com', spellcheck: 'false' }),
    port: el('input', { class: 'input mono', type: 'number', min: '0', max: '65535', value: existing?.port || '', placeholder: 'type default' }),
    username: el('input', { class: 'input mono', value: existing?.username || '', autocomplete: 'off' }),
    password: el('input', { class: 'input mono', type: 'password', placeholder: existing?.password || 'password', autocomplete: 'new-password' }),
    root: el('input', { class: 'input mono', value: existing?.root || '', placeholder: 'starting directory (optional)' }),
    // local
    localRoot: el('input', { class: 'input mono', value: existing?.localRoot || '', placeholder: 'C:\\data or /home/user/data', spellcheck: 'false' }),
  };
  f.type.value = existing?.type || 's3';
  f.pathStyle.checked = !!existing?.s3?.pathStyle;
  f.insecure.checked = !!existing?.s3?.insecure;

  let color = existing?.color || SOURCE_COLORS[0];
  const chips = el('div', { class: 'chips' }, SOURCE_COLORS.map((c) =>
    el('div', { class: `chip${c === color ? ' sel' : ''}`, style: `background:${c}`, onclick: (e) => {
      color = c;
      chips.querySelectorAll('.chip').forEach((n) => n.classList.remove('sel'));
      e.target.classList.add('sel');
    } }),
  ));

  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)' });

  // ---- auto-filled name (kept until the user edits it) ----
  let nameDirty = !!existing?.name;
  const suggestName = () => {
    const t = f.type.value;
    let s = '';
    if (t === 's3') {
      // an S3 data source IS one bucket — the bucket is the natural name
      const b = f.bucket.value.trim();
      if (b) {
        s = b;
      } else {
        const ep = f.endpoint.value.trim();
        if (ep) {
          try { s = new URL(ep).hostname.split('.')[0] || ''; } catch { s = ''; }
        }
      }
    } else if (t === 'local') {
      s = f.localRoot.value.split(/[\\/]/).filter(Boolean).pop() || '';
    } else {
      const root = f.root.value.trim();
      if (root && root !== '/') {
        s = root.replace(/\/+$/, '').split('/').pop() || '';
      } else {
        const h = f.host.value.trim();
        s = h ? h.split('.')[0] : '';
      }
    }
    return s.replace(/[^A-Za-z0-9._-]+/g, '-').replace(/^-+|-+$/g, '');
  };
  const applySuggest = () => {
    if (nameDirty) return;
    const s = suggestName();
    if (s) f.name.value = s;
  };
  f.name.addEventListener('input', () => { nameDirty = f.name.value.trim() !== ''; });
  for (const inp of [f.bucket, f.endpoint, f.host, f.root, f.localRoot]) inp.addEventListener('change', applySuggest);
  if (!nameDirty) applySuggest();

  // bucket completions: a successful account-wide Test reports the visible
  // bucket names; they become the bucket field's datalist options.
  const bucketList = el('datalist', { id: 'src-bucket-list' });
  const fillBucketList = (names) => {
    bucketList.replaceChildren(...(names || []).map((b) => el('option', { value: b })));
  };

  // draftSource builds the Source the form currently describes (for the
  // remote Test dial and the start-directory browser).
  const draftSource = () => {
    const t = f.type.value;
    const src = {
      id: existing?.id || '',
      name: f.name.value.trim() || suggestName() || 'draft',
      type: t,
    };
    if (t === 'local') src.localRoot = f.localRoot.value.trim();
    else if (t !== 's3') {
      src.host = f.host.value.trim();
      src.port = parseInt(f.port.value, 10) || 0;
      src.username = f.username.value.trim();
      src.password = f.password.value;
      src.root = f.root.value.trim();
    }
    return src;
  };

  // Type-specific field sets, re-rendered when the type select changes.
  const s3Fields = () => el('div', {},
    el('label', { class: 'field', text: 'Bucket' }),
    el('div', { style: 'display:flex;gap:8px' },
      f.bucket,
      bucketList,
      el('button', {
        class: 'btn', text: 'Pick\u2026', title: 'Connect with the values above and list the buckets you can see (Test fills the same list)',
        onclick: async () => {
          status.textContent = 'Listing buckets\u2026';
          status.style.color = 'var(--text-dim)';
          try {
            const res = await api.TestS3Draft({
              id: existing?.id || '',
              name: f.name.value.trim() || 'draft',
              type: 's3',
              s3: {
                name: f.name.value.trim() || 'draft',
                endpoint: f.endpoint.value.trim(),
                region: f.region.value.trim(),
                accessKeyId: f.accessKey.value.trim(),
                secretKey: f.secretKey.value,
                sessionToken: f.token.value,
                pathStyle: f.pathStyle.checked,
                insecure: f.insecure.checked,
              },
            });
            if (res.ok && res.buckets?.length) {
              fillBucketList(res.buckets);
              status.textContent = `\u2705 ${res.buckets.length} bucket(s) visible — pick one`;
              status.style.color = 'var(--ok)';
              f.bucket.focus();
            } else {
              status.textContent = res.ok ? `\u2705 ${res.message} (no buckets visible)` : `\u274C ${res.message}`;
              status.style.color = res.ok ? 'var(--ok)' : 'var(--danger)';
            }
          } catch (err) {
            status.textContent = `\u274C ${err}`;
            status.style.color = 'var(--danger)';
          }
        },
      }),
    ),
    el('label', { class: 'field', text: 'Endpoint URL' }), f.endpoint,
    el('label', { class: 'field', text: 'Region' }), f.region,
    el('div', { style: 'display:grid;grid-template-columns:1fr 1fr;gap:12px' },
      el('div', {}, el('label', { class: 'field', text: 'Access key ID' }), f.accessKey),
      el('div', {}, el('label', { class: 'field', text: 'Secret access key' }), f.secretKey),
    ),
    el('label', { class: 'field', text: 'Session token' }), f.token,
    el('div', { style: 'display:flex;gap:18px;margin-top:10px' },
      el('label', { style: 'display:flex;align-items:center;gap:6px' }, f.pathStyle, 'Path-style addressing'),
      el('label', { style: 'display:flex;align-items:center;gap:6px' }, f.insecure, 'Skip TLS verify'),
    ),
  );
  const remoteFields = () => el('div', {},
    el('div', { style: 'display:grid;grid-template-columns:2fr 1fr;gap:12px' },
      el('div', {}, el('label', { class: 'field', text: 'Host' }), f.host),
      el('div', {}, el('label', { class: 'field', text: 'Port' }), f.port),
    ),
    el('div', { style: 'display:grid;grid-template-columns:1fr 1fr;gap:12px' },
      el('div', {}, el('label', { class: 'field', text: 'Username' }), f.username),
      el('div', {}, el('label', { class: 'field', text: 'Password' }), f.password),
    ),
    el('label', { class: 'field', text: 'Start directory' }),
    el('div', { style: 'display:flex;gap:8px' },
      f.root,
      el('button', {
        class: 'btn', text: 'Browse\u2026', title: 'Browse the start directory on the server (connects with the values above)',
        onclick: async () => {
          const p = await browseDirDialog({
            title: 'Start directory',
            kind: 'remote',
            draft: draftSource(),
            start: f.root.value.trim() || '/',
          });
          if (p) { f.root.value = p; applySuggest(); }
        },
      }),
    ),
  );
  const localFields = () => el('div', {},
    el('label', { class: 'field', text: 'Folder' }),
    el('div', { style: 'display:flex;gap:8px' },
      f.localRoot,
      el('button', {
        class: 'btn', text: 'Browse\u2026', onclick: async () => {
          const dir = await api.PickFolder('Choose the source folder');
          if (dir) { f.localRoot.value = dir; applySuggest(); }
        },
      }),
    ),
  );

  const fields = el('div', { style: 'margin-top:4px' });
  const draw = () => {
    const t = f.type.value;
    fields.replaceChildren(
      t === 's3' ? s3Fields() : t === 'local' ? localFields() : remoteFields(),
    );
    applySuggest();
  };
  f.type.onchange = draw;
  draw();

  const body = el('div', {},
    el('label', { class: 'field', text: 'Name' }), f.name,
    el('label', { class: 'field', text: 'Type' }), f.type,
    fields,
    el('label', { class: 'field', text: 'Accent color' }), chips,
    status,
  );

  openModal({
    title: existing ? `Edit source — ${existing.name}` : 'Add data source',
    body,
    buttons: [
      {
        label: 'Test',
        onclick: async () => {
          status.textContent = 'Testing\u2026';
          status.style.color = 'var(--text-dim)';
          try {
            let res;
            if (f.type.value === 's3') {
              // Dial the FORM values, saved or not (masked secrets are
              // re-attached server-side from the stored source). With a
              // bucket typed the probe checks THAT bucket (a HEAD); without
              // one it lists the account and fills the bucket completions.
              res = await api.TestS3Draft({
                id: existing?.id || '',
                name: f.name.value.trim(),
                type: 's3',
                bucket: f.bucket.value.trim(),
                s3: {
                  name: f.name.value.trim(),
                  endpoint: f.endpoint.value.trim(),
                  region: f.region.value.trim(),
                  accessKeyId: f.accessKey.value.trim(),
                  secretKey: f.secretKey.value,
                  sessionToken: f.token.value,
                  pathStyle: f.pathStyle.checked,
                  insecure: f.insecure.checked,
                },
              });
              if (res.ok && res.buckets?.length) fillBucketList(res.buckets);
            } else if (f.type.value === 'local') {
              res = await api.TestSource(existing?.id || f.name.value.trim());
            } else {
              // remote engines dial the draft directly (masked secrets
              // inherited from a same-named stored source server-side)
              const entries = await api.RemoteListDraft(draftSource(), '/');
              res = { ok: true, message: `connected — ${entries.length} entries at the source root` };
            }
            status.textContent = res.ok ? `\u2705 ${res.message}` : `\u274C ${res.message}`;
            status.style.color = res.ok ? 'var(--ok)' : 'var(--danger)';
          } catch (err) {
            status.textContent = `\u274C ${err}`;
            status.style.color = 'var(--danger)';
          }
        },
      },
      {
        label: 'Save',
        class: 'primary',
        onclick: async (close) => {
          const t = f.type.value;
          const src = {
            id: existing?.id || '',
            name: f.name.value.trim(),
            type: t,
            color,
          };
          if (t === 's3') {
            if (!f.bucket.value.trim()) {
              status.textContent = '\u274C Bucket is required — an S3 data source is one bucket';
              status.style.color = 'var(--danger)';
              f.bucket.focus();
              return;
            }
            src.bucket = f.bucket.value.trim();
            src.s3 = {
              name: src.name,
              endpoint: f.endpoint.value.trim(),
              region: f.region.value.trim(),
              accessKeyId: f.accessKey.value.trim(),
              secretKey: f.secretKey.value,
              sessionToken: f.token.value,
              pathStyle: f.pathStyle.checked,
              insecure: f.insecure.checked,
            };
          } else if (t === 'local') {
            src.localRoot = f.localRoot.value.trim();
          } else {
            src.host = f.host.value.trim();
            src.port = parseInt(f.port.value, 10) || 0;
            src.username = f.username.value.trim();
            src.password = f.password.value;
            src.root = f.root.value.trim();
          }
          try {
            await api.SaveSource(src);
            close();
            onSaved?.(src);
          } catch (err) {
            status.textContent = `\u274C ${err}`;
            status.style.color = 'var(--danger)';
          }
        },
      },
      { label: 'Cancel' },
    ],
  });
  f.name.focus();
}

// ---------- help sheet (F1) ----------
export function helpSheet() {
  const rows = [
    ['Enter', 'Open bucket / folder / download object'],
    ['F2', 'Rename'],
    ['Del', 'Delete selection — versioned buckets ask: marker (restorable) or permanent'],
    ['Shift+Del', 'Delete permanently (all versions)'],
    ['Ctrl+C / X / V', 'Copy / cut / paste'],
    ['Ctrl+A', 'Select all'],
    ['Ctrl+F', 'Filter'],
    ['F5', 'Refresh'],
    ['Alt+\u2190 / \u2192', 'Back / forward'],
    ['Alt+\u2191, Backspace', 'Go to parent'],
    ['Type letters', 'Jump to item'],
    ['Ctrl+Shift+N', 'New folder'],
    ['Ctrl+U / Ctrl+D', 'Upload files / download selection'],
    ['F9', 'Toggle dual-pane local browser'],
    ['Ctrl+L', 'Toggle log area'],
    ['Ctrl+S', 'Save profile file'],
    ['Esc', 'Clear selection / close'],
    ['F1', 'This sheet'],
  ];
  const body = el('div', { class: 'help-grid' },
    rows.map(([k, v]) => el('div', { class: 'row' }, el('kbd', { text: k }), el('span', { text: v }))),
  );
  openModal({ title: 'Keyboard shortcuts', body, wide: true, buttons: [{ label: 'Close' }] });
}

// ---------- usage guide + supported data sources (Help menu) ----------

// Guide sections rendered as tabs. Content stays English (like the admin
// panel and the keyboard map): the menu labels are localized, the prose
// is stable documentation.
const GUIDE_SECTIONS = [
  ['Getting started', [
    ['Add a data source', 'Click the + button next to DATA SOURCES on the left (or the button on the empty state). Every connection is a data source; click one to browse it in the main view.'],
    ['Import existing credentials', '"Import S3 Credential" (File menu or the empty state) reads credential files — AWS INI (~/.aws/credentials), rclone, JSON, .env, encrypted .s3bprofile — or a KMS service (Vault, AWS SM, Azure, GCP). Profiles with an endpoint_url become MinIO/R2/Wasabi/… sources; plain profiles connect to Amazon S3. Test each candidate before importing.'],
    ['Save your workspace', 'Data sources live in the session until saved. Ctrl+S / File → Save As writes an encrypted .s3bprofile you can reopen, keep or share; the status bar counts unsaved sources.'],
    ['Secrets', 'Keys and passwords are stored in the OS keyring (Windows Credential Manager, macOS Keychain, Linux SecretService) when available, with a 0600-permission file fallback on headless hosts.'],
  ]],
  ['Browsing', [
    ['Sidebar tree', 'Sources → buckets → folders. Click to navigate; right-click a node for Properties, Admin panel, transfers and more.'],
    ['Grid', 'Click, Ctrl+click and Shift+click to select, Ctrl+A for all, drag a marquee, or just type to jump to an item. The funnel row under the header filters per column; Ctrl+F focuses the quick filter.'],
    ['Path bar', 'The breadcrumb shows where you are; click it (or the edit icon) and type a path like s3://bucket/folder/ to jump directly. Back / forward / up history works like Explorer.'],
    ['Dual pane', 'F9 opens a local-filesystem pane (or another source) beside the main view — drag between panes, and Compare Any color-codes newer/older/size-diff/only-here.'],
  ]],
  ['Transfers', [
    ['Upload', 'Toolbar ▲ and the context menus open one Upload menu: Files… (Ctrl+U) picks files, Folder… a whole directory tree — or just drag files/folders from the OS anywhere onto the window.'],
    ['Download', 'Toolbar ▼, Ctrl+D, Enter, or the context menu. Multistep downloads/uploads are multipart and resumable per file.'],
    ['Copy & move', 'Ctrl+C / Ctrl+X / Ctrl+V, or drag rows onto folders, the tree, or the other pane. Same-source S3 copies run server-side; hold Shift while dragging to force a move.'],
    ['Conflicts & speed', 'Every transfer asks for a conflict policy (overwrite / skip / rename) unless a default is set in Settings, and can be throttled (256 kB/s … 10 MB/s).'],
    ['Transfer manager', 'View → Transfers (or the status-bar counter) shows every job with per-file and byte-level progress, speed and cancel.'],
  ]],
  ['Versions & safety', [
    ['Versioning', 'Buckets with versioning show a 🔄 icon in the tree. Open an object\u2019s context menu → Versions for the timeline: restore a previous version as latest, view text diffs, or purge old versions.'],
    ['Undo delete', 'Deleted objects leave a delete marker — "Versions → undo delete" brings the object back in one click. On versioned buckets Del asks marker-vs-permanent; Shift+Del goes straight to permanent. Rows with markers in their history carry a ⛔ badge; a folder whose every file is delete-marked shows ⛔ all deleted.'],
    ['Object Lock', 'Locked buckets show a 🔒 icon; retention (GOVERNANCE/COMPLIANCE) and legal hold are per version, with the same confirm gates as the CLI.'],
    ['Safety ladder', 'Deletes count first and act second; large selections require a typed confirmation; removing a bucket means typing its name.'],
  ]],
  ['Administration', [
    ['Admin panel', 'Right-click a bucket → Admin panel: versioning, policy, ACL, CORS, lifecycle, encryption, public-access block, website, tags, versions and lock — one tabbed dialog.'],
    ['Doctor', 'Help → Doctor runs a guided diagnosis: DNS → TCP → TLS → auth → permissions, with one-click re-runs of individual checks.'],
    ['Presign & storage class', 'The context menu creates time-limited pre-signed URLs and converts objects between storage classes (server-side copy).'],
    ['Properties', 'Context menu → Properties shows full metadata for buckets, folders, objects and sources — provider, region, versioning, lock, encryption, policy state.'],
  ]],
  ['Tips & tricks', [
    ['Find anything', 'Ctrl+Shift+F deep-searches every object under the open bucket/folder by name glob, size, age or storage class; results stream in and are cancelable.'],
    ['Local log', 'Ctrl+L toggles the event log; Settings can mirror it to a file.'],
    ['Portable mode', 'Drop an empty s3b-portable marker file next to the binary and all settings stay beside it — perfect for USB sticks.'],
    ['Same binary, full CLI', 's3b on the terminal drives the same engine: ls, cp, sync, find, doctor, bucket admin and more — see `s3b --help`.'],
  ]],
];

export function usageGuideDialog() {
  const strip = el('div', { class: 'tabstrip' });
  const content = el('div', { class: 'tabbody' });
  const select = (name) => {
    strip.querySelectorAll('.tab').forEach((t) => t.classList.toggle('active', t.dataset.tab === name));
    const [, entries] = GUIDE_SECTIONS.find(([n]) => n === name);
    content.replaceChildren(el('div', { class: 'guide' },
      entries.map(([h, text]) => el('div', { class: 'guide-item' },
        el('div', { class: 'guide-h', text: h }),
        el('div', { class: 'guide-p', text }),
      )),
    ));
  };
  strip.replaceChildren(...GUIDE_SECTIONS.map(([name]) =>
    el('div', { class: 'tab', 'data-tab': name, text: name, onclick: () => select(name) })));
  openModal({
    title: 'User guide',
    body: el('div', { class: 'admin' }, strip, content),
    cls: 'admin-modal',
    buttons: [{ label: 'Close' }],
  });
  select(GUIDE_SECTIONS[0][0]);
}

const SOURCE_KINDS = [
  ['S3 — Amazon S3 and any S3-compatible endpoint', [
    'Access key + secret key, optional region and path-style addressing; custom endpoints are typed as a URL (https://…).',
    'Well-known providers are detected automatically and their capabilities shown in the Admin panel:',
    'MinIO / AIStor — full S3, synthetic ACLs · Wasabi — very AWS-compatible · Cloudflare R2 — IAM-only policies, no path-style · Backblaze B2 — IAM-only policies, no ACL APIs · DigitalOcean Spaces — AWS-like · IBM Cloud Object Storage — full · Hetzner Storage Boxes — basic S3 · Ceph RGW — complete S3 · Dell ECS · NetApp StorageGRID',
  ]],
  ['Remote filesystems', [
    'SFTP and SCP (ssh) — host, port 22 default, username/password, anchored root path.',
    'FTP and FTPS — plain and TLS, port 21/990 defaults.',
    'WebDAV and WebDAVs — RFC 4918 over HTTP(S), port 80/443 defaults; works with Apache, nginx, rclone serve webdav, Nextcloud, IIS.',
    'All of them browse, upload, download, rename and delete like any other source — and join the transfer matrix (drag & drop, copy/paste, compare) with S3 and the local pane.',
  ]],
  ['Local filesystem', [
    'The dual-pane side (F9) browses local drives and folders, and any source type can be bound to it.',
  ]],
  ['CLI parity', [
    'The CLI accepts the same connections as URIs: s3://, sftp://, scp://, ftp://, ftps://, webdav://, webdavs:// — see `s3b source add --help`.',
  ]],
];

export function sourcesInfoDialog() {
  const body = el('div', { class: 'guide' },
    SOURCE_KINDS.map(([name, lines]) => el('div', { class: 'guide-item' },
      el('div', { class: 'guide-h', text: name }),
      ...lines.map((l) => el('div', { class: 'guide-p', text: l })),
    )),
  );
  openModal({ title: 'Supported data sources', body, wide: true, buttons: [{ label: 'Close' }] });
}

// ---------- conflict policy + transfer throttle ----------
const RATE_LIMITS = [
  [0, 'Unlimited'],
  [262144, '256 KB/s'],
  [1048576, '1 MB/s'],
  [2097152, '2 MB/s'],
  [5242880, '5 MB/s'],
  [10485760, '10 MB/s'],
];

// savedThrottle returns the remembered speed limit. The control is hidden
// by default; Settings ("Show speed limit when transferring") opts in
// (localStorage s3b-show-throttle).
function savedThrottle() {
  return parseInt(localStorage.getItem('s3b-throttle') || '0', 10) || 0;
}

export function showThrottle() {
  return localStorage.getItem('s3b-show-throttle') === '1';
}

// resolveTransferOpts decides how a transfer starts:
//   1. a saved conflict default (Settings) applies silently;
//   2. else the destination is probed (check) — a clean destination
//      starts immediately, no dialog at all;
//   3. only real collisions show the per-file conflict dialog.
// check is () => Promise<ConflictInfo[]>; a throwing/failed pre-check
// falls back to the whole-transfer dialog (classic behavior).
// Resolves null (canceled) or { policy, maxBps, decisions }.
export async function resolveTransferOpts(kind, target, check) {
  const preset = localStorage.getItem('s3b-conflict') || 'ask';
  if (preset !== 'ask') {
    return { policy: preset, maxBps: savedThrottle(), decisions: null };
  }
  let conflicts = null;
  if (check) {
    try { conflicts = await check(); } catch { conflicts = null; }
  }
  if (conflicts && conflicts.length) {
    return conflictChoices(kind, target, conflicts);
  }
  // Clean destination (or pre-check unavailable): no dialog. The classic
  // whole-transfer policy dialog only appears when the pre-check itself
  // failed — the rare degenerate case.
  if (conflicts) {
    return { policy: 'overwrite', maxBps: savedThrottle(), decisions: null };
  }
  return classicTransferDialog(kind, target);
}

// classicTransferDialog is the whole-transfer fallback (no per-file rows):
// used when the pre-check could not run (too many files, stat failures).
function classicTransferDialog(kind, target) {
  let settled = false;
  return new Promise((resolve) => {
    const done = (v) => { if (!settled) { settled = true; resolve(v); } };
    const options = [
      ['overwrite', t('transfer.overwrite'), t('transfer.overwriteSub')],
      ['skip', t('transfer.skip'), t('transfer.skipSub')],
      ['rename', t('transfer.rename'), t('transfer.renameSub')],
    ];
    let choice = 'overwrite';
    const list = el('div', {}, options.map(([id, label, sub]) => {
      const r = el('input', { type: 'radio', name: 'policy', ...(id === choice ? { checked: true } : {}) });
      r.addEventListener('change', () => { choice = id; });
      return el('label', { style: 'display:flex;gap:8px;align-items:flex-start;padding:6px 0;cursor:pointer' },
        r,
        el('div', {}, el('div', { text: label, style: 'font-weight:600' }), el('div', { text: sub, style: 'color:var(--text-dim)' })),
      );
    }));
    const rate = throttleSelect();
    openModal({
      title: t('transfer.classicTitle', { kind: kindLabel(kind), target }),
      body: el('div', {},
        list,
        rate.wrap),
      buttons: [
        { label: t('dlg.cancel'), onclick: (c) => { done(null); c(); } },
        {
          label: t('transfer.start'),
          class: 'primary',
          onclick: (c) => {
            if (rate.save) localStorage.setItem('s3b-throttle', String(rate.value()));
            done({ policy: choice, maxBps: rate.value(), decisions: null });
            c();
          },
        },
      ],
      onClose: () => done(null),
    });
  });
}

function kindLabel(kind) {
  return t(kind === 'upload' ? 'transfer.kindUpload' : kind === 'transfer' ? 'transfer.kindTransfer' : 'transfer.kindDownload');
}

// throttleSelect builds the speed-limit control; hidden unless opted in
// via Settings. Returns { wrap, value, save }.
function throttleSelect() {
  if (!showThrottle()) {
    return { wrap: el('div'), value: () => savedThrottle(), save: false };
  }
  const sel = el('select', { class: 'input', style: 'width:auto' },
    RATE_LIMITS.map(([v, label]) => el('option', { value: String(v) }, label)));
  sel.value = String(savedThrottle());
  const wrap = el('label', { class: 'field', style: 'display:flex;align-items:center;gap:8px;margin-top:10px' },
    t('transfer.speedLimit'), sel);
  return { wrap, value: () => parseInt(sel.value, 10) || 0, save: true };
}

// conflictChoices shows the per-file conflict list: both sides' size and
// modification time, a checkbox per row, select/unselect all, and bulk
// actions for the selection. Each row carries its own action (default
// overwrite); Start resolves { policy, maxBps, decisions } where decisions
// maps the engine's decision key to the per-file action.
function conflictChoices(kind, target, conflicts) {
  let settled = false;
  return new Promise((resolve) => {
    const done = (v) => { if (!settled) { settled = true; resolve(v); } };
    const actions = conflicts.map(() => 'overwrite');
    const checked = conflicts.map(() => true);

    const summary = el('div', { class: 'cf-summary' });
    const updateSummary = () => {
      const n = { overwrite: 0, skip: 0, rename: 0 };
      for (const a of actions) n[a]++;
      summary.textContent = `${n.overwrite} ${t('transfer.overwrite')} · ${n.skip} ${t('transfer.skip')} · ${n.rename} ${t('transfer.rename')}`;
    };

    const master = el('input', { type: 'checkbox' });
    master.checked = true;
    master.addEventListener('change', () => {
      rows.forEach(({ check }, i) => { check.checked = master.checked; checked[i] = master.checked; });
    });

    const rows = conflicts.map((c, i) => {
      const check = el('input', { type: 'checkbox' });
      check.checked = true;
      check.addEventListener('change', () => {
        checked[i] = check.checked;
        master.checked = checked.every(Boolean);
      });
      const sel = el('select', { class: 'input cf-action' },
        [['overwrite', t('transfer.overwrite')], ['skip', t('transfer.skip')], ['rename', t('transfer.rename')]]
          .map(([v, l]) => el('option', { value: v }, l)));
      sel.value = actions[i];
      sel.addEventListener('change', () => { actions[i] = sel.value; updateSummary(); });
      const row = el('div', { class: 'cf-row' },
        el('label', { class: 'cf-check' }, check),
        el('div', { class: 'cf-name', text: c.key, title: c.decisionKey }),
        el('div', { class: 'cf-side' },
          el('div', { class: 'cf-tag', text: t('transfer.source') }),
          el('div', { text: fmtBytes(c.srcSize) }),
          el('div', { class: 'cf-time', text: fmtDate(c.srcTime) })),
        el('div', { class: 'cf-side' },
          el('div', { class: 'cf-tag', text: t('transfer.destination') }),
          el('div', { text: fmtBytes(c.dstSize) }),
          el('div', { class: 'cf-time', text: fmtDate(c.dstTime) })),
        sel,
      );
      return { row, check, sel };
    });

    const applyBulk = (action) => {
      rows.forEach((r, i) => {
        if (!checked[i]) return;
        actions[i] = action;
        r.sel.value = action;
      });
      updateSummary();
    };
    const bulk = el('div', { class: 'cf-bulk' },
      el('span', { class: 'cf-bulk-label', text: t('transfer.applySelected') }),
      el('button', { class: 'btn', text: t('transfer.overwrite'), onclick: () => applyBulk('overwrite') }),
      el('button', { class: 'btn', text: t('transfer.skip'), onclick: () => applyBulk('skip') }),
      el('button', { class: 'btn', text: t('transfer.rename'), onclick: () => applyBulk('rename') }),
    );

    const rate = throttleSelect();
    const header = el('div', { class: 'cf-head' },
      el('label', { class: 'cf-check' }, master),
      el('div', { class: 'cf-name', text: t('transfer.file') }),
      el('div', { class: 'cf-side' }, el('div', { class: 'cf-tag', text: t('transfer.source') })),
      el('div', { class: 'cf-side' }, el('div', { class: 'cf-tag', text: t('transfer.destination') })),
      el('div', {}),
    );

    openModal({
      title: t('transfer.conflictTitle', { n: conflicts.length }),
      cls: 'cf-modal',
      wide: true,
      body: el('div', {},
        el('div', { class: 'field', text: t('transfer.conflictIntro') }),
        header,
        el('div', { class: 'cf-list' }, rows.map((r) => r.row)),
        bulk,
        summary,
        rate.wrap),
      buttons: [
        { label: t('dlg.cancel'), onclick: (c) => { done(null); c(); } },
        {
          label: t('transfer.start'),
          class: 'primary',
          onclick: (c) => {
            if (rate.save) localStorage.setItem('s3b-throttle', String(rate.value()));
            const decisions = {};
            conflicts.forEach((cf, i) => { decisions[cf.decisionKey] = actions[i]; });
            done({ policy: 'overwrite', maxBps: rate.value(), decisions });
            c();
          },
        },
      ],
      onClose: () => done(null),
    });
    updateSummary();
  });
}

// ---------- presign ----------
export function presignDialog(url) {
  const input = el('input', { class: 'input mono', value: url, readonly: 'readonly' });
  openModal({
    title: 'Pre-signed URL (expires as configured)',
    body: el('div', {}, el('label', { class: 'field', text: 'Share this URL — anyone with it can download the object' }), input),
    buttons: [
      {
        label: 'Copy',
        onclick: async () => {
          input.select();
          try { await navigator.clipboard.writeText(url); } catch { document.execCommand('copy'); }
        },
      },
      { label: 'Close', class: 'primary' },
    ],
  });
  input.focus();
  input.select();
}

// presignListDialog shows one pre-signed URL per selected object with
// copy-per-row and copy-all (multi-run presign — signing itself is local).
export function presignListDialog(list) {
  const copyText = async (text) => {
    try { await navigator.clipboard.writeText(text); } catch { /* clipboard unavailable */ }
  };
  openModal({
    title: `Pre-signed URLs — ${list.length} object(s)`,
    body: el('div', {},
      el('div', { class: 'field', text: 'Anyone with a URL can download that object (expires as configured).' }),
      el('div', { class: 'psn-list' }, list.map(({ name, url }) => el('div', { class: 'psn-row' },
        el('span', { class: 'psn-name', text: name, title: url }),
        el('input', { class: 'input mono', value: url, readonly: 'readonly' }),
        el('button', { class: 'btn', text: 'Copy', onclick: () => copyText(url) }),
      ))),
    ),
    buttons: [
      { label: 'Copy all', onclick: () => copyText(list.map((x) => `${x.name}\t${x.url}`).join('\n')) },
      { label: 'Close', class: 'primary' },
    ],
    wide: true,
  });
}

// ---------- object versions (M4) ----------
const asMillis = (v) => (typeof v === 'string' ? Date.parse(v) : v);

export function versionsDialog(bucket, key, onChanged) {
  const list = el('div', { class: 'ver-list' });
  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)' });
  // A/B version picks (Panels v2): any two versions can be compared —
  // metadata always, a unified text diff when both sides are text.
  const pick = { a: null, b: null };
  const pickBar = el('div', { class: 'ver-pickbar' });

  const verShort = (v) => (v
    ? `${v.lastModified ? fmtDate(asMillis(v.lastModified)) : ''}${v.versionId ? ` \u00b7 \u2026${v.versionId.slice(-6)}` : ''}`
    : '\u2014');
  const renderPickBar = () => {
    const ready = pick.a && pick.b && pick.a.versionId !== pick.b.versionId;
    pickBar.replaceChildren(
      el('span', { class: 'ver-picklbl', text: `A: ${verShort(pick.a)}` }),
      el('span', { class: 'ver-picklbl', text: `B: ${verShort(pick.b)}` }),
      el('button', {
        class: 'btn', text: 'Compare A \u2194 B', disabled: !ready,
        title: 'Compare the two picked versions (metadata + text diff)',
        onclick: () => versionDiffDialog(bucket, key, pick.a, pick.b),
      }),
    );
  };

  const act = async (fn, msg) => {
    try {
      await fn();
      toast(msg, 'ok');
      onChanged?.();
      draw();
    } catch (err) {
      toast(`Failed: ${err}`, 'error');
    }
  };
  const destroy = async (v) => {
    if (!(await typedConfirm({
      title: 'Permanently delete version',
      message: `This destroys one version of s3://${bucket}/${key}.\nIt cannot be recovered — not even from version history.`,
      typeWord: 'permanent',
    }))) return;
    act(() => api.DeleteVersionPermanent(bucket, key, v.versionId), 'Version destroyed');
  };

  async function draw() {
    status.textContent = 'Loading…';
    let vers;
    try {
      vers = await api.ObjectVersions(bucket, key);
    } catch (err) {
      list.replaceChildren();
      status.textContent = String(err);
      status.style.color = 'var(--danger)';
      return;
    }
    status.style.color = 'var(--text-dim)';
    status.textContent = vers.length
      ? `${vers.length} version(s), newest first`
      : 'No versions — versioning is off or the object never existed.';
    // Drop picks that vanished (e.g. destroyed in another window).
    if (pick.a && !vers.some((v) => v.versionId === pick.a.versionId)) pick.a = null;
    if (pick.b && !vers.some((v) => v.versionId === pick.b.versionId)) pick.b = null;
    renderPickBar();
    // vs-current (M10.4): each old version shows its size delta against the
    // current one and can be diffed against it with one click.
    const latest = vers.find((v) => v.isLatest && !v.isDeleteMarker);
    const deltaChip = (v) => {
      if (!latest || v.isLatest) return null;
      if (v.etag && v.etag === latest.etag) return el('span', { class: 'tag', text: 'identical' });
      const d = (v.size || 0) - (latest.size || 0);
      return el('span', { class: 'ver-delta', text: `${d >= 0 ? '+' : '\u2212'}${fmtBytes(Math.abs(d))} vs current` });
    };
    list.replaceChildren(...vers.map((v) => el('div', { class: `ver-row${v.isLatest ? ' latest' : ''}` },
      el('span', { class: 'ver-icon', text: v.isDeleteMarker ? '\u26D4' : (v.isLatest ? '\u25CF' : '\u25CB') }),
      el('span', { class: 'ver-main' },
        el('div', { text: v.isDeleteMarker ? 'Delete marker (object hidden)' : `${fmtBytes(v.size)} — ${v.storageClass || 'STANDARD'}${v.etag ? ` — ${v.etag}` : ''}` }),
        el('div', { class: 'ver-sub', text: `${v.lastModified ? fmtDate(asMillis(v.lastModified)) : ''}${v.versionId ? ` — ${v.versionId}` : ''}` }),
      ),
      el('span', { class: 'ver-actions' },
        deltaChip(v),
        ...(v.versionId && !v.isDeleteMarker
          ? ['a', 'b'].map((side) => el('button', {
              class: `btn ver-pick${pick[side]?.versionId === v.versionId ? ' on' : ''}`,
              text: side.toUpperCase(),
              title: `Pick as side ${side.toUpperCase()} of the compare`,
              onclick: () => { pick[side] = pick[side]?.versionId === v.versionId ? null : v; draw(); },
            }))
          : []),
        v.isLatest && !v.isDeleteMarker ? el('span', { class: 'tag', text: 'current' }) : null,
        !v.isLatest && !v.isDeleteMarker && latest
          ? el('button', { class: 'btn', text: 'vs current', title: 'Diff this version against the current one', onclick: () => versionDiffDialog(bucket, key, v, latest) })
          : null,
        !v.isLatest && !v.isDeleteMarker
          ? el('button', { class: 'btn', text: 'Restore as latest', onclick: () => act(() => api.RestoreVersion(bucket, key, v.versionId), 'Restored as latest') })
          : null,
        v.isDeleteMarker
          ? el('button', { class: 'btn', text: 'Undo delete', onclick: () => act(() => api.UndoDelete(bucket, key, v.versionId), 'Delete removed — object is back') })
          : null,
        el('button', { class: 'btn danger', text: 'Destroy', title: 'Delete this version permanently', onclick: () => destroy(v) }),
      ),
    )));
  }

  openModal({
    title: `Versions — s3://${bucket}/${key}`,
    body: el('div', {}, status, pickBar, list),
    wide: true,
    buttons: [{ label: 'Close' }],
  });
  draw();
}

// ---------- version compare (Panels v2) ----------
// versionDiffDialog compares two picked versions of one object: a metadata
// table (size / mtime / ETag / class) plus a unified line diff when both
// sides are text within the backend's 512 KB cap.
function versionDiffDialog(bucket, key, va, vb) {
  const short = (id) => (!id ? '(null version)' : (id.length > 12 ? `\u2026${id.slice(-8)}` : id));
  const meta = el('div', { class: 'verd-meta' });
  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)' });
  const diffBox = el('div', { class: 'diff' });

  const mrow = (label, a, b, head = false) => el('div', { class: `verd-mrow${head ? ' verd-mhead' : ''}` },
    el('span', { class: 'verd-mlbl', text: label }),
    el('span', { text: a }),
    el('span', { text: b }));

  openModal({
    title: `Compare versions — s3://${bucket}/${key}`,
    body: el('div', {}, meta, status, diffBox),
    wide: true,
    buttons: [{ label: 'Close' }],
  });

  meta.replaceChildren(
    mrow('', `A \u00b7 ${short(va.versionId)}`, `B \u00b7 ${short(vb.versionId)}`, true),
    mrow('Size', fmtBytes(va.size || 0), fmtBytes(vb.size || 0)),
    mrow('Last modified',
      va.lastModified ? fmtDate(asMillis(va.lastModified)) : '\u2014',
      vb.lastModified ? fmtDate(asMillis(vb.lastModified)) : '\u2014'),
    mrow('ETag', va.etag || '\u2014', vb.etag || '\u2014'),
    mrow('Storage class', va.storageClass || '\u2014', vb.storageClass || '\u2014'),
  );

  if (va.etag && va.etag === vb.etag) {
    status.textContent = 'ETags match — the two versions hold identical content.';
    return;
  }

  status.textContent = 'Loading contents\u2026';
  api.VersionDiffText(bucket, key, va.versionId, vb.versionId).then((res) => {
    if (res.skipped) { status.textContent = res.skipped; return; }
    status.textContent = res.truncated
      ? 'Text exceeds the 512 KB diff cap — showing the first 512 KB of each side.'
      : '';
    renderDiff(diffBox, res.aText, res.bText);
  }).catch((err) => {
    status.textContent = String(err);
    status.style.color = 'var(--danger)';
  });
}

// renderDiff paints a unified line diff: common prefix/suffix are trimmed,
// the changed middle runs through an LCS table (huge rewrites fall back to
// one wholesale replacement block), and long equal runs collapse to markers.
function renderDiff(box, aText, bText) {
  const A = String(aText ?? '').replace(/\r\n/g, '\n').split('\n');
  const B = String(bText ?? '').replace(/\r\n/g, '\n').split('\n');
  let p = 0;
  while (p < A.length && p < B.length && A[p] === B[p]) p++;
  let s = 0;
  while (s < A.length - p && s < B.length - p && A[A.length - 1 - s] === B[B.length - 1 - s]) s++;
  const midA = A.slice(p, A.length - s);
  const midB = B.slice(p, B.length - s);
  const rows = [];
  if (midA.length && midB.length && midA.length * midB.length > 4_000_000) {
    for (let i = 0; i < midA.length; i++) rows.push({ t: '-', text: midA[i], la: p + i + 1 });
    for (let j = 0; j < midB.length; j++) rows.push({ t: '+', text: midB[j], lb: p + j + 1 });
  } else {
    const n = midA.length, m = midB.length, w = m + 1;
    const dp = new Int32Array((n + 1) * w);
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        dp[i * w + j] = midA[i] === midB[j]
          ? dp[(i + 1) * w + j + 1] + 1
          : Math.max(dp[(i + 1) * w + j], dp[i * w + j + 1]);
      }
    }
    let i = 0, j = 0;
    while (i < n && j < m) {
      if (midA[i] === midB[j]) { rows.push({ t: ' ', text: midA[i], la: p + i + 1, lb: p + j + 1 }); i++; j++; }
      else if (dp[(i + 1) * w + j] >= dp[i * w + j + 1]) { rows.push({ t: '-', text: midA[i], la: p + i + 1 }); i++; }
      else { rows.push({ t: '+', text: midB[j], lb: p + j + 1 }); j++; }
    }
    for (; i < n; i++) rows.push({ t: '-', text: midA[i], la: p + i + 1 });
    for (; j < m; j++) rows.push({ t: '+', text: midB[j], lb: p + j + 1 });
  }
  const full = [
    ...A.slice(0, p).map((text, k) => ({ t: ' ', text, la: k + 1, lb: k + 1 })),
    ...rows,
    ...A.slice(A.length - s).map((text, k) => ({ t: ' ', text, la: A.length - s + k + 1, lb: B.length - s + k + 1 })),
  ];
  // Keep 3 context lines around every change; collapse the remaining runs.
  const keep = new Array(full.length).fill(false);
  full.forEach((r, idx) => {
    if (r.t !== ' ') for (let k = Math.max(0, idx - 3); k <= Math.min(full.length - 1, idx + 3); k++) keep[k] = true;
  });
  const shown = [];
  let inGap = false;
  for (let k = 0; k < full.length; k++) {
    if (keep[k]) { shown.push(full[k]); inGap = false; }
    else if (!inGap) { shown.push({ t: '@' }); inGap = true; }
  }
  const cap = 4000;
  const clipped = shown.length > cap;
  const draw = (r) => (r.t === '@'
    ? el('div', { class: 'diff-row gap', text: '\u00b7\u00b7\u00b7' })
    : el('div', { class: `diff-row ${r.t === ' ' ? 'ctx' : r.t === '-' ? 'del' : 'add'}` },
        el('span', { class: 'diff-ln', text: r.la != null ? String(r.la) : '' }),
        el('span', { class: 'diff-ln', text: r.lb != null ? String(r.lb) : '' }),
        el('span', { class: 'diff-sign', text: r.t === ' ' ? '' : r.t }),
        el('span', { class: 'diff-text', text: r.text })));
  box.replaceChildren(
    ...(clipped ? shown.slice(0, cap) : shown).map(draw),
    ...(clipped ? [el('div', { class: 'diff-row gap', text: `\u00b7\u00b7\u00b7 ${shown.length - cap} more rows not shown` })] : []),
  );
}

// ---------- bucket admin panel (M3) ----------
export function adminDialog(bucket, onChanged) {
  const TABS = ['Overview', 'Security', 'Policy', 'ACL', 'CORS', 'Lifecycle', 'Encryption', 'Website', 'Tags', 'Versions', 'Lock'];
  const strip = el('div', { class: 'tabstrip' });
  const content = el('div', { class: 'tabbody' });
  let panel = null;
  let active = 'Overview';

  const errBox = (e) => el('div', { class: 'banner warn', text: String(e) });

  async function save(fn, okMsg) {
    try {
      await fn();
      toast(okMsg, 'ok');
      await reload();
      onChanged?.();
    } catch (e) {
      toast(`Failed: ${e}`, 'error');
    }
  }
  async function reload() {
    content.replaceChildren(el('div', { class: 'field', text: 'Loading…' }));
    panel = await api.GetBucketAdmin(bucket);
    drawTab(active);
  }
  function select(name) {
    active = name;
    strip.querySelectorAll('.tab').forEach((t) => t.classList.toggle('active', t.dataset.tab === name));
    drawTab(name);
  }

  function drawTab(name) {
    if (!panel) return;
    switch (name) {
      case 'Overview': {
        content.replaceChildren(
          panel.publicWarning ? el('div', { class: 'banner danger', text: `\u26A0 ${panel.publicWarning}` }) : null,
          el('div', { class: 'kv' },
            el('div', { class: 'k', text: 'Bucket' }), el('div', { class: 'v mono', text: bucket }),
            el('div', { class: 'k', text: 'Region' }), el('div', { class: 'v mono', text: panel.region || '—' }),
            el('div', { class: 'k', text: 'Versioning' }), el('div', { class: 'v', text: panel.versions || 'off (never configured)' }),
            el('div', { class: 'k', text: 'Object Lock' }), el('div', { class: 'v', text: panel.lock?.enabled
              ? `enabled — ${panel.lock.mode || 'on'}${panel.lock.days ? `, ${panel.lock.days}d default retention` : ''}` : 'off' }),
            el('div', { class: 'k', text: 'Default encryption' }), el('div', { class: 'v', text: panel.encryption?.algorithm
              ? `${panel.encryption.algorithm}${panel.encryption.kmsKeyId ? ` (${panel.encryption.kmsKeyId})` : ''}` : 'none set' }),
            el('div', { class: 'k', text: 'Public access block' }), el('div', { class: 'v', text: panel.pab
              ? `${['blockPublicAcls', 'ignorePublicAcls', 'blockPublicPolicy', 'restrictPublicBuckets'].filter((k) => panel.pab[k]).length} of 4 on`
              : '—' }),
            el('div', { class: 'k', text: 's3:// URI' }), el('div', { class: 'v mono', text: `s3://${bucket}` }),
          ),
          el('div', { style: 'margin-top:12px' },
            el('button', {
              class: 'btn',
              text: panel.versions === 'Enabled' ? 'Suspend versioning' : 'Enable versioning',
              onclick: () => save(() => api.SetBucketVersioning(bucket, panel.versions !== 'Enabled'), 'Versioning updated'),
            })),
        );
        break;
      }
      case 'Security': {
        if (panel.pabErr) { content.replaceChildren(errBox(panel.pabErr)); break; }
        const fields = [
          ['blockPublicAcls', 'Block public ACLs'],
          ['ignorePublicAcls', 'Ignore public ACLs'],
          ['blockPublicPolicy', 'Block public bucket policies'],
          ['restrictPublicBuckets', 'Restrict public buckets'],
        ];
        const boxes = {};
        const box = el('div', { class: 'pab' }, fields.map(([k, label]) => {
          const c = el('input', { type: 'checkbox' });
          c.checked = !!(panel.pab || {})[k];
          boxes[k] = c;
          return el('label', { class: 'pab-row' }, c, ` ${label}`);
        }));
        content.replaceChildren(
          panel.publicWarning ? el('div', { class: 'banner danger', text: `\u26A0 ${panel.publicWarning}` }) : null,
          el('div', { class: 'field', text: 'Public access block (recommended: all on)' }),
          box,
          el('div', { style: 'margin-top:12px' },
            el('button', {
              class: 'btn primary',
              text: 'Save',
              onclick: () => save(() => api.PutBucketPAB(bucket, {
                blockPublicAcls: boxes.blockPublicAcls.checked,
                ignorePublicAcls: boxes.ignorePublicAcls.checked,
                blockPublicPolicy: boxes.blockPublicPolicy.checked,
                restrictPublicBuckets: boxes.restrictPublicBuckets.checked,
              }), 'Public access block saved'),
            })),
        );
        break;
      }
      case 'Policy': {
        if (panel.policyErr) { content.replaceChildren(errBox(panel.policyErr)); break; }
        const raw = el('textarea', { class: 'input mono', rows: '14', spellcheck: 'false' });
        raw.value = panel.policy?.raw || '{\n  "Version": "2012-10-17",\n  "Statement": []\n}';
        const s = panel.policy?.summary;
        content.replaceChildren(
          s ? el('div', { class: 'kv', style: 'margin-bottom:10px' },
            el('div', { class: 'k', text: 'Statements' }), el('div', { class: 'v', text: String(s.statementCount) }),
            el('div', { class: 'k', text: 'Public read' }), el('div', { class: 'v', text: s.hasPublicRead ? 'YES' : 'no' }),
            el('div', { class: 'k', text: 'Public write' }), el('div', { class: 'v', text: s.hasPublicWrite ? 'YES' : 'no' }),
          ) : null,
          ...(s?.warnings || []).map((w) => el('div', { class: 'banner warn', text: w })),
          raw,
          el('div', { style: 'display:flex;gap:8px;margin-top:10px' },
            el('button', { class: 'btn primary', text: 'Save policy', onclick: () => save(() => api.PutBucketPolicy(bucket, raw.value), 'Policy saved') }),
            el('button', { class: 'btn danger', text: 'Delete policy', onclick: () => save(() => api.DeleteBucketPolicy(bucket), 'Policy removed') }),
          ),
        );
        break;
      }
      case 'ACL': {
        if (panel.aclErr) { content.replaceChildren(errBox(panel.aclErr)); break; }
        const s = panel.acl?.summary || {};
        content.replaceChildren(
          el('div', { class: 'kv' },
            el('div', { class: 'k', text: 'Owner' }), el('div', { class: 'v mono', text: panel.acl?.owner || '—' }),
            el('div', { class: 'k', text: 'Public read' }), el('div', { class: 'v', text: s.publicRead ? 'YES' : 'no' }),
            el('div', { class: 'k', text: 'Authenticated read' }), el('div', { class: 'v', text: s.authenticatedRead ? 'YES' : 'no' }),
          ),
          (s.grants || []).length ? el('div', { class: 'field', style: 'margin-top:8px', text: `Grants: ${(s.grants || []).join(', ')}` }) : null,
          ...(s.warnings || []).map((w) => el('div', { class: 'banner warn', text: w })),
          el('div', { class: 'field', style: 'margin-top:10px;color:var(--text-dim)', text: 'ACLs are read-only here — manage access through the bucket policy (most providers deprecated bucket ACLs).' }),
        );
        break;
      }
      case 'CORS': {
        if (panel.corsErr) { content.replaceChildren(errBox(panel.corsErr)); break; }
        const rules = (panel.cors || []).map((r) => ({ ...r }));
        const rowsBox = el('div', {});
        const splitList = (v) => v.split(',').map((x) => x.trim()).filter(Boolean);
        function drawRules() {
          rowsBox.replaceChildren(...rules.map((r, i) => {
            const origins = el('input', { class: 'input mono', value: (r.origins || []).join(', '), spellcheck: 'false' });
            const methods = el('input', { class: 'input mono', value: (r.methods || []).join(', '), spellcheck: 'false' });
            const headers = el('input', { class: 'input mono', value: (r.headers || []).join(', '), spellcheck: 'false' });
            const expose = el('input', { class: 'input mono', value: (r.expose || []).join(', '), spellcheck: 'false' });
            const maxAge = el('input', { class: 'input', type: 'number', value: String(r.maxAge || '') });
            const bind = () => {
              r.origins = splitList(origins.value);
              r.methods = splitList(methods.value).map((x) => x.toUpperCase());
              r.headers = splitList(headers.value);
              r.expose = splitList(expose.value);
              r.maxAge = parseInt(maxAge.value, 10) || 0;
            };
            [origins, methods, headers, expose, maxAge].forEach((inp) => inp.addEventListener('change', bind));
            return el('div', { class: 'rule-card' },
              el('div', { class: 'rule-grid' },
                el('label', { class: 'lc-cell' }, 'Origins', origins),
                el('label', { class: 'lc-cell' }, 'Methods', methods),
                el('label', { class: 'lc-cell' }, 'Allow headers', headers),
                el('label', { class: 'lc-cell' }, 'Expose headers', expose),
                el('label', { class: 'lc-cell' }, 'Max age (s)', maxAge),
              ),
              el('button', { class: 'btn', text: 'Remove rule', onclick: () => { rules.splice(i, 1); drawRules(); } }),
            );
          }));
        }
        drawRules();
        content.replaceChildren(
          el('div', { class: 'field', text: 'Comma-separated lists; * allowed' }),
          rowsBox,
          el('div', { style: 'display:flex;gap:8px;margin-top:10px' },
            el('button', { class: 'btn', text: '+ Add rule', onclick: () => { rules.push({ origins: ['*'], methods: ['GET'] }); drawRules(); } }),
            el('button', { class: 'btn primary', text: 'Save', onclick: () => save(() => api.PutBucketCORS(bucket, rules), 'CORS saved') }),
            el('button', { class: 'btn danger', text: 'Delete all', onclick: () => save(() => api.DeleteBucketCORS(bucket), 'CORS removed') }),
          ),
        );
        break;
      }
      case 'Lifecycle': {
        if (panel.lifecycleErr) { content.replaceChildren(errBox(panel.lifecycleErr)); break; }
        const rules = (panel.lifecycle || []).map((r) => ({ ...r }));
        const rowsBox = el('div', {});
        function drawRules() {
          rowsBox.replaceChildren(...rules.map((r, i) => {
            const mkNum = (label, key) => {
              const inp = el('input', { class: 'input', type: 'number', value: String(r[key] || '') });
              inp.addEventListener('change', () => { r[key] = parseInt(inp.value, 10) || 0; });
              return el('label', { class: 'lc-cell' }, label, inp);
            };
            const id = el('input', { class: 'input', value: r.id || '' });
            id.addEventListener('change', () => { r.id = id.value; });
            const prefix = el('input', { class: 'input mono', value: r.prefix || '', spellcheck: 'false' });
            prefix.addEventListener('change', () => { r.prefix = prefix.value; });
            const cls = el('select', { class: 'input' },
              ['', 'STANDARD_IA', 'INTELLIGENT_TIERING', 'ONEZONE_IA', 'GLACIER_IR', 'GLACIER', 'DEEP_ARCHIVE']
                .map((c) => el('option', { value: c }, c || '—')));
            cls.value = r.transitionClass || '';
            cls.addEventListener('change', () => { r.transitionClass = cls.value; });
            const on = el('input', { type: 'checkbox' });
            on.checked = !!r.enabled;
            on.addEventListener('change', () => { r.enabled = on.checked; });
            const dm = el('input', { type: 'checkbox' });
            dm.checked = !!r.deleteMarker;
            dm.addEventListener('change', () => { r.deleteMarker = dm.checked; });
            return el('div', { class: `rule-card${r.enabled ? '' : ' off'}` },
              el('div', { class: 'lc-head' },
                el('label', { class: 'lc-cell' }, 'Rule ID', id),
                el('label', { class: 'lc-cell' }, 'Prefix filter', prefix),
                el('label', { style: 'display:flex;align-items:center;gap:6px' }, on, 'Enabled'),
              ),
              el('div', { class: 'rule-grid' },
                mkNum('Transition after (days)', 'transitionDays'),
                el('label', { class: 'lc-cell' }, 'Transition class', cls),
                mkNum('Expire after (days)', 'expirationDays'),
                mkNum('Expire noncurrent after (days)', 'noncurrentDays'),
                mkNum('Abort incomplete uploads after (days)', 'abortMpuDays'),
                el('label', { style: 'display:flex;align-items:center;gap:6px' }, dm, 'Expire delete markers'),
              ),
              el('div', { style: 'margin-top:8px' },
                el('button', { class: 'btn', text: 'Remove rule', onclick: () => { rules.splice(i, 1); drawRules(); } })),
            );
          }));
        }
        drawRules();
        content.replaceChildren(
          el('div', { class: 'field', text: 'Transition moves objects to cheaper storage after N days; expiration deletes them. At least one action per rule.' }),
          rowsBox,
          el('div', { style: 'display:flex;gap:8px;margin-top:10px' },
            el('button', { class: 'btn', text: '+ Add rule', onclick: () => { rules.push({ id: `rule-${rules.length + 1}`, enabled: true }); drawRules(); } }),
            el('button', { class: 'btn primary', text: 'Save', onclick: () => save(() => api.PutBucketLifecycle(bucket, rules), 'Lifecycle saved') }),
            el('button', { class: 'btn danger', text: 'Delete all', onclick: () => save(() => api.DeleteBucketLifecycle(bucket), 'Lifecycle removed') }),
          ),
        );
        break;
      }
      case 'Encryption': {
        if (panel.encryptionErr) { content.replaceChildren(errBox(panel.encryptionErr)); break; }
        const algo = el('select', { class: 'input', style: 'width:auto' }, [
          el('option', { value: 'AES256' }, 'AES256 (SSE-S3)'),
          el('option', { value: 'aws:kms' }, 'aws:kms (SSE-KMS)'),
        ]);
        algo.value = panel.encryption?.algorithm === 'aws:kms' ? 'aws:kms' : 'AES256';
        const kms = el('input', { class: 'input mono', value: panel.encryption?.kmsKeyId || '', placeholder: 'KMS key ARN / ID (aws:kms only)', spellcheck: 'false' });
        content.replaceChildren(
          el('div', { class: 'field', text: `Current: ${panel.encryption?.algorithm || 'none — objects are stored unencrypted by default'}` }),
          el('label', { class: 'field', text: 'Algorithm' }), algo,
          el('label', { class: 'field', text: 'KMS key' }), kms,
          el('div', { style: 'display:flex;gap:8px;margin-top:12px' },
            el('button', { class: 'btn primary', text: 'Save', onclick: () => save(() => api.PutBucketEncryption(bucket, algo.value, kms.value.trim()), 'Default encryption saved') }),
            el('button', { class: 'btn danger', text: 'Disable', onclick: () => save(() => api.DeleteBucketEncryption(bucket), 'Default encryption removed') }),
          ),
        );
        break;
      }
      case 'Website': {
        if (panel.websiteErr) { content.replaceChildren(errBox(panel.websiteErr)); break; }
        const w = panel.website || {};
        const index = el('input', { class: 'input mono', value: w.indexSuffix || 'index.html', spellcheck: 'false' });
        const errorKey = el('input', { class: 'input mono', value: w.errorKey || '', placeholder: '404.html', spellcheck: 'false' });
        const host = el('input', { class: 'input mono', value: w.redirectHost || '', placeholder: 'redirect all requests to host (optional)', spellcheck: 'false' });
        const proto = el('select', { class: 'input', style: 'width:auto' }, ['https', 'http'].map((p) => el('option', { value: p }, p)));
        proto.value = w.redirectProtocol || 'https';
        content.replaceChildren(
          el('div', { class: 'field', text: 'Static website hosting (endpoint URL depends on the provider)' }),
          el('label', { class: 'field', text: 'Index document' }), index,
          el('label', { class: 'field', text: 'Error document' }), errorKey,
          el('label', { class: 'field', text: 'Redirect all requests to host (overrides index/error)' }), host,
          el('label', { class: 'field', text: 'Redirect protocol' }), proto,
          el('div', { style: 'display:flex;gap:8px;margin-top:12px' },
            el('button', {
              class: 'btn primary',
              text: 'Save',
              onclick: () => save(() => api.PutBucketWebsite(bucket, {
                indexSuffix: index.value.trim(),
                errorKey: errorKey.value.trim(),
                redirectHost: host.value.trim(),
                redirectProtocol: host.value.trim() ? proto.value : '',
              }), 'Website configuration saved'),
            }),
            el('button', { class: 'btn danger', text: 'Disable', onclick: () => save(() => api.DeleteBucketWebsite(bucket), 'Website hosting disabled') }),
          ),
        );
        break;
      }
      case 'Tags': {
        if (panel.tagsErr) { content.replaceChildren(errBox(panel.tagsErr)); break; }
        const tags = (panel.tags || []).map((t) => ({ ...t }));
        const box = el('div', {});
        function drawTags() {
          box.replaceChildren(...tags.map((t, i) => {
            const k = el('input', { class: 'input mono', value: t.key, placeholder: 'key', spellcheck: 'false' });
            const v = el('input', { class: 'input mono', value: t.value, placeholder: 'value', spellcheck: 'false' });
            k.addEventListener('change', () => { t.key = k.value.trim(); });
            v.addEventListener('change', () => { t.value = v.value; });
            return el('div', { class: 'tag-row' }, k, v,
              el('button', { class: 'btn', text: '\u00D7', onclick: () => { tags.splice(i, 1); drawTags(); } }));
          }));
        }
        drawTags();
        content.replaceChildren(
          el('div', { class: 'field', text: 'Cost-allocation tags' }),
          box,
          el('div', { style: 'display:flex;gap:8px;margin-top:10px' },
            el('button', { class: 'btn', text: '+ Add tag', onclick: () => { tags.push({ key: '', value: '' }); drawTags(); } }),
            el('button', { class: 'btn primary', text: 'Save', onclick: () => save(() => api.PutBucketTags(bucket, tags), 'Tags saved') }),
            el('button', { class: 'btn danger', text: 'Delete all', onclick: () => save(() => api.DeleteBucketTags(bucket), 'Tags removed') }),
          ),
        );
        break;
      }
      case 'Versions': {
        content.replaceChildren(el('div', { class: 'field', text: 'Loading version statistics…' }));
        api.BucketVersionStats(bucket).then((st) => {
          const purge = async (mode, label) => {
            try {
              const n = await api.PurgePreview(bucket, '', mode);
              if (!n) { toast('Nothing to purge'); return; }
              const msg = `Permanently remove ${n} ${label} from s3://${bucket}.\nThis cannot be undone.`;
              const ok = n > 50
                ? await typedConfirm({ title: 'Purge versions', message: msg, typeWord: 'purge' })
                : await confirm({ title: 'Purge versions', message: msg, okLabel: 'Purge', danger: true });
              if (!ok) return;
              const res = await api.PurgeVersions(bucket, '', mode, true);
              toast(`Purged ${res.deleted} version(s)`, 'ok');
              onChanged?.();
              select('Versions');
            } catch (e) {
              toast(`Purge failed: ${e}`, 'error');
            }
          };
          content.replaceChildren(
            el('div', { class: 'kv' },
              el('div', { class: 'k', text: 'Versioning' }), el('div', { class: 'v', text: panel.versions || 'off' }),
              el('div', { class: 'k', text: 'Current objects' }), el('div', { class: 'v', text: String(st.currentObjects) }),
              el('div', { class: 'k', text: 'Total versions' }), el('div', { class: 'v', text: String(st.versions) }),
              el('div', { class: 'k', text: 'Delete markers' }), el('div', { class: 'v', text: String(st.deleteMarkers) }),
              el('div', { class: 'k', text: 'Noncurrent versions' }), el('div', { class: 'v', text: `${st.noncurrent} (${fmtBytes(st.noncurrentBytes)})` }),
            ),
            el('div', { class: 'field', style: 'margin-top:12px', text: 'Cleanup tools (bucket-wide, permanent)' }),
            el('div', { style: 'display:flex;flex-direction:column;gap:8px;align-items:flex-start' },
              el('button', { class: 'btn', text: 'Purge noncurrent versions\u2026', onclick: () => purge('noncurrent', 'noncurrent version(s)') }),
              el('button', { class: 'btn', text: 'Purge delete markers\u2026', onclick: () => purge('markers', 'delete marker(s)') }),
              el('button', {
                class: 'btn danger',
                text: 'Empty bucket (all versions)\u2026',
                onclick: async () => {
                  if (!(await typedConfirm({
                    title: `Empty bucket ${bucket}`,
                    message: `Every object and EVERY version in s3://${bucket} will be permanently destroyed.`,
                    typeWord: bucket,
                  }))) return;
                  try {
                    const res = await api.EmptyBucketAllVersions(bucket);
                    toast(`Emptied ${res.deleted} version(s)`, 'ok');
                    onChanged?.();
                  } catch (e) {
                    toast(`Empty failed: ${e}`, 'error');
                  }
                },
              }),
            ),
          );
        }).catch((e) => content.replaceChildren(errBox(e)));
        break;
      }
      case 'Lock': {
        if (panel.lockErr) { content.replaceChildren(errBox(panel.lockErr)); break; }
        const lock = panel.lock || {};
        const mode = el('select', { class: 'input', style: 'width:auto' },
          ['', 'GOVERNANCE', 'COMPLIANCE'].map((m) => el('option', { value: m }, m || '— no default retention —')));
        mode.value = lock.mode || '';
        const days = el('input', { class: 'input', type: 'number', min: '1', value: String(lock.days || '') , placeholder: 'days'});
        if (lock.enabled) {
          content.replaceChildren(
            el('div', { class: 'banner warn', text: 'Object lock is ENABLED — permanent: it cannot be disabled, only tightened.' }),
            el('div', { class: 'kv', style: 'margin-top:8px' },
              el('div', { class: 'k', text: 'Status' }), el('div', { class: 'v', text: 'enabled (permanent)' }),
              el('div', { class: 'k', text: 'Default mode' }), el('div', { class: 'v', text: lock.mode || '— none —' }),
              el('div', { class: 'k', text: 'Default days' }), el('div', { class: 'v', text: lock.days ? String(lock.days) : '—' }),
            ),
            el('div', { class: 'field', style: 'margin-top:12px', text: 'Adjust the default retention rule' }),
            el('div', { style: 'display:flex;gap:8px;align-items:center;flex-wrap:wrap' },
              mode, days,
              el('button', {
                class: 'btn primary', text: 'Save rule',
                onclick: () => save(() => api.PutBucketLockConfig(bucket, true, mode.value, parseInt(days.value, 10) || 0), 'Default retention updated'),
              }),
            ),
            el('div', { class: 'field', style: 'margin-top:10px;color:var(--text-dim)', text: 'Per-object retention and legal holds: right-click an object \u2192 Object lock\u2026' }),
          );
        } else {
          content.replaceChildren(
            el('div', { class: 'field', text: 'Object lock (WORM): once enabled it is permanent and versioning turns on automatically.' }),
            el('div', { class: 'field', style: 'color:var(--text-dim)', text: 'Most providers (AWS, MinIO) only allow enabling object lock at bucket creation — use "s3b mb --object-lock" and create a new bucket if this one rejects it. Optionally set a default retention applied to every new object.' }),
            el('div', { style: 'display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-top:8px' },
              mode, days,
              el('button', {
                class: 'btn danger', text: 'Enable object lock',
                onclick: async () => {
                  const ok = await typedConfirm({
                    title: `Enable object lock on ${bucket}`,
                    message: 'Object lock is permanent: it can never be disabled, only tightened.\nRetention-protected versions cannot be deleted until they expire.',
                    typeWord: bucket,
                    okLabel: 'Enable',
                    danger: false,
                  });
                  if (!ok) return;
                  save(() => api.PutBucketLockConfig(bucket, true, mode.value, parseInt(days.value, 10) || 0), 'Object lock enabled');
                },
              }),
            ),
          );
        }
        break;
      }
      default:
        content.replaceChildren(el('div', { text: name }));
    }
  }

  strip.replaceChildren(...TABS.map((t) => el('div', { class: 'tab', 'data-tab': t, text: t, onclick: () => select(t) })));
  openModal({
    title: `Admin panel — ${bucket}`,
    body: el('div', { class: 'admin' }, strip, content),
    cls: 'admin-modal',
    buttons: [{ label: 'Close' }],
  });
  reload().catch((e) => content.replaceChildren(errBox(e)));
}

// ---------- files open in external editor ----------
export function editingDialog(onChanged) {
  const list = el('div', {});
  openModal({
    title: 'Files open in editor',
    body: list,
    wide: true,
    buttons: [{ label: 'Close', onclick: (c) => { c(); onChanged?.(); } }],
  });
  const stop = async (f, upload) => {
    try {
      await api.StopEdit(f.bucket, f.key, upload);
      onChanged?.();
      draw();
    } catch (e) {
      toast(`Stop failed: ${e}`, 'error');
    }
  };
  async function draw() {
    let files;
    try {
      files = await api.EditingFiles();
    } catch (e) {
      list.replaceChildren(el('div', { text: String(e), style: 'color:var(--danger)' }));
      return;
    }
    list.replaceChildren(...(files.length ? files.map((f) => el('div', { class: 'tr-job' },
      el('div', { class: 'tr-top' },
        el('span', { class: 'tr-name', text: `${f.bucket}/${f.key}${f.dirty ? ' \u270E' : ''}` }),
        el('span', { class: 'tr-status mono', text: f.local }),
        el('button', { class: 'btn', text: 'Stop & upload', onclick: () => stop(f, true) }),
        el('button', { class: 'btn', text: 'Stop & discard', onclick: () => stop(f, false) }),
      ),
    )) : [el('div', { text: 'No files are being edited.', style: 'color:var(--text-dim)' })]));
  }
  draw();
}

// ---------- deep search (M5) ----------
// findDialog streams matches of a cancelable deep search under
// bucket/prefix. onOpen({bucket, prefix, key}) navigates to a result.
export function findDialog(bucket, prefix = '', onOpen) {
  const f = {
    name: el('input', { class: 'input mono', placeholder: 'report*', spellcheck: 'false' }),
    larger: el('input', { class: 'input mono', placeholder: '10MB', spellcheck: 'false' }),
    smaller: el('input', { class: 'input mono', placeholder: '500KB', spellcheck: 'false' }),
    older: el('input', { class: 'input mono', placeholder: '30d', spellcheck: 'false' }),
    newer: el('input', { class: 'input mono', placeholder: '24h', spellcheck: 'false' }),
    class: el('select', { class: 'input' },
      ['', 'STANDARD', 'REDUCED_REDUNDANCY', 'STANDARD_IA', 'ONEZONE_IA', 'INTELLIGENT_TIERING', 'GLACIER_IR', 'GLACIER', 'DEEP_ARCHIVE']
        .map((c) => el('option', { value: c }, c || '— any —'))),
    limit: el('input', { class: 'input', type: 'number', min: '0', value: '0' }),
  };
  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)' });
  const list = el('div', { class: 'ver-list', role: 'list' });
  let token = null;
  let running = false;
  let offPage = null;
  let offDone = null;

  const fmtRes = (r) => el('div', {
    class: 'ver-row',
    role: 'listitem',
    onclick: () => onOpen?.({ bucket: r.bucket || bucket, prefix: parentPrefix(r.key), key: r.key }),
  },
    el('span', { class: 'ver-icon', text: '\u{1F50D}' }),
    el('span', { class: 'ver-main' },
      el('div', { class: 'mono', text: r.key }),
      el('div', { class: 'ver-sub', text: `${fmtBytes(r.size || 0)} — ${r.storageClass || 'STANDARD'}${r.lastModified ? ` — ${fmtDate(r.lastModified)}` : ''}` }),
    ),
  );

  function stop() {
    if (token) { api.CancelSearch(token); token = null; }
    running = false;
    offPage?.();
    offDone?.();
    offPage = offDone = null;
  }

  openModal({
    title: `${t('findTitle')} — s3://${bucket}/${prefix || ''}`,
    body: el('div', {},
      el('div', { style: 'display:grid;grid-template-columns:1fr 1fr;gap:10px' },
        el('div', { style: 'grid-column:1/-1' }, el('label', { class: 'field', text: t('findName') }), f.name),
        el('div', {}, el('label', { class: 'field', text: t('findLarger') }), f.larger),
        el('div', {}, el('label', { class: 'field', text: t('findSmaller') }), f.smaller),
        el('div', {}, el('label', { class: 'field', text: t('findOlder') }), f.older),
        el('div', {}, el('label', { class: 'field', text: t('findNewer') }), f.newer),
        el('div', {}, el('label', { class: 'field', text: t('class') }), f.class),
        el('div', {}, el('label', { class: 'field', text: t('findLimit') }), f.limit),
      ),
      status,
      list,
    ),
    wide: true,
    buttons: [
      {
        label: t('findCancel'),
        onclick: (c) => { stop(); },
      },
      {
        label: t('findStart'),
        class: 'primary',
        onclick: async () => {
          let opts;
          try {
            opts = {
              pattern: f.name.value.trim(),
              largerThan: parseSizeStr(f.larger.value) || 0,
              smallerThan: parseSizeStr(f.smaller.value) || 0,
              olderThanSec: parseDurStr(f.older.value) || 0,
              newerThanSec: parseDurStr(f.newer.value) || 0,
              class: f.class.value,
              limit: parseInt(f.limit.value, 10) || 0,
            };
          } catch (err) {
            status.textContent = String(err);
            status.style.color = 'var(--danger)';
            return;
          }
          status.style.color = 'var(--text-dim)';
          status.textContent = t('findRunning', { matched: 0 });
          list.replaceChildren();
          stop(); // cancel any previous run
          running = true;
          // Subscribe BEFORE the call: a search over a small bucket can
          // finish (search:done) before the call resolving with the token
          // reaches the page, and events dispatched to no listener would
          // leave the dialog stuck on "running". Early events buffer and
          // replay once the token is known; foreign tokens drop out.
          const onPage = (p) => {
            for (const r of p.entries || []) list.appendChild(fmtRes(r));
            status.textContent = t('findRunning', { matched: p.matched });
            list.scrollTop = list.scrollHeight;
          };
          const onDone = (d) => {
            token = null;
            running = false;
            status.textContent = d.error
              ? d.error
              : t('findDone', { matched: d.matched, scanned: d.scanned, bucket, prefix: prefix || '' });
            if (d.error) status.style.color = 'var(--danger)';
          };
          const early = [];
          let tok = null;
          offPage = onEvent('search:page', (p) => {
            if (tok === null) { early.push({ page: p }); return; }
            if (p.token === tok) onPage(p);
          });
          offDone = onEvent('search:done', (d) => {
            if (tok === null) { early.push({ done: d }); return; }
            if (d.token === tok) onDone(d);
          });
          const myToken = await api.DeepSearch(bucket, prefix, opts);
          tok = myToken;
          if (!running) { api.CancelSearch(myToken); return; } // closed meanwhile
          token = myToken;
          for (const e of early.splice(0)) {
            if (e.page) onPage(e.page); else onDone(e.done);
          }
        },
      },
      { label: 'Close', onclick: (c) => { stop(); c(); } },
    ],
    onClose: () => stop(),
  });
  f.name.focus();
}

// ---------- storage-class conversion (M5) ----------
export function classDialog(bucket, rows, onChanged) {
  if (!rows?.length) return;
  const targets = ['STANDARD', 'REDUCED_REDUNDANCY', 'STANDARD_IA', 'ONEZONE_IA', 'INTELLIGENT_TIERING', 'GLACIER_IR', 'GLACIER', 'DEEP_ARCHIVE'];
  const sel = el('select', { class: 'input', style: 'width:auto' }, targets.map((c) => el('option', { value: c }, c)));
  sel.value = 'GLACIER';
  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)' });
  const folders = rows.filter((r) => r.isDir).length;

  const run = async (force) => {
    status.style.color = 'var(--text-dim)';
    status.textContent = 'Converting…';
    try {
      const n = await api.ConvertStorageClass(bucket, rows.map((r) => r.key), sel.value, force);
      status.textContent = `Converted ${n} object(s) to ${sel.value}`;
      toast(`Converted ${n} object(s) to ${sel.value}`, 'ok');
      onChanged?.();
    } catch (err) {
      const msg = String(err);
      if (!force && msg.includes('would convert')) {
        const m = msg.match(/would convert (\d+)/);
        const ok = await typedConfirm({
          title: 'Convert storage class',
          message: `${m ? m[0] : 'This batch'} — server-side copies every object. Continue?`,
          typeWord: 'convert',
          okLabel: 'Convert',
        });
        if (ok) run(true);
        else { status.textContent = 'Canceled'; }
      } else {
        status.textContent = msg;
        status.style.color = 'var(--danger)';
      }
    }
  };

  openModal({
    title: `Storage class — ${rows.length} item(s) in ${bucket}`,
    body: el('div', {},
      el('div', { class: 'field', text: `Converts via a server-side self-copy. ${folders ? `${folders} folder(s) expand${folders === 1 ? 's' : ''} recursively. ` : ''}Objects already in GLACIER/DEEP_ARCHIVE stay frozen — restoring needs an explicit restore.` }),
      el('label', { class: 'field', text: 'Target class' }), sel,
      status,
    ),
    buttons: [
      { label: 'Convert', class: 'primary', onclick: () => run(false) },
      { label: 'Close' },
    ],
  });
  sel.focus();
}

// ---------- batch runner (M10.3) ----------
// batchDialog runs one operation per item, sequentially, with live
// per-item status — the batch-progress surface for multi-run commands.
// run(item) throws to mark a failure; Stop (or closing) abandons the rest.
function batchDialog({ title, intro = '', items, run, onDone }) {
  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)', text: `0/${items.length}` });
  const list = el('div', { class: 'batch-list' });
  let stopped = false;

  const rowEls = items.map((it) => {
    const r = el('div', { class: 'batch-row pending' },
      el('span', { class: 'batch-ic', text: '\u23F3' }),
      el('span', { class: 'batch-lbl', text: it.label }));
    list.appendChild(r);
    return r;
  });
  const setRow = (i, state, note = '') => {
    rowEls[i].className = `batch-row ${state}`;
    rowEls[i].replaceChildren(
      el('span', { class: 'batch-ic', text: state === 'ok' ? '\u2713' : state === 'fail' ? '\u2717' : state === 'running' ? '\u25B6' : '\u23F3' }),
      el('span', { class: 'batch-lbl', text: items[i].label }),
      ...(note ? [el('span', { class: 'batch-note', text: note })] : []),
    );
  };

  openModal({
    title,
    body: el('div', {},
      ...(intro ? [el('div', { class: 'field', text: intro })] : []),
      status, list),
    buttons: [
      { label: 'Stop', onclick: () => { stopped = true; } },
      { label: 'Close', class: 'primary' },
    ],
    onClose: () => { stopped = true; },
  });

  (async () => {
    let ok = 0, fail = 0;
    for (let i = 0; i < items.length; i++) {
      if (stopped) break;
      setRow(i, 'running');
      try { await run(items[i]); ok++; setRow(i, 'ok'); }
      catch (err) { fail++; setRow(i, 'fail', String(err)); }
      status.textContent = `${ok + fail}/${items.length} — ${ok} ok${fail ? `, ${fail} failed` : ''}`;
    }
    status.textContent = stopped
      ? `Stopped — ${ok} ok, ${fail} failed, ${items.length - ok - fail} not run`
      : `Finished — ${ok} ok${fail ? `, ${fail} failed` : ''}`;
    onDone?.(ok, fail);
  })();
}

// ---------- object lock per object (M5; multi since M10.3) ----------
// lockDialog applies retention / legal hold to one object (with live state)
// or, for a multi-selection, to every selected object via the batch runner.
export function lockDialog(bucket, rows, onChanged) {
  if (!rows?.length) return;
  if (rows.length === 1) return lockDialogOne(bucket, rows[0], onChanged);

  const mode = el('select', { class: 'input', style: 'width:auto' },
    ['GOVERNANCE', 'COMPLIANCE'].map((m) => el('option', { value: m }, m)));
  const until = el('input', { class: 'input mono', value: '+30d', spellcheck: 'false' });

  const launch = (label, fn) => () => batchDialog({
    title: `${label} — ${rows.length} object(s)`,
    intro: `Applying to ${rows.length} object(s) in s3://${bucket}.`,
    items: rows.map((r) => ({ label: r.name, key: r.key })),
    run: (it) => fn(it.key),
    onDone: (ok) => { if (ok) onChanged?.(); },
  });

  openModal({
    title: `Object lock — ${rows.length} object(s) in ${bucket}`,
    body: el('div', {},
      el('div', { class: 'field', text: 'The four actions below run on every selected object, one at a time, with per-item results.' }),
      el('div', { class: 'field', style: 'margin-top:10px', text: 'Retention (current version of each object)' }),
      el('div', { style: 'display:flex;gap:8px;align-items:center;flex-wrap:wrap' },
        mode, until,
        el('button', { class: 'btn primary', text: 'Set\u2026', onclick: launch('Set retention', (key) => api.PutObjectRetention(bucket, key, '', mode.value, until.value.trim())) }),
        el('button', { class: 'btn', text: 'Clear\u2026', title: 'Removes GOVERNANCE retention (COMPLIANCE cannot be removed)', onclick: launch('Clear retention', (key) => api.ClearObjectRetention(bucket, key, '')) }),
      ),
      el('div', { class: 'field', style: 'margin-top:14px', text: 'Legal hold' }),
      el('div', { style: 'display:flex;gap:8px;align-items:center' },
        el('button', { class: 'btn', text: 'Hold ON\u2026', onclick: launch('Legal hold ON', (key) => api.SetObjectLegalHold(bucket, key, '', true)) }),
        el('button', { class: 'btn', text: 'Hold OFF\u2026', onclick: launch('Legal hold OFF', (key) => api.SetObjectLegalHold(bucket, key, '', false)) }),
      ),
      el('div', { class: 'field', style: 'margin-top:14px;color:var(--text-dim)', text: 'COMPLIANCE retention cannot be shortened or removed. GOVERNANCE can. Legal hold keeps every version until turned off.' }),
    ),
    buttons: [{ label: 'Close' }],
  });
}

function lockDialogOne(bucket, row, onChanged) {
  const key = row.key;
  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)' });
  const mode = el('select', { class: 'input', style: 'width:auto' },
    ['GOVERNANCE', 'COMPLIANCE'].map((m) => el('option', { value: m }, m)));
  const until = el('input', { class: 'input mono', value: '+30d', spellcheck: 'false' });
  const holdState = el('div', { class: 'v', text: '…' });

  const act = async (fn, msg) => {
    status.style.color = 'var(--text-dim)';
    status.textContent = msg ? `${msg}…` : 'Working…';
    try {
      await fn();
      status.textContent = msg || 'Done';
      if (msg) toast(msg, 'ok');
      onChanged?.();
      draw();
    } catch (err) {
      status.textContent = String(err);
      status.style.color = 'var(--danger)';
    }
  };

  async function draw() {
    let lock;
    try {
      lock = await api.GetObjectLock(bucket, key, '');
    } catch (err) {
      status.textContent = String(err);
      status.style.color = 'var(--danger)';
      return;
    }
    holdState.textContent = lock.legalHold || 'off (never configured)';
    status.textContent = lock.mode
      ? `Retention: ${lock.mode} until ${lock.retainUntil ? fmtDate(lock.retainUntil) : '?'}`
      : 'No retention configured.';
  }

  openModal({
    title: `Object lock — s3://${bucket}/${key}`,
    body: el('div', {},
      status,
      el('div', { class: 'field', style: 'margin-top:10px', text: 'Retention (applies to the current version)' }),
      el('div', { style: 'display:flex;gap:8px;align-items:center;flex-wrap:wrap' },
        mode, until,
        el('button', { class: 'btn primary', text: 'Set', onclick: () => act(() => api.PutObjectRetention(bucket, key, '', mode.value, until.value.trim()), 'Retention set') }),
        el('button', { class: 'btn', text: 'Clear', title: 'Removes GOVERNANCE retention (COMPLIANCE cannot be removed)', onclick: () => act(() => api.ClearObjectRetention(bucket, key, ''), 'Retention cleared') }),
      ),
      el('div', { class: 'field', style: 'margin-top:14px', text: 'Legal hold' }),
      el('div', { style: 'display:flex;gap:8px;align-items:center' },
        el('span', { class: 'kv' }, el('div', { class: 'k', text: 'State' }), holdState),
        el('button', { class: 'btn', text: 'On', onclick: () => act(() => api.SetObjectLegalHold(bucket, key, '', true), 'Legal hold ON') }),
        el('button', { class: 'btn', text: 'Off', onclick: () => act(() => api.SetObjectLegalHold(bucket, key, '', false), 'Legal hold OFF') }),
      ),
      el('div', { class: 'field', style: 'margin-top:14px;color:var(--text-dim)', text: 'COMPLIANCE retention cannot be shortened or removed. GOVERNANCE can. Legal hold keeps every version until turned off.' }),
    ),
    buttons: [{ label: 'Close' }],
  });
  draw();
}

// ---------- toasts ----------
export function toast(message, type = '') {
  const box = document.getElementById('toasts');
  const t = el('div', { class: `toast ${type}`, text: message });
  box.appendChild(t);
  setTimeout(() => t.remove(), type === 'error' ? 7000 : 3500);
}

// ---------- browse-style directory picker (any source) ----------
// A connected mini-browser: lists the folders of one directory (or the
// buckets of an S3 source), Up/OK navigation, optional new-folder. Works
// against a SAVED source (kind:'s3'/'remote' + source id) or an unsaved
// editor draft (draft Source object, remote engines only).
// Resolves: remote → '/path/' string; s3 → {bucket, prefix}.
export function browseDirDialog({ title, kind, source = '', name = '', draft = null, start = '/', startBucket = '', startPrefix = '' }) {
  return new Promise((resolve) => {
    let cur = kind === 's3'
      ? { bucket: startBucket || '', prefix: startPrefix || '' }
      : { dir: start || '/' };
    const labelOf = () => kind === 's3'
      ? `${name || source}://${cur.bucket}${cur.bucket ? '/' + cur.prefix : ''}`
      : `${name || source}://${cur.dir || '/'}`;
    const crumb = el('div', { class: 'bd-crumb mono', title: '' });
    const list = el('div', { class: 'bd-list' });
    const status = el('div', { class: 'bd-status' });

    const listS3Dirs = (src, bucket, prefix) => new Promise((res, rej) => {
      const dirs = [];
      let settled = false;
      const finish = (fn, v) => { if (settled) return; settled = true; stream.off(); fn(v); };
      const stream = subscribeStream(
        () => api.ListSourceObjectsStream(src, bucket, prefix),
        (p) => {
          if (p.error) { finish(rej, new Error(p.error)); return; }
          for (const e of p.entries || []) if (e.isDir) dirs.push(e);
          if (p.done) finish(res, dirs);
        },
      );
      stream.begin.then(() => stream.flush(), (e) => finish(rej, e));
    });

    async function load() {
      crumb.textContent = labelOf();
      crumb.title = labelOf();
      list.replaceChildren(el('div', { class: 'bd-row dim', text: 'Loading…' }));
      try {
        let rows = [];
        if (kind === 's3') {
          if (!cur.bucket) {
            const buckets = await api.ListSourceBuckets(source);
            rows = buckets.map((b) => ({ name: b.name, isBucket: true }));
          } else {
            rows = await listS3Dirs(source, cur.bucket, cur.prefix);
          }
        } else {
          const entries = draft
            ? await api.RemoteListDraft(draft, cur.dir)
            : await api.RemoteList(source, cur.dir);
          rows = entries.filter((e) => e.isDir);
        }
        rows.sort((a, b) => (a.name.toLowerCase() < b.name.toLowerCase() ? -1 : 1));
        draw(rows);
      } catch (e) {
        list.replaceChildren(el('div', { class: 'bd-row dim', text: String(e) }));
      }
    }

    function draw(rows) {
      list.replaceChildren();
      if (!rows.length) list.appendChild(el('div', { class: 'bd-row dim', text: '(no folders)' }));
      for (const r of rows) {
        list.appendChild(el('div', {
          class: 'bd-row', text: `${r.isBucket ? '\u{1F5C0} ' : '\u{1F4C1} '}${r.name}`,
          title: r.name,
          onclick: () => {
            if (kind === 's3') {
              if (!cur.bucket) cur = { bucket: r.name, prefix: '' };
              else cur.prefix = r.key || `${r.name}/`;
            } else {
              cur.dir = r.key || r.path;
            }
            load();
          },
        }));
      }
    }

    const up = async () => {
      if (kind === 's3') {
        if (!cur.prefix) { if (cur.bucket) cur = { bucket: '', prefix: '' }; }
        else {
          const p = cur.prefix.replace(/\/+$/, '');
          const i = p.lastIndexOf('/');
          cur.prefix = i <= 0 ? '' : p.slice(0, i + 1);
        }
      } else {
        const p = (cur.dir || '/').replace(/\/+$/, '');
        if (p === '' || p === '/') return;
        const i = p.lastIndexOf('/');
        cur.dir = i <= 0 ? '/' : p.slice(0, i + 1);
      }
      await load();
    };

    const newFolder = async () => {
      const nm = await prompt({ title: 'New folder', label: 'Folder name' });
      if (!nm) return;
      try {
        if (kind === 's3') {
          if (!cur.bucket) { status.textContent = 'Open a bucket first.'; return; }
          await api.SourceCreateFolder(source, cur.bucket, cur.prefix, nm);
        } else if (draft) {
          status.textContent = 'Save the source first, then create folders.';
          return;
        } else {
          await api.RemoteMkdir(source, `${cur.dir.replace(/\/+$/, '')}/${nm}`.replace(/\/{2,}/g, '/'));
        }
        await load();
      } catch (e) {
        status.textContent = String(e);
      }
    };

    openModal({
      title,
      body: el('div', { class: 'bd' },
        el('div', { style: 'display:flex;gap:8px;align-items:center;margin-bottom:8px' },
          el('button', { class: 'btn', text: '\u2191 Up', onclick: () => up() }),
          el('button', { class: 'btn', text: '\u{1F4C1}+ New folder', title: 'Create a folder here', onclick: () => newFolder() }),
          crumb,
        ),
        list,
        status,
      ),
      buttons: [
        { label: 'Cancel', onclick: (c) => { c(); resolve(null); } },
        {
          label: 'OK', class: 'primary',
          onclick: (c) => {
            c();
            resolve(kind === 's3' ? { ...cur } : cur.dir);
          },
        },
      ],
      onClose: () => resolve(null),
      wide: true,
    });
    load();
  });
}

// ---------- Import S3 Credential (files + KMS services) ----------
// KMS_PARAM_DEFS mirrors the Go-side KmsFetch params per service.
const KMS_PARAM_DEFS = {
  vault: [
    ['url', 'Vault URL', 'https://vault.example.com'],
    ['token', 'Token', ''],
    ['path', 'Secret path (KV v2)', 'secret/my-app'],
    ['mount', 'Mount (default secret)', 'secret'],
    ['version', 'Version (optional)', ''],
    ['namespace', 'Namespace (optional)', ''],
  ],
  awssm: [
    ['region', 'Region', 'us-east-1'],
    ['accessKey', 'Access key ID', ''],
    ['secretKey', 'Secret access key', ''],
    ['secretId', 'Secret name (or full ARN)', ''],
  ],
  azure: [
    ['tenantId', 'Tenant ID', ''],
    ['clientId', 'Client ID', ''],
    ['clientSecret', 'Client secret', ''],
    ['vaultUrl', 'Key vault (name or URL)', ''],
    ['secretName', 'Secret name', ''],
  ],
  gcp: [
    ['project', 'Project ID', ''],
    ['secretName', 'Secret name', ''],
    ['version', 'Version (default latest)', ''],
    ['credentialsJson', 'Service-account JSON (full file contents)', ''],
  ],
  http: [
    ['url', 'URL', ''],
    ['method', 'Method (GET or POST)', 'GET'],
    ['token', 'Bearer token (optional)', ''],
    ['jsonPath', 'JSON path to credentials (optional, e.g. data.s3)', ''],
    ['body', 'POST body (optional)', ''],
    ['headerName1', 'Header 1 name', ''],
    ['headerValue1', 'Header 1 value', ''],
    ['headerName2', 'Header 2 name', ''],
    ['headerValue2', 'Header 2 value', ''],
  ],
};

const KMS_SERVICES = [
  ['vault', 'HashiCorp Vault'],
  ['awssm', 'AWS Secrets Manager'],
  ['azure', 'Azure Key Vault'],
  ['gcp', 'GCP Secret Manager'],
  ['http', 'Custom HTTP endpoint'],
];

// kmsFetchDialog collects one service's params and fetches its secrets.
// onDone fires when this dialog closes (any path) so a caller whose modal
// it replaced can re-show itself.
function kmsFetchDialog(onCandidates, onDone) {
  const service = el('select', { class: 'input' }, KMS_SERVICES.map(([v, l]) => el('option', { value: v }, l)));
  const holder = el('div', { style: 'margin-top:4px' });
  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)' });
  const inputs = {};
  const drawFields = () => {
    const def = KMS_PARAM_DEFS[service.value] || [];
    holder.replaceChildren(...def.flatMap(([key, label, ph]) => {
      const inp = inputs[key] || (inputs[key] = el('input', {
        class: `input${/key|token|secret/i.test(key) ? ' mono' : ''}`,
        placeholder: ph, spellcheck: 'false', autocomplete: 'off',
      }));
      return [el('label', { class: 'field', text: label }), inp];
    }));
  };
  service.onchange = drawFields;
  drawFields();
  openModal({
    title: 'Import from a secrets service',
    body: el('div', {},
      el('label', { class: 'field', text: 'Service' }), service,
      holder,
      el('div', { class: 'field', style: 'color:var(--text-dim)', text: 'The secret must contain recognizable connection fields (endpoint/access key/secret, or host/user/password) in JSON or key=value form.' }),
      status,
    ),
    buttons: [
      { label: 'Cancel' },
      {
        label: 'Fetch', class: 'primary',
        onclick: async () => {
          const params = {};
          for (const def of KMS_PARAM_DEFS[service.value] || []) {
            const v = (inputs[def[0]]?.value || '').trim();
            if (v) params[def[0]] = v;
          }
          status.textContent = 'Fetching…';
          status.style.color = 'var(--text-dim)';
          try {
            const cs = await api.KmsFetch(service.value, params);
            if (!cs.length) {
              status.textContent = 'Fetched, but no recognizable connection was found in the secret.';
              status.style.color = 'var(--danger)';
              return;
            }
            document.querySelector('#modal-root .modal-head .x')?.click();
            onCandidates(cs);
          } catch (e) {
            status.textContent = String(e);
            status.style.color = 'var(--danger)';
          }
        },
      },
    ],
    onClose: () => onDone?.(),
  });
}

// importCredsDialog (the "Import S3 Credential" dialog): pick credential files or fetch from a KMS service,
// review + test the parsed candidates (bucket counts), then import them
// as data sources. Secrets never leave the Go side — the dialog only sees
// metadata.
export function importCredsDialog(onImported) {
  let candidates = [];
  const list = el('div', { class: 'cred-list' });
  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)' });

  const add = (cs) => {
    candidates = cs.map((c) => ({ ...c, checked: true, tested: null }));
    draw();
  };

  const draw = () => {
    list.replaceChildren();
    if (!candidates.length) {
      list.appendChild(el('div', { class: 'cred-empty', text: 'No candidates yet — pick a credential file or fetch from a secrets service.' }));
      return;
    }
    for (const c of candidates) {
      const testBtn = el('button', {
        class: 'btn', text: 'Test', title: 'Connect with this candidate (does not save it)',
        onclick: async () => {
          c.tested = { pending: true };
          draw();
          try {
            const r = await api.TestCredentialDraft(c.id);
            c.tested = r;
          } catch (e) {
            c.tested = { ok: false, message: String(e) };
          }
          draw();
        },
      });
      const where = c.endpoint || c.host || '';
      list.appendChild(el('div', { class: 'cred-row' },
        (() => {
          const cb = el('input', { type: 'checkbox', title: 'Import this candidate' });
          cb.checked = !!c.checked;
          cb.addEventListener('change', () => { c.checked = cb.checked; });
          return cb;
        })(),
        el('div', { class: 'cred-main' },
          el('div', { class: 'cred-name' },
            el('span', { class: 'cred-badge', text: c.type }),
            el('span', { text: c.name || '(unnamed)' }),
          ),
          el('div', { class: 'cred-sub', text: `${where || 'no endpoint'}${c.origin ? ` — ${c.origin}` : ''}${c.hasSecret ? ' • secret kept' : ''}` }),
        ),
        c.tested && !c.tested.pending
          ? el('div', { class: `cred-test ${c.tested.ok ? 'ok' : 'err'}`, text: c.tested.ok ? `\u2705 ${c.tested.message}` : `\u274C ${c.tested.message}` })
          : c.tested?.pending ? el('div', { class: 'cred-test', text: 'Testing…' }) : null,
        testBtn,
      ));
    }
  };
  draw();

  const fromFiles = async () => {
    try {
      const files = await api.PickCredentialFiles();
      if (!files || !files.length) return;
      for (const p of files) {
        let cs = null;
        try {
          cs = await api.ParseCredentialFile(p, '');
        } catch (e) {
          const msg = String(e);
          // encrypted profile containers need their password
          if (/password|decrypt/i.test(msg)) {
            const pw = await prompt({ title: 'Profile password', label: `Password for ${p.split(/[\\/]/).pop()}`, password: true });
            if (!pw) continue;
            cs = await api.ParseCredentialFile(p, pw);
          } else throw e;
        }
        if (cs?.length) add(cs);
        else status.textContent = `${p.split(/[\\/]/).pop()}: no importable connection found`;
      }
    } catch (e) {
      status.textContent = String(e);
      status.style.color = 'var(--danger)';
    }
  };

  // show (re)opens the dialog. Nested modals (the profile password prompt,
  // the KMS fetch dialog) replace this one in the modal root — the candidate
  // state lives here, so it re-shows itself when they close.
  const show = () => openModal({
    title: 'Import S3 Credential',
    body: el('div', {},
      el('div', { class: 'field', style: 'color:var(--text-dim)', text: 'Turn connection credentials into data sources. Files: AWS CLI INI, rclone.conf, JSON (any shape), .env, or an exported .s3bprofile. Services: Vault, AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, or any custom HTTP endpoint.' }),
      list,
      status,
    ),
    wide: true,
    buttons: [
      { label: 'From file\u2026', onclick: async () => { await fromFiles(); show(); } },
      { label: 'From service\u2026', onclick: () => kmsFetchDialog(add, show) },
      {
        label: 'Import', class: 'primary',
        onclick: async (close) => {
          const sel = candidates.filter((c) => c.checked).map((c) => c.id);
          if (!sel.length) { status.textContent = 'Nothing selected.'; return; }
          try {
            const res = await api.ImportCredentials(sel);
            const upd = res.updated?.length ? `, ${res.updated.length} updated` : '';
            toast(`Imported ${res.imported?.length || 0} source(s)${upd}${res.skipped?.length ? `, ${res.skipped.length} skipped` : ''}`,
              (res.imported?.length || res.updated?.length) ? 'ok' : 'error');
            close();
            onImported?.(res);
          } catch (e) {
            status.textContent = String(e);
            status.style.color = 'var(--danger)';
          }
        },
      },
      { label: 'Close' },
    ],
  });
  show();
}
