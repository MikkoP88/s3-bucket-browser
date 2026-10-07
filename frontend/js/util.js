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

// fileIcon picks a type glyph from the name's extension — the closer the
// metaphor, the faster a results list reads. One map shared by the grid,
// the markers column and the Search window, so a type always looks like
// itself everywhere.
export function fileIcon(name, isDir) {
  if (isDir) return '\u{1F4C1}';
  const ext = (name.split('.').pop() || '').toLowerCase();
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico', 'tif', 'tiff', 'heic', 'avif'].includes(ext)) return '\u{1F5BC}'; // image
  if (['mp4', 'mkv', 'mov', 'avi', 'webm', 'flv', 'm4v', 'mpg', 'mpeg', 'wmv'].includes(ext)) return '\u{1F3AC}';               // video
  if (['mp3', 'wav', 'flac', 'ogg', 'm4a', 'aac', 'opus', 'wma', 'mid', 'midi'].includes(ext)) return '\u{1F3B5}';               // audio
  if (['zip', 'tar', 'gz', 'bz2', 'xz', '7z', 'rar', 'zst', 'tgz', 'iso', 'img'].includes(ext)) return '\u{1F4E6}';              // archive
  if (ext === 'pdf') return '\u{1F4D1}';                                                                                        // pdf
  if (['doc', 'docx', 'odt', 'rtf', 'pages', 'epub', 'mobi', 'azw3'].includes(ext)) return '\u{1F4D8}';                         // document
  if (['xls', 'xlsx', 'ods', 'csv', 'tsv'].includes(ext)) return '\u{1F4CA}';                                                   // spreadsheet
  if (['ppt', 'pptx', 'odp'].includes(ext)) return '\u{1F4C8}';                                                                 // presentation
  if (['js', 'ts', 'mjs', 'cjs', 'jsx', 'tsx', 'py', 'go', 'rs', 'c', 'h', 'cpp', 'hpp', 'java', 'rb', 'php', 'cs', 'swift', 'kt', 'sql', 'html', 'htm', 'css', 'scss', 'vue', 'svelte'].includes(ext)) return '\u{1F4DC}'; // code
  if (['txt', 'md', 'log', 'json', 'xml', 'yaml', 'yml', 'toml', 'ini', 'conf', 'cfg', 'nfo'].includes(ext)) return '\u{1F4DD}'; // text
  if (['exe', 'msi', 'msix', 'app', 'dmg', 'deb', 'rpm', 'bat', 'cmd', 'sh', 'ps1'].includes(ext)) return '\u{2699}';           // executable
  return '\u{1F4C4}';                                                                                                           // generic file
}

// Source-type badges: bold text names the type — S3, SFTP, WebDAV,
// Local, … — everywhere a source's identity shows, replacing the old
// icon set. The casing matches the Add-source dialog's type list so a
// type reads the same everywhere it is named; an unknown type falls
// back to a plain badge rather than a wrong metaphor.
const SRC_TYPE_LABEL = {
  s3: 'S3',
  sftp: 'SFTP',
  scp: 'SCP',
  ftp: 'FTP',
  ftps: 'FTPS',
  webdav: 'WebDAV',
  webdavs: 'WebDAVS',
  local: 'Local',
};

function srcIcon(stype) {
  return SRC_TYPE_LABEL[stype] || 'Other';
}

// slashPath joins a source name to its /-anchored contents for the
// normalized Name/contents display without doubling the slash: remote
// paths arrive as '/incoming/', so Name + that must read Name/incoming/;
// the root alone ('/') reads Name/
export function slashPath(name, path) {
  return `${name}/${String(path || '/').replace(/^\/+/, '')}`;
}

// srcIconEl wraps the type label in a bold span: an explicit color
// paints the source's accent while the inherited text color keeps the
// theme default. Every surface that shows a source's identity (sidebar
// rows, the breadcrumb's root crumb, pickers) builds on this one
// helper, so the badge is identical everywhere.
export function srcIconEl(stype, color) {
  const s = el('span', { class: 'src-ic', text: srcIcon(stype) });
  if (color) s.style.color = color;
  return s;
}

// typedSourceLabel is the badge's text twin: a native <option> cannot
// hold a styled span, so the places that list sources as plain text —
// the Search window's Sources dropdown, the pane's source picker —
// speak the same "S3 · name" identity the .src-ic chip paints elsewhere.
export function typedSourceLabel(stype, label) {
  return label ? `${srcIcon(stype)} · ${label}` : label;
}
