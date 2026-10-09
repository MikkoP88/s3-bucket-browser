package cli

import (
	"fmt"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/bucketops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/versioning"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"
)

func mbCmd() *cobra.Command {
	var objectLock bool
	cmd := &cobra.Command{
		Use:               "mb s3://bucket",
		Short:             "Make a bucket",
		Long:              "Creates a bucket. --object-lock enables object lock at creation (the only moment it can be turned on; versioning comes with it).",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeS3URIs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			if u.HasPrefix {
				return usageErr("mb takes a bucket, not a key: s3://%s", u.Bucket)
			}
			region := first(flagRegion, c.Region)
			if err := bucketops.Create(cmd.Context(), c.S3, u.Bucket, region, objectLock); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{"created": u.Bucket, "region": region, "objectLock": objectLock})
			}
			col.ok.Printf("created bucket %s (region %s)\n", u.Bucket, region)
			if objectLock {
				col.ok.Printf("object lock enabled (permanent; versioning on)\n")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&objectLock, "object-lock", false, "enable object lock at creation (irreversible)")
	return cmd
}

func rbCmd() *cobra.Command {
	var force, dryRun bool
	cmd := &cobra.Command{
		Use:               "rb s3://bucket",
		Short:             "Remove a bucket (must be empty, or pass --force)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeS3URIs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			if u.HasPrefix {
				return usageErr("rb takes a bucket, not a key: s3://%s", u.Bucket)
			}
			// --dry-run is the count-then-act preview every other
			// destructive command has: what a forced removal would delete,
			// walked and totaled, before anything happens. The preview
			// mirrors the removal path itself (DeleteBucket's own branch):
			// a bucket with configured versioning and any history loses
			// the whole history — versions and delete markers both, their
			// bytes summed — while a plain one loses only the live objects.
			if dryRun {
				if vs, verr := versioning.Status(cmd.Context(), c.S3, u.Bucket); verr == nil && vs != "" {
					var versions, markers, vbytes int64
					werr := versioning.WalkVersions(cmd.Context(), c.S3, u.Bucket, "", func(v versioning.Version) error {
						if v.IsDeleteMarker {
							markers++
						} else {
							versions++
							vbytes += v.Size
						}
						return nil
					})
					if werr != nil {
						return opErr(werr)
					}
					if versions+markers > 0 {
						if flagJSON {
							return printJSON(map[string]any{"dryRun": true, "bucket": u.Bucket,
								"versions": versions, "markers": markers, "bytes": vbytes})
						}
						col.warn.Printf("would delete %d object version(s) (%d delete marker(s), %s) and remove bucket s3://%s\n",
							versions, markers, humanSize(vbytes), u.Bucket)
						if !force {
							rprintf("bucket is not empty — removal would need --force\n")
						}
						return nil
					}
					// configured but empty history: deletes like a plain one
				}
				var objects, bytes int64
				err := listing.Walk(cmd.Context(), c.S3, u.Bucket, "", func(o s3types.Object) error {
					if strings.HasSuffix(aws.ToString(o.Key), "/") {
						return nil // folder markers are not content
					}
					objects++
					bytes += aws.ToInt64(o.Size)
					return nil
				})
				if err != nil {
					return opErr(err)
				}
				if flagJSON {
					return printJSON(map[string]any{"dryRun": true, "bucket": u.Bucket, "objects": objects, "bytes": bytes})
				}
				col.warn.Printf("would delete %d object(s) (%s) and remove bucket s3://%s\n", objects, humanSize(bytes), u.Bucket)
				if objects > 0 && !force {
					rprintf("bucket is not empty — removal would need --force\n")
				}
				return nil
			}
			res, err := bucketops.DeleteBucket(cmd.Context(), c.S3, u.Bucket, force)
			if err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{
					"removed": u.Bucket, "force": force, "objectsDeleted": res.Deleted,
				})
			}
			if force && res.Deleted > 0 {
				col.warn.Printf("emptied %d object(s)\n", res.Deleted)
			}
			col.ok.Printf("removed bucket %s\n", u.Bucket)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "empty the bucket before removing it (L2 destructive)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what a forced removal would delete, do nothing")
	return cmd
}

func mkdirCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "mkdir s3://bucket/path/... | NAME://dir",
		Short:             "Create a folder marker (S3) or a real folder (source URIs)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSourceURIs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if r, err := dialSourceURI(cmd.Context(), args[0]); err != nil {
				return err
			} else if r != nil {
				defer r.Close()
				return remoteMkdir(cmd.Context(), r)
			}
			c, err := resolveClient(cmd.Context())
			if err != nil {
				return err
			}
			u, err := parseS3URI(args[0])
			if err != nil {
				return err
			}
			if !u.HasPrefix {
				return usageErr("mkdir needs a folder path (s3://bucket/folder/); trailing slash optional")
			}
			key := transfer.JoinKey(u.Key) // normalized to trailing-slash form
			if _, err := c.S3.PutObject(cmd.Context(), &s3.PutObjectInput{
				Bucket: aws.String(u.Bucket),
				Key:    aws.String(key),
				Body:   strings.NewReader(""),
			}); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{"created": "s3://" + u.Bucket + "/" + key})
			}
			col.ok.Printf("created folder s3://%s/%s\n", u.Bucket, key)
			return nil
		},
	}
}

// reportDelete prints the outcome of a batch delete (human mode) and
// reports partial failure: a delete that left survivors must never exit
// 0 — a scripted purge would read success and act on it.
func reportDelete(res transfer.DeleteResult) error {
	if len(res.Errors) > 0 {
		for _, e := range res.Errors {
			col.errf.Printf("  error: %s\n", e)
		}
	}
	if !flagJSON {
		col.ok.Printf("deleted %d object(s)\n", res.Deleted)
	}
	if len(res.Errors) > 0 {
		return exitError{exitOpFail, fmt.Errorf("%d deletion(s) failed (errors above)", len(res.Errors))}
	}
	return nil
}
