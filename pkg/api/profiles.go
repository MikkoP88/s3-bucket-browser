package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/bucketops"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
	"github.com/aws/aws-sdk-go-v2/aws"
)

// profiles.go carries the connectivity probe (TestProfile). The legacy
// profile-mirror API (ListProfiles, SaveProfile, RemoveProfile,
// SetDefaultProfile) is gone with the strict sources model — the GUI
// workspace is sources-only and never touches the CLI's profile store.
// Credential imports (files and KMS services, AWS INI included) live in
// importcreds.go.

// isMasked reports whether s is a masked secret or empty placeholder.
func isMasked(s string) bool {
	return s == "" || strings.Contains(s, "…") || strings.Contains(s, "****")
}

// TestResult is the outcome of a lightweight connectivity probe.
type TestResult struct {
	OK          bool     `json:"ok"`
	Message     string   `json:"message"`
	Provider    string   `json:"provider"`
	Endpoint    string   `json:"endpoint"`
	BucketCount int      `json:"bucketCount"`
	Buckets     []string `json:"buckets,omitempty"` // names (account-wide probes)
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
// deadline and shape the verdict (names included — the editor's bucket
// field offers them as completions).
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
	names := make([]string, 0, len(buckets))
	for _, b := range buckets {
		names = append(names, aws.ToString(b.Name))
	}
	res.OK = true
	res.Buckets = names
	res.BucketCount = len(names)
	res.Message = fmt.Sprintf("connected — %d bucket(s) visible", len(names))
	return res
}

// probeBucket is probeBuckets' bucket-scoped twin: the credential may not
// be allowed to list all buckets, so the check is a HEAD on the one bucket
// the source is scoped to.
func probeBucket(ctx context.Context, c *s3client.Client, bucket string) TestResult {
	res := TestResult{
		Provider: c.ProviderKey,
		Endpoint: c.Endpoint,
	}
	if _, err := bucketops.Head(ctx, c.S3, bucket); err != nil {
		res.Message = err.Error()
		return res
	}
	res.OK = true
	res.Message = fmt.Sprintf("connected — bucket %s accessible", bucket)
	return res
}

// ImportResult reports what a credentials import did. Updated lists
// existing sources that matched a candidate by connection and were
// refreshed in place (re-imports never duplicate).
type ImportResult struct {
	Imported []string `json:"imported"`
	Updated  []string `json:"updated"`
	Skipped  []string `json:"skipped"`
}

// importSourceName maps a credential-file section name onto a source
// name. AWS's [default] section must not become a source literally named
// "default" — a leftover section with stale keys would then surface as a
// non-functional "default" data source; "aws" names what it actually is.
func importSourceName(section string) string {
	if strings.EqualFold(strings.TrimSpace(section), "default") {
		return "aws"
	}
	return section
}
