// Lazy sidebar tree: bucket roots expand into folder levels on demand.
import { api, onEvent } from './api.js';
import { el } from './util.js';

export class Tree {
  constructor({ onNavigate, onDropTo }) {
    this.container = document.getElementById('tree');
    this.onNavigate = onNavigate;
    this.onDropTo = onDropTo;
    this.nodes = new Map(); // id -> node
    this.currentId = null;
  }

  nodeKey(bucket, prefix) { return `${bucket}/${prefix}`; }

  async refresh(buckets, currentLoc) {
    const keep = new Map();
    for (const b of buckets) {
      const id = this.nodeKey(b.name, '');
      const prev = this.nodes.get(id);
      keep.set(id, {
        id, bucket: b.name, prefix: '', label: b.name,
        expanded: prev?.expanded ?? false,
        loaded: false, children: [], level: 0, el: null, twistEl: null,
      });
    }
    // preserve expanded folder nodes that still exist under kept roots
    for (const [id, n] of this.nodes) {
      if (n.prefix && keep.has(this.nodeKey(n.bucket, ''))) keep.set(id, n);
    }
    this.nodes = keep;
    this.render(currentLoc);
    // re-expand the previously-open roots lazily
    for (const n of keep.values()) {
      if (n.prefix === '' && n.expanded) this.expand(n.id);
    }
  }

  async expand(id) {
    const n = this.nodes.get(id);
    if (!n) return;
    n.expanded = true;
    if (!n.loaded) {
      try {
        const dirs = await this.listDirs(n.bucket, n.prefix);
        for (const d of dirs) {
          const cid = this.nodeKey(n.bucket, d.key);
          const prev = this.nodes.get(cid);
          this.nodes.set(cid, prev ?? {
            id: cid, bucket: n.bucket, prefix: d.key, label: d.name,
            expanded: false, loaded: false, children: [], level: n.level + 1, el: null, twistEl: null,
          });
          const node = this.nodes.get(cid);
          node.level = n.level + 1;
          node.loaded = false; // reload children on demand
          if (node.expanded) this.expand(cid);
          n.children.push(node);
        }
        n.loaded = true;
      } catch (err) {
        n.expanded = false;
        console.error('tree expand failed', err);
      }
    }
    this.render();
  }

  // listDirs streams one directory view and keeps only the folders. The
  // streaming API keeps Go-side memory at one page even for
  // million-object buckets (M5 performance pass, PLAN.md §13).
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

  markCurrent(loc) {
    this.currentId = loc.kind === 'buckets' ? null : this.nodeKey(loc.bucket, loc.prefix || '');
    this.render();
  }

  render(currentLoc) {
    if (currentLoc) this.markCurrent(currentLoc);
    const roots = [...this.nodes.values()].filter((n) => n.prefix === '');
    roots.sort((a, b) => a.label.toLowerCase() < b.label.toLowerCase() ? -1 : 1);
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
    const row = el('div', {
      class: `tnode${this.currentId === n.id ? ' sel' : ''}`,
      style: `padding-left:${8 + n.level * 14}px`,
      onclick: () => this.onNavigate({ kind: 'objects', bucket: n.bucket, prefix: n.prefix }),
      ondblclick: () => (n.expanded ? this.collapse(n.id) : this.expand(n.id)),
    },
      twist,
      el('span', { class: 'ticon', text: n.prefix === '' ? '\u{1F5C0}' : '\u{1F4C1}' }),
      el('span', { class: 'tlabel', text: n.label }),
    );
    row.dataset.bucket = n.bucket;
    row.dataset.prefix = n.prefix;

    // drop target for internal copy/move
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
      this.onDropTo({ bucket: n.bucket, prefix: n.prefix }, JSON.parse(data), e);
    });

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
    if (loc.kind !== 'objects') return;
    await this.expand(this.nodeKey(loc.bucket, ''));
    if (!loc.prefix) return;
    let acc = '';
    for (const part of loc.prefix.split('/')) {
      if (!part) continue;
      acc += part + '/';
      await this.expand(this.nodeKey(loc.bucket, acc));
    }
    this.markCurrent(loc);
  }
}
