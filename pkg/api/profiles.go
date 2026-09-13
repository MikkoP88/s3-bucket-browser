package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// awsProfile holds the parsed settings of one section of an AWS shared
// credentials/config file.
type awsProfile struct {
	access   string
	secret   string
	token    string
	endpoint string
	region   string
}

// parseAwsIni parses an AWS-style INI file. When configFile is true only
// [default] and [profile x] sections are kept — the config file also
// carries sso-session/services blocks that are not importable credentials.
func parseAwsIni(data string, configFile bool) map[string]*awsProfile {
	out := map[string]*awsProfile{}
	cur := ""
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = strings.TrimSpace(line[1 : len(line)-1])
			if configFile && cur != "default" && !strings.HasPrefix(cur, "profile ") {
				cur = "" // not a profile section
				continue
			}
			cur = strings.TrimPrefix(cur, "profile ")
			if cur != "" && out[cur] == nil {
				out[cur] = &awsProfile{}
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || cur == "" {
			continue
		}
		p := out[cur]
		if p == nil {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "aws_access_key_id":
			p.access = v
		case "aws_secret_access_key":
			p.secret = v
		case "aws_session_token":
			p.token = v
		case "endpoint_url":
			p.endpoint = v
		case "region":
			p.region = v
		}
	}
	return out
}

// ImportAwsCredentials imports connections from the AWS shared credentials
// files (~/.aws/credentials, merged with ~/.aws/config for region and
// endpoint_url) as s3 data sources in the workspace (open Profile file,
// else the session registry), the onboarding shortcut. endpoint_url
// entries become S3-compatible providers (MinIO, R2, Wasabi, …); existing
// source names are skipped.
func (a *App) ImportAwsCredentials() (ImportResult, error) {
	var res ImportResult
	home, err := os.UserHomeDir()
	if err != nil {
		return res, err
	}
	credPath := filepath.Join(home, ".aws", "credentials")
	data, err := os.ReadFile(credPath)
	if err != nil {
		if os.IsNotExist(err) {
			return res, fmt.Errorf("%s not found — create it with 'aws configure' or add a data source manually", credPath)
		}
		return res, fmt.Errorf("cannot read %s: %w", credPath, err)
	}
	profiles := parseAwsIni(string(data), false)
	// The config file is optional; its [profile x] blocks carry region and
	// endpoint_url for profiles whose keys live in the credentials file.
	if cfgData, err := os.ReadFile(filepath.Join(home, ".aws", "config")); err == nil {
		for name, p := range parseAwsIni(string(cfgData), true) {
			if c := profiles[name]; c != nil {
				if c.endpoint == "" {
					c.endpoint = p.endpoint
				}
				if c.region == "" {
					c.region = p.region
				}
			}
		}
	}

	a.pfMu.Lock()
	defer a.pfMu.Unlock()
	dst := &a.session
	if a.pf != nil {
		dst = &a.pf.sources
	}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	imported := 0
	for _, name := range names {
		p := profiles[name]
		if p.access == "" || p.secret == "" {
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
				Name:         name,
				AccessKeyID:  p.access,
				SecretKey:    p.secret,
				SessionToken: p.token,
				Endpoint:     p.endpoint,
				Region:       p.region,
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
