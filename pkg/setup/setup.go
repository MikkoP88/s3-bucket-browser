// Package setup implements the s3b setup face: installing the running
// binary for the current user or the whole system, uninstalling by the
// same selection, and reporting where s3b currently lives. The Windows
// setup wizard (scripts/installer/s3b.nsi) maintains its own seats; this
// package is the terminal twin of the same law — and its status probe
// names the wizard's installs too (via the shell registration), without
// ever touching them.
package setup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Scope is an installation's audience.
type Scope string

const (
	// User installs under the current user's profile: no elevation, no
	// administrator approval, nothing visible to other accounts.
	User Scope = "user"
	// System installs machine-wide: every account, a privileged
	// location (Program Files on Windows, /usr/local/bin on unix).
	System Scope = "system"
)

// ErrSameBinary is returned when the destination IS the running binary:
// nothing to install, and the caller reports the seat as already current.
var ErrSameBinary = errors.New("destination is already the running binary")

// ErrNotInstalled is returned by Uninstall when the scope has no s3b on
// disk — removing nothing must not read as success.
var ErrNotInstalled = errors.New("no s3b installed for this scope")

// Locator resolves this OS's install seats and shell integration. The
// real locator lives in setup_windows.go / setup_unix.go; tests inject
// fakes so no test ever touches a real machine.
type Locator interface {
	// ExeName is the installed binary's file name ("s3b.exe" on
	// Windows, "s3b" elsewhere).
	ExeName() string
	// UserDir is the directory a user-scope install owns.
	UserDir() (string, error)
	// SystemDir is the directory a system-scope install owns.
	SystemDir() (string, error)
	// MaySystem reports whether this process may write the system seat
	// (elevation on Windows, root on unix). When false, Install and
	// Uninstall refuse before touching anything.
	MaySystem() bool
	// SystemRemedy is the one-line fix for MaySystem() == false.
	SystemRemedy() string
	// MayRemoveDir reports whether Uninstall may remove the scope's
	// directory when it ends up empty (a directory the OS shares —
	// ~/.local/bin, /usr/local/bin — must never be removed).
	MayRemoveDir(scope Scope) bool
	// RegisterApp integrates the exe with the shell (App Paths on
	// Windows; a no-op elsewhere). The scope picks the per-user or
	// machine-wide registration surface.
	RegisterApp(exe string, scope Scope) error
	// UnregisterApp removes the shell integration when it points at
	// exe. An integration naming ANOTHER install (the setup wizard's)
	// is deliberately left in place and named in the returned string.
	UnregisterApp(exe string, scope Scope) (left string, err error)
	// RegisteredApp reports the exe the shell integration names, "" when
	// none. Read-only: status probes it freely.
	RegisteredApp(scope Scope) string
	// PATHAdvice is the honest one-liner for calling s3b from a
	// terminal when the seat is not first on PATH.
	PATHAdvice(dir string) string
}

// Options tunes one setup operation.
type Options struct {
	// Locator overrides the OS default (tests).
	Locator Locator
	// Redirect relocates user-scope installs (portable machines, CI,
	// tests). While set, shell registration is skipped: an integration
	// pointing into a scratch dir would lie to the shell.
	Redirect string
	// Exe is the source binary; "" means the running one.
	Exe string
}

func (o *Options) locator() Locator {
	if o != nil && o.Locator != nil {
		return o.Locator
	}
	return defaultLocator()
}

func (o *Options) dirFor(l Locator, sc Scope) (string, error) {
	if sc == User && o != nil && o.Redirect != "" {
		// Normalize: env-supplied paths arrive in the caller's slash
		// grammar (forward slashes from POSIX shells) and the seat
		// should speak the OS's own.
		return localize(filepath.Clean(o.Redirect)), nil
	}
	if sc == User {
		return l.UserDir()
	}
	return l.SystemDir()
}

