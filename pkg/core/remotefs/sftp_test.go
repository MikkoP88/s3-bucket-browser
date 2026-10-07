package remotefs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// startTestSFTPServer runs a real SSH+SFTP server in-process on a random
// localhost port (password auth user "test"/"secret"). It serves the real
// local filesystem rooted at "/", so tests anchor the client at a temp
// directory through src.Root — exactly how production anchoring works.
func startTestSFTPServer(t *testing.T) (host string, port int, stop func()) {
	t.Helper()
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "test" && string(pass) == "secret" {
				return nil, nil
			}
			return nil, errors.New("auth rejected")
		},
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddHostKey(signer)

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
			go serveSFTPConn(conn, cfg, 0)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, func() {
		ln.Close()
		<-done
	}
}

// startTestSFTPServerWedge is startTestSFTPServer with a sabotage: the
// sftp subsystem's request stream is proxied frame by frame, and only
// the first `handshakeFrames` client requests ever reach the server.
// With one frame the SSH handshake and the sftp version exchange
// complete — the dial succeeds — and every later request is silently
// swallowed: read, discarded, never answered. That is the field shape
// of a peer that vanished mid-session — no FIN, no RST, just silence —
// which the per-command deadline must break.
func startTestSFTPServerWedge(t *testing.T, handshakeFrames int) (host string, port int, stop func()) {
	t.Helper()
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "test" && string(pass) == "secret" {
				return nil, nil
			}
			return nil, errors.New("auth rejected")
		},
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddHostKey(signer)

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
			go serveSFTPConn(conn, cfg, handshakeFrames)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, func() {
		ln.Close()
		<-done
	}
}

// serveSFTPConn completes the SSH handshake and hands the sftp subsystem
// to pkg/sftp's in-process server. wedgeAfter > 0 wedges the session
// after that many client requests (see startTestSFTPServerWedge).
func serveSFTPConn(conn net.Conn, cfg *ssh.ServerConfig, wedgeAfter int) {
	defer conn.Close()
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			newCh.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			return
		}
		go func(ch ssh.Channel, chReqs <-chan *ssh.Request) {
			defer ch.Close()
			for req := range chReqs {
				if req.Type == "subsystem" && len(req.Payload) >= 4 && string(req.Payload[4:]) == "sftp" {
					req.Reply(true, nil)
					if wedgeAfter > 0 {
						serveWedgedSFTP(ch, wedgeAfter)
						return
					}
					server, err := sftp.NewServer(ch)
					if err != nil {
						return
					}
					// Serve until the channel closes; closing the channel
					// afterwards lets the client's drain goroutines exit.
					_ = server.Serve()
					server.Close()
					return
				}
				if req.WantReply {
					req.Reply(false, nil)
				}
			}
		}(ch, chReqs)
	}
}

// serveWedgedSFTP hands pkg/sftp's server a request stream that goes
// silent: a framing proxy forwards the first `keep` request packets
// (length-prefixed, so no protocol knowledge is needed), then consumes
// and discards everything the client sends from then on. The server's
// replies flow back unfiltered, which is exactly what makes the dial
// succeed and the first real command wedge.
func serveWedgedSFTP(ch ssh.Channel, keep int) {
	reqs, fromClient := io.Pipe() // server side of the request stream
	go func() {
		hdr := make([]byte, 4)
		for i := 0; i < keep; i++ {
			if _, err := io.ReadFull(ch, hdr); err != nil {
				reqs.CloseWithError(err)
				return
			}
			frame := make([]byte, int(binary.BigEndian.Uint32(hdr)))
			if _, err := io.ReadFull(ch, frame); err != nil {
				reqs.CloseWithError(err)
				return
			}
			if _, err := fromClient.Write(append(hdr, frame...)); err != nil {
				return
			}
		}
		// The wedge: read everything, answer nothing.
		_, _ = io.Copy(io.Discard, ch)
		reqs.Close()
	}()
	server, err := sftp.NewServer(&wedgeSession{r: reqs, w: ch, c: ch})
	if err != nil {
		return
	}
	_ = server.Serve()
	server.Close()
}

