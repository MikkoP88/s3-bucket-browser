package remotefs

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

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
