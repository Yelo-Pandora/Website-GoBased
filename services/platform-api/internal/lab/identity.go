package lab

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func newLabID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate lab id: %w", err)
	}
	return "lab-" + hex.EncodeToString(data), nil
}

func validLabID(value string) bool {
	if len(value) < 4 || len(value) > 64 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		if r == '-' && i > 0 {
			continue
		}
		return false
	}
	return true
}

func validOperationID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && (r == '-' || r == '_' || r == '.') {
			continue
		}
		return false
	}
	return true
}
