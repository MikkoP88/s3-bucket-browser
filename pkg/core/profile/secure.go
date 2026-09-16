// secure.go implements opt-in Secure Storage (the shared-host hardening
// documented in docs/security.md): the whole profiles.json becomes one
// AES-256-GCM envelope keyed by a random 32-byte master key that lives in
// the OS keyring (account "secure/master" of the same "s3b" service the
// other secrets use). The envelope is self-describing — the file carries
// the magic — so the CLI and the GUI can never disagree about the mode,
// and an encrypted store on a host without a keyring fails loudly with
// remediation instead of silently degrading to plaintext.
package profile

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	secureMagic   = "s3bsf1"
	masterAccount = "secure/master"
)

// Errors surfaced to the UI and the CLI.
var (
	ErrSecureNeedsKeyring = errors.New("secure storage requires an OS keyring (Windows Credential Manager, macOS Keychain or Linux SecretService) — none is available")
	ErrSecureDisabled     = errors.New("secure storage is not enabled")
)

// secureMasterKey returns the master key, creating and storing a fresh
// random one on first use. A corrupt (undecodable) stored entry is
// replaced — but note that replacing the key of an existing encrypted
// store makes it unreadable, which openSecure reports as a tag failure.
func secureMasterKey() ([]byte, error) {
	if k, err := loadMasterKey(); err != nil || k != nil {
		if err != nil {
			return nil, err
		}
		return k, nil
	}
	k := make([]byte, keyLen)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	enc := base64.StdEncoding.EncodeToString(k)
	if err := keyring.Set(keyringService, masterAccount, enc); err != nil {
		return nil, err
	}
	return k, nil
}

// loadMasterKey reads the stored master key (nil when absent).
func loadMasterKey() ([]byte, error) {
	v, err := keyring.Get(keyringService, masterAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	k, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(k) != keyLen {
		return nil, nil // absent or corrupt: treat as missing
	}
	return k, nil
}

// deleteMasterKey removes the master key entry (disable path).
func deleteMasterKey() {
	_ = keyring.Delete(keyringService, masterAccount)
}

// isSecureEnvelope reports whether data carries the secure-store magic.
func isSecureEnvelope(data []byte) bool {
	return bytes.HasPrefix(data, []byte(secureMagic+"|"))
}

// sealSecure encrypts data into the s3bsf1 envelope:
//
//	s3bsf1|<b64 nonce 12B>|<b64 AES-256-GCM ciphertext>
func sealSecure(key, data []byte) ([]byte, error) {
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	gcm, err := newGCMAES(key)
	if err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, data, nil)
	var b strings.Builder
	b.WriteString(secureMagic)
	for _, part := range [][]byte{nonce, ct} {
		b.WriteByte('|')
		b.WriteString(base64.StdEncoding.EncodeToString(part))
	}
	return []byte(b.String() + "\n"), nil
}

// openSecure decrypts an s3bsf1 envelope. A tampered or wrong-key file
// fails the GCM tag; the error is deliberately generic (no oracle).
func openSecure(key, data []byte) ([]byte, error) {
	parts := strings.Split(strings.TrimSpace(string(data)), "|")
	if len(parts) != 3 || parts[0] != secureMagic {
		return nil, fmt.Errorf("corrupted secure store (bad envelope)")
	}
	nonce, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || len(nonce) != nonceLen {
		return nil, fmt.Errorf("corrupted secure store (bad nonce)")
	}
	ct, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil || len(ct) == 0 {
		return nil, fmt.Errorf("corrupted secure store (bad ciphertext)")
	}
	gcm, err := newGCMAES(key)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot decrypt the secure store (wrong key or tampered file)")
	}
	return plain, nil
}

// SecureEnabled reports whether this store is in secure mode (either
// loaded from an envelope or enabled this session).
func (s *Store) SecureEnabled() bool { return s.secure }

// EnableSecureStorage turns the store into its encrypted form: requires
// an OS keyring, ensures the master key exists, and rewrites the file
// as an envelope. Transparent to every caller of Load/Save afterwards.
func (s *Store) EnableSecureStorage() error {
	if !keyringAvailable() {
		return ErrSecureNeedsKeyring
	}
	if _, err := secureMasterKey(); err != nil {
		return err
	}
	s.secure = true
	if err := s.Save(); err != nil {
		s.secure = false
		return err
	}
	return nil
}

// DisableSecureStorage decrypts the store back to plain JSON and deletes
// the master key entry.
func (s *Store) DisableSecureStorage() error {
	if !s.secure {
		return ErrSecureDisabled
	}
	s.secure = false
	if err := s.Save(); err != nil {
		s.secure = true
		return err
	}
	deleteMasterKey()
	return nil
}

// KeyringAvailable reports whether an OS keyring is usable on this host
// (exported for the Settings → Security status panel).
func KeyringAvailable() bool { return keyringAvailable() }

// SecureModeOnDisk reports whether the store file at the default path is
// an encrypted envelope (the authoritative mode signal — used by the GUI
// status and the temp-workspace placement without loading the store).
func SecureModeOnDisk() bool {
	path, err := DefaultPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return isSecureEnvelope(data)
}
