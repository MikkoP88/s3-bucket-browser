// engineheal_test.go: the cached-engine healing contract. A dead wire
// must not strand a source; a cancel, a closed engine, a semantic error,
// or a consumed stream (Create) must never trigger a transparent retry —
// the Open handshake, which has flowed zero bytes, heals like Stat.
package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
)

// healStubFS is a scripted engine: every op records itself and returns
// failWith (nil = success); Close counts.
type healStubFS struct {
	name     string
	failWith error

	mu     sync.Mutex
	calls  map[string]int
	closed int
}

func newHealStub(name string, failWith error) *healStubFS {
	return &healStubFS{name: name, failWith: failWith, calls: map[string]int{}}
}

func (s *healStubFS) called(op string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[op]
}

func (s *healStubFS) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *healStubFS) tick(op string) {
	s.mu.Lock()
	s.calls[op]++
	s.mu.Unlock()
}

func (s *healStubFS) List(ctx context.Context, dir string) ([]listing.Entry, error) {
	s.tick("list")
	return nil, s.failWith
}

func (s *healStubFS) Stat(ctx context.Context, p string) (listing.Entry, error) {
	s.tick("stat")
	return listing.Entry{}, s.failWith
}

func (s *healStubFS) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	s.tick("open")
	return nil, 0, s.failWith
}

func (s *healStubFS) Create(ctx context.Context, p string, r io.Reader) error {
	s.tick("create")
	return s.failWith
}

func (s *healStubFS) MkdirAll(ctx context.Context, dir string) error {
	s.tick("mkdirall")
	return s.failWith
}

func (s *healStubFS) Remove(ctx context.Context, p string) error {
	s.tick("remove")
	return s.failWith
}

func (s *healStubFS) Rename(ctx context.Context, oldp, newp string) error {
	s.tick("rename")
	return s.failWith
}

func (s *healStubFS) Close() error {
	s.mu.Lock()
	s.closed++
	s.mu.Unlock()
	return nil
}

// newHealHarness wraps inner in a healingFS whose redial serves fresh()
// (nil fresh = dial fails). The second return value reports dial count.
func newHealHarness(t *testing.T, inner remotefs.FS, fresh func() remotefs.FS) (*healingFS, func() int) {
	t.Helper()
	a := newTestApp(t)
	a.Startup(context.Background())
	dials := 0
	h := &healingFS{
		a:   a,
		src: profile.Source{Name: "heal-test", Type: profile.TypeLocal, LocalRoot: t.TempDir()},
		dial: func(ctx context.Context, _ profile.Source) (remotefs.FS, error) {
			dials++
			if fresh == nil {
				return nil, errors.New("dial refused: wire still dead")
			}
			return fresh(), nil
		},
		inner: inner,
	}
	return h, func() int { return dials }
}

// The classifier must recognize every transport-death shape the engines
// raise, and never treat a semantic failure or the caller's own context
// deadline as a dead wire.
func TestConnDeadClassifier(t *testing.T) {
	dead := []error{
		io.EOF,
		io.ErrUnexpectedEOF,
		net.ErrClosed,
		syscall.ECONNRESET,
		syscall.EPIPE,
		syscall.ETIMEDOUT,
		errors.New("sftp: connection lost"),
		errors.New("read tcp 10.0.0.5:22->10.0.0.9:1: connection reset by peer"),
		errors.New("write tcp: broken pipe"),
		errors.New("ftp: control connection closed"),
		errors.New("dial tcp: connection refused"),
		errors.New("read: i/o timeout"),
		// The engine's own force-break verdict — the cache must redial on
		// it or every later op speaks to a deliberately torn corpse.
		remotefs.ErrCmdDeadline,
		fmt.Errorf("list %s: %w", "/dir", remotefs.ErrCmdDeadline),
	}
	for _, err := range dead {
		if !connDead(err) {
			t.Errorf("connDead(%v) = false, want true", err)
		}
	}

	live := []error{
		nil,
		context.Canceled,
		context.DeadlineExceeded,
		errors.New("cancel by caller wrapped: " + context.Canceled.Error()), // not errors.Is
		os.ErrNotExist,
		&os.PathError{Op: "remove", Path: "/x", Err: os.ErrPermission},
		errors.New("file already exists"),
	}
	for _, err := range live {
		if connDead(err) {
			t.Errorf("connDead(%v) = true, want false", err)
		}
	}

	// A context.Canceled wrapped with %w must never read as dead wire.
	if connDead(errors.Join(io.EOF, context.Canceled)) != false {
		t.Errorf("connDead(EOF+Canceled) = true — a canceled op must not redial")
	}
}

