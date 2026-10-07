package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"golang.org/x/net/webdav"
)

// davTestServer mounts a real WebDAV server (the x/net handler over a temp
// dir) at prefix ("" = root) with optional Basic auth, and returns it plus
// the matching source for DialWebDAV.
func davTestServer(t *testing.T, prefix, user, pass string) (*httptest.Server, profile.Source) {
	t.Helper()
	dir := t.TempDir()
	h := &webdav.Handler{
		FileSystem: webdav.Dir(dir),
		LockSystem: webdav.NewMemLS(),
		Prefix:     prefix, // canonical prefix mounting: strips it from both
		// Request-URIs and Destination headers, and re-adds it to hrefs
	}
	mux := http.NewServeMux()
	if prefix == "" {
		mux.Handle("/", h)
	} else {
		mux.Handle(prefix+"/", h)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != pass {
				w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
				http.Error(w, "401 unauthorized", http.StatusUnauthorized)
				return
			}
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	host, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
	src := profile.Source{
		Name:     "dav",
		Type:     profile.TypeWebDAV,
		Host:     host,
		Username: user,
		Password: pass,
		Root:     prefix,
	}
	if n, err := strconv.Atoi(port); err == nil {
		src.Port = n
	}
	return srv, src
}

// TestWebDAVContract walks the whole FS contract against a live server:
// structure ops, listings, metadata, content, rename, recursive remove,
// not-exist mapping — the same guarantees the sftp/ftp suites pin.
func TestWebDAVContract(t *testing.T) {
	for _, prefix := range []string{"", "/dav"} {
		t.Run("prefix"+prefix, func(t *testing.T) {
			_, src := davTestServer(t, prefix, "", "")
			f, err := DialWebDAV(context.Background(), src)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer f.Close()
			ctx := context.Background()

			if err := f.MkdirAll(ctx, "/docs/2026/report"); err != nil {
				t.Fatalf("mkdirall: %v", err)
			}
			if err := f.Create(ctx, "/docs/2026/report/summary.txt", strings.NewReader("hello dav")); err != nil {
				t.Fatalf("create: %v", err)
			}
			// idempotent MkdirAll over existing segments
			if err := f.MkdirAll(ctx, "/docs/2026"); err != nil {
				t.Fatalf("mkdirall existing: %v", err)
			}

			// List: directory itself is not part of the view
			ents, err := f.List(ctx, "/docs/2026")
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(ents) != 1 || ents[0].Name != "report" || !ents[0].IsDir {
				t.Fatalf("list /docs/2026: want one dir 'report', got %+v", ents)
			}
			if ents[0].Key != "/docs/2026/report/" {
				t.Fatalf("dir key: got %q", ents[0].Key)
			}

			ents, err = f.List(ctx, "/docs/2026/report")
			if err != nil {
				t.Fatalf("list leaf: %v", err)
			}
			if len(ents) != 1 || ents[0].Name != "summary.txt" || ents[0].IsDir || ents[0].Size != 9 {
				t.Fatalf("list leaf: got %+v", ents)
			}
			if ents[0].LastModified == nil {
				t.Fatalf("lastmodified missing on file entry")
			}

			// Stat both shapes
			st, err := f.Stat(ctx, "/docs/2026/report/summary.txt")
			if err != nil {
				t.Fatalf("stat file: %v", err)
			}
			if st.IsDir || st.Size != 9 {
				t.Fatalf("stat file: %+v", st)
			}
			st, err = f.Stat(ctx, "/docs")
			if err != nil {
				t.Fatalf("stat dir: %v", err)
			}
			if !st.IsDir {
				t.Fatalf("stat dir: not a directory: %+v", st)
			}

			// Open reads the content back
			rc, size, err := f.Open(ctx, "/docs/2026/report/summary.txt")
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			buf := make([]byte, 64)
			n := 0
			for n < 9 {
				m, rerr := rc.Read(buf[n:])
				n += m
				if rerr != nil {
					break
				}
			}
			rc.Close()
			if strings.TrimSpace(string(buf[:n])) != "hello dav" {
				t.Fatalf("open: content %q (size %d)", buf[:n], size)
			}

			// Rename a file, then a directory
			if err := f.Rename(ctx, "/docs/2026/report/summary.txt", "/docs/2026/report/renamed.txt"); err != nil {
				t.Fatalf("rename file: %v", err)
			}
			if _, err := f.Stat(ctx, "/docs/2026/report/summary.txt"); !os.IsNotExist(err) && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("old name still stats: %v", err)
			}
			if err := f.Rename(ctx, "/docs/2026/report", "/docs/2026/archive"); err != nil {
				t.Fatalf("rename dir: %v", err)
			}
			if _, err := f.Stat(ctx, "/docs/2026/archive/renamed.txt"); err != nil {
				t.Fatalf("moved file missing after dir rename: %v", err)
			}

			// Remove: recursive on directories
			if err := f.Remove(ctx, "/docs/2026/archive"); err != nil {
				t.Fatalf("remove tree: %v", err)
			}
			if _, err := f.Stat(ctx, "/docs/2026/archive/renamed.txt"); !os.IsNotExist(err) && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("removed file still present: %v", err)
			}

			// Not-exist contract (os.IsNotExist and errors.Is both work)
			if _, err := f.List(ctx, "/nope"); !os.IsNotExist(err) {
				t.Fatalf("list missing: want ErrNotExist, got %v", err)
			}
			if _, err := f.Stat(ctx, "/nope/file.txt"); !os.IsNotExist(err) {
				t.Fatalf("stat missing: want ErrNotExist, got %v", err)
			}

			// Root is off-limits for removal
			if err := f.Remove(ctx, "/"); err == nil {
				t.Fatalf("removing the root must fail")
			}
		})
	}
}

