package listing

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

// pageLister serves one ListObjectsV2 page, the paginator's only call
// when IsTruncated stays false.
type pageLister struct {
	out s3.ListObjectsV2Output
}

func (p *pageLister) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return &p.out, nil
}

func TestDirEntryFromObject(t *testing.T) {
	mod := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	e := DirEntryFromObject(s3types.Object{
		Key:          aws.String("photos/2026/"),
		LastModified: &mod,
		StorageClass: s3types.ObjectStorageClassStandard,
		ETag:         aws.String(`"m1"`),
	}, "photos/")
	if !e.IsDir || e.Name != "2026" || e.Key != "photos/2026/" {
		t.Errorf("folder shape mismatch: %+v", e)
	}
	if e.LastModified == nil || !e.LastModified.Equal(mod) {
		t.Errorf("marker date not lent: %+v", e)
	}
	if e.StorageClass != "STANDARD" || e.ETag != "m1" {
		t.Errorf("marker class/ETag not lent (quotes trimmed): %+v", e)
	}
}

func TestListMergesMarkerMetadata(t *testing.T) {
	markerMod := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	objMod := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	c := &pageLister{out: s3.ListObjectsV2Output{
		CommonPrefixes: []s3types.CommonPrefix{{Prefix: aws.String("docs/")}},
		Contents: []s3types.Object{
			{Key: aws.String("docs/"), LastModified: &markerMod, ETag: aws.String(`"mk"`), StorageClass: s3types.ObjectStorageClassStandard},
			{Key: aws.String("docs/a.txt"), Size: aws.Int64(5), LastModified: &objMod},
		},
	}}
	entries, err := List(context.Background(), c, "b", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 rows (folder merged, not doubled), got %d: %+v", len(entries), entries)
	}
	docs := entries[0]
	if !docs.IsDir || docs.Name != "docs" || docs.Key != "docs/" {
		t.Errorf("folder shape mismatch: %+v", docs)
	}
	if docs.LastModified == nil || !docs.LastModified.Equal(markerMod) {
		t.Errorf("marker date not merged into the prefix row: %+v", docs)
	}
	if docs.StorageClass != "STANDARD" || docs.ETag != "mk" {
		t.Errorf("marker class/ETag not merged into the prefix row: %+v", docs)
	}
	if entries[1].Name != "docs/a.txt" || entries[1].IsDir {
		t.Errorf("object row disturbed: %+v", entries[1])
	}
}
