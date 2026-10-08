package api

import (
	"context"
	"encoding/xml"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// The "is a directory" regression suite. Copying S3 folders that carry
// explicit marker objects (0-byte keys ending in "/") once planned the
// item's own marker as a FILE copy aimed at the folder's own path — the
// local writer then ran os.Create over the just-created directory and the
// job ended with `Testi: open C:\...\Testi: is a directory`. These tests
// pin the whole class: a directory-representing entry (folder marker,
// walk root, empty folder) must never become a planned file, on any
// source kind × destination kind.

// fakeS3 is an in-process path-style S3 endpoint: enough of ListObjectsV2,
// GetObject, PutObject, CopyObject and DeleteObjects for an S3-sourced
// TransferCross to run hermetically (no network, no real bucket). Keys are
// stored per bucket; a key ending in "/" is an explicit folder marker.
type fakeS3 struct {
	mu              sync.Mutex
	url             string
	objects         map[string]map[string]string // bucket → key → content
	etags           map[string]map[string]string // bucket → key → etag (content-derived)
	deleted         []string
	adminOps        []string // admin subresource calls that landed, in order
	sawChecksumMode bool     // some GET asked for response checksums
	// faults injects transient failures: "PUT bucket/key" / "GET bucket/key"
	// → how many requests to fail with a 500 before serving honestly
	// again (the retry rigs' lever).
	faults map[string]int
}

// etagOf mints the store's content-addressed identity: same bytes, same
// etag — the grammar S3's MD5 etags follow, so a re-seeded overwrite is a
// new identity exactly the way a teammate's PUT would be.
func etagOf(content string) string {
	h := fnv.New32a()
	io.WriteString(h, content)
	return fmt.Sprintf("\"%08x\"", h.Sum32())
}

// fault fails the next n requests for one verb+key, then heals.
func (f *fakeS3) fault(verb, bucket, key string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.faults == nil {
		f.faults = map[string]int{}
	}
	f.faults[verb+" "+bucket+"/"+key] = n
}

// tripped consumes one fault for verb+key, reporting whether the request
// must fail.
func (f *fakeS3) tripped(verb, bucket, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := verb + " " + bucket + "/" + key
	if f.faults[k] > 0 {
		f.faults[k]--
		return true
	}
	return false
}

func newFakeS3(buckets ...string) *fakeS3 {
	f := &fakeS3{objects: map[string]map[string]string{}}
	for _, b := range buckets {
		f.objects[b] = map[string]string{}
	}
	return f
}

func (f *fakeS3) seed(bucket, key, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setLocked(bucket, key, content)
}

// setLocked writes content and its derived identity. Callers hold mu.
func (f *fakeS3) setLocked(bucket, key, content string) {
	if f.etags == nil {
		f.etags = map[string]map[string]string{}
	}
	if f.etags[bucket] == nil {
		f.etags[bucket] = map[string]string{}
	}
	f.objects[bucket][key] = content
	f.etags[bucket][key] = etagOf(content)
}

// keys returns the live (non-deleted) keys of one bucket, sorted.
func (f *fakeS3) keys(bucket string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for k := range f.objects[bucket] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (f *fakeS3) deletedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.deleted)
}

// checksumModeSeen reports whether any object GET asked for response
// checksums — the integrity round's request-side assert.
func (f *fakeS3) checksumModeSeen() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sawChecksumMode
}

// splitS3Path splits a path-style URL path into bucket and key.
func splitS3Path(p string) (bucket, key string) {
	p = strings.TrimPrefix(p, "/")
	if i := strings.Index(p, "/"); i >= 0 {
		return p[:i], p[i+1:]
	}
	return p, ""
}

// adminSubresource names the bucket-administration subresource a request
// carries (?versioning, ?policy, ...), or "" when it carries none — the
// admin-log rigs' half of the fake's dispatch.
func adminSubresource(r *http.Request) string {
	for _, sub := range []string{"versioning", "policy", "cors", "lifecycle",
		"encryption", "website", "tagging"} {
		if r.URL.Query().Has(sub) {
			return sub
		}
	}
	if r.URL.Query().Has("publicAccessBlock") { // the wire spells PAB without hyphens
		return "public-access-block"
	}
	return ""
}

