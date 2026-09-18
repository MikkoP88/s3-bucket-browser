// streamwatch_test.go pins the listing-stream watchdog contract: a dead
// endpoint (accepts connections, never answers) is cut off after
// listWatchdog and REPORTED as a timeout on the final page — never a
// silent done with a partial listing — while a slow-but-alive endpoint
// whose pages keep arriving resets the watchdog and completes however
// long the full walk takes.
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
)

// captureListPages routes EventListPage into a guardedslice while the
// test runs (the desktop event bus does not exist in unit tests).
func captureListPages(t *testing.T) (*sync.Mutex, *[]ListPage) {
	t.Helper()
	var mu sync.Mutex
	var pages []ListPage
	prevNoEvents := testNoEvents.Load()
	testNoEvents.Store(false)
	SetEventSink(func(event string, data ...any) {
		if event != EventListPage || len(data) == 0 {
			return
		}
		if p, ok := data[0].(ListPage); ok {
			mu.Lock()
			pages = append(pages, p)
			mu.Unlock()
		}
	})
	t.Cleanup(func() {
		SetEventSink(nil)
		testNoEvents.Store(prevNoEvents)
	})
	return &mu, &pages
}

func waitForDone(t *testing.T, mu *sync.Mutex, pages *[]ListPage) ListPage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*pages)
		var last ListPage
		if n > 0 {
			last = (*pages)[n-1]
		}
		mu.Unlock()
		if n > 0 && last.Done {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("stream never produced a done page")
	return ListPage{}
}

func blackholeClient(t *testing.T, url string) *s3client.Client {
	t.Helper()
	c, err := s3client.New(context.Background(), profile.Profile{
		Endpoint: url, Region: "us-east-1", PathStyle: true,
		AccessKeyID: "k", SecretKey: "s",
	}, s3client.Options{Timeout: -1})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestStreamWatchdogCutsDeadEndpoint(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // accept, never answer
	}))
	t.Cleanup(srv.Close)

	old := listWatchdog
	listWatchdog = 150 * time.Millisecond
	t.Cleanup(func() { listWatchdog = old })

	mu, pages := captureListPages(t)

	if _, err := a.streamObjects(blackholeClient(t, srv.URL), "b", ""); err != nil {
		t.Fatal(err)
	}
	last := waitForDone(t, mu, pages)

	if last.Error == "" || !strings.Contains(last.Error, "timed out") {
		t.Fatalf("done page error = %q, want a reported timeout", last.Error)
	}
	if len(last.Entries) != 0 {
		t.Fatalf("dead endpoint produced %d entries", len(last.Entries))
	}
	// The transient list task is gone once the stream ends (finish(...,
	// true) removes it — navigation would otherwise pile rows forever).
	for _, row := range a.tasks.snapshot() {
		if row.Kind == "list" {
			t.Fatalf("transient list task survived its stream: %+v", row)
		}
	}
}

// pagedXMLServer serves a three-page ListObjectsV2 walk, each response
// delayed past half the (shortened) watchdog: only the per-page reset
// keeps such a stream alive.
func pagedXMLServer(t *testing.T, pageDelay time.Duration) *httptest.Server {
	t.Helper()
	page := func(i int) string {
		next := ""
		if i < 2 {
			next = fmt.Sprintf("<NextContinuationToken>tok%d</NextContinuationToken>", i+1)
		}
		trunc := i < 2
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Name>b</Name><Prefix></Prefix><KeyCount>1</KeyCount><MaxKeys>1</MaxKeys>
<IsTruncated>%t</IsTruncated>%s
<Contents><Key>k%d.txt</Key><LastModified>2026-01-01T00:00:00.000Z</LastModified>
<ETag>&quot;a%d&quot;</ETag><Size>3</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`, trunc, next, i, i)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("continuation-token") {
		case "tok1":
			time.Sleep(pageDelay)
			fmt.Fprint(w, page(1))
		case "tok2":
			time.Sleep(pageDelay)
			fmt.Fprint(w, page(2))
		default:
			time.Sleep(pageDelay)
			fmt.Fprint(w, page(0))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamWatchdogResetsOnPages(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()

	// Three pages, 120ms apart, under a 200ms watchdog: the total walk
	// (360ms+) exceeds the watchdog, so this only completes when every
	// page resets it.
	srv := pagedXMLServer(t, 120*time.Millisecond)

	old := listWatchdog
	listWatchdog = 200 * time.Millisecond
	t.Cleanup(func() { listWatchdog = old })

	mu, pages := captureListPages(t)

	if _, err := a.streamObjects(blackholeClient(t, srv.URL), "b", ""); err != nil {
		t.Fatal(err)
	}
	last := waitForDone(t, mu, pages)

	if last.Error != "" {
		t.Fatalf("slow-but-alive stream errored: %q", last.Error)
	}
	total := 0
	mu.Lock()
	for _, p := range *pages {
		total += len(p.Entries)
	}
	mu.Unlock()
	if total != 3 {
		t.Fatalf("entries across pages = %d, want 3", total)
	}
}
