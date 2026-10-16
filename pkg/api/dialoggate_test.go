package api

import (
	"errors"
	"testing"
)

// installBlockingShell swaps in a shell whose OpenFiles announces entry
// and blocks until release, plus one-shot answers for the other dialog
// kinds. The original shell is restored when the test ends.
func installBlockingShell(t *testing.T) (entered, release chan struct{}) {
	t.Helper()
	entered = make(chan struct{})
	release = make(chan struct{})
	prev := shell
	shell = &DesktopShell{
		NativeWindows: true,
		OpenFiles: func(d ShellDialog) ([]string, error) {
			entered <- struct{}{}
			<-release
			return []string{"picked.bin"}, nil
		},
		OpenFile: func(d ShellDialog) (string, error) { return "open.profile", nil },
		SaveFile: func(d ShellDialog) (string, error) { return "save.profile", nil },
		OpenDir:  func(title string) (string, error) { return `C:\somewhere`, nil },
	}
	t.Cleanup(func() { shell = prev })
	return entered, release
}

func TestDialogGateRefusesSecondPicker(t *testing.T) {
	a := newTestApp(t)
	entered, release := installBlockingShell(t)

	done := make(chan error, 1)
	go func() {
		_, err := a.PickUploadFiles()
		done <- err
	}()
	<-entered // the first dialog is up and blocking

	// The one-dialog law: every picker kind refuses while one is up.
	if _, err := a.PickUploadFiles(); !errors.Is(err, ErrDialogOpen) {
		t.Errorf("second PickUploadFiles: got %v, want ErrDialogOpen", err)
	}
	if _, err := a.PickFolder("Choose a folder"); !errors.Is(err, ErrDialogOpen) {
		t.Errorf("PickFolder during an open dialog: got %v, want ErrDialogOpen", err)
	}
	if _, err := a.PickOpenProfileFile(); !errors.Is(err, ErrDialogOpen) {
		t.Errorf("PickOpenProfileFile during an open dialog: got %v, want ErrDialogOpen", err)
	}
	if _, err := a.PickSaveProfileFile("team"); !errors.Is(err, ErrDialogOpen) {
		t.Errorf("PickSaveProfileFile during an open dialog: got %v, want ErrDialogOpen", err)
	}
	if _, err := a.PickCredentialFiles(); !errors.Is(err, ErrDialogOpen) {
		t.Errorf("PickCredentialFiles during an open dialog: got %v, want ErrDialogOpen", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first picker after unblock: %v", err)
	}

	// The gate drops the moment the dialog closes — a later picker passes.
	if _, err := a.PickFolder("Choose a folder"); err != nil {
		t.Errorf("PickFolder after the dialog closed: %v", err)
	}
}

func TestDialogGatePassesErrorsThroughAndReopens(t *testing.T) {
	a := newTestApp(t)
	prev := shell
	shell = &DesktopShell{
		NativeWindows: true,
		OpenFiles: func(d ShellDialog) ([]string, error) {
			return nil, errors.New("user cancelled")
		},
		OpenDir: func(title string) (string, error) { return "dir", nil },
	}
	defer func() { shell = prev }()

	if _, err := a.PickUploadFiles(); err == nil || errors.Is(err, ErrDialogOpen) {
		t.Fatalf("a cancelled dialog must surface its own error, got %v", err)
	}
	// ...and must not leave the gate wedged.
	if _, err := a.PickFolder("Choose a folder"); err != nil {
		t.Fatalf("gate wedged after an erroring dialog: %v", err)
	}
}

func TestDialogGateSeamsBypassIt(t *testing.T) {
	a := newTestApp(t)
	if shell != nil {
		t.Fatal("no shell in tests: the seams must sit ahead of both the gate and the shell check")
	}
	calls := 0
	SetPickers(func() ([]string, error) { calls++; return []string{"seam"}, nil }, func(string) (string, error) { return "seamdir", nil })
	defer SetPickers(nil, nil)

	got, err := a.PickUploadFiles()
	if err != nil || len(got) != 1 || got[0] != "seam" {
		t.Fatalf("seam pick: %v %v", got, err)
	}
	if dialogBusy.Load() {
		t.Fatal("the seam must not engage the dialog gate")
	}
}
