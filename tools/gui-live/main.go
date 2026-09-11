// gui-live is the live GUI harness backend: it runs the REAL pkg/api app
// (the exact code the desktop exe binds to Wails) behind an HTTP bridge a
// real browser can speak — the Wails binding protocol (window.go[...].App)
// over POST, backend events (list:page, transfer:update, log:line, …) over
// Server-Sent Events — and serves the real frontend/ with that bridge
// injected, so scripts/gui-live.mjs can walk the GUI against real S3, real
// files and real transfers. Wails' own runtime (native dialogs, in-app
// event bus) is unreachable outside wails.Run, so pkg/api exposes two
// harness seams instead: SetEventSink and SetProfileDialogs.
//
// Usage (scripts/gui-live.mjs does this for you):
//
//	go run ./tools/gui-live -frontend frontend \
//	  -open-profile testartifacts/gui-live/profile.s3bprofile \
//	  -save-profile testartifacts/gui-live/profile.s3bprofile
//
// Prints "gui-live http://127.0.0.1:PORT" on stdout, then serves until the
// process is killed. Dev-only; never shipped.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/api"
)

// bridgeJS is injected into index.html before the app's module script. It
// implements exactly the two surfaces the frontend touches: the Wails
// binding proxy (window.go["<import-path>"].App.Method → POST /__live/call)
// and the event runtime (window.runtime.EventsOn → SSE /__live/events).
const bridgeJS = `(function () {
  if (window.__liveBridge) return; window.__liveBridge = true;
  async function call(m, args) {
    const r = await fetch('/__live/call', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ m: m, args: args }),
    });
    let j = null;
    try { j = await r.json(); } catch (e) { /* fall through */ }
    if (!j || !j.ok) throw new Error((j && j.error) || (m + ' failed'));
    return j.result;
  }
  const app = new Proxy({}, { get: function (_, m) {
    return function () { return call(m, Array.prototype.slice.call(arguments)); };
  } });
  window.go = { 'github.com/MikkoP88/s3-bucket-browser/pkg/api': { App: app } };
  const handlers = {};
  const es = new EventSource('/__live/events');
  es.onmessage = function (e) {
    let msg = null;
    try { msg = JSON.parse(e.data); } catch (err) { return; }
    const list = handlers[msg.event] || [];
    for (let i = 0; i < list.length; i++) {
      try { list[i].apply(null, msg.data || []); } catch (err) { console.error(err); }
    }
  };
  window.runtime = {
    EventsOn: function (name, cb) {
      (handlers[name] = handlers[name] || []).push(cb);
      return function () {
        const l = handlers[name] || [];
        const i = l.indexOf(cb);
        if (i >= 0) l.splice(i, 1);
      };
    },
    EventsOff: function (name) { delete handlers[name]; },
    EventsOffAll: function () { for (const k of Object.keys(handlers)) delete handlers[k]; },
    EventsOnce: function (name, cb) {
      const off = window.runtime.EventsOn(name, function () { off(); cb.apply(null, arguments); });
      return off;
    },
    EventsEmit: function () { /* frontend→backend emits are not used */ },
  };
})();`

// ---------- event bus (SSE fan-out) ----------

type eventBus struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newEventBus() *eventBus {
	return &eventBus{subs: map[chan []byte]struct{}{}}
}

func (b *eventBus) push(event string, data ...any) {
	payload, err := json.Marshal(map[string]any{"event": event, "data": data})
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- payload:
		default: // a slow consumer must never stall the backend
		}
	}
}

func (b *eventBus) serve(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan []byte, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}()
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": hello\n\n")
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			fl.Flush()
		}
	}
}

// ---------- binding dispatch (reflection over the bound App) ----------

var errType = reflect.TypeOf((*error)(nil)).Elem()

type callRequest struct {
	M    string            `json:"m"`
	Args []json.RawMessage `json:"args"`
}

