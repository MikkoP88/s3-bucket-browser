package remotefs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
			go serveSFTPConn(conn, cfg)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, func() {
		ln.Close()
		<-done
	}
}

// serveSFTPConn completes the SSH handshake and hands the sftp subsystem
// to pkg/sftp's in-process server.
func serveSFTPConn(conn net.Conn, cfg *ssh.ServerConfig) {
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
