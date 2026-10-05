// search_scope_test.go pins the search engine's source boundaries — the
// leak this file exists for: two S3 sources over one endpoint, and a
// scoped or all-sources search walked the ENDPOINT's bucket list, so a
// bucket-scoped source returned hits from buckets belonging to other
// sources, stamped with the searching source's name (the Source column
// read pairings like toinentesti/testijotain that the workspace never
// configured). Every shape is pinned: all-sources honors each source's
// own boundary, a bucket-scoped source never walks past its one bucket
// (by name and by raw src-* id alike), a folder prefix scopes the walk
// itself, and an account-wide source keeps its every-bucket walk.
// Hermetic: the httptest S3 from usage_test.go, streamed pages captured
// through the event sink.
package api

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/search"
)

// searchScopeApp saves three S3 sources over ONE endpoint — an
// account-wide source and two bucket-scoped ones, the exact shape that
// leaked — and seeds three buckets whose contents name their owner.
func searchScopeApp(t *testing.T, endpoint string) *App {
	t.Helper()
	a := newTestApp(t)
	a.Startup(context.Background())
	s3src := func(name, bucket string) profile.Source {
		return profile.Source{
			Name: name, Type: profile.TypeS3, Bucket: bucket,
			S3: &profile.Profile{Name: name, Endpoint: endpoint, Region: "us-east-1",
				PathStyle: true, AccessKeyID: "k", SecretKey: "s"},
		}
	}
	for _, src := range []profile.Source{
		s3src("acc", ""),
		s3src("toinentesti", "testijotain"),
		s3src("oma", "omakori"),
	} {
		if err := a.SaveSource(src); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.SetViewSource("acc"); err != nil {
		t.Fatal(err)
	}
	return a
}

func searchScopeBuckets() []usageBucket {
	ver := func(key string, size int64) usageVer {
		return usageVer{key: key, id: "v-" + key, latest: true, size: size}
	}
	return []usageBucket{
		{name: "testijotain", entries: []usageVer{
			ver("report.txt", 10), ver("sub/notes.md", 20), ver("sub/deep/x.md", 30),
		}},
		{name: "omakori", entries: []usageVer{
			ver("own/file.dat", 40), ver("sub/notes.md", 50),
		}},
		{name: "julkinen", entries: []usageVer{
			ver("shared.txt", 60),
		}},
	}
}

// runSearchToDone starts one search and returns its streamed hits and the
// done event, both filtered by the run's token. Events buffer until the
// done arrives, so a run whose goroutine finishes before Search returns
// is captured as completely as a slow one. The sink lives only inside
// this test: it is uninstalled (and the bus re-muted) before the next
// test constructs its app.
func runSearchToDone(t *testing.T, a *App, scope SearchScope, opts SearchOptions) ([]search.Result, SearchDone) {
	t.Helper()
	testNoEvents.Store(false)
	t.Cleanup(func() { SetEventSink(nil); testNoEvents.Store(true) })

	var mu sync.Mutex
	var raw []struct {
		event string
		data  any
	}
	done := make(chan struct{})
	SetEventSink(func(event string, data ...any) {
		mu.Lock()
		defer mu.Unlock()
		// the task registry streams no-payload events through the same
		// sink — only the ones that carry data matter here
		if len(data) > 0 && data[0] != nil {
			raw = append(raw, struct {
				event string
				data  any
			}{event, data[0]})
			if event == EventSearchDone {
				close(done)
			}
		}
	})

	tok, err := a.Search(scope, opts)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatalf("search %q never reported done", tok)
	}

	mu.Lock()
	defer mu.Unlock()
	var hits []search.Result
	var fin SearchDone
	for _, e := range raw {
		switch e.event {
		case EventSearchPage:
			if p, ok := e.data.(SearchPage); ok && p.Token == tok {
				hits = append(hits, p.Entries...)
			}
		case EventSearchDone:
			if d, ok := e.data.(SearchDone); ok && d.Token == tok {
				fin = d
			}
		}
	}
	return hits, fin
}

// pairSet folds hits into their (source, bucket) origins.
func pairSet(t *testing.T, hits []search.Result) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, h := range hits {
		out[h.Source+"/"+h.Bucket] = true
	}
	return out
}

