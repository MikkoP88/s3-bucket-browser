// xfer_integrity_test.go: the transfer integrity contract. A source that
// ends clean short of the size its own wire promised — the FTP shape:
// SIZE said 100, the data connection delivered 40 and closed cleanly —
// must fail the file with nothing committed at the destination (io.Copy
// would return nil for that and the copy would land as though it were
// whole), and the S3 legs must ask for checksum mode so the SDK
// validates payloads whenever the server sends checksums.
package api

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/transfer"
)

// shortOpenFS is a real local engine whose Open lies exactly the way a
// truncated FTP/WebDAV stream does: it announces a size and delivers
// fewer bytes, ending in a clean EOF. Stat lies consistently so the
// planner plans the promised size; everything else — Create's staging
// and commit, Remove, Rename — is the real engine, so the destination
// half of the rig behaves exactly as production does.
type shortOpenFS struct {
	remotefs.FS
	claim int64
	body  string
}

func (s shortOpenFS) Stat(ctx context.Context, p string) (listing.Entry, error) {
	return listing.Entry{Key: p, Name: path.Base(strings.TrimPrefix(p, "/")), Size: s.claim}, nil
}

func (s shortOpenFS) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	return io.NopCloser(strings.NewReader(s.body)), s.claim, nil
}

// TestXferShortSourceFailsLoud is the field shape end to end: the
// promised 100 bytes never finish, the file fails with the
// short-stream verdict naming both counts, and the destination holds
// neither a partial at the final name nor a staging leftover.
func TestXferShortSourceFailsLoud(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	liarSrc, _ := emptyLocalSource(t, "liar")
	liarSrc.ID = "src-liar-stub" // engine cache keys by ID; own it so the seed matches the saved source
	if err := a.SaveSource(liarSrc); err != nil {
		t.Fatal(err)
	}
	vaultSrc, vaultRoot := emptyLocalSource(t, "vault")
	if err := a.SaveSource(vaultSrc); err != nil {
		t.Fatal(err)
	}
	inner, err := remotefs.NewLocal(context.Background(), liarSrc.LocalRoot)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-seed the engine cache: the planner's remote leg then speaks to
	// the lying Open instead of dialing a fresh engine.
	a.engMu.Lock()
	a.engines[liarSrc.ID] = shortOpenFS{FS: inner, claim: 100, body: strings.Repeat("x", 40)}
	a.engMu.Unlock()

	ji := mustXfer(t, a,
		[]XferItem{{Source: "liar", Key: "/big.bin", Size: 100}}, nil,
		XferDest{Kind: "remote", Source: "vault", Dir: "/"}, PolicyOverwrite, false)
	if ji.Status != JobError || ji.FailedFiles != 1 {
		t.Fatalf("job = %+v, want exactly one failed file", ji)
	}
	if !strings.Contains(ji.Error, "short stream") || !strings.Contains(ji.Error, "40 of 100") {
		t.Fatalf("job error = %q, want the short-stream verdict with both counts", ji.Error)
	}
	if _, serr := os.Stat(filepath.Join(vaultRoot, "big.bin")); !errors.Is(serr, fs.ErrNotExist) {
		t.Fatalf("partial committed at the final name: %v", serr)
	}
	ents, rerr := os.ReadDir(vaultRoot)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(ents) != 0 {
		t.Fatalf("staging leftovers after the failed copy: %v", ents)
	}
}

// TestXferS3SourceVerifiedAndChecksummed runs the honest S3 leg through
// the same convergence: the object lands whole (an exact promise must
// never trip the verifier) and the GET asked for checksum mode.
func TestXferS3SourceVerifiedAndChecksummed(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	f := newFakeS3("vault")
	f.seed("vault", "hello.txt", "hello")
	if err := a.SaveSource(fakeS3Source("marks", f.serve(t))); err != nil {
		t.Fatal(err)
	}
	destSrc, destRoot := emptyLocalSource(t, "dest")
	if err := a.SaveSource(destSrc); err != nil {
		t.Fatal(err)
	}

	ji := mustXfer(t, a,
		[]XferItem{{Source: "marks", Bucket: "vault", Key: "hello.txt", Size: 5}}, nil,
		XferDest{Kind: "remote", Source: "dest", Dir: "/"}, PolicyOverwrite, false)
	if ji.Status != JobDone || ji.DoneFiles != 1 {
		t.Fatalf("job = %+v, want one done file", ji)
	}
	if got := read(t, filepath.Join(destRoot, "hello.txt")); got != "hello" {
		t.Fatalf("landed content = %q, want the whole object", got)
	}
	if !f.checksumModeSeen() {
		t.Fatal("the object GET never asked for checksum mode")
	}
}

// TestXferUnknownSizeStillStreams pins the exemption: an engine that
// reports no size (FTP without SIZE, chunked WebDAV) streams unverified
// exactly as before — the wrap must not turn unknown into zero and
// truncate every such transfer to nothing.
func TestXferUnknownSizeStillStreams(t *testing.T) {
	a := newTestApp(t)
	a.Startup(context.Background())

	liarSrc, _ := emptyLocalSource(t, "blind")
	liarSrc.ID = "src-blind-stub" // same ID ownership as the liar rig
	if err := a.SaveSource(liarSrc); err != nil {
		t.Fatal(err)
	}
	destSrc, destRoot := emptyLocalSource(t, "vault")
	if err := a.SaveSource(destSrc); err != nil {
		t.Fatal(err)
	}
	inner, err := remotefs.NewLocal(context.Background(), liarSrc.LocalRoot)
	if err != nil {
		t.Fatal(err)
	}
	a.engMu.Lock()
	a.engines[liarSrc.ID] = shortOpenFS{FS: inner, claim: 0, body: "streams anyway"}
	a.engMu.Unlock()

	ji := mustXfer(t, a,
		[]XferItem{{Source: "blind", Key: "/note.txt", Size: int64(len("streams anyway"))}}, nil,
		XferDest{Kind: "remote", Source: "vault", Dir: "/"}, PolicyOverwrite, false)
	if ji.Status != JobDone || ji.DoneFiles != 1 {
		t.Fatalf("job = %+v, want one done file", ji)
	}
	if got := read(t, filepath.Join(destRoot, "note.txt")); got != "streams anyway" {
		t.Fatalf("landed content = %q, want the whole unverified stream", got)
	}
}

// ErrShortStream must stay addressable as the contract's sentinel: the
// job error strings above are matched by substring, but callers in other
// packages assert identity through transfer.ErrShortStream.
var _ = transfer.ErrShortStream
