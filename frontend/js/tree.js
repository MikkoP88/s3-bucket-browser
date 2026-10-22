// Lazy sidebar tree (M9/M11): the configured data sources are the top level
// and EVERY source type expands into its own content — remote/local sources
// list their directories, S3 sources list their bucket's folders (every S3
// data source is scoped to ONE bucket; its node carries the bucket identity
// from the definition, so all source types render the same structure —
// content folders directly under the source). Legacy account-wide S3 sources
// (no bucket) keep the bucket-list level. Node ids are namespaced per
// source, so two sources can hold same-named buckets without colliding.
import { api, subscribeStream } from './api.js';
import { el, srcIconEl } from './util.js';
import { t } from './i18n.js';

// A source can hold more folders than any sidebar should seat as rows
// at once, so the tree renders a window of its flattened order:
// TREE_WINDOW rows to start, grown a page at a time by the scroll
// seam, the walk past the edge and the strip’s own verb —
// append-only, never a rebuild. The side footer keeps speaking the
// true count; the window never lies about the tree it seats.
const TREE_WINDOW = 400;
const TREE_PAGE = 400;

export class Tree {
  // guardOf(bucket, source) returns the cached guard state for the bucket
  // row icons ({versioning, lockEnabled, lockMode, lockDays} or null);
  // onGuardClick(bucket, source) opens the admin panel from those icons;
  // onBuckets(source, names) is called when bucket rows appear so the caller
  // can fetch their guards in the background.
  // onFilterStatus({walking, scanning}) reports the deep-walk state so the
  // side footer can show progress and a Stop control; onRender() fires
  // after every render so the footer can recount the rows it just drew.
  constructor({ onNavigate, onDropTo, onContext, guardOf, onGuardClick, onBuckets, onFilterStatus, onRender, onTypeahead }) {
    this.container = document.getElementById('tree');
    this.onNavigate = onNavigate;
    this.onDropTo = onDropTo;
    this.onContext = onContext;
    this.guardOf = guardOf || (() => null);
    this.onGuardClick = onGuardClick;
    this.onBuckets = onBuckets;
    this.onFilterStatus = onFilterStatus;
    this.onRender = onRender;
    this.onTypeahead = onTypeahead;
    this.nodes = new Map(); // id -> node
    this.currentId = null;
    this.status = new Map(); // source name -> 'ok' | 'error' | 'busy'
    // tree filter state: a matcher over row labels (null = unfiltered),
    // the raw query for the no-match note, the ids the user collapsed
    // while filtered, the deep-walk opt-in, the generation token that
    // aborts an in-flight walk, and what a render may draw.
    this.filter = null;
    this.filterQuery = '';
    this.filterCollapsed = new Set();
    this.deep = false;
    this.walkGen = 0;
    this.walkActive = false; // a walk is in flight (dedupes re-apply)
    this.walkingLabel = null; // source being scanned, for the status line
    this.visibleSet = null;
    this.visibleCount = 0;
    this.loading = new Set(); // node ids with a listing in flight
    // keyboard walk state: walkId is the walked node (the tree’s
    // focusKey — the seat the arrows move, named through the
    // container’s aria-activedescendant), plus the type-ahead buffer
    // every listing face owns
    this.walkId = null;
    this.taBuf = '';
    this.taTimer = 0;
    // the rendered window over the flattened order (this.order): the
    // growth roads widen it, a fresh render re-arms it
    this.order = [];
    this.win = 0;
    // the tree is a keyboard surface: the container is the focus seat
    // (tabindex in the markup) and owns the walk grammar below — the
    // listener sits under the document chain and stops every key it
    // handles, so the grid walk never moves under a tree seat
    this.container.addEventListener('keydown', (e) => this.keydown(e));
    this.container.addEventListener('focusin', () => this.ensureSeat());
    // the windowed tail (.t-window): when the flattened order outgrows
    // the rendered window the strip under the tree speaks the whole
    // truth — how many rows are shown, how many exist — and carries the
    // verb that grows the seat. It sits OUTSIDE the tree (a real button
    // is not a treeitem), a sibling under the scrolling container.
    this.stripText = el('span', { class: 't-window-text' });
    this.stripBtn = el('button', { class: 'btn', type: 'button' });
    this.stripBtn.addEventListener('click', () => this.ensure(this.win + TREE_PAGE));
    this.strip = el('div', { class: 't-window hidden' }, this.stripText, this.stripBtn);
    this.container.after(this.strip);
    // the scroll seam: the last seated row is the sentinel — when the
    // scroll brings it near, the window grows a page, landing before the
    // eye arrives. An IntersectionObserver speaks on every registration
    // too (the initial non-intersecting report still invokes the
    // callback), so only a true crossing grows.
    this.growIo = new IntersectionObserver((ents) => {
      if (!ents.some((en) => en.isIntersecting)) return;
      this.ensure(this.win + TREE_PAGE);
    }, { root: this.container, rootMargin: '800px 0px' });
  }

