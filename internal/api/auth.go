package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mhetem/ALVO-Backtester/internal/auth"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 60 * 24 * time.Hour
	minPasswordLen  = 8
	maxPasswordLen  = 256
	uniqueViolation = "23505"
)

const (
	msgBadCredentials = "incorrect email or password"
	msgBadRefresh     = "invalid or expired refresh token"
	msgBadAccess      = "invalid or expired access token"
)

const (
	refreshCookieName = "alvo_refresh"
	refreshCookiePath = "/api/v1/auth"
)

type credentialsBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userBody struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type sessionBody struct {
	User         userBody `json:"user"`
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	ExpiresIn    int      `json:"expires_in"`
}

type accessBody struct {
	User        userBody `json:"user"`
	AccessToken string   `json:"access_token"`
	ExpiresIn   int      `json:"expires_in"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body credentialsBody
	if err := decodeJSON(w, r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	email, err := normalizeEmail(body.Email)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePassword(body.Password); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	hashed, err := auth.HashPassword(body.Password)
	if err != nil {
		s.logError(r, "hashing password", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	row, err := s.queries.CreateUser(r.Context(), database.CreateUserParams{
		Email:          email,
		HashedPassword: hashed,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			respondError(w, r, http.StatusConflict, "email already registered")
			return
		}
		s.logError(r, "creating user", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusCreated, userBody{
		ID:        row.ID,
		Email:     row.Email,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body credentialsBody
	if err := decodeJSON(w, r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	email, err := normalizeEmail(body.Email)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	user, err := s.queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusUnauthorized, msgBadCredentials)
			return
		}
		s.logError(r, "looking up user", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	match, err := auth.CheckPasswordHash(body.Password, user.HashedPassword)
	if err != nil {
		s.logError(r, "comparing password hash", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if !match {
		respondError(w, r, http.StatusUnauthorized, msgBadCredentials)
		return
	}

	session, err := s.issueSession(r.Context(), userBody{
		ID:        user.ID,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	})
	if err != nil {
		s.logError(r, "issuing session", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	setRefreshCookie(w, session.RefreshToken)
	respondJSON(w, r, http.StatusOK, session)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	token := refreshCredential(r)
	if token == "" {
		respondUnauthorized(w, r, msgBadRefresh)
		return
	}

	user, err := s.queries.GetUserFromRefreshToken(r.Context(), auth.HashRefreshToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondUnauthorized(w, r, msgBadRefresh)
			return
		}
		s.logError(r, "resolving refresh token", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	access, err := auth.MakeJWT(user.ID, s.cfg.JWTSecret, accessTokenTTL)
	if err != nil {
		s.logError(r, "signing access token", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusOK, accessBody{
		User: userBody{
			ID:        user.ID,
			Email:     user.Email,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
		},
		AccessToken: access,
		ExpiresIn:   int(accessTokenTTL.Seconds()),
	})
}

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	token := refreshCredential(r)
	clearRefreshCookie(w)
	if token == "" {
		respondUnauthorized(w, r, msgBadRefresh)
		return
	}

	revoked, err := s.queries.RevokeRefreshToken(r.Context(), auth.HashRefreshToken(token))
	if err != nil {
		s.logError(r, "revoking refresh token", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if revoked == 0 {
		respondUnauthorized(w, r, msgBadRefresh)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := auth.GetBearerToken(r.Header)
		if err != nil {
			respondUnauthorized(w, r, err.Error())
			return
		}

		userID, err := auth.ValidateJWT(token, s.cfg.JWTSecret)
		if err != nil {
			respondUnauthorized(w, r, msgBadAccess)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyUserID, userID)))
	})
}

func (s *Server) issueSession(ctx context.Context, user userBody) (sessionBody, error) {
	access, err := auth.MakeJWT(user.ID, s.cfg.JWTSecret, accessTokenTTL)
	if err != nil {
		return sessionBody{}, err
	}

	refresh, err := auth.MakeRefreshToken()
	if err != nil {
		return sessionBody{}, err
	}

	if err := s.queries.CreateRefreshToken(ctx, database.CreateRefreshTokenParams{
		TokenHash: auth.HashRefreshToken(refresh),
		UserID:    user.ID,
		ExpiresAt: time.Now().UTC().Add(refreshTokenTTL),
	}); err != nil {
		return sessionBody{}, err
	}

	return sessionBody{
		User:         user,
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
	}, nil
}

func (s *Server) logError(r *http.Request, msg string, err error) {
	s.log.ErrorContext(r.Context(), msg,
		slog.String("request_id", RequestIDFrom(r.Context())),
		slog.Any("err", err),
	)
}

func refreshCredential(r *http.Request) string {
	if token, err := auth.GetBearerToken(r.Header); err == nil {
		return token
	}
	if cookie, err := r.Cookie(refreshCookieName); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

func setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		MaxAge:   int(refreshTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func respondUnauthorized(w http.ResponseWriter, r *http.Request, msg string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	respondError(w, r, http.StatusUnauthorized, msg)
}

func normalizeEmail(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.New("email is required")
	}

	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Name != "" || parsed.Address != trimmed {
		return "", errors.New("email must be a plain address, as in trader@example.com")
	}

	return strings.ToLower(parsed.Address), nil
}

func validatePassword(value string) error {
	switch {
	case len(value) < minPasswordLen:
		return fmt.Errorf("password must be at least %d characters", minPasswordLen)
	case len(value) > maxPasswordLen:
		return fmt.Errorf("password must be at most %d characters", maxPasswordLen)
	default:
		return nil
	}
}
