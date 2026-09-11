// sources.go is the M8 API surface: data sources (connections of any type)
// and the password-encrypted Profile file (*.s3bprofile) that bundles them.
//
// Strict sources model: the workspace is either an open Profile file or a
// session-only in-memory registry. The GUI never reads or writes the CLI's
// profiles.json — sources added without an open file live only until the
// app closes (File → Save profile file as… turns them into an encrypted
// Profile file; Close discards them after a confirmation).
package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// PickOpenProfileFile opens the native file dialog for *.s3bprofile.
func (a *App) PickOpenProfileFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open profile file",
		Filters: []runtime.FileFilter{{
			DisplayName: "s3b profile files (*.s3bprofile)",
			Pattern:     "*.s3bprofile",
		}},
	})
}

// PickSaveProfileFile opens the native save dialog for *.s3bprofile.
func (a *App) PickSaveProfileFile(defaultName string) (string, error) {
	if defaultName == "" {
		defaultName = "profile.s3bprofile"
	}
	if !strings.HasSuffix(defaultName, ".s3bprofile") {
		defaultName += ".s3bprofile"
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save profile file",
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{{
			DisplayName: "s3b profile files (*.s3bprofile)",
			Pattern:     "*.s3bprofile",
		}},
	})
}

// errNoProfileFile is returned when a Profile file operation has no session.
var errNoProfileFile = errors.New("no profile file is open")

// ProfileFileState describes the open Profile file (all zero when none).
type ProfileFileState struct {
	Open        bool   `json:"open"`
	Name        string `json:"name,omitempty"`
	Path        string `json:"path,omitempty"`
	Dirty       bool   `json:"dirty"`
	SourceCount int    `json:"sourceCount"`
}

// openProfileFile is the in-memory session over a *.s3bprofile container.
// The password stays in memory for transparent Save (the decrypted sources
// are in memory anyway while the file is open); Close wipes it.
type openProfileFile struct {
	name     string
	path     string // "" = never saved (Save As first)
	password string
	sources  []profile.Source
	dirty    bool
}

// ListSources returns the workspace's data sources, secrets masked: the
// open Profile file's, else the session-only registry's.
func (a *App) ListSources() ([]profile.Source, error) {
	srcs := a.workspaceSources()
	sort.Slice(srcs, func(i, j int) bool { return srcs[i].Name < srcs[j].Name })
	return publicSources(srcs), nil
}

func publicSources(srcs []profile.Source) []profile.Source {
	out := make([]profile.Source, len(srcs))
	for i, src := range srcs {
		out[i] = src.Public()
	}
	return out
}

// SaveSource creates or updates a data source in the workspace (matched
// by ID, else name). Masked or empty secrets inherit the stored values, so
// editor round-trips never wipe credentials.
func (a *App) SaveSource(in profile.Source) error {
	if a.profileFileOpen() {
		return a.saveSourceInContainer(in)
	}
	return a.saveSessionSource(in)
}

// saveSessionSource upserts into the session-only registry (no Profile
// file open): in-memory only, nothing touches disk.
func (a *App) saveSessionSource(in profile.Source) error {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	for i := range a.session {
		if a.session[i].ID == in.ID || (in.ID == "" && a.session[i].Name == in.Name) {
			inheritSourceSecrets(&in, a.session[i])
			break
		}
	}
	if err := profile.UpsertSourceIn(&a.session, in); err != nil {
		return err
	}
	a.invalidateClients()
	return nil
}

// inheritSourceSecrets copies stored secrets over masked/empty incoming
// ones (editor round-trip).
func inheritSourceSecrets(in *profile.Source, existing profile.Source) {
	if isMasked(in.Password) {
		in.Password = existing.Password
	}
	if in.S3 == nil || existing.S3 == nil {
		return
	}
	if isMasked(in.S3.SecretKey) {
		in.S3.SecretKey = existing.S3.SecretKey
	}
	if isMasked(in.S3.SessionToken) {
		in.S3.SessionToken = existing.S3.SessionToken
	}
}

func (a *App) saveSourceInContainer(in profile.Source) error {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	pf := a.pf
	if pf == nil {
		return errNoProfileFile
	}
	for i := range pf.sources {
		if pf.sources[i].ID == in.ID || (in.ID == "" && pf.sources[i].Name == in.Name) {
			inheritSourceSecrets(&in, pf.sources[i])
			break
		}
	}
	if err := profile.UpsertSourceIn(&pf.sources, in); err != nil {
		return err
	}
	pf.dirty = true
	a.invalidateClients()
	return nil
}