func (o *Options) src() (string, error) {
	if o != nil && o.Exe != "" {
		return o.Exe, nil
	}
	return os.Executable()
}

// Advice speaks the OS's PATH guidance for a seat directory. A portable
// (redirected) seat never registered with the shell, so the OS's
// App Paths claims do not apply to it — the advice is PATH, plainly.
func Advice(o *Options, dir string) string {
	if o != nil && o.Redirect != "" && strings.EqualFold(localize(filepath.Clean(o.Redirect)), dir) {
		return fmt.Sprintf("add %s to PATH to call s3b from any terminal", dir)
	}
	return o.locator().PATHAdvice(dir)
}

// localize converts a slash-separated path to the OS grammar, passing
// anything Localize refuses through untouched.
func localize(p string) string {
	if lp, err := filepath.Localize(p); err == nil {
		return lp
	}
	return p
}

// Seat is one scope's installation state.
type Seat struct {
	Scope Scope `json:"scope"`
	// Dir is the install directory.
	Dir string `json:"dir"`
	// Exe is the installed binary's path.
	Exe string `json:"exe"`
	// Installed reports the exe exists on disk.
	Installed bool `json:"installed"`
	// Registered names the exe the shell integration points at (Windows
	// App Paths; "" elsewhere). It can name another install — the setup
	// wizard's — and is reported, never followed.
	Registered string `json:"registered"`
	// Redirected is true when the seat lives at the redirected base.
	Redirected bool `json:"redirected"`
}

// Status is the whole machine's s3b seating.
type Status struct {
	User   Seat `json:"user"`
	System Seat `json:"system"`
	// OnPath names the scope whose exe `s3b` resolves to from PATH —
	// "" when PATH resolves to some other s3b or to nothing.
	OnPath Scope `json:"onPath"`
}

// Probe reports both scopes' seats read-only: what is on disk, what the
// shell integration names, and which seat wins PATH.
func Probe(o *Options) (Status, error) {
	l := o.locator()
	var st Status
	for i, sc := range []Scope{User, System} {
		dir, err := o.dirFor(l, sc)
		if err != nil {
			return Status{}, err
		}
		seat := Seat{Scope: sc, Dir: dir, Exe: filepath.Join(dir, l.ExeName())}
		if fi, err := os.Stat(seat.Exe); err == nil && !fi.IsDir() {
			seat.Installed = true
		}
		seat.Registered = l.RegisteredApp(sc)
		if sc == User && o != nil && o.Redirect != "" {
			seat.Redirected = true
		}
		if i == 0 {
			st.User = seat
		} else {
			st.System = seat
		}
	}
	st.OnPath = onPathScope(l, st)
	return st, nil
}

// onPathScope says which seat the shell would run for a bare `s3b`.
func onPathScope(l Locator, st Status) Scope {
	resolved, err := exec.LookPath(l.ExeName())
	if err != nil {
		return ""
	}
	for _, seat := range []Seat{st.User, st.System} {
		if !seat.Installed {
			continue
		}
		if same, err := sameFile(resolved, seat.Exe); err == nil && same {
			return seat.Scope
		}
	}
	return ""
}