// adminLandings lists the admin subresource calls that landed, in order.
func (f *fakeS3) adminLandings() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.adminOps)
}

// serve wires the endpoint and returns its URL.
func (f *fakeS3) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.mu.Lock()
	f.url = srv.URL
	f.mu.Unlock()
	return srv.URL
}

// ServeHTTP is fakeS3's request dispatch, split out of serve so a test can
// wrap it (hold one key's requests, inject faults) while every other call
// answers with the ordinary fake.
func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key := splitS3Path(r.URL.Path)
	switch {
	case r.URL.Query().Get("list-type") == "2":
		f.list(w, bucket, r.URL.Query().Get("prefix"))
	case r.Method == http.MethodGet && bucket == "":
		// GET / is ListBuckets — the account-level call the split dials.
		f.listBuckets(w)
	case r.Method == http.MethodGet:
		// The SDK serializes the enum verbatim ("ENABLED"); EqualFold
		// tolerates the casing without blessing a wrong one.
		if strings.EqualFold(r.Header.Get("X-Amz-Checksum-Mode"), "ENABLED") {
			f.mu.Lock()
			f.sawChecksumMode = true
			f.mu.Unlock()
		}
		if f.tripped("GET", bucket, key) {
			http.Error(w, "fakeS3: injected fault", http.StatusInternalServerError)
			return
		}
		f.getObject(w, bucket, key)
	case r.Method == http.MethodHead:
		f.headObject(w, bucket, key)
	case r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "":
		f.copyObject(w, r, bucket, key)
	case (r.Method == http.MethodPut || r.Method == http.MethodDelete) && adminSubresource(r) != "":
		sub := adminSubresource(r)
		if f.tripped(r.Method, bucket, sub) {
			http.Error(w, "fakeS3: injected fault", http.StatusInternalServerError)
			return
		}
		f.mu.Lock()
		f.adminOps = append(f.adminOps, r.Method+" "+sub+" "+bucket)
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPut:
		if f.tripped("PUT", bucket, key) {
			http.Error(w, "fakeS3: injected fault", http.StatusInternalServerError)
			return
		}
		f.putObject(w, r, bucket, key)
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		f.deleteObjects(w, r, bucket)
	default:
		http.Error(w, "fakeS3: unsupported "+r.Method, http.StatusBadRequest)
	}
}

func (f *fakeS3) list(w http.ResponseWriter, bucket, prefix string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.objects[bucket]))
	for k := range f.objects[bucket] {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	fmt.Fprintf(&b, "<Name>%s</Name><Prefix>%s</Prefix>", bucket, prefix)
	fmt.Fprintf(&b, "<KeyCount>%d</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>", len(keys))
	for _, k := range keys {
		fmt.Fprintf(&b,
			"<Contents><Key>%s</Key><LastModified>2026-01-01T00:00:00.000Z</LastModified><ETag>%s</ETag><Size>%d</Size><StorageClass>STANDARD</StorageClass></Contents>",
			k, xmlEscape(f.etags[bucket][k]), len(f.objects[bucket][k]))
	}
	b.WriteString("</ListBucketResult>")
	w.Header().Set("Content-Type", "application/xml")
	io.WriteString(w, b.String())
}

// listBuckets answers GET / — the account-level ListBuckets call the
// split dials to fan an account source out per bucket (same XML shape as
// newUsageServer's).
func (f *fakeS3) listBuckets(w http.ResponseWriter) {
	f.mu.Lock()
	names := make([]string, 0, len(f.objects))
	for b := range f.objects {
		names = append(names, b)
	}
	f.mu.Unlock()
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString("<ListAllMyBucketsResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\">")
	b.WriteString("<Owner><ID>fake</ID><DisplayName>fake</DisplayName></Owner><Buckets>")
	for _, n := range names {
		b.WriteString("<Bucket><Name>" + n + "</Name></Bucket>")
	}
	b.WriteString("</Buckets></ListAllMyBucketsResult>")
	w.Header().Set("Content-Type", "application/xml")
	io.WriteString(w, b.String())
}

