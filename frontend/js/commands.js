// Central command-state system: computes whether each toolbar/menu action is
// currently available and greys out the toolbar buttons accordingly.
// main.js injects the live sources (grid selection, profile presence) via
// setCommandContext; nav/clipboard come from state.js directly.
import { nav, parentOf, clipboard, clipHasItems } from './state.js';

const $ = (id) => document.getElementById(id);

// Injected sources — defaults keep commandState() pure and side-effect free.
let ctx = {
  selectionCount: () => 0,
  hasProfile: () => false,
  localSelectionCount: () => 0,
  localPaneOpen: () => false,
  osClipFiles: () => false, // Explorer files waiting on the OS clipboard
};

export function setCommandContext(sources) {
  ctx = { ...ctx, ...sources };
}

// commandState derives every action's availability from the real app state.
export function commandState() {
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
    canUp: (inObjects || inRemote) && !!parentOf(loc),
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
    canPaste: (hasClipboard || ctx.osClipFiles()) && ((inObjects && hasProfile) || inRemote || ctx.localPaneOpen()),
    hasSelection: sel >= 1,
    selectionCount: sel,
    canFind: hasProfile,
    canDoctor: hasProfile,
  };
}

// Toolbar button id -> commandState flag. Refresh/theme/help/panes stay
// always-enabled and are not listed here.
const BUTTONS = {
  'btn-back': 'canBack',
  'btn-forward': 'canForward',
  'btn-up': 'canUp',
  'btn-upload': 'canUpload',
  'btn-download': 'canDownload',
  'btn-newfolder': 'canNewFolder',
  'btn-find': 'canFind',
};

// updateCommandState recomputes the state, applies it to the toolbar buttons
// and stores it for the menu-bar slice to read.
export function updateCommandState() {
  const state = commandState();
  window.__s3bCmdState = state;
  for (const [id, flag] of Object.entries(BUTTONS)) {
    const btn = $(id);
    if (btn) btn.disabled = !state[flag];
  }
  return state;
}
