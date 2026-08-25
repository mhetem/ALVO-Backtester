package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mhetem/ALVO-Backtester/internal/auth"
	"github.com/mhetem/ALVO-Backtester/internal/config"
)

const testJWTSecret = "a-test-signing-secret"

func testAuthServer(t *testing.T) *Server {
	t.Helper()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(config.Config{JWTSecret: testJWTSecret}, nil, log, nil, nil)
}

func postJSON(t *testing.T, handler http.HandlerFunc, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler(rec, req)
	return rec
}

func postWithAuth(t *testing.T, handler http.Handler, target, header string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, target, nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	handler.ServeHTTP(rec, req)
	return rec
}

func TestRegisterRejectsBadRequestsBeforeTouchingTheDatabase(t *testing.T) {
	server := testAuthServer(t)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"not json", `nope`, "request body must be a JSON object"},
		{"empty body", ``, "request body must be a JSON object"},
		{"no email", `{"password":"correct horse"}`, "email is required"},
		{"blank email", `{"email":"   ","password":"correct horse"}`, "email is required"},
		{"no at sign", `{"email":"trader","password":"correct horse"}`, "email must be a plain address"},
		{"display name form", `{"email":"Trader <t@example.com>","password":"correct horse"}`, "email must be a plain address"},
		{"short password", `{"email":"t@example.com","password":"short"}`, "password must be at least 8 characters"},
		{"no password", `{"email":"t@example.com"}`, "password must be at least 8 characters"},
		{"long password", `{"email":"t@example.com","password":"` + strings.Repeat("x", maxPasswordLen+1) + `"}`, "password must be at most 256 characters"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(t, server.handleRegister, "/api/v1/auth/register", tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
			if got := decodeError(t, rec); !strings.Contains(got, tc.want) {
				t.Errorf("error is %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestLoginRejectsBadRequestsBeforeTouchingTheDatabase(t *testing.T) {
	server := testAuthServer(t)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"not json", `nope`, "request body must be a JSON object"},
		{"no email", `{"password":"correct horse"}`, "email is required"},
		{"malformed email", `{"email":"trader@","password":"correct horse"}`, "email must be a plain address"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postJSON(t, server.handleLogin, "/api/v1/auth/login", tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
			if got := decodeError(t, rec); !strings.Contains(got, tc.want) {
				t.Errorf("error is %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestRefreshAndRevokeRejectABadAuthorizationHeader(t *testing.T) {
	server := testAuthServer(t)

	handlers := map[string]http.HandlerFunc{
		"refresh": server.handleRefresh,
		"revoke":  server.handleRevoke,
	}
	headers := map[string]string{
		"missing":      "",
		"wrong scheme": "Basic abc123",
		"bare token":   "abc123",
		"scheme only":  "Bearer",
	}

	for endpoint, handler := range handlers {
		for name, header := range headers {
			t.Run(endpoint+"/"+name, func(t *testing.T) {
				rec := postWithAuth(t, handler, "/api/v1/auth/"+endpoint, header)

				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("status %d, want 401", rec.Code)
				}
				if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate is %q, want %q", got, "Bearer")
				}
			})
		}
	}
}

func TestRequireAuthRejectsAnythingButAValidAccessToken(t *testing.T) {
	server := testAuthServer(t)

	expired, err := auth.MakeJWT(uuid.New(), testJWTSecret, -time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}
	foreign, err := auth.MakeJWT(uuid.New(), "a-different-secret", time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}

	cases := map[string]string{
		"missing":             "",
		"not a token":         "Bearer nonsense",
		"expired":             "Bearer " + expired,
		"signed elsewhere":    "Bearer " + foreign,
		"refresh token shape": "Bearer 6f1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e",
	}

	protected := server.requireAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the protected handler ran")
	}))

	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			rec := postWithAuth(t, protected, "/api/v1/admin/brapi-usage", header)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Errorf("WWW-Authenticate is %q, want %q", got, "Bearer")
			}
		})
	}
}

func TestRequireAuthPutsTheUserIDInTheContext(t *testing.T) {
	server := testAuthServer(t)
	want := uuid.New()

	token, err := auth.MakeJWT(want, testJWTSecret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}

	var got uuid.UUID
	var ok bool
	protected := server.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = UserIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := postWithAuth(t, protected, "/api/v1/admin/brapi-usage", "Bearer "+token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if !ok {
		t.Fatal("the context carried no user id")
	}
	if got != want {
		t.Errorf("user id is %s, want %s", got, want)
	}
}

func TestUserIDFromIsAbsentWithoutRequireAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/candles", nil)

	if _, ok := UserIDFrom(req.Context()); ok {
		t.Error("an unauthenticated request carried a user id")
	}
}

func TestNormalizeEmailLowercasesSoOneAddressIsOneAccount(t *testing.T) {
	cases := map[string]string{
		"Trader@Example.com":   "trader@example.com",
		"  trader@example.com": "trader@example.com",
		"TRADER@EXAMPLE.COM":   "trader@example.com",
	}

	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			got, err := normalizeEmail(input)
			if err != nil {
				t.Fatalf("normalizeEmail: %v", err)
			}
			if got != want {
				t.Errorf("email is %q, want %q", got, want)
			}
		})
	}
}
