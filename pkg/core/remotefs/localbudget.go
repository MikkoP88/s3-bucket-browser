// localbudget.go: the local wire's share of the silent-death cure. FTP,
// SFTP and WebDAV got force-breakers (a socket or transport to tear), S3
// got a per-request transport budget — but a local syscall has neither a
// socket nor a context: os.ReadDir on a root whose backing went away (a
// UNC path or mapped drive whose server vanished — no FIN, no RST, the
// same wedge class) parks the calling goroutine inside the kernel until
// the redirector itself gives up, which can be effectively never. The
// cure here cannot be interruption, so it is abandonment: the step runs
// on its own goroutine and the caller waits only as long as the budget
// (or its own patience) allows. The abandoned step keeps running until
// the OS ends it — a bounded leak of one goroutine per wedged operation,
// never a parked caller — and whatever the step eventually produced is
// released rather than leaked (an abandoned Open's file handle closes).
package remotefs

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"time"
)

// ErrLocalDeadline is the verdict when a local point operation outran
// LocalOpBudget. It is deliberately NOT ErrCmdDeadline: that sentinel
// means "the engine tore its connection; redial" — local has no
// connection to tear and nothing to redial (a fresh dial would Stat the
// same hung root and pay the same budget again), so this verdict must
// reach the caller as an ordinary failure to voice, never as a death to
// heal.
var ErrLocalDeadline = errors.New("local operation exceeded its deadline")

// LocalOpBudget bounds one POINT operation on the local wire — one
// directory read, one stat, one open, one mkdir, one rename. Point work
// is normally sub-millisecond; fifteen seconds already tolerates the
// pathological-but-honest (antivirus interception, cold spin-up, a
// hundred-thousand-entry directory) before the verdict calls it dead.
// Deliberately NOT applied to streams (a transfer's reads and writes own
// their duration) nor to recursive acts like os.RemoveAll (a big tree's
// delete time is real work, not a dead peer — capping it would report a
// failure while the delete kept running). A var so tests can shorten it;
// read once per step in the caller's goroutine (the retire-grace
// discipline).
var LocalOpBudget = 15 * time.Second

// LocalStep runs one point operation under the budget and the caller's
// patience, returning the step's own verdict, the caller's context
// error, or ErrLocalDeadline — never a park. The caller's context is
// honored for the first time on this wire: cancel and caller deadlines
// end the wait (the step is abandoned to finish alone).
//
// If the wait was abandoned, the step's eventual result is released
// rather than dropped: a T that is an io.Closer (an open file) is
// closed, so an abandoned Open cannot leak its handle. Ownership of the
// verdict is decided by one compare-and-swap (0 waiting, 1 delivered,
// 2 abandoned) — a buffered send can never prove a receiver will come,
// so the winner of the CAS is the only side that may touch the result.
func LocalStep[T any](ctx context.Context, step func() (T, error)) (T, error) {
	budget := LocalOpBudget
	var zero T
	if ctx == nil {
		ctx = context.Background()
	}
	type verdict struct {
		v   T
		err error
	}
	const (
		waiting   = 0
		delivered = 1
		abandoned = 2
	)
	var state atomic.Uint32
	ch := make(chan verdict) // unbuffered: a send means a handoff
	go func() {
		v, err := step()
		if !state.CompareAndSwap(waiting, delivered) {
			// Abandoned: the caller is gone and nobody will read this
			// verdict — release what it holds instead of leaking it.
			if c, ok := any(v).(io.Closer); ok {
				_ = c.Close()
			}
			return
		}
		ch <- verdict{v, err}
	}()
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		if state.CompareAndSwap(waiting, abandoned) {
			return zero, ctx.Err()
		}
		r := <-ch // the step won the race as patience ended; its verdict is the truth
		return r.v, r.err
	case <-timer.C:
		if state.CompareAndSwap(waiting, abandoned) {
			return zero, ErrLocalDeadline
		}
		r := <-ch // the step finished as the budget fired; same truth
		return r.v, r.err
	}
}
