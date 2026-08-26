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
	"github.com/jackc/pgx/v5/pgconn"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

const (
	maxStrategyName        = 80
	maxStrategyDescription = 500
	maxUserStrategies      = 100
)

type strategyRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Spec        json.RawMessage `json:"spec"`
}

type legBody struct {
	Trades     bool `json:"trades"`
	RuleExit   bool `json:"rule_exit"`
	StopLoss   bool `json:"stop_loss"`
	TakeProfit bool `json:"take_profit"`
}

type planBody struct {
	Inputs     int      `json:"inputs"`
	Indicators []string `json:"indicators"`
	Slots      int      `json:"slots"`
	Warmup     int      `json:"warmup"`
	PrimeBars  int      `json:"prime_bars"`
	Depth      int      `json:"depth"`
	Long       legBody  `json:"long"`
	Short      legBody  `json:"short"`
}

type strategyBody struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Version     int32           `json:"version"`
	Spec        json.RawMessage `json:"spec"`
	Plan        *planBody       `json:"plan,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type strategiesBody struct {
	Count      int            `json:"count"`
	Limit      int            `json:"limit"`
	Strategies []strategyBody `json:"strategies"`
}

type validationBody struct {
	Valid bool            `json:"valid"`
	Spec  json.RawMessage `json:"spec"`
	Plan  *planBody       `json:"plan"`
}

type faultBody struct {
	Error     string `json:"error"`
	Pointer   string `json:"pointer,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func (s *Server) handleValidateStrategy(w http.ResponseWriter, r *http.Request) {
	var body strategyRequest
	if err := decodeWithin(w, r, maxSpecBytes, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	canonical, plan, ok := s.compileSpec(w, r, body.Spec)
	if !ok {
		return
	}

	respondJSON(w, r, http.StatusOK, validationBody{Valid: true, Spec: canonical, Plan: describePlan(plan)})
}

