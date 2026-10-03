// Navigation history + selection/clipboard/view state.
// Locations: {kind:'buckets', source}                    — buckets of one S3 source
//          | {kind:'objects', source, bucket, prefix}    — objects of one S3 source
//          | {kind:'remote', source, path}               — sftp/scp/ftp/ftps/webdav/local
//          | {kind:'local', dir}                        — a workstation folder (native path)

export const nav = {
  stack: [],       // back stack
  forward: [],     // forward stack
  current: null,   // active location

  canBack() { return this.stack.length > 0; },
  canForward() { return this.forward.length > 0; },

  to(loc, { push = true } = {}) {
    // Two old rules bend here. Re-listing the very same place (clicking
    // the current breadcrumb segment or the tree node already open) is a
    // refresh in disguise — no stack moves. And a climb to an ancestor
    // (tree node, breadcrumb segment, parent row, typed path) is a
    // backward move in disguise: the deeper view just left stays one
    // Forward away instead of being cut, so the arrow answers no matter
    // which road uphill was taken — not only after the Back button.
    if (push && this.current && !sameLoc(loc, this.current)) {
      this.stack.push(this.current);
      if (isAncestorLoc(loc, this.current)) this.forward.push(this.current);
      else this.forward = [];
    }
    this.current = loc;
    this.listeners.forEach((fn) => fn(loc));
  },

  back() {
    if (!this.stack.length) return null;
    const loc = this.stack.pop();
    if (this.current) this.forward.push(this.current);
    this.current = loc;
    this.listeners.forEach((fn) => fn(loc));
    return loc;
  },

  forwardGo() {
    if (!this.forward.length) return null;
    const loc = this.forward.pop();
    if (this.current) this.stack.push(this.current);
    this.current = loc;
    this.listeners.forEach((fn) => fn(loc));
    return loc;
  },

  // Replace in place (refresh / rename navigation), keeping history intact.
  replace(loc) {
    this.current = loc;
    this.listeners.forEach((fn) => fn(loc));
  },

  listeners: [],
  onNavigate(fn) { this.listeners.push(fn); },
};

// localParentDir: the parent of a native workstation path, mirroring the
// backend's LocalParent (filepath.Dir of the cleaned path) — the drive
// root (C:\), a UNC share root (\\server\share) and the unix root sit at
// the top and answer null, so the climb just ends ('' and '~' — the
// pane's roots/home shorthands — never reach a main view).
function localParentDir(p) {
  const s = String(p || '');
  if (s === '' || s === '~') return null;
  if (s.startsWith('\\\\')) {
    const parts = s.replace(/\\+$/, '').split('\\').filter(Boolean);
    if (parts.length <= 2) return null; // \\server\share is the top
    return `\\\\${parts.slice(0, -1).join('\\')}`;
  }
  const d = s.match(/^([a-zA-Z]:)([\\/](.*))?$/);
  if (d) {
    const segs = (d[3] || '').split(/[\\/]/).filter(Boolean);
    if (!segs.length) return null; // the drive root
    return `${d[1]}\\${segs.slice(0, -1).join('\\')}`;
  }
  const u = s.replace(/\/+$/, '');
  if (u.startsWith('/')) {
    const segs = u.slice(1).split('/').filter(Boolean);
    if (segs.length > 1) return `/${segs.slice(0, -1).join('/')}`;
    return null; // the unix root
  }
  return null; // relative junk — no parent to climb to
}

// Parent of a location; null when already at top.
export function parentOf(loc) {
  if (loc.kind === 'buckets') return null;
  if (loc.kind === 'remote') {
    if (!loc.path || loc.path === '' || loc.path === '/') return null;
    const p = loc.path.replace(/\/+$/, '');
    const i = p.lastIndexOf('/');
    return { kind: 'remote', source: loc.source, path: i >= 0 ? p.slice(0, i + 1) : '' };
  }
  if (loc.kind === 'local') {
    const up = localParentDir(loc.dir);
    return up === null ? null : { kind: 'local', dir: up };
  }
  if (!loc.prefix || loc.prefix === '') return { kind: 'buckets', source: loc.source };
  const p = loc.prefix.replace(/\/+$/, '');
  const i = p.lastIndexOf('/');
  return { kind: 'objects', source: loc.source, bucket: loc.bucket, prefix: i >= 0 ? p.slice(0, i + 1) : '' };
}

// normRemotePath normalizes a remote path for comparison: listings, crumb
// segments and tree nodes spell the same place differently ('' vs '/',
// '/a/b/' vs 'a/b/'), so the slashes go at both ends before comparing.
const normRemotePath = (p) => String(p || '').replace(/^\/+/, '').replace(/\/+$/, '');

// sameLoc: do two locations name the same listing? The source must match
// exactly; remote paths compare slash-normalized; object prefixes keep
// their trailing-slash vocabulary but tolerate a missing one at the root.
export function sameLoc(a, b) {
  if (!a || !b || a.kind !== b.kind) return false;
  if ((a.source || '') !== (b.source || '')) return false;
  if (a.kind === 'remote') return normRemotePath(a.path) === normRemotePath(b.path);
  if (a.kind === 'objects') {
    if ((a.bucket || '') !== (b.bucket || '')) return false;
    return (a.prefix || '').replace(/\/+$/, '') === (b.prefix || '').replace(/\/+$/, '');
  }
  if (a.kind === 'local') return (a.dir || '') === (b.dir || ''); // native paths, exact spelling
  return true; // buckets (and any kind keyed by source alone)
}

// isAncestorLoc: does the first location sit strictly above the second in
// the same lineage, so navigating there should read as going backward and
// keep Forward alive? Walks the parent chain — the buckets view above any
// listing of the source and every crumb-segment hop up qualify equally.
export function isAncestorLoc(up, loc) {
  if (!up || !loc) return false;
  let cur = loc;
  for (let i = 0; i < 128 && cur; i += 1) {
    cur = parentOf(cur);
    if (!cur) return false;
    if (sameLoc(cur, up)) return true;
  }
  return false;
}

// Clipboard for the cross-source matrix. kind records the ORIGIN so paste
// can build the right TransferCross payload: 's3' (a named S3 source),
// 'remote' (a named remote source) or 'local' (local-pane paths). dir is the
// origin directory (same-dir paste is a no-op and gets refused).
export const clipboard = {
  mode: null,      // 'copy' | 'cut'
  kind: null,      // 's3' | 'remote' | 'local'
  bucket: null,    // s3 origin
  source: null,    // remote origin (source name)
  dir: null,       // origin prefix/path/dir
  keys: [],        // s3/remote: selected entry keys (folders end with '/')
  paths: [],       // local origin: absolute paths
};

// clipHasItems: whether any origin's payload is present.
export function clipHasItems() {
  return clipboard.keys.length > 0 || clipboard.paths.length > 0;
}

export const view = {
  sortKey: 'name',
  sortDir: 1,      // 1 asc, -1 desc
  filter: '',
};
