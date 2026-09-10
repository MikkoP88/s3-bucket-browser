// keyring.go stores profile secrets in the OS keyring (Windows Credential
// Manager / macOS Keychain / Linux SecretService) instead of the profiles
// JSON (M5, PLAN.md §14). When no keyring is available — headless boxes,
// test sandboxes, S3B_NO_KEYRING=1 — secrets transparently stay in the
// 0600 config file like before.
package profile

import (
	"errors"
	"os"
	"sync"

	"github.com/zalando/go-keyring"
)

const keyringService = "s3b"

var (
	keyringOnce   sync.Once
	keyringUsable bool
)

// keyringAvailable probes the OS keyring once and caches the verdict.
// The probe stores and deletes a throwaway entry. S3B_NO_KEYRING=1 skips
// the keyring entirely (checked live so tests can toggle it).
func keyringAvailable() bool {
	if os.Getenv("S3B_NO_KEYRING") == "1" {
		return false
	}
	keyringOnce.Do(func() {
		const probe = "s3b-keyring-probe"
		if err := keyring.Set(keyringService, probe, "x"); err != nil {
			keyringUsable = false
			return
		}
		_ = keyring.Delete(keyringService, probe)
		keyringUsable = true
	})
	return keyringUsable
}

// secretAccount is the keyring account name for a profile's secret kind
// ("secret" or "token").
func secretAccount(name, kind string) string {
	return "profiles/" + name + "/" + kind
}

// sourceSecretAccount is the keyring account for a source's secret kinds
// ("password", "s3secret", "s3token") — keyed by source ID so the entries
// live and die with the source, independent of any profile.
func sourceSecretAccount(id, kind string) string {
	return "sources/" + id + "/" + kind
}

// keyringPutAccount stores a secret under a full account name; an empty
// value deletes the entry.
func keyringPutAccount(account, value string) error {
	if value == "" {
		return keyring.Delete(keyringService, account)
	}
	return keyring.Set(keyringService, account, value)
}

// keyringPut stores a secret; an empty value deletes the entry.
func keyringPut(name, kind, value string) error {
	return keyringPutAccount(secretAccount(name, kind), value)
}

// keyringGet reads a secret; empty string when absent.
func keyringGet(name, kind string) (string, error) {
	v, err := keyring.Get(keyringService, secretAccount(name, kind))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return v, err
}

// keyringDeleteAll removes both keyring entries of a profile.
func keyringDeleteAll(name string) {
	_ = keyring.Delete(keyringService, secretAccount(name, "secret"))
	_ = keyring.Delete(keyringService, secretAccount(name, "token"))
}

// sourceKeyringDeleteAll removes every keyring entry of a source.
func sourceKeyringDeleteAll(id string) {
	for _, kind := range []string{"password", "s3secret", "s3token"} {
		_ = keyring.Delete(keyringService, sourceSecretAccount(id, kind))
	}
}

// migrateSecretsToKeyring moves every plaintext secret in the store into
// the OS keyring (when available), blanking the JSON fields. Returns how
// many profiles were migrated; 0 when the keyring is unavailable.
func (s *Store) migrateSecretsToKeyring() int {
	if !keyringAvailable() {
		return 0
	}
	moved := 0
	for i := range s.Profiles {
		p := &s.Profiles[i]
		if p.SecretKey == "" && p.SessionToken == "" {
			continue
		}
		if err := keyringPut(p.Name, "secret", p.SecretKey); err != nil {
			continue
		}
		if p.SessionToken != "" {
			if err := keyringPut(p.Name, "token", p.SessionToken); err != nil {
				continue
			}
		}
		p.SecretKey = ""
		p.SessionToken = ""
		p.SecretInKeyring = true
		moved++
	}
	moved += s.migrateSourceSecretsToKeyring()
	return moved
}

// migrateSourceSecretsToKeyring moves source secrets (sftp/ftp password,
// embedded S3 credentials) into source-scoped keyring accounts.
func (s *Store) migrateSourceSecretsToKeyring() int {
	moved := 0
	for i := range s.Sources {
		src := &s.Sources[i]
		if src.ID == "" {
			continue // no stable account name until persisted with an ID
		}
		if src.Password != "" {
			if err := keyringPutAccount(sourceSecretAccount(src.ID, "password"), src.Password); err == nil {
				src.Password = ""
				src.PassInKeyring = true
				moved++
			}
		}
		if src.S3 == nil || (src.S3.SecretKey == "" && src.S3.SessionToken == "") {
			continue
		}
		ok := true
		if src.S3.SecretKey != "" {
			ok = keyringPutAccount(sourceSecretAccount(src.ID, "s3secret"), src.S3.SecretKey) == nil
		}
		if ok && src.S3.SessionToken != "" {
			ok = keyringPutAccount(sourceSecretAccount(src.ID, "s3token"), src.S3.SessionToken) == nil
		}
		if ok {
			src.S3.SecretKey = ""
			src.S3.SessionToken = ""
			src.S3.SecretInKeyring = true
			moved++
		}
	}
	return moved
}

// hydrateSecretsFromKeyring restores secrets for profiles flagged as
// keyring-stored (called right after loading the store).
func (s *Store) hydrateSecretsFromKeyring() {
	if !keyringAvailable() {
		return
	}
	for i := range s.Profiles {
		p := &s.Profiles[i]
		if !p.SecretInKeyring || p.SecretKey != "" {
			continue
		}
		if v, err := keyringGet(p.Name, "secret"); err == nil {
			p.SecretKey = v
		}
		if v, err := keyringGet(p.Name, "token"); err == nil {
			p.SessionToken = v
		}
	}
	for i := range s.Sources {
		src := &s.Sources[i]
		if src.PassInKeyring && src.Password == "" {
			if v, err := keyring.Get(keyringService, sourceSecretAccount(src.ID, "password")); err == nil {
				src.Password = v
			}
		}
		if src.S3 == nil || !src.S3.SecretInKeyring || src.S3.SecretKey != "" {
			continue
		}
		if v, err := keyring.Get(keyringService, sourceSecretAccount(src.ID, "s3secret")); err == nil {
			src.S3.SecretKey = v
		}
		if v, err := keyring.Get(keyringService, sourceSecretAccount(src.ID, "s3token")); err == nil {
			src.S3.SessionToken = v
		}
	}
}
