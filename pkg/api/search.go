// search.go exposes cancelable search to the GUI: one S3 bucket/prefix,
// one remote source path, or every data source from its root. Matches
// stream as page events and a done event terminates the run (M5).
package api

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/remotefs"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/search"
	"github.com/aws/aws-sdk-go-v2/aws"
)

// Wails event names for streaming search results and relaying a result
// picked in a floating search window back to the main window.
const (
	EventSearchPage = "search:page" // payload: SearchPage
	EventSearchDone = "search:done" // payload: SearchDone
	EventSearchOpen = "search:open" // payload: search.Result (SearchGoto relay)
)

const searchPageSize = 500

// SearchOptions is the frontend-facing filter. Durations arrive as seconds
// (time.Duration does not cross the JS bridge cleanly).
type SearchOptions struct {
	Pattern      string `json:"pattern"`
	Kind         string `json:"kind"`        // "" any, "file", "dir"
	Ext          string `json:"ext"`         // comma-separated name extensions — global, every source type
	Path         string `json:"path"`        // substring the parent directory must contain — global
	LargerThan   int64  `json:"largerThan"`  // bytes
	SmallerThan  int64  `json:"smallerThan"` // bytes
	OlderThanSec int64  `json:"olderThanSec"`
	NewerThanSec int64  `json:"newerThanSec"`
	Class        string `json:"class"`
	Limit        int    `json:"limit"`
}

func (o SearchOptions) filter() search.Filter {
	return search.Filter{
		Pattern:     o.Pattern,
		Kind:        o.Kind,
		Ext:         o.Ext,
		Path:        o.Path,
		LargerThan:  o.LargerThan,
		SmallerThan: o.SmallerThan,
		OlderThan:   time.Duration(o.OlderThanSec) * time.Second,
		NewerThan:   time.Duration(o.NewerThanSec) * time.Second,
		Class:       o.Class,
		Limit:       o.Limit,
	}
}

// SearchScope picks where Search runs. Mode "all" (the default, also "")
// searches every data source: each S3 source's every bucket, each remote
// engine from its root. Mode "s3" walks one bucket/prefix of one S3 source
// (Bucket "" = every bucket of the source; Source "" = the source the main
// view is browsing). Mode "remote" walks one remote source from Path
// (default "/"). Mode "local" walks the workstation's filesystem under
// Prefix ("" or "~" = the home directory) — the secondary pane's own
// search scope. Hit keys are absolute local paths, the shape the pane's
// rows navigate by.
type SearchScope struct {
	Mode   string `json:"mode"`
	Source string `json:"source"`
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
}

// SearchPage is one streamed batch of matches.
type SearchPage struct {
	Token   string          `json:"token"`
	Entries []search.Result `json:"entries"`
	Matched int             `json:"matched"` // running total
}

// SearchDone terminates a search (stats always set; error non-empty on
// failure — cancellation reports empty error). SourceErrors carries
// per-source failures of an all-sources run that searched the rest anyway.
type SearchDone struct {
	Token        string `json:"token"`
	Scanned      int    `json:"scanned"`
	Matched      int    `json:"matched"`
	Sources      int    `json:"sources"` // sources actually searched
	Skipped      int    `json:"skipped"` // remote sources skipped (class filter)
	SourceErrors string `json:"sourceErrors,omitempty"`
	Error        string `json:"error,omitempty"`
}

// searchJob is one source's slice of a run: resolved lazily so a dead
// source is reported and the rest still searched.
type searchJob struct {
	name   string // source name (routing + per-source errors)
	s3     bool
	local  bool // local mode: prefix is an absolute directory, not a source
	bucket string // s3 scoped mode: one bucket; "": every bucket
	prefix string // s3 prefix | remote root path | local absolute directory
}