func (f *fakeS3) getObject(w http.ResponseWriter, bucket, key string) {
	f.mu.Lock()
	content, ok := f.objects[bucket][key]
	etag := f.etags[bucket][key]
	f.mu.Unlock()
	if !ok {
		http.Error(w, "fakeS3: no such key", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", etag)
	io.WriteString(w, content)
}

// headObject answers HeadObject (DownloadFile sizes the pull with it):
// same lookup as getObject, headers only.
func (f *fakeS3) headObject(w http.ResponseWriter, bucket, key string) {
	f.mu.Lock()
	content, ok := f.objects[bucket][key]
	etag := f.etags[bucket][key]
	f.mu.Unlock()
	if !ok {
		http.Error(w, "fakeS3: no such key", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
}

// xmlEscape hides an etag's quotes from the XML bodies (an etag without
// quotes stays bare — the honest shape of an unknown identity).
func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (f *fakeS3) putObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	// The conditional-write grammar: If-Match lands only while the stored
	// identity still is the one the writer named — the editor guard's
	// verdict wire. Mismatch (or naming a missing key) answers the S3
	// PreconditionFailed shape the SDK deserializes and the guard classifies.
	if m := r.Header.Get("If-Match"); m != "" {
		f.mu.Lock()
		cur, ok := f.etags[bucket][key]
		f.mu.Unlock()
		if !ok || (m != "*" && cur != m) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusPreconditionFailed)
			io.WriteString(w, xml.Header+
				`<Error><Code>PreconditionFailed</Code><Message>The condition specified in the If-Match header was not met</Message></Error>`)
			return
		}
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.setLocked(bucket, key, string(body))
	etag := f.etags[bucket][key]
	f.mu.Unlock()
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3) copyObject(w http.ResponseWriter, r *http.Request, dstBucket, dstKey string) {
	src := strings.TrimPrefix(r.Header.Get("x-amz-copy-source"), "/")
	i := strings.Index(src, "/")
	if i < 0 {
		http.Error(w, "fakeS3: malformed copy source", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	content, ok := f.objects[src[:i]][src[i+1:]]
	if !ok {
		http.Error(w, "fakeS3: no such key", http.StatusNotFound)
		return
	}
	f.setLocked(dstBucket, dstKey, content)
	w.Header().Set("Content-Type", "application/xml")
	io.WriteString(w, xml.Header+`<CopyObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ETag>`+xmlEscape(f.etags[dstBucket][dstKey])+`</ETag><LastModified>2026-01-01T00:00:00.000Z</LastModified></CopyObjectResult>`)
}

func (f *fakeS3) deleteObjects(w http.ResponseWriter, r *http.Request, bucket string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		Object []struct {
			Key string `xml:"Key"`
		} `xml:"Object"`
	}
	if err := xml.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	var b strings.Builder
	b.WriteString(xml.Header + `<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	for _, o := range req.Object {
		delete(f.objects[bucket], o.Key)
		delete(f.etags[bucket], o.Key)
		f.deleted = append(f.deleted, o.Key)
		fmt.Fprintf(&b, "<Deleted><Key>%s</Key></Deleted>", o.Key)
	}
	f.mu.Unlock()
	b.WriteString("</DeleteResult>")
	w.Header().Set("Content-Type", "application/xml")
	io.WriteString(w, b.String())
}

// fakeS3Source returns a TypeS3 data source whose client points at url.
func fakeS3Source(name, url string) profile.Source {
	return profile.Source{Name: name, Type: profile.TypeS3, S3: &profile.Profile{
		Name: name, Endpoint: url, Region: "us-east-1", PathStyle: true,
		AccessKeyID: "test", SecretKey: "test",
	}}
}

// seedMarkerTree seeds the bucket shape that broke directory copies: an
// explicit root marker ("Testi/" — what a console's "create folder"
// writes), a nested marker ("Testi/sub/"), real files under both, a
// marker-only empty folder ("Kuvat/") and a plain prefix folder with no
// marker object at all ("Plain/").
func seedMarkerTree(f *fakeS3, bucket string) {
	f.seed(bucket, "Testi/", "")
	f.seed(bucket, "Testi/a.txt", "alpha")
	f.seed(bucket, "Testi/sub/", "")
	f.seed(bucket, "Testi/sub/deep.txt", "deep")
	f.seed(bucket, "Kuvat/", "")
	f.seed(bucket, "Plain/f.txt", "f")
	f.seed(bucket, "root.txt", "r")
}

// markerDirs is a multi-folder selection off the seeded bucket: one folder
// with explicit markers, one marker-only empty folder, one plain prefix.
var markerDirs = []XferItem{
	{Source: "marks", Bucket: "src", Key: "Testi/", IsDir: true},
	{Source: "marks", Bucket: "src", Key: "Kuvat/", IsDir: true},
	{Source: "marks", Bucket: "src", Key: "Plain/", IsDir: true},
}

// newMarkerApp boots an app whose "marks" data source is a fake S3
// endpoint seeded with seedMarkerTree's bucket shape.
func newMarkerApp(t *testing.T, extraBuckets ...string) (*App, *fakeS3) {
	t.Helper()
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3(append([]string{"src"}, extraBuckets...)...)
	seedMarkerTree(f, "src")
	if err := a.SaveSource(fakeS3Source("marks", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	return a, f
}

// wantKeys pins a bucket's exact live key set (sorted).
func wantKeys(t *testing.T, f *fakeS3, bucket string, want ...string) {
	t.Helper()
	if got := f.keys(bucket); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("bucket %s keys = %v, want %v", bucket, got, want)
	}
}

// remoteTreeSource builds a TypeLocal (remote-kind engine) source over a
// seeded tree: docs/a.md, docs/nested/b.md, empty/.
func remoteTreeSource(t *testing.T, name string) (profile.Source, string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"docs/nested", "empty"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "a.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "nested", "b.md"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	return profile.Source{Name: name, Type: profile.TypeLocal, LocalRoot: root}, root
}

// The reported scenario end to end: multiple S3 folders (one carrying
// explicit markers) plus a loose file, pasted into a local directory.
// Markers must become directories — never files, never failures — and a
// second paste over the finished tree stays clean too.
func TestTransferCrossS3FolderMarkersToLocal(t *testing.T) {
	a, _ := newMarkerApp(t)
	items := append(append([]XferItem(nil), markerDirs...),
		XferItem{Source: "marks", Bucket: "src", Key: "root.txt", Size: 1})
	dest := XferDest{Kind: "local", Dir: t.TempDir()}

	ji := mustXfer(t, a, items, nil, dest, PolicyOverwrite, false)
	if ji.Status != JobDone || ji.FailedFiles != 0 || ji.TotalFiles != 4 || ji.TotalBytes != 11 {
		t.Fatalf("job = %+v", ji)
	}
	mustExist(t, filepath.Join(dest.Dir, "Testi"), true)
	if got := read(t, filepath.Join(dest.Dir, "Testi", "a.txt")); got != "alpha" {
		t.Fatalf("Testi/a.txt = %q", got)
	}
	mustExist(t, filepath.Join(dest.Dir, "Testi", "sub"), true)
	if got := read(t, filepath.Join(dest.Dir, "Testi", "sub", "deep.txt")); got != "deep" {
		t.Fatalf("Testi/sub/deep.txt = %q", got)
	}
	mustExist(t, filepath.Join(dest.Dir, "Kuvat"), true)
	if ents, err := os.ReadDir(filepath.Join(dest.Dir, "Kuvat")); err != nil || len(ents) != 0 {
		t.Fatalf("marker-only Kuvat must materialize as an empty folder, got %d entries (err %v)", len(ents), err)
	}
	if got := read(t, filepath.Join(dest.Dir, "Plain", "f.txt")); got != "f" {
		t.Fatalf("Plain/f.txt = %q", got)
	}
	if got := read(t, filepath.Join(dest.Dir, "root.txt")); got != "r" {
		t.Fatalf("root.txt = %q", got)
	}

	// Pasting over the finished tree again: still zero failures.
	ji = mustXfer(t, a, items, nil, dest, PolicyOverwrite, false)
	if ji.Status != JobDone || ji.FailedFiles != 0 {
		t.Fatalf("second paste = %+v", ji)
	}
}

// Same selection into a remote (engine) destination: directories are
// materialized as real directories and the files land inside them.
func TestTransferCrossS3FolderMarkersToRemote(t *testing.T) {
	a, _ := newMarkerApp(t)
	vaultSrc, vaultRoot := emptyLocalSource(t, "vault")
	if err := a.SaveSource(vaultSrc); err != nil {
		t.Fatal(err)
	}

	ji := mustXfer(t, a, markerDirs, nil,
		XferDest{Kind: "remote", Source: "vault", Dir: "/"}, PolicyOverwrite, false)
	if ji.Status != JobDone || ji.FailedFiles != 0 || ji.TotalFiles != 3 {
		t.Fatalf("job = %+v", ji)
	}
	if got := read(t, filepath.Join(vaultRoot, "Testi", "sub", "deep.txt")); got != "deep" {
		t.Fatalf("Testi/sub/deep.txt = %q", got)
	}
	mustExist(t, filepath.Join(vaultRoot, "Kuvat"), true)
	if got := read(t, filepath.Join(vaultRoot, "Plain", "f.txt")); got != "f" {
		t.Fatalf("Plain/f.txt = %q", got)
	}
}

// S3 → S3 both ways: the same source on both sides streams server-side
// CopyObject, different sources stream Get → Put. Neither may write an
// object at the folder's own key.
func TestTransferCrossS3FolderMarkersToS3(t *testing.T) {
	a, f := newMarkerApp(t, "dst", "dst2")
	if err := a.SaveSource(fakeS3Source("marks2", f.url)); err != nil {
		t.Fatal(err)
	}

	ji := mustXfer(t, a, markerDirs, nil,
		XferDest{Kind: "s3", Source: "marks", Bucket: "dst"}, PolicyOverwrite, false)
	if ji.Status != JobDone || ji.FailedFiles != 0 || ji.TotalFiles != 3 {
		t.Fatalf("server-side copy job = %+v", ji)
	}
	wantKeys(t, f, "dst", "Plain/f.txt", "Testi/a.txt", "Testi/sub/deep.txt")

	ji = mustXfer(t, a, markerDirs, nil,
		XferDest{Kind: "s3", Source: "marks2", Bucket: "dst2"}, PolicyOverwrite, false)
	if ji.Status != JobDone || ji.FailedFiles != 0 {
		t.Fatalf("streamed copy job = %+v", ji)
	}
	wantKeys(t, f, "dst2", "Plain/f.txt", "Testi/a.txt", "Testi/sub/deep.txt")
}

// Moving the marker-carrying selection out of S3: the copy is clean and
// the delete removes every key of the moved items — markers included —
// while unselected keys stay.
func TestTransferCrossS3FolderMarkersMove(t *testing.T) {
	a, f := newMarkerApp(t)
	dest := XferDest{Kind: "local", Dir: t.TempDir()}

	ji := mustXfer(t, a, markerDirs, nil, dest, PolicyOverwrite, true)
	if ji.Status != JobDone || ji.FailedFiles != 0 || ji.TotalFiles != 3 {
		t.Fatalf("move job = %+v", ji)
	}
	wantKeys(t, f, "src", "root.txt") // only the unselected file remains
	deleted := f.deletedKeys()
	for _, k := range []string{"Testi/", "Testi/a.txt", "Testi/sub/", "Testi/sub/deep.txt", "Kuvat/", "Plain/f.txt"} {
		if !slices.Contains(deleted, k) {
			t.Errorf("key %q was not deleted on move (deleted: %v)", k, deleted)
		}
	}
	mustExist(t, filepath.Join(dest.Dir, "Testi", "sub", "deep.txt"), false)
	mustExist(t, filepath.Join(dest.Dir, "Kuvat"), true)
}

// The class-level invariant, swept across every source kind (S3 with
// explicit folder markers, remote engine tree, local pane tree) × every
// destination kind (local, S3, remote): no planned file may land ON a
// directory path — neither exactly on it nor where a directory's parent
// chain needs it. That is precisely the "is a directory" failure shape.
func TestPlanNeverPlansAFileOnADirectoryPath(t *testing.T) {
	a, _ := newMarkerApp(t)
	treeSrc, _ := remoteTreeSource(t, "tree")
	if err := a.SaveSource(treeSrc); err != nil {
		t.Fatal(err)
	}
	vaultSrc, _ := emptyLocalSource(t, "vault")
	if err := a.SaveSource(vaultSrc); err != nil {
		t.Fatal(err)
	}

	// Local-pane tree: a.txt, sub/b.bin, empty/.
	pane := t.TempDir()
	for _, d := range []string{"sub", "empty"} {
		if err := os.MkdirAll(filepath.Join(pane, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(pane, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pane, "sub", "b.bin"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	sides := map[string]xferDestSide{
		"local":  mustResolveDest(t, a, XferDest{Kind: "local", Dir: t.TempDir()}),
		"s3":     mustResolveDest(t, a, XferDest{Kind: "s3", Source: "marks", Bucket: "dst"}),
		"remote": mustResolveDest(t, a, XferDest{Kind: "remote", Source: "vault", Dir: "/"}),
	}
	cases := []struct {
		name       string
		items      []XferItem
		localPaths []string
		files      int // planned file copies
	}{
		{"s3 markers", markerDirs, nil, 3},
		{"remote tree", []XferItem{
			{Source: "tree", Key: "/docs/", IsDir: true},
			{Source: "tree", Key: "/empty/", IsDir: true},
		}, nil, 2},
		{"local pane", nil, []string{pane}, 2},
	}
	for _, c := range cases {
		for name, side := range sides {
			plan, err := a.planXfer(ctx, c.items, c.localPaths, side)
			if err != nil {
				t.Fatalf("%s → %s: planXfer: %v", c.name, name, err)
			}
			if len(plan.files) != c.files {
				t.Errorf("%s → %s: planned %d files, want %d", c.name, name, len(plan.files), c.files)
			}
			sep := "/"
			if side.kind == "local" {
				sep = string(filepath.Separator)
			}
			dirs := map[string]bool{}
			for _, d := range plan.emptyDirs {
				dirs[d] = true
			}
			for _, fl := range plan.files {
				if dirs[fl.dstPath] {
					t.Errorf("%s → %s: file %q is planned onto directory path %q",
						c.name, name, fl.srcPath, fl.dstPath)
					continue
				}
				for d := range dirs {
					if strings.HasPrefix(d, fl.dstPath+sep) {
						t.Errorf("%s → %s: file %q → %q sits where directory %q must exist",
							c.name, name, fl.srcPath, fl.dstPath, d)
					}
				}
			}
		}
	}

	// Deterministic spot-check of the S3-marker plan against a local
	// destination: exactly the three real files, under their folders.
	plan, err := a.planXfer(ctx, markerDirs, nil, sides["local"])
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(plan.files))
	for i, fl := range plan.files {
		got[i] = fl.dstPath
	}
	sort.Strings(got)
	want := []string{
		filepath.Join(sides["local"].dir, "Plain", "f.txt"),
		filepath.Join(sides["local"].dir, "Testi", "a.txt"),
		filepath.Join(sides["local"].dir, "Testi", "sub", "deep.txt"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("planned local dstPaths = %v, want %v", got, want)
	}
}

// mustResolveDest resolves one write side for the plan-level matrix.
func mustResolveDest(t *testing.T, a *App, d XferDest) xferDestSide {
	t.Helper()
	side, err := a.resolveXferDest(d)
	if err != nil {
		t.Fatalf("resolveXferDest(%+v): %v", d, err)
	}
	return side
}
