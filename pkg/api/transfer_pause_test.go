package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/pprof"
	"testing"
	"time"
)

// holdFake wraps the one-bucket fake so one chosen PUT blocks on a
// channel: the rig parks the job's in-flight file exactly where it
// wants it, then releases the wire and watches the between-files gate.
type holdFake struct {
	*fakeS3
	key     string
	arrived chan struct{} // closed once the held PUT lands on the wire
	release chan struct{}
}

func (h *holdFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key := splitS3Path(r.URL.Path)
	if r.Method == http.MethodPut && bucket+"/"+key == h.key {
		select {
		case <-h.arrived:
		default:
			close(h.arrived)
		}
		<-h.release
	}
	h.fakeS3.ServeHTTP(w, r)
}

// pauseRigApp boots an app whose default client serves the holding fake.
func pauseRigApp(t *testing.T, holdKey string) (*App, *holdFake, *fakeS3) {
	t.Helper()
	a := newTestApp(t)
	a.Startup(context.Background())
	f := newFakeS3("docs")
	h := &holdFake{fakeS3: f, key: holdKey,
		arrived: make(chan struct{}), release: make(chan struct{})}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	f.mu.Lock()
	f.url = srv.URL
	f.mu.Unlock()
	if err := a.SaveSource(fakeS3Source("pausebox", srv.URL)); err != nil {
		t.Fatal(err)
	}
	return a, h, f
}

