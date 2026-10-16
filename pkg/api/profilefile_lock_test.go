package api

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/lockfile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// writeContainer seals one source into a fresh *.s3bprofile beside the test.
func writeContainer(t *testing.T, name, password string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".s3bprofile")
	srcs := []profile.Source{{Name: name, Type: profile.TypeLocal, LocalRoot: t.TempDir()}}
	data, err := profile.EncryptContainer(name, srcs, password)
	if err != nil {
		t.Fatalf("EncryptContainer: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing container: %v", err)
	}
	return path
}

func heldElsewhere(t *testing.T, path string) bool {
	t.Helper()
	release, err := lockfile.TryLock(path + ".lock")
	if errors.Is(err, lockfile.ErrLocked) {
		return true
	}
	if err != nil {
		t.Fatalf("probe TryLock on %s: %v", path, err)
	}
	release()
	return false
}

func TestOpenProfileFileCrossWindowLock(t *testing.T) {
	a := newTestApp(t)
	aPath := writeContainer(t, "team-a", "pw")
	bPath := writeContainer(t, "team-b", "pw")

	if err := a.OpenProfileFile(aPath, "pw"); err != nil {
		t.Fatalf("open A: %v", err)
	}
	if !heldElsewhere(t, aPath) {
		t.Fatal("an open container must hold its lock against every other taker")
	}

	// The same window reopening its own file is a replace, not a conflict.
	if err := a.OpenProfileFile(aPath, "pw"); err != nil {
		t.Fatalf("same-window reopen: %v", err)
	}

	// Opening a different file re-paths the session: A's lock must go.
	if err := a.OpenProfileFile(bPath, "pw"); err != nil {
		t.Fatalf("open B over clean A: %v", err)
	}
	if heldElsewhere(t, aPath) {
		t.Fatal("the replaced file's lock must release on re-path")
	}
	if !heldElsewhere(t, bPath) {
		t.Fatal("the newly opened file must be locked")
	}

	if err := a.CloseProfileFile(true); err != nil {
		t.Fatalf("close: %v", err)
	}
	if heldElsewhere(t, bPath) {
		t.Fatal("close must release the container lock")
	}
}

func TestOpenProfileFileRefusesExternallyHeldContainer(t *testing.T) {
	a := newTestApp(t)
	path := writeContainer(t, "held", "pw")

	release, err := lockfile.TryLock(path + ".lock")
	if err != nil {
		t.Fatalf("external holder: %v", err)
	}
	err = a.OpenProfileFile(path, "pw")
	if err == nil || !strings.Contains(err.Error(), "another s3b window") {
		t.Fatalf("open over an externally held container must name the other window, got %v", err)
	}
	if a.profileFileOpen() {
		t.Fatal("the refused open must not adopt the file")
	}
	release()

	// With the other window gone, the same open succeeds.
	if err := a.OpenProfileFile(path, "pw"); err != nil {
		t.Fatalf("open after release: %v", err)
	}
	if err := a.CloseProfileFile(true); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestOpenProfileFileWrongPasswordLeavesNoLock(t *testing.T) {
	a := newTestApp(t)
	path := writeContainer(t, "secret", "right")

	if err := a.OpenProfileFile(path, "wrong"); err == nil {
		t.Fatal("a wrong password must fail the decrypt")
	}
	if heldElsewhere(t, path) {
		t.Fatal("a failed open must leave no lock behind")
	}
}

func TestSaveProfileFileAsLocksDestination(t *testing.T) {
	a := newTestApp(t)
	aPath := writeContainer(t, "origin", "pw")
	cPath := filepath.Join(t.TempDir(), "re-pathed.s3bprofile")

	if err := a.OpenProfileFile(aPath, "pw"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := a.SaveProfileFileAs(cPath, ""); err != nil {
		t.Fatalf("save as: %v", err)
	}
	if heldElsewhere(t, aPath) {
		t.Fatal("the origin's lock must release after the re-path")
	}
	if !heldElsewhere(t, cPath) {
		t.Fatal("the save-as destination must be locked")
	}

	// Save As onto the file's own path keeps the session (and its lock).
	if err := a.SaveProfileFileAs(cPath, "pw2"); err != nil {
		t.Fatalf("same-path save as: %v", err)
	}
	if !heldElsewhere(t, cPath) {
		t.Fatal("a same-path save as must keep holding the lock")
	}

	if err := a.CloseProfileFile(true); err != nil {
		t.Fatalf("close: %v", err)
	}
	if heldElsewhere(t, cPath) {
		t.Fatal("close must release the re-pathed lock")
	}
}

func TestSaveProfileFileAsRefusesExternallyHeldDestination(t *testing.T) {
	a := newTestApp(t)
	cPath := filepath.Join(t.TempDir(), "foreign.s3bprofile")

	release, err := lockfile.TryLock(cPath + ".lock")
	if err != nil {
		t.Fatalf("external holder: %v", err)
	}
	defer release()
	err = a.SaveProfileFileAs(cPath, "pw")
	if err == nil || !strings.Contains(err.Error(), "another s3b window") {
		t.Fatalf("save as onto a foreign-held file must name the other window, got %v", err)
	}
	if _, statErr := os.Stat(cPath); !os.IsNotExist(statErr) {
		t.Fatal("the refused save as must write nothing")
	}
}
