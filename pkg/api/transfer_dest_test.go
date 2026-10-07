// transfer_dest_test.go: the destination contract. Every job carries
// where its bytes land in the navigation grammar (typed XferDest, not the
// human To label), stamped at each entry point before the worker spawns —
// and a retried job restamps its own dest, because retry re-enters the
// same entry points.
package api

import (
	"context"
	"testing"
)

// TestUploadJobCarriesDest pins the upload stamp: the view source, the
// bucket and the key-grammar prefix (dirPrefix — trailing slash, empty
// at the bucket root), never the human label.
func TestUploadJobCarriesDest(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	f := newFakeS3("up")
	if err := a.SaveSource(fakeS3Source("upsrc", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewSource("upsrc"); err != nil {
		t.Fatal(err)
	}
	paths := writeFiles(t, t.TempDir(), "pic.jpg", "clip.mp4")

	id, err := a.Upload(paths, "up", "photos/", PolicyOverwrite, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	ji := waitXferJob(t, a, id)
	if want := (XferDest{Kind: "s3", Source: "upsrc", Bucket: "up", Dir: "photos/"}); ji.Dest != want {
		t.Fatalf("dest = %+v, want %+v", ji.Dest, want)
	}

	// The bucket root uploads to the empty prefix, not "/".
	id, ji = mustUpload(t, a, paths, "up", PolicyOverwrite)
	if want := (XferDest{Kind: "s3", Source: "upsrc", Bucket: "up", Dir: ""}); ji.Dest != want {
		t.Fatalf("root dest = %+v, want %+v", ji.Dest, want)
	}
}

// TestDownloadJobCarriesDest pins the download stamp: the local folder
// exactly as the entry received it.
func TestDownloadJobCarriesDest(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	f := newFakeS3("dn")
	f.seed("dn", "a.txt", "body")
	if err := a.SaveSource(fakeS3Source("dnsrc", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewSource("dnsrc"); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	id, err := a.Download("dn", []DownloadItem{{Key: "a.txt", Size: 4}}, dest, PolicyOverwrite, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	ji := waitXferJob(t, a, id)
	if want := (XferDest{Kind: "local", Dir: dest}); ji.Dest != want {
		t.Fatalf("dest = %+v, want %+v", ji.Dest, want)
	}
}

// TestTransferCrossCarriesDest pins the cross-transfer stamp: the caller's
// XferDest rides the row verbatim — remote and local alike.
func TestTransferCrossCarriesDest(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	srcSrc, srcRoot := emptyLocalSource(t, "origin")
	if err := a.SaveSource(srcSrc); err != nil {
		t.Fatal(err)
	}
	vaultSrc, _ := emptyLocalSource(t, "vault")
	if err := a.SaveSource(vaultSrc); err != nil {
		t.Fatal(err)
	}
	paths := writeFiles(t, srcRoot, "a.txt", "b.txt")

	remoteDest := XferDest{Kind: "remote", Source: "vault", Dir: "/"}
	id, err := a.TransferCross([]XferItem{{Source: "origin", Key: "/a.txt", Size: int64(len("content of a.txt"))}}, nil, remoteDest, PolicyOverwrite, 0, false, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	ji := waitXferJob(t, a, id)
	if ji.Dest != remoteDest {
		t.Fatalf("remote dest = %+v, want %+v verbatim", ji.Dest, remoteDest)
	}

	localDest := XferDest{Kind: "local", Dir: t.TempDir()}
	id, err = a.TransferCross(nil, []string{paths[1]}, localDest, PolicyOverwrite, 0, false, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	ji = waitXferJob(t, a, id)
	if ji.Dest != localDest {
		t.Fatalf("local dest = %+v, want %+v verbatim", ji.Dest, localDest)
	}
}

// TestRetryRestampsDest pins the recovery shape: a retried job re-enters
// the same entry point, so its own row carries the same destination.
func TestRetryRestampsDest(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())
	retryTuning(t, a)

	f := newFakeS3("up")
	if err := a.SaveSource(fakeS3Source("upsrc", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	if err := a.SetViewSource("upsrc"); err != nil {
		t.Fatal(err)
	}
	paths := writeFiles(t, t.TempDir(), "good.txt", "bad.txt")
	f.fault("PUT", "up", "bad.txt", 1)

	id, ji := mustUpload(t, a, paths, "up", PolicyOverwrite)
	if ji.Status != JobError || ji.Dest.Dir != "" {
		t.Fatalf("job = %+v, want a failed upload at the bucket root", ji)
	}
	nid, err := a.RetryTransfer(id)
	if err != nil {
		t.Fatal(err)
	}
	ji2 := waitXferJob(t, a, nid)
	if ji2.Dest != ji.Dest {
		t.Fatalf("retry dest = %+v, want the original %+v", ji2.Dest, ji.Dest)
	}
}
