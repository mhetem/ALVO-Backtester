package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mhetem/ALVO-Backtester/internal/auth"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

const sharePrefix = "/s/"

type shareBody struct {
	Token    string     `json:"token"`
	Path     string     `json:"path"`
	SharedAt *time.Time `json:"shared_at,omitempty"`
}

// What a link hands over: the strategy itself and nothing about who wrote it. The owner is
// deliberately absent — a shared spec is a recipe, not an introduction.
type sharedStrategyBody struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Version     int32           `json:"version"`
	Spec        json.RawMessage `json:"spec"`
	Plan        *planBody       `json:"plan,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	SharedAt    *time.Time      `json:"shared_at,omitempty"`
}

func (s *Server) handleShareStrategy(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, ok := strategyID(w, r)
	if !ok {
		return
	}

	token, err := auth.MakeShareToken()
	if err != nil {
		s.logError(r, "minting a share token", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	row, err := s.queries.ShareStrategy(r.Context(), database.ShareStrategyParams{
		ID:         id,
		UserID:     userID,
		ShareToken: &token,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such strategy")
			return
		}
		s.logError(r, "sharing a strategy", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusOK, shareOf(row))
}

func (s *Server) handleUnshareStrategy(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, ok := strategyID(w, r)
	if !ok {
		return
	}

	if _, err := s.queries.UnshareStrategy(r.Context(), database.UnshareStrategyParams{ID: id, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such strategy")
			return
		}
		s.logError(r, "unsharing a strategy", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetSharedStrategy(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("token"))
	if token == "" {
		respondError(w, r, http.StatusNotFound, "no such shared strategy")
		return
	}

	row, err := s.queries.GetSharedStrategy(r.Context(), &token)
	if err != nil {
		// A revoked link and a link that never existed answer the same way. Telling the
		// two apart would turn the endpoint into a way to probe for tokens.
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such shared strategy")
			return
		}
		s.logError(r, "reading a shared strategy", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	body := sharedStrategyBody{
		Name:        row.Name,
		Description: row.Description,
		Version:     row.Version,
		Spec:        json.RawMessage(row.Spec),
		Plan:        s.replan(r, database.Strategy{ID: row.ID, Spec: row.Spec}),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		SharedAt:    row.SharedAt,
	}

	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, r, http.StatusOK, body)
}

func shareOf(row database.Strategy) shareBody {
	body := shareBody{SharedAt: row.SharedAt}
	if row.ShareToken != nil {
		body.Token = *row.ShareToken
		body.Path = sharePrefix + *row.ShareToken
	}

	return body
}
