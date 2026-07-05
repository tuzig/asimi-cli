package tools

import (
	"crypto/sha256"
	"encoding/hex"
)

// GenerateID creates a unique ID using SHA256.
// Tools use this to generate manifest, verdict, ling, and precedent IDs
// without depending on the shogunate package.
func GenerateID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
