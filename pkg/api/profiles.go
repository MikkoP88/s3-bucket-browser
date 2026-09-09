package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// ProfileDTO is a profile as shown in the GUI: secrets masked.
type ProfileDTO struct {
	Name         string `json:"name"`
	Endpoint     string `json:"endpoint,omitempty"`
	Region       string `json:"region,omitempty"`
	AccessKeyID  string `json:"accessKeyId,omitempty"`
	SecretMasked string `json:"secretMasked,omitempty"`
	PathStyle    bool   `json:"pathStyle"`
	Insecure     bool   `json:"insecure"`
	Default      bool   `json:"default,omitempty"`
	Provider     string `json:"provider"`
	Color        string `json:"color,omitempty"`
}

// ProfileInput carries a profile editor submission. Empty/masked secrets keep
// the stored values so the round-trip through the UI never wipes credentials.
type ProfileInput struct {
	Name         string `json:"name"`
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	AccessKeyID  string `json:"accessKeyId"`
	SecretKey    string `json:"secretKey"`
	SessionToken string `json:"sessionToken"`
	PathStyle    bool   `json:"pathStyle"`
	Insecure     bool   `json:"insecure"`
	Color        string `json:"color"`
	SetDefault   bool   `json:"setDefault"`
}

// ListProfiles returns all profiles (masked), sorted by name.
func (a *App) ListProfiles() ([]ProfileDTO, error) {
	s, err := a.loadStore()
	if err != nil {
		return nil, err
	}
	out := make([]ProfileDTO, 0, len(s.Profiles))
	for _, p := range s.Sorted() {
		out = append(out, ProfileDTO{
			Name:         p.Name,
			Endpoint:     p.Endpoint,
			Region:       p.Region,
			AccessKeyID:  p.AccessKeyID,
			SecretMasked: profile.Mask(p.SecretKey),
			PathStyle:    p.PathStyle,
			Insecure:     p.Insecure,
			Default:      p.Default,
			Provider:     p.Provider(),
			Color:        p.Color,
		})
	}
	return out, nil
}

// SaveProfile creates or updates a profile (Upsert by name).
func (a *App) SaveProfile(in ProfileInput) error {
	s, err := a.loadStore()
	if err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return fmt.Errorf("profile name is required")
	}
	if strings.ContainsAny(in.Name, " /\t") {
		return fmt.Errorf("profile name must not contain spaces or slashes")
	}

	p := profile.Profile{
		Name:         in.Name,
		Endpoint:     strings.TrimSpace(in.Endpoint),
		Region:       strings.TrimSpace(in.Region),
		AccessKeyID:  strings.TrimSpace(in.AccessKeyID),
		SecretKey:    in.SecretKey,
		SessionToken: in.SessionToken,
		PathStyle:    in.PathStyle,
		Insecure:     in.Insecure,
		Color:        in.Color,
	}
	// Editor round-trip: masked or empty secrets preserve stored values.
	if existing, err := s.Get(in.Name); err == nil {
		if isMasked(in.SecretKey) {
			p.SecretKey = existing.SecretKey
		}
		if isMasked(in.SessionToken) {
			p.SessionToken = existing.SessionToken
		}
	}
	if err := s.Upsert(p); err != nil {
		return err
	}
	if in.SetDefault || len(s.Profiles) == 1 {
		if err := s.SetDefault(p.Name); err != nil {
			return err
		}
	}
	if err := s.Save(); err != nil {
		return err
	}
	a.invalidateClients()
	return nil
}

// isMasked reports whether s is a masked secret or empty placeholder.
func isMasked(s string) bool {
	return s == "" || strings.Contains(s, "…") || strings.Contains(s, "****")
}

// RemoveProfile deletes a profile by name.
func (a *App) RemoveProfile(name string) error {
	s, err := a.loadStore()
	if err != nil {
		return err
	}
	if err := s.Remove(name); err != nil {
		return err
	}
	if err := s.Save(); err != nil {
		return err
	}
	a.invalidateClients()
	return nil
}

// SetDefaultProfile marks one profile as the default connection.
func (a *App) SetDefaultProfile(name string) error {
	s, err := a.loadStore()
	if err != nil {
		return err
	}
	if err := s.SetDefault(name); err != nil {
		return err
	}
	if err := s.Save(); err != nil {
		return err
	}
	a.invalidateClients()
	return nil
}

// TestResult is the outcome of a lightweight connectivity probe.
type TestResult struct {
	OK          bool   `json:"ok"`
	Message     string `json:"message"`
	Provider    string `json:"provider"`
	Endpoint    string `json:"endpoint"`
	BucketCount int    `json:"bucketCount"`
}

// TestProfile lists buckets with a short timeout: the fastest honest answer
// to "do these credentials work?".
func (a *App) TestProfile(name string) TestResult {
	c, err := a.client(name)
	if err != nil {
		return TestResult{OK: false, Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	buckets, err := listing.ListBuckets(ctx, c.S3)
	res := TestResult{
		Provider: c.ProviderKey,
		Endpoint: c.Endpoint,
	}
	if err != nil {
		res.Message = err.Error()
		return res
	}
	res.OK = true
	res.BucketCount = len(buckets)
	res.Message = fmt.Sprintf("connected — %d bucket(s) visible", len(buckets))
	return res
}

// ImportResult reports what an AWS credentials import did.
type ImportResult struct {
	Imported []string `json:"imported"`
	Skipped  []string `json:"skipped"`
}

// ImportAwsCredentials imports profiles from ~/.aws/credentials (INI), the
// M2 onboarding shortcut (PLAN.md §8). Existing profile names are skipped.
func (a *App) ImportAwsCredentials() (ImportResult, error) {
	var res ImportResult
	s, err := a.loadStore()
	if err != nil {
		return res, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return res, err
	}
	path := filepath.Join(home, ".aws", "credentials")
	data, err := os.ReadFile(path)
	if err != nil {
		return res, fmt.Errorf("cannot read %s: %w", path, err)
	}

	imported := 0
	cur := ""
	creds := map[string][2]string{} // section -> {access, secret}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || cur == "" {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		pair := creds[cur]
		switch k {
		case "aws_access_key_id":
			pair[0] = v
		case "aws_secret_access_key":
			pair[1] = v
		}
		creds[cur] = pair
	}

	for name, pair := range creds {
		if pair[0] == "" || pair[1] == "" {
			res.Skipped = append(res.Skipped, name+" (incomplete)")
			continue
		}
		if _, err := s.Get(name); err == nil {
			res.Skipped = append(res.Skipped, name+" (already exists)")
			continue
		}
		if err := s.Upsert(profile.Profile{
			Name:        name,
			AccessKeyID: pair[0],
			SecretKey:   pair[1],
		}); err != nil {
			return res, err
		}
		res.Imported = append(res.Imported, name)
		imported++
	}
	if imported > 0 {
		if err := s.Save(); err != nil {
			return res, err
		}
		a.invalidateClients()
	}
	return res, nil
}
