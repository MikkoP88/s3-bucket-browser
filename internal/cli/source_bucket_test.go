// source_bucket_test.go: hermetic coverage for the bucket scoping of S3
// data sources — `source add --bucket` and the s3://BUCKET shorthand.
// Adds are store-only (no dial), so no network or S3 backend is needed.
package cli

import (
	"strings"
	"testing"
)

func TestSourceAddS3Bucket(t *testing.T) {
	cliEnv(t)

	args := []string{"source", "add", "pics", "--type", "s3",
		"--access-key", "AKIATEST", "--secret-key", "sec",
		"--endpoint", "https://s3.example.com", "--bucket", "holiday-pics"}
	if code := Execute(args); code != 0 {
		t.Fatalf("source add --bucket: exit %d", code)
	}
	j, code := captureOut(t, "source", "list", "--json")
	if code != 0 || !strings.Contains(j, `"bucket": "holiday-pics"`) {
		t.Fatalf("source list --json missing bucket: exit %d, %s", code, j)
	}

	// re-adding the same name updates in place — no duplicates, and a
	// changed bucket rescopes the existing source
	args = []string{"source", "add", "pics", "--type", "s3",
		"--access-key", "AKIATEST", "--secret-key", "sec",
		"--endpoint", "https://s3.example.com", "--bucket", "other"}
	if code := Execute(args); code != 0 {
		t.Fatalf("re-add: exit %d", code)
	}
	s := reloadStore(t)
	if got := len(s.Sources); got != 1 {
		t.Fatalf("re-add duplicated sources: %d", got)
	}
	src, err := s.GetSource("pics")
	if err != nil || src.Bucket != "other" {
		t.Fatalf("re-add did not rescope the bucket: %+v %v", src, err)
	}
}

func TestSourceAddS3BucketURL(t *testing.T) {
	cliEnv(t)

	// one argument: the bucket is also the name
	if code := Execute([]string{"source", "add", "s3://my-bucket",
		"--access-key", "AKIATEST", "--secret-key", "sec"}); code != 0 {
		t.Fatalf("source add s3://: exit %d", code)
	}
	s := reloadStore(t)
	src, err := s.GetSource("my-bucket")
	if err != nil || src.Bucket != "my-bucket" || src.Type != "s3" {
		t.Fatalf("s3:// shorthand: %+v %v", src, err)
	}

	// NAME + URL keeps NAME
	if code := Execute([]string{"source", "add", "prod", "s3://prod-data",
		"--access-key", "AKIATEST", "--secret-key", "sec"}); code != 0 {
		t.Fatalf("source add NAME s3://: exit %d", code)
	}
	s = reloadStore(t)
	if src, err := s.GetSource("prod"); err != nil || src.Bucket != "prod-data" {
		t.Fatalf("NAME + s3://: %+v %v", src, err)
	}

	// --bucket disagreeing with the URL is a usage error
	if code := Execute([]string{"source", "add", "x", "s3://a-bucket", "--bucket", "nope"}); code != exitUsage {
		t.Fatalf("conflicting --bucket: exit %d (want usage %d)", code, exitUsage)
	}
	// empty shorthand is malformed
	if code := Execute([]string{"source", "add", "s3://"}); code != exitUsage {
		t.Fatalf("empty s3://: exit %d (want usage %d)", code, exitUsage)
	}
}