// Install copies the running binary into the scope's seat and integrates
// it with the shell. An existing binary is replaced in place — a locked
// destination (a running s3b) is refused with its remedy, never half
// written. Installing onto the running binary itself returns the seat
// and ErrSameBinary.
func Install(o *Options, sc Scope) (Seat, error) {
	l := o.locator()
	if sc == System && !l.MaySystem() {
		return Seat{}, fmt.Errorf("system scope needs elevated permissions: %s", l.SystemRemedy())
	}
	dir, err := o.dirFor(l, sc)
	if err != nil {
		return Seat{}, err
	}
	src, err := o.src()
	if err != nil {
		return Seat{}, err
	}
	dst := filepath.Join(dir, l.ExeName())

	if same, _ := sameFile(src, dst); same {
		return Seat{Scope: sc, Dir: dir, Exe: dst, Installed: true}, ErrSameBinary
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Seat{}, fmt.Errorf("cannot create install dir %s: %w", dir, err)
	}
	// Stage-then-rename: the destination either stays wholly the old
	// binary or becomes wholly the new one, and a rename onto a running
	// exe fails loudly instead of truncating it mid-flight.
	tmp := dst + ".new"
	if err := copyExecutable(src, tmp); err != nil {
		os.Remove(tmp)
		return Seat{}, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return Seat{}, fmt.Errorf("could not replace %s: %w — close any running s3b and retry", dst, err)
	}

	seat := Seat{Scope: sc, Dir: dir, Exe: dst, Installed: true}
	if sc == User && o != nil && o.Redirect != "" {
		return seat, nil // portable seat: no shell integration
	}
	if err := l.RegisterApp(dst, sc); err != nil {
		// The binary IS on disk; the registration failing must not read
		// as a failed install — say both truths in one error.
		return seat, fmt.Errorf("installed at %s, but shell registration failed: %w", dst, err)
	}
	return seat, nil
}

// copyExecutable stages src as a fresh executable at dst.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("cannot read source binary %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("cannot stage %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("copying %s failed: %w", src, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("writing %s failed: %w", dst, err)
	}
	// Umask may have trimmed the create mode; the executable bit is the
	// whole point of the file.
	if err := os.Chmod(dst, 0o755); err != nil {
		os.Remove(dst)
		return fmt.Errorf("cannot mark %s executable: %w", dst, err)
	}
	return nil
}

// UninstallReport tells what Uninstall actually did.
type UninstallReport struct {
	Scope Scope  `json:"scope"`
	Dir   string `json:"dir"`
	// RemovedExe is the binary that was deleted.
	RemovedExe string `json:"removedExe"`
	// RemovedDir is true when the scope's own directory was removed
	// (it was ours and ended up empty — shared dirs are never touched).
	RemovedDir bool `json:"removedDir"`
	// RegisteredLeft names a shell integration that still points at
	// another install and was deliberately left in place ("" when none).
	RegisteredLeft string `json:"registeredLeft"`
}

// Uninstall removes the scope's s3b. The uninstall follows the install
// selection — the same --user/--system pair that put it there takes it
// back out, and an integration naming another install is left alone and
// named in the report.
func Uninstall(o *Options, sc Scope) (UninstallReport, error) {
	l := o.locator()
	dir, err := o.dirFor(l, sc)
	if err != nil {
		return UninstallReport{}, err
	}
	exe := filepath.Join(dir, l.ExeName())
	if _, err := os.Stat(exe); err != nil {
		// The honest verdict when nothing is there — even before the
		// privilege check: a user without rights still deserves to know
		// there is nothing to remove.
		return UninstallReport{}, fmt.Errorf("%w (%s)", ErrNotInstalled, sc)
	}
	if sc == System && !l.MaySystem() {
		return UninstallReport{}, fmt.Errorf("system scope needs elevated permissions: %s", l.SystemRemedy())
	}

	rep := UninstallReport{Scope: sc, Dir: dir, RemovedExe: exe}
	if err := os.Remove(exe); err != nil {
		return rep, fmt.Errorf("could not remove %s: %w — close any running s3b and retry", exe, err)
	}
	left, err := l.UnregisterApp(exe, sc)
	if err != nil {
		return rep, fmt.Errorf("removed %s, but shell registration failed: %w", exe, err)
	}
	rep.RegisteredLeft = left
	if l.MayRemoveDir(sc) {
		// Remove only succeeds when the directory is empty — the same
		// law the setup wizard's uninstaller follows, so anything the
		// user parked next to the binary survives.
		if err := os.Remove(dir); err == nil {
			rep.RemovedDir = true
		}
	}
	return rep, nil
}

func sameFile(a, b string) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ia, ib), nil
}
