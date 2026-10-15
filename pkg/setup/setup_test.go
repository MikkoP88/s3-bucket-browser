package setup

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeLocator is the whole OS surface as an in-memory fake: two
// directories, an elevation verdict, a registration map that honors the
// pointing-at law, and no machine state ever touched.
type fakeLocator struct {
	user, system string
	exeName      string
	maySystem    bool
	removeDir    map[Scope]bool
	regErr       error

	registered map[Scope]string
}

func newFake(t *testing.T) *fakeLocator {
	t.Helper()
	base := t.TempDir()
	f := &fakeLocator{
		user:    filepath.Join(base, "user"),
		system:  filepath.Join(base, "system"),
		exeName: "s3b",
		removeDir: map[Scope]bool{
			User:   true, // windows law: dedicated dirs are ours
			System: false,
		},
		registered: map[Scope]string{},
	}
	t.Cleanup(func() {})
	return f
}

func (f *fakeLocator) ExeName() string               { return f.exeName }
func (f *fakeLocator) UserDir() (string, error)      { return f.user, nil }
func (f *fakeLocator) SystemDir() (string, error)    { return f.system, nil }
func (f *fakeLocator) MaySystem() bool               { return f.maySystem }
func (f *fakeLocator) SystemRemedy() string          { return "rerun as the administrator of this fake" }
func (f *fakeLocator) MayRemoveDir(sc Scope) bool    { return f.removeDir[sc] }
func (f *fakeLocator) RegisteredApp(sc Scope) string { return f.registered[sc] }
func (f *fakeLocator) PATHAdvice(dir string) string  { return "PATH advice for " + dir }

func (f *fakeLocator) RegisterApp(exe string, sc Scope) error {
	if f.regErr != nil {
		return f.regErr
	}
	f.registered[sc] = exe
	return nil
}

func (f *fakeLocator) UnregisterApp(exe string, sc Scope) (string, error) {
	cur := f.registered[sc]
	if cur != exe {
		return cur, nil // another install's entry: left in place, named
	}
	delete(f.registered, sc)
	return "", nil
}

// srcExe stages a stand-in source binary (the running test binary
// works, but a small staged file keeps every read local to the test).
func srcExe(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s3b-src")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseScope(t *testing.T) {
	for s, want := range map[string]Scope{"user": User, "system": System} {
		got, err := ParseScope(s)
		if err != nil || got != want {
			t.Errorf("ParseScope(%q) = %v, %v; want %v", s, got, err, want)
		}
	}
	if _, err := ParseScope("everyone"); err == nil {
		t.Error("ParseScope accepted an unknown scope")
	}
}

func TestInstall_CreatesSeatAndRegisters(t *testing.T) {
	f := newFake(t)
	src := srcExe(t)
	o := &Options{Locator: f, Exe: src}

	seat, err := Install(o, User)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	dst := filepath.Join(f.user, f.exeName)
	if seat.Exe != dst || !seat.Installed {
		t.Fatalf("seat = %+v, want exe at %s installed", seat, dst)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("installed exe unreadable: %v", err)
	}
	want, _ := os.ReadFile(src)
	if string(got) != string(want) {
		t.Error("installed exe bytes differ from the source binary")
	}
	// The executable bit is a unix law; Windows modes never carry it.
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(dst); err == nil && fi.Mode()&0o111 == 0 {
			t.Error("installed exe is not executable")
		}
	}
	if f.registered[User] != dst {
		t.Errorf("registration = %q, want the installed exe", f.registered[User])
	}
}

func TestInstall_RedirectSkipsRegistration(t *testing.T) {
	f := newFake(t)
	o := &Options{Locator: f, Exe: srcExe(t), Redirect: filepath.Join(t.TempDir(), "portable")}

	seat, err := Install(o, User)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if want := filepath.Join(o.Redirect, f.exeName); seat.Exe != want {
		t.Errorf("seat exe = %q, want %q", seat.Exe, want)
	}
	if seat.Dir != o.Redirect {
		t.Errorf("seat dir = %q, want the redirect", seat.Dir)
	}
	if _, ok := f.registered[User]; ok {
		t.Error("redirected install still registered with the shell")
	}
}