  nodeKey(source, bucket, prefix) { return `bkt:${source}:${bucket}:${prefix}`; }
  srcKey(name) { return `src:${name}`; }
  rsrcKey(name, path) { return `src:${name}:${path}`; }

  // setStatus feeds the per-source connectivity balls ('ok' | 'error' |
  // 'busy'); a partial map keeps the previous state of untouched sources.
  setStatus(map) {
    // merge — a single-source update must not wipe every other ball —
    // and render only when a ball actually moved: landings green the
    // view's source on every listing, and a needless tree re-render per
    // navigation is pure churn
    const merged = { ...Object.fromEntries(this.status), ...map };
    if (!Object.entries(merged).some(([k, v]) => this.status.get(k) !== v)) return;
    this.status = new Map(Object.entries(merged));
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
      n.readOnly = !!s.readOnly;
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
    this.walkGen++; // a changed source set invalidates any in-flight walk
    this.render(currentLoc);
    // re-expand previously-open sources lazily
    for (const [id, n] of keep) {
      if (n.kind === 'source' && n.expanded) this.expand(id);
    }
    // the filter itself survives a sources change; a deep filter re-walks
    if (this.filter && this.deep) this.startWalk();
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

  // loadChildren fills one node's children by kind (source roots dispatch
  // by type, S3 nodes stream their folder level, remote nodes list their
  // directory) without touching expansion state — expand(), the filter's
  // first-level preload and the deep walk all share it. The in-flight Set
  // keeps concurrent callers from listing the same node twice.
  async loadChildren(n) {
    if (n.loaded || this.loading.has(n.id)) return;
    this.loading.add(n.id);
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
    } finally {
      this.loading.delete(n.id);
    }
  }

  async expand(id) {
    const n = this.nodes.get(id);
    if (!n) return;
    n.expanded = true;
    this.filterCollapsed.delete(id); // a reveal under a filter must open
    if (!n.loaded) {
      try {
        await this.loadChildren(n);
      } catch (err) {
        n.expanded = false;
        console.error('tree expand failed', err);
      }
    }
    this.render();
  }

  // ========================= tree filter =========================
  // setFilter applies (matcher, query); a null matcher clears. The
  // instant pass narrows everything the tree already knows, and unloaded
  // SOURCE roots preload their first level in the background (expansion
  // state is never touched, so clearing the filter restores exactly what
  // the user had open). A changed query aborts any in-flight deep walk;
  // deep mode restarts it for the new pattern.
  setFilter(matcher, query) {
    const changed = query !== this.filterQuery;
    this.filter = matcher || null;
    this.filterQuery = query || '';
    this.filterCollapsed.clear();
    if (changed) this.abortWalk();
    // the deep opt-in lives and dies with the pattern: the checkbox is
    // gone, so leaving deep armed after a clear would walk every source
    // again on the next pattern with nothing on screen saying so
    this.deep = this.deep && !!this.filter;
    if (this.filter) {
      for (const n of this.nodes.values()) {
        if (n.kind === 'source' && !n.loaded) {
          this.loadChildren(n).then(() => this.render()).catch(() => {});
        }
      }
      // an unchanged query re-applied (the input's debounce landing after
      // Enter) must not restart a walk that is already in flight
      if (this.deep && !this.walkActive) this.startWalk();
    }
    this.render();
  }

  // setDeep opts in or out of walking unloaded folders for the filter —
  // the no-match note's action opts in, the footer's Stop opts out (a
  // plain abort would leave deep armed and re-walk on the next pattern
  // with nothing on screen saying so; the re-render brings the note's
  // action back).
  setDeep(on) {
    this.deep = !!on;
    if (this.deep && this.filter && !this.walkActive) this.startWalk();
    else if (!this.deep) { this.abortWalk(); this.render(); }
  }

