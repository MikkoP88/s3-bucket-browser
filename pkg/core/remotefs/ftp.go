// ftp.go is the remotefs engine over FTP and FTPS. FTPS comes in two
// flavors: implicit TLS (the legacy dedicated port, default 990) and
// explicit AUTH TLS on the plain port (usually 21) — the engine picks
// implicit when the resolved port is 990, explicit otherwise.
//
// FTP listings depend on server capabilities (MLSD vs LIST parsing), so
// timestamps can be coarse and hidden files appear or not per server
// policy; the engine reports whatever the server lists, nothing more.
package remotefs

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	ftp "github.com/jlaffaye/ftp"
)

// FTP is an FS over one FTP(S) control connection. Not safe for
// concurrent use (one data connection at a time).
type FTP struct {
	c    *ftp.ServerConn
	root string // absolute remote path of the source root (no trailing slash)
}

// DialFTP connects to src (ftp or ftps) and anchors "/" at src.Root (the
// login directory when Root is empty).
func DialFTP(ctx context.Context, src profile.Source) (FS, error) {
	port := src.Port
	if port == 0 {
		port = src.DefaultPort()
	}
	addr := net.JoinHostPort(src.Host, strconv.Itoa(port))
	user := src.Username
	if user == "" {
		user = "anonymous"
	}

	opts := []ftp.DialOption{
		ftp.DialWithContext(ctx),
		ftp.DialWithTimeout(15 * time.Second),
		ftp.DialWithShutTimeout(15 * time.Second),
	}
	var conn *ftp.ServerConn
	var err error
	if src.Type == profile.TypeFTPS {
		tlsCfg := &tls.Config{ServerName: src.Host}
		if port == 990 { // implicit TLS on the dedicated port
			conn, err = ftp.Dial(addr, append(opts, ftp.DialWithTLS(tlsCfg))...)
		} else { // explicit AUTH TLS upgrade on the plain port
			conn, err = ftp.Dial(addr, append(opts, ftp.DialWithExplicitTLS(tlsCfg))...)
		}
	} else {
		conn, err = ftp.Dial(addr, opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("ftp %s: %w", addr, err)
	}
	if err := conn.Login(user, src.Password); err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("ftp %s login %q: %w", addr, user, err)
	}

	root := strings.TrimRight(src.Root, "/")
	if root == "" {
		if wd, err := conn.CurrentDir(); err == nil && wd != "" {
			root = strings.TrimRight(wd, "/")
		}
	}
	if root != "" && !strings.HasPrefix(root, "/") {
		root = "/" + root
	}
	return &FTP{c: conn, root: root}, nil
}

// abs maps an anchored path onto the remote filesystem.
func (f *FTP) abs(p string) string {
	cleaned := CleanPath(p)
	if f.root == "" {
		return cleaned
	}
	if cleaned == "/" {
		return f.root
	}
	return f.root + cleaned
}

func ftpEntry(dir string, e *ftp.Entry) listing.Entry {
	isDir := e.Type == ftp.EntryTypeFolder
	out := listing.Entry{
		Key:   Key(dir, e.Name, isDir),
		Name:  e.Name,
		IsDir: isDir,
		Size:  int64(e.Size),
	}
	if !e.Time.IsZero() {
		t := e.Time
		out.LastModified = &t
	}
	return out
}

func (f *FTP) List(ctx context.Context, dir string) ([]listing.Entry, error) {
	raw, err := f.c.List(f.abs(dir))
	if err != nil {
		return nil, err
	}
	parent := CleanPath(dir)
	entries := make([]listing.Entry, 0, len(raw))
	for _, e := range raw {
		if e.Name == "." || e.Name == ".." || e.Name == "" {
			continue
		}
		entries = append(entries, ftpEntry(parent, e))
	}
	listing.SortEntries(entries)
	return entries, nil
}

func (f *FTP) Stat(ctx context.Context, p string) (listing.Entry, error) {
	cleaned := CleanPath(p)
	e, err := f.c.GetEntry(f.abs(cleaned))
	if err != nil {
		return listing.Entry{}, err
	}
	name := path.Base(strings.TrimSuffix(cleaned, "/"))
	entry := ftpEntry(path.Dir(cleaned), e)
	entry.Name = name
	return entry, nil
}

func (f *FTP) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	resp, err := f.c.Retr(f.abs(p))
	if err != nil {
		return nil, 0, err
	}
	size, err := f.c.FileSize(f.abs(p))
	if err != nil {
		size = 0 // server refused SIZE (ASCII mode); stream without it
	}
	return resp, size, nil
}

func (f *FTP) Create(ctx context.Context, p string, r io.Reader) error {
	return f.c.Stor(f.abs(p), r)
}

// MkdirAll creates dir segment by segment: FTP's MKD is single-level and
// servers reject existing directories, so each segment that already
// exists is verified and skipped.
func (f *FTP) MkdirAll(ctx context.Context, dir string) error {
	cleaned := CleanPath(dir)
	if cleaned == "/" {
		return nil
	}
	cur := ""
	for _, seg := range strings.Split(strings.Trim(cleaned, "/"), "/") {
		cur += "/" + seg
		if err := f.c.MakeDir(cur); err != nil {
			e, statErr := f.c.GetEntry(cur)
			if statErr != nil || e.Type != ftp.EntryTypeFolder {
				return fmt.Errorf("mkdir %s: %w", cur, err)
			}
		}
	}
	return nil
}

func (f *FTP) Remove(ctx context.Context, p string) error {
	cleaned := CleanPath(p)
	if cleaned == "/" {
		return fmt.Errorf("refusing to remove the source root")
	}
	e, err := f.c.GetEntry(f.abs(cleaned))
	if err != nil {
		return err
	}
	if e.Type == ftp.EntryTypeFolder {
		return f.c.RemoveDirRecur(f.abs(cleaned))
	}
	return f.c.Delete(f.abs(cleaned))
}

func (f *FTP) Rename(ctx context.Context, oldp, newp string) error {
	if CleanPath(newp) == "/" {
		return fmt.Errorf("invalid target")
	}
	return f.c.Rename(f.abs(oldp), f.abs(newp))
}

func (f *FTP) Close() error {
	_ = f.c.Logout()
	return f.c.Quit()
}
