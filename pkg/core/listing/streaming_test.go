package listing

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// These tests pin the streaming guarantee: listing a huge
// prefix through the callback APIs costs O(page) memory on the Go side.
// They run against an in-process httptest S3 (hermetic — no network, no
// keyring, no real bucket) that serves N pages of ListObjectsV2 XML.

const (
	memPages      = 250  // pages per walk
	memPageSize   = 1000 // one S3 page
	memTotal      = memPages * memPageSize
	maxHeapGrowth = 32 << 20 // streaming must stay far below this
)

// newMockS3 serves a flat bucket of pages*pageSize objects via paginated
// ListObjectsV2 (WalkDir's delimiter collapses nothing: keys have no "/").
// A start-after query narrows page 0 to keys strictly greater, carrying the
// running base in the continuation token — the wire grammar the windowed
// fetch rides.
func newMockS3(t testing.TB, pages, pageSize int) *s3.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := 0
		if tok := r.URL.Query().Get("continuation-token"); tok != "" {
			fmt.Sscanf(tok, "from-%d", &base)
		}
		if sa := r.URL.Query().Get("start-after"); sa != "" {
			var n int
			fmt.Sscanf(sa, "obj-%d.txt", &n)
			base = max(base, n+1)
		}
		total := pages * pageSize
		if base > total {
			base = total
		}
		var b strings.Builder
		b.WriteString(xml.Header)
		b.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
		count := min(total-base, pageSize)
		fmt.Fprintf(&b, "<Name>bench</Name><Prefix></Prefix><KeyCount>%d</KeyCount><MaxKeys>%d</MaxKeys>", count, pageSize)
		if base+count < total {
			fmt.Fprintf(&b, "<IsTruncated>true</IsTruncated><NextContinuationToken>from-%d</NextContinuationToken>", base+count)
		} else {
			b.WriteString("<IsTruncated>false</IsTruncated>")
		}
		for i := 0; i < count; i++ {
			fmt.Fprintf(&b,
				"<Contents><Key>obj-%08d.txt</Key><LastModified>2026-09-09T00:00:00.000Z</LastModified><ETag>&quot;%08d&quot;</ETag><Size>1024</Size><StorageClass>STANDARD</StorageClass></Contents>",
				base+i, base+i)
		}
		b.WriteString("</ListBucketResult>")
		w.Header().Set("Content-Type", "application/xml")
		io.WriteString(w, b.String()) // generated per request: the server itself retains nothing
	}))
	t.Cleanup(srv.Close)
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(srv.URL),
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		UsePathStyle: true,
	})
}

