package instance

import (
	"crypto/rand"
	"encoding/hex"
)

// GenerateSuffix returns a random 4-character lowercase hex string ([0-9a-f])
// suitable for appending to instance names to avoid collisions.
func GenerateSuffix() (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// AppendSuffix returns name + "-" + a random 4-char suffix.
func AppendSuffix(name string) (string, error) {
	suffix, err := GenerateSuffix()
	if err != nil {
		return "", err
	}
	return name + "-" + suffix, nil
}