// TestSearchAllModeHonorsSourceBoundaries: an all-sources run walks the
// account-wide source across every bucket, each bucket-scoped source
// exactly its own — and never a pairing the workspace did not configure.
func TestSearchAllModeHonorsSourceBoundaries(t *testing.T) {
	a := searchScopeApp(t, newUsageServer(t, searchScopeBuckets()...))

	hits, fin := runSearchToDone(t, a, SearchScope{Mode: "all"}, SearchOptions{})
	if fin.Error != "" || fin.SourceErrors != "" {
		t.Fatalf("all-sources run failed: %q / %q", fin.Error, fin.SourceErrors)
	}
	if fin.Sources != 3 {
		t.Errorf("done.Sources = %d, want 3", fin.Sources)
	}
	got := pairSet(t, hits)
	want := map[string]bool{
		"acc/testijotain": true, "acc/omakori": true, "acc/julkinen": true,
		"toinentesti/testijotain": true,
		"oma/omakori":             true,
	}
	for p := range got {
		if !want[p] {
			t.Errorf("all-sources run leaked origin %q (configured: %v)", p, want)
		}
	}
	for p := range want {
		if !got[p] {
			t.Errorf("all-sources run missed origin %q (got %v)", p, got)
		}
	}
}

// TestSearchScopedBucketSourceByName: a whole-source search on a
// bucket-scoped source walks exactly its one bucket — not the endpoint's
// list, which sees the other sources' buckets too.
func TestSearchScopedBucketSourceByName(t *testing.T) {
	a := searchScopeApp(t, newUsageServer(t, searchScopeBuckets()...))

	hits, fin := runSearchToDone(t, a,
		SearchScope{Mode: "s3", Source: "toinentesti"}, SearchOptions{})
	if fin.Error != "" || fin.SourceErrors != "" {
		t.Fatalf("scoped run failed: %q / %q", fin.Error, fin.SourceErrors)
	}
	if len(hits) == 0 {
		t.Fatal("scoped run returned no hits")
	}
	for _, h := range hits {
		if h.Source != "toinentesti" || h.Bucket != "testijotain" {
			t.Errorf("hit %q stamped %s/%s — a bucket-scoped source walks only its own bucket",
				h.Entry.Key, h.Source, h.Bucket)
		}
	}
}

// TestSearchScopedBucketSourceByID: the pane binds sources by raw src-*
// id; the id resolution must carry the boundary with the canonical name.
func TestSearchScopedBucketSourceByID(t *testing.T) {
	a := searchScopeApp(t, newUsageServer(t, searchScopeBuckets()...))
	srcs, err := a.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, s := range srcs {
		if s.Name == "toinentesti" {
			id = s.ID
		}
	}
	if id == "" {
		t.Fatal("toinentesti has no id")
	}

	hits, _ := runSearchToDone(t, a,
		SearchScope{Mode: "s3", Source: id}, SearchOptions{})
	if len(hits) == 0 {
		t.Fatal("id-scoped run returned no hits")
	}
	for _, h := range hits {
		if h.Source != "toinentesti" || h.Bucket != "testijotain" {
			t.Errorf("hit %q stamped %s/%s — the id resolution lost the source boundary",
				h.Entry.Key, h.Source, h.Bucket)
		}
	}
}

// TestSearchScopedPrefixWalksTheFolder: a folder-scoped search walks the
// prefix itself, not the whole bucket — the engine used to drop the
// scope's prefix on the floor and sweep everything.
func TestSearchScopedPrefixWalksTheFolder(t *testing.T) {
	a := searchScopeApp(t, newUsageServer(t, searchScopeBuckets()...))

	hits, _ := runSearchToDone(t, a,
		SearchScope{Mode: "s3", Source: "acc", Bucket: "testijotain", Prefix: "sub"},
		SearchOptions{})
	if len(hits) == 0 {
		t.Fatal("prefix-scoped run returned no hits")
	}
	for _, h := range hits {
		key := h.Entry.Key
		if !strings.HasPrefix(key, "sub/") {
			t.Errorf("hit %q sits outside the sub/ prefix — the walk swept the whole bucket", key)
		}
	}
	saw := map[string]bool{}
	for _, h := range hits {
		saw[h.Entry.Key] = true
	}
	if !saw["sub/notes.md"] || !saw["sub/deep/x.md"] {
		t.Errorf("prefix run missed expected keys, saw %v", saw)
	}
	if saw["report.txt"] {
		t.Errorf("prefix run returned report.txt from outside the prefix")
	}
}

// TestSearchAccountWideExplicitBucket: the classic preset — an
// account-wide source with one bucket pinned — keeps walking that bucket
// alone.
func TestSearchAccountWideExplicitBucket(t *testing.T) {
	a := searchScopeApp(t, newUsageServer(t, searchScopeBuckets()...))

	hits, _ := runSearchToDone(t, a,
		SearchScope{Mode: "s3", Source: "acc", Bucket: "julkinen"}, SearchOptions{})
	if len(hits) == 0 {
		t.Fatal("explicit-bucket run returned no hits")
	}
	for _, h := range hits {
		if h.Bucket != "julkinen" {
			t.Errorf("hit %q from bucket %q, want julkinen only", h.Entry.Key, h.Bucket)
		}
	}
}
