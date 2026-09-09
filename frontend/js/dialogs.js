// Modal framework + every dialog: confirmations (L1/L2 ladder), prompts,
// properties, doctor, profile editor, transfer manager, help sheet.
import { api } from './api.js';
import { el, fmtBytes, fmtSpeed, fmtDate } from './util.js';

const root = () => document.getElementById('modal-root');

export function openModal({ title, body, buttons = [], wide = false, onClose }) {
  const r = root();
  r.classList.remove('hidden');

  const close = (result) => {
    r.classList.add('hidden');
    r.replaceChildren();
    document.removeEventListener('keydown', esc, true);
    onClose?.(result);
  };
  const esc = (e) => { if (e.key === 'Escape') close(null); };
  document.addEventListener('keydown', esc, true);

  const foot = el('div', { class: 'modal-foot' },
    buttons.map((b) => el('button', {
      class: `btn ${b.class || ''}`,
      text: b.label,
      onclick: () => b.onclick ? b.onclick(close) : close(null),
    })),
  );

  const box = el('div', { class: `modal${wide ? ' wide' : ''}` },
    el('div', { class: 'modal-head' },
      el('span', { text: title }),
      el('span', { class: 'x', text: '\u00D7', onclick: () => close(null) }),
    ),
    el('div', { class: 'modal-body' }, body),
    foot,
  );
  box.addEventListener('mousedown', (e) => { if (e.target === r) close(null); });
  r.replaceChildren(box);
  return { close, body: box.querySelector('.modal-body') };
}

// ---------- confirmations (safety ladder, PLAN.md §9) ----------
export function confirm({ title, message, okLabel = 'OK', danger = false }) {
  return new Promise((resolve) => {
    openModal({
      title,
      body: el('div', {}, message),
      buttons: [
        { label: 'Cancel', onclick: (close) => { close(); resolve(false); } },
        { label: okLabel, class: danger ? 'danger' : 'primary', onclick: (close) => { close(); resolve(true); } },
      ],
      onClose: () => resolve(false),
    });
  });
}

export function typedConfirm({ title, message, typeWord, okLabel = 'Delete', danger = true }) {
  return new Promise((resolve) => {
    let input;
    let okBtn;
    const body = el('div', {},
      el('div', {}, message),
      el('label', { class: 'field', text: `Type "${typeWord}" to confirm:` }),
      input = el('input', { class: 'input', autocomplete: 'off' }),
    );
    const { close } = openModal({
      title,
      body,
      buttons: [
        { label: 'Cancel', onclick: (c) => { c(); resolve(false); } },
        {
          label: okLabel,
          class: 'danger',
          onclick: (c) => { if (input.value.trim() === typeWord) { c(); resolve(true); } },
        },
      ],
      onClose: () => resolve(false),
    });
    okBtn = null;
    input.addEventListener('input', () => { });
    input.focus();
  });
}

export function prompt({ title, label, value = '', okLabel = 'OK' }) {
  return new Promise((resolve) => {
    const input = el('input', { class: 'input', value, spellcheck: 'false' });
    const submit = (c) => { c(); resolve(input.value.trim() || null); };
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') submit(current); });
    const current = openModal({
      title,
      body: el('div', {}, el('label', { class: 'field', text: label }), input),
      buttons: [
        { label: 'Cancel', onclick: (c) => { c(); resolve(null); } },
        { label: okLabel, class: 'primary', onclick: submit },
      ],
      onClose: () => resolve(null),
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

// ---------- doctor ----------
export function doctorDialog(report) {
  const body = el('div', {});
  body.appendChild(el('div', { class: 'kv', },
    el('div', { class: 'k', text: 'Endpoint' }), el('div', { class: 'v mono', text: report.endpoint }),
    el('div', { class: 'k', text: 'Provider' }), el('div', { class: 'v', text: report.provider }),
    el('div', { class: 'k', text: 'Bucket' }), el('div', { class: 'v mono', text: report.bucket || '—' }),
  ));
  body.appendChild(el('div', { style: 'height:10px' }));
  for (const c of report.checks || []) {
    body.appendChild(el('div', { class: 'doc-check' },
      el('span', { class: `st ${c.status}`, text: c.status }),
      el('span', {}, `${c.check}${c.detail ? ` — ${c.detail}` : ''}${c.error ? ` (${c.error})` : ''}`),
      el('span', { class: 'dt', text: `${c.durationMs} ms` }),
    ));
  }
  for (const w of report.warnings || []) {
    body.appendChild(el('div', { class: 'doc-check' },
      el('span', { class: 'st WARN', text: 'WARN' }), el('span', { text: w })));
  }
  openModal({ title: `Connection doctor — ${report.summary.pass} pass, ${report.summary.warn} warn, ${report.summary.fail} fail`, body, wide: true, buttons: [{ label: 'Close' }] });
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
    ['Ctrl+C / X / V', 'Copy / cut / paste'],
    ['Ctrl+A', 'Select all'],
    ['Ctrl+F', 'Filter'],
    ['F5', 'Refresh'],
    ['Alt+\u2190 / \u2192', 'Back / forward'],
    ['Alt+\u2191, Backspace', 'Go to parent'],
    ['Type letters', 'Jump to item'],
    ['Ctrl+Shift+N', 'New folder'],
    ['Ctrl+U / Ctrl+D', 'Upload files / download selection'],
    ['Esc', 'Clear selection / close'],
    ['F1', 'This sheet'],
  ];
  const body = el('div', { class: 'help-grid' },
    rows.map(([k, v]) => el('div', { class: 'row' }, el('kbd', { text: k }), el('span', { text: v }))),
  );
  openModal({ title: 'Keyboard shortcuts', body, buttons: [{ label: 'Close' }] });
}

// ---------- conflict policy ----------
export function conflictPolicy(kind, target) {
  return new Promise((resolve) => {
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
    openModal({
      title: `${kind === 'upload' ? 'Upload' : 'Download'} — conflicting files at ${target}`,
      body: list,
      buttons: [
        { label: 'Cancel', onclick: (c) => { c(); resolve(null); } },
        { label: 'Start', class: 'primary', onclick: (c) => { c(); resolve(choice); } },
      ],
      onClose: () => resolve(null),
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

// ---------- toasts ----------
export function toast(message, type = '') {
  const box = document.getElementById('toasts');
  const t = el('div', { class: `toast ${type}`, text: message });
  box.appendChild(t);
  setTimeout(() => t.remove(), type === 'error' ? 7000 : 3500);
}
