// xfer_react_test.go pins the transfer reactivity law: TransferCross
// answers in the time of a registry read — the job registers (badge,
// transfers-window row, cancel verb) the moment its wire-free refusals
// pass, the plan walk runs inside the registered job in the counting
// phase, a cancel mid-walk aborts it, and the pre-check's finished plan
// is handed to the follow-up start instead of walked a second time.
// Hermetic: the in-memory fakeS3 over httptest, wrapped with a delay
// and a request counter (the dispatch is split out of serve for exactly
// this), local destinations over temp dirs.
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// reactS3 wraps the fake with the reactivity rig's two levers: every
// HEAD (and optionally GET) pays a delay before the fake answers, and
// every request is counted per verb so the rigs can prove what ran
// (one walk, not two) and what never did.
type reactS3 struct {
	*fakeS3
	delay   time.Duration
	getsToo bool

	mu     sync.Mutex
	counts map[string]int
}

func (s *reactS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	verb := r.Method
	if r.URL.Query().Get("list-type") == "2" {
		verb = "LIST"
	}
	s.mu.Lock()
	s.counts[verb]++
	s.mu.Unlock()
	if r.Method == http.MethodHead || (s.getsToo && r.Method == http.MethodGet) {
		select {
		case <-time.After(s.delay):
		case <-r.Context().Done(): // the client walked away — stop holding the handler
		}
	}
	s.fakeS3.ServeHTTP(w, r)
}

// serve seats the WRAPPER as the endpoint's handler — fakeS3.serve would
// bind the embedded fake directly and the levers would never see a
// request.
func (s *reactS3) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	s.fakeS3.mu.Lock()
	s.fakeS3.url = srv.URL
	s.fakeS3.mu.Unlock()
	return srv.URL
}

func (s *reactS3) counted(verb string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[verb]
}

