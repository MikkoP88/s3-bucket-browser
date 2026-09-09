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

// These tests pin the streaming guarantee of PLAN.md §13: listing a huge
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
func newMockS3(t testing.TB, pages, pageSize int) *s3.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 0
		if tok := r.URL.Query().Get("continuation-token"); tok != "" {
			fmt.Sscanf(tok, "page-%d", &page)
		}
		var b strings.Builder
		b.WriteString(xml.Header)
		b.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
		fmt.Fprintf(&b, "<Name>bench</Name><Prefix></Prefix><KeyCount>%d</KeyCount><MaxKeys>%d</MaxKeys>", pageSize, pageSize)
		if last := page == pages-1; !last {
			fmt.Fprintf(&b, "<IsTruncated>true</IsTruncated><NextContinuationToken>page-%d</NextContinuationToken>", page+1)
		} else {
			b.WriteString("<IsTruncated>false</IsTruncated>")
		}
		base := page * pageSize
		for i := 0; i < pageSize; i++ {
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

// BenchmarkWalkDir100k tracks per-object listing cost (PLAN.md §13: CI
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
