package microsoft

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"golang.org/x/oauth2"
)

// credentialVersion labels sealed blobs and is authenticated as AAD, so a
// blob cannot be replayed under a different format.
const credentialVersion = "v1."

// sealToken serializes the token and seals it with AES-256-GCM under the
// deployment key from APP_ENCRYPTION_KEY, using a fresh random nonce per
// blob. Format: "v1." + base64(nonce || ciphertext).
func (s *Service) sealToken(token *oauth2.Token) ([]byte, error) {
	plaintext, err := json.Marshal(token)
	if err != nil {
		return nil, fmt.Errorf("encoding the Microsoft token failed: %w", err)
	}
	aead, err := s.cipher()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("gathering randomness failed: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, plaintext, []byte(credentialVersion))
	return []byte(credentialVersion + base64.StdEncoding.EncodeToString(sealed)), nil
}

// openCredential unseals a stored blob. Missing key, wrong key, truncation,
// or tampering all report ErrCredentialsUnreadable.
func (s *Service) openCredential(stored string) ([]byte, error) {
	invalid := fmt.Errorf("%w: check that APP_ENCRYPTION_KEY matches the key used to connect the mailbox", ErrCredentialsUnreadable)
	if !strings.HasPrefix(stored, credentialVersion) {
		return nil, invalid
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, credentialVersion))
	if err != nil {
		return nil, invalid
	}
	aead, err := s.cipher()
	if err != nil {
		return nil, err
	}
	if len(raw) < aead.NonceSize() {
		return nil, invalid
	}
	nonce, ciphertext := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(credentialVersion))
	if err != nil {
		return nil, invalid
	}
	return plaintext, nil
}

// cipher builds the AES-256-GCM primitive from the configured key. A missing
// or malformed key is reported as unreadable credentials so callers leave
// the stored ciphertext alone.
func (s *Service) cipher() (cipher.AEAD, error) {
	key, err := s.cfg.EncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCredentialsUnreadable, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCredentialsUnreadable, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCredentialsUnreadable, err)
	}
	return aead, nil
}
