// Navigation history + selection/clipboard/view state.
// Locations: {kind:'buckets'} | {kind:'objects', bucket, prefix}
//          | {kind:'srcroot', source}          — non-default S3 source root
//          | {kind:'remote', source, path}     — sftp/scp/ftp/ftps/local

export const nav = {
  stack: [],       // back stack
  forward: [],     // forward stack
  current: null,   // active location

  canBack() { return this.stack.length > 0; },
  canForward() { return this.forward.length > 0; },

  to(loc, { push = true } = {}) {
    if (push && this.current) this.stack.push(this.current);
    if (push) this.forward = [];
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

// Parent of a location; null when already at top.
export function parentOf(loc) {
  if (loc.kind === 'buckets' || loc.kind === 'srcroot') return null;
  if (loc.kind === 'remote') {
    if (!loc.path || loc.path === '' || loc.path === '/') return null;
    const p = loc.path.replace(/\/+$/, '');
    const i = p.lastIndexOf('/');
    return { kind: 'remote', source: loc.source, path: i >= 0 ? p.slice(0, i + 1) : '' };
  }
  if (!loc.prefix || loc.prefix === '') return { kind: 'buckets' };
  const p = loc.prefix.replace(/\/+$/, '');
  const i = p.lastIndexOf('/');
  return { kind: 'objects', bucket: loc.bucket, prefix: i >= 0 ? p.slice(0, i + 1) : '' };
}

// Clipboard for the cross-source matrix. kind records the ORIGIN so paste
// can build the right TransferCross payload: 's3' (default-profile bucket),
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
