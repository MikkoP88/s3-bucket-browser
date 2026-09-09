// class.go implements server-side storage-class conversion: an object is
// copied onto itself with a new storage class (S3's only conversion
// mechanism, PLAN.md §8.6).
package transfer

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ValidStorageClasses lists the storage classes a conversion may target.
var ValidStorageClasses = []string{
	"STANDARD", "REDUCED_REDUNDANCY", "STANDARD_IA", "ONEZONE_IA",
	"INTELLIGENT_TIERING", "GLACIER", "GLACIER_IR", "DEEP_ARCHIVE",
}

// ValidStorageClass reports whether class is a known S3 storage class.
func ValidStorageClass(class string) bool {
	for _, c := range ValidStorageClasses {
		if strings.EqualFold(c, class) {
			return true
		}
	}
	return false
}

// ConvertStorageClass copies one object (or one concrete version) onto
// itself with the new storage class. Metadata is preserved (self-copy with
// the default COPY directive); GLACIER targets note that restoring needs
// an explicit restore request.
func ConvertStorageClass(ctx context.Context, client *s3.Client, bucket, key, versionID, class string) error {
	if !ValidStorageClass(class) {
		return fmt.Errorf("unknown storage class %q (use one of: %s)", class, strings.Join(ValidStorageClasses, ", "))
	}
	src := bucket + "/" + key
	if versionID != "" {
		src += "?versionId=" + versionID
	}
	_, err := client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(key),
		CopySource:   aws.String(src),
		StorageClass: s3types.StorageClass(strings.ToUpper(class)),
	})
	if err != nil && strings.Contains(err.Error(), "to itself without changing") {
		// S3 rejects a self-copy that changes nothing — for a class
		// conversion that means the object is already in the target
		// class. Treat as success so conversions are idempotent.
		return nil
	}
	return err
}
