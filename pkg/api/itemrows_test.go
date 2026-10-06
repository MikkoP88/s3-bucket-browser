// itemrows_test.go pins the per-item transfer-row contract: the
// pending→active→settled lifecycle inside one job, the outcome precedence
// when an item's files mix done/failed/skipped, the byte honesty of skips,
// the >itemRowCap fallback to bare names, and the copy-on-write discipline
// that keeps emitted snapshots stable while the worker keeps moving.
package api

import (
	"context"
	"strconv"
	"testing"
)

func TestItemRowsSettlePerFile(t *testing.T) {
	a := newTestApp(t)
	a.jobs.setContext(context.Background())
	j := a.jobs.add("upload", 4, 40)
	j.setMeta("batch", `C:\in`, "s3://team-files/in", 3, false)
	j.setItems([]TransferItem{
		{Name: "folder/", Files: 2, Total: 20}, // aggregates two planned files
		{Name: "b.txt", Files: 1, Total: 10},
		{Name: "c.txt", Files: 1, Total: 10},
	})

	snap := a.jobs.snapshot()[0]
	for i, r := range snap.ItemRows {
		if r.State != ItemPending {
			t.Fatalf("item %d starts %q, want pending", i, r.State)
		}
	}

	// first file of the folder starts → that row goes active, others wait
	j.startFile(0, 1, "folder/a.txt", 10)
	snap = a.jobs.snapshot()[0]
	if snap.ItemRows[0].State != ItemActive || snap.ItemRows[1].State != ItemPending {
		t.Fatalf("mid-item states = %+v", snap.ItemRows)
	}
	j.fileDone(0, 10, ItemDone)
	snap = a.jobs.snapshot()[0]
	r := snap.ItemRows[0]
	if r.Done != 1 || r.State != ItemActive || r.Sent != 10 {
		t.Fatalf("folder row after 1/2 files = %+v", r) // settled file, item still open
	}

	// second file of the folder closes the item → done
	j.startFile(0, 2, "folder/b.txt", 10)
	j.fileDone(0, 10, ItemDone)
	snap = a.jobs.snapshot()[0]
	if r = snap.ItemRows[0]; r.Done != 2 || r.State != ItemDone || r.Sent != 20 {
		t.Fatalf("closed folder row = %+v", r)
	}

	// b.txt fails, c.txt skips → their rows settle failed and skipped,
	// and the skip adds no bytes anywhere
	j.startFile(1, 3, "b.txt", 10)
	j.fileDone(1, 10, ItemFailed)
	j.startFile(2, 4, "c.txt", 10)
	j.fileDone(2, 0, ItemSkipped)
	snap = a.jobs.snapshot()[0]
	if got := snap.ItemRows[1].State; got != ItemFailed {
		t.Fatalf("failed row state = %q", got)
	}
	if got := snap.ItemRows[2].State; got != ItemSkipped {
		t.Fatalf("skipped row state = %q", got)
	}
	if snap.SentBytes != 30 { // 20 folder + 10 failed; the skip settled nothing
		t.Fatalf("SentBytes = %d, want 30 (skip adds none)", snap.SentBytes)
	}
	if snap.DoneFiles != 2 || snap.FailedFiles != 1 || snap.SkippedFiles != 1 {
		t.Fatalf("job counters = %+v", snap)
	}
}

// TestItemRowsPrecedenceAndZeroFile: within one item, failed outranks
// skipped which outranks done; an item with nothing planned is born done,
// never pending.
func TestItemRowsPrecedenceAndZeroFile(t *testing.T) {
	a := newTestApp(t)
	j := a.jobs.add("upload", 3, 30)
	j.setItems([]TransferItem{
		{Name: "mixed/", Files: 2, Total: 20}, // one done + one failed → failed
		{Name: "skipmix/", Files: 2, Total: 10},
		{Name: "empty/", Files: 0},
	})
	snap := a.jobs.snapshot()[0]
	if got := snap.ItemRows[2].State; got != ItemDone {
		t.Fatalf("zero-file item state = %q, want done at plan time", got)
	}

	j.fileDone(0, 10, ItemDone)
	j.fileDone(0, 10, ItemFailed) // precedence over the earlier done
	j.fileDone(1, 0, ItemSkipped)
	j.fileDone(1, 0, ItemSkipped) // skipped-only item settles skipped
	snap = a.jobs.snapshot()[0]
	if got := snap.ItemRows[0].State; got != ItemFailed {
		t.Fatalf("mixed row = %q, want failed to win", got)
	}
	if got := snap.ItemRows[1].State; got != ItemSkipped {
		t.Fatalf("skip-only row = %q, want skipped", got)
	}
}

// TestItemRowsCapFallsBackToNames: beyond itemRowCap the live rows are
// dropped (bounded payload on every event) and TransferItems still lists
// every top-level name.
func TestItemRowsCapFallsBackToNames(t *testing.T) {
	a := newTestApp(t)
	many := make([]TransferItem, itemRowCap+10)
	for i := range many {
		many[i] = TransferItem{Name: string(rune('a'+i%26)) + strconv.Itoa(i), Files: 1}
	}
	j := a.jobs.add("upload", len(many), 0)
	j.setItems(many)

	if rows := a.jobs.snapshot()[0].ItemRows; rows != nil {
		t.Fatalf("over-cap job carries %d rows, want none", len(rows))
	}
	names := a.TransferItems(j.info.ID)
	if len(names) != len(many) || names[0] != many[0].Name || names[len(many)-1] != many[len(many)-1].Name {
		t.Fatalf("TransferItems fallback = %d names, want %d", len(names), len(many))
	}
}

// TestItemRowsCOWKeepsSnapshotsStable: a taken snapshot shares its backing
// array with a marshal riding outside the lock — the next worker update
// must swap in a fresh array, never write through the held one.
func TestItemRowsCOWKeepsSnapshotsStable(t *testing.T) {
	a := newTestApp(t)
	j := a.jobs.add("upload", 2, 20)
	j.setItems([]TransferItem{{Name: "a", Files: 1, Total: 10}, {Name: "b", Files: 1, Total: 10}})

	held := a.jobs.snapshot()[0].ItemRows
	j.startFile(0, 1, "a", 10)
	j.fileDone(0, 10, ItemDone)

	if held[0].State != ItemPending || held[0].Done != 0 {
		t.Fatalf("held snapshot mutated: %+v", held[0])
	}
	fresh := a.jobs.snapshot()[0].ItemRows
	if fresh[0].State != ItemDone || fresh[0].Done != 1 {
		t.Fatalf("fresh snapshot stale: %+v", fresh[0])
	}
}

// TestRunningTasksCarriesItemRows: the merged task rows expose the same
// live per-item states so the tasks window's disclosure matches the
// transfer window's.
func TestRunningTasksCarriesItemRows(t *testing.T) {
	a := newTestApp(t)
	a.jobs.setContext(context.Background())
	j := a.jobs.add("upload", 2, 20)
	j.setMeta("pics", `D:\p`, "s3://team-files", 2, false)
	j.setItems([]TransferItem{{Name: "a.jpg", Files: 1, Total: 10}, {Name: "b.jpg", Files: 1, Total: 10}})
	j.startFile(0, 1, "a.jpg", 10)
	j.fileDone(0, 10, ItemDone)

	got := a.RunningTasks()
	if len(got) != 1 {
		t.Fatalf("RunningTasks = %d rows, want 1", len(got))
	}
	rows := got[0].ItemRows
	if len(rows) != 2 || rows[0].State != ItemDone || rows[1].State != ItemPending {
		t.Fatalf("merged item rows = %+v", rows)
	}
}
