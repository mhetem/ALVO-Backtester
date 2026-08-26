package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

const refreshTokenBytes = 32

func MakeRefreshToken() (string, error) {
	key := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}

	return hex.EncodeToString(key), nil
}

func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

const shareTokenBytes = 16

func MakeShareToken() (string, error) {
	key := make([]byte, shareTokenBytes)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(key), nil
}
