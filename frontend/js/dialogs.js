// Modal framework + every dialog: confirmations (L1/L2 ladder), prompts,
// properties, doctor, profile editor, transfer manager, help sheet.
import { api, onEvent } from './api.js';
import { el, fmtBytes, fmtSpeed, fmtDate, parseSizeStr, parseDurStr, parentPrefix } from './util.js';
import { t } from './i18n.js';

const root = () => document.getElementById('modal-root');

export function openModal({ title, body, buttons = [], wide = false, onClose }) {
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
    class: `modal${wide ? ' wide' : ''}`,
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

// ---------- confirmations (safety ladder, PLAN.md §9) ----------
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

export function prompt({ title, label, value = '', okLabel = 'OK' }) {
  let settled = false;
  return new Promise((resolve) => {
    const done = (v) => { if (!settled) { settled = true; resolve(v); } };
    const input = el('input', { class: 'input', value, spellcheck: 'false' });
    const submit = (close) => { done(input.value.trim() || null); close(); };
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
export function properties(title, rows) {
  const body = el('div', { class: 'kv' });
  for (const [k, v] of rows) {
    body.appendChild(el('div', { class: 'k', text: k }));
    body.appendChild(el('div', { class: 'v mono', text: String(v ?? '') }));
  }
  openModal({ title, body, buttons: [{ label: 'Close' }] });
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

  // detail body for one check: advice, error, info (each hidden if absent)
  const detailOf = (c) => el('div', { class: 'doc-detail' },
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
  const { close, body } = openModal({
    title: 'Transfers',
    body: list,
    wide: true,
    buttons: [{ label: 'Clear finished', onclick: async (c) => { await api.ClearFinishedTransfers(); draw(); } }, { label: 'Close', onclick: (c) => { c(); onClose?.(); } }],
  });

  async function draw() {
    const jobs = await api.ActiveTransfers();
    list.replaceChildren(...jobs.map(renderJob));
    if (!jobs.length) list.appendChild(el('div', { text: 'No transfers.', style: 'color:var(--text-dim)' }));
  }

  function renderJob(j) {
    const pct = j.totalBytes > 0 ? Math.min(100, (j.sentBytes / j.totalBytes) * 100) : 0;
    const bar = el('div', { class: 'tr-bar' }, el('div', { style: `width:${pct}%` }));
    const job = el('div', { class: `tr-job ${j.status}` },
      el('div', { class: 'tr-top' },
        el('span', { class: 'tr-name', text: `${j.op === 'upload' ? '\u2191' : '\u2193'} ${j.currentFile || j.id}` }),
        el('span', { class: 'tr-status', text: `${j.status} — ${j.doneFiles}/${j.totalFiles} files, ${fmtBytes(j.sentBytes)}${j.totalBytes ? ` / ${fmtBytes(j.totalBytes)}` : ''}${j.speedBps ? ` @ ${fmtSpeed(j.speedBps)}` : ''}` }),
        j.status === 'running' ? el('button', { class: 'btn', text: 'Cancel', onclick: async () => { await api.CancelTransfer(j.id); } }) : null,
      ),
      bar,
      j.error ? el('div', { class: 'tr-sub', text: j.error }) : null,
    );
    job.dataset.id = j.id;
    return job;
  }

  draw();
  const off = window.runtime?.EventsOn('transfer:update', () => draw());
  const origClose = close;
  return { close: () => { origClose(); } };
}

// ---------- profile editor ----------
const PROFILE_COLORS = ['#0b63ce', '#1b7f3b', '#b3261e', '#9a6700', '#7c3aed', '#0e7490', '#be185d', '#57606a'];

export function profileEditor(existing, onSaved) {
  const f = {
    name: el('input', { class: 'input', value: existing?.name || '', spellcheck: 'false' }),
    endpoint: el('input', { class: 'input mono', value: existing?.endpoint || '', placeholder: 'https://s3.amazonaws.com (empty = AWS)' }),
    region: el('input', { class: 'input', value: existing?.region || '', placeholder: 'us-east-1' }),
    accessKey: el('input', { class: 'input mono', value: existing?.accessKeyId || '', autocomplete: 'off' }),
    secretKey: el('input', { class: 'input mono', type: 'password', placeholder: existing?.secretMasked || '', autocomplete: 'new-password' }),
    token: el('input', { class: 'input mono', type: 'password', placeholder: 'optional (temporary credentials)', autocomplete: 'new-password' }),
    pathStyle: el('input', { type: 'checkbox' }),
    insecure: el('input', { type: 'checkbox' }),
    setDefault: el('input', { type: 'checkbox', checked: true }),
  };
  f.pathStyle.checked = !!existing?.pathStyle;
  f.insecure.checked = !!existing?.insecure;

  let color = existing?.color || PROFILE_COLORS[0];
  const chips = el('div', { class: 'chips' }, PROFILE_COLORS.map((c) =>
    el('div', { class: `chip${c === color ? ' sel' : ''}`, style: `background:${c}`, onclick: (e) => {
      color = c;
      chips.querySelectorAll('.chip').forEach((n) => n.classList.remove('sel'));
      e.target.classList.add('sel');
    } }),
  ));

  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)' });
  const body = el('div', {},
    el('label', { class: 'field', text: 'Profile name' }), f.name,
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
      el('label', { style: 'display:flex;align-items:center;gap:6px' }, f.setDefault, 'Default'),
    ),
    el('label', { class: 'field', text: 'Accent color' }), chips,
    status,
  );

  openModal({
    title: existing ? `Edit profile — ${existing.name}` : 'Add profile',
    body,
    buttons: [
      {
        label: 'Test',
        onclick: async () => {
          status.textContent = 'Testing…';
          try {
            const res = await api.TestProfile(f.name.value.trim());
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
          try {
            await api.SaveProfile({
              name: f.name.value.trim(),
              endpoint: f.endpoint.value.trim(),
              region: f.region.value.trim(),
              accessKeyId: f.accessKey.value.trim(),
              secretKey: f.secretKey.value,
              sessionToken: f.token.value,
              pathStyle: f.pathStyle.checked,
              insecure: f.insecure.checked,
              color,
              setDefault: f.setDefault.checked,
            });
            close();
            onSaved?.();
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
    ['Del', 'Delete selection'],
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
    ['Esc', 'Clear selection / close'],
    ['F1', 'This sheet'],
  ];
  const body = el('div', { class: 'help-grid' },
    rows.map(([k, v]) => el('div', { class: 'row' }, el('kbd', { text: k }), el('span', { text: v }))),
  );
  openModal({ title: 'Keyboard shortcuts', body, buttons: [{ label: 'Close' }] });
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

// conflictPolicy resolves null (canceled) or { policy, maxBps }. The speed
// limit choice is remembered across transfers (localStorage s3b-throttle).
export function conflictPolicy(kind, target) {
  let settled = false;
  return new Promise((resolve) => {
    const done = (v) => { if (!settled) { settled = true; resolve(v); } };
    const options = [
      ['overwrite', 'Overwrite', 'Replace existing files'],
      ['skip', 'Skip', 'Keep existing files'],
      ['rename', 'Rename', 'Keep both — new files get " (1)" suffix'],
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
    const rate = el('select', { class: 'input', style: 'width:auto' },
      RATE_LIMITS.map(([v, label]) => el('option', { value: String(v) }, label)));
    rate.value = localStorage.getItem('s3b-throttle') || '0';
    openModal({
      title: `${kind === 'upload' ? 'Upload' : 'Download'} — conflicting files at ${target}`,
      body: el('div', {},
        list,
        el('label', { class: 'field', style: 'display:flex;align-items:center;gap:8px;margin-top:10px' },
          'Speed limit:', rate)),
      buttons: [
        { label: 'Cancel', onclick: (c) => { done(null); c(); } },
        {
          label: 'Start',
          class: 'primary',
          onclick: (c) => {
            localStorage.setItem('s3b-throttle', rate.value);
            done({ policy: choice, maxBps: parseInt(rate.value, 10) || 0 });
            c();
          },
        },
      ],
      onClose: () => done(null),
    });
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

// ---------- object versions (M4) ----------
const asMillis = (v) => (typeof v === 'string' ? Date.parse(v) : v);

export function versionsDialog(bucket, key, onChanged) {
  const list = el('div', { class: 'ver-list' });
  const status = el('div', { class: 'field', style: 'min-height:18px;color:var(--text-dim)' });

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
    list.replaceChildren(...vers.map((v) => el('div', { class: `ver-row${v.isLatest ? ' latest' : ''}` },
      el('span', { class: 'ver-icon', text: v.isDeleteMarker ? '\u26D4' : (v.isLatest ? '\u25CF' : '\u25CB') }),
      el('span', { class: 'ver-main' },
        el('div', { text: v.isDeleteMarker ? 'Delete marker (object hidden)' : `${fmtBytes(v.size)} — ${v.storageClass || 'STANDARD'}${v.etag ? ` — ${v.etag}` : ''}` }),
        el('div', { class: 'ver-sub', text: `${v.lastModified ? fmtDate(asMillis(v.lastModified)) : ''}${v.versionId ? ` — ${v.versionId}` : ''}` }),
      ),
      el('span', { class: 'ver-actions' },
        v.isLatest && !v.isDeleteMarker ? el('span', { class: 'tag', text: 'current' }) : null,
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
    body: el('div', {}, status, list),
    wide: true,
    buttons: [{ label: 'Close' }],
  });
  draw();
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
            el('div', { class: 'k', text: 'Region' }), el('div', { class: 'v mono', text: panel.region || '—' }),
            el('div', { class: 'k', text: 'Versioning' }), el('div', { class: 'v', text: panel.versions || 'off (never configured)' }),
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
          ...(s.grants || []).length ? el('div', { class: 'field', style: 'margin-top:8px', text: `Grants: ${(s.grants || []).join(', ')}` }) : null,
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
    title: `Bucket administration — ${bucket}`,
    body: el('div', { class: 'admin' }, strip, content),
    wide: true,
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
          const myToken = await api.DeepSearch(bucket, prefix, opts);
          if (!running) { api.CancelSearch(myToken); return; } // closed meanwhile
          token = myToken;
          offPage = onEvent('search:page', (p) => {
            if (p.token !== token) return;
            for (const r of p.entries || []) list.appendChild(fmtRes(r));
            status.textContent = t('findRunning', { matched: p.matched });
            list.scrollTop = list.scrollHeight;
          });
          offDone = onEvent('search:done', (d) => {
            if (d.token !== token) return;
            token = null;
            running = false;
            status.textContent = d.error
              ? d.error
              : t('findDone', { matched: d.matched, scanned: d.scanned, bucket, prefix: prefix || '' });
            if (d.error) status.style.color = 'var(--danger)';
          });
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

// ---------- object lock per object (M5) ----------
export function lockDialog(bucket, key, onChanged) {
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
