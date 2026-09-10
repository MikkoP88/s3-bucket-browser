// walk.go: recursive traversal over any FS engine — the remotefs analogue
// of listing.Walk for the S3 pipeline. Used by count-then-act deletes
// (PLAN.md §9) and cross-source transfer planning.
package remotefs

import (
	"context"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
)

// Walk visits every entry under dir (excluding dir itself), depth-first,
// each directory before its contents. fn receives entries whose Key is the
// anchored path (directories keep their trailing slash). An error from fn
// aborts the walk and is returned.
func Walk(ctx context.Context, fs FS, dir string, fn func(listing.Entry) error) error {
	entries, err := fs.List(ctx, dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
		if e.IsDir {
			if err := Walk(ctx, fs, e.Key, fn); err != nil {
				return err
			}
		}
	}
	return nil
}
