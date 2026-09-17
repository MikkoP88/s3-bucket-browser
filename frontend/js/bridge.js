// Wails v3 frontend bridge: v2 surface on top of the v3 runtime.
//
// Wails v3 speaks a different frontend dialect than this app was written
// against: bound methods are called through window.wails.Call.ByName with
// '<import path>.<Type>.<Method>' names, event listeners receive a single
// WailsEvent object (v2 spread the payloads as positional arguments), and
// the runtime is a plain script the page loads itself (/wails/runtime.js,
// see index.html) instead of an injected window.go/window.runtime pair.
//
// This module restores the v2 surface so the rest of the frontend stays
// untouched:
//   window.go['github.com/MikkoP88/s3-bucket-browser/pkg/api'].App.M(...)
//     -> window.wails.Call.ByName('<import path>.App.M', ...)
//   window.runtime.EventsOn(name, cb) -> window.wails.Events.On with a
//     payload adapter: the callback gets the event data; multi-payload Go
//     emits arrive as arrays and are spread into positional arguments (the
//     only multi-payload event is the file drop: x, y, paths).
//
// It needs no readiness handshake of its own: the v3 runtime installs
// window._wails.invoke and announces "wails:runtime:ready" itself at load
// (module scope of /wails/runtime.js), which starts backend→frontend event
// delivery in every context — desktop webview and server-mode browser alike.
//
// The test harnesses (gui-visual's addInitScript shim, gui-live's injected
// bridge) install window.go/window.runtime before any page script runs; this
// module yields to them untouched. Runs before main.js (script order in
// index.html), so the surface is in place without anyone awaiting anything.

const PKG = 'github.com/MikkoP88/s3-bucket-browser/pkg/api';

if (!window.go && window.wails?.Call?.ByName && window.wails?.Events?.On) {
  window.go = {
    [PKG]: {
      App: new Proxy({}, {
        get: (_, method) => (...args) => window.wails.Call.ByName(`${PKG}.App.${method}`, ...args),
      }),
    },
  };
  window.runtime = {
    // v2 EventsOn spread the event payloads as positional arguments; v3
    // hands the listener one WailsEvent whose .data is the single payload
    // (or an array for multi-payload emits). Both shapes become v2-style
    // positional callbacks. Returns the unsubscribe function.
    EventsOn: (name, cb) => window.wails.Events.On(name, (ev) => {
      const d = ev ? ev.data : undefined;
      cb(...(Array.isArray(d) ? d : [d]));
    }),
  };
}