// waitJobSettled polls until the job leaves "running" and returns its
// final info — planning failures settle on the row, so the rigs wait
// for any verdict rather than one named status.
func waitJobSettled(t *testing.T, a *App, id string, patience ...time.Duration) JobInfo {
	t.Helper()
	wait := 5 * time.Second
	if len(patience) > 0 {
		wait = patience[0]
	}
	deadline := time.Now().Add(wait)
	for {
		for _, ji := range a.ActiveTransfers() {
			if ji.ID == id && ji.Status != JobRunning {
				return ji
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s never settled", id)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// reactApp seats one app over the wrapped fake and makes it the view
// source, the seating every cross-transfer rig starts from.
func reactApp(t *testing.T, s *reactS3) *App {
	t.Helper()
	a := newTestApp(t)
	a.Startup(context.Background())
	if err := a.SaveSource(fakeS3Source("store", s.serve(t))); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewSource("store"); err != nil {
		t.Fatal(err)
	}
	return a
}

// TestTransferCrossRegistersInstantly pins the law the round is named
// for: while the plan's own HEAD is still sleeping on the wire, the
// verb has already returned — and the returned job is right there,
// running in the counting phase with unknown totals — then the walk
// lands, the totals seat, and the file arrives.
func TestTransferCrossRegistersInstantly(t *testing.T) {
	s := &reactS3{fakeS3: newFakeS3("up"), delay: 400 * time.Millisecond, counts: map[string]int{}}
	s.seed("up", "big.bin", "hello world")
	a := reactApp(t, s)

	dest := XferDest{Kind: "local", Dir: t.TempDir()}
	// Size 0 is the clipboard's honest shape: the planner must HEAD the
	// object to learn it — the exact request the delay parks behind.
	t0 := time.Now()
	id, err := a.TransferCross([]XferItem{{Bucket: "up", Key: "big.bin", Size: 0}}, nil,
		dest, PolicyOverwrite, 0, false, nil, false)
	if err != nil || id == "" {
		t.Fatalf("TransferCross: id=%q err=%v", id, err)
	}
	if dt := time.Since(t0); dt > 200*time.Millisecond {
		t.Fatalf("TransferCross returned after %v — the verb must answer long before the %v walk does", dt, s.delay)
	}
	// The row exists NOW, mid-walk: running, counting, totals honestly
	// unknown — never an invisible void between gesture and first row.
	for _, ji := range a.ActiveTransfers() {
		if ji.ID != id {
			continue
		}
		if ji.Status != JobRunning {
			t.Fatalf("mid-plan status = %q, want running", ji.Status)
		}
		if ji.Phase != TaskPhaseCount {
			t.Fatalf("mid-plan phase = %q, want %q", ji.Phase, TaskPhaseCount)
		}
		if ji.TotalFiles != 0 {
			t.Fatalf("mid-plan TotalFiles = %d, want 0 until the walk lands", ji.TotalFiles)
		}
	}
	ji := waitJobSettled(t, a, id)
	if ji.Status != JobDone || ji.TotalFiles != 1 || ji.TotalBytes != int64(len("hello world")) {
		t.Fatalf("job = %+v, want one file landed with its true totals", ji)
	}
	got, err := os.ReadFile(filepath.Join(dest.Dir, "big.bin"))
	if err != nil || string(got) != "hello world" {
		t.Fatalf("landed = %q (%v), want the seeded content", got, err)
	}
}

// TestTransferCrossCancelDuringPlanning pins the other half of the
// reactivity law: the cancel verb works from the first millisecond —
// the walk aborts mid-HEAD, the job settles canceled long before the
// parked request would have answered, and nothing lands.
func TestTransferCrossCancelDuringPlanning(t *testing.T) {
	s := &reactS3{fakeS3: newFakeS3("up"), delay: 5 * time.Second, counts: map[string]int{}}
	s.seed("up", "big.bin", "hello world")
	a := reactApp(t, s)

	dest := XferDest{Kind: "local", Dir: t.TempDir()}
	id, err := a.TransferCross([]XferItem{{Bucket: "up", Key: "big.bin", Size: 0}}, nil,
		dest, PolicyOverwrite, 0, false, nil, false)
	if err != nil || id == "" {
		t.Fatalf("TransferCross: id=%q err=%v", id, err)
	}
	if !a.CancelTransfer(id) {
		t.Fatal("CancelTransfer must find the planning job — it exists from the gesture's return")
	}
	ji := waitJobSettled(t, a, id, 2*time.Second)
	if ji.Status != JobCanceled {
		t.Fatalf("status = %q, want canceled well inside the 5s parked walk", ji.Status)
	}
	if _, err := os.Stat(filepath.Join(dest.Dir, "big.bin")); !os.IsNotExist(err) {
		t.Fatal("a canceled plan must never land a file")
	}
}

// TestPlanMemoHandsCheckToStart pins the walk-once law: a clean
// pre-check hands its finished plan to the immediately-following start
// (the same gesture), so the provider sees the walk exactly once — and
// a second, later start of the same shape plans fresh, because the memo
// is single-use.
func TestPlanMemoHandsCheckToStart(t *testing.T) {
	s := &reactS3{fakeS3: newFakeS3("up"), counts: map[string]int{}}
	s.seed("up", "docs/a.txt", "alpha")
	s.seed("up", "docs/b.txt", "bravo")
	s.seed("up", "docs/c.txt", "charlie")
	a := reactApp(t, s)

	items := []XferItem{{Bucket: "up", Key: "docs/", IsDir: true}}
	dest := XferDest{Kind: "local", Dir: t.TempDir()}
	rows, err := a.CheckConflicts(items, nil, dest)
	if err != nil || len(rows) != 0 {
		t.Fatalf("CheckConflicts = %v, %v; want a clean destination", rows, err)
	}
	if got := s.counted("LIST"); got != 1 {
		t.Fatalf("pre-check walks = %d, want exactly 1", got)
	}

	id, err := a.TransferCross(items, nil, dest, PolicyOverwrite, 0, false, nil, false)
	if err != nil || id == "" {
		t.Fatalf("TransferCross: id=%q err=%v", id, err)
	}
	if ji := waitJobSettled(t, a, id); ji.Status != JobDone || ji.TotalFiles != 3 {
		t.Fatalf("job = %+v, want all three files landed", ji)
	}
	if got := s.counted("LIST"); got != 1 {
		t.Fatalf("walks after the memo start = %d, want still 1 — the pre-check's plan is the transfer's plan", got)
	}
	if got := s.counted("GET"); got != 3 {
		t.Fatalf("downloads = %d, want 3", got)
	}

	// The same gesture again (overwrite onto the landed files): the memo
	// was consumed, so this start plans fresh.
	id2, err := a.TransferCross(items, nil, dest, PolicyOverwrite, 0, false, nil, false)
	if err != nil || id2 == "" {
		t.Fatalf("second TransferCross: id=%q err=%v", id2, err)
	}
	waitJobSettled(t, a, id2)
	if got := s.counted("LIST"); got != 2 {
		t.Fatalf("walks after the second start = %d, want 2 — the memo is single-use", got)
	}
}

// TestCheckConflictsProbesReportCollisions pins the fanned-out probe
// leg on its correctness contract: with the walk-order keys colliding
// at a local destination, the rows come back in plan order with both
// sides' true sizes.
func TestCheckConflictsProbesReportCollisions(t *testing.T) {
	s := &reactS3{fakeS3: newFakeS3("up"), counts: map[string]int{}}
	for i := 0; i < 16; i++ {
		s.seed("up", nameN("set/a", ".txt", i), "payload-"+string(rune('a'+i)))
	}
	a := reactApp(t, s)

	dest := XferDest{Kind: "local", Dir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(dest.Dir, "set"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ { // the first eight names already sit at the destination
		if err := os.WriteFile(filepath.Join(dest.Dir, nameN("set/a", ".txt", i)), []byte("older"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := a.CheckConflicts([]XferItem{{Bucket: "up", Key: "set/", IsDir: true}}, nil, dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 8 {
		t.Fatalf("rows = %d, want the 8 colliding files", len(rows))
	}
	for i, r := range rows {
		if want := nameN("set/a", ".txt", i); r.Key != want {
			t.Fatalf("row %d key = %q, want %q (plan order)", i, r.Key, want)
		}
		if r.DstSize != int64(len("older")) || r.SrcSize != int64(len("payload-"+string(rune('a'+i)))) {
			t.Fatalf("row %d sizes = src %d / dst %d, want both sides' true sizes", i, r.SrcSize, r.DstSize)
		}
	}
}

// TestPlanMemoLaws pins the memo itself: single-use, TTL-bounded,
// size-capped, entry-capped.
func TestPlanMemoLaws(t *testing.T) {
	var m xferPlanMemo
	m.store("k1", &xferPlan{})
	m.store("k2", &xferPlan{})
	if _, ok := m.take("missing"); ok {
		t.Fatal("an unknown key must miss")
	}
	if _, ok := m.take("k1"); !ok {
		t.Fatal("a stored entry must hit")
	}
	if _, ok := m.take("k1"); ok {
		t.Fatal("take is single-use — the first taker consumes the entry")
	}
	if _, ok := m.take("k2"); !ok {
		t.Fatal("a sibling entry survives another entry's take")
	}

	m.mu.Lock()
	m.entries = append(m.entries, xferPlanMemoEntry{key: "old", plan: &xferPlan{}, at: time.Now().Add(-3 * time.Minute)})
	m.mu.Unlock()
	if _, ok := m.take("old"); ok {
		t.Fatal("an expired entry must miss")
	}
	if len(m.entries) != 0 {
		t.Fatalf("expired entries must drop as they pass by, got %d", len(m.entries))
	}

	m.store("big", &xferPlan{files: make([]xferFile, planMemoMaxFiles+1)})
	if _, ok := m.take("big"); ok {
		t.Fatal("an oversized plan must never park in the memo")
	}

	for i := 0; i < planMemoEntries+2; i++ {
		m.store(nameN("k", "", i), &xferPlan{})
	}
	if len(m.entries) > planMemoEntries {
		t.Fatalf("entries = %d, want <= %d", len(m.entries), planMemoEntries)
	}
	if _, ok := m.take(nameN("k", "", 0)); ok {
		t.Fatal("the oldest entry must have been evicted")
	}
	if _, ok := m.take(nameN("k", "", planMemoEntries+1)); !ok {
		t.Fatal("the newest entry must be live")
	}
}

// nameN renders a zero-padded numbered name (a00, a01, …) so walk order
// and assertion order agree.
func nameN(prefix, suffix string, i int) string {
	return fmt.Sprintf("%s%02d%s", prefix, i, suffix)
}
