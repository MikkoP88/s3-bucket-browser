// copy_versions.go: `cp --versions` — recreate the source's version
// timeline at another bucket. Both operands must be S3 (s3:// URIs or
// saved S3 sources): a same-client pair copies each source version
// server-side, different sources stream through this machine. mv
// --versions purges the copied source versions afterwards behind the
// same --force gate as rm --versions.
package cli

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
)

// copyS3Versions plans and runs one versioned copy. Returns the number of
// versions written (delete markers recreated are reported separately by
// the caller-visible verbose lines and counted into nothing else).
func copyS3Versions(ctx context.Context, srcC, dstC *s3client.Client, su, du s3URI, opts copyOptions) (int, error) {
	// Without destination versioning every write overwrites and the
	// timeline collapses to the last version — refuse instead.
	status, err := versioning.Status(ctx, dstC.S3, du.Bucket)
	if err != nil {
		return 0, opErr(err)
	}
	if status != "Enabled" {
		return 0, usageErr("destination bucket %s must have versioning enabled (it is %s) — without it the copied timeline collapses to the last write",
			du.Bucket, versionStatusName(status))
	}

	if su.Key == "" && !opts.Recursive {
		return 0, usageErr("copying a whole bucket's history needs --recursive")
	}
	exact := su.HasPrefix && !su.IsPrefix && !opts.Recursive
	srcPrefix := su.Key
	if !exact {
		srcPrefix = dirPrefix(su)
	}

	dstKey := func(srcKey string) string {
		if exact {
			if du.HasPrefix && !du.IsPrefix {
				return du.Key // exact destination object (rename)
			}
			return joinKeyNoSlash(du.Key, path.Base(srcKey))
		}
		return joinKeyNoSlash(du.Key, strings.TrimPrefix(srcKey, srcPrefix))
	}
	if exact && su.Bucket == du.Bucket && dstKey(su.Key) == su.Key {
		return 0, usageErr("source and destination are the same object")
	}
	if !exact && su.Bucket == du.Bucket && srcPrefix == dirPrefix(du) {
		return 0, usageErr("source and destination are the same prefix")
	}

	plan, err := versioning.PlanVersionedCopy(ctx, srcC.S3, su.Bucket, srcPrefix, exact)
	if err != nil {
		return 0, opErr(err)
	}
	if len(plan) == 0 {
		return 0, nil // nothing versioned under the operand — success
	}
	total := versioning.VersionsTotal(plan)
	if opts.Move && total > rmForceThreshold && !opts.Force && !opts.DryRun {
		return 0, opErr(fmt.Errorf(
			"mv --versions would permanently destroy %d source version(s) — pass --force to proceed", total))
	}

	stats, err := versioning.CopyVersionedTimelines(ctx, srcC.S3, su.Bucket, dstC.S3, du.Bucket, plan, dstKey,
		versioning.CopyVersionsOptions{
			DryRun: opts.DryRun,
			OnItem: func(key, versionID string, isMarker bool, n, total int) {
				dk := dstKey(key)
				if isMarker {
					rprintf("recreate delete marker s3://%s/%s\n", du.Bucket, dk)
					return
				}
				if opts.DryRun {
					rprintf("would copy s3://%s/%s@%s -> s3://%s/%s (%d/%d)\n",
						su.Bucket, key, versionID, du.Bucket, dk, n, total)
				} else if flagVerbose {
					col.dim.Fprintf(out, "copied s3://%s/%s@%s -> s3://%s/%s (%d/%d)\n",
						su.Bucket, key, versionID, du.Bucket, dk, n, total)
				}
			},
		})
	if err != nil {
		return stats.VersionsCopied, opErr(err)
	}

	if opts.Move && !opts.DryRun {
		for _, t := range plan {
			if _, err := versioning.DeleteAllVersions(ctx, srcC.S3, su.Bucket, t.Key); err != nil {
				return stats.VersionsCopied, opErr(err)
			}
		}
	}
	return stats.VersionsCopied, nil
}

// runCopyVersions resolves both operands of a --versions copy to their
// client/URI forms — every combination of s3:// and saved S3 sources —
// and runs it.
func runCopyVersions(ctx context.Context, c *s3client.Client, src, dst string, srcS3, dstS3 *s3SourceRef, opts copyOptions) (int, error) {
	resolve := func(arg string, ref *s3SourceRef) (*s3client.Client, s3URI, bool, error) {
		if ref != nil {
			return ref.c, ref.s3URI(), true, nil
		}
		if strings.HasPrefix(arg, "s3://") {
			u, err := parseS3URI(arg)
			if err != nil {
				return nil, s3URI{}, false, err
			}
			return c, u, true, nil
		}
		return nil, s3URI{}, false, nil
	}
	srcC, su, okSrc, err := resolve(src, srcS3)
	if err != nil {
		return 0, err
	}
	dstC, du, okDst, err := resolve(dst, dstS3)
	if err != nil {
		return 0, err
	}
	if !okSrc || !okDst {
		return 0, usageErr("--versions needs both operands on S3 (s3:// URIs or saved S3 sources)")
	}
	return copyS3Versions(ctx, srcC, dstC, su, du, opts)
}

// versionStatusName renders a bucket versioning status for errors.
func versionStatusName(status string) string {
	switch status {
	case "Suspended":
		return "suspended"
	case "":
		return "off (never enabled)"
	}
	return status
}
