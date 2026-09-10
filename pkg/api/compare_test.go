package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Hermetic end-to-end tests for CompareAny: the same tree pair is compared
// through the local walker and through the remote (local-engine) walker, so
// both path trimmings are exercised without any network.

// cmpTree writes a file with an exact size and mtime (deterministic
// verdicts despite the 2s clock tolerance).
func cmpTree(t *testing.T, root, rel string, size int, m time.Time) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, m, m); err != nil {
		t.Fatal(err)
	}
}

func TestCompareAnyLocalAndRemote(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	labSrc, labRoot := emptyLocalSource(t, "lab")
	if err := a.SaveSource(labSrc); err != nil {
		t.Fatal(err)
	}
	vaultSrc, vaultRoot := emptyLocalSource(t, "vault")
	if err := a.SaveSource(vaultSrc); err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-time.Hour).Truncate(time.Second)

	mk := func(root string) {
		cmpTree(t, root, "same.txt", 100, base)
		cmpTree(t, root, "nested/dir.txt", 7, base)
		cmpTree(t, root, "size.txt", 100, base)
		cmpTree(t, root, "newer-left.txt", 100, base)
		cmpTree(t, root, "newer-right.txt", 100, base)
		cmpTree(t, root, "tolerance.txt", 100, base)
	}
	mk(labRoot)
	mk(vaultRoot)
	cmpTree(t, labRoot, "only-left.txt", 10, base)
	cmpTree(t, vaultRoot, "only-right.txt", 10, base)
	cmpTree(t, labRoot, "size.txt", 101, base) // left variant
	cmpTree(t, labRoot, "newer-left.txt", 100, base.Add(time.Hour))
	cmpTree(t, vaultRoot, "newer-right.txt", 100, base.Add(time.Hour))
	cmpTree(t, vaultRoot, "tolerance.txt", 100, base.Add(time.Second)) // inside tolerance

	// The identical tree pair runs through both walkers: local↔local and
	// remote(local engine)↔remote(local engine).
	pairs := [][2]CompareRef{
		{{Kind: "local", Dir: labRoot}, {Kind: "local", Dir: vaultRoot}},
		{{Kind: "remote", Source: "lab", Dir: "/"}, {Kind: "remote", Source: "vault", Dir: "/"}},
	}
	for _, pair := range pairs {
		rows, err := a.CompareAny(pair[0], pair[1])
		if err != nil {
			t.Fatalf("CompareAny(%+v, %+v): %v", pair[0], pair[1], err)
		}
		got := map[string]string{}
		for _, r := range rows {
			got[r.Key] = r.Status
		}
		want := map[string]string{
			"same.txt":        CmpSame,
			"nested/dir.txt":  CmpSame,
			"only-left.txt":   CmpOnlyLocal,
			"only-right.txt":  CmpOnlyRemote,
			"size.txt":        CmpSizeDiff,
			"newer-left.txt":  CmpNewerLocal,
			"newer-right.txt": CmpNewerRemote,
			"tolerance.txt":   CmpSame,
		}
		for k, w := range want {
			if got[k] != w {
				t.Errorf("[%s↔%s] %s = %q, want %q (all: %v)",
					pair[0].Kind, pair[1].Kind, k, got[k], w, got)
			}
		}
	}

	// A subdirectory ref trims its prefix from the relative keys.
	rows, err := a.CompareAny(
		CompareRef{Kind: "local", Dir: filepath.Join(labRoot, "nested")},
		CompareRef{Kind: "remote", Source: "lab", Dir: "/nested"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Key != "dir.txt" || rows[0].Status != CmpSame {
		t.Fatalf("subdir compare rows = %+v, want one same dir.txt", rows)
	}

	// Unknown side kinds are refused.
	if _, err := a.CompareAny(CompareRef{Kind: "ftp?"}, CompareRef{Kind: "local", Dir: labRoot}); err == nil {
		t.Error("unknown kind accepted")
	}
}

func TestLooksTexty(t *testing.T) {
	for _, ok := range []string{"", "plain text\nwith lines", "unicode åäö ✓", "{\"json\": true}"} {
		if !looksTexty([]byte(ok)) {
			t.Errorf("looksTexty(%q) = false, want true", ok)
		}
	}
	for _, bad := range [][]byte{
		{0x00, 0x01},                      // NUL byte
		{0xff, 0xfe, 0xfd, 0xfc},          // invalid UTF-8
		[]byte("text\x00with NUL"),        // NUL inside
		{0x50, 0x4b, 0x03, 0x04, 0x14, 0}, // zip-ish
	} {
		if looksTexty(bad) {
			t.Errorf("looksTexty(%v) = true, want false", bad)
		}
	}
}
