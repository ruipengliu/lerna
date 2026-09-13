// Package randomid produces opaque random values without assigning domain meaning.
package randomid

import (
	"crypto/rand"
	"encoding/hex"
)

func New() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
