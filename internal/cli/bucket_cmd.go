package cli

import (
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/bucketops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
)

func mbCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mb s3://bucket",
		Short: "Make a bucket",
		Args:  cobra.ExactArgs(1),
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
			if err := bucketops.Create(cmd.Context(), c.S3, u.Bucket, region); err != nil {
				return opErr(err)
			}
			if flagJSON {
				return printJSON(map[string]any{"created": u.Bucket, "region": region})
			}
			col.ok.Printf("created bucket %s (region %s)\n", u.Bucket, region)
			return nil
		},
	}
}

func rbCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rb s3://bucket",
		Short: "Remove a bucket (must be empty, or pass --force)",
		Args:  cobra.ExactArgs(1),
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
	return cmd
}

func mkdirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mkdir s3://bucket/path/...",
		Short: "Create a folder marker (zero-byte object ending in \"/\")",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

// reportDelete prints the outcome of a batch delete (human mode).
func reportDelete(res transfer.DeleteResult) {
	if len(res.Errors) > 0 {
		for _, e := range res.Errors {
			col.errf.Printf("  error: %s\n", e)
		}
	}
	if !flagJSON {
		col.ok.Printf("deleted %d object(s)\n", res.Deleted)
	}
}
