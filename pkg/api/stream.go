// stream.go: memory-bounded streaming directory listing for the GUI
// (M5 performance pass, PLAN.md §13). Pages of entries flow to the
// frontend as events; the Go side never accumulates the folder, so a
// million-object prefix costs one paginator page of memory.
package api

import (
	"context"
	"fmt"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
)

// EventListPage streams listing pages; the final page carries Done=true.
const EventListPage = "list:page"

const listPageSize = 1000

// ListPage is one streamed chunk of a directory view.
type ListPage struct {
	Token   string          `json:"token"`
	Bucket  string          `json:"bucket"`
	Prefix  string          `json:"prefix"`
	Entries []listing.Entry `json:"entries"`
	Total   int             `json:"total"` // running count
	Done    bool            `json:"done"`
	Error   string          `json:"error,omitempty"`
}

// ListObjectsStream starts a streaming directory listing of bucket/prefix
// and returns a token immediately. Entries arrive via EventListPage; the
// stream always terminates with a Done=true page. Cancel with
// CancelList(token) — e.g. when navigation moved on.
func (a *App) ListObjectsStream(bucket, prefix string) (string, error) {
	c, err := a.client("")
	if err != nil {
		return "", err
	}
	if a.ctx == nil {
		return "", errNoContext
	}
	prefix = dirPrefix(prefix)
	token := fmt.Sprintf("l%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(a.ctx)
	a.streamMu.Lock()
	a.streams[token] = cancel
	a.streamMu.Unlock()

	go func() {
		defer func() {
			cancel()
			a.streamMu.Lock()
			delete(a.streams, token)
			a.streamMu.Unlock()
		}()
		total := 0
		batch := make([]listing.Entry, 0, listPageSize)
		emit := func(done bool, errMsg string) {
			a.emit(EventListPage, ListPage{
				Token: token, Bucket: bucket, Prefix: prefix,
				Entries: batch, Total: total, Done: done, Error: errMsg,
			})
			batch = make([]listing.Entry, 0, listPageSize)
		}
		err := listing.WalkDir(ctx, c.S3, bucket, prefix, listing.Options{}, func(e listing.Entry) error {
			batch = append(batch, e)
			total++
			if len(batch) >= listPageSize {
				emit(false, "")
			}
			return nil
		})
		msg := ""
		if err != nil && ctx.Err() == nil {
			msg = err.Error()
		}
		emit(true, msg)
	}()
	return token, nil
}

// CancelList aborts a running listing stream (unknown tokens are a no-op).
func (a *App) CancelList(token string) {
	a.streamMu.Lock()
	if cancel, ok := a.streams[token]; ok {
		cancel()
		delete(a.streams, token)
	}
	a.streamMu.Unlock()
}
