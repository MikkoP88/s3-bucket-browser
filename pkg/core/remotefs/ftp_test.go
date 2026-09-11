// ftp_test.go: the FTP engine's unit suite. A minimal but real FTP server
// (RFC 959 control protocol + RFC 3659 MLST/MLSD facts, EPSV/PASV data
// connections) runs in-process on a random localhost port and serves the
// local filesystem, mirroring sftp_test.go: tests anchor the client at a
// temp directory through src.Root — exactly how production anchoring
// works. MLST/MLSD are advertised because the engine's Stat/MkdirAll/
// Remove paths go through jlaffaye/ftp's GetEntry, which answers 502
// without them (as do vsftpd/ProFTPD in the wild).
package remotefs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
)

// startTestFTPServer runs a real FTP server in-process on a random
// localhost port (password auth user "test"/"secret"), advertising
// MLST/MLSD. Use startTestFTPServerMode(t, false) for a vsftpd-style
// server without them (LIST listings, no single-entry stat).
func startTestFTPServer(t *testing.T) (host string, port int, stop func()) {
	return startTestFTPServerMode(t, true)
}

func startTestFTPServerMode(t *testing.T, mlst bool) (host string, port int, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFTPTestConn(conn, mlst)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, func() {
		ln.Close()
		<-done
	}
}

// ftpTestSession is one control connection. Paths on the wire are virtual
// slash paths ("/dir/file", or bare names relative to the session cwd);
// ftpTestLocal maps them onto the serving filesystem.
type ftpTestSession struct {
	c      net.Conn
	br     *bufio.Reader
	bw     *bufio.Writer
	cwd    string // virtual working directory
	user   string
	authed bool
	rnfr   string
	mlst   bool // advertise MLST/MLSD and answer MLST
	dataLn net.Listener
}

func serveFTPTestConn(conn net.Conn, mlst bool) {
	defer conn.Close()
	s := &ftpTestSession{
		c:    conn,
		br:   bufio.NewReader(conn),
		bw:   bufio.NewWriter(conn),
		cwd:  "/",
		mlst: mlst,
	}
	s.reply("220 s3b test ftpd ready")
	for {
		line, err := s.br.ReadString('\n')
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		if s.handle(strings.ToUpper(verb), arg) {
			return
		}
	}
}

func (s *ftpTestSession) reply(format string, a ...any) {
	fmt.Fprintf(s.bw, format+"\r\n", a...)
	s.bw.Flush()
}

// vpath resolves a wire path into a clean virtual slash path.
func (s *ftpTestSession) vpath(p string) string {
	if p == "" {
		return s.cwd
	}
	if !strings.HasPrefix(p, "/") {
		p = s.cwd + "/" + p
	}
	return path.Clean(p)
}

// ftpTestLocal maps a virtual slash path onto the serving filesystem —
// including the Windows drive-letter form the engines' Root anchoring
// produces ("/C:/dir" → "C:\dir"), the same translation pkg/sftp's
// server performs (server_windows.go toLocalPath).
func ftpTestLocal(v string) string {
	lp := filepath.FromSlash(v)
	if path.IsAbs(v) {
		trimmed := strings.TrimLeft(lp, string(filepath.Separator))
		if filepath.IsAbs(trimmed) {
			return trimmed
		}
	}
	return lp
}

