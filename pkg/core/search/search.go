// Package search implements cancelable deep search over object listings:
// stream every object under a prefix and match it against a filter (name
// glob, extension, path, size, age, storage class). Results are delivered
// through a callback so both the CLI and the GUI can consume them
// incrementally without ever holding the whole bucket in memory.
package search

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Filter describes one deep search. Zero fields mean "no restriction".
// Every field except Class is a name, size or age predicate, so it applies
// identically to S3 objects and remote-engine paths alike.
type Filter struct {
	Pattern     string        `json:"pattern,omitempty"`     // substring, or glob when it has * or ?
	Kind        string        `json:"kind,omitempty"`        // "" any, "file", "dir" (case-insensitive)
	Ext         string        `json:"ext,omitempty"`         // comma-separated name extensions ("pdf, .jpg"), case-insensitive
	Path        string        `json:"path,omitempty"`        // substring the parent directory must contain
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

// Result is one match. Bucket is set on S3 runs, Source on every run the
// bridge launches (the S3 account or remote engine's source name) so hits
// from a multi-source search stay routable to their origin.
type Result struct {
	listing.Entry
	Bucket string `json:"bucket"`
	Source string `json:"source,omitempty"`
	// Pane marks a pick relayed from a secondary-pane search window
	// (SearchGoto): the hit must navigate the pane the window was scoped
	// to, not the main view. Transport-only — search runs never set it.
	Pane bool `json:"pane,omitempty"`
}

// Match reports whether one object passes the filter. key is the full
// object key (globs are unanchored, so "backup*" also hits nested paths).
// mod may be nil (treated as unknown; age filters skip).
func Match(f Filter, key string, isDir bool, size int64, mod *time.Time, class string, now time.Time) bool {
	if f.Pattern != "" {
		if !matchPattern(f.Pattern, key) {
			return false
		}
	}
	if f.Ext != "" {
		if !matchExt(f.Ext, key) {
			return false
		}
	}
	if f.Path != "" {
		if !matchPath(f.Path, key) {
			return false
		}
	}
	if f.Kind != "" {
		if strings.EqualFold(f.Kind, "dir") != isDir {
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

// matchExt reports whether the key's base name ends with one of the
// comma-separated extensions (case-insensitive; a leading dot is
// optional, so "pdf" and ".pdf" are the same filter).
func matchExt(list, key string) bool {
	base := key
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base = strings.ToLower(base)
	for e := range strings.SplitSeq(list, ",") {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if strings.HasSuffix(base, e) {
			return true
		}
	}
	return false
}

// matchPath reports whether the key's directory part contains the
// substring (case-insensitive): "docs" matches docs/notes.md and
// archive/docs/old.txt alike, while the name filters stay on the base
// name. A key with no slash has an empty directory, so only files under
// something can match a non-empty Path.
func matchPath(sub, key string) bool {
	dir := ""
	if i := strings.LastIndex(key, "/"); i >= 0 {
		dir = key[:i]
	}
	return strings.Contains(strings.ToLower(dir), strings.ToLower(sub))
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
		// A trailing-slash key is an S3 folder placeholder — the only
		// "directory" an object listing ever surfaces, so Kind=dir finds
		// exactly those.
		if !Match(f, key, strings.HasSuffix(key, "/"), aws.ToInt64(o.Size), o.LastModified, string(o.StorageClass), now) {
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

// RunRemote is Run over a remote filesystem tree (local/SFTP/FTP/WebDAV
// engines): every entry under root, depth-first, matched against the same
// filter. Remote trees carry no storage class — entries never match a
// Class filter (the bridge skips remote scopes entirely when one is set).
// Result keys are anchored full paths (directories keep their trailing
// slash), the shape the remote views navigate by.
func RunRemote(ctx context.Context, fs remotefs.FS, root string, f Filter, fn func(Result) error) (Stats, error) {
	st := Stats{}
	now := time.Now()
	err := remotefs.Walk(ctx, fs, root, func(e listing.Entry) error {
		st.Scanned++
		if !Match(f, e.Key, e.IsDir, e.Size, e.LastModified, "", now) {
			return nil
		}
		st.Matched++
		if err := fn(Result{Entry: e}); err != nil {
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
