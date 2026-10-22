// shares.go: the session's share registry — every pre-signed URL the
// user chose to keep sharing, in one list the Shares window reads. A
// share is created from the Pre-signed URL dialog (which owns the
// duration), listed live in the View → Shares window, and stopped from
// either seat. The protocol's own law is labeled everywhere it matters:
// a pre-signed URL is signed at creation and cannot be revoked remotely
// — stopping removes the record, scrubs the local clipboard, and the URL
// itself dies at its expiry. Short durations are the honest mitigation
// for sensitive objects, so the dialog offers them first.
package api

import (
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/s3client"
)

// EventSharesChanged fires whenever the session's share list changes
// (created, stopped) — the Shares window re-renders and the View menu's
// live count rides it.
const EventSharesChanged = "shares:changed"

// ShareInfo is one entry in the session's share list. Kind "presign" is
// a pre-signed GET URL.
type ShareInfo struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // "presign"
	Source  string `json:"source,omitempty"`
	Bucket  string `json:"bucket"`
	Key     string `json:"key"`
	URL     string `json:"url"`
	Created string `json:"created"` // RFC3339
	Expires string `json:"expires"` // RFC3339
	Expired bool   `json:"expired"` // recomputed at list time
}

type shareEntry struct {
	info    ShareInfo
	expires time.Time
}

// presignGet mints a pre-signed GET URL and reports the TTL actually
// granted (the clamps are the law: <=0 becomes 1h, over 7d becomes 7d —
// the S3 presign maximum).
func (a *App) presignGet(c *s3client.Client, bucket, key string, ttlSeconds int) (string, int, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	if ttlSeconds > 7*24*3600 {
		ttlSeconds = 7 * 24 * 3600
	}
	presigner := s3.NewPresignClient(c.S3)
	req, err := presigner.PresignGetObject(a.ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	}, s3.WithPresignExpires(time.Duration(ttlSeconds)*time.Second))
	if err != nil {
		return "", 0, err
	}
	return req.URL, ttlSeconds, nil
}

// CreateShare mints a pre-signed GET URL through the named source (""
// = the view-source resolution, exactly client("")) and records it in
// the session's share list.
func (a *App) CreateShare(source, bucket, key string, ttlSeconds int) (ShareInfo, error) {
	c, err := a.client(source)
	if err != nil {
		return ShareInfo{}, err
	}
	url, ttl, err := a.presignGet(c, bucket, key, ttlSeconds)
	if err != nil {
		return ShareInfo{}, err
	}
	name := source
	if src, ok := a.gateSource(source); ok {
		name = src.Name
	}
	now := time.Now()
	a.shareMu.Lock()
	a.shareSeq++
	e := &shareEntry{
		info: ShareInfo{
			ID:      fmt.Sprintf("sh-%d", a.shareSeq),
			Kind:    "presign",
			Source:  name,
			Bucket:  bucket,
			Key:     key,
			URL:     url,
			Created: now.Format(time.RFC3339),
			Expires: now.Add(time.Duration(ttl) * time.Second).Format(time.RFC3339),
		},
		expires: now.Add(time.Duration(ttl) * time.Second),
	}
	a.shares[e.info.ID] = e
	a.shareMu.Unlock()
	a.emitLogSrc(LogInfo, "share", bucket, fmt.Sprintf("shared GET %s for %s (%s)", key, ttlLabel(ttl), e.info.ID))
	a.emit(EventSharesChanged)
	return e.info, nil
}

// ListShares answers the session's shares, newest first, with expiry
// recomputed against the clock.
func (a *App) ListShares() []ShareInfo {
	a.shareMu.Lock()
	defer a.shareMu.Unlock()
	out := make([]ShareInfo, 0, len(a.shares))
	now := time.Now()
	for _, e := range a.shares {
		i := e.info
		i.Expired = now.After(e.expires)
		out = append(out, i)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created > out[j].Created
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// StopShare removes a share from the list and scrubs the local clipboard
// if it still carries the URL (a newer copy wins, as every scrub law
// holds). A stopped presign URL keeps working for anyone who saved it
// until it expires — the protocol has no revocation, which the Shares
// window states plainly.
func (a *App) StopShare(id string) error {
	a.shareMu.Lock()
	e, ok := a.shares[id]
	if ok {
		delete(a.shares, id)
	}
	a.shareMu.Unlock()
	if !ok {
		return fmt.Errorf("%w: share %s", profile.ErrNotFound, id)
	}
	a.scrubClipboard(e.info.URL)
	a.emitLogSrc(LogInfo, "share", e.info.Bucket, fmt.Sprintf("stopped share %s (%s)", id, e.info.Key))
	a.emit(EventSharesChanged)
	return nil
}
