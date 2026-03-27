package common

import (
	"crypto/rand"
	"encoding/hex"
)

func NewID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "fallback-id"
	}
	return hex.EncodeToString(buf)
}