func (s *ftpTestSession) handle(verb, arg string) (quit bool) {
	switch verb {
	case "USER":
		s.user = arg
		s.reply("331 password required")
	case "PASS":
		if s.user == "test" && arg == "secret" {
			s.authed = true
			s.reply("230 logged in")
		} else {
			s.reply("530 authentication failed")
		}
	case "FEAT":
		s.reply("211-Features:")
		if s.mlst {
			s.reply(" MLST type*;size*;modify*;")
		}
		s.reply("211 End")
	case "TYPE", "OPTS", "NOOP":
		s.reply("200 ok")
	case "SYST":
		s.reply("215 UNIX Type: L8")
	case "PWD":
		s.reply("257 %q is the current directory", s.cwd)
	case "CWD":
		if st, err := os.Stat(ftpTestLocal(s.vpath(arg))); err != nil || !st.IsDir() {
			s.reply("550 no such directory")
		} else {
			s.cwd = s.vpath(arg)
			s.reply("250 directory changed")
		}
	case "CDUP":
		s.cwd = path.Dir(s.cwd)
		s.reply("250 directory changed")
	case "EPSV", "PASV":
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			s.reply("425 cannot open data connection")
			return false
		}
		s.dataLn = ln
		port := ln.Addr().(*net.TCPAddr).Port
		if verb == "EPSV" {
			s.reply("229 Entering Extended Passive Mode (|||%d|)", port)
		} else {
			s.reply("227 Entering Passive Mode (127,0,0,1,%d,%d)", port/256, port%256)
		}
	case "MLSD", "LIST":
		s.transferList(arg)
	case "MLST":
		if !s.mlst {
			s.reply("502 not implemented") // vsftpd behavior
			return false
		}
		v := s.vpath(arg)
		st, err := os.Stat(ftpTestLocal(v))
		if err != nil {
			s.reply("550 no such file")
			return false
		}
		s.reply("250-Listing %s", v)
		s.reply(" %s %s", ftpTestFacts(st), v)
		s.reply("250 End")
	case "SIZE":
		st, err := os.Stat(ftpTestLocal(s.vpath(arg)))
		if err != nil || st.IsDir() {
			s.reply("550 no such file")
		} else {
			s.reply("213 %d", st.Size())
		}
	case "RETR":
		s.transferFile(arg, false)
	case "STOR":
		s.transferFile(arg, true)
	case "MKD":
		if err := os.Mkdir(ftpTestLocal(s.vpath(arg)), 0o755); err != nil {
			s.reply("550 mkdir failed") // existing dirs must fail: MkdirAll skips them
		} else {
			s.reply("257 created")
		}
	case "RMD", "DELE":
		if err := os.Remove(ftpTestLocal(s.vpath(arg))); err != nil {
			s.reply("550 %s failed", verb)
		} else {
			s.reply("250 done")
		}
	case "RNFR":
		if _, err := os.Stat(ftpTestLocal(s.vpath(arg))); err != nil {
			s.reply("550 no such file")
		} else {
			s.rnfr = s.vpath(arg)
			s.reply("350 ready for destination")
		}
	case "RNTO":
		if s.rnfr == "" || os.Rename(ftpTestLocal(s.rnfr), ftpTestLocal(s.vpath(arg))) != nil {
			s.reply("550 rename failed")
		} else {
			s.reply("250 renamed")
		}
		s.rnfr = ""
	case "REIN":
		s.authed = false
		s.reply("220 ready for new user")
	case "QUIT":
		s.reply("221 bye")
		return true
	default:
		s.reply("502 not implemented")
	}
	return false
}

// ftpTestListLine renders one listing entry for the wire: RFC 3659 facts
// on MLSD servers, or — when the server impersonates vsftpd (no MLST) —
// a unix "ls -l" line, the layout jlaffaye's LIST parser consumes:
// "mode links owner group size mon day hh:mm name" with a 10-char mode
// and a time-of-day (not a year) for recent mtimes.
func ftpTestListLine(mlst bool, st os.FileInfo, name string) string {
	if mlst {
		return fmt.Sprintf("%s %s\r\n", ftpTestFacts(st), name)
	}
	perms := "-rw-r--r--"
	if st.IsDir() {
		perms = "drwxr-xr-x"
	}
	mod := st.ModTime().Format("Jan _2 15:04")
	return fmt.Sprintf("%s 1 ftp ftp %d %s %s\r\n", perms, st.Size(), mod, name)
}

