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

// Store mode: legacy profiles seed sources once, S3 sources mirror into the
// profile store, masked secrets survive editor round-trips, removal takes
// the mirror with it.
func TestSourceStoreModeLifecycle(t *testing.T) {
	a := newTestApp(t)

	if err := a.SaveProfile(ProfileInput{Name: "lab", AccessKeyID: "AKIA1", SecretKey: "s3secret"}); err != nil {
		t.Fatalf("save legacy profile: %v", err)
	}
	srcs, err := a.ListSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 1 || srcs[0].Type != profile.TypeS3 || srcs[0].Name != "lab" {
		t.Fatalf("seeding failed: %+v", srcs)
	}
	if srcs[0].S3 == nil || srcs[0].S3.SecretKey == "s3secret" {
		t.Fatalf("list must mask embedded secrets: %+v", srcs[0].S3)
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
	}

	// Editor round-trip with masked password keeps the stored value
	// (updates carry the source ID, exactly like the editor does).
	st, _ := a.loadStore()
	box, err := st.GetSource("box")
	if err != nil {
		t.Fatal(err)
	}
	box.Password = "pw…23"
	if err := a.SaveSource(box); err != nil {
		t.Fatalf("masked sftp save: %v", err)
	}
	st, _ = a.loadStore()
	got, err := st.GetSource("box")
	if err != nil || got.Password != "pw123" {
		t.Fatalf("masked round-trip lost sftp password: %+v (%v)", got, err)
	}

	// S3 source round-trip with masked secret keeps the mirror's secret.
	if err := a.SaveSource(s3Source("extra", "topsecret")); err != nil {
		t.Fatalf("save s3 source: %v", err)
	}
	st, _ = a.loadStore()
	extra, err := st.GetSource("extra")
	if err != nil {
		t.Fatal(err)
	}
	extra.S3.SecretKey = "to…et" // masked secret from the editor
	if err := a.SaveSource(extra); err != nil {
		t.Fatalf("masked s3 save: %v", err)
	}
	st, _ = a.loadStore()
	p, err := st.Get("extra")
	if err != nil || p.SecretKey != "topsecret" {
		t.Fatalf("masked round-trip lost s3 secret: %+v (%v)", p, err)
	}
	src, err := st.GetSource("extra")
	if err != nil || src.S3.SecretKey != "topsecret" {
		t.Fatalf("source copy lost s3 secret: %+v (%v)", src, err)
	}

	// Removing an s3 source removes its mirrored profile too.
	if err := a.RemoveSource("extra"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := a.loadStore(); err != nil {
		t.Fatal(err)
	}
	st, _ = a.loadStore()
	if _, err := st.GetSource("extra"); err == nil {
		t.Fatal("source still present after remove")
	}
	if _, err := st.Get("extra"); err == nil {
		t.Fatal("mirrored profile must be removed with its source")
	}

	// Duplicate names are rejected across types.
	if err := a.SaveSource(profile.Source{Name: "lab", Type: profile.TypeLocal, LocalRoot: "/x"}); err == nil {
		t.Fatal("duplicate source name must be rejected")
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
	stS, _ := a.loadStore()
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