// Search starts a cancelable search and returns immediately with a token.
// Matches stream via EventSearchPage and the run terminates with
// EventSearchDone.
func (a *App) Search(scope SearchScope, opts SearchOptions) (string, error) {
	if a.ctx == nil {
		return "", errNoContext
	}
	var jobs []searchJob
	skipped := 0
	switch scope.Mode {
	case "", "all":
		for _, src := range a.workspaceSources() {
			if src.Type == profile.TypeS3 && src.S3 != nil {
				jobs = append(jobs, searchJob{name: src.Name, s3: true})
				continue
			}
			// Remote trees carry no storage class — a class filter can
			// never match there, so they are not walked at all.
			if opts.Class != "" {
				skipped++
				continue
			}
			jobs = append(jobs, searchJob{name: src.Name, prefix: "/"})
		}
	case "s3": // Bucket "" walks every bucket of the named source
		if _, err := a.s3ClientFor(scope.Source); err != nil { // validates sync
			return "", err
		}
		name := scope.Source
		if name == "" {
			name = a.currentViewSource()
		} else if src, err := a.sourceByIDOrName(name); err == nil {
			name = src.Name // canonical name for routing
		}
		jobs = append(jobs, searchJob{name: name, s3: true, bucket: scope.Bucket, prefix: scope.Prefix})
	case "remote":
		src, _, err := a.remoteSource(scope.Source) // validates sync
		if err != nil {
			return "", err
		}
		if opts.Class != "" {
			return "", errors.New("remote sources carry no storage class — drop the class filter or search S3 only")
		}
		root := scope.Prefix
		if root == "" {
			root = "/"
		}
		jobs = append(jobs, searchJob{name: src.Name, prefix: root})
	case "local": // the workstation's filesystem under one directory
		if opts.Class != "" {
			return "", errors.New("local files carry no storage class — drop the class filter or search S3 only")
		}
		root := scope.Prefix
		if root == "" || root == "~" {
			home, err := a.LocalHome()
			if err != nil {
				return "", err
			}
			root = home
		}
		jobs = append(jobs, searchJob{name: "local", local: true, prefix: filepath.Clean(root)})
	default:
		return "", fmt.Errorf("unknown search scope %q", scope.Mode)
	}

	token := fmt.Sprintf("s%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(a.ctx)
	a.searchMu.Lock()
	a.searches[token] = cancel
	a.searchMu.Unlock()

	label := fmt.Sprintf("%q — all sources", opts.Pattern)
	if scope.Mode == "s3" {
		if scope.Bucket == "" {
			label = fmt.Sprintf("%q — %s (all buckets)", opts.Pattern, jobs[0].name)
		} else {
			label = fmt.Sprintf("%q — s3://%s/%s", opts.Pattern, scope.Bucket, dirPrefix(scope.Prefix))
		}
	} else if scope.Mode == "remote" {
		if p := strings.Trim(jobs[0].prefix, "/"); p != "" {
			label = fmt.Sprintf("%q — %s/%s", opts.Pattern, jobs[0].name, p)
		} else {
			label = fmt.Sprintf("%q — %s", opts.Pattern, jobs[0].name)
		}
	} else if scope.Mode == "local" {
		label = fmt.Sprintf("%q — %s", opts.Pattern, jobs[0].prefix)
	}
	// The search rides the unified task registry under its token, so
	// CancelTask and the search window's own cancel agree on one ID.
	task := a.tasks.addWithID(token, "search", label)

	filt := opts.filter()
	go func() {
		defer func() {
			cancel()
			a.searchMu.Lock()
			delete(a.searches, token)
			a.searchMu.Unlock()
		}()
		matched := 0
		scanned := 0
		batch := make([]search.Result, 0, searchPageSize)
		onHit := func(source string) func(search.Result) error {
			return func(r search.Result) error {
				r.Source = source
				batch = append(batch, r)
				matched++
				if len(batch) >= searchPageSize {
					a.emit(EventSearchPage, SearchPage{Token: token, Entries: batch, Matched: matched})
					batch = make([]search.Result, 0, searchPageSize)
					task.progress(matched) // live match count on the task row
				}
				return nil
			}
		}
		searched := map[string]bool{}
		var srcErrs []string
		for _, j := range jobs {
			if ctx.Err() != nil {
				break
			}
			if filt.Limit > 0 && matched >= filt.Limit {
				break
			}
			if j.s3 {
				c, err := a.client(j.name)
				if err != nil {
					srcErrs = append(srcErrs, fmt.Sprintf("%s: %v", j.name, err))
					continue
				}
				buckets := []string{j.bucket}
				if j.bucket == "" {
					names, err := listing.ListBuckets(ctx, c.S3)
					if err != nil {
						srcErrs = append(srcErrs, fmt.Sprintf("%s: %v", j.name, err))
						continue
					}
					buckets = buckets[:0]
					for _, b := range names {
						buckets = append(buckets, aws.ToString(b.Name))
					}
				}
				for _, b := range buckets {
					if ctx.Err() != nil || (filt.Limit > 0 && matched >= filt.Limit) {
						break
					}
					searched[j.name] = true
					st, err := search.Run(ctx, c.S3, b, "", filt, onHit(j.name))
					scanned += st.Scanned
					if err != nil && ctx.Err() == nil {
						srcErrs = append(srcErrs, fmt.Sprintf("%s: %v", j.name, err))
					}
				}
				continue
			}
			if j.local {
				// workstation filesystem: the FS is rooted at the directory
				// itself, so the walk starts at its root and every hit key
				// is re-anchored onto the absolute path the local pane
				// navigates by (no trailing separator — IsDir carries
				// dirness, matching ListLocal row paths). Not a data
				// source: hits carry no Source.
				fs, err := remotefs.NewLocal(j.prefix)
				if err != nil {
					srcErrs = append(srcErrs, fmt.Sprintf("%s: %v", j.prefix, err))
					continue
				}
				searched["local"] = true
				hit := onHit("")
				st, err := search.RunRemote(ctx, fs, "/", filt, func(r search.Result) error {
					r.Entry.Key = filepath.Join(j.prefix, filepath.FromSlash(r.Entry.Key))
					return hit(r)
				})
				scanned += st.Scanned
				if err != nil && ctx.Err() == nil {
					srcErrs = append(srcErrs, fmt.Sprintf("%s: %v", j.prefix, err))
				}
				continue
			}
			// remote engine: one walk under the source's operation lock —
			// the engine serialization contract every remote op honors
			src, fs, err := a.remoteSource(j.name)
			if err != nil {
				srcErrs = append(srcErrs, fmt.Sprintf("%s: %v", j.name, err))
				continue
			}
			lock := a.srcLock(src.ID)
			lock.Lock()
			st, err := search.RunRemote(ctx, fs, j.prefix, filt, onHit(j.name))
			lock.Unlock()
			scanned += st.Scanned
			searched[j.name] = true
			if err != nil && ctx.Err() == nil {
				srcErrs = append(srcErrs, fmt.Sprintf("%s: %v", j.name, err))
			}
		}
		if len(batch) > 0 {
			a.emit(EventSearchPage, SearchPage{Token: token, Entries: batch, Matched: matched})
		}
		done := SearchDone{Token: token, Scanned: scanned, Matched: matched, Sources: len(searched), Skipped: skipped}
		if len(srcErrs) > 0 {
			done.SourceErrors = strings.Join(srcErrs, "; ")
		}
		task.progress(matched)
		task.finish(nil, false)
		a.emit(EventSearchDone, done)
	}()
	return token, nil
}

// SearchGoto relays a result picked in a floating search window to the
// main window: navigation is a frontend concern, and the app-wide event
// bus is the only channel that crosses OS windows.
func (a *App) SearchGoto(hit search.Result) {
	a.emit(EventSearchOpen, hit)
}

// CancelSearch aborts a running search (unknown tokens are a no-op).
func (a *App) CancelSearch(token string) {
	a.searchMu.Lock()
	if cancel, ok := a.searches[token]; ok {
		cancel()
		delete(a.searches, token)
	}
	a.searchMu.Unlock()
}
