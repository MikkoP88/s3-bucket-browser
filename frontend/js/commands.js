// Central command-state system: computes whether each toolbar/menu action is
// currently available and greys out the toolbar buttons accordingly.
// main.js injects the live sources (grid selection, profile presence) via
// setCommandContext; nav/clipboard come from state.js directly. In
// dual-pane mode the same engine drives the secondary pane's toolbar from
// a pane adapter (SidePane.cmdAdapter) — commandState(pane) — so both
// toolbars grey out honestly, each from its own view.
import { nav, clipboard, clipHasItems } from './state.js';

const $ = (id) => document.getElementById(id);

// Injected sources — defaults keep commandState() pure and side-effect free.
let ctx = {
  selectionCount: () => 0,
  hasProfile: () => false,
  localSelectionCount: () => 0,
  localPaneOpen: () => false,
  osClipFiles: () => false, // Explorer files waiting on the OS clipboard
  paneAdapter: () => null,  // the secondary pane's view adapter, when open
};

export function setCommandContext(sources) {
  ctx = { ...ctx, ...sources };
}

// paneTarget: the secondary pane accepts transfers/pastes when its binding
// is a live directory — a remote anywhere, an S3 bucket (root included),
// a local directory (the roots view is not a target).
function paneTarget() {
  const p = ctx.paneAdapter?.();
  return !!(p && p.bound && (p.kind === 'remote'
    || (p.kind === 's3' && !!p.bucket)
    || (p.kind === 'local' && !!p.dir)));
}

// commandState derives every action's availability from the real app
// state. Without an argument it describes the main view; passed a pane
// adapter (SidePane.cmdAdapter()) it describes the secondary pane — the
// pane's own location, history and selection, never the main view's.
export function commandState(pane = null) {
  if (pane) {
    const bound = !!pane.bound;
    const inObjects = pane.kind === 's3' && !!pane.bucket; // inside a bucket, root included
    const inRemote = pane.kind === 'remote';
    const inLocalDir = pane.kind === 'local' && !!pane.dir; // roots view is not a target
    const sel = pane.selCount();
    // source-pinned transfers carry their own credentials — no S3 profile
    // gate on the pane; local New folder/File rest (no local mkdir/create
    // APIs), like the pane's context menus already do.
    const transferable = bound && (inObjects || inRemote || inLocalDir);
    return {
      canBack: bound && pane.canBack(),
      canForward: bound && pane.canForward(),
      canUpload: transferable,
      // Download pulls from a remote store — the local binding already IS
      // the workstation, so its Download rests (uploads still land there)
      canDownload: bound && (inObjects || inRemote) && sel >= 1,
      canNewFolder: bound && (inObjects || inRemote),
      canNewFile: bound && (inObjects || inRemote),
      // Search scopes to the pane's own binding — a whole pane-content run,
      // local workstation FS included — so it rides on bound, not the S3
      // profile gate the main view's copy answers to
      canFind: bound,
    };
  }
  const loc = nav.current;
  const inObjects = loc?.kind === 'objects';
  const inBuckets = loc?.kind === 'buckets';
  const inRemote = loc?.kind === 'remote'; // remote-native ops need no S3 profile
  const hasProfile = ctx.hasProfile();
  const sel = ctx.selectionCount();
  const localSel = ctx.localSelectionCount();
  const hasClipboard = clipHasItems();

  return {
    hasProfile,
    canBack: nav.canBack(),
    canForward: nav.canForward(),
    canUpload: (inObjects && hasProfile) || inRemote,
    canDownload: ((inObjects && hasProfile) || inRemote) && sel >= 1,
    canNewFolder: (inObjects && hasProfile) || inRemote,
    canCreateBucket: inBuckets && hasProfile,
    canDelete: ((inObjects && hasProfile) || inRemote) && sel >= 1,
    canRename: ((inObjects && hasProfile) || inRemote) && sel === 1,
    canCopy: (((inObjects && hasProfile) || inRemote) && sel >= 1) || localSel >= 1,
    canCut: (((inObjects && hasProfile) || inRemote) && sel >= 1) || localSel >= 1,
    // Paste acts on the app clipboard OR Explorer files waiting on the OS
    // clipboard (Ctrl+C in Explorer — see main.js osClipPayload).
    canPaste: (hasClipboard || ctx.osClipFiles()) && ((inObjects && hasProfile) || inRemote || paneTarget()),
    // Compare needs the secondary pane to be open on something
    canCompare: ctx.localPaneOpen() && !!ctx.paneAdapter?.()?.bound,
    hasSelection: sel >= 1,
    selectionCount: sel,
    canFind: hasProfile,
    canDoctor: hasProfile,
  };
}

// Toolbar button id -> commandState flag, per toolbar. Refresh/theme/help
// and the dual-pane toggle stay always-enabled and are not listed here.
const BUTTONS = {
  'btn-back': 'canBack',
  'btn-forward': 'canForward',
  'btn-upload': 'canUpload',
  'btn-download': 'canDownload',
  'btn-newfolder': 'canNewFolder',
  'btn-find': 'canFind',
  'btn-compare': 'canCompare',
};

// The secondary pane's mirror set — its own back/forward/upload/download/
// new-folder/new-file/find. The window-scope copies it used to carry
// (Compare, theme, help, the Dual-pane toggle) live in the global bar now;
// its close stays always-enabled.
const PANE_BUTTONS = {
  'local-btn-back': 'canBack',
  'local-btn-forward': 'canForward',
  'local-btn-upload': 'canUpload',
  'local-btn-download': 'canDownload',
  'local-btn-newfolder': 'canNewFolder',
  'local-btn-newfile': 'canNewFile',
  'local-btn-find': 'canFind',
};

function applyButtons(map, state) {
  for (const [id, flag] of Object.entries(map)) {
    const btn = $(id);
    if (btn) btn.disabled = !state[flag];
  }
}

// updateCommandState recomputes both toolbars' states — the main view's
// (stored for the menu-bar slice to read) and, when the pane is open,
// the pane adapter's — and applies them to the buttons.
export function updateCommandState() {
  const state = commandState();
  window.__s3bCmdState = state;
  applyButtons(BUTTONS, state);
  const pane = ctx.paneAdapter?.() || null;
  applyButtons(PANE_BUTTONS, pane ? commandState(pane) : {});
  return state;
}
