// webdav.go is the remotefs engine over WebDAV (RFC 4918). It is a minimal
// stdlib-only HTTP client — PROPFIND for metadata/listings, GET/PUT for
// content, MKCOL/MOVE/DELETE for structure — with HTTP Basic auth and an
// anchored root path, so it works against any compliant server (Apache,
// nginx, rclone serve webdav, Nextcloud, IIS) without third-party
// dependencies. webdav speaks plain HTTP (port 80), webdavs HTTPS (443).
package remotefs

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// WebDAV is an FS over one WebDAV endpoint. Stateless (one HTTP client),
// so it is safe for concurrent use.
type WebDAV struct {
	hc   *http.Client
	base *url.URL // scheme://host:port[/root] — Path is "" or "/root" (no trailing slash)
	user string
	pass string
}

// DialWebDAV prepares the engine for src (webdav or webdavs) and verifies
// the endpoint with a root PROPFIND, so a bad host/credentials fail at
// dial time like the other engines.
func DialWebDAV(ctx context.Context, src profile.Source) (FS, error) {
	scheme := "https"
	port := src.Port
	if src.Type == profile.TypeWebDAV {
		scheme = "http"
		if port == 0 {
			port = 80
		}
	} else if port == 0 {
		port = 443
	}
	host := src.Host
	if port != 0 && !strings.Contains(host, ":") { // bare IPv6 hosts carry their own brackets
		host = host + ":" + strconv.Itoa(port)
	}
	root := "/" + strings.Trim(src.Root, "/")
	base := &url.URL{Scheme: scheme, Host: host, Path: strings.TrimSuffix(root, "/")}
	d := &WebDAV{
		hc:   &http.Client{},
		base: base,
		user: src.Username,
		pass: src.Password,
	}
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := d.propfindDepth(dctx, "/", "0"); err != nil {
		return nil, fmt.Errorf("webdav %s: %w", base.Host, davMapDialErr(err))
	}
	return d, nil
}

// davMapDialErr strips the not-exist wrapper: a failed dial probe is a
// connectivity/credentials problem, never "path missing".
func davMapDialErr(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// urlFor builds the request URL for an anchored path under the source
// root; escaping is handled by url.URL.
func (d *WebDAV) urlFor(p string) *url.URL {
	u := *d.base
	u.Path = strings.TrimSuffix(u.Path, "/") + CleanPath(p)
	return &u
}

// davStatusError carries a non-2xx response.
type davStatusError struct {
	method string
	url    string
	code   int
}

func (e *davStatusError) Error() string {
	return fmt.Sprintf("webdav %s %s: HTTP %d", e.method, e.url, e.code)
}

// do issues one request with auth attached; the caller closes the body.
func (d *WebDAV) do(ctx context.Context, method string, u *url.URL, body io.Reader, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if d.user != "" {
		req.SetBasicAuth(d.user, d.pass)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, &davStatusError{method: method, url: u.Path, code: resp.StatusCode}
	}
	return resp, nil
}

// --- PROPFIND plumbing (RFC 4918 §9.1, multistatus §9.1.2) ---

type davProp struct {
	GetContentLength int64  `xml:"getcontentlength"`
	GetLastModified  string `xml:"getlastmodified"`
	ResourceType     struct {
		Inner string `xml:",innerxml"`
	} `xml:"resourcetype"`
}

type davResponse struct {
	Href     string `xml:"href"`
	Propstat []struct {
		Status string  `xml:"status"`
		Prop   davProp `xml:"prop"`
	} `xml:"propstat"`
}

type davMultistatus struct {
	XMLName  xml.Name      `xml:"multistatus"`
	Response []davResponse `xml:"response"`
}

// propfindDepth runs PROPFIND with the wanted Depth header and decodes the
// multistatus body ("0" for one resource, "1" for a directory view).
func (d *WebDAV) propfindDepth(ctx context.Context, p, depth string) ([]davResponse, error) {
	resp, err := d.do(ctx, "PROPFIND", d.urlFor(p), nil, map[string]string{"Depth": depth})
	if err != nil {
		return nil, davNotExist(p, err)
	}
	defer resp.Body.Close()
	var ms davMultistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("propfind %s: %w", CleanPath(p), err)
	}
	return ms.Response, nil
}

// davHrefPath unescapes a response href onto an anchored server path and
// strips the source root prefix, so entries compare against CleanPath
// space ("/sub/file.txt").
func (d *WebDAV) davHrefPath(href string) string {
	h, err := url.Parse(href)
	if err != nil {
		return strings.TrimSuffix(href, "/")
	}
	p := h.Path
	root := strings.TrimSuffix(d.base.Path, "/")
	if root != "" && strings.HasPrefix(p, root) {
		p = strings.TrimPrefix(p, root)
	}
	if p == "" {
		return "/"
	}
	return p
}

