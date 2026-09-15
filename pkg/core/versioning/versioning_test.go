package versioning

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// The tests here are hermetic: an in-process httptest S3 serving
// ListObjectVersions XML (no network, no keys, no real bucket).

// ver is one timeline entry the mock serves: a real version or a delete
// marker.
type ver struct {
	key     string
	id      string
	latest  bool
	marker  bool
	size    int64
	storage string
}

// newVersionsS3 serves one ListObjectVersions page of the given entries.
func newVersionsS3(t testing.TB, entries []ver) *s3.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var b strings.Builder
		b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
		b.WriteString("<ListVersionsResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\">")
		b.WriteString("<Name>bucket</Name><IsTruncated>false</IsTruncated>")
		for _, e := range entries {
			if e.marker {
				b.WriteString("<DeleteMarker>")
				b.WriteString("<Key>" + e.key + "</Key><VersionId>" + e.id + "</VersionId>")
				writeBool(&b, "IsLatest", e.latest)
				b.WriteString("<LastModified>2026-09-01T00:00:00.000Z</LastModified>")
				b.WriteString("</DeleteMarker>")
				continue
			}
			b.WriteString("<Version>")
			b.WriteString("<Key>" + e.key + "</Key><VersionId>" + e.id + "</VersionId>")
			writeBool(&b, "IsLatest", e.latest)
			b.WriteString("<LastModified>2026-09-01T00:00:00.000Z</LastModified>")
			b.WriteString("<ETag>&quot;e&quot;</ETag>")
			b.WriteString("<Size>1024</Size><StorageClass>STANDARD</StorageClass>")
			b.WriteString("</Version>")
		}
		b.WriteString("</ListVersionsResult>")
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, b.String())
	}))
	t.Cleanup(srv.Close)
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(srv.URL),
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		UsePathStyle: true,
	})
}

func writeBool(b *strings.Builder, tag string, v bool) {
	if v {
		b.WriteString("<" + tag + ">true</" + tag + ">")
	} else {
		b.WriteString("<" + tag + ">false</" + tag + ">")
	}
}

func byName(kids []ChildSummary, name string, isDir bool) *ChildSummary {
	for i := range kids {
		if kids[i].Name == name && kids[i].IsDir == isDir {
			return &kids[i]
		}
	}
	return nil
}

// TestChildSummaries pins the folder-view badge contract: per-immediate-
// child aggregates under a prefix, file/dir namespaces kept apart, the
// prefix's own rows skipped, and AllDeleted exactly when no live latest
// version remains beneath a child.
func TestChildSummaries(t *testing.T) {
	entries := []ver{
		// the prefix's own folder marker — must be skipped
		{key: "docs/", id: "folder", latest: false, marker: true},
		// readme.md: 2 real versions (one live) + an old marker in history
		{key: "docs/readme.md", id: "r2", latest: true, size: 1024},
		{key: "docs/readme.md", id: "r1", latest: false},
		{key: "docs/readme.md", id: "rm", latest: false, marker: true},
		// keep.txt: live, no markers
		{key: "docs/keep.txt", id: "k1", latest: true},
		// legacy/ dir: one file whose newest state is a delete marker →
		// nothing live beneath → AllDeleted
		{key: "docs/legacy/old.txt", id: "o1", latest: false},
		{key: "docs/legacy/old.txt", id: "om", latest: true, marker: true},
		// mixed/ dir: one live file + one fully-marked file → markers>0,
		// but not AllDeleted
		{key: "docs/mixed/live.txt", id: "l1", latest: true},
		{key: "docs/mixed/gone.txt", id: "g1", latest: false},
		{key: "docs/mixed/gone.txt", id: "gm", latest: true, marker: true},
		// a file "spec" AND a directory "spec/" coexisting under docs/
		{key: "docs/spec", id: "s1", latest: true},
		{key: "docs/spec/inner.txt", id: "s2", latest: true},
	}
	ctx := context.Background()
	kids, err := ChildSummaries(ctx, newVersionsS3(t, entries), "bucket", "docs/")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		isDir      bool
		versions   int
		markers    int
		allDeleted bool
	}{
		{"readme.md", false, 2, 1, false}, // marker in history, still live
		{"keep.txt", false, 1, 0, false},  // clean file still reported
		{"legacy", true, 1, 1, true},      // everything beneath is marked
		{"mixed", true, 2, 1, false},      // one live file keeps the folder up
		{"spec", false, 1, 0, false},      // file "spec" …
		{"spec", true, 1, 0, false},       // … and dir "spec/" stay separate
	}
	for _, c := range cases {
		got := byName(kids, c.name, c.isDir)
		if got == nil {
			t.Fatalf("missing child %q (isDir=%v) in %+v", c.name, c.isDir, kids)
		}
		if got.Versions != c.versions || got.Markers != c.markers || got.AllDeleted != c.allDeleted {
			t.Errorf("child %q (isDir=%v) = %+v, want versions=%d markers=%d allDeleted=%v",
				c.name, c.isDir, *got, c.versions, c.markers, c.allDeleted)
		}
	}
	// the prefix's own marker row must not surface as a child
	if byName(kids, "", false) != nil || byName(kids, "", true) != nil {
		t.Errorf("the prefix's own rows leaked into the summary: %+v", kids)
	}
}

// TestCountPurgeModes pins what each purge mode would remove under a
// prefix — the count-then-act contract the API layer and CLI rely on.
func TestCountPurgeModes(t *testing.T) {
	entries := []ver{
		{key: "docs/a.txt", id: "a2", latest: true},  // current
		{key: "docs/a.txt", id: "a1", latest: false}, // noncurrent
		{key: "docs/b.txt", id: "bm", latest: true, marker: true},
		{key: "docs/c.txt", id: "c1", latest: true},
	}
	ctx := context.Background()
	c := newVersionsS3(t, entries)
	for _, tc := range []struct {
		mode PurgeMode
		want int
	}{
		{PurgeAll, 4},
		{PurgeNoncurrent, 1}, // only a1
		{PurgeMarkers, 1},    // only bm
	} {
		n, err := CountPurge(ctx, c, "bucket", "docs/", tc.mode)
		if err != nil {
			t.Fatal(err)
		}
		if n != tc.want {
			t.Errorf("CountPurge(%v) = %d, want %d", tc.mode, n, tc.want)
		}
	}
}
