// xfer_retry_test.go: the retry contract. A settled job with failed
// items resubmits ONLY those items under skip semantics — what landed
// stays (skipped honestly, never re-copied), what failed gets another
// chance, a canceled job resumes its unfinished rows — and the refusals
// are honest: still running, nothing failed, no per-item rows, internal
// staging.
package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
)

// retryTuning pins the retry budget to one attempt per request so the
// fault rigs are deterministic: an injected 500 fails the file on its
// single try, and the healed wire passes on the retry's first one.
func retryTuning(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.SetTuning(30000, 300000, 1, 0, 0, 10000); err != nil {
		t.Fatal(err)
	}
}

// writeFiles seeds named files under dir and returns their paths.
func writeFiles(t *testing.T, dir string, names ...string) []string {
	t.Helper()
	out := make([]string, len(names))
	for i, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("content of "+n), 0o644); err != nil {
			t.Fatal(err)
		}
		out[i] = p
	}
	return out
}

// blockableS3 parks the FIRST PUT of one key until release closes — the
// cancel rig's lever (a wedged write the job's own cancellation must
// break); every later request passes straight through.
type blockableS3 struct {
	*fakeS3
	blockKey string
	release  chan struct{}
	parked   sync.Once
	closed   sync.Once
}

func (b *blockableS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key := splitS3Path(r.URL.Path)
	if r.Method == http.MethodPut && bucket+"/"+key == b.blockKey {
		b.parked.Do(func() { <-b.release })
	}
	b.fakeS3.ServeHTTP(w, r)
}

func (b *blockableS3) unblock() { b.closed.Do(func() { close(b.release) }) }

// mustUpload settles an upload and returns its verdict.
func mustUpload(t *testing.T, a *App, paths []string, bucket, policy string) (string, JobInfo) {
	t.Helper()
	id, err := a.Upload(paths, bucket, "", policy, 0, nil)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	return id, waitXferJob(t, a, id)
}

