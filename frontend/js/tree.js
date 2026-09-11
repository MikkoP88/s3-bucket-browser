// Lazy sidebar tree (M9): the configured data sources are the top level.
// The default S3 source expands into its buckets (the classic view);
// remote/local sources expand into their remote directories on demand.
import { api, onEvent } from './api.js';
import { el } from './util.js';

const SRC_ICON = {
  s3: '\u{1F5C2}',
  sftp: '\u{1F5DD}',
  scp: '\u{1F5DD}',
  ftp: '\u{1F517}',
  ftps: '\u{1F517}',
  webdav: '\u{1F310}',
  webdavs: '\u{1F310}',
  local: '\u{1F4BB}',
};

export class Tree {
  constructor({ onNavigate, onDropTo, onContext }) {
    this.container = document.getElementById('tree');
    this.onNavigate = onNavigate;
    this.onDropTo = onDropTo;
    this.onContext = onContext;
    this.nodes = new Map(); // id -> node
    this.currentId = null;
    this.defaultS3 = null; // name of the S3 source owning the bucket subtree
  }

  nodeKey(bucket, prefix) { return `${bucket}/${prefix}`; }
  srcKey(name) { return `src:${name}`; }
  rsrcKey(name, path) { return `src:${name}:${path}`; }

  // setSources rebuilds the top level from the configured sources,
  // preserving the expansion state of every surviving node.
  setSources(sources, currentLoc) {
    const def = sources.find((s) => s.type === 's3' && s.default)
      || sources.find((s) => s.type === 's3');
    this.defaultS3 = def ? def.name : null;

    const keep = new Map();
    for (const s of sources) {
      const id = this.srcKey(s.name);
      const prev = this.nodes.get(id);
      keep.set(id, prev || {
        id, kind: 'source', source: s.name, stype: s.type,
        label: s.name, expanded: false, loaded: false, children: [],
        level: 0, el: null, twistEl: null,
      });
      keep.get(id).stype = s.type;
    }
    // preserve the S3 bucket subtree (owned by the default source) and
    // any expanded remote directory nodes under surviving sources
    for (const [id, n] of this.nodes) {
      if (n.kind === 'source') continue;
      if (n.bucket !== undefined && this.defaultS3) keep.set(id, n);
      else if (n.kind === 'rdir' && keep.has(this.srcKey(n.source))) keep.set(id, n);
    }
    this.nodes = keep;
    this.render(currentLoc);
    // re-expand previously-open sources lazily
    for (const [id, n] of keep) {
      if (n.kind === 'source' && n.expanded) this.expand(id);
    }
  }

  // refresh feeds the buckets view's listing into the default S3 source's
  // children (creating it defensively when setSources has not run yet).
  async refresh(buckets, currentLoc) {
    if (!this.defaultS3) return;
    const root = this.nodes.get(this.srcKey(this.defaultS3));
    if (!root) return;
    this.buildBucketChildren(root, buckets);
    root.expanded = true;
    this.render(currentLoc);
  }

  buildBucketChildren(n, buckets) {
    n.children = [];
    for (const b of buckets) {
      const id = this.nodeKey(b.name, '');
      const prev = this.nodes.get(id);
      const node = prev || {
        id, bucket: b.name, prefix: '', label: b.name,
        expanded: false, loaded: false, children: [], level: n.level + 1, el: null, twistEl: null,
      };
      node.level = n.level + 1;
      this.nodes.set(id, node);
      n.children.push(node);
    }
    n.loaded = true;
  }

  async expand(id) {
    const n = this.nodes.get(id);
    if (!n) return;
    // non-default S3 sources are leaves: their objects are not operable
    // until multi-source transfers land (click opens the root listing)
    if (n.kind === 'source' && n.stype === 's3' && n.source !== this.defaultS3) return;
    n.expanded = true;
    if (!n.loaded) {
      try {
        if (n.kind === 'source' && n.stype === 's3') {
          this.buildBucketChildren(n, await api.ListBuckets());
        } else if (n.kind === 'source' || n.kind === 'rdir') {
          this.buildRemoteChildren(n, await api.RemoteList(n.source, n.kind === 'source' ? '/' : n.path));
        } else if (n.bucket !== undefined) {
          await this.buildS3Children(n);
        }
      } catch (err) {
        n.expanded = false;
        console.error('tree expand failed', err);
      }
    }
    this.render();
  }

