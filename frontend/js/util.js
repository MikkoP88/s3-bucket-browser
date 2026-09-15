// Small DOM + formatting helpers.
export function el(tag, attrs = {}, ...children) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') n.className = v;
    else if (k === 'text') n.textContent = v;
    else if (k.startsWith('on') && typeof v === 'function') n.addEventListener(k.slice(2), v);
    else if (v !== null && v !== undefined && v !== false) n.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    n.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
  }
  return n;
}

// multiSel builds a compact multi-select control: a button summarizing
// the current selection (allLabel when nothing is picked) over a popover
// of checkboxes with an "All" toggle on top. Options are seeded up front
// and can grow via add(v) as new values stream in. The live selection is
// exposed as .sel (a Set — an EMPTY set means "everything", so a filter
// pass is sel.size === 0 || sel.has(v)). Used by the log drawer filters
// and the Settings file-log filters.
export function multiSel(allLabel, values = [], selected = [], onChange) {
  const sel = new Set(selected);
  const checks = new Map();
  const btn = el('button', { class: 'ms-btn', type: 'button' });
  const allChk = el('input', { type: 'checkbox', checked: sel.size === 0 });
  const pop = el('div', { class: 'ms-pop hidden' },
    el('label', { class: 'ms-opt' }, allChk, ` ${allLabel}`));
  const wrap = el('div', { class: 'ms' }, btn, pop);

  const sync = () => {
    allChk.checked = sel.size === 0;
    for (const [v, chk] of checks) chk.checked = sel.has(v);
    btn.textContent = sel.size ? [...sel].join(', ') : allLabel;
    onChange?.();
  };
  btn.addEventListener('click', (e) => { e.stopPropagation(); pop.classList.toggle('hidden'); });
  document.addEventListener('click', (e) => { if (!wrap.contains(e.target)) pop.classList.add('hidden'); });
  allChk.addEventListener('change', () => { if (allChk.checked) sel.clear(); sync(); });

  const add = (v) => {
    if (!v || checks.has(v)) return;
    const chk = el('input', { type: 'checkbox', checked: sel.has(v) });
    chk.addEventListener('change', () => {
      if (chk.checked) sel.add(v); else sel.delete(v);
      sync();
    });
    checks.set(v, chk);
    pop.appendChild(el('label', { class: 'ms-opt' }, chk, ` ${v}`));
  };
  for (const v of values) add(v);
  sync();
  return { root: wrap, sel, add };
}

export function fmtBytes(n) {
  if (n === null || n === undefined) return '';
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB', 'PB'];
  let v = n;
  for (const u of units) {
    v /= 1024;
    if (v < 1024) return `${v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${u}`;
  }
  return `${v.toFixed(1)} EB`;
}

export function fmtSpeed(bps) {
  if (!bps || bps <= 0) return '';
  return `${fmtBytes(bps)}/s`;
}

