// Central command-state system: computes whether each toolbar/menu action is
// currently available and greys out the toolbar buttons accordingly.
// main.js injects the live sources (grid selection, profile presence) via
// setCommandContext; nav/clipboard come from state.js directly.
import { nav, parentOf, clipboard } from './state.js';

const $ = (id) => document.getElementById(id);

// Injected sources — defaults keep commandState() pure and side-effect free.
let ctx = {
  selectionCount: () => 0,
  hasProfile: () => false,
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
  const hasClipboard = clipboard.keys.length > 0;

  return {
    hasProfile,
    canBack: nav.canBack(),
    canForward: nav.canForward(),
    canUp: (inObjects || inRemote) && !!parentOf(loc),
    canUpload: inObjects && hasProfile,
    canDownload: inObjects && hasProfile && sel >= 1,
    canNewFolder: (inObjects && hasProfile) || inRemote,
    canCreateBucket: inBuckets && hasProfile,
    canDelete: ((inObjects && hasProfile) || inRemote) && sel >= 1,
    canRename: ((inObjects && hasProfile) || inRemote) && sel === 1,
    canCopy: inObjects && hasProfile && sel >= 1,
    canCut: inObjects && hasProfile && sel >= 1,
    canPaste: inObjects && hasProfile && hasClipboard,
    hasSelection: sel >= 1,
    selectionCount: sel,
    canFind: hasProfile,
    canDoctor: hasProfile,
  };
}

// Toolbar button id -> commandState flag. Refresh/theme/help/transfers/
// panes/profiles stay always-enabled and are not listed here.
const BUTTONS = {
  'btn-back': 'canBack',
  'btn-forward': 'canForward',
  'btn-up': 'canUp',
  'btn-upload': 'canUpload',
  'btn-download': 'canDownload',
  'btn-newfolder': 'canNewFolder',
  'btn-find': 'canFind',
  'btn-doctor': 'canDoctor',
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
