// bound_test.go pins the panic net: a command that panics inside its
// bound runner reaches the caller as an honest ErrCmdPanic error —
// never an unrecovered panic that kills the process — and the verdict
// classifies as connection-death, so the FTP engine's redial and the
// api layer's healing cache both treat the channel as gone.
package remotefs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type stubBreaker struct {
	quitCalled *bool
}

func (s stubBreaker) Quit() error { *s.quitCalled = true; return nil }

func TestBoundPanicBecomesError(t *testing.T) {
	quitCalled := false
	v, err := bound(context.Background(), stubBreaker{&quitCalled}, time.Second, func() (string, error) {
		panic("slice bounds out of range [98:49]")
	})
	if err == nil || !errors.Is(err, ErrCmdPanic) {
		t.Fatalf("err = %v, want ErrCmdPanic", err)
	}
	if !strings.Contains(err.Error(), "slice bounds out of range") {
		t.Fatalf("err = %v, want the panic value carried", err)
	}
	if v != "" {
		t.Fatalf("value = %q, want the zero value", v)
	}
	if quitCalled {
		t.Fatal("Quit must not fire for a panicking command — the caller gets the error directly, no break needed")
	}
}

func TestBoundPanicStillHonorsTimeoutShape(t *testing.T) {
	// The net changes nothing about the healthy paths: a returning fn
	// still delivers its own value and error verbatim.
	quitCalled := false
	v, err := bound(context.Background(), stubBreaker{&quitCalled}, time.Second, func() (string, error) {
		return "ok", nil
	})
	if err != nil || v != "ok" {
		t.Fatalf("v=%q err=%v, want the plain result untouched", v, err)
	}
	if quitCalled {
		t.Fatal("Quit must not fire for a healthy command")
	}
}

func TestErrCmdPanicClassifiesAsConnDeath(t *testing.T) {
	wrapped := fmt.Errorf("list /dir: %w", ErrCmdPanic)
	if !isConnErr(wrapped) {
		t.Fatal("isConnErr(ErrCmdPanic) = false, want true — the engine must redial, never reuse the torn channel")
	}
}
