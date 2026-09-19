package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Task-coverage: the Running-tasks window is an everything-monitor, so the
// late additions to it must actually register tasks that finish — not run
// silently. (Doctor runs and bucket deletes need a live S3 endpoint; they
// follow the same pattern and are asserted in scripts/gui-v3live.mjs.)

// CompareAny registers a compare task that lands done with the walked
// row count as its progress and both sides named in its label.
func TestCompareAnyRegistersTask(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	x, y := t.TempDir(), t.TempDir()
	for _, f := range []struct {
		dir, name string
		body      string
	}{
		{x, "same.txt", "a"},
		{x, "only-left.txt", "bb"},
		{y, "same.txt", "a"},
		{y, "only-right.txt", "ccc"},
	} {
		if err := os.WriteFile(filepath.Join(f.dir, f.name), []byte(f.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := a.CompareAny(
		CompareRef{Kind: "local", Dir: x},
		CompareRef{Kind: "local", Dir: y},
	)
	if err != nil {
		t.Fatalf("CompareAny: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}

	tasks := a.RunningTasks()
	if len(tasks) != 1 || tasks[0].Kind != "compare" {
		t.Fatalf("RunningTasks = %+v, want exactly one compare task", tasks)
	}
	cmp := tasks[0]
	if cmp.Status != TaskDone || cmp.Error != "" {
		t.Fatalf("compare task = %+v", cmp)
	}
	if cmp.DoneUnits != 3 {
		t.Errorf("DoneUnits = %d, want 3 (the row count)", cmp.DoneUnits)
	}
	if !strings.Contains(cmp.Label, x) || !strings.Contains(cmp.Label, y) ||
		!strings.Contains(cmp.Label, "\u2194") {
		t.Errorf("label = %q, want both sides joined", cmp.Label)
	}
}

// RemoteRemove registers a delete task with the selection as its total and
// the removed count as its progress (count-then-act without a count phase:
// the total is the selection itself).
func TestRemoteRemoveRegistersTask(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	src, root := localSource(t, "lab")
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoteCreateFile("lab", "/", "junk", "txt"); err != nil {
		t.Fatal(err)
	}

	res, err := a.RemoteRemove("lab", []string{"/readme.md", "/junk.txt"})
	if err != nil {
		t.Fatalf("RemoteRemove: %v", err)
	}
	if res.Deleted != 2 {
		t.Fatalf("Deleted = %d, want 2", res.Deleted)
	}
	if _, err := os.Stat(filepath.Join(root, "junk.txt")); !os.IsNotExist(err) {
		t.Fatalf("junk.txt survived: %v", err)
	}

	var del, mk *TaskInfo
	for i, tk := range a.RunningTasks() {
		switch tk.Kind {
		case "delete":
			del = &a.RunningTasks()[i]
		case "mkfile":
			mk = &a.RunningTasks()[i]
		}
	}
	if del == nil {
		t.Fatalf("no delete task in RunningTasks: %+v", a.RunningTasks())
	}
	if del.Status != TaskDone || del.TotalUnits != 2 || del.DoneUnits != 2 {
		t.Fatalf("delete task = %+v, want done 2/2", del)
	}
	if del.Label != "lab — 2 selected item(s)" {
		t.Errorf("delete label = %q", del.Label)
	}
	if mk == nil || mk.Status != TaskDone {
		t.Errorf("mkfile task missing or unfinished: %+v", mk)
	}
}
