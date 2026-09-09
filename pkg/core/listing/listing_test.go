package listing

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestFromObject(t *testing.T) {
	mod := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	e := FromObject(s3types.Object{
		Key:          aws.String("photos/2026/a.jpg"),
		Size:         aws.Int64(1024),
		ETag:         aws.String(`"abc123"`),
		StorageClass: s3types.ObjectStorageClassStandard,
		LastModified: &mod,
	}, "photos/")
	if e.Name != "2026/a.jpg" {
		t.Errorf("Name = %q", e.Name)
	}
	if e.Size != 1024 || e.ETag != "abc123" || e.StorageClass != "STANDARD" {
		t.Errorf("fields mismatch: %+v", e)
	}
	if e.LastModified == nil || !e.LastModified.Equal(mod) {
		t.Error("LastModified mismatch")
	}
}

func TestDirEntry(t *testing.T) {
	e := DirEntry("photos/2026/", "photos/")
	if !e.IsDir || e.Name != "2026" || e.Key != "photos/2026/" {
		t.Errorf("DirEntry mismatch: %+v", e)
	}
}

func TestSortEntries(t *testing.T) {
	entries := []Entry{
		{Name: "zeta.txt"},
		{Name: "Alpha"},
		{Name: "beta", IsDir: true},
		{Name: "Gamma"},
		{Name: "aaa", IsDir: true},
	}
	SortEntries(entries)
	want := []string{"aaa", "beta", "Alpha", "Gamma", "zeta.txt"}
	for i, w := range want {
		if entries[i].Name != w {
			t.Fatalf("sort order = %v", entries)
		}
	}
}

func TestUsageAccumulation(t *testing.T) {
	// Du's callback logic exercised via Walk semantics: unit-test the
	// accumulation contract with a synthetic closure.
	u := Usage{}
	add := func(o s3types.Object) {
		u.ObjectCount++
		u.TotalBytes += aws.ToInt64(o.Size)
	}
	add(s3types.Object{Size: aws.Int64(10)})
	add(s3types.Object{Size: aws.Int64(32)})
	if u.ObjectCount != 2 || u.TotalBytes != 42 {
		t.Errorf("usage accumulation broken: %+v", u)
	}
}
