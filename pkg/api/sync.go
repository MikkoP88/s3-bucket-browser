package api

import (
	"context"
	"fmt"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/syncplan"
)

// SyncFile names one planned item: its slash-relative path and the size the
// plan's source side reported (uploads carry the local size, downloads and
// both delete vectors the target side's).
type SyncFile struct {
	Rel  string `json:"rel"`
	Size int64  `json:"size"`
}

// SyncPlanInfo is everything the Synchronize dialog presents and runs: the
// two copy vectors (local→s3 and s3→local), the two delete vectors (target
// files absent at the source — the delete leg is opt-in exactly like the
// CLI's --delete), the unchanged count, and the side identities the run
// needs (bucket, normalized prefix, local dir, source id).
type SyncPlanInfo struct {
	Bucket    string     `json:"bucket"`
	Prefix    string     `json:"prefix"` // dirPrefix form ("photos/")
	LocalDir  string     `json:"localDir"`
	Source    string     `json:"source"` // s3 side's source id ("" = view source)
	Uploads   []SyncFile `json:"uploads"`
	Downloads []SyncFile `json:"downloads"`
	DelRemote []SyncFile `json:"delRemote"` // s3 files absent locally
	DelLocal  []SyncFile `json:"delLocal"`  // local files absent at the s3 side
	Skipped   int        `json:"skipped"`   // size-equal pairs (either direction)
}

// SyncPreview plans a local↔s3 synchronization — the same walk the compare
// pane rides (any local dir vs any bucket prefix, sizes only, mtimes never
// consulted because clocks lie) through the same shared predicate the CLI's
// sync command computes its plan with, so the two faces can never drift.
// The delete vectors are always reported; whether the delete leg RUNS is
// the caller's explicit choice (the dialog's checkbox, the CLI's --delete).
// Tracked as a task under the deep-compare budget: a recursive two-side
// walk is a long, killable action exactly like CompareAny. Other side pairs
// (remote sources, s3↔s3) are refused honestly — the CLI's own contract —
// pointing the user at the compare + copy surfaces that already serve them.
func (a *App) SyncPreview(x, y CompareRef) (info *SyncPlanInfo, err error) {
	var local, s3 CompareRef
	for _, r := range []CompareRef{x, y} {
		switch r.Kind {
		case "local":
			local = r
		case "s3":
			s3 = r
		}
	}
	if local.Kind != "local" || s3.Kind != "s3" {
		return nil, fmt.Errorf("synchronize needs one local folder and one S3 prefix — use Compare + copy for other pairs")
	}
	info = &SyncPlanInfo{
		Bucket:   s3.Bucket,
		Prefix:   dirPrefix(s3.Prefix),
		LocalDir: local.Dir,
		Source:   s3.Source,
	}
	task := a.tasks.add("sync-preview", fmt.Sprintf("sync %s ↔ %s", info.LocalDir, compareRefLabel(s3)))
	ctx, cancel := context.WithTimeout(task.ctx, a.tuning().CompareTimeout())
	defer cancel()
	defer func() { task.finish(err, false) }()

	lm, err := a.walkCompareSide(ctx, local)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", compareRefLabel(local), err)
	}
	sm, err := a.walkCompareSide(ctx, s3)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", compareRefLabel(s3), err)
	}
	lSizes := sizesOnly(lm)
	sSizes := sizesOnly(sm)

	up, delRemote, skipped := syncplan.Plan(lSizes, sSizes, true)
	down, delLocal, _ := syncplan.Plan(sSizes, lSizes, true)
	info.Skipped = skipped
	info.Uploads = withSizes(up, lSizes)
	info.Downloads = withSizes(down, sSizes)
	info.DelRemote = withSizes(delRemote, sSizes)
	info.DelLocal = withSizes(delLocal, lSizes)
	task.progress(len(info.Uploads) + len(info.Downloads))
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
