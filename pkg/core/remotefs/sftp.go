// sftp.go is the remotefs engine over SSH File Transfer Protocol. scp
// sources (TypeSCP) use the same engine — scp:// browsing is served by
// the SFTP subsystem on every modern OpenSSH server.
//
// Authentication: password (also answering keyboard-interactive prompts,
// which some servers require) plus the standard OpenSSH default identity
// files when they exist and load without a passphrase (~/.ssh/id_ed25519,
// id_ecdsa, id_rsa). Host keys are currently accepted without verification
// (first-class TOFU/known-hosts handling is a schema addition planned
// alongside private-key sources); Test surfaces this honestly.
package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/MikkoP88/s3-bucket-browser/pkg/core/listing"
	"github.com/MikkoP88/s3-bucket-browser/pkg/core/profile"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTP is an FS over an SFTP subsystem connection.
type SFTP struct {
	conn *ssh.Client
	c    *sftp.Client
	root string // absolute remote path of the source root (no trailing slash)
}

// DialSFTP connects to src (sftp or scp) and anchors "/" at src.Root (the
// account home directory when Root is empty).
func DialSFTP(ctx context.Context, src profile.Source) (FS, error) {
	port := src.Port
	if port == 0 {
		port = src.DefaultPort()
	}
	addr := net.JoinHostPort(src.Host, fmt.Sprintf("%d", port))

	var auths []ssh.AuthMethod
	for _, k := range defaultSSHIdentities() {
		data, err := os.ReadFile(k)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			continue // encrypted with a passphrase or unsupported — skip
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if pw := src.Password; pw != "" {
		auths = append(auths, ssh.Password(pw), ssh.KeyboardInteractive(
			func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pw
				}
				return answers, nil
			}))
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("sftp %s: no authentication method — set a password or provide a default key (~/.ssh/id_ed25519)", addr)
	}
	user := src.Username
	if user == "" {
		return nil, fmt.Errorf("sftp %s: username is required", addr)
	}

	dialer := net.Dialer{Timeout: 15 * time.Second}
	tcp, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("sftp %s: %w", addr, err)
	}
	sshCfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
		ClientVersion:   "SSH-2.0-s3b_M9",
	}
	cc, chans, reqs, err := ssh.NewClientConn(tcp, addr, sshCfg)
	if err != nil {
		tcp.Close()
		return nil, fmt.Errorf("sftp %s: %w", addr, err)
	}
	conn := ssh.NewClient(cc, chans, reqs)
	sc, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("sftp %s: %w", addr, err)
	}

	root := strings.TrimRight(src.Root, "/")
	if root == "" {
		if wd, err := sc.Getwd(); err == nil && wd != "" {
			root = wd
		}
	}
	if root != "" && !strings.HasPrefix(root, "/") {
		root = "/" + root
	}
	return &SFTP{conn: conn, c: sc, root: root}, nil
}

// abs maps an anchored path onto the remote filesystem.
func (s *SFTP) abs(p string) string {
	cleaned := CleanPath(p)
	if s.root == "" {
		return cleaned
	}
	if cleaned == "/" {
		return s.root
	}
	return s.root + cleaned
}

func sftpEntry(dir, name string, info os.FileInfo) listing.Entry {
	e := listing.Entry{
		Key:   Key(dir, name, info.IsDir()),
		Name:  name,
		IsDir: info.IsDir(),
		Size:  info.Size(),
	}
	if t := info.ModTime(); !t.IsZero() {
		e.LastModified = &t
	}
	return e
}

func (s *SFTP) List(ctx context.Context, dir string) ([]listing.Entry, error) {
	infos, err := s.c.ReadDir(s.abs(dir))
	if err != nil {
		return nil, err
	}
	parent := CleanPath(dir)
	entries := make([]listing.Entry, 0, len(infos))
	for _, info := range infos {
		name := path.Base(info.Name()) // servers send POSIX paths
		entries = append(entries, sftpEntry(parent, name, info))
	}
	listing.SortEntries(entries)
	return entries, nil
}

func (s *SFTP) Stat(ctx context.Context, p string) (listing.Entry, error) {
	cleaned := CleanPath(p)
	info, err := s.c.Stat(s.abs(cleaned))
	if err != nil {
		return listing.Entry{}, err
	}
	name := path.Base(strings.TrimSuffix(cleaned, "/"))
	return sftpEntry(path.Dir(cleaned), name, info), nil
}

func (s *SFTP) Open(ctx context.Context, p string) (io.ReadCloser, int64, error) {
	f, err := s.c.Open(s.abs(p))
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if info.IsDir() {
		f.Close()
		return nil, 0, fmt.Errorf("%s is a directory", CleanPath(p))
	}
	return f, info.Size(), nil
}

func (s *SFTP) Create(ctx context.Context, p string, r io.Reader) error {
	f, err := s.c.Create(s.abs(p))
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (s *SFTP) MkdirAll(ctx context.Context, dir string) error {
	return s.c.MkdirAll(s.abs(dir))
}

func (s *SFTP) Remove(ctx context.Context, p string) error {
	if CleanPath(p) == "/" {
		return errors.New("refusing to remove the source root")
	}
	return s.c.RemoveAll(s.abs(p))
}

func (s *SFTP) Rename(ctx context.Context, oldp, newp string) error {
	if CleanPath(newp) == "/" {
		return errors.New("invalid target")
	}
	// posix-rename overwrites the target (Explorer semantics); plain
	// rename is the fallback for servers without the extension.
	if err := s.c.PosixRename(s.abs(oldp), s.abs(newp)); err == nil {
		return nil
	}
	return s.c.Rename(s.abs(oldp), s.abs(newp))
}

func (s *SFTP) Close() error {
	// Close the SSH transport first: tearing down the underlying
	// connection unblocks the sftp client's reader/drain goroutines, so
	// its own Close (which waits on them) cannot hang against servers
	// that are slow to close the session channel.
	err1 := s.conn.Close()
	err2 := s.c.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// defaultSSHIdentities lists the OpenSSH default client key files in
// preference order (matching ssh(1)).
func defaultSSHIdentities() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	names := []string{"id_ed25519", "id_ecdsa", "id_rsa"}
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.ToSlash(filepath.Join(home, ".ssh", n)))
	}
	return out
}