func (s *Server) handleListStrategies(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	rows, err := s.queries.ListStrategies(r.Context(), userID)
	if err != nil {
		s.logError(r, "listing strategies", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	body := strategiesBody{
		Count:      len(rows),
		Limit:      maxUserStrategies,
		Strategies: make([]strategyBody, 0, len(rows)),
	}
	for _, row := range rows {
		body.Strategies = append(body.Strategies, storedStrategy(row, nil))
	}

	respondJSON(w, r, http.StatusOK, body)
}

func (s *Server) handleGetStrategy(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, ok := strategyID(w, r)
	if !ok {
		return
	}

	row, err := s.queries.GetStrategy(r.Context(), database.GetStrategyParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such strategy")
			return
		}
		s.logError(r, "reading strategy", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusOK, storedStrategy(row, s.replan(r, row)))
}

func (s *Server) handleCreateStrategy(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	name, description, canonical, plan, ok := s.readStrategy(w, r)
	if !ok {
		return
	}

	held, err := s.queries.CountStrategies(r.Context(), userID)
	if err != nil {
		s.logError(r, "counting strategies", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if held >= maxUserStrategies {
		respondError(w, r, http.StatusConflict,
			fmt.Sprintf("at most %d saved strategies — delete one first", maxUserStrategies))
		return
	}

	row, err := s.queries.CreateStrategy(r.Context(), database.CreateStrategyParams{
		UserID:      userID,
		Name:        name,
		Description: description,
		Spec:        canonical,
	})
	if err != nil {
		if takenName(err) {
			respondError(w, r, http.StatusConflict, "a strategy named "+name+" already exists")
			return
		}
		s.logError(r, "creating strategy", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusCreated, storedStrategy(row, describePlan(plan)))
}

func (s *Server) handleUpdateStrategy(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, ok := strategyID(w, r)
	if !ok {
		return
	}

	name, description, canonical, plan, ok := s.readStrategy(w, r)
	if !ok {
		return
	}

	row, err := s.queries.UpdateStrategy(r.Context(), database.UpdateStrategyParams{
		ID:          id,
		UserID:      userID,
		Name:        name,
		Description: description,
		Spec:        canonical,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such strategy")
			return
		}
		if takenName(err) {
			respondError(w, r, http.StatusConflict, "a strategy named "+name+" already exists")
			return
		}
		s.logError(r, "saving strategy", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusOK, storedStrategy(row, describePlan(plan)))
}

func (s *Server) handleDeleteStrategy(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, ok := strategyID(w, r)
	if !ok {
		return
	}

	deleted, err := s.queries.DeleteStrategy(r.Context(), database.DeleteStrategyParams{ID: id, UserID: userID})
	if err != nil {
		if stillReferenced(err) {
			respondError(w, r, http.StatusConflict,
				"this strategy has backtest runs, which hold the spec they ran — delete the runs first")
			return
		}
		s.logError(r, "deleting strategy", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if deleted == 0 {
		respondError(w, r, http.StatusNotFound, "no such strategy")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) readStrategy(w http.ResponseWriter, r *http.Request) (string, string, []byte, *strategy.Plan, bool) {
	var body strategyRequest
	if err := decodeWithin(w, r, maxSpecBytes, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return "", "", nil, nil, false
	}

	name, err := normalizeStrategyName(body.Name)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return "", "", nil, nil, false
	}

	description := strings.TrimSpace(body.Description)
	if len(description) > maxStrategyDescription {
		respondError(w, r, http.StatusBadRequest,
			fmt.Sprintf("a strategy description is at most %d characters", maxStrategyDescription))
		return "", "", nil, nil, false
	}

	canonical, plan, ok := s.compileSpec(w, r, body.Spec)
	if !ok {
		return "", "", nil, nil, false
	}

	return name, description, canonical, plan, true
}

func (s *Server) compileSpec(w http.ResponseWriter, r *http.Request, raw json.RawMessage) ([]byte, *strategy.Plan, bool) {
	if len(raw) == 0 {
		respondFault(w, r, &strategy.Fault{Message: "a strategy needs a spec"})
		return nil, nil, false
	}

	spec, err := strategy.Parse(raw)
	if err != nil {
		var fault *strategy.Fault
		if errors.As(err, &fault) {
			respondFault(w, r, fault)
			return nil, nil, false
		}
		respondError(w, r, http.StatusBadRequest, err.Error())
		return nil, nil, false
	}

	canonical, err := json.Marshal(spec)
	if err != nil {
		s.logError(r, "encoding strategy spec", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return nil, nil, false
	}

	plan, err := strategy.Compile(spec)
	if err != nil {
		s.logError(r, "compiling strategy spec", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return nil, nil, false
	}

	return canonical, plan, true
}

func (s *Server) replan(r *http.Request, row database.Strategy) *planBody {
	spec, err := strategy.Parse(row.Spec)
	if err != nil {
		s.logError(r, "reading a stored strategy", err)
		return nil
	}

	plan, err := strategy.Compile(spec)
	if err != nil {
		s.logError(r, "compiling a stored strategy", err)
		return nil
	}

	return describePlan(plan)
}

func describePlan(plan *strategy.Plan) *planBody {
	if plan == nil {
		return nil
	}

	body := planBody{
		Inputs:     len(plan.Spec.Inputs),
		Indicators: make([]string, 0, len(plan.Units)),
		Slots:      len(plan.Slots),
		Warmup:     plan.Warmup,
		PrimeBars:  plan.PrimeBars,
		Depth:      plan.Depth,
		Long:       describeLeg(plan.Long),
		Short:      describeLeg(plan.Short),
	}
	for _, unit := range plan.Units {
		body.Indicators = append(body.Indicators, unit.Instance.Key)
	}

	return &body
}

func describeLeg(leg strategy.Leg) legBody {
	return legBody{
		Trades:     leg.Trades(),
		RuleExit:   leg.Exit != nil,
		StopLoss:   leg.Stop != nil,
		TakeProfit: leg.Target != nil,
	}
}

func storedStrategy(row database.Strategy, plan *planBody) strategyBody {
	return strategyBody{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		Version:     row.Version,
		Spec:        json.RawMessage(row.Spec),
		Plan:        plan,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func stillReferenced(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyUsed
}

func strategyID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "strategy id must be a UUID")
		return uuid.UUID{}, false
	}
	return id, true
}

func respondFault(w http.ResponseWriter, r *http.Request, fault *strategy.Fault) {
	respondJSON(w, r, http.StatusBadRequest, faultBody{
		Error:     fault.Message,
		Pointer:   fault.Pointer,
		RequestID: RequestIDFrom(r.Context()),
	})
}

func normalizeStrategyName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return "", errors.New("a strategy needs a name")
	case len(trimmed) > maxStrategyName:
		return "", fmt.Errorf("a strategy name is at most %d characters", maxStrategyName)
	default:
		return trimmed, nil
	}
}
