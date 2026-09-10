package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"
)

// rmForceThreshold is the L1 safety gate: deleting more objects than this
// under a prefix requires --force (PLAN.md §9).
const rmForceThreshold = 50

// copyOptions drives cp/mv (mv = cp + source removal).
type copyOptions struct {
	Recursive    bool
	StorageClass string
	SSE          string // "AES256" (M1: SSE-S3)
	NoClobber    bool
	DryRun       bool
	Move         bool
}

func (o copyOptions) uploadOptions() transfer.UploadOptions {
	return transfer.UploadOptions{
		StorageClass: o.StorageClass,
		SSE:          o.SSE,
		NoClobber:    o.NoClobber,
	}
}

func cpCmd() *cobra.Command {
	return copyLikeCmd("cp", "Copy files (local↔S3, S3→S3 server-side)", false)
}

func mvCmd() *cobra.Command {
	return copyLikeCmd("mv", "Move files (copy, then delete sources on success)", true)
}

func copyLikeCmd(name, short string, move bool) *cobra.Command {
	var opts copyOptions
	cmd := &cobra.Command{
		Use:   fmt.Sprintf("%s SRC DST", name),
		Short: short,
		Long: "Directions: local→s3:// (upload), s3://→local (download), s3://→s3:// (server-side copy),\n" +
			"plus NAME:// source URIs (any saved non-S3 source) on either side.\n" +
			"SRC or DST being a directory/prefix (or --recursive) copies everything beneath it.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The S3 client is only needed when one side is s3:// — source-
			// URI-only copies work without any configured S3 profile.
			var c *s3client.Client
			if strings.HasPrefix(args[0], "s3://") || strings.HasPrefix(args[1], "s3://") {
				var err error
				c, err = resolveClient(cmd.Context())
				if err != nil {
					return err
				}
			}
			opts.Move = move
			n, err := runCopy(cmd.Context(), c, args[0], args[1], opts)
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{"verb": name, "items": n})
			}
			verb := "copied"
			if move {
				verb = "moved"
			}
			col.ok.Printf("%s %d item(s)\n", verb, n)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&opts.Recursive, "recursive", "r", false, "copy everything under a prefix/directory")
	f.StringVar(&opts.StorageClass, "storage-class", "", "storage class (STANDARD, GLACIER, ...)")
	f.StringVar(&opts.SSE, "sse", "", "server-side encryption (AES256)")
	f.BoolVar(&opts.NoClobber, "no-clobber", false, "skip uploads when the object already exists")
	f.BoolVar(&opts.DryRun, "dry-run", false, "show what would transfer, do nothing")
	return cmd
}

// runCopy dispatches on direction and returns the item count. Source URIs
// (NAME:// — any saved non-S3 source, M10.5) take the remotefs pipeline;
// everything else keeps its historical path.
func runCopy(ctx context.Context, c *s3client.Client, src, dst string, opts copyOptions) (int, error) {
	srcRef, err := dialSourceURI(ctx, src)
	if err != nil {
		return 0, err
	}
	dstRef, err := dialSourceURI(ctx, dst)
	if err != nil {
		srcRef.Close()
		return 0, err
	}
	defer srcRef.Close()
	defer dstRef.Close()
	if srcRef != nil || dstRef != nil {
		return copyRemoteDispatch(ctx, c, src, dst, srcRef, dstRef, opts)
	}
	srcS3 := strings.HasPrefix(src, "s3://")
	dstS3 := strings.HasPrefix(dst, "s3://")
	switch {
	case srcS3 && dstS3:
		return copyS3ToS3(ctx, c, src, dst, opts)
	case srcS3 && !dstS3:
		return downloadPath(ctx, c, src, dst, opts)
	case !srcS3 && dstS3:
		return uploadPath(ctx, c, src, dst, opts)
	default:
		return 0, usageErr("cp between two local paths is not supported")
	}
}

// joinKeyNoSlash builds an object key (no trailing slash) from parts.
func joinKeyNoSlash(parts ...string) string {
	return strings.TrimSuffix(transfer.JoinKey(parts...), "/")
}