  // startWalk depth-first loads every folder of every source so the
  // filter reaches depths the tree never expanded. Sources walk in render
  // order, one node's listing at a time — gentle on remote engines and
  // easy to follow in the status line. walkGen aborts the walk on filter
  // change, clear, toggle-off or a sources change; children are re-read
  // every iteration because a listing elsewhere can swap the array
  // mid-walk.
  async startWalk() {
    const gen = ++this.walkGen;
    this.walkActive = true;
    const roots = [...this.nodes.values()].filter((n) => n.kind === 'source')
      .sort((a, b) => (a.label.toLowerCase() < b.label.toLowerCase() ? -1 : 1));
    for (const r of roots) {
      if (gen !== this.walkGen) return;
      this.walkingLabel = r.label;
      this.onFilterStatus?.({ walking: true, scanning: r.label });
      await this.walkNode(r, gen);
    }
    if (gen === this.walkGen) {
      this.walkActive = false;
      this.walkingLabel = null;
      this.onFilterStatus?.({ walking: false });
    }
  }

  async walkNode(n, gen) {
    if (gen !== this.walkGen) return;
    if (!n.loaded) {
      try {
        await this.loadChildren(n);
      } catch {
        return; // an unreadable folder skips its subtree; the walk goes on
      }
      if (gen !== this.walkGen) return;
      this.render();
    }
    for (let i = 0; i < n.children.length; i++) {
      if (gen !== this.walkGen) return;
      await this.walkNode(n.children[i], gen);
    }
  }

  abortWalk() {
    this.walkGen++;
    this.walkActive = false;
    this.walkingLabel = null;
    this.onFilterStatus?.({ walking: false });
  }

  // computeVisibility marks the nodes a render may draw: a row stays when
  // its label matches or any LOADED child stays — unloaded folders cannot
  // participate yet, which is exactly what the deep walk adds.
  computeVisibility() {
    const vis = new Set();
    const visit = (n) => {
      let v = this.filter ? !!this.filter(n.label) : true;
      for (const c of n.children) if (visit(c)) v = true;
      if (v) vis.add(n.id);
      return v;
    };
    // every node hangs off a source root — visiting the roots covers all
    for (const n of this.nodes.values()) if (n.kind === 'source') visit(n);
    return vis;
  }

  // buildS3Children loads the folder level of a bucket/prefix node through
  // the source-pinned streaming listing (memory stays at one page, §13).
  buildS3Children(n) {
    return this.listDirs(n.source, n.bucket, n.prefix).then((dirs) => {
      this.setS3Children(n, dirs);
    });
  }

