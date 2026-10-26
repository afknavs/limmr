package remote

import (
	"crypto/sha256"
	"encoding/base64"
)

func shaChallenge(v string) string {
	b := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func validVerifier(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~') {
			return false
		}
	}
	return true
}
func singleValues(v map[string][]string) bool {
	for _, values := range v {
		if len(values) != 1 {
			return false
		}
	}
	return true
}