// heapLive returns the live heap after forced GCs (call twice so sweeping
// and finalizer noise settle).
func heapLive() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestWalkDirStreamingMemoryBounded(t *testing.T) {
	if testing.Short() {
		t.Skipf("walks %d objects; skipped in -short mode", memTotal)
	}
	ctx := context.Background()

	// Warm every lazy path (XML parser setup, resolver caches) so the
	// measured walks only measure the walk.
	warm := newMockS3(t, 1, memPageSize)
	if err := WalkDir(ctx, warm, "bench", "", Options{}, func(Entry) error { return nil }); err != nil {
		t.Fatal(err)
	}

	t.Run("discard stays flat", func(t *testing.T) {
		client := newMockS3(t, memPages, memPageSize)
		before := heapLive()
		n := 0
		err := WalkDir(ctx, client, "bench", "", Options{}, func(Entry) error { n++; return nil })
		if err != nil {
			t.Fatal(err)
		}
		after := heapLive()
		if n != memTotal {
			t.Fatalf("walked %d objects, want %d", n, memTotal)
		}
		grew := int64(after) - int64(before)
		t.Logf("heap growth walking %d objects (callback retains nothing): %.1f MB", n, float64(grew)/1e6)
		if grew > maxHeapGrowth {
			t.Fatalf("streaming walk grew the live heap by %.1f MB (> %d MB) — the listing accumulates instead of streaming",
				float64(grew)/1e6, maxHeapGrowth>>20)
		}
	})

	t.Run("retain-everything control exceeds the bound", func(t *testing.T) {
		// Guard against a vacuous bound: if even a callback that keeps
		// every entry stays under it, the object count no longer
		// discriminates and the first subtest proves nothing.
		client := newMockS3(t, memPages, memPageSize)
		before := heapLive()
		var keep []Entry
		err := WalkDir(ctx, client, "bench", "", Options{}, func(e Entry) error {
			keep = append(keep, e)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		after := heapLive()
		grew := int64(after) - int64(before)
		t.Logf("control (callback retains every entry): %.1f MB for %d entries", float64(grew)/1e6, len(keep))
		runtime.KeepAlive(keep)
		if grew <= maxHeapGrowth {
			t.Fatalf("control walk retained only %.1f MB — the %d MB bound no longer discriminates; raise memPages",
				float64(grew)/1e6, maxHeapGrowth>>20)
		}
	})
}

// TestWalkDirStartAfterWindow pins the windowed fetch: a walk starting
// strictly past key N answers exactly the keys after N, in order, with the
// delimiter grammar intact — the big-folder law's random-access half (a
// folder with more objects than the frontend materializes stays reachable
// at any depth through bounded windows).
func TestWalkDirStartAfterWindow(t *testing.T) {
	ctx := context.Background()
	const total = 1000
	client := newMockS3(t, 1, total)

	after := 499
	var got []string
	err := WalkDir(ctx, client, "bench", "", Options{StartAfter: fmt.Sprintf("obj-%08d.txt", after)}, func(e Entry) error {
		got = append(got, e.Name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != total-after-1 {
		t.Fatalf("window walked %d keys, want the %d after key %d", len(got), total-after-1, after)
	}
	if got[0] != fmt.Sprintf("obj-%08d.txt", after+1) || got[len(got)-1] != fmt.Sprintf("obj-%08d.txt", total-1) {
		t.Fatalf("window edges = %q..%q, want obj-%08d..obj-%08d strictly after %d",
			got[0], got[len(got)-1], after+1, total-1, after)
	}
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("window order broke at %d: %q after %q", i, got[i], got[i-1])
		}
	}

	// An empty StartAfter stays the full walk (the streaming API's own path).
	n := 0
	if err := WalkDir(ctx, client, "bench", "", Options{}, func(Entry) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	if n != total {
		t.Fatalf("bare walk = %d entries, want %d", n, total)
	}
}

// TestWalkDirTwoMillionKeys proves the streaming math at a count no real
// fixture will ever seed: the walk answers every key of a two-million-key
// prefix through pages the caller counts one at a time. The 100M law is
// the same arithmetic — pages of a bounded size, memory flat per page
// (TestWalkDirStreamingMemoryBounded pins the flatness) — only the page
// count grows.
func TestWalkDirTwoMillionKeys(t *testing.T) {
	if testing.Short() {
		t.Skipf("walks 2M objects; skipped in -short mode")
	}
	ctx := context.Background()
	const pages, pageSize = 2000, 1000
	client := newMockS3(t, pages, pageSize)

	n := 0
	var prev string
	err := WalkDir(ctx, client, "bench", "", Options{}, func(e Entry) error {
		if n > 0 && e.Name <= prev {
			t.Fatalf("key order broke at %d: %q after %q", n, e.Name, prev)
		}
		prev = e.Name
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != pages*pageSize {
		t.Fatalf("walked %d keys, want %d", n, pages*pageSize)
	}
}

// dirsMockS3 serves a tree-shaped bucket: common prefixes, folder
// markers, and plain objects across two pages — every shape WalkDirs must
// tell apart. The root listing is deliberately adversarial: markers that
// repeat their own common prefix (on the same page and on the page after)
// and objects hiding inside the rolled-up folders, the provider shapes
// the dedup law exists for. Any non-root prefix is answered honestly from
// the same key set (a leaf folder's single page, no roll-ups).
func dirsMockS3(t testing.TB) *s3.Client {
	t.Helper()
	keys := []string{"alpha/", "alpha/inner.txt", "beta/", "beta/x.txt", "docs/", "file-a.txt", "file-b.txt"}
	marker := func(k string) string {
		// markers lend their own metadata to the folder row
		return fmt.Sprintf("<Contents><Key>%s</Key><LastModified>2026-09-09T00:00:00.000Z</LastModified><ETag>&quot;m-%s&quot;</ETag><Size>0</Size><StorageClass>STANDARD</StorageClass></Contents>", k, k)
	}
	object := func(k string) string {
		return fmt.Sprintf("<Contents><Key>%s</Key><LastModified>2026-09-09T00:00:00.000Z</LastModified><ETag>&quot;o&quot;</ETag><Size>7</Size><StorageClass>STANDARD</StorageClass></Contents>", k)
	}
	page1 := `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Name>bench</Name><Prefix></Prefix><KeyCount>4</KeyCount><MaxKeys>1000</MaxKeys>
<IsTruncated>true</IsTruncated><NextContinuationToken>page2</NextContinuationToken>
<CommonPrefixes><Prefix>alpha/</Prefix></CommonPrefixes>
<CommonPrefixes><Prefix>beta/</Prefix></CommonPrefixes>
` + marker("alpha/") + marker("docs/") + `
</ListBucketResult>`
	page2 := `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Name>bench</Name><Prefix></Prefix><KeyCount>3</KeyCount><MaxKeys>1000</MaxKeys>
<IsTruncated>false</IsTruncated>
` + marker("beta/") + object("file-a.txt") + object("file-b.txt") + `
</ListBucketResult>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		prefix := r.URL.Query().Get("prefix")
		if prefix == "" {
			if r.URL.Query().Get("continuation-token") == "page2" {
				io.WriteString(w, page2)
			} else {
				io.WriteString(w, page1)
			}
			return
		}
		// honest single page for a deeper prefix: what S3 itself would say
		var cps, contents []string
		seenCp := map[string]bool{}
		for _, k := range keys {
			if !strings.HasPrefix(k, prefix) || k == prefix {
				continue
			}
			rest := k[len(prefix):]
			if i := strings.Index(rest, "/"); i >= 0 {
				cp := prefix + rest[:i+1]
				if !seenCp[cp] {
					seenCp[cp] = true
					cps = append(cps, fmt.Sprintf("<CommonPrefixes><Prefix>%s</Prefix></CommonPrefixes>", cp))
				}
				continue
			}
			contents = append(contents, object(k))
		}
		fmt.Fprintf(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Name>bench</Name><Prefix>%s</Prefix><KeyCount>%d</KeyCount><MaxKeys>1000</MaxKeys>
<IsTruncated>false</IsTruncated>%s%s
</ListBucketResult>`, prefix, len(cps)+len(contents), strings.Join(cps, ""), strings.Join(contents, ""))
	}))
	t.Cleanup(srv.Close)
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(srv.URL),
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		UsePathStyle: true,
	})
}

