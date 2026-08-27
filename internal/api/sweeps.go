package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mhetem/ALVO-Backtester/internal/backtest"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
	"github.com/mhetem/ALVO-Backtester/internal/sweep"
)

const (
	maxActiveSweeps  = 1
	defaultSweepPage = 20
	maxSweepPage     = 50
)

type sweepRequest struct {
	StrategyID      string              `json:"strategy_id"`
	Kind            string              `json:"kind"`
	Objective       string              `json:"objective"`
	Symbol          string              `json:"symbol"`
	Symbols         []string            `json:"symbols"`
	Timeframe       string              `json:"timeframe"`
	Start           string              `json:"start"`
	End             string              `json:"end"`
	CapitalCents    int64               `json:"capital_cents"`
	Axes            []sweep.AxisRequest `json:"axes"`
	InSampleDays    int                 `json:"in_sample_days"`
	OutOfSampleDays int                 `json:"out_of_sample_days"`
}

type sweepProgress struct {
	Total   int64 `json:"total"`
	Queued  int64 `json:"queued"`
	Running int64 `json:"running"`
	Done    int64 `json:"done"`
	Failed  int64 `json:"failed"`
}

type sweepRunBody struct {
	ID        uuid.UUID          `json:"id"`
	Point     int32              `json:"point"`
	Fold      *int32             `json:"fold,omitempty"`
	Phase     string             `json:"phase,omitempty"`
	Status    string             `json:"status"`
	Params    map[string]float64 `json:"params"`
	Score     *float64           `json:"score,omitempty"`
	ReturnPct *float64           `json:"return_pct,omitempty"`
	Trades    int                `json:"trades"`
	Start     string             `json:"start"`
	End       string             `json:"end"`
	Error     string             `json:"error,omitempty"`
}

type sweepBody struct {
	ID           uuid.UUID      `json:"id"`
	StrategyID   uuid.UUID      `json:"strategy_id"`
	Kind         string         `json:"kind"`
	Objective    string         `json:"objective"`
	Symbol       string         `json:"symbol"`
	Symbols      []string       `json:"symbols"`
	Timeframe    string         `json:"timeframe"`
	Start        string         `json:"start"`
	End          string         `json:"end"`
	CapitalCents int64          `json:"capital_cents"`
	Points       int32          `json:"points"`
	Axes         []sweep.Axis   `json:"axes"`
	Folds        []sweep.Fold   `json:"folds,omitempty"`
	Progress     sweepProgress  `json:"progress"`
	Runs         []sweepRunBody `json:"runs,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type sweepsBody struct {
	Count  int         `json:"count"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
	Sweeps []sweepBody `json:"sweeps"`
}

