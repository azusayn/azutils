package crypto

import (
	"crypto/sha256"
	"fmt"
)

// hash salt and password with SHA-256.
func Sha256(text string, salt string) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(salt))
	_, _ = hasher.Write([]byte(text))
	return fmt.Sprintf("%x", hasher.Sum(nil))
}
