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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/textproto"
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
	root string         // absolute remote path of the source root (no trailing slash)
	src  profile.Source // redial template (see withRetry)
}

// dialFTPConn dials and logs in one control connection — the shared body
// of the first dial and every redial (withRetry).
func dialFTPConn(ctx context.Context, src profile.Source) (*ftp.ServerConn, error) {
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
	return conn, nil
}

// DialFTP connects to src (ftp or ftps) and anchors "/" at src.Root (the⁠​‌‌‌​​‌‌​​‌‌​​‌‌​‌‌​​​‌​​​‌​‌‌​‌​‌‌‌​​​​​‌‌‌​​‌​​‌‌​‌‌‌‌​‌‌‌​‌‌​​‌‌​​‌​‌​‌‌​‌‌‌​​‌‌​​​​‌​‌‌​‌‌‌​​‌‌​​​‌‌​‌‌​​‌​‌​​‌​‌‌​‌​‌‌‌​‌‌​​​‌‌​​​‌​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌​​​​‌‌​‌‌​‌‌‌‌​‌‌‌​​​​​‌‌‌‌​​‌​‌‌‌​​‌​​‌‌​‌​​‌​‌‌​​‌‌‌​‌‌​‌​​​​‌‌‌​‌​​​​‌​​​​​​​‌​‌​​​​‌‌​​​‌‌​​‌​‌​​‌​​‌​​​​​​​‌‌​​‌​​​‌‌​​​​​​‌‌​​‌​​​‌‌​‌‌​​​‌​​​​​​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​​‌​​​​​​‌​‌​​​​​‌‌​​‌​‌​‌‌‌​​‌‌​‌‌​‌‌‌‌​‌‌​‌‌‌​​‌‌​​‌​‌​‌‌​‌‌‌​​​‌​​​​​​​‌​‌​​​​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​‌​‌​​​​​​‌‌‌​​​​​‌‌‌​​​​​‌​‌​​‌​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌​‌​​​​​‌‌​‌‌‌‌​‌‌​‌‌​​​‌‌‌‌​​‌​‌​​​‌‌​​‌‌​‌‌‌‌​‌‌‌​​‌​​‌‌​‌‌​‌​​‌​​​​​​‌​​‌​​‌​‌‌​‌‌‌​​‌‌‌​‌​​​‌‌​​‌​‌​‌‌‌​​‌​​‌‌​‌‌‌​​‌‌​​​​‌​‌‌​‌‌​​​​‌​​​​​​‌​‌​‌​‌​‌‌‌​​‌‌​‌‌​​‌​‌​​‌​​​​​​‌​​‌‌​​​‌‌​‌​​‌​‌‌​​​‌‌​‌‌​​‌​‌​‌‌​‌‌‌​​‌‌‌​​‌‌​‌‌​​‌​‌​​‌​​​​​​​‌‌​​​‌​​‌​‌‌‌​​​‌‌​​​​​​‌​‌‌‌​​​‌‌​​​​​​‌​​​​​​‌‌‌‌‌​​​​‌​​​​​​‌‌​​‌‌‌​‌‌​‌​​‌​‌‌‌​‌​​​‌‌​‌​​​​‌‌‌​‌​‌​‌‌​​​‌​​​‌​‌‌‌​​‌‌​​​‌‌​‌‌​‌‌‌‌​‌‌​‌‌​‌​​‌​‌‌‌‌​‌​​‌‌​‌​‌‌​‌​​‌​‌‌​‌​‌‌​‌‌​‌​‌‌​‌‌​‌‌‌‌​‌​‌​​​​​​‌‌‌​​​​​‌‌‌​​​​​‌​‌‌‌‌​‌‌‌​​‌‌​​‌‌​​‌‌​​‌​‌‌​‌​‌‌​​​‌​​‌‌‌​‌​‌​‌‌​​​‌‌​‌‌​‌​‌‌​‌‌​​‌​‌​‌‌‌​‌​​​​‌​‌‌​‌​‌‌​​​‌​​‌‌‌​​‌​​‌‌​‌‌‌‌​‌‌‌​‌‌‌​‌‌‌​​‌‌​‌‌​​‌​‌​‌‌‌​​‌​⁠
// login directory when Root is empty).
func DialFTP(ctx context.Context, src profile.Source) (FS, error) {
	conn, err := dialFTPConn(ctx, src)
	if err != nil {
		return nil, err
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
	return &FTP{c: conn, root: root, src: src}, nil
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

// isConnErr reports whether err means the CONTROL CONNECTION is gone —
// an idle-timeout write (the classic "server dropped us after ~5 minutes"
// failure), an EOF/RST, or the server's polite 421 close. Server
// rejections about the filesystem (4xx/5xx such as 550 not-found) are
// honest answers, never a reason to redial.
func isConnErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var tp *textproto.Error
	if errors.As(err, &tp) && tp.Code == 421 {
		return true
	}
	return false
}

// withRetry runs one engine operation against the control connection.
// FTP servers drop idle control connections (vsftpd after ~5 minutes) and
// often silently — the next command write times out at the OS level or
// reads EOF. That failure is about the CONNECTION, not the source, so
// redial once with the stored source settings and retry; only a second
// failure is reported. f.c is swapped here, which is race-safe because
// every engine call on a source serializes on the app's per-source lock
// (remote.go srcLock) and DialFTP publishes the engine only after
// constructing it.
func withRetry[T any](f *FTP, ctx context.Context, op func() (T, error)) (T, error) {
	v, err := op()
	if err == nil || !(isConnErr(err) || errors.Is(err, errFTPEntryUnreliable)) {
		return v, err
	}
	_ = f.c.Quit() // already dead (or verdict-poisoned); best effort
	c, derr := dialFTPConn(ctx, f.src)
	if derr != nil {
		return v, err // the original connection error is the honest one
	}
	f.c = c
	return op()
}

func (f *FTP) List(ctx context.Context, dir string) ([]listing.Entry, error) {
	parent := CleanPath(dir)
	filter := func(raw []*ftp.Entry) []listing.Entry {
		entries := make([]listing.Entry, 0, len(raw))
		for _, e := range raw {
			if e.Name == "." || e.Name == ".." || e.Name == "" {
				continue
			}
			entries = append(entries, ftpEntry(parent, e))
		}
		return entries
	}
	return withRetry(f, ctx, func() ([]listing.Entry, error) {
		raw, err := f.c.List(f.abs(dir))
		if err != nil {
			return nil, err
		}
		entries := filter(raw)
		if len(entries) == 0 {
			// An empty listing is ambiguous twice over. A data connection
			// that closes early truncates LIST to an empty success — the
			// scanner cannot tell an aborted stream from a completed one,
			// and vsftpd's three-port pasv range drops data connections
			// under churn — and vsftpd answers LIST on a MISSING directory
			// the same empty way, with the control-channel 550 arriving
			// too late for the parser. Both read as "no children" to every
			// caller, which inverts the safety of deletes and transfer
			// planners: a wipe scoped to a glitched path reports a no-op
			// success. One retry absorbs the transient; the stat then
			// separates "gone" (550) from honestly empty.
			if again, rerr := f.c.List(f.abs(dir)); rerr == nil {
				entries = filter(again)
			}
			if len(entries) == 0 {
				if _, gerr := ftpGetEntry(f.c, f.abs(dir)); gerr != nil {
					if is550(gerr) {
						return nil, &fs.PathError{Op: "list", Path: parent, Err: fs.ErrNotExist}
					}
					return nil, gerr
				}
			}
		}
		listing.SortEntries(entries)
		return entries, nil
	})
}

// ftpGetEntry stats one path. jlaffaye's GetEntry needs server-side MLST
// support; vsftpd (the common Linux FTP server) implements neither MLST
// nor MLSD and answers 502 — so fall back to listing the parent and
// matching the basename, which works everywhere LIST does.
// errFTPEntryUnreliable marks an entry probe whose control channel gave a
// suspect answer — a failed or garbage reply on the listing path. A verdict
// of "no such file" from such a channel is not trustworthy: the classic case
// is a control connection desynced by an aborted transfer reading the dead
// command's stale reply as its own. Callers (via withRetry) reconnect and
// re-probe instead of acting on it; treating it as a clean miss once left a
// transfer's staging file stranded on the server.
var errFTPEntryUnreliable = errors.New("ftp: entry probe unreliable")

func ftpGetEntry(c *ftp.ServerConn, absPath string) (*ftp.Entry, error) {
	if strings.TrimSuffix(absPath, "/") == "" {
		return &ftp.Entry{Type: ftp.EntryTypeFolder, Name: "/"}, nil // the root
	}
	e, err := c.GetEntry(absPath)
	if err == nil {
		return e, nil
	}
	dir, name := path.Split(strings.TrimSuffix(absPath, "/"))
	parent := strings.TrimSuffix(dir, "/")
	if parent == "" {
		parent = "/" // never a bare LIST — that reads the connection's CWD, which earlier engine ops may have moved
	}
	entries, lerr := c.List(parent)
	if lerr == nil && len(entries) == 0 {
		// An empty parent listing is ambiguous — an honestly empty parent
		// or a data connection that closed early (see List) — and the
		// basename match below would turn the transient into a false
		// "no such file" for a live path. One retry before declaring the
		// path missing; Remove's pre-check and Mkdir's exists-tolerance
		// both route through here.
		entries, lerr = c.List(parent)
	}
	if lerr != nil {
		// A 550 on the parent listing means the parent is gone, so the
		// path cannot exist — surface that, not the MLST 502, or
		// callers can't recognize a missing file.
		var l550 *textproto.Error
		if errors.As(lerr, &l550) && l550.Code == 550 {
			return nil, l550
		}
		// Anything else is a channel that answered garbage, not a
		// verdict. Returning the original MLST error here once let the
		// poison masquerade as a clean "unsupported" and skip the
		// reconnect — mark it unreliable so withRetry redials.
		return nil, fmt.Errorf("%w: listing %s: %v", errFTPEntryUnreliable, parent, lerr)
	}
	for _, e := range entries {
		if e.Name == name {
			return e, nil
		}
	}
	// The basename miss rests entirely on the listing, and a listing can
	// lie empty (see List). SIZE addresses the exact path over the
	// control channel, independent of any data connection: if it answers,
	// the file is real and the miss was the lie — this is what makes a
	// canceled transfer's stage removable even when the listing that
	// should show it reads empty.
	if size, serr := c.FileSize(absPath); serr == nil {
		sz := uint64(0)
		if size > 0 {
			sz = uint64(size)
		}
		return &ftp.Entry{Type: ftp.EntryTypeFile, Name: name, Size: sz}, nil
	} else if !is550(serr) {
		return nil, fmt.Errorf("%w: size %s: %v", errFTPEntryUnreliable, absPath, serr)
	}
	return nil, &textproto.Error{Code: 550, Msg: "no such file"}
}

// is550 reports whether err is FTP's clean "not available" reply — the
// server's honest word that a path is gone.
func is550(err error) bool {
	var tp *textproto.Error
	return errors.As(err, &tp) && tp.Code == 550
}

func (f *FTP) Stat(ctx context.Context, p string) (listing.Entry, error) {
	v, err := withRetry(f, ctx, func() (listing.Entry, error) {
		e, err := ftpGetEntry(f.c, f.abs(CleanPath(p)))
		if err != nil {
			return listing.Entry{}, err
		}
		cleaned := CleanPath(p)
		name := path.Base(strings.TrimSuffix(cleaned, "/"))
		entry := ftpEntry(path.Dir(cleaned), e)
		entry.Name = name
		return entry, nil
	})
	if err != nil {
		return listing.Entry{}, ftpNotExist(p, err)
	}
	return v, nil
}

// ftpNotExist maps the FTP 550 "unavailable" reply to a PathError over
// fs.ErrNotExist, so both os.IsNotExist and errors.Is guards behave like
// the local/sftp engines (os.IsNotExist only unwraps PathError-style
// errors, not arbitrary %w chains).
func ftpNotExist(p string, err error) error {
	if is550(err) {
		return &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
	}
	return err
}

// ftpOpen carries Open's pair through withRetry's single-value channel.
type ftpOpen struct {
	rc   io.ReadCloser
	size int64
}

func (f *FTP) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	v, err := withRetry(f, ctx, func() (ftpOpen, error) {
		// SIZE must precede RETR: the server sends the transfer-complete
		// reply (226) right after the data connection closes, and that reply
		// would still sit unread on the control channel when SIZE is issued,
		// desyncing it (the size would silently come back 0).
		size, err := f.c.FileSize(f.abs(p))
		if err != nil {
			size = 0 // server refused SIZE (ASCII mode); stream without it
		}
		resp, err := f.c.Retr(f.abs(p))
		if err != nil {
			return ftpOpen{}, err
		}
		return ftpOpen{rc: resp, size: size}, nil
	})
	return v.rc, v.size, err
}

