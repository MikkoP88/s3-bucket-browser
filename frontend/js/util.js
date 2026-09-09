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
