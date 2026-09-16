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

// Source-type glyphs for the sidebar and the breadcrumb's source root.
// Real-world metaphors from Bootstrap Icons (MIT), embedded as inline SVG
// path data so the app stays dependency-free (attribution in
// scripts/gen-notice.sh and docs/security.md) — one consistent style,
// every glyph the filled 16-grid variant:
//   pc-display (local), hdd-network (FTP/SFTP), bucket (S3),
//   hdd-rack (unknown fallback), globe (WebDAV) and the terminal/lock
//   badges for the secured composites
// Everything renders in currentColor — the theme text color, or the
// source's own accent color where one is set. The secured variants are
// composites: the base glyph shrinks to 75% in the top-left and a small
// badge takes the bottom-right corner. An SVG mask punches the badge
// silhouette (at 62%, for a visible gap) out of the base first, so the
// badge stays readable on any background — light or dark theme, hover,
// selection, or source tint. Mask ids are per-type: the same type twice
// on a page duplicates an identical id, which resolves fine.
const bsWrap = (inner) => `<svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor" aria-hidden="true">${inner}</svg>`;
const comp = (id, base, badge) => bsWrap(
  `<mask id="s3b-bdg-${id}"><rect width="16" height="16" fill="#fff"/><g transform="translate(8 8) scale(0.62)" fill="#000">${badge}</g></mask>`
  + `<g mask="url(#s3b-bdg-${id})">${base}</g>`
  + `<g transform="translate(8 8) scale(0.5)">${badge}</g>`,
);