// wedgeSession is the ReadWriteCloser handed to pkg/sftp's server for a
// wedged session: requests arrive only while the framing proxy lets
// them, replies go straight to the client.
type wedgeSession struct {
	r io.Reader
	w io.Writer
	c io.Closer
}

func (s *wedgeSession) Read(p []byte) (int, error)  { return s.r.Read(p) }
func (s *wedgeSession) Write(p []byte) (int, error) { return s.w.Write(p) }
func (s *wedgeSession) Close() error                { return s.c.Close() }

func dialTestSFTP(t *testing.T, host string, port int, root string) FS {
	t.Helper()
	fs, err := DialSFTP(context.Background(), profile.Source{
		Name:     "lab",
		Type:     profile.TypeSFTP,
		Host:     host,
		Port:     port,
		Username: "test",
		Password: "secret",
		Root:     filepath.ToSlash(root),
	})
	if err != nil {
		t.Fatalf("DialSFTP: %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	return fs
}

func TestSFTPEngineContract(t *testing.T) {
	host, port, stop := startTestSFTPServer(t)
	defer stop()

	// Seed a tree through the LOCAL filesystem, browse it over SFTP.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("over sftp"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := dialTestSFTP(t, host, port, root)
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
	if entries[1].Size != int64(len("over sftp")) {
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

// A peer that stops answering mid-session must not hang the engine for
// the life of the process: the command deadline tears the transport
// down, the call returns the deadline verdict, and the follow-up on the
// torn engine fails fast instead of joining the wedge (the api layer's
// healing cache redials on exactly that verdict).
func TestSFTPCommandDeadlineBreaksSilentWedge(t *testing.T) {
	host, port, stop := startTestSFTPServerWedge(t, 1) // INIT passes, everything after wedges
	defer stop()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("over sftp"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := sftpCmdTimeout
	sftpCmdTimeout = 150 * time.Millisecond
	t.Cleanup(func() { sftpCmdTimeout = old })

	fs := dialTestSFTP(t, host, port, root) // the dial completes — the wedge is mid-session

	start := time.Now()
	_, err := fs.List(context.Background(), "/")
	if !errors.Is(err, ErrCmdDeadline) {
		t.Fatalf("List under a wedged channel = %v, want the command-deadline verdict", err)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("the break took %v — the deadline never fired", el)
	}
	// The transport is torn, not merely parked: a follow-up on the same
	// engine fails fast rather than blocking behind the wedge.
	if _, serr := fs.Stat(context.Background(), "/readme.md"); serr == nil {
		t.Fatal("Stat on the torn engine unexpectedly succeeded")
	}
}

func TestDialSFTPAuthAndHostErrors(t *testing.T) {
	host, port, stop := startTestSFTPServer(t)
	defer stop()
	ctx := context.Background()

	// Wrong password: dial fails with the ssh error wrapped.
	if _, err := DialSFTP(ctx, profile.Source{Name: "x", Type: profile.TypeSFTP,
		Host: host, Port: port, Username: "test", Password: "nope"}); err == nil {
		t.Error("wrong password must fail the dial")
	}
	// No username.
	if _, err := DialSFTP(ctx, profile.Source{Name: "x", Type: profile.TypeSFTP,
		Host: host, Port: port, Password: "secret"}); err == nil || !strings.Contains(err.Error(), "username") {
		t.Errorf("missing username err = %v", err)
	}
	// No auth method at all (temporarily hide any default keys by using a
	// nonexistent HOME — best effort on all platforms).
	t.Setenv("USERPROFILE", t.TempDir()) // windows
	t.Setenv("HOME", t.TempDir())        // unix
	if _, err := DialSFTP(ctx, profile.Source{Name: "x", Type: profile.TypeSFTP,
		Host: host, Port: port, Username: "test"}); err == nil || !strings.Contains(err.Error(), "authentication") {
		t.Errorf("no-auth err = %v", err)
	}
	// Unreachable host.
	if _, err := DialSFTP(ctx, profile.Source{Name: "x", Type: profile.TypeSFTP,
		Host: "127.0.0.1", Port: 1, Username: "test", Password: "secret"}); err == nil {
		t.Error("unreachable host must fail")
	}
}
