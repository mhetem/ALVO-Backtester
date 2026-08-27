package api

import (
	"context"
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
	maxBasket           = 20
	minCapitalCents     = 10_000
	maxCapitalCents     = 1_000_000_000_000
	defaultCapitalCents = 10_000_000
)

type Queue interface {
	Nudge()
}

type backtestRequest struct {
	StrategyID   string   `json:"strategy_id"`
	Symbol       string   `json:"symbol"`
	Symbols      []string `json:"symbols"`
	Timeframe    string   `json:"timeframe"`
	Start        string   `json:"start"`
	End          string   `json:"end"`
	CapitalCents int64    `json:"capital_cents"`
}

type backtestBody struct {
	ID           uuid.UUID       `json:"id"`
	StrategyID   uuid.UUID       `json:"strategy_id"`
	Symbol       string          `json:"symbol"`
	Symbols      []string        `json:"symbols"`
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

type runWindow struct {
	timeframe market.Timeframe
	start     time.Time
	end       time.Time
	capital   int64
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

	tickers, err := readBasket(body.Symbol, body.Symbols)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	window, ok := s.readWindow(w, r, timeframe, body.Start, body.End, body.CapitalCents)
	if !ok {
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

	basket, ok := s.findBasket(w, r, tickers)
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

	row, err := s.createRun(r.Context(), database.CreateBacktestRunParams{
		UserID:       userID,
		StrategyID:   strategyID,
		Spec:         canonical,
		SymbolID:     basket[0].ID,
		Timeframe:    window.timeframe.String(),
		StartDate:    window.start,
		EndDate:      window.end,
		CapitalCents: window.capital,
		MaxPositions: int32Of(len(tickers)),
	}, basket)
	if err != nil {
		s.logError(r, "queueing a backtest", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	if s.queue != nil {
		s.queue.Nudge()
	}

	w.Header().Set("Location", "/api/v1/backtests/"+row.ID.String())
	respondJSON(w, r, http.StatusAccepted, queuedBacktest(row, tickersOf(basket)))
}

// A run and its basket are written together: a run row with no symbols would be claimed by
// a worker that then has nothing to load, and the fallback to the primary symbol would
// quietly turn a portfolio into a single-symbol run.
func (s *Server) createRun(ctx context.Context, params database.CreateBacktestRunParams, basket []database.Symbol) (database.BacktestRun, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return database.BacktestRun{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inTx := s.queries.WithTx(tx)

	row, err := inTx.CreateBacktestRun(ctx, params)
	if err != nil {
		return database.BacktestRun{}, err
	}

	symbols := make([]database.CreateBacktestRunSymbolsParams, 0, len(basket))
	for i, symbol := range basket {
		symbols = append(symbols, database.CreateBacktestRunSymbolsParams{
			RunID:    row.ID,
			Ord:      int32(i),
			SymbolID: symbol.ID,
		})
	}
	if _, err := inTx.CreateBacktestRunSymbols(ctx, symbols); err != nil {
		return database.BacktestRun{}, err
	}

	return row, tx.Commit(ctx)
}

func (s *Server) handleGetBacktest(w http.ResponseWriter, r *http.Request) {
	run, ok := s.ownedRun(w, r)
	if !ok {
		return
	}

	respondJSON(w, r, http.StatusOK, storedBacktest(run, s.runTickers(r, run.ID, run.Ticker)))
}

func (s *Server) readWindow(w http.ResponseWriter, r *http.Request, timeframe market.Timeframe, from, to string, cents int64) (runWindow, bool) {
	loc := s.cal.Location()
	start, err := parseDay(from, "start", loc)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return runWindow{}, false
	}

	end, err := parseDay(to, "end", loc)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return runWindow{}, false
	}
	if end.Before(start) {
		respondError(w, r, http.StatusBadRequest, "end must not be before start")
		return runWindow{}, false
	}

	capital, err := backtestCapital(cents)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return runWindow{}, false
	}

	return runWindow{timeframe: timeframe, start: start, end: end, capital: capital}, true
}

func (s *Server) findBasket(w http.ResponseWriter, r *http.Request, tickers []string) ([]database.Symbol, bool) {
	basket := make([]database.Symbol, 0, len(tickers))

	for _, ticker := range tickers {
		symbol, ok := s.findSymbol(w, r, ticker)
		if !ok {
			return nil, false
		}
		basket = append(basket, symbol)
	}

	return basket, true
}

func (s *Server) runTickers(r *http.Request, id uuid.UUID, fallback string) []string {
	rows, err := s.queries.ListBacktestRunSymbols(r.Context(), id)
	if err != nil {
		s.logError(r, "reading a run's basket", err)
		return []string{fallback}
	}
	if len(rows) == 0 {
		return []string{fallback}
	}

	tickers := make([]string, 0, len(rows))
	for _, row := range rows {
		tickers = append(tickers, row.Ticker)
	}

	return tickers
}

// One symbol or many: the request may name either, and a basket of one is not a portfolio,
// it is the run every phase before this one already produced.
func readBasket(single string, many []string) ([]string, error) {
	raw := many
	if len(raw) == 0 && strings.TrimSpace(single) != "" {
		raw = []string{single}
	}

	tickers, err := normalizeTickers(raw)
	if err != nil {
		return nil, err
	}
	if len(tickers) == 0 {
		return nil, errors.New("symbol is required, as in PETR4, or symbols for a basket")
	}

	return tickers, nil
}

func normalizeTickers(raw []string) ([]string, error) {
	tickers := make([]string, 0, len(raw))
	seen := map[string]bool{}

	for _, ticker := range raw {
		clean := strings.ToUpper(strings.TrimSpace(ticker))
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		tickers = append(tickers, clean)
	}

	if len(tickers) > maxBasket {
		return nil, fmt.Errorf("at most %d symbols in one basket, got %d", maxBasket, len(tickers))
	}

	return tickers, nil
}

func tickersOf(basket []database.Symbol) []string {
	tickers := make([]string, 0, len(basket))
	for _, symbol := range basket {
		tickers = append(tickers, symbol.Ticker)
	}
	return tickers
}

func queuedBacktest(row database.BacktestRun, tickers []string) backtestBody {
	return backtestBody{
		ID:           row.ID,
		StrategyID:   row.StrategyID,
		Symbol:       tickers[0],
		Symbols:      tickers,
		Timeframe:    row.Timeframe,
		Start:        row.StartDate.Format(time.DateOnly),
		End:          row.EndDate.Format(time.DateOnly),
		CapitalCents: row.CapitalCents,
		Status:       row.Status,
		Spec:         json.RawMessage(row.Spec),
		CreatedAt:    row.CreatedAt,
	}
}

func listedBacktest(row database.ListBacktestRunsRow, tickers []string) backtestBody {
	return storedBacktest(database.GetBacktestRunRow(row), tickers)
}

func storedBacktest(row database.GetBacktestRunRow, tickers []string) backtestBody {
	if len(tickers) == 0 {
		tickers = []string{row.Ticker}
	}

	body := backtestBody{
		ID:           row.ID,
		StrategyID:   row.StrategyID,
		Symbol:       tickers[0],
		Symbols:      tickers,
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