  // buildS3Children loads the folder level of a bucket/prefix node through
  // the streaming listing API (memory stays at one page, PLAN.md §13).
  buildS3Children(n) {
    return this.listDirs(n.bucket, n.prefix).then((dirs) => {
      n.children = [];
      for (const d of dirs) {
        const cid = this.nodeKey(n.bucket, d.key);
        const prev = this.nodes.get(cid);
        const node = prev || {
          id: cid, bucket: n.bucket, prefix: d.key, label: d.name,
          expanded: false, loaded: false, children: [], level: n.level + 1, el: null, twistEl: null,
        };
        node.level = n.level + 1;
        node.loaded = false; // reload children on demand
        this.nodes.set(cid, node);
        n.children.push(node);
        if (node.expanded) this.expand(cid);
      }
      n.loaded = true;
    });
  }

  // buildRemoteChildren keeps the folders of one remotefs listing. Entry
  // keys are full anchored paths ('/docs/'), so child ids concatenate
  // nothing — the key is the identity.
  buildRemoteChildren(n, entries) {
    n.children = [];
    for (const e of entries) {
      if (!e.isDir) continue;
      const id = this.rsrcKey(n.source, e.key);
      const prev = this.nodes.get(id);
      const node = prev || {
        id, kind: 'rdir', source: n.source, path: e.key, label: e.name,
        expanded: false, loaded: false, children: [], level: n.level + 1, el: null, twistEl: null,
      };
      node.level = n.level + 1;
      node.loaded = false;
      this.nodes.set(id, node);
      n.children.push(node);
      if (node.expanded) this.expand(id);
    }
    n.loaded = true;
  }

  // updateRemoteDir feeds a freshly listed directory view into its tree
  // node, so the tree tracks remote mutations (mkdir/rename/delete) with
  // the grid instead of showing stale children until re-expand.
  updateRemoteDir(source, path, entries) {
    const id = (!path || path === '/') ? this.srcKey(source) : this.rsrcKey(source, path);
    const n = this.nodes.get(id);
    if (!n) return;
    this.buildRemoteChildren(n, entries);
    n.expanded = true;
    this.render();
  }

  // listDirs streams one directory view and keeps only the folders.
  listDirs(bucket, prefix) {
    return new Promise((resolve, reject) => {
      const dirs = [];
      let settled = false;
      let offPage = null;
      const finish = (fn, val) => {
        if (settled) return;
        settled = true;
        offPage?.();
        fn(val);
      };
      api.ListObjectsStream(bucket, prefix).then((token) => {
        offPage = onEvent('list:page', (p) => {
          if (p.token !== token) return;
          if (p.error) { finish(reject, new Error(p.error)); return; }
          for (const e of p.entries || []) if (e.isDir) dirs.push(e);
          if (p.done) finish(resolve, dirs);
        });
      }).catch((e) => finish(reject, e));
    });
  }

  collapse(id) {
    const n = this.nodes.get(id);
    if (n) { n.expanded = false; this.render(); }
  }

  // reload drops a node's children and re-expands it (context Refresh).
  reload(id) {
    const n = this.nodes.get(id);
    if (!n) return;
    n.loaded = false;
    n.children = [];
    this.expand(id);
  }

  collapseAll() {
    for (const n of this.nodes.values()) n.expanded = false;
    this.render();
  }

  markCurrent(loc) {
    if (loc && loc.kind === 'buckets') {
      this.currentId = this.defaultS3 ? this.srcKey(this.defaultS3) : null;
    } else if (loc && loc.kind === 'srcroot') {
      this.currentId = this.srcKey(loc.source);
    } else if (loc && loc.kind === 'remote') {
      this.currentId = this.rsrcKey(loc.source, loc.path || '/');
    } else if (loc && loc.kind === 'objects') {
      this.currentId = this.nodeKey(loc.bucket, loc.prefix || '');
    } else {
      this.currentId = null;
    }
    this.render();
  }

  // navigate maps a node click onto a location: source roots open their
  // top view, remote folders their directory, buckets stay classic.
  navigate(n) {
    if (n.kind === 'source') {
      if (n.stype === 's3') {
        this.onNavigate(n.source === this.defaultS3
          ? { kind: 'buckets' }
          : { kind: 'srcroot', source: n.source });
      } else {
        this.onNavigate({ kind: 'remote', source: n.source, path: '' });
      }
    } else if (n.kind === 'rdir') {
      this.onNavigate({ kind: 'remote', source: n.source, path: n.path });
    } else {
      this.onNavigate({ kind: 'objects', bucket: n.bucket, prefix: n.prefix });
    }
  }

  render(currentLoc) {
    if (currentLoc) this.markCurrent(currentLoc);
    const roots = [...this.nodes.values()].filter((n) => n.kind === 'source');
    roots.sort((a, b) => (a.label.toLowerCase() < b.label.toLowerCase() ? -1 : 1));
    this.container.replaceChildren();
    for (const r of roots) this.container.appendChild(this.renderNode(r));
  }

