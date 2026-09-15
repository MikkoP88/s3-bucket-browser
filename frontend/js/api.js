// Thin wrapper over the generated Wails bindings.
// Wails exposes bound methods at window.go[<package>].App.<Method>, where
// <package> is the struct's full import path ("github.com/.../pkg/api"),
// a module-relative path ("pkg.api") or "main" depending on the Wails
// version — so resolve the App struct once by probing window.go.
let appObj = null;
const resolveApp = () => {
  if (appObj) return appObj;
  const go = window.go;
  if (go) {
    for (const pkg of Object.keys(go)) {
      const app = go[pkg]?.App;
      if (app && typeof app.GetVersion === 'function') {
        appObj = app;
        return app;
      }
    }
  }
  return null;
};

const call = (method, ...args) => {
  const fn = resolveApp()?.[method];
  if (typeof fn !== 'function') return Promise.reject(new Error(`backend binding missing: ${method}`));
  return fn(...args);
};

export const api = new Proxy({}, {
  get: (_, method) => (...args) => call(method, ...args),
});

// Subscribe to a backend event (window.runtime is injected by Wails).
// Returns an unsubscribe function for this single handler — Wails >= 2.5
// EventsOn already returns one; fall back to a no-op outside the app
// (devtools, checks).
export function onEvent(name, handler) {
  const off = window.runtime?.EventsOn(name, handler);
  return typeof off === 'function' ? off : () => {};
}

// subscribeStream pairs a List*Stream binding call with a list:page
// subscription that exists BEFORE the call resolves. The backend goroutine
// starts listing the moment the call returns server-side; on small folders
// the single done page can beat the call response, and an event dispatched
// before the subscription exists would be lost forever (the view would
// never complete). Pages arriving before the token is known are buffered
// and replayed by flush(), which the caller runs once it has committed its
// own navigation state; pages of other tokens (concurrent streams, canceled
// predecessors) never reach `handle`.
export function subscribeStream(start, handle) {
  let token = null;
  const early = [];
  const off = onEvent('list:page', (p) => {
    if (token === null) { early.push(p); return; }
    if (p.token === token) handle(p);
  });
  return {
    off,
    begin: start().then((t) => (token = t), (e) => { off(); throw e; }),
    flush: () => { for (const p of early.splice(0)) if (p.token === token) handle(p); },
  };
}
