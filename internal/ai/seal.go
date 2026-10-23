package ai

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// sealedVersion labels sealed API keys. The owner and the stored credential
// version are authenticated as additional data, so a blob cannot be moved to
// another operator or replayed under a different credential generation.
const sealedVersion = "v1."

func sealedAAD(ownerID string, credentialGeneration int) []byte {
	return []byte(fmt.Sprintf("%sai|%s|%d", sealedVersion, ownerID, credentialGeneration))
}

func (s *Store) cipher() (cipher.AEAD, error) {
	if s.key == nil {
		return nil, fmt.Errorf("%w: APP_ENCRYPTION_KEY is not set", ErrCredentialUnreadable)
	}
	key, err := s.key()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCredentialUnreadable, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCredentialUnreadable, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCredentialUnreadable, err)
	}
	return aead, nil
}

func (s *Store) sealKey(ownerID string, credentialGeneration int, apiKey string) (string, error) {
	aead, err := s.cipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("gather randomness: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(apiKey), sealedAAD(ownerID, credentialGeneration))
	return sealedVersion + base64.StdEncoding.EncodeToString(sealed), nil
}

func (s *Store) openKey(ownerID string, credentialGeneration int, stored string) (string, error) {
	invalid := ErrCredentialUnreadable
	if stored == "" || !strings.HasPrefix(stored, sealedVersion) {
		return "", invalid
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, sealedVersion))
	if err != nil {
		return "", invalid
	}
	aead, err := s.cipher()
	if err != nil {
		return "", err
	}
	if len(raw) < aead.NonceSize() {
		return "", invalid
	}
	nonce, ciphertext := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, sealedAAD(ownerID, credentialGeneration))
	if err != nil {
		return "", invalid
	}
	if len(plaintext) == 0 {
		return "", invalid
	}
	return string(plaintext), nil
}