export function fmtDate(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (x) => String(x).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function debounce(fn, ms) {
  let t;
  return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
}

// parseSizeStr turns "500", "10KB", "1.5MB" into bytes; null when empty.
// Throws on malformed input (caller shows the message).
export function parseSizeStr(s) {
  s = String(s || '').trim();
  if (!s) return null;
  const m = s.toUpperCase().match(/^([\d.]+)\s*(B|KB|MB|GB|TB)?$/);
  if (!m || isNaN(parseFloat(m[1]))) throw new Error(`invalid size "${s}" (use e.g. 10MB)`);
  const mult = { undefined: 1, B: 1, KB: 1024, MB: 1024 ** 2, GB: 1024 ** 3, TB: 1024 ** 4 }[m[2]];
  return Math.round(parseFloat(m[1]) * mult);
}

// parseDurStr turns "30d", "24h", "90m", "10s" into seconds; null when
// empty. A unit is required (mirrors the CLI). Throws on bad input.
export function parseDurStr(s) {
  s = String(s || '').trim();
  if (!s) return null;
  const m = s.toLowerCase().match(/^(\d+)\s*(s|m|h|d)$/);
  if (!m) throw new Error(`invalid duration "${s}" (use e.g. 30d, 24h)`);
  const mult = { s: 1, m: 60, h: 3600, d: 86400 }[m[2]];
  return parseInt(m[1], 10) * mult;
}

// parentPrefix returns the folder prefix containing a key ("a/b/c.txt" ->
// "a/b/"; "x.txt" -> "").
export function parentPrefix(key) {
  const i = key.lastIndexOf('/');
  return i >= 0 ? key.slice(0, i + 1) : '';
}

export function basename(key) {
  const k = key.replace(/\/+$/, '');
  const i = k.lastIndexOf('/');
  return i >= 0 ? k.slice(i + 1) : k;
}

export function fileIcon(name, isDir) {
  if (isDir) return '\u{1F4C1}';
  const ext = (name.split('.').pop() || '').toLowerCase();
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico'].includes(ext)) return '\u{1F5BC}';
  if (['mp4', 'mkv', 'mov', 'avi', 'webm'].includes(ext)) return '\u{1F3AC}';
  if (['mp3', 'wav', 'flac', 'ogg', 'm4a'].includes(ext)) return '\u{1F3B5}';
  if (['zip', 'tar', 'gz', 'bz2', 'xz', '7z', 'rar'].includes(ext)) return '\u{1F4E6}';
  if (['pdf'].includes(ext)) return '\u{1F4D1}';
  if (['txt', 'md', 'log', 'json', 'xml', 'yaml', 'yml', 'csv', 'ini', 'conf'].includes(ext)) return '\u{1F4C4}';
  if (['exe', 'msi', 'bat', 'sh', 'ps1'].includes(ext)) return '\u{2699}';
  return '\u{1F4C5}';
}

// Source-type glyphs for the sidebar: original inline SVGs drawn for this
// project (16×16, stroke-based, currentColor so they follow the theme).
// No external icon set is embedded — nothing to attribute, no license to
// carry. S3 gets the storage-bucket silhouette; sftp/scp the SSH terminal
// prompt; ftp a folder with opposing transfer arrows; ftps the locked
// folder; webdav(s) the globe (the s variant badged with a padlock); local
// a disk drive; anything unknown a server stack.
const svgWrap = (inner) => `<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${inner}</svg>`;
const svgFolder = '<path d="M1.7 5V3.9c0-.66.54-1.2 1.2-1.2h3l1.5 1.6h5.7c.66 0 1.2.54 1.2 1.2V12c0 .66-.54 1.2-1.2 1.2H2.9c-.66 0-1.2-.54-1.2-1.2Z"/>';
const svgLock = '<g><rect x="9.7" y="9.5" width="4.9" height="4.1" rx="0.7"/><path d="M10.9 9.5V8.4a1.25 1.25 0 0 1 2.5 0v1.1"/></g>';
const svgShell = svgWrap('<rect x="2" y="2.7" width="12" height="10.6" rx="1.4"/><path d="m5 6.3 2.3 2-2.3 2.1"/><path d="M8.7 10.7h3"/>');
const SRC_SVG = {
  s3: svgWrap('<path d="M3 4.3 4.35 13c.14 1 1.64 1.8 3.65 1.8s3.51-.8 3.65-1.8L13 4.3"/><ellipse cx="8" cy="4.3" rx="5" ry="1.9"/>'),
  sftp: svgShell,
  scp: svgShell,
  ftp: svgWrap(`${svgFolder}<path d="M9.6 7.1v5.3M8.3 8.4l1.3-1.3 1.3 1.3"/><path d="M12.6 12.4V7.1M11.3 11.1l1.3 1.3 1.3-1.3"/>`),
  ftps: svgWrap(svgFolder + svgLock),
  webdav: svgWrap('<circle cx="8" cy="8" r="5.7"/><path d="M2.3 8h11.4"/><ellipse cx="8" cy="8" rx="2.6" ry="5.7"/>'),
  webdavs: svgWrap('<circle cx="7" cy="7.1" r="4.9"/><path d="M2.1 7.1h9.8"/><ellipse cx="7" cy="7.1" rx="2.2" ry="4.9"/>' + svgLock),
  local: svgWrap('<rect x="2" y="4.6" width="12" height="6.8" rx="1.3"/><path d="M4.2 9.3h4.6"/><circle cx="11.9" cy="9" r="0.9"/>'),
  other: svgWrap('<rect x="2.6" y="2.4" width="10.8" height="4.6" rx="1"/><rect x="2.6" y="9" width="10.8" height="4.6" rx="1"/><circle cx="12" cy="4.7" r="0.55"/><circle cx="12" cy="11.3" r="0.55"/>'),
};

export function srcIcon(stype) {
  return SRC_SVG[stype] || SRC_SVG.other;
}
