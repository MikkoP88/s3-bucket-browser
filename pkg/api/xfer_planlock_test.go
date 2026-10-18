// xfer_planlock_test.go pins the planner's serialization law: planXfer
// holds every involved source's operation lock while it stats and walks
// remote engines, and CheckConflicts holds the destination's lock across
// its probe loop — the FTP control channel answers one command at a
// time, and a plan that raced the tree's listing on one ServerConn tore
// the textproto reader until the process died (the GUI's FTPS paste
// crash, 18 Oct 2026).
package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
)

// parkingFS parks each Stat at its own stage: the nth Stat signals
// entered[n] and blocks until release[n] closes, so a rig can observe
// the planner inside the engine at every command boundary.
type parkingFS struct {
	remotefs.FS
	mu      sync.Mutex
	calls   int
	entered []chan struct{}
	release []chan struct{}
}

func (p *parkingFS) Stat(ctx context.Context, path string) (listing.Entry, error) {
	p.mu.Lock()
	n := p.calls
	p.calls++
	p.mu.Unlock()
	if n < len(p.entered) {
		close(p.entered[n])
		<-p.release[n]
	}
	return listing.Entry{Key: path, Name: path, Size: 7}, nil
}

// seedParkedSource saves one local-rooted source and pre-seeds the
// engine cache with a parkingFS staged for stages Stats.
func seedParkedSource(t *testing.T, a *App, name string, stages int) *parkingFS {
	t.Helper()
	src, _ := emptyLocalSource(t, name)
	src.ID = "src-" + name + "-lock" // engine cache keys by ID; own it so the seed matches the saved source
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	inner, err := remotefs.NewLocal(context.Background(), src.LocalRoot)
	if err != nil {
		t.Fatal(err)
	}
	p := &parkingFS{FS: inner}
	for range stages {
		p.entered = append(p.entered, make(chan struct{}))
		p.release = append(p.release, make(chan struct{}))
	}
	a.engMu.Lock()
	a.engines[src.ID] = p
	a.engMu.Unlock()
	return p
}

// assertLockHeld is the law's oracle: while the planner is inside the
// engine, the source's operation lock must be unavailable to anyone
// else — a concurrent listing on the same source waits, never races.
func assertLockHeld(t *testing.T, a *App, id, story string) {
	t.Helper()
	if mu := a.srcLock(id); mu.TryLock() {
		mu.Unlock()
		t.Fatalf("%s: the source lock was free — the planner must hold it", story)
	}
}

// TestPlanXferHoldsSourceLocks runs the planner over one clipboard-shaped
// item (a key with no size — exactly what stats the engine) and pins the
// lock hold from inside the engine call.
func TestPlanXferHoldsSourceLocks(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	p := seedParkedSource(t, a, "parked", 1)

	// Resolve the saved source's ID the way the planner will.
	src, err := a.sourceByIDOrName("parked")
	if err != nil {
		t.Fatal(err)
	}

	type verdict struct {
		plan *xferPlan
		err  error
	}
	done := make(chan verdict, 1)
	go func() {
		pctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		plan, err := a.planXfer(pctx, []XferItem{{Source: "parked", Key: "/f.txt"}}, nil, xferDestSide{kind: "s3", bucket: "b"})
		done <- verdict{plan, err}
	}()

	select {
	case <-p.entered[0]:
	case <-time.After(20 * time.Second):
		t.Fatal("planner never reached the engine Stat")
	}
	assertLockHeld(t, a, src.ID, "planXfer during its engine Stat")
	close(p.release[0])

	v := <-done
	if v.err != nil {
		t.Fatalf("planXfer: %v", v.err)
	}
	if len(v.plan.files) != 1 || v.plan.files[0].size != 7 {
		t.Fatalf("plan files = %+v, want the parked file planned at its statted size", v.plan.files)
	}
}

// TestCheckConflictsProbeHoldsDestLock walks the conflict pre-check end
// to end over a remote destination: the planning stat holds the lock,
// and the destination probe loop holds it again — every engine command
// of the verb serialized.
func TestCheckConflictsProbeHoldsDestLock(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	p := seedParkedSource(t, a, "vault", 2)

	src, err := a.sourceByIDOrName("vault")
	if err != nil {
		t.Fatal(err)
	}

	type verdict struct {
		out []ConflictInfo
		err error
	}
	done := make(chan verdict, 1)
	go func() {
		out, err := a.CheckConflicts([]XferItem{{Source: "vault", Key: "/f.txt"}}, nil, XferDest{Kind: "remote", Source: "vault", Dir: "/"})
		done <- verdict{out, err}
	}()

	// Stage 0: the planner stats the item on the destination's own
	// engine (same source both sides), under the lock.
	select {
	case <-p.entered[0]:
	case <-time.After(20 * time.Second):
		t.Fatal("planner never reached the engine Stat")
	}
	assertLockHeld(t, a, src.ID, "CheckConflicts planning")
	close(p.release[0])

	// Stage 1: the destination probe stats the landing path, again
	// under the lock.
	select {
	case <-p.entered[1]:
	case <-time.After(20 * time.Second):
		t.Fatal("probe never reached the destination Stat")
	}
	assertLockHeld(t, a, src.ID, "CheckConflicts destination probe")
	close(p.release[1])

	v := <-done
	if v.err != nil {
		t.Fatalf("CheckConflicts: %v", v.err)
	}
	if len(v.out) != 1 || v.out[0].DstSize != 7 {
		t.Fatalf("conflicts = %+v, want the probed row with the statted size", v.out)
	}
}
