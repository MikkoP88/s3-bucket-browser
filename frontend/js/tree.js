// Lazy sidebar tree (M9/M11): the configured data sources are the top level
// and EVERY source type expands into its own content — remote/local sources
// list their directories, S3 sources list their bucket's folders (every S3
// data source is scoped to ONE bucket; its node carries the bucket identity
// from the definition, so all source types render the same structure —
// content folders directly under the source). Legacy account-wide S3 sources
// (no bucket) keep the bucket-list level. Node ids are namespaced per
// source, so two sources can hold same-named buckets without colliding.
import { api, subscribeStream } from './api.js';
import { el, srcIcon } from './util.js';

export class Tree {
  // guardOf(bucket, source) returns the cached guard state for the bucket
  // row icons ({versioning, lockEnabled, lockMode, lockDays} or null);
  // onGuardClick(bucket, source) opens the admin panel from those icons;
  // onBuckets(source, names) is called when bucket rows appear so the caller
  // can fetch their guards in the background.
  constructor({ onNavigate, onDropTo, onContext, guardOf, onGuardClick, onBuckets }) {
    this.container = document.getElementById('tree');
    this.onNavigate = onNavigate;
    this.onDropTo = onDropTo;
    this.onContext = onContext;
    this.guardOf = guardOf || (() => null);
    this.onGuardClick = onGuardClick;
    this.onBuckets = onBuckets;
    this.nodes = new Map(); // id -> node
    this.currentId = null;
    this.status = new Map(); // source name -> 'ok' | 'error' | 'busy'
  }

  nodeKey(source, bucket, prefix) { return `bkt:${source}:${bucket}:${prefix}`; }
  srcKey(name) { return `src:${name}`; }
  rsrcKey(name, path) { return `src:${name}:${path}`; }

  // setStatus feeds the per-source connectivity balls ('ok' | 'error' |
  // 'busy'); a partial map keeps the previous state of untouched sources.
  setStatus(map) {
    this.status = new Map(Object.entries(map));
    this.render();
  }

  // setSources rebuilds the top level from the configured sources,
  // preserving the expansion state of every surviving node.
  setSources(sources, currentLoc) {
    const keep = new Map();
    for (const s of sources) {
      const id = this.srcKey(s.name);
      const prev = this.nodes.get(id);
      keep.set(id, prev || {
        id, kind: 'source', source: s.name, stype: s.type, color: s.color,
        label: s.name, expanded: false, loaded: false, children: [],
        level: 0, el: null, twistEl: null,
      });
      const n = keep.get(id);
      n.stype = s.type;
      n.color = s.color;
      // S3 sources are bucket-scoped by definition: the node adopts the
      // bucket identity up front (bucket-grade navigation, guard icons,
      // drop target). Legacy account-wide sources (no bucket) keep the
      // bucket-list level. A changed/removed bucket resets the subtree.
      if (s.type === 's3') {
        if (s.bucket) {
          if (n.bucket !== s.bucket) { n.children = []; n.loaded = false; }
          n.bucket = s.bucket;
          n.prefix = '';
        } else if (n.bucket !== undefined) {
          delete n.bucket;
          delete n.prefix;
          n.children = [];
          n.loaded = false;
        }
      }
    }
    // preserve bucket subtrees under surviving S3 sources and expanded
    // remote directory nodes under surviving sources
    for (const [id, n] of this.nodes) {
      if (n.kind === 'source') continue;
      if (n.bucket !== undefined && keep.has(this.srcKey(n.source))) keep.set(id, n);
      else if (n.kind === 'rdir' && keep.has(this.srcKey(n.source))) keep.set(id, n);
    }
    this.nodes = keep;
    this.render(currentLoc);
    // re-expand previously-open sources lazily
    for (const [id, n] of keep) {
      if (n.kind === 'source' && n.expanded) this.expand(id);
    }
  }

  // refresh feeds one LEGACY account-wide S3 source's buckets view into its
  // tree node (bucket-scoped sources never show the bucket list).
  refresh(source, buckets, currentLoc) {
    const root = this.nodes.get(this.srcKey(source));
    if (!root || root.bucket !== undefined) return;
    this.buildBucketChildren(root, buckets);
    root.expanded = true;
    this.render(currentLoc);
  }

