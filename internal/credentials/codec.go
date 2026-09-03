package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	prefix         = "v1:"
	additionalData = "hedging-credentials:v1"
)

// Codec decrypts credentials written by agent_admin. Values without the v1
// prefix are treated as legacy plaintext so existing test accounts continue to
// work while they are rotated.
type Codec struct {
	aead cipher.AEAD
}

func NewCodec(encodedKey string) (*Codec, error) {
	encodedKey = strings.TrimSpace(encodedKey)
	if encodedKey == "" {
		return &Codec{}, nil
	}

	encodedKey = strings.TrimPrefix(encodedKey, "base64:")
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("decode credential encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("credential encryption key must decode to 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create credential gcm: %w", err)
	}
	return &Codec{aead: aead}, nil
}

func (c *Codec) Decrypt(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, prefix) {
		return value, nil
	}
	if c == nil || c.aead == nil {
		return "", errors.New("HEDGING_CREDENTIAL_KEY is required for encrypted credentials")
	}

	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted credential: %w", err)
	}
	nonceSize := c.aead.NonceSize()
	tagSize := c.aead.Overhead()
	if len(payload) < nonceSize+tagSize {
		return "", errors.New("encrypted credential payload is too short")
	}

	nonce := payload[:nonceSize]
	tag := payload[nonceSize : nonceSize+tagSize]
	ciphertext := payload[nonceSize+tagSize:]
	sealed := append(append(make([]byte, 0, len(ciphertext)+len(tag)), ciphertext...), tag...)
	plaintext, err := c.aead.Open(nil, nonce, sealed, []byte(additionalData))
	if err != nil {
		return "", errors.New("decrypt credential failed")
	}
	return string(plaintext), nil
}