// entryOf converts one multistatus response into a listing.Entry; dir is
// the anchored directory the response belongs to.
func (d *WebDAV) entryOf(dir string, r davResponse) (listing.Entry, bool) {
	var prop davProp
	found := false
	for _, ps := range r.Propstat {
		if strings.Contains(ps.Status, " 200 ") {
			prop = ps.Prop
			found = true
		}
	}
	if !found && len(r.Propstat) > 0 {
		prop = r.Propstat[0].Prop // servers that omit the status line
	}
	hrefPath := d.davHrefPath(r.Href)
	trimmed := strings.TrimSuffix(hrefPath, "/")
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return listing.Entry{}, false
	}
	// collection detection: the resourcetype XML contains a <collection/>
	// element, and (belt and braces) collection hrefs end with a slash.
	isDir := strings.Contains(prop.ResourceType.Inner, "collection") || strings.HasSuffix(hrefPath, "/")
	name := path.Base(trimmed)
	t := parseDavTime(prop.GetLastModified)
	out := listing.Entry{
		Key:   Key(dir, name, isDir),
		Name:  name,
		IsDir: isDir,
		Size:  prop.GetContentLength,
	}
	if !t.IsZero() {
		out.LastModified = &t
	}
	return out, true
}

// parseDavTime accepts the three timestamp shapes servers send.
func parseDavTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{http.TimeFormat, time.RFC1123Z, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// davNotExist maps a 404 status error onto a PathError over fs.ErrNotExist
// (same contract as the local/sftp/ftp engines).
func davNotExist(p string, err error) error {
	var se *davStatusError
	if errors.As(err, &se) && (se.code == 404 || se.code == 410) {
		return &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
	}
	return err
}

// --- FS contract ---

func (d *WebDAV) List(ctx context.Context, dir string) ([]listing.Entry, error) {
	cleaned := CleanPath(dir)
	// Directory PROPFINDs carry a trailing slash by convention; some
	// servers (Apache, IIS) 301-redirect slashless collection URLs, and
	// a followed redirect would degrade PROPFIND into a GET.
	responses, err := d.propfindDepth(ctx, strings.TrimSuffix(cleaned, "/")+"/", "1")
	if err != nil {
		return nil, err
	}
	self := strings.TrimSuffix(cleaned, "/")
	entries := make([]listing.Entry, 0, len(responses))
	for _, r := range responses {
		hrefPath := strings.TrimSuffix(d.davHrefPath(r.Href), "/")
		if hrefPath == self { // the directory itself, first in the multistatus
			continue
		}
		if e, ok := d.entryOf(cleaned, r); ok {
			entries = append(entries, e)
		}
	}
	listing.SortEntries(entries)
	return entries, nil
}

func (d *WebDAV) Stat(ctx context.Context, p string) (listing.Entry, error) {
	cleaned := CleanPath(p)
	responses, err := d.propfindDepth(ctx, cleaned, "0")
	if err != nil {
		return listing.Entry{}, err
	}
	for _, r := range responses {
		hrefPath := strings.TrimSuffix(d.davHrefPath(r.Href), "/")
		if hrefPath == strings.TrimSuffix(cleaned, "/") {
			e, ok := d.entryOf(path.Dir(cleaned), r)
			if !ok {
				break
			}
			e.Name = path.Base(strings.TrimSuffix(cleaned, "/"))
			return e, nil
		}
	}
	// no matching response: treat like a missing path
	return listing.Entry{}, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
}

func (d *WebDAV) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	resp, err := d.do(ctx, "GET", d.urlFor(p), nil, nil)
	if err != nil {
		return nil, 0, davNotExist(p, err)
	}
	size := resp.ContentLength
	if size < 0 {
		size = 0 // chunked transfer: unknown, callers stream
	}
	return resp.Body, size, nil
}

func (d *WebDAV) Create(ctx context.Context, p string, r io.Reader) error {
	resp, err := d.do(ctx, "PUT", d.urlFor(p), r, map[string]string{"Content-Type": "application/octet-stream"})
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

// MkdirAll creates dir segment by segment with MKCOL; 405 means the
// collection already exists (RFC 4918 §9.3.1) and is skipped.
func (d *WebDAV) MkdirAll(ctx context.Context, dir string) error {
	cleaned := CleanPath(dir)
	if cleaned == "/" {
		return nil
	}
	cur := ""
	for _, seg := range strings.Split(strings.Trim(cleaned, "/"), "/") {
		cur += "/" + seg
		resp, err := d.do(ctx, "MKCOL", d.urlFor(cur), nil, nil)
		if err != nil {
			var se *davStatusError
			if errors.As(err, &se) && se.code == 405 {
				continue // exists
			}
			return fmt.Errorf("mkcol %s: %w", cur, davNotExist(cur, err))
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	return nil
}

// Remove deletes one file or a whole collection tree (a WebDAV DELETE on
// a collection removes all members — RFC 4918 §9.6.1).
func (d *WebDAV) Remove(ctx context.Context, p string) error {
	cleaned := CleanPath(p)
	if cleaned == "/" {
		return fmt.Errorf("refusing to remove the source root")
	}
	resp, err := d.do(ctx, "DELETE", d.urlFor(cleaned), nil, nil)
	if err != nil {
		return davNotExist(cleaned, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

// Rename moves within the server with MOVE (one request, no body); the
// Destination must be an absolute URI and may overwrite the target,
// matching the overwrite semantics of the local/sftp/ftp engines.
func (d *WebDAV) Rename(ctx context.Context, oldp, newp string) error {
	if CleanPath(newp) == "/" {
		return fmt.Errorf("invalid target")
	}
	resp, err := d.do(ctx, "MOVE", d.urlFor(oldp), nil, map[string]string{
		"Destination": d.urlFor(newp).String(),
		"Overwrite":   "T",
	})
	if err != nil {
		return davNotExist(oldp, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

// Close is a no-op: the engine is stateless HTTP.
func (d *WebDAV) Close() error { return nil }
