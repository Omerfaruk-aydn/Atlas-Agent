//go:build !windows

package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
)

func keyCipher() (cipher.AEAD, error) {
	key, err := base64.StdEncoding.DecodeString(os.Getenv("ATLAS_VAULT_KEY"))
	if err != nil || len(key) != 32 {
		return nil, errors.New("configure ATLAS_VAULT_KEY with a base64 32-byte key from your secret manager")
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(data []byte) ([]byte, error) {
	c, err := keyCipher()
	if err != nil {
		return nil, err
	}
	n := make([]byte, c.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return nil, err
	}
	return c.Seal(n, n, data, []byte("atlas-vault-v1")), nil
}

func unseal(data []byte) ([]byte, error) {
	c, err := keyCipher()
	if err != nil {
		return nil, err
	}
	if len(data) < c.NonceSize() {
		return nil, errors.New("invalid encrypted credential")
	}
	return c.Open(nil, data[:c.NonceSize()], data[c.NonceSize():], []byte("atlas-vault-v1"))
}
