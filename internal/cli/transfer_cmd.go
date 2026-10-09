package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/syncplan"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"
)

// rmForceThreshold is the L1 safety gate: deleting more objects than this
// under a prefix requires --force.
const rmForceThreshold = 50

// copyOptions drives cp/mv (mv = cp + source removal).
type copyOptions struct {
	Recursive    bool
	StorageClass string
	SSE          string // "AES256" (M1: SSE-S3)
	NoClobber    bool
	DryRun       bool
	Move         bool
	Versions     bool // recreate the source's version timeline (S3→S3)
	Force        bool // mv --versions: allow purging >50 source versions
}

func (o copyOptions) uploadOptions(progress transfer.ProgressFn) transfer.UploadOptions {
	return transfer.UploadOptions{
		StorageClass: o.StorageClass,
		SSE:          o.SSE,
		NoClobber:    o.NoClobber,
		Progress:     progress,
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
			"plus NAME:// source URIs on either side — any saved data source, S3 sources too:\n" +
			"NAME://bucket/key for account-wide S3 sources, NAME://key for per-bucket ones.\n" +
			"Both sides on the same S3 source copy server-side; anything else streams.\n" +
			"--versions (S3→S3) recreates the source's version timeline at the destination,\n" +
			"delete markers included; mv --versions then purges the sources (L3, --force gates).\n" +
			"SRC or DST being a directory/prefix (or --recursive) copies everything beneath it.\n" +
			"A destination inside the source's own subtree is refused before a byte moves —\n" +
			"a move beneath itself would delete the fresh copies with the originals.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeTransferURIs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The S3 client is only needed when one side is s3:// — source-
			// URI operands dial their own engines (S3 sources included).
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
	f.BoolVar(&opts.Versions, "versions", false,
		"recreate the full version timeline (both sides must be S3; destination must be versioned)")
	f.BoolVar(&opts.Force, "force", false,
		fmt.Sprintf("with mv --versions: allow purging more than %d source versions", rmForceThreshold))
	return cmd
}

// runCopy dispatches on direction and returns the item count. Source URIs
// (NAME:// — any saved source, M10.5) take the remotefs pipeline; S3
// sources dial their own client (cross-cloud streaming — or server-side
// when both sides are the same source); everything else keeps its
// historical path.
func runCopy(ctx context.Context, c *s3client.Client, src, dst string, opts copyOptions) (int, error) {
	srcS3Ref, err := dialS3SourceURI(ctx, src)
	if err != nil {
		return 0, err
	}
	dstS3Ref, err := dialS3SourceURI(ctx, dst)
	if err != nil {
		return 0, err
	}
	// One operand is one kind of source URI — skip the remotefs dialer
	// when the S3 dialer already claimed it.
	var srcRef, dstRef *remoteRef
	if srcS3Ref == nil {
		srcRef, err = dialSourceURI(ctx, src)
		if err != nil {
			return 0, err
		}
	}
	if dstS3Ref == nil {
		dstRef, err = dialSourceURI(ctx, dst)
		if err != nil {
			srcRef.Close()
			return 0, err
		}
	}
	defer srcRef.Close()
	defer dstRef.Close()
	if opts.Versions {
		return runCopyVersions(ctx, c, src, dst, srcS3Ref, dstS3Ref, opts)
	}
	if srcS3Ref != nil && dstS3Ref != nil && srcS3Ref.src.ID == dstS3Ref.src.ID {
		// Both sides are the same S3 source (one client): plain
		// server-side copy under synthesized URIs — mv's deletes and the
		// exact-object gates included.
		return copyS3ToS3(ctx, srcS3Ref.c, srcS3Ref.uriStr(), dstS3Ref.uriStr(), opts)
	}
	if srcRef != nil || dstRef != nil || srcS3Ref != nil || dstS3Ref != nil {
		return copyRemoteDispatch(ctx, c, src, dst, srcRef, dstRef, srcS3Ref, dstS3Ref, opts)
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

	// ok=false means "skipped under --no-clobber": nothing was uploaded,
	// so a move must NOT remove the local source.
	uploadOne := func(local, key string) (ok bool, err error) {
		if opts.DryRun {
			fmt.Printf("upload %s -> s3://%s/%s\n", local, u.Bucket, key)
			return true, nil
		}
		if opts.NoClobber {
			exists, perr := transfer.ObjectExists(ctx, c.S3, u.Bucket, key)
			if perr != nil {
				return false, fmt.Errorf("%s: could not check whether s3://%s/%s exists: %w", local, u.Bucket, key, perr)
			}
			if exists {
				if !flagJSON {
					rprintf("skip s3://%s/%s — already exists\n", u.Bucket, key)
				}
				return false, nil
			}
		}
		pr := newProgressLine(local)
		err = transfer.UploadFile(ctx, c.S3, local, u.Bucket, key, opts.uploadOptions(pr.fn()))
		pr.done()
		if err != nil {
			return false, fmt.Errorf("%s: %w", local, err)
		}
		if opts.Move {
			if err := os.Remove(local); err != nil {
				return false, err
			}
		}
		if flagVerbose {
			col.dim.Printf("up s3://%s/%s\n", u.Bucket, key)
		}
		return true, nil
	}

	if !st.IsDir() {
		key := u.Key
		if u.IsPrefix || !u.HasPrefix { // DST is a folder or bucket root: keep filename
			key = joinKeyNoSlash(u.Key, filepath.Base(localPath))
		}
		ok, err := uploadOne(localPath, key)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, nil
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
		ok, err := uploadOne(p, joinKeyNoSlash(u.Key, rel))
		if err != nil {
			return err
		}
		if ok {
			count++
		}
		return nil
	})
	if err != nil {
		return count, err
	}
	return count, nil
}

func downloadPath(ctx context.Context, c *s3client.Client, src, dst string, opts copyOptions) (int, error) {
	u, err := parseS3URI(src)
	if err != nil {
		return 0, err
	}

	// ok=false means "skipped under --no-clobber": nothing was written, so
	// a move must NOT delete the source object of a skipped download.
	downloadOne := func(key, local string) (ok bool, err error) {
		if opts.DryRun {
			fmt.Printf("download s3://%s/%s -> %s\n", u.Bucket, key, local)
			return true, nil
		}
		// --no-clobber: never overwrite an existing local file (the
		// download would truncate it silently — local disks have no
		// version history to recover from)
		if opts.NoClobber {
			if _, err := os.Stat(local); err == nil {
				if !flagJSON {
					rprintf("skip %s — already exists\n", local)
				}
				return false, nil
			}
		}
		pr := newProgressLine(local)
		err = transfer.DownloadFile(ctx, c.S3, u.Bucket, key, local, transfer.DownloadOptions{Progress: pr.fn()})
		pr.done()
		if err != nil {
			return false, fmt.Errorf("%s: %w", key, err)
		}
		if flagVerbose {
			col.dim.Printf("down %s\n", local)
		}
		return true, nil
	}

	if u.IsPrefix || opts.Recursive {
		prefix := dirPrefix(u)
		count := 0
		var keys []string // only really-downloaded keys: mv may remove these
		err = listing.Walk(ctx, c.S3, u.Bucket, prefix, func(o s3types.Object) error {
			key := aws.ToString(o.Key)
			if strings.HasSuffix(key, "/") {
				return nil // folder markers
			}
			rel := strings.TrimPrefix(key, prefix)
			ok, err := downloadOne(key, transfer.SafeLocalJoin(dst, rel))
			if err != nil {
				return err
			}
			if ok {
				count++
				keys = append(keys, key)
			}
			return nil
		})
		if err != nil {
			return count, err
		}
		if opts.Move && !opts.DryRun && len(keys) > 0 {
			res, err := transfer.DeleteKeys(ctx, c.S3, u.Bucket, keys)
			if rerr := reportDelete(res); rerr != nil {
				return count, rerr
			}
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
	ok, err := downloadOne(u.Key, local)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil // skipped under --no-clobber: nothing moved
	}
	if opts.Move && !opts.DryRun {
		res, err := transfer.DeleteKeys(ctx, c.S3, u.Bucket, []string{u.Key})
		if rerr := reportDelete(res); rerr != nil {
			return 1, rerr
		}
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

	// copied=false means "skipped under --no-clobber": nothing was
	// written, so a move must NOT delete the source either.
	copyOne := func(srcKey, dstKey string) (copied bool, err error) {
		if opts.DryRun {
			fmt.Printf("copy s3://%s/%s -> s3://%s/%s\n", su.Bucket, srcKey, du.Bucket, dstKey)
			return true, nil
		}
		if opts.NoClobber {
			exists, perr := transfer.ObjectExists(ctx, c.S3, du.Bucket, dstKey)
			if perr != nil {
				return false, fmt.Errorf("%s: could not check whether s3://%s/%s exists: %w", srcKey, du.Bucket, dstKey, perr)
			}
			if exists {
				if !flagJSON {
					rprintf("skip s3://%s/%s — already exists\n", du.Bucket, dstKey)
				}
				return false, nil
			}
		}
		if err := transfer.Copy(ctx, c.S3, su.Bucket, srcKey, du.Bucket, dstKey); err != nil {
			return false, fmt.Errorf("%s: %w", srcKey, err)
		}
		if flagVerbose {
			col.dim.Printf("copy s3://%s/%s\n", du.Bucket, dstKey)
		}
		return true, nil
	}

	if su.IsPrefix || opts.Recursive {
		prefix := dirPrefix(su)
		// Cycle guard: within one bucket, a destination inside the source
		// (or the source itself) copies the tree beneath itself and a move
		// then deletes the sources — fresh copies included. Refused before
		// anything transfers.
		if su.Bucket == du.Bucket && (dirPrefix(du) == prefix || strings.HasPrefix(dirPrefix(du), prefix)) {
			return 0, usageErr("destination s3://%s/%s is inside the source — a folder cannot be copied or moved into itself",
				du.Bucket, du.Key)
		}
		count := 0
		var keys []string
		err = listing.Walk(ctx, c.S3, su.Bucket, prefix, func(o s3types.Object) error {
			key := aws.ToString(o.Key)
			if strings.HasSuffix(key, "/") {
				return nil
			}
			copied, err := copyOne(key, joinKeyNoSlash(du.Key, strings.TrimPrefix(key, prefix)))
			if err != nil {
				return err
			}
			if copied { // moves delete only what actually landed
				count++
				keys = append(keys, key)
			}
			return nil
		})
		if err != nil {
			return count, err
		}
		if opts.Move && !opts.DryRun && len(keys) > 0 {
			res, err := transfer.DeleteKeys(ctx, c.S3, su.Bucket, keys)
			if rerr := reportDelete(res); rerr != nil {
				return count, rerr
			}
			if err != nil {
				return count, err
			}
		}
		return count, nil
	}

	dstKey := du.Key
	if du.IsPrefix || !du.HasPrefix {
		dstKey = joinKeyNoSlash(du.Key, path.Base(su.Key))
	}
	// The same-key refusal on the COMPUTED destination: the file onto its
	// own folder (s3://b/docs/f.txt → s3://b/docs/) joins back to the same
	// key — a move would copy it onto itself and then delete it.
	if su.Bucket == du.Bucket && su.Key == dstKey {
		return 0, usageErr("source and destination are the same object")
	}
	copied, err := copyOne(su.Key, dstKey)
	if err != nil {
		return 0, err
	}
	if !copied {
		return 0, nil
	}
	if opts.Move && !opts.DryRun {
		res, err := transfer.DeleteKeys(ctx, c.S3, su.Bucket, []string{su.Key})
		if rerr := reportDelete(res); rerr != nil {
			return 1, rerr
		}
		if err != nil {
			return 1, err
		}
	}
	return 1, nil
}

func rmCmd() *cobra.Command {
	var recursive, force, dryRun, versions bool
	cmd := &cobra.Command{
		Use:               "rm s3://bucket[/prefix] | NAME://path",
		Short:             "Delete objects or source files (folders need --recursive; large batches --force)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSourceURIs,
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
				return reportDelete(res)
			}

			// Prefix (folder) delete: L1 gates.
			if !recursive {
				return usageErr("refusing to delete prefix %q without --recursive", u.Key)
			}
			keys, err := transfer.CollectPrefixKeys(cmd.Context(), c.S3, u.Bucket, dirPrefix(u))
			if err != nil {
				return opErr(err)
			}
			// Stores disagree whether the folder marker itself is listed
			// under its own prefix (AWS: yes, MinIO: no) — a delete that
			// misses it leaves a ghost folder row behind. Delete-only:
			// never fold markers into copies or conversions.
			keys = transfer.IncludeFolderMarker(keys, dirPrefix(u))
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
			rerr := reportDelete(res)
			if flagJSON {
				return printJSON(res)
			}
			return rerr
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&recursive, "recursive", "r", false, "delete everything under the prefix")
	f.BoolVar(&force, "force", false,
		fmt.Sprintf("allow deleting more than %d objects, or destroying a >%d-version timeline with --versions (L1 safety gate)",
			rmForceThreshold, rmForceThreshold))
	f.BoolVar(&dryRun, "dry-run", false, "list what would be deleted, do nothing")
	f.BoolVar(&versions, "versions", false,
		"permanently destroy every version and delete marker too (L3: unrecoverable)")
	return cmd
}

// runRmVersions implements `rm --versions`: an exact key destroys its whole
// timeline; a prefix (with --recursive) purges every version beneath it.
// Both are L3 and both count first — an exact key with a deep timeline hits
// the same L1 threshold gate as a prefix purge.
func runRmVersions(ctx context.Context, c *s3client.Client, u s3URI, recursive, force, dryRun bool) error {
	if !u.IsPrefix && !recursive {
		vers, err := versioning.ListForObject(ctx, c.S3, u.Bucket, u.Key)
		if err != nil {
			return opErr(err)
		}
		n := len(vers)
		if dryRun {
			if flagJSON {
				return printJSON(map[string]any{"wouldDestroy": n, "bucket": u.Bucket, "key": u.Key})
			}
			fmt.Printf("would permanently delete every version of s3://%s/%s (%d version(s)/marker(s))\n", u.Bucket, u.Key, n)
			if n > rmForceThreshold && !force {
				col.dim.Printf("note: running it requires --force (> %d)\n", rmForceThreshold)
			}
			return nil
		}
		if n > rmForceThreshold && !force {
			return opErr(fmt.Errorf(
				"would permanently delete %d version(s) of s3://%s/%s — pass --force to proceed", n, u.Bucket, u.Key))
		}
		res, err := versioning.DeleteAllVersions(ctx, c.S3, u.Bucket, u.Key)
		if err != nil {
			return opErr(err)
		}
		rerr := reportDelete(res)
		if flagJSON {
			return printJSON(res)
		}
		return rerr
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
	rerr := reportDelete(res)
	if flagJSON {
		return printJSON(res)
	}
	return rerr
}

func syncCmd() *cobra.Command {
	var del, dryRun, force, watch bool
	var watchEvery time.Duration
	cmd := &cobra.Command{
		Use:   "sync SRC DST",
		Short: "Sync any two locations (local, S3, or a NAME:// source path)",
		Long: "Each operand is a local directory, an s3://bucket/prefix/ URI, or a\n" +
			"NAME:// path into any saved data source — the same operand grammar cp\n" +
			"speaks. Copies files whose size differs or that are missing on the target;\n" +
			"--delete also removes extra files on the target (--force required above the\n" +
			fmt.Sprintf("safety threshold of %d files).", rmForceThreshold) + "\n" +
			"--watch re-runs the sync until Ctrl+C (default every 30s, --interval to\n" +
			"change) — quiet on passes that move nothing.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeTransferURIs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The view client is only needed when an operand is s3:// —
			// NAME:// operands dial their own engines (S3 sources too).
			var c *s3client.Client
			if strings.HasPrefix(args[0], "s3://") || strings.HasPrefix(args[1], "s3://") {
				var err error
				c, err = resolveClient(cmd.Context())
				if err != nil {
					return err
				}
			}
			if watch && flagJSON {
				return usageErr("--watch is interactive; drop it (or --json)")
			}
			if watch {
				if dryRun {
					return usageErr("--watch keeps syncing — drop --dry-run (it plans once, it does not watch)")
				}
				return watchSync(cmd.Context(), c, args, watchEvery, del, force)
			}
			x, err := syncDialSide(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			y, err := syncDialSide(cmd.Context(), c, args[1])
			if err != nil {
				x.Close()
				return err
			}
			defer y.Close()
			defer x.Close()
			if sameSyncLocation(x, y) {
				return usageErr("sync needs two different locations — both operands name %s", x.label)
			}
			res, err := syncRun(cmd.Context(), x, y, del, dryRun, force)
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
	f.BoolVar(&watch, "watch", false, "keep re-syncing until Ctrl+C")
	f.DurationVar(&watchEvery, "interval", 30*time.Second, "poll interval for --watch")
	f.BoolVar(&del, "delete", false, "remove files that no longer exist at the source")
	f.BoolVar(&dryRun, "dry-run", false, "show planned actions, transfer nothing")
	f.BoolVar(&force, "force", false,
		fmt.Sprintf("with --delete: allow removing more than %d files (L1 safety gate)", rmForceThreshold))
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

// ---- the sync matrix ----

// syncSide is one dialed sync operand: a local directory, an S3 prefix
// (the view client or a saved S3 source's own), or a directory on any
// other saved source — the same operand grammar cp and mv speak. The
// side knows how to census itself (rel → size), stream one relative file
// off itself, name one relative file for the plan output, and delete one
// relative file at itself (the S3 delete batches — see syncDeleteAt).
type syncSide struct {
	label  string // display form for plans, gates and results
	srcID  string // source identity ("" = the view client); same-location and server-side-copy detection
	local  bool
	dir    string           // local: OS path
	ref    *remoteRef       // remote engine side (holds the open FS)
	client *s3client.Client // s3 side
	bucket string           // s3 side
	prefix string           // s3 side, dirPrefix form
}

func (s *syncSide) isS3() bool { return s.client != nil }

// Close releases the engine side's open filesystem (no-op elsewhere).
func (s *syncSide) Close() {
	if s.ref != nil {
		s.ref.Close()
	}
}

// keyFor rebuilds the object key of one relative file (root prefix stays
// empty, never a lone slash).
func (s *syncSide) keyFor(rel string) string { return joinKeyNoSlash(s.prefix, rel) }

// remotePath anchors one relative file under the engine side.
func (s *syncSide) remotePath(rel string) string {
	return remotefs.CleanPath(s.ref.path + "/" + rel)
}

// locate names one relative file at the side for the plan output.
func (s *syncSide) locate(rel string) string {
	switch {
	case s.isS3():
		return "s3://" + s.bucket + "/" + s.keyFor(rel)
	case s.ref != nil:
		return uri(s.ref.src.Name, s.remotePath(rel))
	default:
		return filepath.Join(s.dir, filepath.FromSlash(rel))
	}
}

// walkCensus maps slash-relative paths to sizes under the side.
func (s *syncSide) walkCensus(ctx context.Context) (map[string]int64, error) {
	switch {
	case s.local:
		return collectLocalFiles(s.dir)
	case s.ref != nil:
		return collectEngineFiles(ctx, s.ref)
	default:
		return collectRemoteFiles(ctx, s.client, s.bucket, s.prefix)
	}
}

// open streams one relative file off the side with its size (engines may
// report unknown as zero — the caller falls back to the census size).
func (s *syncSide) open(ctx context.Context, rel string) (io.ReadCloser, int64, error) {
	switch {
	case s.local:
		f, err := os.Open(filepath.Join(s.dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, 0, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, 0, err
		}
		return f, st.Size(), nil
	case s.ref != nil:
		return s.ref.fs.Open(ctx, s.remotePath(rel))
	default:
		return s3OpenCLI(ctx, s.client, s.bucket, s.keyFor(rel))
	}
}

// syncDialSide resolves one operand: NAME:// first (S3 sources dial
// their own client, every other engine its filesystem), then plain
// s3:// on the view client, then an existing local directory.
func syncDialSide(ctx context.Context, c *s3client.Client, arg string) (*syncSide, error) {
	if s3r, err := dialS3SourceURI(ctx, arg); err != nil {
		return nil, err
	} else if s3r != nil {
		prefix := ""
		if k := strings.TrimSuffix(s3r.key, "/"); k != "" {
			prefix = k + "/"
		}
		return &syncSide{label: s3r.uriStr(), srcID: s3r.src.ID, client: s3r.c, bucket: s3r.bucket, prefix: prefix}, nil
	}
	if ref, err := dialSourceURI(ctx, arg); err != nil {
		return nil, err
	} else if ref != nil {
		return &syncSide{label: uri(ref.src.Name, ref.path), srcID: ref.src.ID, ref: ref}, nil
	}
	if strings.HasPrefix(arg, "s3://") {
		u, err := parseS3URI(arg)
		if err != nil {
			return nil, err
		}
		return &syncSide{label: arg, client: c, bucket: u.Bucket, prefix: dirPrefix(u)}, nil
	}
	st, err := os.Stat(arg)
	if err != nil || !st.IsDir() {
		return nil, usageErr("%s is neither a local directory, an s3:// URI nor a NAME:// source path", arg)
	}
	return &syncSide{label: arg, dir: arg, local: true}, nil
}

// sameSyncLocation reports whether both sides name one location — the
// degenerate pair a two-way tool refuses honestly.
func sameSyncLocation(x, y *syncSide) bool {
	switch {
	case x.local && y.local:
		return filepath.Clean(x.dir) == filepath.Clean(y.dir)
	case x.isS3() && y.isS3():
		return x.srcID == y.srcID && x.bucket == y.bucket && x.prefix == y.prefix
	case x.ref != nil && y.ref != nil:
		return x.srcID == y.srcID && remotefs.CleanPath(x.ref.path) == remotefs.CleanPath(y.ref.path)
	}
	return false
}

// syncDirection keeps the historical labels for the local↔s3 pair and
// names the pair for everything else the matrix seats.
func syncDirection(x, y *syncSide) string {
	if x.local && y.isS3() {
		return "upload"
	}
	if x.isS3() && y.local {
		return "download"
	}
	return x.label + " → " + y.label
}

// syncRun walks both sides, plans through the one shared predicate (the
// GUI's Synchronize dialog computes with the same core, so the semantics
// can never drift between faces) and executes: copies one direction,
// then the opt-in deletes at the target under the same L1 gate as rm.
func syncRun(ctx context.Context, x, y *syncSide, del, dryRun, force bool) (syncResult, error) {
	res := syncResult{Direction: syncDirection(x, y)}
	xm, err := x.walkCensus(ctx)
	if err != nil {
		return res, fmt.Errorf("%s: %w", x.label, err)
	}
	ym, err := y.walkCensus(ctx)
	if err != nil {
		return res, fmt.Errorf("%s: %w", y.label, err)
	}
	copies, deletes, skipped := syncplan.Plan(xm, ym, del)
	res.Skipped = skipped

	if dryRun {
		printSyncPlan(y, copies, deletes, xm)
		res.Uploaded, res.Deleted = len(copies), len(deletes)
		syncGateNote(len(deletes), force)
		return res, nil
	}
	// The --delete half is gated exactly like rm: refuse above the L1
	// threshold without --force, before any copy runs (fail fast — a
	// scripted sync must not half-apply a plan it will then refuse).
	if len(deletes) > rmForceThreshold && !force {
		return res, fmt.Errorf("sync --delete would remove %d file(s) at %s — pass --force to proceed",
			len(deletes), y.label)
	}
	for _, rel := range copies {
		if err := syncCopyOne(ctx, x, y, rel, xm[rel]); err != nil {
			return res, fmt.Errorf("%s: %w", rel, err)
		}
		res.Uploaded++
	}
	n, err := syncDeleteAt(ctx, y, deletes)
	res.Deleted = n
	return res, err
}

// watchSync re-runs the pair until Ctrl+C (or the context ends): every
// pass dials both sides fresh — an engine's idle wire may have died
// between passes — and runs the one sync law. A pass that moves nothing
// prints nothing; transient errors after the first pass are printed, not
// fatal (the next tick retries); the --delete gate refuses in place,
// exactly the one-shot voice.
func watchSync(ctx context.Context, c *s3client.Client, args []string, every time.Duration, del, force bool) error {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	tick := time.NewTicker(every)
	defer tick.Stop()
	fmt.Fprintln(os.Stderr, "syncing "+args[0]+" → "+args[1]+" every "+every.String()+" — Ctrl+C to stop")
	first := true
	pass := func() error {
		x, err := syncDialSide(ctx, c, args[0])
		if err != nil {
			return err
		}
		y, err := syncDialSide(ctx, c, args[1])
		if err != nil {
			x.Close()
			return err
		}
		defer y.Close()
		defer x.Close()
		if sameSyncLocation(x, y) {
			return usageErr("sync needs two different locations — both operands name %s", x.label)
		}
		res, err := syncRun(ctx, x, y, del, false, force)
		if err != nil {
			return err
		}
		if first || res.Uploaded > 0 || res.Deleted > 0 {
			res.print()
		}
		first = false
		return nil
	}
	if err := pass(); err != nil {
		return opErr(err)
	}
	for {
		select {
		case <-stop:
			return nil
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if err := pass(); err != nil {
				fmt.Fprintln(os.Stderr, "sync watch error:", err)
			}
		}
	}
}

// syncCopyOne lands one planned file x → y. A same-source S3 pair copies
// server-side (no bytes on the wire); every other pairing streams through
// the cp machinery's own primitives — the integrity wrap on positive
// sizes, staged local writes, and the temp spool for single-connection
// engines copying onto themselves.
func syncCopyOne(ctx context.Context, x, y *syncSide, rel string, size int64) error {
	if x.isS3() && y.isS3() && x.srcID == y.srcID {
		pr := newProgressLine(rel)
		err := transfer.Copy(ctx, x.client.S3, x.bucket, x.keyFor(rel), y.bucket, y.keyFor(rel))
		pr.done()
		return err
	}
	rc, sz, err := x.open(ctx, rel)
	if err != nil {
		return err
	}
	defer rc.Close()
	if sz <= 0 {
		sz = size // engines report unknown as zero; the census knew
	}
	pr := newProgressLine(rel)
	defer pr.done()
	switch {
	case y.local:
		dst := transfer.SafeLocalJoin(y.dir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if sz > 0 {
			rc = transfer.VerifiedStream(rc, sz)
		}
		// Staged and committed, never truncated in place (StageAndCommit
		// syncs and renames only on success) — the cp local leg's twin.
		return transfer.StageAndCommit(dst, func(f *os.File) error {
			_, err := io.Copy(f, rc)
			return err
		})
	case y.ref != nil:
		if x.ref != nil && x.srcID == y.srcID {
			return copyViaTemp(ctx, rc, y.ref, y.remotePath(rel))
		}
		return writeRemoteFile(ctx, y.ref, y.remotePath(rel), rc)
	default:
		if sz > 0 {
			rc = transfer.VerifiedStream(rc, sz)
		}
		return transfer.UploadReader(ctx, y.client.S3, rc, sz, y.bucket, y.keyFor(rel),
			transfer.UploadOptions{Progress: pr.fn()})
	}
}

// syncDeleteAt removes the plan's target-side extras. S3 batches through
// DeleteKeys; local and engine sides delete one anchored file at a time.
func syncDeleteAt(ctx context.Context, y *syncSide, deletes []string) (int, error) {
	if y.isS3() {
		keys := make([]string, len(deletes))
		for i, rel := range deletes {
			keys[i] = y.keyFor(rel)
		}
		dr, err := transfer.DeleteKeys(ctx, y.client.S3, y.bucket, keys)
		return dr.Deleted, err
	}
	n := 0
	for _, rel := range deletes {
		var err error
		if y.local {
			err = os.Remove(filepath.Join(y.dir, filepath.FromSlash(rel)))
		} else {
			err = y.ref.fs.Remove(ctx, y.remotePath(rel))
		}
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// syncGateNote is the --dry-run half of the sync --delete L1 gate: it names
// the threshold a real run would enforce, without deleting anything.
func syncGateNote(deletes int, force bool) {
	if deletes > rmForceThreshold && !force {
		col.dim.Printf("note: deleting them requires --force (> %d files)\n", rmForceThreshold)
	}
}

// printSyncPlan is the --dry-run face: cp's own dry-run grammar ("copy
// -> <destination> (size)", "delete <location>") over the planned
// vectors, side-agnostic.
func printSyncPlan(y *syncSide, copies, deletes []string, sizes map[string]int64) {
	for _, rel := range copies {
		rprintf("copy -> %s (%s)\n", y.locate(rel), humanSize(sizes[rel]))
	}
	for _, rel := range deletes {
		rprintf("delete %s\n", y.locate(rel))
	}
}

// collectEngineFiles maps slash-relative paths to sizes under a remote
// source directory — the compare walker's own grammar.
func collectEngineFiles(ctx context.Context, r *remoteRef) (map[string]int64, error) {
	out := map[string]int64{}
	root := r.path
	prefix := strings.TrimSuffix(root, "/") + "/"
	err := remotefs.Walk(ctx, r.fs, root, func(e listing.Entry) error {
		if e.IsDir {
			return nil
		}
		key := strings.TrimSuffix(e.Key, "/")
		rel := strings.TrimPrefix(key, prefix)
		if root == "/" {
			rel = strings.TrimPrefix(key, "/")
		}
		if rel == "" {
			return nil
		}
		out[rel] = e.Size
		return nil
	})
	return out, err
}
