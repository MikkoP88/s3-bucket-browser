// Package profile manages named connection profiles stored locally.
//
// Secret storage (PLAN.md §14, M5): secrets live in the OS keyring
// (Windows Credential Manager / macOS Keychain / SecretService) whenever
// one is available and are hydrated back on load; the profiles JSON only
// keeps a flag. Without a keyring (headless hosts, tests, S3B_NO_KEYRING=1)
// secrets stay in the 0600 JSON file exactly as in M1.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/provider"
)

// Profile is one named S3 connection.
type Profile struct {
	Name         string `json:"name"`
	Endpoint     string `json:"endpoint,omitempty"` // empty = AWS default resolution
	Region       string `json:"region,omitempty"`   // empty = us-east-1 at call time
	AccessKeyID  string `json:"accessKeyId"`
	SecretKey    string `json:"secretKey,omitempty"`
	SessionToken string `json:"sessionToken,omitempty"`
	PathStyle    bool   `json:"pathStyle"`
	Insecure     bool   `json:"insecure"`        // skip TLS verification (labs)
	Color        string `json:"color,omitempty"` // GUI accent color (M2)
	Default      bool   `json:"default,omitempty"`
	// SecretInKeyring: the secret/token live in the OS keyring, not in the
	// JSON file (M5). Set automatically by Save when a keyring is present.
	SecretInKeyring bool      `json:"secretInKeyring,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Provider returns the detected provider key for the profile endpoint.
func (p Profile) Provider() string {
	if p.Endpoint == "" {
		return "aws"
	}
	return provider.Detect(p.Endpoint)
}

// Public returns a copy safe for display or JSON output: secrets masked.
func (p Profile) Public() Profile {
	out := p
	out.SecretKey = Mask(p.SecretKey)
	out.SessionToken = Mask(p.SessionToken)
	return out
}

// Mask redacts a secret, keeping a short recognizable prefix/suffix.
func Mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 6 {
		return "****"
	}
	return s[:4] + "…" + s[len(s)-2:]
}

// Store is a collection of profiles persisted to a JSON file. Since M8 it
// also carries Sources (connections of any type); the legacy Profiles array
// stays the S3 specialization the CLI and the browsing stack resolve.
type Store struct {
	Path     string
	Profiles []Profile `json:"profiles"`
	Sources  []Source  `json:"sources,omitempty"`
}

// ErrNotFound is returned when a profile name is unknown.
var ErrNotFound = errors.New("profile not found")

// DefaultDir returns the configuration directory. Precedence:
// $S3B_CONFIG, then portable mode (a "s3b-portable" marker next to the
// executable), then the OS user-config dir.
func DefaultDir() (string, error) {
	if v := os.Getenv("S3B_CONFIG"); v != "" {
		return v, nil
	}
	if dir, ok := portableConfigDir(); ok {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "s3b"), nil
}

// DefaultPath returns the default profiles.json path.
func DefaultPath() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "profiles.json"), nil
}

// Load reads the store from the default path (empty store if file missing).
func Load() (*Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads the store from a specific path. Keyring-stored secrets
// are hydrated back into the in-memory profiles.
func LoadFrom(path string) (*Store, error) {
	s := &Store{Path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("invalid profile store %s: %w", path, err)
	}
	s.ensureSourceIDs()
	s.hydrateSecretsFromKeyring()
	return s, nil
}

// ensureSourceIDs assigns an ID to any source that arrived without one
// (hand-edited stores); persisted on the next Save.
func (s *Store) ensureSourceIDs() {
	used := map[string]bool{}
	for _, src := range s.Sources {
		used[src.ID] = true
	}
	for i := range s.Sources {
		if s.Sources[i].ID == "" {
			id := newSourceID()
			for used[id] {
				id = newSourceID()
			}
			s.Sources[i].ID = id
			used[id] = true
		}
	}
}

// Save persists the store (0600). When an OS keyring is available,
// plaintext secrets are migrated out of the JSON file first (M5).
func (s *Store) Save() error {
	if s.Path == "" {
		return errors.New("store path not set")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	s.migrateSecretsToKeyring()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, data, 0o600)
}

// Get returns the named profile.
func (s *Store) Get(name string) (Profile, error) {
	for _, p := range s.Profiles {
		if p.Name == name {
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("%w: %q", ErrNotFound, name)
}

// DefaultProfile returns the profile marked default, or the only profile.
func (s *Store) DefaultProfile() (Profile, error) {
	if len(s.Profiles) == 0 {
		return Profile{}, errors.New("no profiles configured — run: s3b profile add <name> ...")
	}
	for _, p := range s.Profiles {
		if p.Default {
			return p, nil
		}
	}
	if len(s.Profiles) == 1 {
		return s.Profiles[0], nil
	}
	return Profile{}, errors.New("multiple profiles and none marked default — pass --profile <name> or run: s3b profile use <name>")
}

// Upsert inserts or updates a profile by name, preserving default flags.
func (s *Store) Upsert(p Profile) error {
	if p.Name == "" {
		return errors.New("profile name is required")
	}
	now := time.Now().UTC()
	p.UpdatedAt = now
	for i, existing := range s.Profiles {
		if existing.Name == p.Name {
			p.CreatedAt = existing.CreatedAt
			p.Default = existing.Default
			// Editor round-trips arrive without the secret (masked or
			// empty): inherit the stored one, keyring flag included.
			if p.SecretKey == "" && existing.SecretInKeyring {
				p.SecretInKeyring = true
				p.SecretKey = existing.SecretKey
				if p.SessionToken == "" {
					p.SessionToken = existing.SessionToken
				}
			}
			s.Profiles[i] = p
			return nil
		}
	}
	p.CreatedAt = now
	s.Profiles = append(s.Profiles, p)
	return nil
}

// Remove deletes a profile by name (and its keyring entries).
func (s *Store) Remove(name string) error {
	for i, p := range s.Profiles {
		if p.Name == name {
			s.Profiles = append(s.Profiles[:i], s.Profiles[i+1:]...)
			keyringDeleteAll(name)
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrNotFound, name)
}

// SetDefault marks exactly one profile as default.
func (s *Store) SetDefault(name string) error {
	found := false
	for i := range s.Profiles {
		s.Profiles[i].Default = s.Profiles[i].Name == name
		if s.Profiles[i].Default {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return nil
}

// Sorted returns profiles sorted by name.
func (s *Store) Sorted() []Profile {
	out := make([]Profile, len(s.Profiles))
	copy(out, s.Profiles)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
