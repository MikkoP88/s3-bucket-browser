package procprobe

import (
	"os"
	"testing"
)

func TestAliveSelfAndTraps(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Fatalf("Own PID %d must read alive", os.Getpid())
	}
	if Alive(0) {
		t.Fatal("PID 0 is never alive")
	}
	if Alive(-4) {
		t.Fatal("Negative PIDs are never alive")
	}
	// PIDs this process can see but that do not exist. 0x7fffffff sits far
	// above every real Windows PID and past the unix PID_MAX_LIMIT, so the
	// probe must call it dead on both families.
	if Alive(0x7fffffff) {
		t.Fatal("A PID far beyond every live process must read dead")
	}
}
