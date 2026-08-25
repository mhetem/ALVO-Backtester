package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestMakeRefreshTokenIsHexOfTheExpectedWidth(t *testing.T) {
	token, err := MakeRefreshToken()
	if err != nil {
		t.Fatalf("MakeRefreshToken: %v", err)
	}

	if want := refreshTokenBytes * 2; len(token) != want {
		t.Errorf("token is %d characters, want %d", len(token), want)
	}
	if _, err := hex.DecodeString(token); err != nil {
		t.Errorf("token is not hex: %v", err)
	}
}

func TestHashRefreshTokenIsStableAndHidesTheToken(t *testing.T) {
	token, err := MakeRefreshToken()
	if err != nil {
		t.Fatalf("MakeRefreshToken: %v", err)
	}

	hash := HashRefreshToken(token)

	if hash == token {
		t.Fatal("the stored hash is the token itself")
	}
	if hash != HashRefreshToken(token) {
		t.Error("hashing the same token twice produced different digests")
	}
	if len(hash) != sha256.Size*2 {
		t.Errorf("digest is %d characters, want %d", len(hash), sha256.Size*2)
	}
	if _, err := hex.DecodeString(hash); err != nil {
		t.Errorf("digest is not hex: %v", err)
	}
}

func TestHashRefreshTokenSeparatesDistinctTokens(t *testing.T) {
	first, err := MakeRefreshToken()
	if err != nil {
		t.Fatalf("MakeRefreshToken: %v", err)
	}
	second, err := MakeRefreshToken()
	if err != nil {
		t.Fatalf("MakeRefreshToken: %v", err)
	}

	if HashRefreshToken(first) == HashRefreshToken(second) {
		t.Error("two different tokens hashed to the same digest")
	}
}

func TestMakeRefreshTokenDoesNotRepeat(t *testing.T) {
	const draws = 100

	seen := make(map[string]struct{}, draws)
	for range draws {
		token, err := MakeRefreshToken()
		if err != nil {
			t.Fatalf("MakeRefreshToken: %v", err)
		}
		if _, dup := seen[token]; dup {
			t.Fatalf("token %q was drawn twice", token)
		}
		seen[token] = struct{}{}
	}
}