func isDirPath(p string) bool {
	if strings.HasSuffix(p, "/") || strings.HasSuffix(p, string(filepath.Separator)) {
		return true
	}
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func uploadPath(ctx context.Context, c *s3client.Client, localPath, dst string, opts copyOptions) (int, error) {
	u, err := parseS3URI(dst)
	if err != nil {
		return 0, err
	}
	st, err := os.Stat(localPath)
	if err != nil {
		return 0, err
	}

	uploadOne := func(local, key string) error {
		if opts.DryRun {
			fmt.Printf("upload %s -> s3://%s/%s\n", local, u.Bucket, key)
			return nil
		}
		if err := transfer.UploadFile(ctx, c.S3, local, u.Bucket, key, opts.uploadOptions()); err != nil {
			return fmt.Errorf("%s: %w", local, err)
		}
		if opts.Move {
			if err := os.Remove(local); err != nil {
				return err
			}
		}
		if flagVerbose {
			col.dim.Printf("up s3://%s/%s\n", u.Bucket, key)
		}
		return nil
	}

	if !st.IsDir() {
		key := u.Key
		if u.IsPrefix || !u.HasPrefix { // DST is a folder or bucket root: keep filename
			key = joinKeyNoSlash(u.Key, filepath.Base(localPath))
		}
		if err := uploadOne(localPath, key); err != nil {
			return 0, err
		}
		return 1, nil
	}

	if !opts.Recursive {
		return 0, usageErr("%s is a directory: add --recursive to upload its contents", localPath)
	}
	count := 0
	err = filepath.WalkDir(localPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(localPath, p)
		if err != nil {
			return err
		}
		count++
		return uploadOne(p, joinKeyNoSlash(u.Key, rel))
	})
	if err != nil {
		return count - 1, err
	}
	return count, nil
}

func downloadPath(ctx context.Context, c *s3client.Client, src, dst string, opts copyOptions) (int, error) {
	u, err := parseS3URI(src)
	if err != nil {
		return 0, err
	}

	downloadOne := func(key, local string) error {
		if opts.DryRun {
			fmt.Printf("download s3://%s/%s -> %s\n", u.Bucket, key, local)
			return nil
		}
		if err := transfer.DownloadFile(ctx, c.S3, u.Bucket, key, local, transfer.DownloadOptions{}); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if flagVerbose {
			col.dim.Printf("down %s\n", local)
		}
		return nil
	}

	if u.IsPrefix || opts.Recursive {
		prefix := dirPrefix(u)
		count := 0
		var keys []string
		err = listing.Walk(ctx, c.S3, u.Bucket, prefix, func(o s3types.Object) error {
			key := aws.ToString(o.Key)
			if strings.HasSuffix(key, "/") {
				return nil // folder markers
			}
			rel := strings.TrimPrefix(key, prefix)
			count++
			keys = append(keys, key)
			return downloadOne(key, filepath.Join(dst, filepath.FromSlash(rel)))
		})
		if err != nil {
			return count - 1, err
		}
		if opts.Move && !opts.DryRun && len(keys) > 0 {
			res, err := transfer.DeleteKeys(ctx, c.S3, u.Bucket, keys)
			reportDelete(res)
			if err != nil {
				return count, err
			}
		}
		return count, nil
	}

	local := dst
	if isDirPath(dst) {
		local = filepath.Join(dst, path.Base(u.Key))
	}
	if err := downloadOne(u.Key, local); err != nil {
		return 0, err
	}
	if opts.Move && !opts.DryRun {
		res, err := transfer.DeleteKeys(ctx, c.S3, u.Bucket, []string{u.Key})
		reportDelete(res)
		if err != nil {
			return 1, err
		}
	}
	return 1, nil
}

