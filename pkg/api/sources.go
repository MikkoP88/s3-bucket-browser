// sources.go is the M8 API surface: data sources (connections of any type)
// and the password-encrypted Profile file (*.s3bprofile) that bundles them.
//
// Two modes govern where source edits land:
//
//   - store mode (default): sources live in profiles.json next to the
//     legacy profiles; S3 sources are mirrored as profiles so browsing and
//     the CLI keep resolving them by name.
//   - Profile file mode: an open container is the single source of truth;
//     edits stay in memory (dirty flag) until Save/Save As re-encrypts.
//     Nothing leaks into the local store, so a colleague's Profile file
//     leaves no trace after Close.
package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

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

// ListSources returns all configured data sources, secrets masked. When a
// Profile file is open it wins; otherwise the local store is used, seeded
// once from the legacy S3 profiles (M8 migration).
func (a *App) ListSources() ([]profile.Source, error) {
	if srcs := a.containerSources(); srcs != nil {
		return publicSources(srcs), nil
	}
	s, err := a.loadStore()
	if err != nil {
		return nil, err
	}
	if err := a.seedSources(s); err != nil {
		return nil, err
	}
	return publicSources(s.SortedSources()), nil
}

func publicSources(srcs []profile.Source) []profile.Source {
	out := make([]profile.Source, len(srcs))
	for i, src := range srcs {
		out[i] = src.Public()
	}
	return out
}

// seedSources is the one-way M8 migration: legacy S3 profiles become s3
// data sources once, then the two lists evolve independently.
func (a *App) seedSources(s *profile.Store) error {
	if len(s.Sources) > 0 || len(s.Profiles) == 0 {
		return nil
	}
	now := time.Now().UTC()
	srcs := make([]profile.Source, 0, len(s.Profiles))
	for _, p := range s.Profiles {
		src := profile.FromProfile(p)
		src.CreatedAt, src.UpdatedAt = now, now
		srcs = append(srcs, src)
	}
	norm, err := profile.NormalizeSources(srcs)
	if err != nil {
		return fmt.Errorf("migrating profiles to data sources: %w", err)
	}
	s.Sources = norm
	return s.Save()
}

// SaveSource creates or updates a data source (matched by ID, else name).
// Masked or empty secrets inherit the stored values, so editor round-trips
// never wipe credentials. S3 sources in store mode are mirrored into the
// legacy profile store so browsing and the CLI keep working.
func (a *App) SaveSource(in profile.Source) error {
	if a.profileFileOpen() {
		return a.saveSourceInContainer(in)
	}
	s, err := a.loadStore()
	if err != nil {
		return err
	}
	if existing, err := s.GetSource(sourceKey(in)); err == nil {
		inheritSourceSecrets(&in, existing)
		// A renamed s3 source leaves its old mirror profile behind.
		if in.Type == profile.TypeS3 && existing.Type == profile.TypeS3 && existing.Name != in.Name {
			_ = s.Remove(existing.Name)
		}
	}
	if in.Type == profile.TypeS3 && in.S3 != nil {
		p := *in.S3
		p.Name = in.Name
		if err := s.Upsert(p); err != nil {
			return err
		}
	}
	if err := s.UpsertSource(in); err != nil {
		return err
	}
	if err := s.Save(); err != nil {
		return err
	}
	a.invalidateClients()
	return nil
}

// sourceKey returns the match key for an incoming source: the ID when set,
// else the name.
func sourceKey(in profile.Source) string {
	if in.ID != "" {
		return in.ID
	}
	return in.Name
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

// RemoveSource deletes a data source by ID or name. In store mode an s3
// source takes its mirrored profile with it (they are the same connection).
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
	a.pfMu.Unlock()
	s, err := a.loadStore()
	if err != nil {
		return err
	}
	src, err := s.GetSource(idOrName)
	if err != nil {
		return err
	}
	if err := s.RemoveSource(src.ID); err != nil {
		return err
	}
	if src.Type == profile.TypeS3 {
		_ = s.Remove(src.Name) // mirror cleanup; absent mirror is fine
	}
	if err := s.Save(); err != nil {
		return err
	}
	a.invalidateClients()
	return nil
}