// TestWalkDirsStreamsOnlyFolders pins the tree's listing walk: common
// prefixes and folder markers are the only entries that stream — never an
// object row — and a folder appearing as both a common prefix and a
// marker (the same page or the page after) is emitted exactly once, the
// common prefix's plain entry winning (tree nodes carry no metadata, but
// a marker-only folder still lends its own).
func TestWalkDirsStreamsOnlyFolders(t *testing.T) {
	ctx := context.Background()
	client := dirsMockS3(t)

	var got []Entry
	err := WalkDirs(ctx, client, "bench", "", func(e Entry) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range got {
		if !e.IsDir {
			t.Fatalf("object row streamed to the tree walk: %+v", e)
		}
		names = append(names, e.Name)
	}
	// alpha (cp+marker same page) and beta (cp page 1, marker page 2) each
	// appear once; docs exists only as its marker.
	want := []string{"alpha", "beta", "docs"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("dirs = %v, want %v", names, want)
	}
	for _, e := range got {
		switch e.Name {
		case "docs":
			if e.ETag != "m-docs/" {
				t.Fatalf("marker-only folder lost the marker's metadata: %+v", e)
			}
		case "alpha":
			if e.ETag != "" {
				t.Fatalf("common prefix's first sighting borrowed the marker's metadata: %+v", e)
			}
		}
	}

	// a leaf folder (objects inside, no sub-folders) streams nothing.
	n := 0
	if err := WalkDirs(ctx, client, "bench", "alpha/", func(Entry) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("leaf walk emitted %d entries, want 0", n)
	}
}

// TestWalkDirsFlatBucketStreamsNothing pins the cost law at scale: a
// folder of ten thousand objects with no sub-folders costs the wire its
// full scan (every page must be fetched — folders can hide behind any key
// range) but the stream zero rows. The tree's lazy expansion stays cheap
// at any depth because of exactly this.
func TestWalkDirsFlatBucketStreamsNothing(t *testing.T) {
	if testing.Short() {
		t.Skipf("walks 10k objects; skipped in -short mode")
	}
	ctx := context.Background()
	client := newMockS3(t, 10, memPageSize)

	n := 0
	err := WalkDirs(ctx, client, "bench", "", func(Entry) error { n++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("flat bucket streamed %d dir rows, want 0", n)
	}
}

// BenchmarkWalkDir100M is the 100-million opt-in probe: the same lazy mock
// at one hundred thousand pages. Run with:
//
//	go test -bench WalkDir100M -run '^$' ./pkg/core/listing
//
// It proves nothing the 250k heap rig does not — it exists to answer "does
// the pipeline keep up at 100M?" with a wall-clock number when anyone asks.
func BenchmarkWalkDir100M(b *testing.B) {
	client := newMockS3(b, 100_000, memPageSize)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := 0
		if err := WalkDir(ctx, client, "bench", "", Options{}, func(Entry) error { n++; return nil }); err != nil {
			b.Fatal(err)
		}
		if n != 100_000_000 {
			b.Fatalf("n = %d, want 100000000", n)
		}
	}
}

// BenchmarkWalkDir100k tracks per-object listing cost (CI
// benchmark artifacts). Run with: go test -bench . -run '^$' ./pkg/core/listing
func BenchmarkWalkDir100k(b *testing.B) {
	client := newMockS3(b, 100, memPageSize)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := 0
		if err := WalkDir(ctx, client, "bench", "", Options{}, func(Entry) error { n++; return nil }); err != nil {
			b.Fatal(err)
		}
		if n != 100_000 {
			b.Fatalf("n = %d, want 100000", n)
		}
	}
}
