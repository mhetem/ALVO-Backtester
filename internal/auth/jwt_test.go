package auth

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testSecret = "a-test-signing-secret"

func TestValidateJWTRoundTripsTheUserID(t *testing.T) {
	want := uuid.New()

	token, err := MakeJWT(want, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}

	got, err := ValidateJWT(token, testSecret)
	if err != nil {
		t.Fatalf("ValidateJWT: %v", err)
	}
	if got != want {
		t.Errorf("user id is %s, want %s", got, want)
	}
}

func TestValidateJWTRejectsAnExpiredToken(t *testing.T) {
	token, err := MakeJWT(uuid.New(), testSecret, -time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}

	if _, err := ValidateJWT(token, testSecret); err == nil {
		t.Fatal("an expired token validated")
	} else if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error is %q, want it to name expiry", err)
	}
}

func TestValidateJWTRejectsATamperedToken(t *testing.T) {
	token, err := MakeJWT(uuid.New(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}

	other, err := MakeJWT(uuid.New(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}
	otherParts := strings.Split(other, ".")

	cases := map[string]string{
		"swapped payload":   parts[0] + "." + otherParts[1] + "." + parts[2],
		"swapped signature": parts[0] + "." + parts[1] + "." + otherParts[2],
		"truncated":         parts[0] + "." + parts[1],
		"garbage":           "not-a-token",
	}

	for name, tampered := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateJWT(tampered, testSecret); err == nil {
				t.Error("a tampered token validated")
			}
		})
	}
}

func TestValidateJWTRejectsAnotherSecret(t *testing.T) {
	token, err := MakeJWT(uuid.New(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}

	if _, err := ValidateJWT(token, "a-different-secret"); err == nil {
		t.Fatal("a token signed with another secret validated")
	}
}

func TestValidateJWTRejectsTheNoneAlgorithm(t *testing.T) {
	now := time.Now().UTC()
	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Issuer:    IssuerAccess,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		Subject:   uuid.New().String(),
	})

	token, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("signing with alg none: %v", err)
	}

	if _, err := ValidateJWT(token, testSecret); err == nil {
		t.Fatal("an unsigned token validated")
	}
}

func TestValidateJWTRejectsAForeignIssuer(t *testing.T) {
	now := time.Now().UTC()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "chirpy-access",
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		Subject:   uuid.New().String(),
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	if _, err := ValidateJWT(token, testSecret); err == nil {
		t.Fatal("a token from another issuer validated")
	}
}

func TestValidateJWTRejectsATokenWithoutExpiry(t *testing.T) {
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:   IssuerAccess,
		IssuedAt: jwt.NewNumericDate(time.Now().UTC()),
		Subject:  uuid.New().String(),
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	if _, err := ValidateJWT(token, testSecret); err == nil {
		t.Fatal("a token with no expiry validated")
	}
}

func TestValidateJWTRejectsANonUUIDSubject(t *testing.T) {
	now := time.Now().UTC()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    IssuerAccess,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		Subject:   "42",
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	if _, err := ValidateJWT(token, testSecret); err == nil {
		t.Fatal("a token with a non-uuid subject validated")
	}
}

func TestGetBearerToken(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
		wantOK bool
	}{
		{"missing", "", "", false},
		{"bearer", "Bearer abc123", "abc123", true},
		{"lowercase scheme", "bearer abc123", "abc123", true},
		{"padded", "  Bearer   abc123  ", "abc123", true},
		{"scheme only", "Bearer", "", false},
		{"scheme and blank", "Bearer   ", "", false},
		{"wrong scheme", "Basic abc123", "", false},
		{"bare token", "abc123", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{}
			if tc.header != "" {
				headers.Set("Authorization", tc.header)
			}

			got, err := GetBearerToken(headers)
			if tc.wantOK && err != nil {
				t.Fatalf("GetBearerToken: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatalf("header %q returned token %q, want an error", tc.header, got)
			}
			if got != tc.want {
				t.Errorf("token is %q, want %q", got, tc.want)
			}
		})
	}
}