// ftpTestFacts renders RFC 3659 facts for one filesystem entry.
func ftpTestFacts(st os.FileInfo) string {
	mod := st.ModTime().UTC().Format("20060102150405")
	if st.IsDir() {
		return fmt.Sprintf("type=dir;modify=%s;", mod)
	}
	return fmt.Sprintf("type=file;size=%d;modify=%s;", st.Size(), mod)
}

// openData answers the transfer command (150) and accepts the client's
// data connection on the listener the preceding EPSV/PASV opened.
func (s *ftpTestSession) openData() (net.Conn, bool) {
	if s.dataLn == nil {
		s.reply("425 use EPSV/PASV first")
		return nil, false
	}
	ln := s.dataLn
	s.dataLn = nil
	if tcp, ok := ln.(*net.TCPListener); ok {
		_ = tcp.SetDeadline(time.Now().Add(5 * time.Second))
	}
	s.reply("150 opening data connection")
	conn, err := ln.Accept()
	ln.Close()
	if err != nil {
		s.reply("425 data connection failed")
		return nil, false
	}
	return conn, true
}

func (s *ftpTestSession) transferList(arg string) {
	dc, ok := s.openData()
	if !ok {
		return
	}
	entries, err := os.ReadDir(ftpTestLocal(s.vpath(arg)))
	if err != nil {
		dc.Close()
		s.reply("550 list failed")
		return
	}
	w := bufio.NewWriter(dc)
	for _, e := range entries {
		st, err := e.Info()
		if err != nil {
			continue
		}
		fmt.Fprint(w, ftpTestListLine(s.mlst, st, e.Name()))
	}
	w.Flush()
	dc.Close()
	s.reply("226 transfer complete")
}

func (s *ftpTestSession) transferFile(arg string, stor bool) {
	dc, ok := s.openData()
	if !ok {
		return
	}
	real := ftpTestLocal(s.vpath(arg))
	if stor {
		f, err := os.Create(real)
		if err != nil {
			dc.Close()
			s.reply("550 cannot create file")
			return
		}
		_, copyErr := io.Copy(f, dc)
		closeErr := f.Close()
		dc.Close()
		if copyErr != nil || closeErr != nil {
			s.reply("550 stor failed")
			return
		}
		s.reply("226 transfer complete")
		return
	}
	f, err := os.Open(real)
	if err != nil {
		dc.Close()
		s.reply("550 no such file")
		return
	}
	_, wErr := io.Copy(dc, f)
	f.Close()
	dc.Close()
	if wErr != nil {
		s.reply("426 transfer aborted")
		return
	}
	s.reply("226 transfer complete")
}

