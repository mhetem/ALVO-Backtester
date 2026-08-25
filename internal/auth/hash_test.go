package auth

import (
	"strings"
	"testing"
)

func TestHashPasswordVerifiesTheSamePassword(t *testing.T) {
	const password = "correct horse battery staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(hash, password) {
		t.Fatal("the hash contains the password verbatim")
	}

	match, err := CheckPasswordHash(password, hash)
	if err != nil {
		t.Fatalf("CheckPasswordHash: %v", err)
	}
	if !match {
		t.Error("the password did not verify against its own hash")
	}
}

func TestCheckPasswordHashRejectsTheWrongPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	match, err := CheckPasswordHash("Correct horse battery staple", hash)
	if err != nil {
		t.Fatalf("CheckPasswordHash: %v", err)
	}
	if match {
		t.Error("a wrong password verified")
	}
}

func TestHashPasswordSaltsEachHash(t *testing.T) {
	const password = "correct horse battery staple"

	first, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if first == second {
		t.Error("hashing the same password twice produced the same hash")
	}
}

func TestCheckPasswordHashFailsOnAMalformedHash(t *testing.T) {
	if _, err := CheckPasswordHash("whatever", "not-an-argon2id-hash"); err == nil {
		t.Error("a malformed hash compared without an error")
	}
}
