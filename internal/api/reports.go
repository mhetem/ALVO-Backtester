package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

const (
	defaultRunPage   = 20
	maxRunPage       = 100
	defaultCurvePage = 2000
	maxCurvePage     = 20000
)

type tradeBody struct {
	Seq            int32      `json:"seq"`
	Symbol         string     `json:"symbol"`
	Side           string     `json:"side"`
	Qty            int64      `json:"qty"`
	EntryTS        time.Time  `json:"entry_ts"`
	EntryPrice     float64    `json:"entry_price"`
	ExitTS         *time.Time `json:"exit_ts,omitempty"`
	ExitPrice      *float64   `json:"exit_price,omitempty"`
	PnLCents       int64      `json:"pnl_cents"`
	FeesCents      int64      `json:"fees_cents"`
	DividendsCents int64      `json:"dividends_cents"`
	BorrowCents    int64      `json:"borrow_cents"`
	SplitCashCents int64      `json:"split_cash_cents"`
	ExitReason     string     `json:"exit_reason,omitempty"`
}

type tradesBody struct {
	RunID   uuid.UUID   `json:"run_id"`
	Symbol  string      `json:"symbol"`
	Symbols []string    `json:"symbols"`
	Count   int         `json:"count"`
	Trades  []tradeBody `json:"trades"`
}

type equityBody struct {
	RunID    uuid.UUID `json:"run_id"`
	Symbol   string    `json:"symbol"`
	Count    int       `json:"count"`
	Total    int64     `json:"total"`
	Sampled  bool      `json:"sampled"`
	TS       []int64   `json:"ts"`
	Equity   []int64   `json:"equity"`
	Hold     []int64   `json:"hold,omitempty"`
	Index    []int64   `json:"index,omitempty"`
	Drawdown []float64 `json:"drawdown"`
}

type runsBody struct {
	Count  int            `json:"count"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
	Runs   []backtestBody `json:"runs"`
}

func (s *Server) handleListBacktests(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	limit, err := intParam(r, "limit", defaultRunPage)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	limit = min(limit, maxRunPage)

	offset, err := offsetParam(r)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := s.queries.ListBacktestRuns(r.Context(), database.ListBacktestRunsParams{
		UserID: userID,
		Limit:  int32Of(limit),
		Offset: int32Of(offset),
	})
	if err != nil {
		s.logError(r, "listing backtests", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	baskets := s.basketsOf(r, rows)

	body := runsBody{Count: len(rows), Limit: limit, Offset: offset, Runs: make([]backtestBody, 0, len(rows))}
	for _, row := range rows {
		body.Runs = append(body.Runs, listedBacktest(row, baskets[row.ID]))
	}

	respondJSON(w, r, http.StatusOK, body)
}

func (s *Server) handleBacktestTrades(w http.ResponseWriter, r *http.Request) {
	run, ok := s.ownedRun(w, r)
	if !ok {
		return
	}

	rows, err := s.queries.ListBacktestTrades(r.Context(), run.ID)
	if err != nil {
		s.logError(r, "reading run trades", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	body := tradesBody{
		RunID:   run.ID,
		Symbol:  run.Ticker,
		Symbols: s.runTickers(r, run.ID, run.Ticker),
		Count:   len(rows),
		Trades:  make([]tradeBody, 0, len(rows)),
	}
	for _, row := range rows {
		trade := tradeBody{
			Seq:            row.Seq,
			Symbol:         row.Ticker,
			Side:           row.Side,
			Qty:            row.Qty,
			EntryTS:        row.EntryTs,
			EntryPrice:     row.EntryPrice,
			ExitTS:         row.ExitTs,
			ExitPrice:      row.ExitPrice,
			FeesCents:      row.FeesCents,
			DividendsCents: row.DividendsCents,
			BorrowCents:    row.BorrowCents,
			SplitCashCents: row.SplitCashCents,
		}
		if row.PnlCents != nil {
			trade.PnLCents = *row.PnlCents
		}
		if row.ExitReason != nil {
			trade.ExitReason = *row.ExitReason
		}
		body.Trades = append(body.Trades, trade)
	}

	respondJSON(w, r, http.StatusOK, body)
}

func (s *Server) handleBacktestEquity(w http.ResponseWriter, r *http.Request) {
	run, ok := s.ownedRun(w, r)
	if !ok {
		return
	}

	points, err := intParam(r, "points", defaultCurvePage)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	points = min(points, maxCurvePage)

	total, err := s.queries.CountBacktestEquity(r.Context(), run.ID)
	if err != nil {
		s.logError(r, "counting equity points", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	rows, err := s.queries.ListBacktestEquity(r.Context(), database.ListBacktestEquityParams{
		RunID:   run.ID,
		Column2: int64(points),
	})
	if err != nil {
		s.logError(r, "reading the equity curve", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	body := equityBody{
		RunID:    run.ID,
		Symbol:   run.Ticker,
		Count:    len(rows),
		Total:    total,
		Sampled:  int64(len(rows)) < total,
		TS:       make([]int64, 0, len(rows)),
		Equity:   make([]int64, 0, len(rows)),
		Drawdown: make([]float64, 0, len(rows)),
	}

	hold := make([]int64, 0, len(rows))
	index := make([]int64, 0, len(rows))
	peak := int64(0)

	for i, row := range rows {
		body.TS = append(body.TS, row.Ts.Unix())
		body.Equity = append(body.Equity, row.EquityCents)

		if i == 0 || row.EquityCents > peak {
			peak = row.EquityCents
		}
		fall := 0.0
		if peak > 0 {
			fall = float64(row.EquityCents-peak) / float64(peak) * 100
		}
		body.Drawdown = append(body.Drawdown, fall)

		if row.HoldCents != nil {
			hold = append(hold, *row.HoldCents)
		}
		if row.IndexCents != nil {
			index = append(index, *row.IndexCents)
		}
	}

	if len(hold) == len(rows) {
		body.Hold = hold
	}
	if len(index) == len(rows) {
		body.Index = index
	}

	respondJSON(w, r, http.StatusOK, body)
}

// One query for every run on the page, rather than one per run: a list of twenty portfolio
// runs is otherwise twenty round trips for what is a single join.
func (s *Server) basketsOf(r *http.Request, rows []database.ListBacktestRunsRow) map[uuid.UUID][]string {
	baskets := map[uuid.UUID][]string{}
	if len(rows) == 0 {
		return baskets
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	held, err := s.queries.ListBacktestRunTickers(r.Context(), ids)
	if err != nil {
		s.logError(r, "reading run baskets", err)
		return baskets
	}

	for _, row := range held {
		baskets[row.RunID] = append(baskets[row.RunID], row.Ticker)
	}

	return baskets
}

func (s *Server) ownedRun(w http.ResponseWriter, r *http.Request) (database.GetBacktestRunRow, bool) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return database.GetBacktestRunRow{}, false
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "backtest id must be a UUID")
		return database.GetBacktestRunRow{}, false
	}

	run, err := s.queries.GetBacktestRun(r.Context(), database.GetBacktestRunParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such backtest")
			return database.GetBacktestRunRow{}, false
		}
		s.logError(r, "reading a backtest", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return database.GetBacktestRunRow{}, false
	}

	return run, true
}

func offsetParam(r *http.Request) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get("offset"))
	if value == "" {
		return 0, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New("offset must be a whole number")
	}
	if parsed < 0 {
		return 0, errors.New("offset must not be negative")
	}

	return parsed, nil
}