func copyS3ToS3(ctx context.Context, c *s3client.Client, src, dst string, opts copyOptions) (int, error) {
	su, err := parseS3URI(src)
	if err != nil {
		return 0, err
	}
	du, err := parseS3URI(dst)
	if err != nil {
		return 0, err
	}

	copyOne := func(srcKey, dstKey string) error {
		if opts.DryRun {
			fmt.Printf("copy s3://%s/%s -> s3://%s/%s\n", su.Bucket, srcKey, du.Bucket, dstKey)
			return nil
		}
		if err := transfer.Copy(ctx, c.S3, su.Bucket, srcKey, du.Bucket, dstKey); err != nil {
			return fmt.Errorf("%s: %w", srcKey, err)
		}
		if flagVerbose {
			col.dim.Printf("copy s3://%s/%s\n", du.Bucket, dstKey)
		}
		return nil
	}

	if su.IsPrefix || opts.Recursive {
		prefix := dirPrefix(su)
		count := 0
		var keys []string
		err = listing.Walk(ctx, c.S3, su.Bucket, prefix, func(o s3types.Object) error {
			key := aws.ToString(o.Key)
			if strings.HasSuffix(key, "/") {
				return nil
			}
			count++
			keys = append(keys, key)
			return copyOne(key, joinKeyNoSlash(du.Key, strings.TrimPrefix(key, prefix)))
		})
		if err != nil {
			return count - 1, err
		}
		if opts.Move && !opts.DryRun && len(keys) > 0 {
			res, err := transfer.DeleteKeys(ctx, c.S3, su.Bucket, keys)
			reportDelete(res)
			if err != nil {
				return count, err
			}
		}
		return count, nil
	}

	if su.Bucket == du.Bucket && su.Key == du.Key {
		return 0, usageErr("source and destination are the same object")
	}
	dstKey := du.Key
	if du.IsPrefix || !du.HasPrefix {
		dstKey = joinKeyNoSlash(du.Key, path.Base(su.Key))
	}
	if err := copyOne(su.Key, dstKey); err != nil {
		return 0, err
	}
	if opts.Move && !opts.DryRun {
		res, err := transfer.DeleteKeys(ctx, c.S3, su.Bucket, []string{su.Key})
		reportDelete(res)
		if err != nil {
			return 1, err
		}
	}
	return 1, nil
}

func rmCmd() *cobra.Command {
	var recursive, force, dryRun, versions bool
	cmd := &cobra.Command{
		Use:   "rm s3://bucket[/prefix] | NAME://path",
		Short: "Delete objects or source files (folders need --recursive; large batches --force)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if r, err := dialSourceURI(cmd.Context(), args[0]); err != nil {
				return err
			} else if r != nil {
				defer r.Close()
				return remoteRm(cmd.Context(), r, recursive, force, dryRun)
			}
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			if versions {
				return runRmVersions(cmd.Context(), c, u, recursive, force, dryRun)
			}

			// Single object delete: bare key, no --recursive.
			if !u.IsPrefix && !recursive {
				if dryRun {
					fmt.Printf("would delete s3://%s/%s\n", u.Bucket, u.Key)
					return nil
				}
				res, err := transfer.DeleteKeys(cmd.Context(), c.S3, u.Bucket, []string{u.Key})
				if err != nil {
					return opErr(err)
				}
				reportDelete(res)
				return nil
			}

			// Prefix (folder) delete: L1 gates.
			if !recursive {
				return usageErr("refusing to delete prefix %q without --recursive", u.Key)
			}
			keys, err := transfer.CollectPrefixKeys(cmd.Context(), c.S3, u.Bucket, dirPrefix(u))
			if err != nil {
				return opErr(err)
			}
			if dryRun {
				for _, k := range keys {
					fmt.Printf("would delete s3://%s/%s\n", u.Bucket, k)
				}
				fmt.Printf("total: %d object(s)\n", len(keys))
				if len(keys) > rmForceThreshold && !force {
					fmt.Printf("note: deleting requires --force (> %d objects)\n", rmForceThreshold)
				}
				return nil
			}
			if len(keys) > rmForceThreshold && !force {
				return opErr(fmt.Errorf(
					"%d object(s) under s3://%s/%s — pass --force to delete them all",
					len(keys), u.Bucket, u.Key))
			}
			res, err := transfer.DeleteKeys(cmd.Context(), c.S3, u.Bucket, keys)
			if err != nil {
				return opErr(err)
			}
			reportDelete(res)
			if flagJSON {
				return printJSON(res)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&recursive, "recursive", "r", false, "delete everything under the prefix")
	f.BoolVar(&force, "force", false,
		fmt.Sprintf("allow deleting more than %d objects (L1 safety gate)", rmForceThreshold))
	f.BoolVar(&dryRun, "dry-run", false, "list what would be deleted, do nothing")
	f.BoolVar(&versions, "versions", false,
		"permanently destroy every version and delete marker too (L3: unrecoverable)")
	return cmd
}

