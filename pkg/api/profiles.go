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
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
)

// profiles.go carries the connectivity probe (TestProfile) and the
// ~/.aws/credentials import. The legacy profile-mirror API (ListProfiles,
// SaveProfile, RemoveProfile, SetDefaultProfile) is gone with the strict
// sources model — the GUI workspace is sources-only and never touches the
// CLI's profile store.

// isMasked reports whether s is a masked secret or empty placeholder.
func isMasked(s string) bool {
	return s == "" || strings.Contains(s, "…") || strings.Contains(s, "****")
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
	return probeBuckets(ctx, c)
}

// probeBuckets is the shared Test tail: list buckets with the caller's
// deadline and shape the verdict.
func probeBuckets(ctx context.Context, c *s3client.Client) TestResult {
	res := TestResult{
		Provider: c.ProviderKey,
		Endpoint: c.Endpoint,
	}
	buckets, err := listing.ListBuckets(ctx, c.S3)
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

// ImportAwsCredentials imports connections from ~/.aws/credentials (INI)
// as s3 data sources in the workspace (open Profile file, else the session
// registry), the onboarding shortcut. Existing source names are skipped.
func (a *App) ImportAwsCredentials() (ImportResult, error) {
	var res ImportResult
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

	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	dst := &a.session
	if a.pf != nil {
		dst = &a.pf.sources
	}
	for name, pair := range creds {
		if pair[0] == "" || pair[1] == "" {
			res.Skipped = append(res.Skipped, name+" (incomplete)")
			continue
		}
		exists := false
		for _, s := range *dst {
			if s.Name == name {
				exists = true
				break
			}
		}
		if exists {
			res.Skipped = append(res.Skipped, name+" (already exists)")
			continue
		}
		if err := profile.UpsertSourceIn(dst, profile.Source{
			Name: name,
			Type: profile.TypeS3,
			S3: &profile.Profile{
				Name:        name,
				AccessKeyID: pair[0],
				SecretKey:   pair[1],
			},
		}); err != nil {
			return res, err
		}
		res.Imported = append(res.Imported, name)
		imported++
	}
	if imported > 0 {
		if a.pf != nil {
			a.pf.dirty = true
		}
		a.invalidateClients()
	}
	return res, nil
}