  // setS3Children (re)builds the folder rows of an S3 node from a dir
  // listing, preserving surviving child nodes (and their expansion
  // state) by id so a refresh never collapses what the user opened.
  setS3Children(n, dirs) {
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

  // updateObjectsDir is the S3 twin of updateRemoteDir: it feeds a
  // freshly listed bucket folder view into its tree node, so the tree
  // tracks S3 mutations (new folder, delete, rename) with the grid.
  // Bucket-scoped sources use the source node itself as the bucket
  // root (reveal()'s rule); folders resolve through the namespaced
  // bucket/prefix node id.
  updateObjectsDir(source, bucket, prefix, dirs) {
    let id = this.nodeKey(source, bucket, prefix);
    if (!prefix) {
      const s = this.nodes.get(this.srcKey(source));
      if (s && s.stype === 's3' && s.bucket === bucket) id = s.id;
    }
    const n = this.nodes.get(id);
    if (!n) return;
    // Skip the rebuild when the folder set is unchanged: silent
    // auto-refresh ticks land here constantly, and re-rendering the
    // whole tree on every one would churn the sidebar DOM (and detach
    // anything mid-click) for zero visible difference.
    const want = dirs.map((d) => this.nodeKey(n.source, n.bucket, d.key));
    const have = n.loaded ? n.children.map((c) => c.id) : null;
    if (have && want.length === have.length && want.every((k, i) => k === have[i])) return;
    this.setS3Children(n, dirs);
    n.expanded = true;
    this.render();
  }

  // listDirs streams one source-pinned prefix's folders through the
  // dirs-only walk (ListSourceDirsStream): the wire speaks folders alone,
  // so a million-object folder ships the tree no object row.
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
        () => api.ListSourceDirsStream(source, bucket, prefix),
        (p) => {
          if (p.error) { finish(reject, new Error(p.error)); return; }
          for (const e of p.entries || []) dirs.push(e); // dirs-only wire: every entry is a folder
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

  // reloadParentOf re-lists one child node's parent after a sidebar
  // mutation (context Delete): the parent's folder rows rebuild without
  // the deleted child. The sidebar only self-updates when the main view
  // lists a folder (updateObjectsDir/updateRemoteDir) — that is why grid
  // deletes refreshed the tree but sidebar deletes left a stale row.
  // Parent ids follow the tree's own rules: rdir paths climb one segment
  // (the source node at the root); S3 prefixes drop their last segment
  // (a bucket-scoped source node IS the bucket root).
  reloadParentOf(node) {
    let id;
    if (node.kind === 'rdir') {
      const up = node.path.replace(/\/+$/, '').replace(/\/[^/]*$/, '');
      id = up ? this.rsrcKey(node.source, up + '/') : this.srcKey(node.source);
    } else {
      const parent = String(node.prefix || '').replace(/[^/]*\/$/, '');
      id = this.nodeKey(node.source, node.bucket, parent);
      if (!parent) {
        const s = this.nodes.get(this.srcKey(node.source));
        if (s && s.stype === 's3' && s.bucket === node.bucket) id = s.id;
      }
    }
    const n = this.nodes.get(id);
    if (n && n.loaded) this.reload(id);
  }

  collapseAll() {
    for (const n of this.nodes.values()) n.expanded = false;
    // under a filter the effective expansion ignores n.expanded, so the
    // collapse lands on the filtered view's own override
    if (this.filter) for (const n of this.nodes.values()) this.filterCollapsed.add(n.id);
    this.render();
  }

  // ========================= the keyboard walk =========================
  // The tree answers the keyboard the way every listing face does: the
  // container is one focus seat (tabindex in the markup) and the walk
  // moves a painted row — never per-row DOM focus — with the
  // container naming the walked row through aria-activedescendant so
  // the reader speaks the seat. The grammar is the tree’s own
  // hierarchy: Down and Up stride the flat visible order, ArrowRight
  // expands a collapsed folder and walks into an open one, ArrowLeft
  // folds an open folder and climbs to the parent of a collapsed one,
  // Enter navigates the row the way a click does, Space toggles
  // expansion, the Menu key (Shift+F10) opens the row’s verb menu
  // with the row itself as the anchor, and the type-ahead buffer
  // jumps by label prefix — the same law every face owns.

  visibleRows() {
    return [...this.container.children].filter((r) => r.classList.contains('tnode'));
  }

  walkRow() {
    return this.visibleRows().find((r) => r.dataset.tid === this.walkId) || null;
  }

  // ensureSeat seats the walk when it has no rendered row: the current
  // row first (focus follows navigation), else the first visible row
  ensureSeat() {
    if (this.walkRow()) return;
    const rows = this.visibleRows();
    if (!rows.length) return;
    const cn = this.nodes.get(this.currentId);
    const ci = cn ? this.order.indexOf(cn) : -1;
    if (ci >= this.win) { this.ensure(ci + 1); this.seat(this.visibleRows()[ci]); return; }
    this.seat(rows.find((r) => r.dataset.tid === this.currentId) || rows[0]);
  }

  seat(row) {
    const prev = this.walkRow();
    if (prev) prev.classList.remove('focus');
    this.walkId = row.dataset.tid;
    row.classList.add('focus');
    this.container.setAttribute('aria-activedescendant', row.id);
    row.scrollIntoView({ block: 'nearest' });
  }

  // typeFeed folds one typed letter into the buffer (600ms past the
  // last key, case-folded, prefix on the label) and jumps to the next
  // visible match from the seat — a miss holds the seat and keeps
  // the keys for the next letter
  typeFeed(ch, from) {
    this.taBuf = (this.taBuf + ch).toLowerCase().slice(-64);
    clearTimeout(this.taTimer);
    this.taTimer = setTimeout(() => {
      this.taBuf = '';
      this.onTypeahead?.('');
    }, 600);
    // the buffer walks the whole order — a match past the window grows
    // the seat it names (the seated rows are the order’s own prefix, so
    // one index law serves both)
    const labels = this.order.map((nd) => nd.label.toLowerCase());
    for (let k = 1; k <= labels.length; k += 1) {
      const j = (from + k) % labels.length;
      if (labels[j].startsWith(this.taBuf)) {
        this.ensure(j + 1);
        this.seat(this.visibleRows()[j]);
        break;
      }
    }
    this.onTypeahead?.(this.taBuf);
  }

  keydown(e) {
    // modified keys are the app’s own (the Alt+arrow history walk
    // rides the document chain above this listener) — pass them through
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    const rows = this.visibleRows();
    if (!rows.length) return;
    this.ensureSeat();
    const row = this.walkRow();
    if (!row) return;
    const i = rows.indexOf(row);
    const n = this.nodes.get(row.dataset.tid);
    if (!n) return;
    const filtered = !!this.visibleSet;
    const open = filtered ? !this.filterCollapsed.has(n.id) : n.expanded;
    const hasKids = n.loaded && n.children.length > 0;
    const expandable = hasKids || open; // the twist-visibility law
    // ...but the keyboard keeps the pointer’s own expand roads: a click
    // reveals (loads) and a double-click expands an unloaded folder, so
    // ArrowRight owes the same parity — gated on the kinds loadChildren
    // can fill (the menu-wiring predicate)
    const loadable = n.bucket !== undefined || n.kind === 'rdir' || n.kind === 'source';
    const toggle = (wantOpen) => {
      // the twist click’s own body, routed from the keyboard
      if (filtered) {
        if (wantOpen) {
          this.filterCollapsed.delete(n.id);
          if (!n.loaded) this.loadChildren(n).then(() => this.render()).catch(() => {});
        } else this.filterCollapsed.add(n.id);
        this.render();
      } else if (wantOpen) this.expand(n.id);
      else this.collapse(n.id);
    };
    let handled = true;
    switch (e.key) {
      case 'ArrowDown': {
        if (i < rows.length - 1) this.seat(rows[i + 1]);
        // a walk off the seated edge grows the window to the row it names
        else if (rows.length < this.order.length) {
          this.ensure(i + 2);
          this.seat(this.visibleRows()[i + 1]);
        }
        break;
      }
      case 'ArrowUp': if (i > 0) this.seat(rows[i - 1]); break;
      case 'Home': this.seat(rows[0]); break;
      case 'End': {
        this.ensure(this.order.length); // the far end seats the whole order
        const rs = this.visibleRows();
        this.seat(rs[rs.length - 1]);
        break;
      }
      case 'ArrowRight': {
        if (!expandable && !loadable) break; // a leaf keeps its seat — the key is still consumed
        if (!open) toggle(true);
        else {
          this.ensure(i + 2); // the first child past the window grows the seat
          const nx = this.visibleRows()[i + 1];
          const nn = nx && this.nodes.get(nx.dataset.tid);
          if (nn && nn.level > n.level) this.seat(nx); // the first child
        }
        break;
      }
      case 'ArrowLeft': {
        if (open) { toggle(false); break; }
        // climb: the nearest preceding row shallower than this one
        let p = i - 1;
        while (p >= 0) {
          const pn = this.nodes.get(rows[p].dataset.tid);
          if (pn && pn.level < n.level) { this.seat(rows[p]); break; }
          p--;
        }
        break;
      }
      case 'Enter': this.navigate(n); break;
      case ' ': case 'Spacebar': if (expandable || loadable) toggle(!open); break;
      case 'ContextMenu':
      case 'F10': {
        if (e.key === 'F10' && !e.shiftKey) { handled = false; break; }
        // the verb menu the right-click opens, the row itself the
        // anchor (openMenu seats an element by its rect — the menu
        // lands beside the row because there is no pointer there)
        if (n.bucket !== undefined || n.kind === 'rdir' || n.kind === 'source') {
          this.onContext?.(row, n);
        }
        break;
      }
      default:
        if (e.key.length === 1) this.typeFeed(e.key, i);
        else handled = false;
    }
    if (handled) { e.preventDefault(); e.stopPropagation(); }
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
    // navigation IS a focus move: the walk seat follows the current
    // row through every road that seats it (a click, Enter, a reveal
    // from the grid) — one choke point, the naming law the grids own
    this.walkId = this.currentId;
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
    // the flattened order every law shares (the walk, the window, the
    // footer’s truth): roots sorted, depth-first through open nodes —
    // under a filter only visible rows and their subtrees survive (the
    // same gate renderNode seats, mirrored here so the order can never
    // drift from the rows)
    const order = [];
    const flat = (n) => {
      if (this.visibleSet && !this.visibleSet.has(n.id)) return;
      order.push(n);
      const open = this.visibleSet ? !this.filterCollapsed.has(n.id) : n.expanded;
      if (open) for (const c of n.children) flat(c);
    };
    if (this.filter) {
      this.visibleSet = this.computeVisibility();
      for (const r of roots) flat(r);
      if (!order.length) {
        const note = el('div', {
          class: 'tree-empty-note',
          text: t('treeFilterNone', { q: this.filterQuery }),
        });
        // zero matches in everything loaded: offer the one search that can
        // reach further — the walk opt-in surfaces only when it can pay off
        if (!this.deep) {
          note.appendChild(el('button', {
            class: 'tree-note-act', type: 'button', text: t('treeFilterDeep'),
            onclick: () => this.setDeep(true),
          }));
        }
        this.container.appendChild(note);
      }
    } else {
      this.visibleSet = null;
      for (const r of roots) flat(r);
    }
    this.order = order;
    this.visibleCount = order.length; // the footer speaks the truth, never the seat
    // a fresh render re-arms the window — a viewport, not state — but the
    // current row always seats: navigation IS a focus move, and the
    // container cannot name a row that does not exist
    this.win = Math.min(order.length, TREE_WINDOW);
    const cur = this.nodes.get(this.currentId);
    const ci = cur ? order.indexOf(cur) : -1;
    if (ci >= this.win) this.win = Math.min(order.length, Math.ceil((ci + 1) / TREE_PAGE) * TREE_PAGE);
    for (let i = 0; i < this.win; i += 1) this.container.appendChild(this.renderNode(order[i]));
    this.syncWindow();
    // the container names the walked row the way every listing body
    // does — the reader speaks the seat the walk or the navigation
    // last seated
    const wr = this.walkRow();
    if (wr) this.container.setAttribute('aria-activedescendant', wr.id);
    else this.container.removeAttribute('aria-activedescendant');
    this.onRender?.();
  }

  // ensure seats at least n rows of the flattened order, append-only:
  // the growth roads (the scroll seam, the walk past the edge, the
  // strip’s own verb) widen the window without a re-render — a wider
  // window is not a model change — rounding up to a whole page so a far
  // walk lands on a page boundary.
  ensure(n) {
    if (n <= this.win) return;
    const target = Math.min(this.order.length, Math.ceil(n / TREE_PAGE) * TREE_PAGE);
    if (target <= this.win) return;
    for (let i = this.win; i < target; i += 1) this.container.appendChild(this.renderNode(this.order[i]));
    this.win = target;
    this.syncWindow();
  }

  // syncWindow speaks the window’s own truth: hidden while everything
  // fits — a small world renders whole, exactly the old law — naming
  // what is shown of what exists the moment it does not, and keeping
  // the seam’s sentinel honest (the last seated row).
  syncWindow() {
    this.growIo.disconnect();
    if (this.win >= this.order.length) {
      this.strip.classList.add('hidden');
      return;
    }
    this.stripText.textContent = t('treeShown', { shown: this.win, total: this.order.length });
    this.stripBtn.textContent = t('treeMore');
    this.strip.classList.remove('hidden');
    this.growIo.observe(this.container.lastElementChild);
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
    // under a filter an invisible row is simply not rendered; a visible
    // row that only carries a matching descendant renders dimmed, and the
    // subtree reads as expanded so matches at depth surface without
    // touching the user's collapse state (filterCollapsed remembers the
    // manual collapses made while filtered)
    if (this.visibleSet && !this.visibleSet.has(n.id)) return document.createDocumentFragment();
    const filtered = !!this.visibleSet;
    const open = filtered ? !this.filterCollapsed.has(n.id) : n.expanded;
    const hasKids = n.loaded && n.children.length > 0;
    const twist = el('span', {
      class: 'twist',
      text: open ? '\u25BC' : '\u25B6',
      onclick: (e) => {
        e.stopPropagation();
        if (filtered) {
          if (open) {
            this.filterCollapsed.add(n.id);
          } else {
            this.filterCollapsed.delete(n.id);
            // expanding an unloaded folder under a filter still fetches it
            if (!n.loaded) this.loadChildren(n).then(() => this.render()).catch(() => {});
          }
          this.render();
        } else {
          n.expanded ? this.collapse(n.id) : this.expand(n.id);
        }
      },
    });
    n.twistEl = twist;
    if (!hasKids && !open) twist.style.visibility = 'hidden';
    const ticon = el('span', { class: 'ticon' });
    if (n.kind === 'source') {
      // the tinted type chip painted in the source's own accent color —
      // the same badge the breadcrumb's root crumb and every picker carry
      ticon.appendChild(srcIconEl(n.stype, n.color));
    } else ticon.textContent = n.kind === 'rdir' ? '\u{1F4C1}' : n.prefix === '' ? '\u{1F5C0}' : '\u{1F4C1}';
    const row = el('div', {
      class: `tnode${this.currentId === n.id ? ' sel' : ''}${n.id === this.walkId ? ' focus' : ''}${filtered && !this.filter(n.label) ? ' dim' : ''}`,
      style: `padding-left:${8 + n.level * 14}px`,
      // the tree is a flat DOM (depth is padding), so the treeitems
      // carry their own level and only expandable nodes claim expansion
      role: 'treeitem',
      'aria-level': String(n.level + 1),
      'aria-expanded': hasKids || open ? (open ? 'true' : 'false') : undefined,
      title: n.kind === 'source'
        ? (n.bucket !== undefined ? `${n.label} (s3 — bucket ${n.bucket})` : `${n.label} (${n.stype})`)
        : undefined,
      onclick: () => this.navigate(n),
      ondblclick: () => (n.expanded ? this.collapse(n.id) : this.expand(n.id)),
    },
      twist,
      ticon,
      el('span', { class: 'tlabel', text: n.label }),
      // a read-only source wears its lock right after the name — the
      // same law the verbs honor (every write refuses, Go-side too)
      ...(n.kind === 'source' && n.readOnly ? [el('span', { class: 't-ro', title: 'Read-only', text: '🔒' })] : []),
      // source rows carry their connectivity ball right after the name
      ...(n.kind === 'source' ? [this.statusBall(n.source)] : []),
      // bucket rows carry their versioning / lock state right after the name
      ...(n.bucket !== undefined && n.prefix === '' ? this.guardIcons(n.bucket, n.source) : []),
    );
    // Node identity for DOM-level hit-testing (the OS file-drop handler in
    // main.js resolves the node under the cursor from these). Attributes
    // are set only when defined — dataset assignment stringifies
    // undefined into "undefined" otherwise.
    row.id = `tn-${n.id}`; // the activedescendant idref — id-keyed, stable across renders
    row.dataset.tid = n.id; // the keyboard walk’s row→node map
    row.dataset.tkind = n.kind;
    if (n.bucket !== undefined) row.dataset.bucket = n.bucket;
    if (n.prefix !== undefined) row.dataset.prefix = n.prefix;
    if (n.source) row.dataset.source = n.source;
    if (n.stype) row.dataset.stype = n.stype;
    if (n.kind === 'rdir') row.dataset.rdir = n.path || '/';

    // S3 nodes and remote directory nodes are both drop targets and carry
    // full context menus (the cross-source matrix treats them uniformly).
    // wireMenu wires the full context menu; wireDrop additionally makes
    // the row a drop target for internal s3b drags, handing the decoded
    // payload to onDropTo with the row's target descriptor (an S3
    // bucket/prefix, a remote directory, or a remote source root).
    const wireMenu = () => row.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      e.stopPropagation();
      this.onContext?.(e, n);
    });
    const wireDrop = (target) => {
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
        this.onDropTo(target, JSON.parse(data), e);
      });
      wireMenu();
    };
    if (n.bucket !== undefined) {
      wireDrop({ kind: 's3', source: n.source, bucket: n.bucket, dir: n.prefix });
    } else if (n.kind === 'rdir') {
      wireDrop({ kind: 'remote', source: n.source, dir: n.path });
    } else if (n.kind === 'source') {
      // Source roots: full context menu; non-S3 roots also accept drops
      // into their root directory. Bucket-scoped S3 roots are drop targets
      // via the S3 branch above; legacy account-wide roots need a bucket
      // picked first — no drop.
      if (n.stype !== 's3') wireDrop({ kind: 'remote', source: n.source, dir: '/' });
      else wireMenu();
    } else {
      row.addEventListener('contextmenu', (e) => e.preventDefault());
    }

    n.el = row;
    // row-only: render() seats the flattened order (the window’s law),
    // so the recursion that once lived here is the flatten’s job now
    return row;
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
