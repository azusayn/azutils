package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
)

// Sha256 computes the SHA-256 digest of the given inputs in order and
// returns it as a hex string.
func Sha256(inputs ...string) string {
	hasher := sha256.New()
	for _, input := range inputs {
		_, _ = hasher.Write([]byte(input))
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

// GenerateRandomHexString returns a hex string of random bytes with the
// specified length.
func GenerateRandomHexString(length int) string {
	salt := make([]byte, length)
	_, _ = rand.Read(salt)
	return fmt.Sprintf("%x", salt)
}

// GenerateRandomBytes returns a slice of random bytes with the specified length.
func GenerateRandomBytes(length int) []byte {
	bytes := make([]byte, length)
	_, _ = rand.Read(bytes)
	return bytes
}
