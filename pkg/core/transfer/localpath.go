package transfer

import (
	"path/filepath"
	"strings"
)

// reservedDevices are the Windows device names: a segment whose base
// (before the first dot) matches one of these maps to a device, not a
// file, so a key literally named NUL would silently discard its bytes
// on download. Case-insensitive, per Windows.
var reservedDevices = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// safeSeg neutralizes one path segment for the local filesystem:
// traversal collapses to an underscore, Windows-invalid characters (the
// colon included — "C:" mid-path is a drive prefix) become underscores,
// trailing dots and spaces (invalid in Windows names) are trimmed, and
// reserved device names are pushed past their device meaning with a
// leading underscore. The identity for every ordinary name is the
// point: normal keys map to themselves, byte for byte.
func safeSeg(seg string) string {
	if seg == "" || seg == "." || seg == ".." {
		return "_"
	}
	seg = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, seg)
	seg = strings.TrimRight(seg, ". ")
	if seg == "" {
		return "_"
	}
	if base, _, _ := strings.Cut(seg, "."); reservedDevices[strings.ToLower(base)] {
		return "_" + seg
	}
	return seg
}

// SafeLocalJoin maps server-supplied relative paths (an object key, a
// listing-derived base, a staged local rename) onto a destination
// directory without ever escaping it. A hostile endpoint — or a merely
// exotic key: S3 legally allows "..", backslashes and names Windows
// reserves — must never turn a download into an arbitrary-path write
// (filepath.Join resolves ".." lexically, so the raw join escapes).
// Traversal is neutralized in place — the object still downloads,
// under a contained name — rather than refused, and both slash kinds
// are treated as separators: which side of the wire a path came from
// is the server's business, containment is ours.
func SafeLocalJoin(dir string, parts ...string) string {
	segs := make([]string, 0, 8)
	for _, p := range parts {
		for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
			segs = append(segs, safeSeg(seg))
		}
	}
	all := append([]string{dir}, segs...)
	return filepath.Join(all...)
}
