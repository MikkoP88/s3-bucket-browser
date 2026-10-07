package transfer

import (
	"path/filepath"
	"strings"
	"testing"
)

// SafeLocalJoin contains server-supplied paths inside the destination:
// ordinary keys map to themselves (byte for byte), traversal and the
// Windows minefield never escape. The table asserts the mapped path as a
// dir-relative slash path — the join itself speaks the platform's
// separator, so the expectations stay truthful on every OS (on Linux the
// backslashed dir is just a name, and filepath.Join uses "/").
func TestSafeLocalJoin(t *testing.T) {
	const dir = `C:\Users\demo\Downloads`
	for _, tc := range []struct{ in, wantRel string }{
		// identity for ordinary keys — the compat contract every
		// existing transfer rides on
		{"notes.txt", `notes.txt`},
		{"docs/notes.txt", `docs/notes.txt`},
		{"a/b/c/report 2026.pdf", `a/b/c/report 2026.pdf`},
		{"archive.tar.gz", `archive.tar.gz`},
		// traversal is neutralized in place, never resolved
		{"../evil.txt", `_/evil.txt`},
		{"..\\..\\evil.txt", `_/_/evil.txt`},
		{"docs/../../../Windows/system32/evil.dll", `docs/_/_/_/Windows/system32/evil.dll`},
		{"/abs/path.txt", `abs/path.txt`},
		// drive and UNC shapes become literal contained names
		{"C:/evil.txt", `C_/evil.txt`},
		{`\\server\share\evil.txt`, `server/share/evil.txt`},
		// reserved device names are pushed past their device meaning
		{"NUL", `_NUL`},
		{"con.txt", `_con.txt`},
		{"logs/COM1", `logs/_COM1`},
		// Windows-invalid characters and trailing dots/spaces
		{"re:port?.txt", `re_port_.txt`},
		{"name. ", `name`},
		{"name...", `name`},
		// dot segments collapse; dotfiles survive
		{"./a/.hidden", `_/a/.hidden`},
	} {
		got := SafeLocalJoin(dir, tc.in)
		if !strings.HasPrefix(got, dir+string(filepath.Separator)) {
			t.Errorf("SafeLocalJoin(%q, %q) = %q, want it under %q", dir, tc.in, got, dir)
			continue
		}
		rel, err := filepath.Rel(dir, got)
		if err != nil {
			t.Errorf("SafeLocalJoin(%q, %q) = %q: %v", dir, tc.in, got, err)
			continue
		}
		if rel != filepath.FromSlash(tc.wantRel) {
			t.Errorf("SafeLocalJoin(%q, %q) = %q, want relative %q", dir, tc.in, got, tc.wantRel)
		}
	}
}

// SafeLocalJoin stays inside the destination for anything thrown at it,
// and joins several server-supplied parts as one relative path.
func TestSafeLocalJoinContainment(t *testing.T) {
	const dir = `C:\Users\demo\Downloads`
	nasties := []string{
		"..", "../", "a/../../..\\../..", "C:\\Windows\\system32",
		"\\\\?\\C:\\x", "con/NUL/../COM9", "..\\..", "////////..",
		"\x00\x01evil", "....",
	}
	for _, in := range nasties {
		got := SafeLocalJoin(dir, in)
		if !strings.HasPrefix(got, dir+string(filepath.Separator)) && got != dir {
			t.Errorf("SafeLocalJoin(%q) = %q escaped the destination", in, got)
		}
		if rel, err := filepath.Rel(dir, got); err == nil && strings.HasPrefix(rel, "..") {
			t.Errorf("SafeLocalJoin(%q) = %q climbs out of the destination (rel %q)", in, got, rel)
		}
	}
	// multi-part join (the transfer engine's base + rel shape)
	if got, want := SafeLocalJoin(dir, "folder", "sub/file.txt"),
		filepath.Join(dir, "folder", "sub", "file.txt"); got != want {
		t.Errorf("SafeLocalJoin(dir, folder, rel) = %q, want %q", got, want)
	}
}