// RemoveSource deletes a data source by ID or name from the workspace.
func (a *App) RemoveSource(idOrName string) error {
	a.pfMu.Lock()
	pf := a.pf
	if pf != nil {
		for i, src := range pf.sources {
			if src.ID == idOrName || src.Name == idOrName {
				pf.sources = append(pf.sources[:i], pf.sources[i+1:]...)
				pf.dirty = true
				a.pfMu.Unlock()
				a.invalidateClients()
				return nil
			}
		}
		a.pfMu.Unlock()
		return fmt.Errorf("%w: source %q", profile.ErrNotFound, idOrName)
	}
	for i, src := range a.session {
		if src.ID == idOrName || src.Name == idOrName {
			a.session = append(a.session[:i], a.session[i+1:]...)
			a.pfMu.Unlock()
			a.invalidateClients()
			return nil
		}
	}
	a.pfMu.Unlock()
	return fmt.Errorf("%w: source %q", profile.ErrNotFound, idOrName)
}

// TestSource probes a data source. S3 sources get the real bucket-listing
// check; other engines land with M9 and report so honestly.
func (a *App) TestSource(idOrName string) TestResult {
	src, err := a.sourceByIDOrName(idOrName)
	if err != nil {
		return TestResult{OK: false, Message: err.Error()}
	}
	if src.Type != profile.TypeS3 || src.S3 == nil {
		return a.testRemoteSource(src)
	}
	return a.TestProfile(src.Name)
}

// sourceByIDOrName resolves a source from the workspace (open Profile
// file, else the session registry). The embedded S3 profile is deep-copied
// so callers may edit their result without corrupting the workspace.
func (a *App) sourceByIDOrName(idOrName string) (profile.Source, error) {
	for _, src := range a.workspaceSources() {
		if (idOrName != "" && src.ID == idOrName) || src.Name == idOrName {
			if src.S3 != nil {
				p := *src.S3
				src.S3 = &p
			}
			return src, nil
		}
	}
	return profile.Source{}, fmt.Errorf("%w: source %q", profile.ErrNotFound, idOrName)
}

// --- Profile file (container) session -------------------------------

// NewProfileFile starts a fresh Profile file session (Save As to give it a
// file). Refuses to replace an open file with unsaved changes, or to
// shadow unsaved session sources (save or close them first).
func (a *App) NewProfileFile(name, password string) error {
	if password == "" {
		return fmt.Errorf("a password is required to encrypt the profile file")
	}
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	if a.pf != nil && a.pf.dirty {
		return errors.New("the open profile file has unsaved changes — save or close it first")
	}
	if n := len(a.session); n > 0 {
		return fmt.Errorf("%d unsaved session source(s) exist — save them first (Save profile file as…) or close the session", n)
	}
	a.pf = &openProfileFile{name: strings.TrimSpace(name), password: password}
	a.invalidateClients()
	return nil
}

// OpenProfileFile decrypts a *.s3bprofile into a session. A wrong password
// and a corrupted file are indistinguishable by design. Refuses while the
// open file is dirty or unsaved session sources exist (no silent merges).
func (a *App) OpenProfileFile(path, password string) error {
	a.pfMu.Lock()
	if a.pf != nil && a.pf.dirty {
		a.pfMu.Unlock()
		return errors.New("the open profile file has unsaved changes — save or close it first")
	}
	if n := len(a.session); n > 0 {
		a.pfMu.Unlock()
		return fmt.Errorf("%d unsaved session source(s) exist — save them first (Save profile file as…) or close the session", n)
	}
	a.pfMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	name, srcs, err := profile.DecryptContainer(data, password)
	if err != nil {
		return err
	}
	if _, err := profile.NormalizeSources(srcs); err != nil {
		return fmt.Errorf("profile file %s: %w", filepath.Base(path), err)
	}
	a.pfMu.Lock()
	a.pf = &openProfileFile{name: name, path: path, password: password, sources: srcs}
	a.pfMu.Unlock()
	a.invalidateClients()
	return nil
}

// SaveProfileFile re-encrypts the session to its current path.
func (a *App) SaveProfileFile() error {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	pf := a.pf
	if pf == nil {
		return errNoProfileFile
	}
	if pf.path == "" {
		return errors.New("the profile file has no path yet — use Save As")
	}
	data, err := profile.EncryptContainer(pf.name, pf.sources, pf.password)
	if err != nil {
		return err
	}
	if err := os.WriteFile(pf.path, data, 0o600); err != nil {
		return err
	}
	pf.dirty = false
	return nil
}

