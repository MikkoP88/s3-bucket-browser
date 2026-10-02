//go:build windows && !s3b_headless && !server

package gui

import (
	"testing"
	"time"
)

// Named kernel objects are matched by name, not by process, so the whole
// guard ladder is exercisable in one process: the first acquireInstanceLock
// is "the running instance", the second on the same names is "a second
// launch". Unique names keep the tests clear of the production objects (a
// real app may be running while tests execute).

func TestAcquireInstanceLockSecondSignalsFirst(t *testing.T) {
	mn := `Local\s3b.test.single-a.instance`
	en := `Local\s3b.test.single-a.focus`
	focus, existing, _ := acquireInstanceLock(mn, en)
	if existing {
		t.Fatal("first acquire on fresh names reported an existing instance")
	}
	if focus == nil {
		t.Fatal("first acquire returned no focus channel")
	}
	// The second launch: existing=true, and its signal must reach the
	// winner's channel (the pump goroutine forwards the kernel event).
	_, existing2, focused2 := acquireInstanceLock(mn, en)
	if !existing2 {
		t.Fatal("second acquire on held names did not report an existing instance")
	}
	if !focused2 {
		t.Fatal("second acquire could not signal the winner's focus event")
	}
	select {
	case <-focus:
		// the winner woke — exactly the focus-existing contract
	case <-time.After(2 * time.Second):
		t.Fatal("the winner's focus channel never received the second launch's signal")
	}
}

func TestAcquireInstanceLockDistinctNamesDoNotCollide(t *testing.T) {
	_, existingA, _ := acquireInstanceLock(`Local\s3b.test.single-b.a.instance`, `Local\s3b.test.single-b.a.focus`)
	_, existingB, _ := acquireInstanceLock(`Local\s3b.test.single-b.b.instance`, `Local\s3b.test.single-b.b.focus`)
	if existingA || existingB {
		t.Fatalf("distinct names collided: existingA=%v existingB=%v", existingA, existingB)
	}
}

func TestGuardSingleInstanceEnvOptOut(t *testing.T) {
	// The bypass must decide before any kernel object is touched, so a
	// held production mutex is irrelevant to it — assert the pure bypass.
	t.Setenv(multiInstanceEnv, "1")
	focus, existing := guardSingleInstance()
	if existing {
		t.Fatal("S3B_MULTI_INSTANCE=1 still reported an existing instance")
	}
	if focus != nil {
		t.Fatal("S3B_MULTI_INSTANCE=1 returned a focus channel")
	}
}
