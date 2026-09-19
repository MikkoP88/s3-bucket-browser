package api

import "testing"

// vcopyDstKey is the versioned copy's destination mapping. The folder case
// is the regression guard: a selected folder must re-root its subtree
// under its own name (Explorer paste semantics — pasting docs/ into
// target/ creates target/docs/...), never flatten the contents straight
// into the destination.
func TestVcopyDstKey(t *testing.T) {
	for _, tc := range []struct {
		name     string
		dstPrefx string
		sel      string
		exact    bool
		srcKey   string
		want     string
	}{
		{
			name:     "folder re-roots its subtree under its own name",
			dstPrefx: "target/",
			sel:      "docs/",
			srcKey:   "docs/a.txt",
			want:     "target/docs/a.txt",
		},
		{
			name:     "folder into the bucket root keeps its name",
			dstPrefx: "",
			sel:      "docs/",
			srcKey:   "docs/sub/b.txt",
			want:     "docs/sub/b.txt",
		},
		{
			name:     "nested selected folder uses its leaf under the new root",
			dstPrefx: "target/",
			sel:      "a/b/",
			srcKey:   "a/b/c/d.txt",
			want:     "target/b/c/d.txt", // matches the plain copy path (copyMove)
		},
		{
			name:     "folder marker itself keeps the marker (slash) form",
			dstPrefx: "target/",
			sel:      "docs/",
			srcKey:   "docs/",
			want:     "target/docs/",
		},
		{
			name:     "exact object lands under its base name",
			dstPrefx: "target/",
			sel:      "docs/readme.md",
			exact:    true,
			srcKey:   "docs/readme.md",
			want:     "target/readme.md",
		},
		{
			name:     "paste into the same place maps onto itself (skip guard)",
			dstPrefx: "",
			sel:      "docs/",
			srcKey:   "docs/a.txt",
			want:     "docs/a.txt",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := vcopyDstKey(tc.dstPrefx, tc.sel, tc.exact, tc.srcKey)
			if got != tc.want {
				t.Fatalf("vcopyDstKey(%q, %q, %v, %q) = %q, want %q",
					tc.dstPrefx, tc.sel, tc.exact, tc.srcKey, got, tc.want)
			}
		})
	}
}
