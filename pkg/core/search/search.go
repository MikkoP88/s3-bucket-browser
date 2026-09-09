// Package search implements cancelable deep search over object listings:
// stream every object under a prefix and match it against a filter (name
// glob, size, age, storage class). Results are delivered through a
// callback so both the CLI and the GUI can consume them incrementally
// without ever holding the whole bucket in memory (PLAN.md §13).
package search

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Filter describes one deep search. Zero fields mean "no restriction".
type Filter struct {
	Pattern     string        `json:"pattern,omitempty"`     // substring, or glob when it has * or ?
	LargerThan  int64         `json:"largerThan,omitempty"`  // bytes, strict >
	SmallerThan int64         `json:"smallerThan,omitempty"` // bytes, strict <
	OlderThan   time.Duration `json:"olderThan,omitempty"`   // matches LastModified older than now-X
	NewerThan   time.Duration `json:"newerThan,omitempty"`   // matches LastModified newer than now-X
	Class       string        `json:"class,omitempty"`       // exact storage class (case-insensitive)
	Limit       int           `json:"limit,omitempty"`       // stop after N matches (0 = unlimited)
}

// ErrStop is the sentinel a callback returns to end a search early without
// reporting an error.
var ErrStop = fmt.Errorf("stop search")

// Result is one match.
type Result struct {
	listing.Entry
	Bucket string `json:"bucket"`
}

// Match reports whether one object passes the filter. key is the full
// object key (globs are unanchored, so "backup*" also hits nested paths).
// mod may be nil (treated as unknown; age filters skip).
func Match(f Filter, key string, size int64, mod *time.Time, class string, now time.Time) bool {
	if f.Pattern != "" {
		if !matchPattern(f.Pattern, key) {
			return false
		}
	}
	if f.LargerThan > 0 && !(size > f.LargerThan) {
		return false
	}
	if f.SmallerThan > 0 && !(size < f.SmallerThan) {
		return false
	}
	if f.Class != "" && !strings.EqualFold(class, f.Class) {
		return false
	}
	if f.OlderThan > 0 {
		if mod == nil || !mod.Before(now.Add(-f.OlderThan)) {
			return false
		}
	}
	if f.NewerThan > 0 {
		if mod == nil || !mod.After(now.Add(-f.NewerThan)) {
			return false
		}
	}
	return true
}

// matchPattern matches substring (no wildcard chars) or glob (* = any
// run incl. '/', ? = one char), case-insensitively. Globs are unanchored:
// they may match anywhere in the full key, so "backup*" also hits nested
// paths. The empty key never matches.
func matchPattern(pattern, key string) bool {
	if key == "" {
		return false
	}
	if !strings.ContainsAny(pattern, "*?") {
		return strings.Contains(strings.ToLower(key), strings.ToLower(pattern))
	}
	var b strings.Builder
	b.WriteString(`(?i)`)
	for _, c := range pattern {
		switch c {
		case '*':
			b.WriteString(`.*`)
		case '?':
			b.WriteString(`.`)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(key)
}

// Stats reports a search outcome.
type Stats struct {
	Scanned int `json:"scanned"`
	Matched int `json:"matched"`
}

// Run walks every object under bucket/prefix and calls fn for each match.
// fn returning ErrStop (or any error) ends the walk; ErrStop is swallowed,
// other errors are returned. The callback receives entries whose Name is
// the object key relative to the prefix.
func Run(ctx context.Context, client s3.ListObjectsV2APIClient, bucket, prefix string, f Filter, fn func(Result) error) (Stats, error) {
	st := Stats{}
	now := time.Now()
	err := listing.Walk(ctx, client, bucket, prefix, func(o s3types.Object) error {
		st.Scanned++
		key := aws.ToString(o.Key)
		if !Match(f, key, aws.ToInt64(o.Size), o.LastModified, string(o.StorageClass), now) {
			return nil
		}
		st.Matched++
		if err := fn(Result{Entry: listing.FromObject(o, prefix), Bucket: bucket}); err != nil {
			return err
		}
		if f.Limit > 0 && st.Matched >= f.Limit {
			return ErrStop
		}
		return nil
	})
	if err == ErrStop {
		return st, nil
	}
	return st, err
}

// ParseSize parses "500", "10KB", "1.5MB", "2GB" (binary units, like
// everywhere else in s3b) into bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	unit := int64(1)
	if len(s) >= 2 {
		switch strings.ToUpper(s[len(s)-2:]) {
		case "KB":
			unit, s = 1<<10, s[:len(s)-2]
		case "MB":
			unit, s = 1<<20, s[:len(s)-2]
		case "GB":
			unit, s = 1<<30, s[:len(s)-2]
		case "TB":
			unit, s = 1<<40, s[:len(s)-2]
		default:
			if last := s[len(s)-1]; last == 'B' || last == 'b' {
				s = s[:len(s)-1]
			}
		}
	} else if last := s[len(s)-1]; last == 'B' || last == 'b' {
		s = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid size %q (use e.g. 10MB, 1.5GB)", s)
	}
	return int64(v * float64(unit)), nil
}
