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

export const clipboard = {
  mode: null,      // 'copy' | 'cut'
  bucket: null,
  keys: [],        // selected entry keys (folders end with '/')
};

export const view = {
  sortKey: 'name',
  sortDir: 1,      // 1 asc, -1 desc
  filter: '',
};