func (s *Server) handleCreateSweep(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	var body sweepRequest
	if err := decodeWithin(w, r, maxSpecBytes, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	strategyID, err := uuid.Parse(strings.TrimSpace(body.StrategyID))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "strategy_id must be a UUID")
		return
	}

	kind, err := sweep.ParseKind(body.Kind)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	objective, err := sweep.ParseObjective(body.Objective)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	axes, err := sweep.ReadAxes(body.Axes)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
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
		s.logError(r, "reading the strategy to sweep", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	canonical, _, ok := s.compileSpec(w, r, stored.Spec)
	if !ok {
		return
	}

	// Every point is built and re-parsed here, so a range that would drive a period out of
	// bounds is a 400 on the sweep rather than a hundred runs that each fail alone.
	points, err := sweep.Grid(canonical, axes)
	if err != nil {
		var fault *strategy.Fault
		if errors.As(err, &fault) {
			respondFault(w, r, fault)
			return
		}
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	folds, err := s.readFolds(kind, window, body, len(points))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	basket, ok := s.findBasket(w, r, tickers)
	if !ok {
		return
	}

	active, err := s.queries.CountActiveSweeps(r.Context(), userID)
	if err != nil {
		s.logError(r, "counting active sweeps", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if active >= maxActiveSweeps {
		respondError(w, r, http.StatusConflict,
			fmt.Sprintf("at most %d sweep running at a time — wait for it to finish, or delete it", maxActiveSweeps))
		return
	}

	row, err := s.createSweep(r.Context(), sweepPlan{
		userID:     userID,
		strategyID: strategyID,
		kind:       kind,
		objective:  objective,
		spec:       canonical,
		axes:       axes,
		folds:      folds,
		points:     points,
		window:     window,
		positions:  len(tickers),
		basket:     basket,
	})
	if err != nil {
		s.logError(r, "queueing a sweep", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	if s.queue != nil {
		s.queue.Nudge()
	}

	w.Header().Set("Location", "/api/v1/sweeps/"+row.ID.String())
	respondJSON(w, r, http.StatusAccepted, sweepFrom(createdSweep(row), tickersOf(basket), axes, folds, sweepProgress{}, nil))
}

func (s *Server) readFolds(kind string, window runWindow, body sweepRequest, points int) ([]sweep.Fold, error) {
	if kind != sweep.KindWalkForward {
		return nil, nil
	}

	folds, err := sweep.Folds(window.start, window.end, body.InSampleDays, body.OutOfSampleDays)
	if err != nil {
		return nil, err
	}

	// Every fold runs the whole grid in sample, so the run count is the product. Saying so
	// before anything is queued beats discovering it as a queue nobody can drain.
	if total := len(folds) * points; total > sweep.MaxRuns {
		return nil, fmt.Errorf("%d folds over %d points is %d runs, and at most %d are queued at once: shorten the grid or widen the windows",
			len(folds), points, total, sweep.MaxRuns)
	}

	return folds, nil
}

type childWindow struct {
	fold  *int32
	phase *string
	start time.Time
	end   time.Time
}

type sweepPlan struct {
	userID     uuid.UUID
	strategyID uuid.UUID
	kind       string
	objective  string
	spec       []byte
	axes       []sweep.Axis
	folds      []sweep.Fold
	points     []sweep.Point
	window     runWindow
	positions  int
	basket     []database.Symbol
}

func (s *Server) createSweep(ctx context.Context, plan sweepPlan) (database.BacktestSweep, error) {
	axes, err := json.Marshal(plan.axes)
	if err != nil {
		return database.BacktestSweep{}, err
	}

	var folds []byte
	if len(plan.folds) > 0 {
		if folds, err = json.Marshal(plan.folds); err != nil {
			return database.BacktestSweep{}, err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return database.BacktestSweep{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inTx := s.queries.WithTx(tx)

	row, err := inTx.CreateSweep(ctx, database.CreateSweepParams{
		UserID:       plan.userID,
		StrategyID:   plan.strategyID,
		Kind:         plan.kind,
		Spec:         plan.spec,
		Axes:         axes,
		Folds:        folds,
		Objective:    plan.objective,
		SymbolID:     plan.basket[0].ID,
		Timeframe:    plan.window.timeframe.String(),
		StartDate:    plan.window.start,
		EndDate:      plan.window.end,
		CapitalCents: plan.window.capital,
		MaxPositions: int32Of(plan.positions),
		Points:       int32Of(len(plan.points)),
	})
	if err != nil {
		return database.BacktestSweep{}, err
	}

	symbols := make([]database.CreateSweepSymbolsParams, 0, len(plan.basket))
	for i, symbol := range plan.basket {
		symbols = append(symbols, database.CreateSweepSymbolsParams{
			SweepID:  row.ID,
			Ord:      int32(i),
			SymbolID: symbol.ID,
		})
	}
	if _, err := inTx.CreateSweepSymbols(ctx, symbols); err != nil {
		return database.BacktestSweep{}, err
	}

	runs, baskets, err := childRuns(row.ID, plan)
	if err != nil {
		return database.BacktestSweep{}, err
	}
	if _, err := inTx.CreateSweepRuns(ctx, runs); err != nil {
		return database.BacktestSweep{}, err
	}
	if _, err := inTx.CreateBacktestRunSymbols(ctx, baskets); err != nil {
		return database.BacktestSweep{}, err
	}

	return row, tx.Commit(ctx)
}

// Ids are minted here rather than returned by the insert, which is what lets a four hundred
// run sweep and every one of its basket rows go in as two copies instead of eight hundred
// round trips.
func childRuns(sweepID uuid.UUID, plan sweepPlan) ([]database.CreateSweepRunsParams, []database.CreateBacktestRunSymbolsParams, error) {
	var windows []childWindow

	if plan.kind == sweep.KindWalkForward {
		phase := sweep.PhaseInSample
		for _, fold := range plan.folds {
			start, end, err := fold.Window(sweep.PhaseInSample, plan.window.start.Location())
			if err != nil {
				return nil, nil, err
			}
			at := int32Of(fold.Fold)
			windows = append(windows, childWindow{fold: &at, phase: &phase, start: start, end: end})
		}
	} else {
		windows = append(windows, childWindow{start: plan.window.start, end: plan.window.end})
	}

	children := len(windows) * len(plan.points)
	runs := make([]database.CreateSweepRunsParams, 0, children)
	baskets := make([]database.CreateBacktestRunSymbolsParams, 0, children*len(plan.basket))

	for _, window := range windows {
		for _, point := range plan.points {
			params, err := json.Marshal(point.Values)
			if err != nil {
				return nil, nil, err
			}

			id := uuid.New()
			at := int32Of(point.Index)

			runs = append(runs, database.CreateSweepRunsParams{
				ID:           id,
				UserID:       plan.userID,
				StrategyID:   plan.strategyID,
				Spec:         point.Spec,
				SymbolID:     plan.basket[0].ID,
				Timeframe:    plan.window.timeframe.String(),
				StartDate:    window.start,
				EndDate:      window.end,
				CapitalCents: plan.window.capital,
				MaxPositions: int32Of(plan.positions),
				Status:       "queued",
				SweepID:      &sweepID,
				Params:       params,
				Point:        &at,
				Fold:         window.fold,
				Phase:        window.phase,
			})

			for i, symbol := range plan.basket {
				baskets = append(baskets, database.CreateBacktestRunSymbolsParams{
					RunID:    id,
					Ord:      int32(i),
					SymbolID: symbol.ID,
				})
			}
		}
	}

	return runs, baskets, nil
}

func (s *Server) handleListSweeps(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	limit, err := intParam(r, "limit", defaultSweepPage)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	limit = min(limit, maxSweepPage)

	offset, err := offsetParam(r)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := s.queries.ListSweeps(r.Context(), database.ListSweepsParams{
		UserID: userID,
		Limit:  int32Of(limit),
		Offset: int32Of(offset),
	})
	if err != nil {
		s.logError(r, "listing sweeps", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	body := sweepsBody{Count: len(rows), Limit: limit, Offset: offset, Sweeps: make([]sweepBody, 0, len(rows))}
	for _, row := range rows {
		held := listedSweep(row)
		axes, folds := plansOf(held.Axes, held.Folds)
		body.Sweeps = append(body.Sweeps, sweepFrom(
			held, s.sweepTickers(r, held.ID, row.Ticker),
			axes, folds, s.progressOf(r, held.ID), nil,
		))
	}

	respondJSON(w, r, http.StatusOK, body)
}

func (s *Server) handleGetSweep(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "sweep id must be a UUID")
		return
	}

	held, err := s.queries.GetSweep(r.Context(), database.GetSweepParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such sweep")
			return
		}
		s.logError(r, "reading a sweep", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	rows, err := s.queries.ListSweepRuns(r.Context(), &id)
	if err != nil {
		s.logError(r, "reading a sweep's runs", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	fields := storedSweep(held)
	axes, folds := plansOf(fields.Axes, fields.Folds)

	runs := make([]sweepRunBody, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, sweepRunOf(row, held.Objective))
	}

	respondJSON(w, r, http.StatusOK, sweepFrom(
		fields, s.sweepTickers(r, held.ID, held.Ticker),
		axes, folds, s.progressOf(r, held.ID), runs,
	))
}

func (s *Server) handleDeleteSweep(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "sweep id must be a UUID")
		return
	}

	deleted, err := s.queries.DeleteSweep(r.Context(), database.DeleteSweepParams{ID: id, UserID: userID})
	if err != nil {
		s.logError(r, "deleting a sweep", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if deleted == 0 {
		respondError(w, r, http.StatusNotFound, "no such sweep")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) progressOf(r *http.Request, id uuid.UUID) sweepProgress {
	row, err := s.queries.SweepProgress(r.Context(), &id)
	if err != nil {
		s.logError(r, "counting a sweep's runs", err)
		return sweepProgress{}
	}

	return sweepProgress{
		Total:   row.Total,
		Queued:  row.Queued,
		Running: row.Running,
		Done:    row.Done,
		Failed:  row.Failed,
	}
}

func (s *Server) sweepTickers(r *http.Request, id uuid.UUID, fallback string) []string {
	rows, err := s.queries.ListSweepSymbols(r.Context(), id)
	if err != nil {
		s.logError(r, "reading a sweep's basket", err)
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

// The three rows a sweep arrives on — the insert's, the get's and the list's — differ only
// by a joined ticker, so the body is built from the fields they share rather than from
// whichever shape sqlc happened to name.
type sweepFields struct {
	ID           uuid.UUID
	StrategyID   uuid.UUID
	Kind         string
	Objective    string
	Timeframe    string
	StartDate    time.Time
	EndDate      time.Time
	CapitalCents int64
	MaxPositions int32
	Points       int32
	Axes         []byte
	Folds        []byte
	CreatedAt    time.Time
}

func storedSweep(row database.GetSweepRow) sweepFields {
	return sweepFields{
		ID:           row.ID,
		StrategyID:   row.StrategyID,
		Kind:         row.Kind,
		Objective:    row.Objective,
		Timeframe:    row.Timeframe,
		StartDate:    row.StartDate,
		EndDate:      row.EndDate,
		CapitalCents: row.CapitalCents,
		MaxPositions: row.MaxPositions,
		Points:       row.Points,
		Axes:         row.Axes,
		Folds:        row.Folds,
		CreatedAt:    row.CreatedAt,
	}
}

func listedSweep(row database.ListSweepsRow) sweepFields {
	return storedSweep(database.GetSweepRow(row))
}

func createdSweep(row database.BacktestSweep) sweepFields {
	return sweepFields{
		ID:           row.ID,
		StrategyID:   row.StrategyID,
		Kind:         row.Kind,
		Objective:    row.Objective,
		Timeframe:    row.Timeframe,
		StartDate:    row.StartDate,
		EndDate:      row.EndDate,
		CapitalCents: row.CapitalCents,
		MaxPositions: row.MaxPositions,
		Points:       row.Points,
		Axes:         row.Axes,
		Folds:        row.Folds,
		CreatedAt:    row.CreatedAt,
	}
}

func plansOf(rawAxes, rawFolds []byte) ([]sweep.Axis, []sweep.Fold) {
	var axes []sweep.Axis
	_ = json.Unmarshal(rawAxes, &axes)

	var folds []sweep.Fold
	if len(rawFolds) > 0 {
		_ = json.Unmarshal(rawFolds, &folds)
	}

	return axes, folds
}

func sweepFrom(row sweepFields, tickers []string, axes []sweep.Axis, folds []sweep.Fold, progress sweepProgress, runs []sweepRunBody) sweepBody {
	if len(tickers) == 0 {
		tickers = []string{""}
	}

	return sweepBody{
		ID:           row.ID,
		StrategyID:   row.StrategyID,
		Kind:         row.Kind,
		Objective:    row.Objective,
		Symbol:       tickers[0],
		Symbols:      tickers,
		Timeframe:    row.Timeframe,
		Start:        row.StartDate.Format(time.DateOnly),
		End:          row.EndDate.Format(time.DateOnly),
		CapitalCents: row.CapitalCents,
		Points:       row.Points,
		Axes:         axes,
		Folds:        folds,
		Progress:     progress,
		Runs:         runs,
		CreatedAt:    row.CreatedAt,
	}
}

func sweepRunOf(row database.ListSweepRunsRow, objective string) sweepRunBody {
	body := sweepRunBody{
		ID:     row.ID,
		Fold:   row.Fold,
		Status: row.Status,
		Params: map[string]float64{},
		Start:  row.StartDate.Format(time.DateOnly),
		End:    row.EndDate.Format(time.DateOnly),
	}

	if row.Point != nil {
		body.Point = *row.Point
	}
	if row.Phase != nil {
		body.Phase = *row.Phase
	}
	if row.Error != nil {
		body.Error = *row.Error
	}
	_ = json.Unmarshal(row.Params, &body.Params)

	if len(row.Metrics) == 0 {
		return body
	}

	var metrics backtest.Metrics
	if err := json.Unmarshal(row.Metrics, &metrics); err != nil {
		return body
	}

	body.Trades = metrics.Trades
	body.ReturnPct = &metrics.ReturnPct

	// A run that ranks at positive infinity — no losing trade under a profit-factor
	// objective — has no number JSON can carry, so it goes out as a null the way the
	// profit factor itself does rather than failing the whole response at the marshal.
	if score, ok := metrics.Score(objective); ok && !math.IsInf(score, 0) && !math.IsNaN(score) {
		body.Score = &score
	}

	return body
}
