package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

func s3Source(name, secret string) profile.Source {
	return profile.Source{
		Name: name,
		Type: profile.TypeS3,
		S3: &profile.Profile{
			Name:        name,
			Endpoint:    "https://s3.example.com",
			AccessKeyID: "AKIAEXAMPLE",
			SecretKey:   secret,
		},
	}
}

// Session mode (no open Profile file): sources live in memory only,
// masked in listings, secrets survive editor round-trips, nothing is ever
// written to the CLI's store, and close discards after a guard.
func TestSourceSessionLifecycle(t *testing.T) {
	a := newTestApp(t)

	// A fresh app lists nothing — the store is never seeded anymore.
	srcs, err := a.ListSources()
	if err != nil || len(srcs) != 0 {
		t.Fatalf("fresh app must list no sources: %+v (%v)", srcs, err)
	}

	if err := a.SaveSource(s3Source("lab", "s3secret")); err != nil {
		t.Fatalf("save s3 source: %v", err)
	}
	// Add an sftp source with a password.
	if err := a.SaveSource(profile.Source{Name: "box", Type: profile.TypeSFTP, Host: "h", Username: "u", Password: "pw123"}); err != nil {
		t.Fatalf("save sftp source: %v", err)
	}
	srcs, _ = a.ListSources()
	if len(srcs) != 2 {
		t.Fatalf("want 2 sources, got %d", len(srcs))
	}
	for _, s := range srcs {
		if s.Password == "pw123" {
			t.Fatal("sftp password must be masked in listings")
		}
		if s.Name == "lab" && (s.S3 == nil || s.S3.SecretKey == "s3secret") {
			t.Fatalf("list must mask embedded secrets: %+v", s.S3)
		}
	}

	// Editor round-trip with masked password keeps the stored value
	// (updates carry the source ID, exactly like the editor does).
	full, err := a.sourceByIDOrName("box")
	if err != nil || full.Password != "pw123" {
		t.Fatalf("sourceByIDOrName must return real secrets: %+v (%v)", full, err)
	}
	full.Password = "pw…23"
	if err := a.SaveSource(full); err != nil {
		t.Fatalf("masked sftp save: %v", err)
	}
	if got, err := a.sourceByIDOrName("box"); err != nil || got.Password != "pw123" {
		t.Fatalf("masked round-trip lost sftp password: %+v (%v)", got, err)
	}

	// S3 source round-trip with masked secret keeps the secret.
	s3full, err := a.sourceByIDOrName("lab")
	if err != nil {
		t.Fatal(err)
	}
	s3full.S3.SecretKey = "s3…et" // masked secret from the editor
	if err := a.SaveSource(s3full); err != nil {
		t.Fatalf("masked s3 save: %v", err)
	}
	if got, err := a.sourceByIDOrName("lab"); err != nil || got.S3.SecretKey != "s3secret" {
		t.Fatalf("masked round-trip lost s3 secret: %+v (%v)", got, err)
	}

	// Session resolution feeds client() lookups (default/single s3 source).
	if src, ok := a.sessionS3Source("lab"); !ok || src.S3 == nil || src.S3.SecretKey != "s3secret" {
		t.Fatalf("sessionS3Source: %+v ok=%v", src, ok)
	}
	if _, ok := a.sessionS3Source("box"); ok {
		t.Fatal("non-s3 sources must not resolve as clients")
	}

	// The CLI's store never sees any of it.
	stS, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stS.Sources) != 0 || len(stS.Profiles) != 0 {
		t.Fatalf("session sources leaked into the store: %+v", stS)
	}

	// Removal works.
	if err := a.RemoveSource("lab"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := a.sourceByIDOrName("lab"); err == nil {
		t.Fatal("source still present after remove")
	}

	// Duplicate names are rejected across types.
	if err := a.SaveSource(profile.Source{Name: "box", Type: profile.TypeLocal, LocalRoot: "/x"}); err == nil {
		t.Fatal("duplicate source name must be rejected")
	}

	// Closing the session discards everything (strict model).
	if st := a.GetProfileFileState(); st.Open || st.SourceCount != 1 {
		t.Fatalf("state with session sources: %+v", st)
	}
	if err := a.CloseProfileFile(false); err == nil {
		t.Fatal("close with unsaved session sources must fail without force")
	}
	if err := a.CloseProfileFile(true); err != nil {
		t.Fatalf("force close: %v", err)
	}
	if srcs, _ := a.ListSources(); len(srcs) != 0 {
		t.Fatalf("session must be empty after close: %+v", srcs)
	}
}