func (f *FTP) Create(ctx context.Context, p string, r io.Reader) error {
	_, err := withRetry(f, ctx, func() (struct{}, error) {
		return struct{}{}, f.c.Stor(f.abs(p), r)
	})
	return err
}

// MkdirAll creates dir segment by segment: FTP's MKD is single-level and
// servers reject existing directories, so each segment that already
// exists is verified and skipped.
func (f *FTP) MkdirAll(ctx context.Context, dir string) error {
	cleaned := CleanPath(dir)
	if cleaned == "/" {
		return nil
	}
	_, err := withRetry(f, ctx, func() (struct{}, error) {
		cur := ""
		for _, seg := range strings.Split(strings.Trim(cleaned, "/"), "/") {
			cur += "/" + seg
			// Every segment is anchored at the source root — a bare path
			// would create the tree at the server root instead.
			if err := f.c.MakeDir(f.abs(cur)); err != nil {
				e, statErr := ftpGetEntry(f.c, f.abs(cur))
				if statErr != nil || e.Type != ftp.EntryTypeFolder {
					return struct{}{}, fmt.Errorf("mkdir %s: %w", cur, err)
				}
			}
		}
		return struct{}{}, nil
	})
	return err
}

// removeDir deletes one directory tree through the engine's own hardened
// listing. jlaffaye's RemoveDirRecur walks with raw c.List calls — no
// empty-listing retry, so the transient above reads as "no children" —
// deletes by RELATIVE name against whatever directory the connection
// happens to sit in, and leaves the pooled connection CWDed at the parent
// when it returns. Walking with f.List instead means every level of the
// wipe gets the retry, every delete targets an absolute path, and the
// server's refusal to RMDIR a non-empty directory stays as the backstop
// against a listing that still lied.
func (f *FTP) removeDir(ctx context.Context, cleaned string) error {
	entries, err := f.List(ctx, cleaned)
	if err != nil {
		return err
	}
	for _, e := range entries {
		child := path.Join(cleaned, e.Name)
		if e.IsDir {
			if err := f.removeDir(ctx, child); err != nil {
				return err
			}
		} else if err := f.c.Delete(f.abs(child)); err != nil {
			return err
		}
	}
	return f.c.RemoveDir(f.abs(cleaned))
}