// SaveProfileFileAs re-encrypts the session under a new path (and optional
// new password; empty keeps the current one). Without an open Profile file
// it adopts the session-only sources — this is how unsaved session sources
// become a Profile file (no "New Profile file" prerequisite).
func (a *App) SaveProfileFileAs(path, password string) error {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	pf := a.pf
	if pf == nil {
		if len(a.session) == 0 {
			return errNoProfileFile
		}
		name := strings.TrimSuffix(filepath.Base(path), ".s3bprofile")
		if name == "" {
			name = "profile"
		}
		pf = &openProfileFile{name: name, sources: a.session}
		a.pf = pf
		a.session = nil
	}
	if password == "" {
		password = pf.password
	}
	if password == "" {
		return fmt.Errorf("a password is required to encrypt the profile file")
	}
	data, err := profile.EncryptContainer(pf.name, pf.sources, password)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	pf.path, pf.password, pf.dirty = path, password, false
	return nil
}

// CloseProfileFile ends the session. Refuses while the container is dirty
// or session-only sources exist, unless force (discard). Closing always
// means a clean slate: the container AND any session remainder go away.
func (a *App) CloseProfileFile(force bool) error {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	pf := a.pf
	if pf == nil {
		if len(a.session) == 0 {
			return nil // idempotent
		}
		if !force {
			return fmt.Errorf("%d unsaved session source(s) exist — save them first or force close", len(a.session))
		}
		a.session = nil
		a.invalidateClients()
		return nil
	}
	if pf.dirty && !force {
		return errors.New("the profile file has unsaved changes — save first or force close")
	}
	pf.password = "" // best-effort wipe
	a.pf = nil
	a.session = nil
	a.invalidateClients()
	return nil
}

// GetProfileFileState reports the session for the File menu / title bar.
// When no file is open, SourceCount is the number of unsaved session
// sources (shown as "● unsaved session" in the status bar).
func (a *App) GetProfileFileState() ProfileFileState {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	pf := a.pf
	if pf == nil {
		return ProfileFileState{SourceCount: len(a.session)}
	}
	return ProfileFileState{
		Open:        true,
		Name:        pf.name,
		Path:        pf.path,
		Dirty:       pf.dirty,
		SourceCount: len(pf.sources),
	}
}

// profileFileOpen reports whether a container session is active.
func (a *App) profileFileOpen() bool {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	return a.pf != nil
}

// containerSources returns a copy of the container's sources (nil when no
// file is open). Shallow: embedded S3 profiles are shared read-only.
func (a *App) containerSources() []profile.Source {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	if a.pf == nil {
		return nil
	}
	out := make([]profile.Source, len(a.pf.sources))
	copy(out, a.pf.sources)
	return out
}

// workspaceSources returns a copy of the workspace's sources: the open
// Profile file's, else the session-only registry's. Shallow: embedded S3
// profiles are shared read-only.
func (a *App) workspaceSources() []profile.Source {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	srcs := a.session
	if a.pf != nil {
		srcs = a.pf.sources
	}
	out := make([]profile.Source, len(srcs))
	copy(out, srcs)
	return out
}

// s3SourceNamed resolves an s3 source for client() lookups: the named
// source, or (name == "") the default/single s3 source. ok is false when
// nothing matches.
func s3SourceNamed(srcs []profile.Source, name string) (src profile.Source, ok bool) {
	isS3 := func(s profile.Source) bool { return s.Type == profile.TypeS3 && s.S3 != nil }
	if name == "" {
		var only profile.Source
		n := 0
		for _, s := range srcs {
			if !isS3(s) {
				continue
			}
			n++
			only = s
			if s.Default {
				return s, true
			}
		}
		if n == 1 {
			return only, true
		}
		return profile.Source{}, false
	}
	for _, s := range srcs {
		if s.Name == name && isS3(s) {
			return s, true
		}
	}
	return profile.Source{}, false
}

// containerS3Source resolves an s3 source from the open Profile file for
// client() lookups.
func (a *App) containerS3Source(name string) (src profile.Source, ok bool) {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	if a.pf == nil {
		return profile.Source{}, false
	}
	return s3SourceNamed(a.pf.sources, name)
}

// sessionS3Source is containerS3Source's twin over the session registry.
func (a *App) sessionS3Source(name string) (profile.Source, bool) {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	return s3SourceNamed(a.session, name)
}