func dispatch(app any, req callRequest) (any, error) {
	mv := reflect.ValueOf(app).MethodByName(req.M)
	if !mv.IsValid() {
		return nil, fmt.Errorf("backend binding missing: %s", req.M)
	}
	t := mv.Type()
	fixed := t.NumIn()
	if t.IsVariadic() {
		fixed--
	}
	if len(req.Args) < fixed {
		return nil, fmt.Errorf("%s: want >= %d args, got %d", req.M, fixed, len(req.Args))
	}
	in := make([]reflect.Value, 0, len(req.Args))
	for i := 0; i < fixed; i++ {
		v, err := decodeArg(t.In(i), req.Args[i])
		if err != nil {
			return nil, fmt.Errorf("%s arg %d: %v", req.M, i, err)
		}
		in = append(in, v)
	}
	if t.IsVariadic() {
		elem := t.In(t.NumIn() - 1).Elem() // slice's element type
		rest := reflect.MakeSlice(reflect.SliceOf(elem), 0, len(req.Args)-fixed)
		for i := fixed; i < len(req.Args); i++ {
			v, err := decodeArg(elem, req.Args[i])
			if err != nil {
				return nil, fmt.Errorf("%s arg %d: %v", req.M, i, err)
			}
			rest = reflect.Append(rest, v)
		}
		in = append(in, rest)
	}
	out := mv.Call(in)
	// Wails bindings are (T, error) or error or T; treat a trailing error
	// accordingly and pass everything else through as JSON.
	if n := len(out); n > 0 && out[n-1].Type().Implements(errType) {
		if !out[n-1].IsNil() {
			return nil, out[n-1].Interface().(error)
		}
		out = out[:n-1]
	}
	switch len(out) {
	case 0:
		return nil, nil
	case 1:
		return out[0].Interface(), nil
	default:
		vals := make([]any, len(out))
		for i, v := range out {
			vals[i] = v.Interface()
		}
		return vals, nil
	}
}

func decodeArg(t reflect.Type, raw json.RawMessage) (reflect.Value, error) {
	v := reflect.New(t)
	if len(raw) == 0 || string(raw) == "null" {
		return v.Elem(), nil
	}
	if err := json.Unmarshal(raw, v.Interface()); err != nil {
		return reflect.Value{}, err
	}
	return v.Elem(), nil
}

// ---------- static frontend with the bridge injected ----------

type injectedFS struct {
	root string
}

func (s injectedFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(filepath.Clean(r.URL.Path), string(os.PathSeparator))
	if rel == "" || rel == "." {
		rel = "index.html"
	}
	file := filepath.Join(s.root, filepath.FromSlash(rel))
	if !strings.HasPrefix(filepath.Clean(file), filepath.Clean(s.root)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	data, err := os.ReadFile(file)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("cache-control", "no-store")
	if strings.HasSuffix(rel, ".html") {
		data = []byte(strings.Replace(string(data),
			`<script type="module" src="js/main.js"></script>`,
			`<script src="/__live/bridge.js"></script><script type="module" src="js/main.js"></script>`, 1))
		w.Header().Set("content-type", "text/html; charset=utf-8")
	} else {
		w.Header().Set("content-type", mimeOf(rel))
	}
	w.Write(data)
}

func mimeOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address (:0 = ephemeral)")
	frontend := flag.String("frontend", "frontend", "frontend/ directory to serve")
	openProfile := flag.String("open-profile", "", "scripted open-profile-file dialog answer")
	saveProfile := flag.String("save-profile", "", "scripted save-profile-file dialog answer")
	flag.Parse()

	bus := newEventBus()
	api.SetEventSink(bus.push)
	if *openProfile != "" || *saveProfile != "" {
		api.SetProfileDialogs(
			func() (string, error) { return *openProfile, nil },
			func(string) (string, error) { return *saveProfile, nil },
		)
	}

	app := api.New("0.0.0-gui-live")
	app.Startup(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /__live/bridge.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/javascript; charset=utf-8")
		w.Header().Set("cache-control", "no-store")
		fmt.Fprint(w, bridgeJS)
	})
	mux.HandleFunc("GET /__live/events", bus.serve)
	mux.HandleFunc("POST /__live/call", func(w http.ResponseWriter, r *http.Request) {
		var req callRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResp(w, map[string]any{"ok": false, "error": "bad request: " + err.Error()})
			return
		}
		result, err := dispatch(app, req)
		if err != nil {
			jsonResp(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonResp(w, map[string]any{"ok": true, "result": result})
	})
	mux.Handle("/", injectedFS{root: *frontend})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("gui-live http://%s\n", ln.Addr().String())
	go func() {
		if err := http.Serve(ln, mux); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	done := make(chan struct{})
	go func() { app.Shutdown(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func jsonResp(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(v)
}