func TestInstall_ReplacesInPlace(t *testing.T) {
	f := newFake(t)
	o := &Options{Locator: f, Exe: srcExe(t)}
	if _, err := Install(o, User); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// An upgrade lands over the old binary — same dance, new bytes.
	upgraded := srcExe(t)
	if err := os.WriteFile(upgraded, []byte("upgraded binary bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	o.Exe = upgraded
	if _, err := Install(o, User); err != nil {
		t.Fatalf("upgrade install: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(f.user, f.exeName))
	if string(got) != "upgraded binary bytes" {
		t.Errorf("exe after upgrade = %q, want the upgraded bytes", got)
	}
	// No staged .new litter left behind.
	entries, _ := os.ReadDir(f.user)
	for _, e := range entries {
		if e.Name() == f.exeName+".new" {
			t.Error("staged .new file survived the upgrade")
		}
	}
}

func TestInstall_SameBinaryIsNoop(t *testing.T) {
	f := newFake(t)
	o := &Options{Locator: f, Exe: srcExe(t)}
	if _, err := Install(o, User); err != nil {
		t.Fatalf("install: %v", err)
	}
	dst := filepath.Join(f.user, f.exeName)

	// Re-running the INSTALLED binary: the source is the destination.
	o.Exe = dst
	seat, err := Install(o, User)
	if !errors.Is(err, ErrSameBinary) {
		t.Fatalf("err = %v, want ErrSameBinary", err)
	}
	if !seat.Installed || seat.Exe != dst {
		t.Errorf("seat = %+v, want the existing seat named", seat)
	}
}

func TestInstall_SystemRefusedWithoutElevation(t *testing.T) {
	f := newFake(t)
	f.maySystem = false
	o := &Options{Locator: f, Exe: srcExe(t)}

	_, err := Install(o, System)
	if err == nil {
		t.Fatal("unelevated system install accepted")
	}
	if want := f.SystemRemedy(); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not carry the remedy %q", err, want)
	}
	if _, statErr := os.Stat(filepath.Join(f.system, f.exeName)); statErr == nil {
		t.Error("refused install still wrote the system seat")
	}
}

func TestInstall_RegistrationFailureKeepsSeat(t *testing.T) {
	f := newFake(t)
	f.regErr = errors.New("registry locked")
	o := &Options{Locator: f, Exe: srcExe(t)}

	seat, err := Install(o, User)
	if err == nil || !strings.Contains(err.Error(), "installed at") {
		t.Fatalf("err = %v, want the partial-state error", err)
	}
	if !seat.Installed {
		t.Error("seat not reported despite the binary landing on disk")
	}
	if _, statErr := os.Stat(filepath.Join(f.user, f.exeName)); statErr != nil {
		t.Error("binary missing despite the copy succeeding")
	}
}

func TestUninstall_RemovesOwnSeatAndDir(t *testing.T) {
	f := newFake(t)
	o := &Options{Locator: f, Exe: srcExe(t)}
	if _, err := Install(o, User); err != nil {
		t.Fatalf("install: %v", err)
	}

	rep, err := Uninstall(o, User)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if rep.RemovedExe != filepath.Join(f.user, f.exeName) || !rep.RemovedDir {
		t.Errorf("report = %+v, want the exe and the owned dir removed", rep)
	}
	if _, statErr := os.Stat(f.user); statErr == nil {
		t.Error("owned empty directory survived uninstall")
	}
	if _, ok := f.registered[User]; ok {
		t.Error("shell registration survived uninstall")
	}
}

func TestUninstall_SharedDirSurvives(t *testing.T) {
	f := newFake(t)
	f.removeDir[User] = false // the unix law: ~/.local/bin is shared
	o := &Options{Locator: f, Exe: srcExe(t)}
	if _, err := Install(o, User); err != nil {
		t.Fatalf("install: %v", err)
	}

	rep, err := Uninstall(o, User)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if rep.RemovedDir {
		t.Error("shared install dir was removed")
	}
	if _, statErr := os.Stat(f.user); statErr != nil {
		t.Error("shared install dir was removed from disk")
	}
}

func TestUninstall_ForeignFileKeepsOwnedDir(t *testing.T) {
	f := newFake(t)
	o := &Options{Locator: f, Exe: srcExe(t)}
	if _, err := Install(o, User); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f.user, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Uninstall(o, User)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if rep.RemovedDir {
		t.Error("directory with a foreign file was removed")
	}
	if _, statErr := os.Stat(filepath.Join(f.user, "stray.txt")); statErr != nil {
		t.Error("foreign file did not survive the uninstall")
	}
}

func TestUninstall_ForeignRegistrationLeftNamed(t *testing.T) {
	f := newFake(t)
	o := &Options{Locator: f, Exe: srcExe(t)}
	if _, err := Install(o, User); err != nil {
		t.Fatalf("install: %v", err)
	}
	// The setup wizard re-registered the same App Paths entry at its own
	// install: the CLI uninstall must leave it alone and name it.
	wizard := `C:\Users\demo\AppData\Local\S3 Bucket Browser\s3b.exe`
	f.registered[User] = wizard

	rep, err := Uninstall(o, User)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if rep.RegisteredLeft != wizard {
		t.Errorf("RegisteredLeft = %q, want the wizard path", rep.RegisteredLeft)
	}
	if f.registered[User] != wizard {
		t.Error("foreign registration was deleted anyway")
	}
}

func TestUninstall_NotInstalledIsHonest(t *testing.T) {
	f := newFake(t)
	o := &Options{Locator: f, Exe: srcExe(t)}

	_, err := Uninstall(o, User)
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("err = %v, want ErrNotInstalled", err)
	}

	// A non-elevated user asking about a system scope with nothing
	// installed still learns there is nothing to remove — not a demand
	// for elevation.
	f.maySystem = false
	_, err = Uninstall(o, System)
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("err = %v, want ErrNotInstalled before the elevation check", err)
	}
}

func TestUninstall_SystemRefusedWhenInstalled(t *testing.T) {
	f := newFake(t)
	f.maySystem = true
	o := &Options{Locator: f, Exe: srcExe(t)}
	if _, err := Install(o, System); err != nil {
		t.Fatalf("install: %v", err)
	}

	f.maySystem = false // a later, unelevated uninstall attempt
	_, err := Uninstall(o, System)
	if err == nil || !strings.Contains(err.Error(), f.SystemRemedy()) {
		t.Fatalf("err = %v, want the elevation refusal with its remedy", err)
	}
}

func TestProbe_ReportsBothSeatsAndPath(t *testing.T) {
	f := newFake(t)
	if runtime.GOOS == "windows" {
		f.exeName = "s3b.exe" // LookPath needs the OS's real exe grammar
	}
	o := &Options{Locator: f, Exe: srcExe(t)}

	st, err := Probe(o)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if st.User.Installed || st.System.Installed {
		t.Error("empty fake reported an install")
	}
	if st.OnPath != "" {
		t.Errorf("OnPath = %q on an empty machine", st.OnPath)
	}

	if _, err := Install(o, User); err != nil {
		t.Fatalf("install: %v", err)
	}
	st, err = Probe(o)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !st.User.Installed || st.System.Installed {
		t.Errorf("seats = %+v, want exactly the user seat installed", st)
	}
	if st.User.Registered != st.User.Exe {
		t.Errorf("registered = %q, want the installed exe", st.User.Registered)
	}

	// PATH resolution: put the seat's dir at the head of PATH and the
	// probe must name the user scope.
	old := os.Getenv("PATH")
	os.Setenv("PATH", st.User.Dir+string(os.PathListSeparator)+old)
	defer os.Setenv("PATH", old)
	st, err = Probe(o)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if st.OnPath != User {
		t.Errorf("OnPath = %q, want user with the seat first on PATH", st.OnPath)
	}
}

func TestProbe_ReportsForeignRegistration(t *testing.T) {
	f := newFake(t)
	wizard := `C:\Program Files\S3 Bucket Browser\s3b.exe`
	f.registered[System] = wizard

	st, err := Probe(&Options{Locator: f, Exe: srcExe(t)})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if st.System.Registered != wizard {
		t.Errorf("System.Registered = %q, want the wizard install named", st.System.Registered)
	}
	if st.System.Installed {
		t.Error("foreign registration read as a CLI install on disk")
	}
}

func TestProbe_RedirectMarksSeat(t *testing.T) {
	f := newFake(t)
	o := &Options{Locator: f, Exe: srcExe(t), Redirect: filepath.Join(t.TempDir(), "portable")}
	if _, err := Install(o, User); err != nil {
		t.Fatalf("install: %v", err)
	}
	st, err := Probe(o)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !st.User.Redirected || st.User.Dir != o.Redirect {
		t.Errorf("user seat = %+v, want the redirected seat marked", st.User)
	}
	if st.System.Redirected {
		t.Error("system seat marked redirected")
	}
}
