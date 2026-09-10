// Encrypted Profile files (M8, PLAN-v2 §M8): a named collection of data
// sources serialized as one password-encrypted file (*.s3bprofile).
//
// Container format (single line, "|"-separated, all binary fields base64):
//
//	s3bpf1|<salt 16B>|<nonce 12B>|<AES-256-GCM ciphertext>
//
// The key is derived from the password with scrypt (N=32768, r=8, p=1 —
// ~32 MiB, a few hundred ms on open; the file is read rarely). A wrong
// password fails the GCM tag: ErrWrongPassword, no partial plaintext.
package profile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

// Container magic and crypto parameters.
const (
	containerMagic = "s3bpf1"
	scryptN        = 32768
	scryptR        = 8
	scryptP        = 1
	keyLen         = 32 // AES-256
	saltLen        = 16
	nonceLen       = 12
)

// Errors surfaced to the UI.
var (
	ErrWrongPassword = errors.New("wrong password or corrupted profile file")
	ErrBadFormat     = errors.New("not an s3b profile file (or unsupported version)")
)

// containerPayload is the plaintext JSON inside the ciphertext.
type containerPayload struct {
	Name    string    `json:"name"`
	SavedAt time.Time `json:"savedAt"`
	Sources []Source  `json:"sources"`
}

// EncryptContainer seals the sources into the s3bpf1 container format.
func EncryptContainer(name string, srcs []Source, password string) ([]byte, error) {
	if password == "" {
		return nil, errors.New("password is required")
	}
	plain, err := json.Marshal(containerPayload{Name: name, SavedAt: time.Now().UTC(), Sources: srcs})
	if err != nil {
		return nil, err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCMAES(key)
	if err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plain, nil)

	var b strings.Builder
	b.WriteString(containerMagic)
	for _, part := range [][]byte{salt, nonce, ct} {
		b.WriteByte('|')
		b.WriteString(base64.StdEncoding.EncodeToString(part))
	}
	return []byte(b.String() + "\n"), nil
}

// DecryptContainer opens an s3bpf1 container and returns the profile name
// and its sources. A wrong password and a corrupted file are deliberately
// indistinguishable (ErrWrongPassword).
func DecryptContainer(data []byte, password string) (string, []Source, error) {
	parts := strings.Split(strings.TrimSpace(string(data)), "|")
	if len(parts) != 4 || parts[0] != containerMagic {
		return "", nil, ErrBadFormat
	}
	salt, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, ErrBadFormat
	}
	nonce, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", nil, ErrBadFormat
	}
	ct, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return "", nil, ErrBadFormat
	}
	if len(salt) != saltLen || len(nonce) != nonceLen || len(ct) == 0 {
		return "", nil, ErrBadFormat
	}
	key, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return "", nil, err
	}
	gcm, err := newGCMAES(key)
	if err != nil {
		return "", nil, err
	}
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", nil, ErrWrongPassword
	}
	var p containerPayload
	if err := json.Unmarshal(plain, &p); err != nil {
		return "", nil, ErrWrongPassword // authenticated but not our payload
	}
	return p.Name, p.Sources, nil
}

// aes.NewCipher returns an error only for invalid key sizes; keyLen is a
// constant, so wrap the two call sites to keep them compact.
func newGCMAES(key []byte) (cipher.AEAD, error) {
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	return cipher.NewGCM(c)
}
