package api

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// --- WinSCP-style "Copy URL" ---------------------------------------------
//
// The real address of a selection, as opposed to the s3:// URI (a
// CLI-interop identifier carrying no endpoint). Every form names the
// where and never the may: no signature, no password.

// ObjectUrls returns the real addresses of keys in a bucket of the source
// the main view is browsing: the endpoint-resolved HTTPS URL, honoring the
// source's custom endpoint and addressing style. It is the presigner's URL
// minus the signature query — exactly the address PresignObject grants
// access to, so the copy can never drift from how the app itself addresses
// the object.
func (a *App) ObjectUrls(bucket string, keys []string) ([]string, error) {
	return a.objectUrls("", bucket, keys)
}

// SourceObjectUrls is ObjectUrls pinned to one named S3 source (the side
// pane's binding, which may differ from the view source).
func (a *App) SourceObjectUrls(idOrName, bucket string, keys []string) ([]string, error) {
	return a.objectUrls(idOrName, bucket, keys)
}

func (a *App) objectUrls(idOrName, bucket string, keys []string) ([]string, error) {
	c, err := a.s3ClientFor(idOrName)
	if err != nil {
		return nil, err
	}
	presigner := s3.NewPresignClient(c.S3)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		req, err := presigner.PresignGetObject(a.ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket), Key: aws.String(k),
		}, s3.WithPresignExpires(time.Hour))
		if err != nil {
			return nil, fmt.Errorf("resolving %s: %w", k, err)
		}
		u := req.URL
		if i := strings.IndexByte(u, '?'); i >= 0 {
			u = u[:i] // the query IS the grant; the address ends before it
		}
		out = append(out, u)
	}
	a.emitLogSrc(LogInfo, "share", bucket, fmt.Sprintf("copied URL of %d object(s)", len(keys)))
	return out, nil
}

// RemoteUrls returns the real addresses of paths of a non-S3 source:
// scheme://user@host[:port]/server-path for remote filesystems (the
// server path joins the source's root with the anchored path, since "/"
// in the GUI is the configured root, not the server root) and file:///
// paths for local-root sources. Default ports are elided; the password
// never appears. Built purely from the stored source — no connection is
// made.
func (a *App) RemoteUrls(idOrName string, paths []string) ([]string, error) {
	src, err := a.sourceByIDOrName(idOrName)
	if err != nil {
		return nil, err
	}
	if src.Type == profile.TypeS3 {
		return nil, fmt.Errorf("source %q is S3 — it is browsed through the S3 pipeline", src.Name)
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if src.Type == profile.TypeLocal {
			out = append(out, fileUrl(rooted(src.LocalRoot, p)))
			continue
		}
		u := url.URL{
			Scheme: src.Type,
			User:   url.User(src.Username),
			Host:   hostPort(src),
			Path:   rooted(src.Root, p),
		}
		out = append(out, u.String())
	}
	return out, nil
}

// rooted joins a source root (Root for "/ "-anchored remotes, LocalRoot
// for local sources) with an anchored GUI path: the address a peer would
// dial. Directory keys keep their trailing slash — a folder URL names the
// folder. Windows local roots become slash paths.
func rooted(root, p string) string {
	base := strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(root), "\\", "/"), "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if base == "" {
		return p
	}
	return base + p
}

// hostPort renders host[:port] — the port only when it deviates from the
// type's conventional default.
func hostPort(src profile.Source) string {
	if src.Port != 0 && src.Port != src.DefaultPort() {
		return net.JoinHostPort(src.Host, strconv.Itoa(src.Port))
	}
	return src.Host
}

// fileUrl renders a slash path as a file URL: C:/x → file:///C:/x, a UNC
// //server/share → file://server/share (authority form).
func fileUrl(p string) string {
	if strings.HasPrefix(p, "//") {
		return "file:" + p
	}
	return "file:///" + strings.TrimLeft(p, "/")
}
