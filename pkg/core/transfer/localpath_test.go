package transfer

import (
	"path/filepath"
	"strings"
	"testing"
)

// SafeLocalJoin contains server-supplied paths inside the destination:
// ordinary keys map to themselves (byte for byte), traversal and the
// Windows minefield never escape.
func TestSafeLocalJoin(t *testing.T) {
	const dir = `C:\Users\demo\Downloads`
	for _, tc := range []struct{ in, want string }{
		// identity for ordinary keys — the compat contract every
		// existing transfer rides on
		{"notes.txt", `C:\Users\demo\Downloads\notes.txt`},
		{"docs/notes.txt", `C:\Users\demo\Downloads\docs\notes.txt`},
		{"a/b/c/report 2026.pdf", `C:\Users\demo\Downloads\a\b\c\report 2026.pdf`},
		{"archive.tar.gz", `C:\Users\demo\Downloads\archive.tar.gz`},
		// traversal is neutralized in place, never resolved
		{"../evil.txt", `C:\Users\demo\Downloads\_\evil.txt`},
		{"..\\..\\evil.txt", `C:\Users\demo\Downloads\_\_\evil.txt`},
		{"docs/../../../Windows/system32/evil.dll", `C:\Users\demo\Downloads\docs\_\_\_\Windows\system32\evil.dll`},
		{"/abs/path.txt", `C:\Users\demo\Downloads\abs\path.txt`},
		// drive and UNC shapes become literal contained names
		{"C:/evil.txt", `C:\Users\demo\Downloads\C_\evil.txt`},
		{`\\server\share\evil.txt`, `C:\Users\demo\Downloads\server\share\evil.txt`},
		// reserved device names are pushed past their device meaning
		{"NUL", `C:\Users\demo\Downloads\_NUL`},
		{"con.txt", `C:\Users\demo\Downloads\_con.txt`},
		{"logs/COM1", `C:\Users\demo\Downloads\logs\_COM1`},
		// Windows-invalid characters and trailing dots/spaces
		{"re:port?.txt", `C:\Users\demo\Downloads\re_port_.txt`},
		{"name. ", `C:\Users\demo\Downloads\name`},
		{"name...", `C:\Users\demo\Downloads\name`},
		// dot segments collapse; dotfiles survive
		{"./a/.hidden", `C:\Users\demo\Downloads\_\a\.hidden`},
	} {
		if got := SafeLocalJoin(dir, tc.in); got != tc.want {
			t.Errorf("SafeLocalJoin(%q, %q) = %q, want %q", dir, tc.in, got, tc.want)
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