// Profile file session: new → save-as → edit → save → close → reopen, and
// the store never sees container edits (no local trace).
func TestProfileFileSession(t *testing.T) {
	a := newTestApp(t)
	path := filepath.Join(t.TempDir(), "bundle.s3bprofile")

	if err := a.NewProfileFile("bundle", ""); err == nil {
		t.Fatal("empty password must be rejected")
	}
	if err := a.NewProfileFile("bundle", "pw1"); err != nil {
		t.Fatalf("new: %v", err)
	}
	st := a.GetProfileFileState()
	if !st.Open || st.Name != "bundle" || st.Path != "" || st.Dirty {
		t.Fatalf("state after new: %+v", st)
	}
	if err := a.SaveProfileFile(); err == nil {
		t.Fatal("save without a path must fail (Save As first)")
	}

	// Container edits.
	if err := a.SaveSource(s3Source("cloud", "cloudsecret")); err != nil {
		t.Fatalf("save source into container: %v", err)
	}
	if st := a.GetProfileFileState(); !st.Dirty || st.SourceCount != 1 {
		t.Fatalf("state after edit: %+v", st)
	}
	if err := a.CloseProfileFile(false); err == nil {
		t.Fatal("dirty close must fail without force")
	}
	if err := a.SaveProfileFileAs(path, ""); err != nil {
		t.Fatalf("save as: %v", err)
	}
	if st := a.GetProfileFileState(); st.Dirty || st.Path != path {
		t.Fatalf("state after save as: %+v", st)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(data), "s3bpf1|") {
		t.Fatalf("container file missing/wrong format: %v", err)
	}

	// Second edit + Save to the existing path.
	if err := a.SaveSource(profile.Source{Name: "box", Type: profile.TypeSFTP, Host: "h", Password: "sftppw"}); err != nil {
		t.Fatalf("second source: %v", err)
	}
	if err := a.SaveProfileFile(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// The local store never saw any of it.
	stS, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stS.Sources) != 0 || len(stS.Profiles) != 0 {
		t.Fatalf("container edits leaked into the store: %+v", stS)
	}

	// Close (force not needed after save) and reopen.
	if err := a.CloseProfileFile(false); err != nil {
		t.Fatalf("close after save: %v", err)
	}
	if st := a.GetProfileFileState(); st.Open {
		t.Fatalf("state after close: %+v", st)
	}
	if err := a.OpenProfileFile(path, "nope"); !errors.Is(err, profile.ErrWrongPassword) {
		t.Fatalf("wrong password err = %v, want ErrWrongPassword", err)
	}
	if err := a.OpenProfileFile(path, "pw1"); err != nil {
		t.Fatalf("open: %v", err)
	}
	st = a.GetProfileFileState()
	if !st.Open || st.Name != "bundle" || st.SourceCount != 2 {
		t.Fatalf("state after reopen: %+v", st)
	}

	// Reopened sources carry their secrets and mask them in listings.
	srcs, err := a.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 {
		t.Fatalf("want 2 sources, got %d", len(srcs))
	}
	for _, s := range srcs {
		if s.S3 != nil && s.S3.SecretKey == "cloudsecret" {
			t.Fatal("container listing must mask s3 secrets")
		}
		if s.Password == "sftppw" {
			t.Fatal("container listing must mask passwords")
		}
	}

	// Container wins over the store for client resolution (no dialing).
	src, ok := a.containerS3Source("cloud")
	if !ok || src.S3 == nil || src.S3.SecretKey != "cloudsecret" {
		t.Fatalf("containerS3Source: %+v ok=%v", src, ok)
	}
	if _, ok := a.containerS3Source("box"); ok {
		t.Fatal("non-s3 sources must not resolve as clients")
	}

	if err := a.CloseProfileFile(true); err != nil {
		t.Fatalf("force close: %v", err)
	}
}