  // buildBucketChildren is the legacy S3 level: one row per bucket.
  buildBucketChildren(n, buckets) {
    n.children = [];
    for (const b of buckets) {
      const id = this.nodeKey(n.source, b.name, '');
      const prev = this.nodes.get(id);
      const node = prev || {
        id, bucket: b.name, prefix: '', source: n.source, label: b.name,
        expanded: false, loaded: false, children: [], level: n.level + 1, el: null, twistEl: null,
      };
      node.level = n.level + 1;
      this.nodes.set(id, node);
      n.children.push(node);
    }
    n.loaded = true;
    this.onBuckets?.(n.source, buckets.map((b) => b.name));
  }

  async expand(id) {
    const n = this.nodes.get(id);
    if (!n) return;
    n.expanded = true;
    if (!n.loaded) {
      try {
        if (n.kind === 'source' && n.stype === 's3') {
          if (n.bucket !== undefined) {
            // bucket-scoped source: content folders directly under it
            await this.buildS3Children(n);
            this.onBuckets?.(n.source, [n.bucket]);
          } else {
            // legacy account-wide source: the bucket list
            this.buildBucketChildren(n, await api.ListSourceBuckets(n.source));
          }
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
  // the source-pinned streaming listing (memory stays at one page, §13).
  buildS3Children(n) {
    return this.listDirs(n.source, n.bucket, n.prefix).then((dirs) => {
      n.children = [];
      for (const d of dirs) {
        const cid = this.nodeKey(n.source, n.bucket, d.key);
        const prev = this.nodes.get(cid);
        const node = prev || {
          id: cid, bucket: n.bucket, prefix: d.key, source: n.source, label: d.name,
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

  // listDirs streams one source-pinned directory view and keeps only the
  // folders.
  listDirs(source, bucket, prefix) {
    return new Promise((resolve, reject) => {
      const dirs = [];
      let settled = false;
      const finish = (fn, val) => {
        if (settled) return;
        settled = true;
        stream.off();
        fn(val);
      };
      const stream = subscribeStream(
        () => api.ListSourceObjectsStream(source, bucket, prefix),
        (p) => {
          if (p.error) { finish(reject, new Error(p.error)); return; }
          for (const e of p.entries || []) if (e.isDir) dirs.push(e);
          if (p.done) finish(resolve, dirs);
        },
      );
      stream.begin.then(() => stream.flush(), (e) => finish(reject, e));
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
      this.currentId = loc.source ? this.srcKey(loc.source) : null;
    } else if (loc.kind === 'remote') {
      this.currentId = this.rsrcKey(loc.source, loc.path || '/');
    } else if (loc && loc.kind === 'objects') {
      let id = this.nodeKey(loc.source || '', loc.bucket, loc.prefix || '');
      if (!this.nodes.has(id) && !loc.prefix) {
        // bucket-scoped source: the bucket row IS the source node
        const s = this.nodes.get(this.srcKey(loc.source || ''));
        if (s && s.bucket === loc.bucket) id = s.id;
      }
      this.currentId = id;
    } else {
      this.currentId = null;
    }
    this.render();
  }

  // navigate maps a node click onto a location: every source opens its own
  // top view (S3 → its bucket's contents, legacy account-wide S3 → its
  // buckets, remote → its root), buckets and folders drill down. The bucket
  // of a bucket-scoped source comes from the definition, so the first click
  // opens the contents directly — no learning round trip.
  navigate(n) {
    if (n.kind === 'source') {
      if (n.stype === 's3' && n.bucket !== undefined) {
        this.onNavigate({ kind: 'objects', source: n.source, bucket: n.bucket, prefix: '' });
      } else if (n.stype === 's3') {
        this.onNavigate({ kind: 'buckets', source: n.source });
      } else {
        this.onNavigate({ kind: 'remote', source: n.source, path: '' });
      }
    } else if (n.kind === 'rdir') {
      this.onNavigate({ kind: 'remote', source: n.source, path: n.path });
    } else {
      this.onNavigate({ kind: 'objects', source: n.source, bucket: n.bucket, prefix: n.prefix });
    }
  }

  render(currentLoc) {
    if (currentLoc) this.markCurrent(currentLoc);
    const roots = [...this.nodes.values()].filter((n) => n.kind === 'source');
    roots.sort((a, b) => (a.label.toLowerCase() < b.label.toLowerCase() ? -1 : 1));
    this.container.replaceChildren();
    for (const r of roots) this.container.appendChild(this.renderNode(r));
  }

  // guardIcons builds the versioning / object-lock indicators shown after a
  // bucket's name in the tree: 🔄 when versioning is on (dim when
  // suspended), 🔒 when object lock is configured. Both open the admin
  // panel on click.
  guardIcons(bucket, source) {
    const g = this.guardOf(bucket, source);
    if (!g) return [];
    const icons = [];
    if (g.versioning === 'Enabled' || g.versioning === 'Suspended') {
      icons.push(el('span', {
        class: `tguard${g.versioning === 'Enabled' ? '' : ' dim'}`,
        text: '\u{1F504}',
        title: g.versioning === 'Enabled'
          ? 'Versioning enabled — every write keeps previous versions'
          : 'Versioning suspended — existing versions are kept',
        onclick: (e) => { e.stopPropagation(); this.onGuardClick?.(bucket, source); },
      }));
    }
    if (g.lockEnabled) {
      icons.push(el('span', {
        class: 'tguard',
        text: '\u{1F512}',
        title: `Object Lock: ${g.lockMode || 'on'}${g.lockDays ? ` — ${g.lockDays}d default retention` : ''}`,
        onclick: (e) => { e.stopPropagation(); this.onGuardClick?.(bucket, source); },
      }));
    }
    return icons;
  }

  // statusBall renders the connectivity ball after a source's name.
  statusBall(source) {
    const st = this.status.get(source) || 'unknown';
    const title = st === 'ok' ? 'Connected'
      : st === 'error' ? 'Connection problem — right-click for Test/Reconnect'
        : st === 'busy' ? 'Checking connection…' : 'Status unknown';
    return el('span', { class: `sball ${st}`, title });
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
    const ticon = el('span', { class: 'ticon' });
    if (n.kind === 'source') {
      // the type glyph painted in the source's own accent color — the
      // same icon the breadcrumb's root crumb carries
      ticon.innerHTML = srcIcon(n.stype);
      if (n.color) ticon.style.color = n.color;
    } else ticon.textContent = n.kind === 'rdir' ? '\u{1F4C1}' : n.prefix === '' ? '\u{1F5C0}' : '\u{1F4C1}';
    const row = el('div', {
      class: `tnode${this.currentId === n.id ? ' sel' : ''}`,
      style: `padding-left:${8 + n.level * 14}px`,
      title: n.kind === 'source'
        ? (n.bucket !== undefined ? `${n.label} (s3 — bucket ${n.bucket})` : `${n.label} (${n.stype})`)
        : undefined,
      onclick: () => this.navigate(n),
      ondblclick: () => (n.expanded ? this.collapse(n.id) : this.expand(n.id)),
    },
      twist,
      ticon,
      el('span', { class: 'tlabel', text: n.label }),
      // source rows carry their connectivity ball right after the name
      ...(n.kind === 'source' ? [this.statusBall(n.source)] : []),
      // bucket rows carry their versioning / lock state right after the name
      ...(n.bucket !== undefined && n.prefix === '' ? this.guardIcons(n.bucket, n.source) : []),
    );
    // Node identity for DOM-level hit-testing (the OS file-drop handler in
    // main.js resolves the node under the cursor from these). Attributes
    // are set only when defined — dataset assignment stringifies
    // undefined into "undefined" otherwise.
    row.dataset.tkind = n.kind;
    if (n.bucket !== undefined) row.dataset.bucket = n.bucket;
    if (n.prefix !== undefined) row.dataset.prefix = n.prefix;
    if (n.source) row.dataset.source = n.source;
    if (n.stype) row.dataset.stype = n.stype;
    if (n.kind === 'rdir') row.dataset.rdir = n.path || '/';

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
        this.onDropTo({ kind: 's3', source: n.source, bucket: n.bucket, dir: n.prefix }, JSON.parse(data), e);
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
      // into their root directory. Bucket-scoped S3 roots are drop targets
      // via the S3 branch above; legacy account-wide roots need a bucket
      // picked first — no drop.
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
      const srcId = this.srcKey(loc.source || '');
      let rootId = this.nodeKey(loc.source || '', loc.bucket, '');
      // the bucket node may not exist yet (a legacy source never expanded)
      // — load the source level first, then pick whichever root it has: a
      // bucket-scoped source node IS the bucket root
      if (!this.nodes.has(rootId)) await this.expand(srcId);
      const s = this.nodes.get(srcId);
      if (s && s.stype === 's3' && s.bucket === loc.bucket) rootId = srcId;
      await this.expand(rootId);
      if (!loc.prefix) { this.markCurrent(loc); return; }
      let acc = '';
      for (const part of loc.prefix.split('/')) {
        if (!part) continue;
        acc += part + '/';
        await this.expand(this.nodeKey(loc.source || '', loc.bucket, acc));
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
    } else if (loc.kind === 'buckets') {
      await this.expand(this.srcKey(loc.source || ''));
      this.markCurrent(loc);
    }
  }
}