// runRmVersions implements `rm --versions`: an exact key destroys its whole
// timeline; a prefix (with --recursive) purges every version beneath it.
func runRmVersions(ctx context.Context, c *s3client.Client, u s3URI, recursive, force, dryRun bool) error {
	if !u.IsPrefix && !recursive {
		if dryRun {
			fmt.Printf("would permanently delete every version of s3://%s/%s\n", u.Bucket, u.Key)
			return nil
		}
		res, err := versioning.DeleteAllVersions(ctx, c.S3, u.Bucket, u.Key)
		if err != nil {
			return opErr(err)
		}
		reportDelete(res)
		if flagJSON {
			return printJSON(res)
		}
		return nil
	}
	if !recursive {
		return usageErr("refusing to purge versions under %q without --recursive", u.Key)
	}
	prefix := dirPrefix(u)
	n, err := versioning.CountPurge(ctx, c.S3, u.Bucket, prefix, versioning.PurgeAll)
	if err != nil {
		return opErr(err)
	}
	if dryRun {
		if flagJSON {
			return printJSON(map[string]any{"wouldPurge": n, "bucket": u.Bucket, "prefix": prefix})
		}
		fmt.Printf("would permanently delete %d version(s)/marker(s) under s3://%s/%s\n", n, u.Bucket, prefix)
		if n > rmForceThreshold && !force {
			col.dim.Printf("note: running it requires --force (> %d)\n", rmForceThreshold)
		}
		return nil
	}
	if n > rmForceThreshold && !force {
		return opErr(fmt.Errorf(
			"would permanently delete %d version(s) — pass --force to proceed", n))
	}
	res, err := versioning.Purge(ctx, c.S3, u.Bucket, prefix, versioning.PurgeAll)
	if err != nil {
		return opErr(err)
	}
	reportDelete(res)
	if flagJSON {
		return printJSON(res)
	}
	return nil
}

func syncCmd() *cobra.Command {
	var del, dryRun bool
	cmd := &cobra.Command{
		Use:   "sync SRC DST",
		Short: "Sync a local folder with an S3 prefix (either direction)",
		Long: "One operand must be s3://bucket/prefix/, the other a local directory.\n" +
			"Uploads/downloads files whose size differs or that are missing on the target;\n" +
			"--delete also removes extra files on the target.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			srcS3 := strings.HasPrefix(args[0], "s3://")
			dstS3 := strings.HasPrefix(args[1], "s3://")
			if srcS3 == dstS3 {
				return usageErr("sync needs exactly one s3:// operand and one local directory")
			}
			var res syncResult
			if srcS3 {
				res, err = syncDownload(cmd.Context(), c, args[0], args[1], del, dryRun)
			} else {
				res, err = syncUpload(cmd.Context(), c, args[0], args[1], del, dryRun)
			}
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(res)
			}
			res.print()
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&del, "delete", false, "remove files that no longer exist at the source")
	f.BoolVar(&dryRun, "dry-run", false, "show planned actions, transfer nothing")
	return cmd
}

type syncResult struct {
	Direction string `json:"direction"` // "upload" | "download"
	Uploaded  int    `json:"transferred"`
	Deleted   int    `json:"deleted"`
	Skipped   int    `json:"skipped"`
}

func (r syncResult) print() {
	col.ok.Printf("sync (%s): %d transferred, %d deleted, %d unchanged\n",
		r.Direction, r.Uploaded, r.Deleted, r.Skipped)
}

// collectLocalFiles maps slash-relative paths to sizes under a local dir.
func collectLocalFiles(dir string) (map[string]int64, error) {
	out := map[string]int64{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = info.Size()
		return nil
	})
	return out, err
}

// collectRemoteFiles maps slash-relative keys to sizes under a prefix.
func collectRemoteFiles(ctx context.Context, c *s3client.Client, bucket, prefix string) (map[string]int64, error) {
	out := map[string]int64{}
	err := listing.Walk(ctx, c.S3, bucket, prefix, func(o s3types.Object) error {
		key := aws.ToString(o.Key)
		if strings.HasSuffix(key, "/") {
			return nil
		}
		out[strings.TrimPrefix(key, prefix)] = aws.ToInt64(o.Size)
		return nil
	})
	return out, err
}

