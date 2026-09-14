package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Drag-out streaming: rows dragged from the grid to the OS are fetched by
// the drop target (Explorer, browsers, …) over a token-gated loopback HTTP
// server. MakeDragUrls registers the selection and returns one URL per
// item; the DownloadURL / text-uri-list drag data carries them. Tokens are
// single-purpose, expire, and only resolve on this machine.

// DragItem is one file to stream out of a source.
type DragItem struct {
	Source string `json:"source"`           // source name/id ("" = view source)
	Bucket string `json:"bucket,omitempty"` // S3 items
	Key    string `json:"key"`              // object key / remote path
	Name   string `json:"name"`             // file name shown to the OS
	Size   int64  `json:"size,omitempty"`
}

type dragEntry struct {
	item    DragItem
	expires time.Time
}

type dragServer struct {
	mu      sync.Mutex
	tokens  map[string]dragEntry
	baseURL string
}

var (
	dragOnce sync.Once
	dragSrv  *dragServer
)

// MakeDragUrls registers a drag selection and returns one loopback URL per
// item, in order. Call it before dragstart fires (selection change); the
// drag data must be set synchronously.
func (a *App) MakeDragUrls(items []DragItem) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	dragOnce.Do(func() { dragSrv = startDragServer(a) })
	if dragSrv == nil || dragSrv.baseURL == "" {
		return nil, fmt.Errorf("drag-out server unavailable")
	}
	urls := make([]string, 0, len(items))
	exp := time.Now().Add(15 * time.Minute)
	dragSrv.mu.Lock()
	defer dragSrv.mu.Unlock()
	// opportunistic cleanup
	for k, e := range dragSrv.tokens {
		if time.Now().After(e.expires) {
			delete(dragSrv.tokens, k)
		}
	}
	for _, it := range items {
		tok := randToken()
		dragSrv.tokens[tok] = dragEntry{item: it, expires: exp}
		urls = append(urls, fmt.Sprintf("%s/d/%s/%s", dragSrv.baseURL, tok, urlSafeName(it.Name)))
	}
	return urls, nil
}

func randToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func urlSafeName(name string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '%':
			return '_'
		}
		return r
	}, name)
}

func startDragServer(a *App) *dragServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return &dragServer{tokens: map[string]dragEntry{}}
	}
	s := &dragServer{tokens: map[string]dragEntry{}, baseURL: "http://" + ln.Addr().String()}
	mux := http.NewServeMux()
	mux.HandleFunc("/d/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/d/"), "/", 2)
		if len(parts) != 2 || parts[0] == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		s.mu.Lock()
		e, ok := s.tokens[parts[0]]
		if ok {
			delete(s.tokens, parts[0]) // one-shot
		}
		s.mu.Unlock()
		if !ok || time.Now().After(e.expires) {
			http.Error(w, "expired drag token", http.StatusGone)
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", e.item.Name))
		if e.item.Size > 0 {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", e.item.Size))
		}
		if err := a.streamDragItem(w, e.item); err != nil {
			// headers may be gone already; nothing else to do
			_ = err
		}
	})
	go http.Serve(ln, mux)
	return s
}

// streamDragItem copies one item from its source into w.
func (a *App) streamDragItem(w io.Writer, it DragItem) error {
	if it.Bucket != "" { // S3 object
		c, err := a.s3ClientFor(it.Source)
		if err != nil {
			return err
		}
		ctx, cancel := a.quickCtx()
		defer cancel()
		out, err := c.S3.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(it.Bucket), Key: aws.String(it.Key),
		})
		if err != nil {
			return err
		}
		defer out.Body.Close()
		_, err = io.Copy(w, out.Body)
		return err
	}
	src, fs, err := a.remoteSource(it.Source)
	if err != nil {
		return err
	}
	// fs is the app-wide cached engine — do NOT close it here. Hold the
	// source's op lock for the whole stream (engines are single-conn).
	unlock := a.lockSrcs(src.ID)
	defer unlock()
	// Drag-out can carry arbitrarily large files; allow a long window but
	// stay cancellable when the app shuts down.
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	rc, _, err := fs.Open(ctx, it.Key)
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(w, rc)
	return err
}
