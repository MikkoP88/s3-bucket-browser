// search.go exposes cancelable deep search to the GUI: matches stream as
// page events and a done event terminates the run (M5, PLAN.md §8.9).
package api

import (
	"context"
	"fmt"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/search"
)

// Wails event names for streaming search results.
const (
	EventSearchPage = "search:page" // payload: SearchPage
	EventSearchDone = "search:done" // payload: SearchDone
)

const searchPageSize = 500

// SearchOptions is the frontend-facing filter. Durations arrive as seconds
// (time.Duration does not cross the JS bridge cleanly).
type SearchOptions struct {
	Pattern      string `json:"pattern"`
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
		LargerThan:  o.LargerThan,
		SmallerThan: o.SmallerThan,
		OlderThan:   time.Duration(o.OlderThanSec) * time.Second,
		NewerThan:   time.Duration(o.NewerThanSec) * time.Second,
		Class:       o.Class,
		Limit:       o.Limit,
	}
}

// SearchPage is one streamed batch of matches.
type SearchPage struct {
	Token   string          `json:"token"`
	Entries []search.Result `json:"entries"`
	Matched int             `json:"matched"` // running total
}

// SearchDone terminates a search (stats always set; error non-empty on
// failure — cancellation reports empty error).
type SearchDone struct {
	Token   string `json:"token"`
	Scanned int    `json:"scanned"`
	Matched int    `json:"matched"`
	Error   string `json:"error,omitempty"`
}

// DeepSearch starts a cancelable deep search under bucket/prefix and
// returns immediately with a token. Matches stream via EventSearchPage and
// the run terminates with EventSearchDone.
func (a *App) DeepSearch(bucket, prefix string, opts SearchOptions) (string, error) {
	c, err := a.client("")
	if err != nil {
		return "", err
	}
	if a.ctx == nil {
		return "", errNoContext
	}
	token := fmt.Sprintf("s%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(a.ctx)
	a.searchMu.Lock()
	a.searches[token] = cancel
	a.searchMu.Unlock()

	go func() {
		defer func() {
			cancel()
			a.searchMu.Lock()
			delete(a.searches, token)
			a.searchMu.Unlock()
		}()
		matched := 0
		batch := make([]search.Result, 0, searchPageSize)
		stats, err := search.Run(ctx, c.S3, bucket, dirPrefix(prefix), opts.filter(), func(r search.Result) error {
			batch = append(batch, r)
			matched++
			if len(batch) >= searchPageSize {
				a.emit(EventSearchPage, SearchPage{Token: token, Entries: batch, Matched: matched})
				batch = make([]search.Result, 0, searchPageSize)
			}
			return nil
		})
		if len(batch) > 0 {
			a.emit(EventSearchPage, SearchPage{Token: token, Entries: batch, Matched: matched})
		}
		done := SearchDone{Token: token, Scanned: stats.Scanned, Matched: stats.Matched}
		if err != nil && ctx.Err() == nil {
			done.Error = err.Error()
		}
		a.emit(EventSearchDone, done)
	}()
	return token, nil
}

// CancelSearch aborts a running deep search (unknown tokens are a no-op).
func (a *App) CancelSearch(token string) {
	a.searchMu.Lock()
	if cancel, ok := a.searches[token]; ok {
		cancel()
		delete(a.searches, token)
	}
	a.searchMu.Unlock()
}
