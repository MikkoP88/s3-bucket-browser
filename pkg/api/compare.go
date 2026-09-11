package api

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// CompareRef addresses one side of a pane-to-pane comparison: a local
// directory, a directory on a remote (non-S3) source, or a bucket prefix on
// an S3 source ("" = the default). The wire statuses keep their historical
// local/remote names — "local" means the left (x) side, "remote" the right
// (y) side — so the grid decorations are unchanged; the summary dialog
// labels the two sides for the user.
type CompareRef struct {
	Kind   string `json:"kind"`   // "local" | "remote" | "s3"
	Dir    string `json:"dir"`    // local: OS path; remote: anchored ("/" = source root)
	Source string `json:"source"` // remote: source id or name; s3: "" = default source
	Bucket string `json:"bucket"` // s3
	Prefix string `json:"prefix"` // s3
}

// compareTimeout bounds one deep pane-to-pane walk (recursive on both
// sides — longer than quickOpTimeout, shorter than unbounded).
const compareTimeout = 5 * time.Minute

// CompareAny recursively compares any two sides (local directory, remote
// source directory, S3 bucket prefix) and returns the merged rows, sorted by
// relative path. Files only — folder markers and empty directories are
// ignored (same semantics as the original CompareDir).
func (a *App) CompareAny(x, y CompareRef) ([]CompareRow, error) {
	ctx, cancel := context.WithTimeout(a.ctx, compareTimeout)
	defer cancel()
	xm, err := a.walkCompareSide(ctx, x)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", compareRefLabel(x), err)
	}
	ym, err := a.walkCompareSide(ctx, y)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", compareRefLabel(y), err)
	}
	return compareFileMaps(xm, ym), nil
}

// compareRefLabel names a side in error messages.
func compareRefLabel(r CompareRef) string {
	switch r.Kind {
	case "local":
		return r.Dir
	case "remote":
		return r.Source + ":" + r.Dir
	case "s3":
		return "s3://" + r.Bucket + "/" + r.Prefix
	}
	return r.Kind
}

// walkCompareSide collects the files under one side, keyed by the
// slash-separated path relative to the compared dir.
func (a *App) walkCompareSide(ctx context.Context, r CompareRef) (map[string]localFile, error) {
	switch r.Kind {
	case "local":
		return walkLocalFiles(filepath.Clean(r.Dir))
	case "remote":
		src, fs, err := a.remoteSource(r.Source)
		if err != nil {
			return nil, err
		}
		unlock := a.lockSrcs(src.ID)
		defer unlock()
		root := remotefs.CleanPath(r.Dir)
		if root == "" {
			root = "/"
		}
		prefix := strings.TrimSuffix(root, "/") + "/"
		out := map[string]localFile{}
		err = remotefs.Walk(ctx, fs, root, func(e listing.Entry) error {
			if e.IsDir {
				return nil
			}
			rel := strings.TrimPrefix(e.Key, prefix)
			if root == "/" {
				rel = strings.TrimPrefix(e.Key, "/")
			}
			if rel == "" || strings.HasSuffix(rel, "/") {
				return nil
			}
			f := localFile{size: e.Size}
			if e.LastModified != nil {
				f.mtime = e.LastModified.UnixMilli()
			}
			out[rel] = f
			return nil
		})
		return out, err
	case "s3":
		c, err := a.s3ClientFor(r.Source)
		if err != nil {
			return nil, err
		}
		prefix := dirPrefix(r.Prefix)
		out := map[string]localFile{}
		err = listing.Walk(ctx, c.S3, r.Bucket, prefix, func(o s3types.Object) error {
			key := aws.ToString(o.Key)
			if strings.HasSuffix(key, "/") {
				return nil // folder markers
			}
			rel := strings.TrimPrefix(key, prefix)
			if rel == "" {
				return nil
			}
			f := localFile{size: aws.ToInt64(o.Size)}
			if o.LastModified != nil {
				f.mtime = o.LastModified.UnixMilli()
			}
			out[rel] = f
			return nil
		})
		return out, err
	}
	return nil, fmt.Errorf("unknown compare side kind %q", r.Kind)
}

// compareFileMaps is the pure comparison (unit-tested): left files vs right
// files keyed by relative path. "local" in the status names is the left (x)
// side, "remote" the right (y) side.
func compareFileMaps(left, right map[string]localFile) []CompareRow {
	keys := make([]string, 0, len(left)+len(right))
	seen := map[string]bool{}
	for k := range left {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range right {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	rows := make([]CompareRow, 0, len(keys))
	for _, k := range keys {
		l, hasL := left[k]
		r, hasR := right[k]
		row := CompareRow{Key: k}
		if hasL {
			row.LocalSize = l.size
			row.LocalMtime = l.mtime
		}
		if hasR {
			row.RemoteSize = r.size
			row.RemoteMtime = r.mtime
		}
		switch {
		case !hasR:
			row.Status = CmpOnlyLocal
		case !hasL:
			row.Status = CmpOnlyRemote
		case l.size != r.size:
			row.Status = CmpSizeDiff
		case l.mtime > r.mtime+2000: // clocks differ; 2s tolerance
			row.Status = CmpNewerLocal
		case r.mtime > l.mtime+2000:
			row.Status = CmpNewerRemote
		default:
			row.Status = CmpSame
		}
		rows = append(rows, row)
	}
	return rows
}
