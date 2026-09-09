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
export function onEvent(name, handler) {
  window.runtime?.EventsOn(name, handler);
}
