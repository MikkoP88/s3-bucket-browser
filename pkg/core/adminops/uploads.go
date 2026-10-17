package adminops

import (
	"context"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ---------------- incomplete multipart uploads ----------------

// IncompleteUpload is one in-progress multipart upload the provider is
// holding: bytes already streamed as parts stay billed (and on most
// providers invisible to listings) until the upload is completed or
// aborted. A transfer that dies mid-flight — process kill, cancel, link
// failure — leaves exactly one of these behind, so the operational face
// (see them, abort them) belongs beside the lifecycle policy that expires
// them.
type IncompleteUpload struct {
	Key          string    `json:"key"`
	UploadID     string    `json:"uploadId"`
	Initiated    time.Time `json:"initiated"`
	StorageClass string    `json:"storageClass,omitempty"`
}

// ListUploads returns every incomplete multipart upload in the bucket,
// oldest first. The listing paginates: a bucket can carry more in-progress
// uploads than one page answers (the same 1000-key boundary object
// listings ride).
func ListUploads(ctx context.Context, client *s3.Client, bucket string) ([]IncompleteUpload, error) {
	paginator := s3.NewListMultipartUploadsPaginator(client, &s3.ListMultipartUploadsInput{
		Bucket: aws.String(bucket),
	})
	var ups []IncompleteUpload
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, mapUnsupported(err, "incomplete upload listing")
		}
		for _, u := range page.Uploads {
			ups = append(ups, IncompleteUpload{
				Key:          aws.ToString(u.Key),
				UploadID:     aws.ToString(u.UploadId),
				Initiated:    aws.ToTime(u.Initiated),
				StorageClass: string(u.StorageClass),
			})
		}
	}
	sort.Slice(ups, func(i, j int) bool { return ups[i].Initiated.Before(ups[j].Initiated) })
	return ups, nil
}

// AbortUpload aborts one incomplete multipart upload, releasing the parts
// the provider held for it. The key names the object the upload targeted
// (the abort wire takes bucket + key + upload ID together); aborting an
// upload never touches the object itself — a completed object at the same
// key stands, and an upload that never completed leaves nothing behind.
func AbortUpload(ctx context.Context, client *s3.Client, bucket, key, uploadID string) error {
	_, err := client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	return mapUnsupported(err, "incomplete upload abort")
}