// TestSource probes a data source. S3 sources get the real bucket-listing
// check; other engines land with M9 and report so honestly.
func (a *App) TestSource(idOrName string) TestResult {
	src, err := a.sourceByIDOrName(idOrName)
	if err != nil {
		return TestResult{OK: false, Message: err.Error()}
	}
	if src.Type != profile.TypeS3 || src.S3 == nil {
		return TestResult{
			OK:      false,
			Message: fmt.Sprintf("%s connections are browsable once the %s engine ships (planned next); the connection is saved as configured", src.Type, src.Type),
		}
	}
	return a.TestProfile(src.Name)
}

// sourceByIDOrName resolves a source: open Profile file first, then store.
func (a *App) sourceByIDOrName(idOrName string) (profile.Source, error) {
	if srcs := a.containerSources(); srcs != nil {
		for _, src := range srcs {
			if (idOrName != "" && src.ID == idOrName) || src.Name == idOrName {
				return src, nil
			}
		}
		return profile.Source{}, fmt.Errorf("%w: source %q", profile.ErrNotFound, idOrName)
	}
	s, err := a.loadStore()
	if err != nil {
		return profile.Source{}, err
	}
	return s.GetSource(idOrName)
}

// --- Profile file (container) session -------------------------------

// NewProfileFile starts a fresh Profile file session (Save As to give it a
// file). Refuses to replace an open file with unsaved changes.
func (a *App) NewProfileFile(name, password string) error {
	if password == "" {
		return fmt.Errorf("a password is required to encrypt the profile file")
	}
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	if a.pf != nil && a.pf.dirty {
		return errors.New("the open profile file has unsaved changes — save or close it first")
	}
	a.pf = &openProfileFile{name: strings.TrimSpace(name), password: password}
	a.invalidateClients()
	return nil
}

// OpenProfileFile decrypts a *.s3bprofile into a session. A wrong password
// and a corrupted file are indistinguishable by design.
func (a *App) OpenProfileFile(path, password string) error {
	a.pfMu.Lock()
	if a.pf != nil && a.pf.dirty {
		a.pfMu.Unlock()
		return errors.New("the open profile file has unsaved changes — save or close it first")
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
// new password; empty keeps the current one).
func (a *App) SaveProfileFileAs(path, password string) error {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	pf := a.pf
	if pf == nil {
		return errNoProfileFile
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

// CloseProfileFile ends the session. Refuses while dirty unless force.
func (a *App) CloseProfileFile(force bool) error {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	pf := a.pf
	if pf == nil {
		return nil // idempotent
	}
	if pf.dirty && !force {
		return errors.New("the profile file has unsaved changes — save first or force close")
	}
	pf.password = "" // best-effort wipe
	a.pf = nil
	a.invalidateClients()
	return nil
}

// GetProfileFileState reports the session for the File menu / title bar.
func (a *App) GetProfileFileState() ProfileFileState {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	pf := a.pf
	if pf == nil {
		return ProfileFileState{}
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

// containerS3Source resolves an s3 source for client() lookups: the named
// source in the open file, or (name == "") the file's default/single s3
// source. ok is false when no file is open or nothing matches.
func (a *App) containerS3Source(name string) (src profile.Source, ok bool) {
	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	pf := a.pf
	if pf == nil {
		return profile.Source{}, false
	}
	isS3 := func(s profile.Source) bool { return s.Type == profile.TypeS3 && s.S3 != nil }
	if name == "" {
		var only profile.Source
		n := 0
		for _, s := range pf.sources {
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
	for _, s := range pf.sources {
		if s.Name == name && isS3(s) {
			return s, true
		}
	}
	return profile.Source{}, false
}