func pauseRigFiles(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	files := []struct{ name, body string }{
		{"a.txt", "alpha"}, {"b.txt", "bravo"}, {"c.txt", "charlie"},
	}
	paths := make([]string, 0, len(files))
	for _, fc := range files {
		p := filepath.Join(dir, fc.name)
		if err := os.WriteFile(p, []byte(fc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

func waitJobPhase(t *testing.T, a *App, id, phase string) JobInfo {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, ji := range a.ActiveTransfers() {
			if ji.ID == id && ji.Status == JobRunning && ji.Phase == phase {
				return ji
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s never reached phase %q", id, phase)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitJobStatus(t *testing.T, a *App, id, status string, patience ...time.Duration) JobInfo {
	t.Helper()
	wait := 5 * time.Second // the shipped patience; a faulted probe pays the SDK retryer’s own backoff and needs more
	if len(patience) > 0 {
		wait = patience[0]
	}
	deadline := time.Now().Add(wait)
	for {
		for _, ji := range a.ActiveTransfers() {
			if ji.ID == id && ji.Status == status {
				return ji
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s never settled %q", id, status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitRemoteKey polls the fake until the key holds exactly want (the held
// PUT's settle is asynchronous with the phase flip since pause turned
// instant — a rig that needs the wire settled waits for the wire, not the
// row).
func waitRemoteKey(t *testing.T, f *fakeS3, key, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := remoteContent(t, f, key); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("key %s never settled its %d bytes", key, len(want))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The between-files gate: pause while a file sits mid-wire, and that file
// still settles (a stream is never abandoned), but nothing new starts —
// the row says "paused", Stalled stays off it, the wire goes quiet, and
// resume walks on at the very file that was next.
func TestTransferPauseResume(t *testing.T) {
	a, h, f := pauseRigApp(t, "docs/b.txt")
	paths := pauseRigFiles(t)
	mu, loglines := captureLogLines(t)
	id, err := a.Upload(paths, "docs", "", PolicyOverwrite, 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	<-h.arrived // b.txt is the in-flight file, parked on the wire
	if !a.PauseTransfer(id) {
		t.Fatal("PauseTransfer(running, flowing) = false, want true")
	}
	if a.PauseTransfer(id) {
		t.Fatal("second PauseTransfer reported true — a parked job must be a no-op")
	}
	close(h.release) // let b.txt finish; the next gate must hold c.txt

	// the phase flip is instant now (the row answers the click), so the
	// wire settle is its own wait — b.txt must land before the gate speaks
	waitRemoteKey(t, f, "b.txt", "bravo")
	paused := waitJobPhase(t, a, id, PhasePaused)
	if paused.Stalled {
		t.Fatal("paused row flagged Stalled — the park is a decision, not a stall")
	}
	if got := remoteContent(t, f, "b.txt"); got != "bravo" {
		t.Fatalf("in-flight file = %q after release, want the settled bytes", got)
	}
	if got := remoteContent(t, f, "a.txt"); got != "alpha" {
		t.Fatalf("first file = %q, want it landed before the park", got)
	}
	if n := len(f.keys("docs")); n != 2 {
		t.Fatalf("paused wire holds %d file(s), want exactly the two settled — the gate must hold c.txt", n)
	}
	time.Sleep(300 * time.Millisecond) // the quiet beat: the gate holds
	if n := len(f.keys("docs")); n != 2 {
		t.Fatalf("wire grew to %d file(s) while paused", n)
	}

	if !a.ResumeTransfer(id) {
		t.Fatal("ResumeTransfer(paused) = false, want true")
	}
	done := waitJobStatus(t, a, id, JobDone)
	if done.DoneFiles != 3 || done.FailedFiles != 0 {
		t.Fatalf("resumed job = %d done / %d failed, want 3/0", done.DoneFiles, done.FailedFiles)
	}
	for k, want := range map[string]string{"a.txt": "alpha", "b.txt": "bravo", "c.txt": "charlie"} {
		if got := remoteContent(t, f, k); got != want {
			t.Fatalf("key %s = %q after resume, want %q", k, got, want)
		}
	}

	// the park and the wake each said so under the job's own source tag
	mu.Lock()
	pausedN, resumedN := 0, 0
	for _, l := range *loglines {
		if l.Scope != "transfer" || l.Source != "docs" || l.Level != LogInfo {
			continue
		}
		switch l.Message {
		case "job " + id + " paused":
			pausedN++
		case "job " + id + " resumed":
			resumedN++
		}
	}
	mu.Unlock()
	if pausedN != 1 || resumedN != 1 {
		t.Fatalf("pause/resume log lines = %d/%d, want exactly 1/1", pausedN, resumedN)
	}

	// settled and unknown rows accept neither verb
	if a.PauseTransfer(id) {
		t.Fatal("PauseTransfer(done) = true")
	}
	if a.ResumeTransfer(id) {
		t.Fatal("ResumeTransfer(done) = true")
	}
	if a.PauseTransfer("nope") || a.ResumeTransfer("nope") {
		t.Fatal("unknown job id accepted")
	}
}

// Cancel reaches a parked job too: the gate hands back the context error
// and the row settles canceled with the un-started files un-started.
func TestTransferPauseThenCancel(t *testing.T) {
	a, h, f := pauseRigApp(t, "docs/b.txt")
	paths := pauseRigFiles(t)
	id, err := a.Upload(paths, "docs", "", PolicyOverwrite, 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	<-h.arrived
	if !a.PauseTransfer(id) {
		t.Fatal("PauseTransfer(running, flowing) = false")
	}
	close(h.release)
	// the phase flip is instant — the cancel must wait for the in-flight
	// file to settle or the count loses it
	waitRemoteKey(t, f, "b.txt", "bravo")
	waitJobPhase(t, a, id, PhasePaused)
	if !a.CancelTransfer(id) {
		t.Fatal("CancelTransfer(paused) = false")
	}
	settled := waitJobStatus(t, a, id, JobCanceled)
	if settled.DoneFiles != 2 {
		t.Fatalf("canceled-from-pause job = %d done, want the two settled files only", settled.DoneFiles)
	}
	time.Sleep(200 * time.Millisecond) // nothing may move after the settle
	if len(f.keys("docs")) != 2 {
		t.Fatalf("wire grew past the two settled files after cancel")
	}
}

// The mid-file brake: pause a single throttled upload while its only file
// is on the wire and the stream itself parks — progress freezes between
// reads (not at a file boundary), the phase answers the click instantly,
// Stalled stays off the parked row, nothing lands while parked, and the
// resumed file is byte-exact. This is the law the Pause button owes the
// user: a pause that waits for a large in-flight file to finish reads as
// "pause does nothing".
func TestTransferPauseMidFile(t *testing.T) {
	a, _, f := pauseRigApp(t, "") // no held key — the throttle is the brake
	// The body must stay UNDER the uploader's PartSize floor (5 MiB even at
	// SDK defaults) or the engine splits into multipart — a path the fake
	// does not serve, and a variable the rig must not ride. Single PUT,
	// deterministically: 2 MiB at 512 KiB/s streams ~4s with reads parked
	// every copy-buffer fill.
	body := make([]byte, 2<<20)
	for i := range body {
		body[i] = byte(i*31 + i>>16) // deterministic, not all-zero
	}
	dir := t.TempDir()
	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, body, 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := a.Upload([]string{big}, "docs", "", PolicyOverwrite, 512<<10, nil) // 512 KiB/s
	if err != nil {
		t.Fatal(err)
	}

	// wait until the file is provably mid-wire, then click pause
	snap := func() JobInfo {
		for _, ji := range a.ActiveTransfers() {
			if ji.ID == id {
				return ji
			}
		}
		return JobInfo{}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		ji := snap()
		if ji.Status == JobRunning && ji.CurrentSent > 128*1024 && ji.CurrentSent < ji.CurrentTotal {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never showed mid-file progress: %+v", snap())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !a.PauseTransfer(id) {
		t.Fatal("PauseTransfer(mid-file, running) = false")
	}
	// the phase answers the click at once — no boundary wait
	if ji := snap(); ji.Phase != PhasePaused {
		t.Fatalf("phase %q right after PauseTransfer, want the instant paused", ji.Phase)
	}

	// the stream parks between reads: progress freezes while the file is
	// provably part-sent. The freeze oracle must out-live the rate limiter's
	// own lulls: a read of n bytes sleeps up to n/maxBPS before its callback
	// runs, so any one read can hold the counters still for at most
	// fileSize/maxBPS — a value unchanged longer than that bound (plus slack)
	// with the phase parked can only be the park itself.
	settled := func() (JobInfo, bool) {
		ji := snap()
		return ji, ji.Status == JobRunning && ji.Phase == PhasePaused
	}
	quietBound := time.Duration(int64(time.Second) * int64(len(body)) / (512 << 10))
	quietBound += 500 * time.Millisecond
	var frozen int64
	stableFor := time.Duration(0)
	lastSaw := time.Now()
	deadline = time.Now().Add(12 * time.Second)
	for {
		ji, ok := settled()
		if ok && frozen > 0 {
			if ji.CurrentSent == frozen {
				stableFor = time.Since(lastSaw)
				if stableFor >= quietBound {
					break
				}
			} else {
				frozen = ji.CurrentSent
				lastSaw = time.Now()
			}
		} else if ok {
			frozen = ji.CurrentSent
			lastSaw = time.Now()
		}
		if time.Now().After(deadline) {
			t.Fatalf("stream never parked: %+v", snap())
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond) // the quiet beat: parked means parked
	ji, ok := settled()
	if !ok {
		t.Fatalf("parked row moved: %+v", snap())
	}
	if ji.SentBytes != frozen {
		t.Fatalf("SentBytes moved while parked mid-file: %d -> %d", frozen, ji.SentBytes)
	}
	if ji.Stalled {
		t.Fatal("parked mid-file row flagged Stalled — the park is a decision, not a stall")
	}
	if ji.CurrentSent <= 0 || ji.CurrentSent >= ji.CurrentTotal {
		t.Fatalf("park boundary = %d/%d, want strictly mid-file", ji.CurrentSent, ji.CurrentTotal)
	}
	if len(f.keys("docs")) != 0 {
		t.Fatalf("wire holds %d object(s) while parked, want none — the file must not land", len(f.keys("docs")))
	}

	if !a.ResumeTransfer(id) {
		t.Fatal("ResumeTransfer(parked mid-file) = false")
	}
	// a resumed stream owes its remaining bytes at the throttle: the whole
	// tail is bounded by file/maxBPS plus the wake, so 15s is generous — and
	// a miss dumps the goroutines, because a stream that never finishes is
	// a stuck goroutine somewhere and its stack is the witness.
	done, ok := JobInfo{}, false
	deadline = time.Now().Add(15 * time.Second)
	for {
		for _, ji := range a.ActiveTransfers() {
			if ji.ID == id {
				done, ok = ji, ji.Status == JobDone
			}
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
			t.Fatalf("resumed job never finished: %+v", done)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if done.DoneFiles != 1 || done.FailedFiles != 0 {
		t.Fatalf("resumed mid-file job = %d done / %d failed, want 1/0", done.DoneFiles, done.FailedFiles)
	}
	if got := remoteContent(t, f, "big.bin"); got != string(body) {
		t.Fatalf("resumed file differs: %d bytes on the wire, want %d", len(got), len(body))
	}
}
