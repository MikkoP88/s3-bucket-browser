// Package atomicfile writes local state the crash-safe way: bytes land
// in a sibling temp file, get flushed to disk, and replace the target by
// rename — the same critical-data contract the transfer engine's staging
// established for downloads, applied to every piece of state the app
// persists. os.WriteFile opens the target with O_TRUNC, so a full disk,
// a quota hit, or a crash mid-write destroys a previously good file — a
// truncated store reads as "invalid profile store" and there is no
// second copy. With the rename, a failure at any point leaves the old
// bytes untouched, and a hard kill at worst strands one temp file,
// never a partial at the final name.
package atomicfile

import (
	"os"
	"path/filepath"
)

// rename is a seam for the failure tests: it is the only step that
// touches the target, and the contract — "a failing write never damages
// the previous good file" — is pinned by breaking it.
var rename = os.Rename

// Write persists data at path, replacing any existing file atomically.
// The temp is created in the target's directory (same volume → the
// rename cannot degrade to a copy) and carries perm from birth, so the
// final path is never wider than ordered. The caller owns the parent
// directory's existence.
func Write(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".s3b-tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	discard := func() {
		f.Close()
		os.Remove(tmp)
	}
	_ = f.Chmod(perm) // best-effort: Windows ignores most of it
	if _, err := f.Write(data); err != nil {
		discard()
		return err
	}
	// Sync before the rename: the bytes reach the platter before the
	// final name ever points at them, so power loss cannot expose a
	// rename whose content never landed.
	if err := f.Sync(); err != nil {
		discard()
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
