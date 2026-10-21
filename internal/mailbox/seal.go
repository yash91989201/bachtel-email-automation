package mailbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// credentialVersion labels sealed blobs. The owner, provider and connection id
// are authenticated as additional data, so a blob cannot be moved to another
// connection or replayed under a different payload provider.
const credentialVersion = "v1."

func credentialAAD(ownerID string, provider Provider, connectionID string) []byte {
	return []byte(credentialVersion + "|mailbox|" + ownerID + "|" + string(provider) + "|" + connectionID)
}

func (s *Store) cipher() (cipher.AEAD, error) {
	if s.key == nil {
		return nil, fmt.Errorf("%w: APP_ENCRYPTION_KEY is not set", ErrCredentialsUnreadable)
	}
	key, err := s.key()
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

func (s *Store) seal(ownerID string, provider Provider, connectionID string, credential Credential) (string, error) {
	if credential.Provider != provider {
		return "", ErrUnsupportedProvider
	}
	plaintext, err := json.Marshal(credential)
	if err != nil {
		return "", fmt.Errorf("encode the mailbox credential: %w", err)
	}
	aead, err := s.cipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("gather randomness: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, plaintext, credentialAAD(ownerID, provider, connectionID))
	return credentialVersion + base64.StdEncoding.EncodeToString(sealed), nil
}

func (s *Store) open(ownerID string, provider Provider, connectionID, stored string) (Credential, error) {
	invalid := fmt.Errorf("%w: check that APP_ENCRYPTION_KEY matches the key used to connect the mailbox", ErrCredentialsUnreadable)
	if stored == "" || !strings.HasPrefix(stored, credentialVersion) {
		return Credential{}, invalid
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, credentialVersion))
	if err != nil {
		return Credential{}, invalid
	}
	aead, err := s.cipher()
	if err != nil {
		return Credential{}, err
	}
	if len(raw) < aead.NonceSize() {
		return Credential{}, invalid
	}
	nonce, ciphertext := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, credentialAAD(ownerID, provider, connectionID))
	if err != nil {
		return Credential{}, invalid
	}
	var credential Credential
	if err := json.Unmarshal(plaintext, &credential); err != nil {
		return Credential{}, invalid
	}
	if credential.Provider != provider {
		return Credential{}, invalid
	}
	return credential, nil
}
