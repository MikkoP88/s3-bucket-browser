// usage_test.go pins the content-viewer size bar's walk contract: real
// recursive sizes on every source type, the version timeline split into
// live bytes / noncurrent bytes / markers, exact-key file children (a
// prefix-colliding sibling must not join), folder children excluding
// their own marker, partial results on a failing walk, and the remote
// engine's per-source operation lock held across the walk. Hermetic: an
// in-process httptest S3 serving versioning XML, and the local engine
// over seeded temp dirs — no network, no keys.
package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/appsettings"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// usageVer is one timeline entry the fake serves.
type usageVer struct {
	key    string
	id     string
	latest bool
	marker bool
	size   int64
}

// usageBucket is one bucket the fake serves: its versioning status (""
// = never configured) or, when broken is set, 500s on every call.
type usageBucket struct {
	name       string
	versioning string
	broken     bool
	entries    []usageVer
}

// newUsageServer serves GetBucketVersioning and one ListObjectVersions
// page per bucket (prefix-filtered, like real S3). It returns the
// endpoint URL.
func newUsageServer(t testing.TB, buckets ...usageBucket) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.Trim(r.URL.Path, "/")
		var b *usageBucket
		for i := range buckets {
			if buckets[i].name == name {
				b = &buckets[i]
			}
		}
		if b == nil || b.broken {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		q := r.URL.Query()
		switch {
		case q.Has("versioning"):
			var sb strings.Builder
			sb.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
			sb.WriteString("<VersioningConfiguration xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\">")
			if b.versioning != "" {
				sb.WriteString("<Status>" + b.versioning + "</Status>")
			}
			sb.WriteString("</VersioningConfiguration>")
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, sb.String())
		case q.Has("versions"):
			prefix := q.Get("prefix")
			var sb strings.Builder
			sb.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
			sb.WriteString("<ListVersionsResult xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\">")
			sb.WriteString("<Name>" + b.name + "</Name><IsTruncated>false</IsTruncated>")
			for _, e := range b.entries {
				if !strings.HasPrefix(e.key, prefix) {
					continue
				}
				if e.marker {
					sb.WriteString("<DeleteMarker><Key>" + e.key + "</Key><VersionId>" + e.id + "</VersionId>")
					usageXMLBool(&sb, "IsLatest", e.latest)
					sb.WriteString("<LastModified>2026-09-01T00:00:00.000Z</LastModified></DeleteMarker>")
					continue
				}
				sb.WriteString("<Version><Key>" + e.key + "</Key><VersionId>" + e.id + "</VersionId>")
				usageXMLBool(&sb, "IsLatest", e.latest)
				sb.WriteString("<LastModified>2026-09-01T00:00:00.000Z</LastModified><ETag>&quot;e&quot;</ETag>")
				sb.WriteString("<Size>" + strconv.FormatInt(e.size, 10) + "</Size>")
				sb.WriteString("<StorageClass>STANDARD</StorageClass></Version>")
			}
			sb.WriteString("</ListVersionsResult>")
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, sb.String())
		default:
			http.Error(w, "unexpected call", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func usageXMLBool(b *strings.Builder, tag string, v bool) {
	if v {
		b.WriteString("<" + tag + ">true</" + tag + ">")
	} else {
		b.WriteString("<" + tag + ">false</" + tag + ">")
	}
}

// usageApp saves one S3 source pointed at the fake endpoint and makes it
// the main view's source — the store S3Usage and BucketUsage walk.
func usageApp(t *testing.T, endpoint string) *App {
	t.Helper()
	a := newTestApp(t)
	a.Startup(context.Background())
	src := profile.Source{
		Name: "store",
		Type: profile.TypeS3,
		S3: &profile.Profile{
			Name: "store", Endpoint: endpoint, Region: "us-east-1",
			PathStyle: true, AccessKeyID: "k", SecretKey: "s",
		},
	}
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewSource("store"); err != nil {
		t.Fatal(err)
	}
	return a
}

// checkStat pins one stat against every field the bar renders. want.Error
// non-empty means "any error text" — the SDK wording is not stable.
func checkStat(t *testing.T, label string, got, want UsageStat) {
	t.Helper()
	if got.Key != want.Key || got.Files != want.Files || got.Dirs != want.Dirs ||
		got.CurrentBytes != want.CurrentBytes || got.VersionBytes != want.VersionBytes ||
		got.VersionCount != want.VersionCount || got.MarkerCount != want.MarkerCount ||
		got.Versioned != want.Versioned || got.Partial != want.Partial {
		t.Errorf("%s = %+v, want %+v", label, got, want)
	}
	if want.Error != "" && got.Error == "" {
		t.Errorf("%s: Error empty, want an error message", label)
	}
}

// labBucket is the shared versioned fixture: nested folders (one marker,
// one implicit), a file with two old versions, a prefix-colliding
// sibling of another file, and both marker flavors.
func labBucket() usageBucket {
	return usageBucket{
		name:       "lab",
		versioning: "Enabled",
		entries: []usageVer{
			{key: "readme.md", id: "r3", latest: true, size: 1234},
			{key: "readme.md", id: "r2", latest: false, size: 800},
			{key: "readme.md", id: "r1", latest: false, size: 500},
			{key: "readme.md.bak", id: "b1", latest: true, size: 77},
			{key: "docs/", id: "d1", latest: true, size: 0},
			{key: "docs/notes.md", id: "n2", latest: true, size: 900},
			{key: "docs/notes.md", id: "n1", latest: false, size: 400},
			{key: "docs/legacy/", id: "l1", latest: true, size: 0},
			{key: "docs/legacy/old.txt", id: "o1", latest: true, size: 10},
			{key: "photos/img.jpg", id: "p1", latest: true, size: 2048},
			{key: "gone.txt", id: "g1", latest: true, marker: true},
			{key: "buried.txt", id: "g0", latest: false, marker: true},
		},
	}
}

// TestS3UsageVersioned pins the whole-prefix and per-child walks on a
// versioned bucket: live bytes from latest versions only, noncurrent
// bytes summed separately, markers counted never summed, folder children
// excluding their own marker, file children matching their exact key
// (the .bak prefix collision must not join), and implicit folders
// (photos/) counted without a marker object.
func TestS3UsageVersioned(t *testing.T) {
	a := usageApp(t, newUsageServer(t, labBucket()))

	// Whole listing: 5 live files (1234+77+900+10+2048), 3 folders
	// (docs/, docs/legacy/, photos/ — the last implicit), 1700 bytes of
	// old versions (800+500+400), 10 versions, 2 markers.
	got, err := a.S3Usage("lab", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("whole-prefix stats = %d, want 1", len(got))
	}
	checkStat(t, "whole", got[0], UsageStat{
		Files: 5, Dirs: 3, CurrentBytes: 4269,
		VersionBytes: 1700, VersionCount: 10, MarkerCount: 2, Versioned: true,
	})

	// Folder child docs/: its own marker skipped, its interior counted.
	got, err = a.S3Usage("lab", "", []string{"docs/"})
	if err != nil {
		t.Fatal(err)
	}
	checkStat(t, "docs/", got[0], UsageStat{
		Key: "docs/", Files: 2, Dirs: 1, CurrentBytes: 910,
		VersionBytes: 400, VersionCount: 4, Versioned: true,
	})

	// File child readme.md: the exact key's timeline only — readme.md.bak
	// shares the prefix but must not join.
	got, err = a.S3Usage("lab", "", []string{"readme.md"})
	if err != nil {
		t.Fatal(err)
	}
	checkStat(t, "readme.md", got[0], UsageStat{
		Key: "readme.md", Files: 1, CurrentBytes: 1234,
		VersionBytes: 1300, VersionCount: 3, Versioned: true,
	})

	// Mixed selection keeps child order and aggregates each part.
	got, err = a.S3Usage("lab", "", []string{"docs/", "readme.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "docs/" || got[1].Key != "readme.md" {
		t.Fatalf("mixed children = %+v", got)
	}
	if got[0].CurrentBytes+got[1].CurrentBytes != 2144 {
		t.Errorf("mixed current = %d, want 910+1234", got[0].CurrentBytes+got[1].CurrentBytes)
	}

	// A prefix inside the bucket (children empty) walks that subtree only.
	got, err = a.S3Usage("lab", "docs/legacy", nil)
	if err != nil {
		t.Fatal(err)
	}
	checkStat(t, "legacy prefix", got[0], UsageStat{
		Key: "docs/legacy/", Files: 1, CurrentBytes: 10,
		VersionCount: 1, Versioned: true, // the marker is the root: skipped
	})
}

// TestS3UsageUnversionedAndSuspended pins the two other versioning
// states: a never-configured bucket (every object its own single null
// version, no version part) and a suspended one (old versions and
// markers from the enabled era still counted). An empty versioned
// bucket reports zeros, not an error.
func TestS3UsageUnversionedAndSuspended(t *testing.T) {
	a := usageApp(t, newUsageServer(t,
		usageBucket{
			name: "plain",
			entries: []usageVer{
				{key: "a.txt", id: "null", latest: true, size: 100},
				{key: "sub/b.txt", id: "null", latest: true, size: 50},
			},
		},
		usageBucket{
			name:       "cold",
			versioning: "Suspended",
			entries: []usageVer{
				{key: "log.txt", id: "c2", latest: true, size: 1000},
				{key: "log.txt", id: "c1", latest: false, size: 600},
				{key: "log.txt", id: "c0", latest: false, marker: true},
			},
		},
		usageBucket{name: "empty", versioning: "Enabled"},
	))

	got, err := a.S3Usage("plain", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	checkStat(t, "plain", got[0], UsageStat{
		Files: 2, Dirs: 1, CurrentBytes: 150,
		VersionCount: 2, Versioned: false,
	})

	got, err = a.S3Usage("cold", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	checkStat(t, "cold", got[0], UsageStat{
		Files: 1, CurrentBytes: 1000,
		VersionBytes: 600, VersionCount: 2, MarkerCount: 1, Versioned: true,
	})

	got, err = a.S3Usage("empty", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	checkStat(t, "empty", got[0], UsageStat{Versioned: true})
}

// TestS3UsageWalkErrorPartial pins the failure contract: a bucket whose
// walk fails reports Partial with an error message and floor (zero)
// totals — never a silent wrong answer.
func TestS3UsageWalkErrorPartial(t *testing.T) {
	// No SDK retries: one 500 per request, so the test stays fast. The
	// tuning file lands in the isolated S3B_CONFIG dir newTestApp set up,
	// and the client (built lazily on first walk) reads it live.
	a := usageApp(t, newUsageServer(t, usageBucket{name: "broken", broken: true}))
	if err := appsettings.Save(appsettings.Tuning{RetryAttempts: 1}); err != nil {
		t.Fatal(err)
	}

	got, err := a.S3Usage("broken", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	checkStat(t, "broken", got[0], UsageStat{Partial: true, Error: "walk failed"})
}

// TestBucketUsage pins the buckets view's bar: one whole-bucket walk per
// name, the stat keyed by bucket name, per-bucket versioning flags.
func TestBucketUsage(t *testing.T) {
	a := usageApp(t, newUsageServer(t,
		usageBucket{
			name: "plain",
			entries: []usageVer{
				{key: "a.txt", id: "null", latest: true, size: 100},
				{key: "sub/b.txt", id: "null", latest: true, size: 50},
			},
		},
		usageBucket{
			name:       "cold",
			versioning: "Suspended",
			entries: []usageVer{
				{key: "log.txt", id: "c2", latest: true, size: 1000},
				{key: "log.txt", id: "c1", latest: false, size: 600},
			},
		},
	))

	got, err := a.BucketUsage([]string{"plain", "cold"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("bucket stats = %d, want 2", len(got))
	}
	checkStat(t, "plain", got[0], UsageStat{
		Key: "plain", Files: 2, Dirs: 1, CurrentBytes: 150,
		VersionCount: 2, Versioned: false,
	})
	checkStat(t, "cold", got[1], UsageStat{
		Key: "cold", Files: 1, CurrentBytes: 1000,
		VersionBytes: 600, VersionCount: 2, Versioned: true,
	})
}

// seedUsageTree seeds the local engine's root with exact byte sizes.
func seedUsageTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"readme.md":           "hello world", // 11
		"docs/notes.md":       "notes",       // 5
		"docs/legacy/old.bin": "abc",         // 3
		"photos/img.jpg":      "1234567",     // 7
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestRemoteUsageLocal pins the remote walk over the local engine: the
// whole directory, folder children (everything inside), file children
// (stated, not walked), a missing child reported Partial, and S3 sources
// rejected — they browse their own pipeline.
func TestRemoteUsageLocal(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	if err := a.SaveSource(profile.Source{
		Name: "lab", Type: profile.TypeLocal, LocalRoot: seedUsageTree(t),
	}); err != nil {
		t.Fatal(err)
	}

	// Whole root: 4 files (26 bytes), 3 folders.
	got, err := a.RemoteUsage("lab", "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("whole stats = %d, want 1", len(got))
	}
	checkStat(t, "whole", got[0], UsageStat{
		Key: "/", Files: 4, Dirs: 3, CurrentBytes: 26,
	})

	// Folder + file children.
	got, err = a.RemoteUsage("lab", "/", []string{"/docs/", "/readme.md"})
	if err != nil {
		t.Fatal(err)
	}
	checkStat(t, "/docs/", got[0], UsageStat{
		Key: "/docs/", Files: 2, Dirs: 1, CurrentBytes: 8,
	})
	checkStat(t, "/readme.md", got[1], UsageStat{
		Key: "/readme.md", Files: 1, CurrentBytes: 11,
	})

	// A missing child walks nowhere: Partial, error message, zero totals.
	got, err = a.RemoteUsage("lab", "/", []string{"/gone/"})
	if err != nil {
		t.Fatal(err)
	}
	checkStat(t, "/gone/", got[0], UsageStat{Key: "/gone/", Partial: true, Error: "missing"})

	// S3 sources are not remote engines.
	if err := a.SaveSource(s3Source("cloud", "s")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoteUsage("cloud", "/", nil); err == nil {
		t.Error("RemoteUsage on an S3 source must error")
	}
}

// TestRemoteUsageHoldsSourceLock pins the engine serialization contract:
// the per-source operation lock is held across the whole walk, so a
// second walk on the same source cannot interleave engine calls.
func TestRemoteUsageHoldsSourceLock(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	if err := a.SaveSource(profile.Source{
		Name: "lab", Type: profile.TypeLocal, LocalRoot: seedUsageTree(t),
	}); err != nil {
		t.Fatal(err)
	}
	// Pre-warm the engine cache (also resolves the source ID) so the
	// walk below proceeds straight to the lock.
	src, _, err := a.remoteSource("lab")
	if err != nil {
		t.Fatal(err)
	}

	unlock := a.lockSrcs(src.ID)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := a.RemoteUsage("lab", "/", nil); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-done:
		t.Fatal("RemoteUsage completed while the source lock was held")
	case <-time.After(150 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RemoteUsage never finished after the lock was released")
	}
}
