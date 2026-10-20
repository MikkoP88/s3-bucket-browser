// streamwatch_test.go pins the listing-stream watchdog contract: a dead
// endpoint (accepts connections, never answers) is cut off after the
// watchdog budget and REPORTED as a timeout on the final page — never a
// silent done with a partial listing — while a slow-but-alive endpoint
// whose pages keep arriving resets the watchdog and completes however
// long the full walk takes. The budget is the Settings → Network listing
// timeout, so the tests shorten it through the tuning file in the
// isolated S3B_CONFIG dir newTestApp sets up — the same path a user's
// Settings change takes.
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

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/appsettings"
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

// shortWatchdog shrinks the listing watchdog to ms milliseconds by
// writing the tuning file into newTestApp's isolated S3B_CONFIG dir —
// the exact path a Settings → Network change takes in the real app.
func shortWatchdog(t *testing.T, ms int) {
	t.Helper()
	if err := appsettings.Save(appsettings.Tuning{ListingTimeoutMS: ms}); err != nil {
		t.Fatal(err)
	}
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

	shortWatchdog(t, 150)

	mu, pages := captureListPages(t)

	if _, err := a.streamObjects(blackholeClient(t, srv.URL), "b", "", ""); err != nil {
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

	shortWatchdog(t, 200)

	mu, pages := captureListPages(t)

	if _, err := a.streamObjects(blackholeClient(t, srv.URL), "b", "", ""); err != nil {
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

// startAfterXMLServer serves N sequential keys w-prefixed, one per page,
// honoring start-after on page 0 — the window grammar ListObjectsWindow
// rides. The window's own continuation pages carry no start-after (the
// token does the walking), so the server only applies it when the request
// carries no continuation-token, exactly like the real wire.
func startAfterXMLServer(t *testing.T, n int) *httptest.Server {
	t.Helper()
	page := func(i int) string {
		next := ""
		if i < n-1 {
			next = fmt.Sprintf("<NextContinuationToken>tok%d</NextContinuationToken>", i)
		}
		trunc := i < n-1
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Name>b</Name><Prefix></Prefix><KeyCount>1</KeyCount><MaxKeys>1</MaxKeys>
<IsTruncated>%t</IsTruncated>%s
<Contents><Key>w%03d.txt</Key><LastModified>2026-01-01T00:00:00.000Z</LastModified>
<ETag>&quot;%03d&quot;</ETag><Size>3</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`, trunc, next, i, i)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := 0
		if tok := r.URL.Query().Get("continuation-token"); tok != "" {
			fmt.Sscanf(tok, "tok%d", &i)
			fmt.Fprint(w, page(i+1))
			return
		}
		if sa := r.URL.Query().Get("start-after"); sa != "" {
			fmt.Sscanf(sa, "w%d.txt", &i)
			fmt.Fprint(w, page(i+1)) // strictly past the named key
			return
		}
		fmt.Fprint(w, page(i))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// dirsXMLServer serves one page of the tree's listing shape — two common
// prefixes, one marker-only folder, and a plain object WalkDirs must
// never stream to the sidebar.
func dirsXMLServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Name>b</Name><Prefix></Prefix><KeyCount>4</KeyCount><MaxKeys>1000</MaxKeys>
<IsTruncated>false</IsTruncated>
<CommonPrefixes><Prefix>a/</Prefix></CommonPrefixes>
<CommonPrefixes><Prefix>c/</Prefix></CommonPrefixes>
<Contents><Key>b/</Key><LastModified>2026-01-01T00:00:00.000Z</LastModified>
<ETag>&quot;mb&quot;</ETag><Size>0</Size><StorageClass>STANDARD</StorageClass></Contents>
<Contents><Key>plain.txt</Key><LastModified>2026-01-01T00:00:00.000Z</LastModified>
<ETag>&quot;mp&quot;</ETag><Size>4</Size><StorageClass>STANDARD</StorageClass></Contents>
</ListBucketResult>`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestListSourceDirsStream pins the tree's binding end to end: one named
// S3 source's prefix streams its FOLDERS only — no object row ever
// arrives — and the walk still lands the always-final done page with the
// running total counting dirs alone.
func TestListSourceDirsStream(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	srv := dirsXMLServer(t)
	if err := a.SaveSource(fakeS3Source("tree", srv.URL)); err != nil {
		t.Fatal(err)
	}

	mu, pages := captureListPages(t)
	if _, err := a.ListSourceDirsStream("tree", "b", ""); err != nil {
		t.Fatal(err)
	}
	last := waitForDone(t, mu, pages)
	if last.Error != "" {
		t.Fatalf("dirs stream errored: %q", last.Error)
	}

	mu.Lock()
	names := []string{}
	for _, p := range *pages {
		for _, e := range p.Entries {
			if !e.IsDir {
				t.Fatalf("object row streamed to the tree: %+v", e)
			}
			names = append(names, e.Name)
		}
	}
	mu.Unlock()
	// emission order is the walk's law, not sorted: common prefixes lead
	// within a page, marker-only folders follow (the tree sorts its nodes).
	if strings.Join(names, ",") != "a,c,b" {
		t.Fatalf("dirs = %v, want [a c b] (prefixes lead, markers follow)", names)
	}
	if last.Total != len(names) {
		t.Fatalf("total = %d, want %d (dirs alone)", last.Total, len(names))
	}
}

// TestStreamWindowStartsAfter pins the windowed-fetch binding: the stream
// answers exactly the keys strictly past startAfter, in arrival order, and
// the running Total counts only the window — the big-folder law's
// random-access half. A bare stream over the same server still walks from
// the first key (ListObjectsStream's own path).
func TestStreamWindowStartsAfter(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	srv := startAfterXMLServer(t, 6)
	c := blackholeClient(t, srv.URL)

	mu, pages := captureListPages(t)
	if _, err := a.streamObjects(c, "b", "", "w001.txt"); err != nil {
		t.Fatal(err)
	}
	last := waitForDone(t, mu, pages)
	mu.Lock()
	got := []string{}
	for _, p := range *pages {
		for _, e := range p.Entries {
			got = append(got, e.Name)
		}
	}
	mu.Unlock()
	want := []string{"w002.txt", "w003.txt", "w004.txt", "w005.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("window keys = %v, want %v", got, want)
	}
	if last.Total != len(want) {
		t.Fatalf("window total = %d, want %d", last.Total, len(want))
	}

	// the bare stream walks the whole set from the first key
	mu2, pages2 := captureListPages(t)
	if _, err := a.streamObjects(c, "b", "", ""); err != nil {
		t.Fatal(err)
	}
	waitForDone(t, mu2, pages2)
	mu2.Lock()
	n := 0
	for _, p := range *pages2 {
		n += len(p.Entries)
	}
	mu2.Unlock()
	if n != 6 {
		t.Fatalf("bare stream entries = %d, want 6", n)
	}
}