// The save flow the strict model enables: sources added without a Profile
// file become one via Save As — no "New Profile file" prerequisite.
func TestSessionSaveFlow(t *testing.T) {
	a := newTestApp(t)
	path := filepath.Join(t.TempDir(), "late.s3bprofile")

	// With an empty workspace both save paths refuse.
	if err := a.SaveProfileFile(); !errors.Is(err, errNoProfileFile) {
		t.Fatalf("save with empty workspace err = %v, want errNoProfileFile", err)
	}
	if err := a.SaveProfileFileAs(path, "pw"); !errors.Is(err, errNoProfileFile) {
		t.Fatalf("save-as with empty workspace err = %v, want errNoProfileFile", err)
	}

	if err := a.SaveSource(s3Source("cloud", "cloudsecret")); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSource(profile.Source{Name: "box", Type: profile.TypeSFTP, Host: "h", Password: "sftppw"}); err != nil {
		t.Fatal(err)
	}
	st := a.GetProfileFileState()
	if st.Open || st.SourceCount != 2 {
		t.Fatalf("state with session sources: %+v", st)
	}

	// Plain Save without an open file still errors; the frontend routes
	// to Save As.
	if err := a.SaveProfileFile(); !errors.Is(err, errNoProfileFile) {
		t.Fatalf("save without session err = %v, want errNoProfileFile", err)
	}

	// New/Open are blocked by the unsaved session (guard against loss).
	if err := a.NewProfileFile("x", "pw"); err == nil {
		t.Fatal("new over unsaved session must fail")
	}
	if err := a.OpenProfileFile("whatever", "pw"); err == nil {
		t.Fatal("open over unsaved session must fail")
	}

	// Save As adopts the session sources.
	if err := a.SaveProfileFileAs(path, "pw2"); err != nil {
		t.Fatalf("save as from session: %v", err)
	}
	st = a.GetProfileFileState()
	if !st.Open || st.Path != path || st.Dirty || st.SourceCount != 2 || st.Name != "late" {
		t.Fatalf("state after save as: %+v", st)
	}

	// The saved file reopens with secrets intact and the session is gone.
	if err := a.CloseProfileFile(false); err != nil {
		t.Fatalf("close: %v", err)
	}
	if st := a.GetProfileFileState(); st.Open || st.SourceCount != 0 {
		t.Fatalf("session must be gone after close: %+v", st)
	}
	if err := a.OpenProfileFile(path, "pw2"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	src, ok := a.containerS3Source("cloud")
	if !ok || src.S3 == nil || src.S3.SecretKey != "cloudsecret" {
		t.Fatalf("reopened source lost its secret: %+v", src)
	}
	if err := a.CloseProfileFile(true); err != nil {
		t.Fatal(err)
	}
}

// A dirty open file blocks New/Open until saved or force-closed.
func TestProfileFileDirtyGuard(t *testing.T) {
	a := newTestApp(t)
	if err := a.NewProfileFile("x", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSource(profile.Source{Name: "l", Type: profile.TypeLocal, LocalRoot: "/tmp"}); err != nil {
		t.Fatal(err)
	}
	if err := a.NewProfileFile("y", "pw"); err == nil {
		t.Fatal("new over a dirty file must fail")
	}
	if err := a.OpenProfileFile("whatever", "pw"); err == nil {
		t.Fatal("open over a dirty file must fail")
	}
	if err := a.CloseProfileFile(true); err != nil {
		t.Fatal(err)
	}
	if err := a.NewProfileFile("y", "pw"); err != nil {
		t.Fatalf("new after force close: %v", err)
	}
}

// containerS3Source("") resolves the default (or single) s3 source — the
// container-mode equivalent of DefaultProfile.
func TestContainerDefaultResolution(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	if err := a.NewProfileFile("d", "pw"); err != nil {
		t.Fatal(err)
	}
	defer a.CloseProfileFile(true)

	if _, ok := a.containerS3Source(""); ok {
		t.Fatal("no s3 sources yet — default must not resolve")
	}
	one := s3Source("one", "s1")
	one.Default = true
	if err := a.SaveSource(one); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSource(s3Source("two", "s2")); err != nil {
		t.Fatal(err)
	}
	src, ok := a.containerS3Source("")
	if !ok || src.Name != "one" {
		t.Fatalf("default resolution: %+v ok=%v", src, ok)
	}
}
