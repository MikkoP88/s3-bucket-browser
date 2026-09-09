// Thin wrapper over the generated Wails bindings (window.go.pkg.api.App).
// Every App method becomes api.<Method>(...args) returning a Promise.
const call = (method, ...args) => {
  const fn = window.go?.pkg?.api?.App?.[method];
  if (!fn) return Promise.reject(new Error(`backend binding missing: ${method}`));
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