// TestRetryUploadFailedItems is the recovery story end to end: one file
// fails on an injected fault, the retry resubmits only that file under
// skip semantics, the healed wire lands it, and the original row keeps
// its honest failure.
func TestRetryUploadFailedItems(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	retryTuning(t, a)

	f := newFakeS3("up")
	if err := a.SaveSource(fakeS3Source("upsrc", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewSource("upsrc"); err != nil {
		t.Fatal(err)
	}
	paths := writeFiles(t, t.TempDir(), "good.txt", "bad.txt")
	f.fault("PUT", "up", "bad.txt", 1)

	id, ji := mustUpload(t, a, paths, "up", PolicyOverwrite)
	if ji.Status != JobError || ji.FailedFiles != 1 || ji.DoneFiles != 1 {
		t.Fatalf("job = %+v, want one failed and one done", ji)
	}
	nid, err := a.RetryTransfer(id)
	if err != nil {
		t.Fatal(err)
	}
	ji2 := waitXferJob(t, a, nid)
	if ji2.Status != JobDone || ji2.TotalFiles != 1 || ji2.DoneFiles != 1 {
		t.Fatalf("retry job = %+v, want exactly the failed file retried and landed", ji2)
	}
	if got := f.keys("up"); len(got) != 2 || got[0] != "bad.txt" || got[1] != "good.txt" {
		t.Fatalf("bucket keys = %v, want both files landed", got)
	}
	// The original row keeps its verdict — retry never rewrites history.
	for _, ji := range a.ActiveTransfers() {
		if ji.ID == id && (ji.Status != JobError || ji.FailedFiles != 1) {
			t.Fatalf("original row after retry = %+v, want its failure preserved", ji)
		}
	}
}

// TestRetryRefusedWhenRunning pins the guard: a running job has nothing
// settled to retry, and the refusal says so.
func TestRetryRefusedWhenRunning(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	retryTuning(t, a)

	blk := &blockableS3{fakeS3: newFakeS3("up"), blockKey: "up/slow.txt", release: make(chan struct{})}
	srv := httptest.NewServer(blk)
	t.Cleanup(srv.Close)
	t.Cleanup(blk.unblock) // registered after the server's close — the parked handler releases first
	if err := a.SaveSource(fakeS3Source("upsrc", srv.URL)); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewSource("upsrc"); err != nil {
		t.Fatal(err)
	}
	paths := writeFiles(t, t.TempDir(), "slow.txt")
	id, err := a.Upload(paths, "up", "", PolicyOverwrite, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RetryTransfer(id); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("err = %v, want the still-running refusal", err)
	}
	blk.unblock()
	if ji := waitXferJob(t, a, id); ji.Status != JobDone {
		t.Fatalf("job = %+v, want it to settle cleanly after unblocking", ji)
	}
}

// TestRetryNothingFailed refuses a clean job: retry is for failures, not
// a re-run button.
func TestRetryNothingFailed(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	retryTuning(t, a)

	f := newFakeS3("up")
	if err := a.SaveSource(fakeS3Source("upsrc", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewSource("upsrc"); err != nil {
		t.Fatal(err)
	}
	id, ji := mustUpload(t, a, writeFiles(t, t.TempDir(), "fine.txt"), "up", PolicyOverwrite)
	if ji.Status != JobDone {
		t.Fatalf("job = %+v, want a clean settle", ji)
	}
	if _, err := a.RetryTransfer(id); err == nil || !strings.Contains(err.Error(), "nothing failed") {
		t.Fatalf("err = %v, want the nothing-failed refusal", err)
	}
}

// TestRetryCanceledResumesUnfinished pins the resume rule: canceling
// mid-file leaves the in-flight row unsettled and later rows unstarted,
// and the retry carries exactly those — the landed file is not re-sent.
func TestRetryCanceledResumesUnfinished(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	retryTuning(t, a)

	blk := &blockableS3{fakeS3: newFakeS3("up"), blockKey: "up/mid.txt", release: make(chan struct{})}
	srv := httptest.NewServer(blk)
	t.Cleanup(srv.Close)
	t.Cleanup(blk.unblock)
	if err := a.SaveSource(fakeS3Source("upsrc", srv.URL)); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewSource("upsrc"); err != nil {
		t.Fatal(err)
	}
	paths := writeFiles(t, t.TempDir(), "first.txt", "mid.txt", "last.txt")
	id, err := a.Upload(paths, "up", "", PolicyOverwrite, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	// first.txt lands, mid.txt parks on the blocked PUT — cancel there,
	// once the parked file is verifiably the in-flight one.
	for ji := jobOf(t, a, id); filepath.Base(ji.CurrentFile) != "mid.txt"; ji = jobOf(t, a, id) {
	}
	if !a.CancelTransfer(id) {
		t.Fatal("cancel did not reach the running job")
	}
	ji := waitXferJob(t, a, id)
	if ji.Status != JobCanceled {
		t.Fatalf("job = %+v, want canceled", ji)
	}
	blk.unblock()
	nid, err := a.RetryTransfer(id)
	if err != nil {
		t.Fatal(err)
	}
	ji2 := waitXferJob(t, a, nid)
	if ji2.Status != JobDone || ji2.TotalFiles != 2 {
		t.Fatalf("retry job = %+v, want the two unfinished files resumed and landed", ji2)
	}
	if got := blk.fakeS3.keys("up"); len(got) != 3 {
		t.Fatalf("bucket keys = %v, want all three files landed after the resume", got)
	}
}

// jobOf snapshots one job's current state.
func jobOf(t *testing.T, a *App, id string) JobInfo {
	t.Helper()
	for _, ji := range a.ActiveTransfers() {
		if ji.ID == id {
			return ji
		}
	}
	t.Fatalf("job %s vanished", id)
	return JobInfo{}
}

// mendableFS lies exactly once: the first open of the target path
// serves a stream short of the size it announced (the truncated-wire
// shape), every later one serves the repaired source whole — the
// operator fixed the remote file between the failed attempt and the
// retry, which lands it. (Named apart from production's healingFS, the
// engine-cache connection healer.)
type mendableFS struct {
	remotefs.FS
	mu    sync.Mutex
	lie   string // the path that lies
	claim int64
	body  string
	fixed string // the whole body once the source is repaired
}

func (h *mendableFS) Stat(ctx context.Context, p string) (listing.Entry, error) {
	h.mu.Lock()
	claim := h.claim
	lying := p == h.lie
	h.mu.Unlock()
	if lying {
		return listing.Entry{Key: p, Name: path.Base(strings.TrimPrefix(p, "/")), Size: claim}, nil
	}
	return h.FS.Stat(ctx, p)
}

func (h *mendableFS) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p != h.lie {
		return h.FS.Open(ctx, p)
	}
	// This open rides the truncated wire; every one after serves the
	// repaired source.
	size, body := h.claim, h.body
	h.claim, h.body = int64(len(h.fixed)), h.fixed
	return io.NopCloser(strings.NewReader(body)), size, nil
}

// TestRetryTransferRecoversFailedItem runs the recovery story through
// the cross-source convergence: one item fails on a lying wire, the
// retry resubmits only that item, the healed wire lands it whole.
func TestRetryTransferRecoversFailedItem(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	liarSrc, liarRoot := emptyLocalSource(t, "liar")
	liarSrc.ID = "src-heal-stub" // engine cache keys by ID; own it so the seed matches the saved source
	if err := a.SaveSource(liarSrc); err != nil {
		t.Fatal(err)
	}
	destSrc, destRoot := emptyLocalSource(t, "vault")
	if err := a.SaveSource(destSrc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(liarRoot, "ok.txt"), []byte("honest"), 0o644); err != nil {
		t.Fatal(err)
	}
	inner, err := remotefs.NewLocal(context.Background(), liarSrc.LocalRoot)
	if err != nil {
		t.Fatal(err)
	}
	healer := &mendableFS{FS: inner, lie: "/big.bin", claim: 100, body: strings.Repeat("x", 40), fixed: strings.Repeat("x", 100)}
	// Pre-seed the engine cache: the remote leg speaks to the healing
	// Open instead of dialing a fresh engine.
	a.engMu.Lock()
	a.engines[liarSrc.ID] = healer
	a.engMu.Unlock()

	id, err := a.TransferCross([]XferItem{
		{Source: "liar", Key: "/ok.txt"},
		{Source: "liar", Key: "/big.bin", Size: 100},
	}, nil, XferDest{Kind: "remote", Source: "vault", Dir: "/"}, PolicyOverwrite, 0, false, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	ji := waitXferJob(t, a, id)
	if ji.Status != JobError || ji.FailedFiles != 1 || ji.DoneFiles != 1 {
		t.Fatalf("job = %+v, want the lying item failed and the honest one done", ji)
	}

	nid, err := a.RetryTransfer(id)
	if err != nil {
		t.Fatal(err)
	}
	ji2 := waitXferJob(t, a, nid)
	if ji2.Status != JobDone || ji2.TotalFiles != 1 {
		t.Fatalf("retry job = %+v, want only the failed item retried and landed", ji2)
	}
	if got := read(t, filepath.Join(destRoot, "big.bin")); got != strings.Repeat("x", 100) {
		t.Fatalf("landed content = %d bytes, want the healed whole file", len(got))
	}
	if _, err := os.Stat(filepath.Join(destRoot, "ok.txt")); err != nil {
		t.Fatalf("first attempt's honest file must still stand: %v", err)
	}
}

// TestRetryDownloadFailedItems rides the same contract through the
// download entry: the faulted GET fails its item, the retry lands it.
func TestRetryDownloadFailedItems(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	retryTuning(t, a)

	f := newFakeS3("dn")
	f.seed("dn", "fine.txt", "fine")
	f.seed("dn", "broken.txt", "broken")
	if err := a.SaveSource(fakeS3Source("dnsrc", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewSource("dnsrc"); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	f.fault("GET", "dn", "broken.txt", 1)
	id, err := a.Download("dn", []DownloadItem{{Key: "fine.txt", Size: 4}, {Key: "broken.txt", Size: 6}}, dest, PolicyOverwrite, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	ji := waitXferJob(t, a, id)
	if ji.Status != JobError || ji.FailedFiles != 1 || ji.DoneFiles != 1 {
		t.Fatalf("job = %+v, want one failed and one done", ji)
	}
	nid, err := a.RetryTransfer(id)
	if err != nil {
		t.Fatal(err)
	}
	ji2 := waitXferJob(t, a, nid)
	if ji2.Status != JobDone || ji2.TotalFiles != 1 || ji2.DoneFiles != 1 {
		t.Fatalf("retry job = %+v, want exactly the faulted object retried and landed", ji2)
	}
	if got := read(t, filepath.Join(dest, "broken.txt")); got != "broken" {
		t.Fatalf("landed content = %q, want the healed object", got)
	}
}
