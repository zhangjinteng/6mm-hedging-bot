package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"testing"
)

func TestDecryptsAgentAdminAESGCMLayout(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("0123456789ab")
	sealed := aead.Seal(nil, nonce, []byte("exchange-secret"), []byte(additionalData))
	ciphertext := sealed[:len(sealed)-aead.Overhead()]
	tag := sealed[len(sealed)-aead.Overhead():]
	value := prefix + base64.StdEncoding.EncodeToString(append(append(nonce, tag...), ciphertext...))

	codec, err := NewCodec(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := codec.Decrypt(value)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext != "exchange-secret" {
		t.Fatalf("unexpected plaintext %q", plaintext)
	}
}

func TestDecryptLegacyPlaintext(t *testing.T) {
	codec, err := NewCodec("")
	if err != nil {
		t.Fatal(err)
	}
	value, err := codec.Decrypt("legacy-key")
	if err != nil {
		t.Fatal(err)
	}
	if value != "legacy-key" {
		t.Fatalf("unexpected legacy value %q", value)
	}
}

func TestRejectEncryptedValueWithoutKey(t *testing.T) {
	codec, err := NewCodec("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decrypt("v1:AAAA"); err == nil {
		t.Fatal("expected missing-key error")
	}
}

func TestRejectInvalidKeyLength(t *testing.T) {
	_, err := NewCodec(base64.StdEncoding.EncodeToString([]byte("short")))
	if err == nil {
		t.Fatal("expected invalid key length error")
	}
}
