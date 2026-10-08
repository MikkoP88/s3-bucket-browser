package api

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/syncplan"
)

// SyncFile names one planned item: its slash-relative path and the size the
// plan's source side reported (each copy vector carries its from-side size,
// each delete vector the to-side's).
type SyncFile struct {
	Rel  string `json:"rel"`
	Size int64  `json:"size"`
}

// SyncPlanInfo is everything the Synchronize dialog presents and runs: the
// two copy vectors (x→y and y→x, in the caller's own seating order — the
// same grammar CompareAny uses), the two delete vectors (target files
// absent at the source — the delete leg is opt-in exactly like the CLI's
// --delete), and the unchanged count. The side identities are not echoed:
// the dialog holds the very refs it passed.
type SyncPlanInfo struct {
	CopiesXY []SyncFile `json:"copiesXY"` // x → y
	CopiesYX []SyncFile `json:"copiesYX"` // y → x
	DelY     []SyncFile `json:"delY"`     // at y, absent at x
	DelX     []SyncFile `json:"delX"`     // at x, absent at y
	Skipped  int        `json:"skipped"`  // size-equal pairs (either direction)
}

// sameSyncSide reports whether two refs name one location: the honest
// refusal for a two-way plan against itself (every vector would be empty
// or self-clobbering).
func sameSyncSide(x, y CompareRef) bool {
	if x.Kind != y.Kind {
		return false
	}
	switch x.Kind {
	case "local":
		return filepath.Clean(x.Dir) == filepath.Clean(y.Dir)
	case "s3":
		return x.Source == y.Source && x.Bucket == y.Bucket && dirPrefix(x.Prefix) == dirPrefix(y.Prefix)
	case "remote":
		return x.Source == y.Source && remotefs.CleanPath(x.Dir) == remotefs.CleanPath(y.Dir)
	}
	return false
}

// SyncPreview plans a synchronization between any two sides — a local
// folder, a remote source directory, an S3 bucket prefix — through the
// same walk the compare pane rides and the same shared predicate the
// CLI's sync command computes its plan with, so the two faces can never
// drift. The call's own order seats the sides (x→y is the first copy
// vector), the CompareAny grammar. mtimes are never consulted because
// clocks lie. The delete vectors are always reported; whether the delete
// leg RUNS is the caller's explicit choice (the dialog's checkbox, the
// CLI's --delete). Tracked as a task under the deep-compare budget: a
// recursive two-side walk is a long, killable action exactly like
// CompareAny. The one refusal is the degenerate pair: both sides naming
// the same location.
func (a *App) SyncPreview(x, y CompareRef) (info *SyncPlanInfo, err error) {
	if sameSyncSide(x, y) {
		return nil, fmt.Errorf("both sides are the same location (%s) — synchronization needs two different sides",
			compareRefLabel(x))
	}
	info = &SyncPlanInfo{}
	task := a.tasks.add("sync-preview", fmt.Sprintf("sync %s ↔ %s", compareRefLabel(x), compareRefLabel(y)))
	ctx, cancel := context.WithTimeout(task.ctx, a.tuning().CompareTimeout())
	defer cancel()
	defer func() { task.finish(err, false) }()

	xm, err := a.walkCompareSide(ctx, x)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", compareRefLabel(x), err)
	}
	ym, err := a.walkCompareSide(ctx, y)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", compareRefLabel(y), err)
	}
	xSizes := sizesOnly(xm)
	ySizes := sizesOnly(ym)

	xy, delY, skipped := syncplan.Plan(xSizes, ySizes, true)
	yx, delX, _ := syncplan.Plan(ySizes, xSizes, true)
	info.Skipped = skipped
	info.CopiesXY = withSizes(xy, xSizes)
	info.CopiesYX = withSizes(yx, ySizes)
	info.DelY = withSizes(delY, ySizes)
	info.DelX = withSizes(delX, xSizes)
	task.progress(len(info.CopiesXY) + len(info.CopiesYX))
	return info, nil
}

// sizesOnly strips the compare walker's mtime half away — the sync
// predicate reads sizes alone.
func sizesOnly(m map[string]localFile) map[string]int64 {
	out := make(map[string]int64, len(m))
	for rel, f := range m {
		out[rel] = f.size
	}
	return out
}

// withSizes pairs the predicate's sorted rel vector with the sizes its
// from-side reported, keeping the sort order.
func withSizes(rels []string, sizes map[string]int64) []SyncFile {
	out := make([]SyncFile, 0, len(rels))
	for _, rel := range rels {
		out = append(out, SyncFile{Rel: rel, Size: sizes[rel]})
	}
	return out
}
