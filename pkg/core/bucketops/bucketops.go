// Package bucketops implements bucket lifecycle operations (create, head,
// empty, delete) with the M1 safety model: destructive operations require an
// explicit force flag and report counts before acting (PLAN.md §9).
package bucketops

import (
	"context"
	"errors"
	"fmt"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Create makes a bucket. An empty region means the client default.
func Create(ctx context.Context, client *s3.Client, bucket, region string) error {
	input := &s3.CreateBucketInput{Bucket: aws.String(bucket)}
	if region != "" && region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}
	_, err := client.CreateBucket(ctx, input)
	return err
}

// Head fetches bucket metadata (region).
func Head(ctx context.Context, client *s3.Client, bucket string) (*s3.GetBucketLocationOutput, error) {
	return client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: aws.String(bucket)})
}

// EmptyStats describes what EmptyBucket would delete (count-then-act, §9).
type EmptyStats struct {
	Bucket      string `json:"bucket"`
	ObjectCount int    `json:"objectCount"`
}

// CountObjects returns how many objects live under a bucket (all prefixes).
func CountObjects(ctx context.Context, client *s3.Client, bucket string) (int, error) {
	keys, err := transfer.CollectPrefixKeys(ctx, client, bucket, "")
	if err != nil {
		return 0, err
	}
	return len(keys), nil
}

// EmptyBucket deletes every object in the bucket. force must be true; the
// caller is responsible for the L2 confirmation dialogue (typed bucket name
// in the GUI, --force on the CLI).
func EmptyBucket(ctx context.Context, client *s3.Client, bucket string, force bool) (transfer.DeleteResult, error) {
	if !force {
		return transfer.DeleteResult{}, errors.New("refusing to empty bucket without force confirmation")
	}
	keys, err := transfer.CollectPrefixKeys(ctx, client, bucket, "")
	if err != nil {
		return transfer.DeleteResult{}, fmt.Errorf("listing bucket contents: %w", err)
	}
	return transfer.DeleteKeys(ctx, client, bucket, keys)
}

// DeleteBucket removes a bucket. S3 requires it to be empty; with force set,
// it is emptied first (L2). Returns the deletion stats for reporting.
func DeleteBucket(ctx context.Context, client *s3.Client, bucket string, force bool) (transfer.DeleteResult, error) {
	count, err := CountObjects(ctx, client, bucket)
	if err != nil {
		// Distinguish "not empty vs not readable": if we cannot list, let the
		// DeleteBucket call surface the real error.
		_, derr := client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
		return transfer.DeleteResult{}, derr
	}
	var res transfer.DeleteResult
	if count > 0 {
		if !force {
			return res, fmt.Errorf(
				"bucket %s contains %d object(s): empty it first or pass --force", bucket, count)
		}
		res, err = EmptyBucket(ctx, client, bucket, true)
		if err != nil {
			return res, err
		}
	}
	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	return res, err
}
