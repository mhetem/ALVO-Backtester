package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	maxActiveBacktests  = 5
	minCapitalCents     = 10_000
	maxCapitalCents     = 1_000_000_000_000
	defaultCapitalCents = 10_000_000
)

type Queue interface {
	Nudge()
}

type backtestRequest struct {
	StrategyID   string `json:"strategy_id"`
	Symbol       string `json:"symbol"`
	Timeframe    string `json:"timeframe"`
	Start        string `json:"start"`
	End          string `json:"end"`
	CapitalCents int64  `json:"capital_cents"`
}

type backtestBody struct {
	ID           uuid.UUID       `json:"id"`
	StrategyID   uuid.UUID       `json:"strategy_id"`
	Symbol       string          `json:"symbol"`
	Timeframe    string          `json:"timeframe"`
	Start        string          `json:"start"`
	End          string          `json:"end"`
	CapitalCents int64           `json:"capital_cents"`
	Status       string          `json:"status"`
	Spec         json.RawMessage `json:"spec,omitempty"`
	Metrics      json.RawMessage `json:"metrics,omitempty"`
	Error        string          `json:"error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
}

func (s *Server) handleCreateBacktest(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	var body backtestRequest
	if err := decodeJSON(w, r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	strategyID, err := uuid.Parse(strings.TrimSpace(body.StrategyID))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "strategy_id must be a UUID")
		return
	}

	timeframe, err := market.ParseTimeframe(body.Timeframe)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	if strings.TrimSpace(body.Symbol) == "" {
		respondError(w, r, http.StatusBadRequest, "symbol is required, as in PETR4")
		return
	}

	loc := s.cal.Location()
	start, err := parseDay(body.Start, "start", loc)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	end, err := parseDay(body.End, "end", loc)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if end.Before(start) {
		respondError(w, r, http.StatusBadRequest, "end must not be before start")
		return
	}

	capital, err := backtestCapital(body.CapitalCents)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	stored, err := s.queries.GetStrategy(r.Context(), database.GetStrategyParams{ID: strategyID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such strategy")
			return
		}
		s.logError(r, "reading the strategy to backtest", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	canonical, _, ok := s.compileSpec(w, r, stored.Spec)
	if !ok {
		return
	}

	symbol, ok := s.findSymbol(w, r, body.Symbol)
	if !ok {
		return
	}

	active, err := s.queries.CountActiveBacktestRuns(r.Context(), userID)
	if err != nil {
		s.logError(r, "counting queued backtests", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if active >= maxActiveBacktests {
		respondError(w, r, http.StatusConflict,
			fmt.Sprintf("at most %d backtests queued at once — wait for one to finish", maxActiveBacktests))
		return
	}

	row, err := s.queries.CreateBacktestRun(r.Context(), database.CreateBacktestRunParams{
		UserID:       userID,
		StrategyID:   strategyID,
		Spec:         canonical,
		SymbolID:     symbol.ID,
		Timeframe:    timeframe.String(),
		StartDate:    start,
		EndDate:      end,
		CapitalCents: capital,
	})
	if err != nil {
		s.logError(r, "queueing a backtest", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	if s.queue != nil {
		s.queue.Nudge()
	}

	w.Header().Set("Location", "/api/v1/backtests/"+row.ID.String())
	respondJSON(w, r, http.StatusAccepted, queuedBacktest(row, symbol.Ticker))
}

func (s *Server) handleGetBacktest(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "backtest id must be a UUID")
		return
	}

	row, err := s.queries.GetBacktestRun(r.Context(), database.GetBacktestRunParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such backtest")
			return
		}
		s.logError(r, "reading a backtest", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusOK, storedBacktest(row))
}

func queuedBacktest(row database.BacktestRun, ticker string) backtestBody {
	return backtestBody{
		ID:           row.ID,
		StrategyID:   row.StrategyID,
		Symbol:       ticker,
		Timeframe:    row.Timeframe,
		Start:        row.StartDate.Format(time.DateOnly),
		End:          row.EndDate.Format(time.DateOnly),
		CapitalCents: row.CapitalCents,
		Status:       row.Status,
		Spec:         json.RawMessage(row.Spec),
		CreatedAt:    row.CreatedAt,
	}
}

func storedBacktest(row database.GetBacktestRunRow) backtestBody {
	body := backtestBody{
		ID:           row.ID,
		StrategyID:   row.StrategyID,
		Symbol:       row.Ticker,
		Timeframe:    row.Timeframe,
		Start:        row.StartDate.Format(time.DateOnly),
		End:          row.EndDate.Format(time.DateOnly),
		CapitalCents: row.CapitalCents,
		Status:       row.Status,
		Spec:         json.RawMessage(row.Spec),
		Metrics:      json.RawMessage(row.Metrics),
		CreatedAt:    row.CreatedAt,
		StartedAt:    row.StartedAt,
		FinishedAt:   row.FinishedAt,
	}
	if row.Error != nil {
		body.Error = *row.Error
	}

	return body
}

func backtestCapital(cents int64) (int64, error) {
	if cents == 0 {
		return defaultCapitalCents, nil
	}
	if cents < minCapitalCents || cents > maxCapitalCents {
		return 0, fmt.Errorf("capital_cents is between %d and %d, got %d", minCapitalCents, maxCapitalCents, cents)
	}

	return cents, nil
}

func parseDay(value, name string, loc *time.Location) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("%s is required, as in %q", name, "2024-01-02")
	}

	parsed, err := time.ParseInLocation(time.DateOnly, trimmed, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be a YYYY-MM-DD date", name)
	}

	return parsed, nil
}