// Bootstrap Icons (MIT) — https://github.com/twbs/icons
const bsPcDisplay = '<path d="M8 1a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H9a1 1 0 0 1-1-1zm1 13.5a.5.5 0 1 0 1 0 .5.5 0 0 0-1 0m2 0a.5.5 0 1 0 1 0 .5.5 0 0 0-1 0M9.5 1a.5.5 0 0 0 0 1h5a.5.5 0 0 0 0-1zM9 3.5a.5.5 0 0 0 .5.5h5a.5.5 0 0 0 0-1h-5a.5.5 0 0 0-.5.5M1.5 2A1.5 1.5 0 0 0 0 3.5v7A1.5 1.5 0 0 0 1.5 12H6v2h-.5a.5.5 0 0 0 0 1H7v-4H1.5a.5.5 0 0 1-.5-.5v-7a.5.5 0 0 1 .5-.5H7V2z"/>';
const bsHddNetwork = '<path d="M4.5 5a.5.5 0 1 0 0-1 .5.5 0 0 0 0 1M3 4.5a.5.5 0 1 1-1 0 .5.5 0 0 1 1 0"/><path d="M0 4a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v1a2 2 0 0 1-2 2H8.5v3a1.5 1.5 0 0 1 1.5 1.5h5.5a.5.5 0 0 1 0 1H10A1.5 1.5 0 0 1 8.5 14h-1A1.5 1.5 0 0 1 6 12.5H.5a.5.5 0 0 1 0-1H6A1.5 1.5 0 0 1 7.5 10V7H2a2 2 0 0 1-2-2zm1 0v1a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V4a1 1 0 0 0-1-1H2a1 1 0 0 0-1 1m6 7.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5h-1a.5.5 0 0 0-.5.5"/>';
const bsTerminal = '<path d="M0 3a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H2a2 2 0 0 1-2-2zm9.5 5.5h-3a.5.5 0 0 0 0 1h3a.5.5 0 0 0 0-1m-6.354-.354a.5.5 0 1 0 .708.708l2-2a.5.5 0 0 0 0-.708l-2-2a.5.5 0 1 0-.708.708L4.793 6.5z"/>';
const bsLock = '<path fill-rule="evenodd" d="M8 0a4 4 0 0 1 4 4v2.05a2.5 2.5 0 0 1 2 2.45v5a2.5 2.5 0 0 1-2.5 2.5h-7A2.5 2.5 0 0 1 2 13.5v-5a2.5 2.5 0 0 1 2-2.45V4a4 4 0 0 1 4-4m0 1a3 3 0 0 0-3 3v2h6V4a3 3 0 0 0-3-3"/>';
const bsGlobe = '<path d="M0 8a8 8 0 1 1 16 0A8 8 0 0 1 0 8m7.5-6.923c-.67.204-1.335.82-1.887 1.855A8 8 0 0 0 5.145 4H7.5zM4.09 4a9.3 9.3 0 0 1 .64-1.539 7 7 0 0 1 .597-.933A7.03 7.03 0 0 0 2.255 4zm-.582 3.5c.03-.877.138-1.718.312-2.5H1.674a7 7 0 0 0-.656 2.5zM4.847 5a12.5 12.5 0 0 0-.338 2.5H7.5V5zM8.5 5v2.5h2.99a12.5 12.5 0 0 0-.337-2.5zM4.51 8.5a12.5 12.5 0 0 0 .337 2.5H7.5V8.5zm3.99 0V11h2.653c.187-.765.306-1.608.338-2.5zM5.145 12q.208.58.468 1.068c.552 1.035 1.218 1.65 1.887 1.855V12zm.182 2.472a7 7 0 0 1-.597-.933A9.3 9.3 0 0 1 4.09 12H2.255a7 7 0 0 0 3.072 2.472M3.82 11a13.7 13.7 0 0 1-.312-2.5h-2.49c.062.89.291 1.733.656 2.5zm6.853 3.472A7 7 0 0 0 13.745 12H11.91a9.3 9.3 0 0 1-.64 1.539 7 7 0 0 1-.597.933M8.5 12v2.923c.67-.204 1.335-.82 1.887-1.855q.26-.487.468-1.068zm3.68-1h2.146c.365-.767.594-1.61.656-2.5h-2.49a13.7 13.7 0 0 1-.312 2.5m2.802-3.5a7 7 0 0 0-.656-2.5H12.18c.174.782.282 1.623.312 2.5zM11.27 2.461c.247.464.462.98.64 1.539h1.835a7 7 0 0 0-3.072-2.472c.218.284.418.598.597.933M10.855 4a8 8 0 0 0-.468-1.068C9.835 1.897 9.17 1.282 8.5 1.077V4z"/>';
const bsBucket = '<path d="M2.522 5H2a.5.5 0 0 0-.494.574l1.372 9.149A1.5 1.5 0 0 0 4.36 16h7.278a1.5 1.5 0 0 0 1.483-1.277l1.373-9.149A.5.5 0 0 0 14 5h-.522A5.5 5.5 0 0 0 2.522 5m1.005 0a4.5 4.5 0 0 1 8.945 0z"/>';
const bsHddRack = '<path d="M2 2a2 2 0 0 0-2 2v1a2 2 0 0 0 2 2h1v2H2a2 2 0 0 0-2 2v1a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-1a2 2 0 0 0-2-2h-1V7h1a2 2 0 0 0 2-2V4a2 2 0 0 0-2-2zm.5 3a.5.5 0 1 1 0-1 .5.5 0 0 1 0 1m2 0a.5.5 0 1 1 0-1 .5.5 0 0 1 0 1m-2 7a.5.5 0 1 1 0-1 .5.5 0 0 1 0 1m2 0a.5.5 0 1 1 0-1 .5.5 0 0 1 0 1M12 7v2H4V7z"/>';

// All glyphs are Bootstrap Icons fills (16-grid, currentColor) — the
// secured variants composite two of the same set, so every source type
// renders in one consistent visual style.
const SRC_SVG = {
  s3: bsWrap(bsBucket),
  sftp: comp('ssh', bsHddNetwork, bsTerminal),
  scp: comp('ssh', bsHddNetwork, bsTerminal),
  ftp: bsWrap(bsHddNetwork),
  ftps: comp('tls', bsHddNetwork, bsLock),
  webdav: bsWrap(bsGlobe),
  webdavs: comp('tls-web', bsGlobe, bsLock),
  local: bsWrap(bsPcDisplay),
  other: bsWrap(bsHddRack),
};

export function srcIcon(stype) {
  return SRC_SVG[stype] || SRC_SVG.other;
}

// srcIconEl wraps a source glyph in a tinted span: the strokes use
// currentColor, so an explicit color paints the source's accent while the
// inherited text color keeps the theme default. Every surface that shows a
// source's identity (sidebar rows, the breadcrumb's root crumb) builds on
// this one helper, so the icon is identical everywhere.
export function srcIconEl(stype, color) {
  const s = el('span', { class: 'src-ic' });
  s.innerHTML = srcIcon(stype);
  if (color) s.style.color = color;
  return s;
}
