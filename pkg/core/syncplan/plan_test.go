package syncplan

import (
	"reflect"
	"testing"
)

// The predicate is the contract both faces ride: the CLI's sync loops and
// the GUI's Synchronize plan must agree with these legs forever.
func TestPlanLegs(t *testing.T) {
	source := map[string]int64{
		"same.txt": 10, // size-equal at the target: unchanged
		"grew.txt": 20, // size differs: copies again
		"new.txt":  5,  // missing at the target: copies
	}
	target := map[string]int64{
		"same.txt":  10,
		"grew.txt":  15,
		"stale.txt": 7, // absent at the source: named for deletion
		"gone.txt":  9, // ditto
	}
	copies, dels, skipped := Plan(source, target, true)
	if want := []string{"grew.txt", "new.txt"}; !reflect.DeepEqual(copies, want) {
		t.Errorf("copies = %v, want %v", copies, want)
	}
	if want := []string{"gone.txt", "stale.txt"}; !reflect.DeepEqual(dels, want) {
		t.Errorf("dels = %v, want %v", dels, want)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}

	// del=false: the delete vector stays empty even though the target
	// carries files the source does not
	copies2, dels2, skipped2 := Plan(source, target, false)
	if len(dels2) != 0 {
		t.Errorf("dels with del=false = %v, want empty", dels2)
	}
	if !reflect.DeepEqual(copies2, copies) || skipped2 != skipped {
		t.Errorf("del flag changed the copy half: %v / %d", copies2, skipped2)
	}
}

func TestPlanEmptySides(t *testing.T) {
	// an empty source over a full target deletes everything (the
	// mirror shape); an empty target over a full source copies everything
	full := map[string]int64{"a": 1, "b": 2}
	copies, dels, skipped := Plan(map[string]int64{}, full, true)
	if len(copies) != 0 || skipped != 0 {
		t.Errorf("copies/skipped over empty source = %v/%d, want empty/0", copies, skipped)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(dels, want) {
		t.Errorf("dels = %v, want %v", dels, want)
	}
	copies, dels, skipped = Plan(full, map[string]int64{}, true)
	if want := []string{"a", "b"}; !reflect.DeepEqual(copies, want) {
		t.Errorf("copies = %v, want %v", copies, want)
	}
	if len(dels) != 0 || skipped != 0 {
		t.Errorf("dels/skipped over empty target = %v/%d, want empty/0", dels, skipped)
	}
}