func syncUpload(ctx context.Context, c *s3client.Client, localDir, dst string, del, dryRun bool) (syncResult, error) {
	res := syncResult{Direction: "upload"}
	u, err := parseS3URI(dst)
	if err != nil {
		return res, err
	}
	prefix := dirPrefix(u)
	local, err := collectLocalFiles(localDir)
	if err != nil {
		return res, err
	}
	remote, err := collectRemoteFiles(ctx, c, u.Bucket, prefix)
	if err != nil {
		return res, err
	}

	var uploads, deletes []string
	for rel, size := range local {
		if rs, ok := remote[rel]; ok && rs == size {
			res.Skipped++
			continue
		}
		uploads = append(uploads, rel)
	}
	if del {
		for rel := range remote {
			if _, ok := local[rel]; !ok {
				deletes = append(deletes, rel)
			}
		}
	}
	sort.Strings(uploads)
	sort.Strings(deletes)

	if dryRun {
		printSyncPlan("upload", uploads, prefix, u.Bucket, deletes)
		res.Uploaded, res.Deleted = len(uploads), len(deletes)
		return res, nil
	}
	for _, rel := range uploads {
		key := joinKeyNoSlash(prefix, rel)
		if err := transfer.UploadFile(ctx, c.S3, filepath.Join(localDir, filepath.FromSlash(rel)),
			u.Bucket, key, transfer.UploadOptions{}); err != nil {
			return res, fmt.Errorf("%s: %w", rel, err)
		}
		res.Uploaded++
	}
	if len(deletes) > 0 {
		keys := make([]string, len(deletes))
		for i, rel := range deletes {
			keys[i] = prefix + rel
		}
		dr, err := transfer.DeleteKeys(ctx, c.S3, u.Bucket, keys)
		if err != nil {
			return res, err
		}
		res.Deleted = dr.Deleted
	}
	return res, nil
}

func syncDownload(ctx context.Context, c *s3client.Client, src, localDir string, del, dryRun bool) (syncResult, error) {
	res := syncResult{Direction: "download"}
	u, err := parseS3URI(src)
	if err != nil {
		return res, err
	}
	prefix := dirPrefix(u)
	remote, err := collectRemoteFiles(ctx, c, u.Bucket, prefix)
	if err != nil {
		return res, err
	}
	local, err := collectLocalFiles(localDir)
	if err != nil {
		return res, err
	}

	var downloads, deletes []string
	for rel, size := range remote {
		if ls, ok := local[rel]; ok && ls == size {
			res.Skipped++
			continue
		}
		downloads = append(downloads, rel)
	}
	if del {
		for rel := range local {
			if _, ok := remote[rel]; !ok {
				deletes = append(deletes, rel)
			}
		}
	}
	sort.Strings(downloads)
	sort.Strings(deletes)

	if dryRun {
		printSyncPlan("download", downloads, prefix, u.Bucket, deletes)
		res.Uploaded, res.Deleted = len(downloads), len(deletes)
		return res, nil
	}
	for _, rel := range downloads {
		localPath := filepath.Join(localDir, filepath.FromSlash(rel))
		if err := transfer.DownloadFile(ctx, c.S3, u.Bucket, prefix+rel, localPath, transfer.DownloadOptions{}); err != nil {
			return res, fmt.Errorf("%s: %w", rel, err)
		}
		res.Uploaded++
	}
	for _, rel := range deletes {
		if err := os.Remove(filepath.Join(localDir, filepath.FromSlash(rel))); err != nil {
			return res, err
		}
		res.Deleted++
	}
	return res, nil
}

func printSyncPlan(direction string, transfers []string, prefix, bucket string, deletes []string) {
	for _, rel := range transfers {
		if direction == "upload" {
			fmt.Printf("would upload %s -> s3://%s/%s\n", rel, bucket, prefix+rel)
		} else {
			fmt.Printf("would download s3://%s/%s -> %s\n", bucket, prefix+rel, rel)
		}
	}
	for _, rel := range deletes {
		if direction == "upload" {
			fmt.Printf("would delete s3://%s/%s\n", bucket, prefix+rel)
		} else {
			fmt.Printf("would delete %s\n", rel)
		}
	}
}