func dialTestFTP(t *testing.T, host string, port int, root string) FS {
	t.Helper()
	fs, err := DialFTP(context.Background(), profile.Source{
		Name:     "lab",
		Type:     profile.TypeFTP,
		Host:     host,
		Port:     port,
		Username: "test",
		Password: "secret",
		Root:     filepath.ToSlash(root),
	})
	if err != nil {
		t.Fatalf("DialFTP: %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	return fs
}

// TestFTPEngineContract walks the whole FS interface against the real
// in-process server: List/Stat/Open/Create/MkdirAll/Rename/Remove plus
// the root-anchoring guarantees the SFTP suite checks.
func TestFTPEngineContract(t *testing.T) {
	host, port, stop := startTestFTPServer(t)
	defer stop()
	runFTPEngineContract(t, host, port)
}

// TestFTPEngineContractWithoutMLST runs the same contract against a
// vsftpd-style server: no MLST/MLSD advertised, so single-entry stat
// must fall back to parent listings and listings parse unix ls lines.
func TestFTPEngineContractWithoutMLST(t *testing.T) {
	host, port, stop := startTestFTPServerMode(t, false)
	defer stop()
	runFTPEngineContract(t, host, port)
}

func runFTPEngineContract(t *testing.T, host string, port int) {

	// Seed a tree through the LOCAL filesystem, browse it over FTP.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("over ftp"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := dialTestFTP(t, host, port, root)
	ctx := context.Background()

	entries, err := fs.List(ctx, "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].IsDir || entries[0].Name != "docs" || entries[1].Name != "readme.md" {
		t.Fatalf("List(/) = %+v", entries)
	}
	if entries[0].Key != "/docs/" || entries[1].Key != "/readme.md" {
		t.Errorf("keys: %+v", entries)
	}
	if entries[1].Size != int64(len("over ftp")) {
		t.Errorf("size = %d", entries[1].Size)
	}

	// Anchoring: the root is the temp dir; nothing above it is visible.
	above, err := fs.List(ctx, "/..")
	if err != nil {
		t.Fatal(err)
	}
	if len(above) != len(entries) {
		t.Errorf("path escaped the source root: %d entries", len(above))
	}

	st, err := fs.Stat(ctx, "/readme.md")
	if err != nil || st.IsDir || st.Name != "readme.md" {
		t.Errorf("Stat = %+v err = %v", st, err)
	}
	if st.LastModified == nil {
		t.Error("Stat must surface the modify fact as mtime")
	}

	if err := fs.Create(ctx, "/docs/uploaded.txt", strings.NewReader("uploaded")); err != nil {
		t.Fatal(err)
	}
	r, size, err := fs.Open(ctx, "/docs/uploaded.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "uploaded" || size != int64(len("uploaded")) {
		t.Errorf("round trip = %q size %d", b, size)
	}
	// The file really landed on the serving filesystem.
	if data, err := os.ReadFile(filepath.Join(root, "docs", "uploaded.txt")); err != nil || string(data) != "uploaded" {
		t.Errorf("file not visible locally: %q %v", data, err)
	}

	if err := fs.MkdirAll(ctx, "/deep/er"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(ctx, "/deep/er"); err != nil {
		t.Errorf("MkdirAll failed: %v", err)
	}
	// An existing directory is skipped, not an error (MKD fails, the
	// engine verifies the segment is a folder and moves on).
	if err := fs.MkdirAll(ctx, "/docs"); err != nil {
		t.Errorf("MkdirAll over existing dir: %v", err)
	}

	if err := fs.Rename(ctx, "/docs/uploaded.txt", "/docs/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(ctx, "/docs/renamed.txt"); err != nil {
		t.Error("rename target missing")
	}

	if err := fs.Remove(ctx, "/docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(ctx, "/docs/renamed.txt"); !os.IsNotExist(err) {
		t.Errorf("tree remove failed: %v", err)
	}
	if err := fs.Remove(ctx, "/"); err == nil {
		t.Error("removing the root must be refused")
	}
}

func TestDialFTPAuthAndHostErrors(t *testing.T) {
	host, port, stop := startTestFTPServer(t)
	defer stop()
	ctx := context.Background()

	// Wrong password: dial fails on login.
	if _, err := DialFTP(ctx, profile.Source{Name: "x", Type: profile.TypeFTP,
		Host: host, Port: port, Username: "test", Password: "nope"}); err == nil || !strings.Contains(err.Error(), "login") {
		t.Errorf("wrong password err = %v", err)
	}
	// No username falls back to anonymous, which the test server rejects.
	if _, err := DialFTP(ctx, profile.Source{Name: "x", Type: profile.TypeFTP,
		Host: host, Port: port, Password: "secret"}); err == nil || !strings.Contains(err.Error(), "login") {
		t.Errorf("anonymous err = %v", err)
	}
	// Unreachable host.
	if _, err := DialFTP(ctx, profile.Source{Name: "x", Type: profile.TypeFTP,
		Host: "127.0.0.1", Port: 1, Username: "test", Password: "secret"}); err == nil {
		t.Error("unreachable host must fail")
	}
}
