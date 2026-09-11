// Source is the M8 generalization of a connection: one named entry of any
// supported type. The legacy Profile type remains the S3 specialization
// (embedded below); sftp/ftp/ftps engines land in M9, but their connection
// schema is fixed here so Profile files and the API are forward-compatible.
package profile

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Source types.
const (
	TypeS3      = "s3"
	TypeSFTP    = "sftp"
	TypeSCP     = "scp" // sftp engine, scp:// URIs
	TypeFTP     = "ftp"
	TypeFTPS    = "ftps"
	TypeWebDAV  = "webdav"
	TypeWebDAVS = "webdavs" // TLS, webdavs:// URIs
	TypeLocal   = "local"
)

// Source is one data source: an S3 endpoint, a remote filesystem host or a
// local directory root. Exactly one type's field set is meaningful; S3
// sources carry the full legacy Profile (endpoint, credentials, options).
type Source struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"` // one of the Type* constants
	Color   string `json:"color,omitempty"`
	Default bool   `json:"default,omitempty"` // default S3 source

	// S3 (Type == s3): the complete legacy profile, secrets included.
	S3 *Profile `json:"s3,omitempty"`

	// Remote filesystem (Type in sftp/scp/ftp/ftps).
	Host          string `json:"host,omitempty"`
	Port          int    `json:"port,omitempty"` // 0 = per-type default at dial time
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"` // masked by Public
	PassInKeyring bool   `json:"passInKeyring,omitempty"`
	Root          string `json:"root,omitempty"` // starting directory

	// Local filesystem (Type == local).
	LocalRoot string `json:"localRoot,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DefaultPort returns the conventional port for the source type (0 for
// types without one).
func (s Source) DefaultPort() int {
	switch s.Type {
	case TypeSFTP, TypeSCP:
		return 22
	case TypeFTP:
		return 21
	case TypeFTPS:
		return 990
	case TypeWebDAV:
		return 80
	case TypeWebDAVS:
		return 443
	default:
		return 0
	}
}

// Validate checks the per-type required fields.
func (s Source) Validate() error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return fmt.Errorf("source name is required")
	}
	switch s.Type {
	case TypeS3:
		if s.S3 == nil {
			return fmt.Errorf("source %q: s3 source needs connection details", s.Name)
		}
	case TypeSFTP, TypeSCP, TypeFTP, TypeFTPS, TypeWebDAV, TypeWebDAVS:
		if strings.TrimSpace(s.Host) == "" {
			return fmt.Errorf("source %q: host is required for %s sources", s.Name, s.Type)
		}
	case TypeLocal:
		if strings.TrimSpace(s.LocalRoot) == "" {
			return fmt.Errorf("source %q: local root path is required", s.Name)
		}
	default:
		return fmt.Errorf("source %q: unsupported type %q", s.Name, s.Type)
	}
	return nil
}

// Public returns a copy safe for display or JSON output: the sftp/ftp
// password and embedded S3 secrets are masked.
func (s Source) Public() Source {
	out := s
	out.Password = Mask(s.Password)
	if s.S3 != nil {
		p := s.S3.Public()
		out.S3 = &p
	}
	return out
}

// FromProfile wraps a legacy S3 profile as a Source (M8 migration: every
// profile in the legacy store is an s3 data source).
func FromProfile(p Profile) Source {
	return Source{
		Name:    p.Name,
		Type:    TypeS3,
		S3:      &p,
		Color:   p.Color,
		Default: p.Default,
	}
}

// NormalizeSources validates a set, deduplicates names and ensures at most
// one default. IDs are assigned when missing.
func NormalizeSources(srcs []Source) ([]Source, error) {
	seen := map[string]bool{}
	usedIDs := map[string]bool{}
	out := make([]Source, 0, len(srcs))
	for _, s := range srcs {
		if err := s.Validate(); err != nil {
			return nil, err
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("duplicate source name %q", s.Name)
		}
		seen[s.Name] = true
		if s.ID == "" || usedIDs[s.ID] {
			s.ID = newSourceID()
		}
		usedIDs[s.ID] = true
		out = append(out, s)
	}
	return out, nil
}

// newSourceID returns a short unique-enough identifier for a source (the
// sources list is tiny; collisions are re-rolled by NormalizeSources).
func newSourceID() string {
	return fmt.Sprintf("src_%d", time.Now().UnixNano())
}

// newUniqueSourceID re-rolls newSourceID until it is free in the list —
// consecutive calls can land on the same nanosecond on coarse clocks
// (observed on Windows), which would turn an append into a replace.
func newUniqueSourceID(list []Source) string {
	used := make(map[string]bool, len(list))
	for _, s := range list {
		used[s.ID] = true
	}
	id := newSourceID()
	for used[id] {
		id = newSourceID()
	}
	return id
}

// GetSource returns a source by ID, falling back to a name match.
func (s *Store) GetSource(idOrName string) (Source, error) {
	for _, src := range s.Sources {
		if (idOrName != "" && src.ID == idOrName) || src.Name == idOrName {
			return src, nil
		}
	}
	return Source{}, fmt.Errorf("%w: source %q", ErrNotFound, idOrName)
}

// UpsertSource inserts or updates a source in the store.
func (s *Store) UpsertSource(src Source) error {
	return UpsertSourceIn(&s.Sources, src)
}

// UpsertSourceIn is the shared insert-or-update for a source list (the
// store or an open Profile file): strictly keyed by ID — an empty ID means
// create, and a create with an already-used name is rejected (updates carry
// their ID and may rename freely, as long as the new name is free). At most
// one source stays default; timestamps set.
func UpsertSourceIn(list *[]Source, src Source) error {
	if err := src.Validate(); err != nil {
		return err
	}
	if src.ID == "" {
		src.ID = newUniqueSourceID(*list)
	}
	now := time.Now().UTC()
	src.UpdatedAt = now
	idx := -1
	for i, existing := range *list {
		if existing.ID == src.ID {
			idx = i
			continue
		}
		if existing.Name == src.Name {
			return fmt.Errorf("duplicate source name %q", src.Name)
		}
	}
	if src.Default {
		for i := range *list {
			(*list)[i].Default = false
		}
	}
	if idx >= 0 {
		src.CreatedAt = (*list)[idx].CreatedAt
		(*list)[idx] = src
		return nil
	}
	src.CreatedAt = now
	*list = append(*list, src)
	return nil
}

// RemoveSource deletes a source by ID or name (and its keyring entries).
func (s *Store) RemoveSource(idOrName string) error {
	for i, src := range s.Sources {
		if src.ID == idOrName || src.Name == idOrName {
			s.Sources = append(s.Sources[:i], s.Sources[i+1:]...)
			sourceKeyringDeleteAll(src.ID)
			return nil
		}
	}
	return fmt.Errorf("%w: source %q", ErrNotFound, idOrName)
}

// SortedSources returns sources sorted by name (stable UI order).
func (s *Store) SortedSources() []Source {
	out := make([]Source, len(s.Sources))
	copy(out, s.Sources)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SeedFromProfiles is the one-way M8 migration: when no sources exist yet,
// every legacy profile becomes an s3 source. Returns true when the store
// changed (the caller persists).
func (s *Store) SeedFromProfiles() (bool, error) {
	if len(s.Sources) > 0 || len(s.Profiles) == 0 {
		return false, nil
	}
	now := time.Now().UTC()
	srcs := make([]Source, 0, len(s.Profiles))
	for _, p := range s.Profiles {
		src := FromProfile(p)
		src.CreatedAt, src.UpdatedAt = now, now
		srcs = append(srcs, src)
	}
	norm, err := NormalizeSources(srcs)
	if err != nil {
		return false, fmt.Errorf("migrating profiles to data sources: %w", err)
	}
	s.Sources = norm
	return true, nil
}

// --- profile ↔ source mirroring (M8) -------------------------------
//
// S3 sources are mirrored as same-named legacy profiles so browsing and
// the CLI keep resolving them. These helpers run the mirror in reverse
// (profile-first writes) so every legacy write path — the old profile
// editor, AWS credential import, `s3b profile add` — leaves the two
// lists consistent instead of growing invisible profiles.

// UpsertS3Profile upserts a profile and keeps its s3 source mirror in
// sync (new name creates the source, existing name updates it while
// preserving source-only fields: ID, color, default flag, timestamps).
func (s *Store) UpsertS3Profile(p Profile) error {
	if err := s.Upsert(p); err != nil {
		return err
	}
	return s.mirrorProfileToSource(p)
}

// mirrorProfileToSource upserts the s3 source matching a profile by name.
func (s *Store) mirrorProfileToSource(p Profile) error {
	src := FromProfile(p)
	for _, existing := range s.Sources {
		if existing.Type == TypeS3 && existing.Name == p.Name {
			src.ID = existing.ID
			src.CreatedAt = existing.CreatedAt
			if existing.Default {
				src.Default = true
			}
			if src.Color == "" {
				src.Color = existing.Color
			}
			break
		}
	}
	return s.UpsertSource(src)
}

// RemoveS3Profile removes a profile and its same-named s3 source — they
// are the same connection.
func (s *Store) RemoveS3Profile(name string) error {
	if err := s.Remove(name); err != nil {
		return err
	}
	if src, err := s.GetSource(name); err == nil && src.Type == TypeS3 && src.Name == name {
		_ = s.RemoveSource(src.ID)
	}
	return nil
}

// SetDefaultS3 flags one profile as the default connection and mirrors
// the flag onto its source (other defaults clear on both sides).
func (s *Store) SetDefaultS3(name string) error {
	if err := s.SetDefault(name); err != nil {
		return err
	}
	p, err := s.Get(name)
	if err != nil {
		return err // unreachable after a successful SetDefault
	}
	return s.mirrorProfileToSource(p)
}