  renderNode(n) {
    const hasKids = n.loaded && n.children.length > 0;
    const twist = el('span', {
      class: 'twist',
      text: n.expanded ? '\u25BC' : '\u25B6',
      onclick: (e) => { e.stopPropagation(); n.expanded ? this.collapse(n.id) : this.expand(n.id); },
    });
    n.twistEl = twist;
    if (!hasKids && !n.expanded) twist.style.visibility = 'hidden';
    const icon = n.kind === 'source' ? (SRC_ICON[n.stype] || '\u{1F5C2}')
      : n.kind === 'rdir' ? '\u{1F4C1}'
        : n.prefix === '' ? '\u{1F5C0}' : '\u{1F4C1}';
    const row = el('div', {
      class: `tnode${this.currentId === n.id ? ' sel' : ''}`,
      style: `padding-left:${8 + n.level * 14}px`,
      title: n.kind === 'source' ? `${n.label} (${n.stype})` : undefined,
      onclick: () => this.navigate(n),
      ondblclick: () => (n.expanded ? this.collapse(n.id) : this.expand(n.id)),
    },
      twist,
      el('span', { class: 'ticon', text: icon }),
      el('span', { class: 'tlabel', text: n.label }),
    );
    row.dataset.bucket = n.bucket;
    row.dataset.prefix = n.prefix;

    // S3 nodes and remote directory nodes are both drop targets and carry
    // full context menus (the cross-source matrix treats them uniformly).
    if (n.bucket !== undefined) {
      row.addEventListener('dragover', (e) => {
        if (!e.dataTransfer.types.includes('application/x-s3b')) return;
        e.preventDefault();
        row.classList.add('drop-target');
      });
      row.addEventListener('dragleave', () => row.classList.remove('drop-target'));
      row.addEventListener('drop', (e) => {
        row.classList.remove('drop-target');
        const data = e.dataTransfer.getData('application/x-s3b');
        if (!data) return;
        e.preventDefault();
        this.onDropTo({ kind: 's3', bucket: n.bucket, dir: n.prefix }, JSON.parse(data), e);
      });
      row.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        e.stopPropagation();
        this.onContext?.(e, n);
      });
    } else if (n.kind === 'rdir') {
      row.addEventListener('dragover', (e) => {
        if (!e.dataTransfer.types.includes('application/x-s3b')) return;
        e.preventDefault();
        row.classList.add('drop-target');
      });
      row.addEventListener('dragleave', () => row.classList.remove('drop-target'));
      row.addEventListener('drop', (e) => {
        row.classList.remove('drop-target');
        const data = e.dataTransfer.getData('application/x-s3b');
        if (!data) return;
        e.preventDefault();
        this.onDropTo({ kind: 'remote', source: n.source, dir: n.path }, JSON.parse(data), e);
      });
      row.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        e.stopPropagation();
        this.onContext?.(e, n);
      });
    } else if (n.kind === 'source') {
      // Source roots: full context menu; non-S3 roots also accept drops
      // into their root directory (S3 roots need a bucket — no drop).
      if (n.stype !== 's3') {
        row.addEventListener('dragover', (e) => {
          if (!e.dataTransfer.types.includes('application/x-s3b')) return;
          e.preventDefault();
          row.classList.add('drop-target');
        });
        row.addEventListener('dragleave', () => row.classList.remove('drop-target'));
        row.addEventListener('drop', (e) => {
          row.classList.remove('drop-target');
          const data = e.dataTransfer.getData('application/x-s3b');
          if (!data) return;
          e.preventDefault();
          this.onDropTo({ kind: 'remote', source: n.source, dir: '/' }, JSON.parse(data), e);
        });
      }
      row.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        e.stopPropagation();
        this.onContext?.(e, n);
      });
    } else {
      row.addEventListener('contextmenu', (e) => e.preventDefault());
    }

    n.el = row;
    const frag = document.createDocumentFragment();
    frag.appendChild(row);
    if (n.expanded && n.children.length) {
      for (const c of n.children) frag.appendChild(this.renderNode(c));
    }
    return frag;
  }

  // Reveal + expand the path to a location (after navigation from grid).
  async reveal(loc) {
    if (loc.kind === 'objects') {
      await this.expand(this.nodeKey(loc.bucket, ''));
      if (!loc.prefix) return;
      let acc = '';
      for (const part of loc.prefix.split('/')) {
        if (!part) continue;
        acc += part + '/';
        await this.expand(this.nodeKey(loc.bucket, acc));
      }
      this.markCurrent(loc);
    } else if (loc.kind === 'remote') {
      await this.expand(this.srcKey(loc.source));
      let acc = '';
      for (const part of (loc.path || '').split('/')) {
        if (!part) continue;
        acc += part + '/';
        await this.expand(this.rsrcKey(loc.source, acc));
      }
      this.markCurrent(loc);
    }
  }
}
