package remotefs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
)

// Walk: depth-first over a seeded tree, directories before contents,
// keys anchored with the S3 slash conventions, and abort-on-error.
func TestWalkLocal(t *testing.T) {
	root := t.TempDir()
	dirs := []string{"docs", "docs/deep", "pics"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"readme.md":       "x",
		"docs/a.txt":      "aa",
		"docs/deep/b.txt": "b",
		"pics/photo.bin":  "12345",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	fs, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}

	var keys []string
	sizes := map[string]int64{}
	err = Walk(context.Background(), fs, "/", func(e listing.Entry) error {
		keys = append(keys, e.Key)
		if !e.IsDir {
			sizes[e.Key] = e.Size
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Pre-order DFS: each directory is visited directly before its
	// contents, and listings sort folders before files.
	want := []string{"/docs/", "/docs/deep/", "/docs/deep/b.txt", "/docs/a.txt", "/pics/", "/pics/photo.bin", "/readme.md"}
	if len(keys) != len(want) {
		t.Fatalf("walk keys = %v", keys)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Fatalf("walk order = %v, want %v", keys, want)
		}
	}
	if sizes["/pics/photo.bin"] != 5 || sizes["/docs/deep/b.txt"] != 1 {
		t.Fatalf("sizes = %v", sizes)
	}

	// Scoped walk of one subtree.
	var sub []string
	if err := Walk(context.Background(), fs, "/docs", func(e listing.Entry) error {
		sub = append(sub, e.Key)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(sub) != 3 || sub[0] != "/docs/deep/" || sub[1] != "/docs/deep/b.txt" || sub[2] != "/docs/a.txt" {
		t.Fatalf("scoped walk = %v", sub)
	}

	// fn errors abort the walk.
	boomerang := Walk(context.Background(), fs, "/", func(e listing.Entry) error {
		return os.ErrNotExist
	})
	if boomerang == nil {
		t.Fatal("fn error must propagate")
	}
}