// One wire death, one redial: the op heals on the fresh engine, the swap
// persists for later ops, and the dead engine is closed exactly once.
func TestHealingFSHealsDeadConnection(t *testing.T) {
	deadWire := errors.New("sftp: connection lost")
	inner1 := newHealStub("dead-engine", deadWire)
	fresh := newHealStub("fresh-engine", nil)
	h, dials := newHealHarness(t, inner1, func() remotefs.FS { return fresh })

	if err := h.Remove(context.Background(), "/gone.txt"); err != nil {
		t.Fatalf("healed Remove: %v", err)
	}
	if got := dials(); got != 1 {
		t.Fatalf("redials = %d, want 1", got)
	}
	if got := fresh.called("remove"); got != 1 {
		t.Fatalf("fresh engine remove calls = %d, want 1 (the retry)", got)
	}
	if got := inner1.called("remove"); got != 1 {
		t.Fatalf("dead engine remove calls = %d, want 1 (no re-beating a dead engine)", got)
	}
	if got := inner1.closeCount(); got != 1 {
		t.Fatalf("dead engine closed %d times, want 1", got)
	}
	// The swap persists: the next op rides the fresh engine directly.
	if _, err := h.Stat(context.Background(), "/x"); err != nil {
		t.Fatalf("post-heal Stat: %v", err)
	}
	if got := fresh.called("stat"); got != 1 {
		t.Fatalf("fresh engine stat calls = %d, want 1 (swap persisted)", got)
	}
	if got := dials(); got != 1 {
		t.Fatalf("redials = %d after a second op — the swap must persist, not re-dial", got)
	}
}

// Every healed op kind redials once; Open and Create never do.
func TestHealingFSHealsEveryIdempotentOp(t *testing.T) {
	for _, op := range []struct {
		name string
		run  func(remotefs.FS, context.Context) error
	}{
		{"list", func(fs remotefs.FS, ctx context.Context) error { _, err := fs.List(ctx, "/"); return err }},
		{"stat", func(fs remotefs.FS, ctx context.Context) error { _, err := fs.Stat(ctx, "/x"); return err }},
		{"mkdirall", func(fs remotefs.FS, ctx context.Context) error { return fs.MkdirAll(ctx, "/d") }},
		{"remove", func(fs remotefs.FS, ctx context.Context) error { return fs.Remove(ctx, "/x") }},
		{"rename", func(fs remotefs.FS, ctx context.Context) error { return fs.Rename(ctx, "/a", "/b") }},
	} {
		t.Run(op.name, func(t *testing.T) {
			inner := newHealStub("dead", errors.New("connection reset by peer"))
			fresh := newHealStub("fresh", nil)
			h, dials := newHealHarness(t, inner, func() remotefs.FS { return fresh })
			if err := op.run(h, context.Background()); err != nil {
				t.Fatalf("healed %s: %v", op.name, err)
			}
			if got := dials(); got != 1 {
				t.Fatalf("%s redials = %d, want 1", op.name, got)
			}
		})
	}
}

// Create surfaces the wire death — its reader is consumed inside the call,
// so a transparent restart would corrupt the transfer.
func TestHealingFSCreateNeverRetries(t *testing.T) {
	deadWire := errors.New("connection closed")
	inner := newHealStub("dead", deadWire)
	h, dials := newHealHarness(t, inner, func() remotefs.FS { return newHealStub("fresh", nil) })

	if err := h.Create(context.Background(), "/up.bin", strings.NewReader("x")); !errors.Is(err, deadWire) {
		t.Fatalf("Create: err = %v, want the wire death surfaced", err)
	}
	if got := dials(); got != 0 {
		t.Fatalf("redials = %d after Create — a consumed stream must never transparently retry", got)
	}
}

