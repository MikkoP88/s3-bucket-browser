package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// composeFileName: the New-file dialog contract — extension appended once,
// case-insensitive dedup, dotted/spaced extensions normalized.
func TestComposeFileName(t *testing.T) {
	cases := []struct {
		name, ext, want string
	}{
		{"notes", "txt", "notes.txt"},
		{"notes", "", "notes"},
		{"notes", "   ", "notes"},   // blank extension = none
		{"notes", ".txt", "notes.txt"}, // dotted extension normalized
		{"notes", " txt", "notes.txt"},
		{"notes.txt", "txt", "notes.txt"},       // already typed it — no double append
		{"NOTES.TXT", "txt", "NOTES.TXT"},       // match is case-insensitive
		{"notes.txt", "TXT", "notes.txt"},
		{"notes.md", "txt", "notes.md.txt"}, // different extension: appended
		{"archive.tar", "gz", "archive.tar.gz"},
		{"notes.", "txt", "notes.txt"}, // trailing dot not doubled
		{"  notes  ", "txt", "notes.txt"},
		{"  /notes/  ", "txt", "notes.txt"}, // slashes trimmed at the edges
		{"report.v1.2", "md", "report.v1.2.md"},
		{"notes.txt", "", "notes.txt"},
		// No usable base name: yields "" so callers reject it (a blank
		// name must not compose into a hidden ".txt").
		{"  ", "txt", ""},
		{".", "txt", ""},
		{"..", "txt", ""},
		{".gitignore", "", ".gitignore"}, // real dotfiles stay legal
	}
	for _, c := range cases {
		if got := composeFileName(c.name, c.ext); got != c.want {
			t.Errorf("composeFileName(%q, %q) = %q, want %q", c.name, c.ext, got, c.want)
		}
	}
}

// RemoteCreateFile over the local engine: returns the cleaned path, the
// empty file lands on disk, and the guards (bad name, S3 source) hold.
func TestRemoteCreateFile(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	src, root := localSource(t, "lab")
	if err := a.SaveSource(src); err != nil {
		t.Fatal(err)
	}

	p, err := a.RemoteCreateFile("lab", "/docs", "notes", "txt")
	if err != nil {
		t.Fatalf("RemoteCreateFile: %v", err)
	}
	if p != "/docs/notes.txt" {
		t.Fatalf("returned path = %q, want /docs/notes.txt", p)
	}
	st, err := os.Stat(filepath.Join(root, "docs", "notes.txt"))
	if err != nil {
		t.Fatalf("file not created on disk: %v", err)
	}
	if st.Size() != 0 {
		t.Fatalf("new file size = %d, want 0", st.Size())
	}

	// Name that already carries the extension: composed once.
	p2, err := a.RemoteCreateFile("lab", "/", "todo.txt", "txt")
	if err != nil || p2 != "/todo.txt" {
		t.Fatalf("dedup create = %q err = %v", p2, err)
	}

	// Empty and nested names are rejected before touching the engine.
	if _, err := a.RemoteCreateFile("lab", "/docs", "  ", "txt"); err == nil {
		t.Error("blank name must be rejected")
	}
	if _, err := a.RemoteCreateFile("lab", "/docs", "a/b", "txt"); err == nil {
		t.Error("slashed name must be rejected")
	}

	// S3 sources stay on their dedicated pipeline.
	if err := a.SaveSource(s3Source("cloud", "s")); err != nil {
		t.Fatal(err)
	}
	_, err = a.RemoteCreateFile("cloud", "/", "x", "txt")
	if err == nil || !strings.Contains(err.Error(), "S3 pipeline") {
		t.Errorf("s3 via remotefs err = %v", err)
	}
}
