// Package profile manages named connection profiles stored locally.
//
// M1 storage note (see PLAN.md §14): secrets live in a 0600 JSON file next to
// the profiles. M5 replaces this with OS keyring storage (go-keyring) plus an
// encrypted-file fallback; the Store API is designed so that swap is internal.
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
	Name         string    `json:"name"`
	Endpoint     string    `json:"endpoint,omitempty"` // empty = AWS default resolution
	Region       string    `json:"region,omitempty"`   // empty = us-east-1 at call time
	AccessKeyID  string    `json:"accessKeyId"`
	SecretKey    string    `json:"secretKey,omitempty"`
	SessionToken string    `json:"sessionToken,omitempty"`
	PathStyle    bool      `json:"pathStyle"`
	Insecure     bool      `json:"insecure"`        // skip TLS verification (labs)
	Color        string    `json:"color,omitempty"` // GUI accent color (M2)
	Default      bool      `json:"default,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
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

// Store is a collection of profiles persisted to a JSON file.
type Store struct {
	Path     string
	Profiles []Profile `json:"profiles"`
}

// ErrNotFound is returned when a profile name is unknown.
var ErrNotFound = errors.New("profile not found")

// DefaultDir returns the configuration directory (S3B_CONFIG overrides).
func DefaultDir() (string, error) {
	if v := os.Getenv("S3B_CONFIG"); v != "" {
		return v, nil
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

// LoadFrom reads the store from a specific path.
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
	return s, nil
}

// Save persists the store (0600).
func (s *Store) Save() error {
	if s.Path == "" {
		return errors.New("store path not set")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
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
			s.Profiles[i] = p
			return nil
		}
	}
	p.CreatedAt = now
	s.Profiles = append(s.Profiles, p)
	return nil
}

// Remove deletes a profile by name.
func (s *Store) Remove(name string) error {
	for i, p := range s.Profiles {
		if p.Name == name {
			s.Profiles = append(s.Profiles[:i], s.Profiles[i+1:]...)
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