func (f *FTP) Remove(ctx context.Context, p string) error {
	cleaned := CleanPath(p)
	if cleaned == "/" {
		return fmt.Errorf("refusing to remove the source root")
	}
	_, err := withRetry(f, ctx, func() (struct{}, error) {
		e, err := ftpGetEntry(f.c, f.abs(cleaned))
		if err != nil {
			return struct{}{}, err
		}
		if e.Type == ftp.EntryTypeFolder {
			return struct{}{}, f.removeDir(ctx, cleaned)
		}
		return struct{}{}, f.c.Delete(f.abs(cleaned))
	})
	// A clean miss arrives as a raw 550; map it to fs.ErrNotExist so
	// callers (rm --force, idempotent re-runs, the transfer discard)
	// recognize "already gone" the same way as on every other engine.
	return ftpNotExist(cleaned, err)
}

func (f *FTP) Rename(ctx context.Context, oldp, newp string) error {
	if CleanPath(newp) == "/" {
		return fmt.Errorf("invalid target")
	}
	_, err := withRetry(f, ctx, func() (struct{}, error) {
		return struct{}{}, f.c.Rename(f.abs(oldp), f.abs(newp))
	})
	return err
}

func (f *FTP) Close() error {
	_ = f.c.Logout()
	return f.c.Quit()
}
