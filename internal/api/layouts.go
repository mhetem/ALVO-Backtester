package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/indicator"
)

const (
	layoutVersion  = 1
	PaletteSlots   = 10
	maxLayoutName  = 60
	maxUserLayouts = 50

	styleSolid  = "solid"
	styleDotted = "dotted"
)

var lineStyles = []string{styleSolid, styleDotted}

type layoutEntryRequest struct {
	Key     string `json:"key"`
	Visible *bool  `json:"visible"`
	Style   string `json:"style"`
	Colors  []int  `json:"colors"`
}

type layoutRequest struct {
	Name       string               `json:"name"`
	Version    int                  `json:"version"`
	Indicators []layoutEntryRequest `json:"indicators"`
}

type layoutEntryBody struct {
	Key     string `json:"key"`
	Visible bool   `json:"visible"`
	Style   string `json:"style"`
	Colors  []int  `json:"colors"`
}

type storedLayout struct {
	Version    int               `json:"version"`
	Indicators []layoutEntryBody `json:"indicators"`
}

type layoutBody struct {
	ID         uuid.UUID         `json:"id"`
	Name       string            `json:"name"`
	Version    int               `json:"version"`
	Indicators []layoutEntryBody `json:"indicators"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

type layoutsBody struct {
	Count   int          `json:"count"`
	Limit   int          `json:"limit"`
	Layouts []layoutBody `json:"layouts"`
}

func (s *Server) handleListChartLayouts(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	rows, err := s.queries.ListChartLayouts(r.Context(), userID)
	if err != nil {
		s.logError(r, "listing chart layouts", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	body := layoutsBody{Count: len(rows), Limit: maxUserLayouts, Layouts: make([]layoutBody, 0, len(rows))}
	for _, row := range rows {
		layout, err := decodeLayout(row)
		if err != nil {
			s.logError(r, "decoding stored chart layout", err)
			respondError(w, r, http.StatusInternalServerError, "internal server error")
			return
		}
		body.Layouts = append(body.Layouts, layout)
	}

	respondJSON(w, r, http.StatusOK, body)
}

func (s *Server) handleCreateChartLayout(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	name, entries, ok := s.readLayout(w, r)
	if !ok {
		return
	}

	held, err := s.queries.CountChartLayouts(r.Context(), userID)
	if err != nil {
		s.logError(r, "counting chart layouts", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if held >= maxUserLayouts {
		respondError(w, r, http.StatusConflict,
			fmt.Sprintf("at most %d saved layouts — delete one first", maxUserLayouts))
		return
	}

	encoded, err := json.Marshal(storedLayout{Version: layoutVersion, Indicators: entries})
	if err != nil {
		s.logError(r, "encoding chart layout", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	row, err := s.queries.CreateChartLayout(r.Context(), database.CreateChartLayoutParams{
		UserID: userID,
		Name:   name,
		Layout: encoded,
	})
	if err != nil {
		if takenName(err) {
			respondError(w, r, http.StatusConflict, "a layout named "+name+" already exists")
			return
		}
		s.logError(r, "creating chart layout", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusCreated, layoutBody{
		ID:         row.ID,
		Name:       row.Name,
		Version:    layoutVersion,
		Indicators: entries,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	})
}

func (s *Server) handleUpdateChartLayout(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "layout id must be a UUID")
		return
	}

	name, entries, ok := s.readLayout(w, r)
	if !ok {
		return
	}

	encoded, err := json.Marshal(storedLayout{Version: layoutVersion, Indicators: entries})
	if err != nil {
		s.logError(r, "encoding chart layout", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	row, err := s.queries.UpdateChartLayout(r.Context(), database.UpdateChartLayoutParams{
		ID:     id,
		UserID: userID,
		Name:   name,
		Layout: encoded,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such layout")
			return
		}
		if takenName(err) {
			respondError(w, r, http.StatusConflict, "a layout named "+name+" already exists")
			return
		}
		s.logError(r, "saving chart layout", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusOK, layoutBody{
		ID:         row.ID,
		Name:       row.Name,
		Version:    layoutVersion,
		Indicators: entries,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	})
}

func (s *Server) handleDeleteChartLayout(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "layout id must be a UUID")
		return
	}

	deleted, err := s.queries.DeleteChartLayout(r.Context(), database.DeleteChartLayoutParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		s.logError(r, "deleting chart layout", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if deleted == 0 {
		respondError(w, r, http.StatusNotFound, "no such layout")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) readLayout(w http.ResponseWriter, r *http.Request) (string, []layoutEntryBody, bool) {
	var body layoutRequest
	if err := decodeJSON(w, r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return "", nil, false
	}

	name, err := normalizeLayoutName(body.Name)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return "", nil, false
	}

	entries, err := normalizeLayout(body)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return "", nil, false
	}

	return name, entries, true
}

func decodeLayout(row database.ChartLayout) (layoutBody, error) {
	var stored storedLayout
	if err := json.Unmarshal(row.Layout, &stored); err != nil {
		return layoutBody{}, err
	}
	if stored.Indicators == nil {
		stored.Indicators = []layoutEntryBody{}
	}

	return layoutBody{
		ID:         row.ID,
		Name:       row.Name,
		Version:    stored.Version,
		Indicators: stored.Indicators,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}, nil
}

func takenName(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

func normalizeLayoutName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return "", errors.New("a layout needs a name")
	case len(trimmed) > maxLayoutName:
		return "", fmt.Errorf("a layout name must be at most %d characters", maxLayoutName)
	default:
		return trimmed, nil
	}
}

func normalizeLayout(req layoutRequest) ([]layoutEntryBody, error) {
	if req.Version != 0 && req.Version != layoutVersion {
		return nil, fmt.Errorf("layout version must be %d, got %d", layoutVersion, req.Version)
	}
	if len(req.Indicators) > indicator.MaxInstances {
		return nil, fmt.Errorf("at most %d indicators per layout", indicator.MaxInstances)
	}

	entries := make([]layoutEntryBody, 0, len(req.Indicators))
	seen := make(map[string]bool, len(req.Indicators))

	for _, entry := range req.Indicators {
		instance, err := indicator.Parse(entry.Key)
		if err != nil {
			return nil, err
		}
		if seen[instance.Key] {
			return nil, fmt.Errorf("%s appears twice in the layout", instance.Key)
		}
		seen[instance.Key] = true

		colors, err := normalizeColors(instance, entry.Colors)
		if err != nil {
			return nil, err
		}

		style, err := normalizeStyle(instance, entry.Style)
		if err != nil {
			return nil, err
		}

		entries = append(entries, layoutEntryBody{
			Key:     instance.Key,
			Visible: entry.Visible == nil || *entry.Visible,
			Style:   style,
			Colors:  colors,
		})
	}

	return entries, nil
}

func normalizeStyle(instance indicator.Instance, style string) (string, error) {
	if style == "" {
		return styleSolid, nil
	}
	if slices.Contains(lineStyles, style) {
		return style, nil
	}
	return "", fmt.Errorf("%s: line style must be one of: %s", instance.Key, strings.Join(lineStyles, ", "))
}

func normalizeColors(instance indicator.Instance, colors []int) ([]int, error) {
	outputs := len(instance.Spec.Outputs)
	if len(colors) > outputs {
		return nil, fmt.Errorf("%s has %d outputs (%s), so %d colours is too many",
			instance.Key, outputs, strings.Join(instance.Spec.Outputs, ", "), len(colors))
	}

	out := make([]int, 0, outputs)
	for i := range outputs {
		slot := i % PaletteSlots
		if i < len(colors) {
			slot = colors[i]
		}
		if slot < 0 || slot >= PaletteSlots {
			return nil, fmt.Errorf("%s: colour for %s must be a palette slot between 0 and %d, got %d",
				instance.Key, instance.Spec.Outputs[i], PaletteSlots-1, slot)
		}
		out = append(out, slot)
	}

	return out, nil
}
