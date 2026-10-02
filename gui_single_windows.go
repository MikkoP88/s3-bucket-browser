//go:build windows && !s3b_headless && !server

package gui

import (
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/eventlog"
)

// Single-instance guard (desktop Windows only): launching the GUI while
// another copy runs would start a second webview tree against the same
// WebView2 user-data folder — the loser blocks on the folder lock and
// hangs into the startup watchdog, the exact 2026-09-17 incident shape
// pkg/guihealth exists for. Instead, the second launch signals the
// running instance forward (restore-if-minimised, foreground, popouts
// lifted as a group) and exits. Server builds (-tags server) and the
// headless CLI stay multi-instance by design; macOS and Linux desktops
// get single-launch semantics from their own windowing systems.

const (
	instanceMutexName = `Local\s3b.bucket.browser.instance`
	focusEventName    = `Local\s3b.bucket.browser.focus`
	// multiInstanceEnv opts out of the guard entirely (side-by-side rigs,
	// running two profiles at once). Any non-empty value opts out.
	multiInstanceEnv = "S3B_MULTI_INSTANCE"
)

var (
	pCreateMutexW        = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW")
	pCreateEventW        = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateEventW")
	pOpenEventW          = syscall.NewLazyDLL("kernel32.dll").NewProc("OpenEventW")
	pSetEvent            = syscall.NewLazyDLL("kernel32.dll").NewProc("SetEvent")
	pWaitForSingleObject = syscall.NewLazyDLL("kernel32.dll").NewProc("WaitForSingleObject")
)

const (
	eventModifyState = 0x0002 // OpenEventW right SetEvent needs
	waitObject0      = 0      // WaitForSingleObject: signaled
	waitTimeoutMs    = 500    // pump slice — see the waiter goroutine below
	signalRetrySpan  = 2 * time.Second
	signalRetryStep  = 50 * time.Millisecond
)

// guardSingleInstance is Run's front door: it decides whether this process
// may become the GUI. existing=true means another instance runs — Run
// returns at once and the process exits 0. The winner gets a channel that
// receives once per later launch (the relay in Run raises the app on it).
// Any guard failure fails open: a broken guard may never block a launch.
func guardSingleInstance() (focus <-chan struct{}, existing bool) {
	if os.Getenv(multiInstanceEnv) != "" {
		return nil, false
	}
	focus, existing, focused := acquireInstanceLock(instanceMutexName, focusEventName)
	if existing {
		if focused {
			eventlog.Append("info", "app", "", "second launch: focused the running instance and exited")
		} else {
			eventlog.Append("warn", "app", "", "second launch: could not signal the running instance (focus event unreachable); exited without focusing")
		}
	}
	return focus, existing
}

// acquireInstanceLock is the kernel half of the guard, parameterized by
// object name so the tests can exercise it without touching the production
// names (a real instance may be running while tests execute).
//
// The mutex's existence marks "an instance runs" — Local\ scopes it to the
// login session, and the kernel reclaims it when the owning process dies,
// so a crashed instance leaves nothing stale behind. The auto-reset focus
// event is the wake-up channel: the winner creates it right after winning
// the mutex and pumps it for the process lifetime; a second launch opens
// and sets it. Auto-reset wakes exactly one waiter, and a signal that
// lands while the pump goroutine is busy stays latched for the next round
// trip — repeated launches each raise the app, none are lost to timing.
func acquireInstanceLock(mutexName, eventName string) (focus <-chan struct{}, existing bool, focused bool) {
	mPtr, err := syscall.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, false, false // invalid name — fail open
	}
	mh, _, err := pCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(mPtr)))
	if mh == 0 {
		return nil, false, false // CreateMutexW failed — fail open
	}
	if err == syscall.ERROR_ALREADY_EXISTS {
		syscall.CloseHandle(syscall.Handle(mh))
		return nil, true, signalExisting(eventName)
	}

	// This process is the instance. Create the focus event before anything
	// else so no second launch can win the mutex and miss the event window.
	ePtr, err := syscall.UTF16PtrFromString(eventName)
	if err != nil {
		syscall.CloseHandle(syscall.Handle(mh))
		return nil, false, false
	}
	eh, _, _ := pCreateEventW.Call(0, 0, 0, uintptr(unsafe.Pointer(ePtr)))
	if eh == 0 {
		syscall.CloseHandle(syscall.Handle(mh))
		return nil, false, false // no event — fail open (multi-instance)
	}
	ch := make(chan struct{})
	go func() {
		// The 500ms timeout keeps the parked thread returning to the
		// scheduler instead of blocking forever; WAIT_OBJECT_0 is the only
		// waking state. Sends block until the relay consumes them, which
		// coalesces bursts into the next raise.
		for {
			r1, _, _ := pWaitForSingleObject.Call(eh, waitTimeoutMs)
			if r1 == waitObject0 {
				ch <- struct{}{}
			}
		}
	}()
	return ch, false, false
}

// signalExisting pokes the running instance's focus event: open by name
// with retry (the winner creates the event a heartbeat after the mutex, so
// an immediate open can lose that race), set, close. False means the event
// stayed unreachable — the caller still exits (never a double launch) but
// knows nothing was raised.
func signalExisting(eventName string) bool {
	ePtr, err := syscall.UTF16PtrFromString(eventName)
	if err != nil {
		return false
	}
	deadline := time.Now().Add(signalRetrySpan)
	for {
		eh, _, _ := pOpenEventW.Call(eventModifyState, 0, uintptr(unsafe.Pointer(ePtr)))
		if eh != 0 {
			pSetEvent.Call(eh)
			syscall.CloseHandle(syscall.Handle(eh))
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(signalRetryStep)
	}
}
