package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const IssuerAccess = "alvo-access"

var (
	ErrNoAuthHeader = errors.New("missing authorization header")
	ErrNotBearer    = errors.New("authorization header must be a bearer token")
	ErrEmptyToken   = errors.New("authorization header carries no token")
)

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	now := time.Now().UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    IssuerAccess,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(expiresIn)),
		Subject:   userID.String(),
	})
	return token.SignedString([]byte(tokenSecret))
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	var claims jwt.RegisteredClaims

	_, err := jwt.ParseWithClaims(
		tokenString,
		&claims,
		func(*jwt.Token) (any, error) { return []byte(tokenSecret), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(IssuerAccess),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return uuid.Nil, err
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid subject: %w", err)
	}

	return id, nil
}

func GetBearerToken(headers http.Header) (string, error) {
	value := strings.TrimSpace(headers.Get("Authorization"))
	if value == "" {
		return "", ErrNoAuthHeader
	}

	scheme, token, found := strings.Cut(value, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", ErrNotBearer
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return "", ErrEmptyToken
	}

	return token, nil
}