// TestWebDAVParsesCreationDate pins the optional RFC 4918 creationdate
// property: x/net/webdav (the contract test's server) does not emit it, so
// this uses a stub answering PROPFIND with canned multistatus XML shaped
// like Apache/Nextcloud output — one file with creationdate, one without
// (the property is optional; absence must leave Created nil, never zero
// time).
func TestWebDAVParsesCreationDate(t *testing.T) {
	const multistatus = `<?xml version="1.0" encoding="utf-8"?>
<D:multistatus xmlns:D="DAV:">
 <D:response>
  <D:href>/with-date.txt</D:href>
  <D:propstat>
   <D:prop>
    <D:getcontentlength>11</D:getcontentlength>
    <D:getlastmodified>Mon, 28 Sep 2026 10:00:00 GMT</D:getlastmodified>
    <D:creationdate>2026-01-02T03:04:05Z</D:creationdate>
   </D:prop>
   <D:status>HTTP/1.1 200 OK</D:status>
  </D:propstat>
 </D:response>
 <D:response>
  <D:href>/no-date.txt</D:href>
  <D:propstat>
   <D:prop>
    <D:getcontentlength>7</D:getcontentlength>
    <D:getlastmodified>Mon, 28 Sep 2026 11:00:00 GMT</D:getlastmodified>
   </D:prop>
   <D:status>HTTP/1.1 200 OK</D:status>
  </D:propstat>
 </D:response>
</D:multistatus>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			http.Error(w, "405", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, multistatus)
	}))
	t.Cleanup(srv.Close)
	host, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
	p, _ := strconv.Atoi(port)
	f, err := DialWebDAV(context.Background(), profile.Source{
		Name: "stub", Type: profile.TypeWebDAV, Host: host, Port: p,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer f.Close()
	ents, err := f.List(context.Background(), "/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ents) != 2 {
		t.Fatalf("want 2 entries, got %+v", ents)
	}
	for _, e := range ents {
		switch e.Name {
		case "with-date.txt":
			if e.Created == nil {
				t.Fatalf("creationdate not parsed: %+v", e)
			}
			if got := e.Created.UTC().Format(time.RFC3339); got != "2026-01-02T03:04:05Z" {
				t.Errorf("Created = %s, want 2026-01-02T03:04:05Z", got)
			}
			if e.LastModified == nil {
				t.Errorf("lastmodified dropped alongside creationdate")
			}
		case "no-date.txt":
			if e.Created != nil {
				t.Errorf("absent creationdate must stay nil, got %v", e.Created)
			}
		default:
			t.Errorf("unexpected entry %q", e.Name)
		}
	}
}

// TestWebDAVAuthBadCredentials proves the dial probe fails fast on 401.
func TestWebDAVAuthBadCredentials(t *testing.T) {
	_, src := davTestServer(t, "", "alice", "secret")
	src.Password = "wrong"
	if _, err := DialWebDAV(context.Background(), src); err == nil {
		t.Fatalf("bad credentials: dial must fail")
	}
}

// TestWebDAVAuthAccepted dials with the right credentials and lists /.
func TestWebDAVAuthAccepted(t *testing.T) {
	_, src := davTestServer(t, "", "alice", "secret")
	f, err := DialWebDAV(context.Background(), src)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer f.Close()
	if _, err := f.List(context.Background(), "/"); err != nil {
		t.Fatalf("list root: %v", err)
	}
}

// A peer that stops answering mid-session must not park the engine: the
// metadata cap breaks the wait and the verdict is the shared deadline
// sentinel the api layer's healing cache redials on. The server answers
// exactly one request (the dial probe) and goes silent on every request
// after it — no FIN, no RST, just a handler that never writes.
func TestWebDAVCommandDeadlineBreaksSilentWedge(t *testing.T) {
	var served atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if served.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<?xml version="1.0" encoding="utf-8"?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
</D:multistatus>`)
			return
		}
		<-r.Context().Done() // the wedge: hold the request open, answer nothing
	}))
	t.Cleanup(srv.Close)

	host, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
	p, _ := strconv.Atoi(port)
	src := profile.Source{Name: "dav", Type: profile.TypeWebDAV, Host: host, Port: p}

	old := davCmdTimeout
	davCmdTimeout = 150 * time.Millisecond
	t.Cleanup(func() { davCmdTimeout = old })

	fs, err := DialWebDAV(context.Background(), src) // the dial completes — the wedge is mid-session
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer fs.Close()

	start := time.Now()
	if _, err := fs.List(context.Background(), "/"); !errors.Is(err, ErrCmdDeadline) {
		t.Fatalf("List under a wedged server = %v, want the command-deadline verdict", err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("the break took %v — the deadline never fired", el)
	}
	// The verdict repeats fast on the next op: the engine never parks, and
	// the redial is the api layer's answer to exactly this sentinel.
	if _, err := fs.Stat(context.Background(), "/x"); !errors.Is(err, ErrCmdDeadline) {
		t.Fatalf("Stat under a wedged server = %v, want the command-deadline verdict", err)
	}
}

