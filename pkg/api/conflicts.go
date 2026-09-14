// conflicts.go: destination collision pre-check. The GUI plans a transfer
// exactly like TransferCross would (same planner, same destination-side
// stat), reports which files already exist at the destination, and only
// then asks for a conflict policy — a clean destination never shows a
// conflict dialog at all.
package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ConflictInfo is one source/destination collision shown in the conflict
// dialog: both sides' size and modification time so the user can decide
// per file.
type ConflictInfo struct {
	Key         string `json:"key"`         // display name (destination-relative)
	DecisionKey string `json:"decisionKey"` // engine key for per-file decisions
	SrcSize     int64  `json:"srcSize"`
	SrcTime     string `json:"srcTime,omitempty"` // RFC3339, "" when unknown
	DstSize     int64  `json:"dstSize"`
	DstTime     string `json:"dstTime,omitempty"`
}

// Pre-check budgets. The planner walks everything (the same walk the
// transfer itself performs); destination stats are one HEAD/Stat per file,
// so both the planned-file count and the returned row list are capped.
// Beyond the caps the caller falls back to the classic whole-transfer
// policy dialog.
const (
	conflictMaxFiles = 500 // planned files worth stat-ing
	conflictMaxRows  = 200 // rows returned to the dialog
)

// CheckConflicts plans the transfer described by (items, localPaths, dest)
// and reports the planned files that already exist at the destination.
// decisionKey is the key the engine's per-file decision map understands:
// for Upload the destination object key, for Download the source object
// key, for TransferCross the planned destination path.
func (a *App) CheckConflicts(items []XferItem, localPaths []string, dest XferDest) ([]ConflictInfo, error) {
	dst, err := a.resolveXferDest(dest)
	if err != nil {
		return nil, err
	}
	pctx, cancel := a.quickCtx()
	plan, err := a.planXfer(pctx, items, localPaths, dst)
	cancel()
	if err != nil {
		return nil, err
	}
	// Success must serialize as [], not null: the frontend treats a null
	// probe result as "pre-check failed" and falls back to the classic
	// whole-transfer dialog — a clean destination must stay silent.
	if len(plan.files) == 0 {
		return []ConflictInfo{}, nil
	}
	if len(plan.files) > conflictMaxFiles {
		return nil, fmt.Errorf("too many files to pre-check (%d)", len(plan.files))
	}
	sctx, scancel := a.quickCtx()
	defer scancel()

	var out = []ConflictInfo{}
	var truncated bool
	for i := range plan.files {
		f := &plan.files[i]
		size, mtime, exists := xferDestStat(sctx, dst, f.dstPath)
		if !exists {
			continue
		}
		if len(out) == conflictMaxRows {
			truncated = true
			break
		}
		out = append(out, ConflictInfo{
			Key:         conflictDisplayKey(dst, f.dstPath),
			DecisionKey: f.dstPath,
			SrcSize:     f.size,
			SrcTime:     rfc3339(f.mtime),
			DstSize:     size,
			DstTime:     rfc3339(mtime),
		})
	}
	// A truncated list cannot carry per-file decisions — the dialog then
	// applies the chosen action to the WHOLE transfer (classic behavior).
	if truncated {
		return nil, fmt.Errorf("too many conflicting files to list")
	}
	return out, nil
}

// xferDestStat returns the destination file's size and mtime when the path
// is taken. An unreadable destination reports not-taken: a failed probe
// must never fake a conflict (or block one).
func xferDestStat(ctx context.Context, dst xferDestSide, p string) (int64, time.Time, bool) {
	switch dst.kind {
	case "s3":
		out, err := dst.client.S3.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(dst.bucket), Key: aws.String(p),
		})
		if err != nil {
			return 0, time.Time{}, false
		}
		return aws.ToInt64(out.ContentLength), aws.ToTime(out.LastModified), true
	case "remote":
		e, err := dst.fs.Stat(ctx, p)
		if err != nil || e.IsDir {
			return 0, time.Time{}, false
		}
		return e.Size, aws.ToTime(e.LastModified), true
	default:
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return 0, time.Time{}, false
		}
		return st.Size(), st.ModTime(), true
	}
}

// conflictDisplayKey renders a destination path relative to the transfer's
// destination directory (what the dialog lists after the common prefix).
func conflictDisplayKey(dst xferDestSide, p string) string {
	switch dst.kind {
	case "s3":
		if dst.dir != "" {
			return strings.TrimPrefix(p, dst.dir)
		}
		return p
	case "remote":
		base := strings.TrimSuffix(dst.dir, "/")
		if base != "" && strings.HasPrefix(p, base+"/") {
			return p[len(base)+1:]
		}
		return strings.TrimPrefix(p, "/")
	default:
		if rel, err := filepath.Rel(dst.dir, p); err == nil {
			return filepath.ToSlash(rel)
		}
		return filepath.Base(p)
	}
}

// filePolicy resolves the effective policy for one file: a per-file
// decision from the conflict dialog wins over the job-wide policy.
func filePolicy(decisions map[string]string, key, fallback string) string {
	if d, ok := decisions[key]; ok {
		switch d {
		case PolicyOverwrite, PolicySkip, PolicyRename:
			return d
		}
	}
	return fallback
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