// The Open handshake heals: an open call that dies on the wire has flowed
// zero bytes and holds no handle, so it redials and retries itself exactly
// like Stat. This is what makes a user's transfer retry after a wire death
// recover on the first click instead of failing with "connection lost"
// until an unrelated listing heals the source.
func TestHealingFSOpenHandshakeHeals(t *testing.T) {
	deadWire := errors.New("sftp: connection lost")
	inner1 := newHealStub("dead-engine", deadWire)
	fresh := newHealStub("fresh-engine", nil)
	h, dials := newHealHarness(t, inner1, func() remotefs.FS { return fresh })

	if _, _, err := h.Open(context.Background(), "/big.bin"); err != nil {
		t.Fatalf("healed Open: %v", err)
	}
	if got := dials(); got != 1 {
		t.Fatalf("redials = %d, want 1", got)
	}
	if got := inner1.called("open"); got != 1 {
		t.Fatalf("dead engine open calls = %d, want 1 (no re-beating a dead engine)", got)
	}
	if got := fresh.called("open"); got != 1 {
		t.Fatalf("fresh engine open calls = %d, want 1 (the retry)", got)
	}
	// The swap persists: the next Open rides the fresh engine directly.
	if _, _, err := h.Open(context.Background(), "/big.bin"); err != nil {
		t.Fatalf("post-heal Open: %v", err)
	}
	if got := fresh.called("open"); got != 2 {
		t.Fatalf("fresh engine open calls = %d, want 2 (swap persisted)", got)
	}
	if got := dials(); got != 1 {
		t.Fatalf("redials = %d after a second Open — the swap must persist, not re-dial", got)
	}
}

// Open on a wire that is STILL down surfaces the original death — the
// handshake heals once, it does not invent a connection.
func TestHealingFSOpenRedialRefusedSurfacesOriginal(t *testing.T) {
	deadWire := errors.New("sftp: connection lost")
	inner := newHealStub("dead", deadWire)
	h, dials := newHealHarness(t, inner, nil) // fresh == nil → dial fails

	if _, _, err := h.Open(context.Background(), "/big.bin"); !connDead(err) {
		t.Fatalf("Open err = %v, want the original wire death", err)
	}
	if got := dials(); got != 1 {
		t.Fatalf("redials = %d, want exactly one attempt", got)
	}
}

// A canceled operation must not redial, even on a dead wire.
func TestHealingFSCanceledOpNeverRedials(t *testing.T) {
	inner := newHealStub("dead", errors.New("connection lost"))
	h, dials := newHealHarness(t, inner, func() remotefs.FS { return newHealStub("fresh", nil) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.Remove(ctx, "/x"); err == nil {
		t.Fatal("canceled Remove on a dead wire returned nil")
	}
	if got := dials(); got != 0 {
		t.Fatalf("redials = %d on a canceled op — cancellation must stop at the error", got)
	}
}

// A semantic failure is not a wire death — no redial, error passes through.
func TestHealingFSSemanticErrorsPassThrough(t *testing.T) {
	missing := os.ErrNotExist
	inner := newHealStub("healthy", missing)
	h, dials := newHealHarness(t, inner, func() remotefs.FS { return newHealStub("fresh", nil) })

	if err := h.Remove(context.Background(), "/nope"); !errors.Is(err, missing) {
		t.Fatalf("Remove err = %v, want os.ErrNotExist verbatim", err)
	}
	if got := dials(); got != 0 {
		t.Fatalf("redials = %d on a semantic error", got)
	}
}

// The wire still down: the original error surfaces, nothing is invented.
func TestHealingFSRedialRefusedSurfacesOriginal(t *testing.T) {
	inner := newHealStub("dead", errors.New("sftp: connection lost"))
	h, dials := newHealHarness(t, inner, nil) // fresh == nil → dial fails

	err := h.Remove(context.Background(), "/x")
	if err == nil || !connDead(err) {
		t.Fatalf("Remove err = %v, want the original wire death", err)
	}
	if got := dials(); got != 1 {
		t.Fatalf("redials = %d, want exactly one attempt", got)
	}
}

// An engine dropped by closeEngines (source edit) must not be resurrected
// by a racing op — Close is final for the wrapper.
func TestHealingFSClosedEngineStaysDead(t *testing.T) {
	inner := newHealStub("dropped", errors.New("connection lost"))
	h, dials := newHealHarness(t, inner, func() remotefs.FS { return newHealStub("fresh", nil) })

	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := h.Remove(context.Background(), "/x"); err == nil {
		t.Fatal("Remove on a closed wrapper returned nil — dropped engines must not resurrect")
	}
	if got := dials(); got != 0 {
		t.Fatalf("redials = %d after Close", got)
	}
}