// Streams ride the caller's patience, not the metadata cap: a body that
// legitimately outlives davCmdTimeout must still complete, because an
// upload's or download's duration is not the engine's to guess.
func TestWebDAVStreamsRideCallerPatience(t *testing.T) {
	const probe = `<?xml version="1.0" encoding="utf-8"?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
</D:multistatus>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, probe)
		case "PUT":
			time.Sleep(250 * time.Millisecond) // slower than the swapped cap
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusCreated)
		case "GET":
			w.Header().Set("Content-Length", "30")
			for i := 0; i < 3; i++ {
				_, _ = w.Write([]byte("0123456789"))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				time.Sleep(120 * time.Millisecond)
			}
		default:
			http.Error(w, "unexpected", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	host, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
	p, _ := strconv.Atoi(port)
	src := profile.Source{Name: "dav", Type: profile.TypeWebDAV, Host: host, Port: p}

	old := davCmdTimeout
	davCmdTimeout = 100 * time.Millisecond // a stream outliving this must still finish
	t.Cleanup(func() { davCmdTimeout = old })

	fs, err := DialWebDAV(context.Background(), src)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer fs.Close()
	ctx := context.Background()

	// Upload: the PUT (handler slower than the cap) completes anyway.
	if err := fs.Create(ctx, "/slow.bin", strings.NewReader("payload beyond the cap")); err != nil {
		t.Fatalf("Create outliving the metadata cap = %v — streams must ride the caller's patience", err)
	}
	// Download: the GET body (three flushed chunks, 360 ms) reads whole.
	rc, _, err := fs.Open(ctx, "/slow.bin")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b, rerr := io.ReadAll(rc)
	rc.Close()
	if rerr != nil || string(b) != "012345678901234567890123456789" {
		t.Fatalf("streamed body = %q err = %v — the cap must not cut a live stream", b, rerr)
	}
}
