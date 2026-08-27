package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

const (
	maxBasketName  = 60
	maxUserBaskets = 50
)

type basketRequest struct {
	Name    string   `json:"name"`
	Symbols []string `json:"symbols"`
}

type basketBody struct {
	ID        uuid.UUID    `json:"id"`
	Name      string       `json:"name"`
	Count     int          `json:"count"`
	Symbols   []symbolBody `json:"symbols"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type basketsBody struct {
	Count   int          `json:"count"`
	Limit   int          `json:"limit"`
	Baskets []basketBody `json:"baskets"`
}

func (s *Server) handleListBaskets(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	rows, err := s.queries.ListSymbolBaskets(r.Context(), userID)
	if err != nil {
		s.logError(r, "listing baskets", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	members, ok := s.basketMembers(w, r, rows)
	if !ok {
		return
	}

	body := basketsBody{Count: len(rows), Limit: maxUserBaskets, Baskets: make([]basketBody, 0, len(rows))}
	for _, row := range rows {
		body.Baskets = append(body.Baskets, basketFrom(row, members[row.ID]))
	}

	respondJSON(w, r, http.StatusOK, body)
}

func (s *Server) handleGetBasket(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, ok := basketID(w, r)
	if !ok {
		return
	}

	row, err := s.queries.GetSymbolBasket(r.Context(), database.GetSymbolBasketParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such basket")
			return
		}
		s.logError(r, "reading basket", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	members, ok := s.basketMembers(w, r, []database.SymbolBasket{row})
	if !ok {
		return
	}

	respondJSON(w, r, http.StatusOK, basketFrom(row, members[row.ID]))
}

func (s *Server) handleCreateBasket(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	name, basket, ok := s.readBasketRequest(w, r)
	if !ok {
		return
	}

	held, err := s.queries.CountSymbolBaskets(r.Context(), userID)
	if err != nil {
		s.logError(r, "counting baskets", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if held >= maxUserBaskets {
		respondError(w, r, http.StatusConflict,
			fmt.Sprintf("at most %d saved baskets — delete one first", maxUserBaskets))
		return
	}

	row, err := s.createBasket(r.Context(), userID, name, basket)
	if err != nil {
		if takenName(err) {
			respondError(w, r, http.StatusConflict, "a basket named "+name+" already exists")
			return
		}
		s.logError(r, "creating basket", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusCreated, basketFrom(row, symbolBodies(basket)))
}

func (s *Server) handleUpdateBasket(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, ok := basketID(w, r)
	if !ok {
		return
	}

	name, basket, ok := s.readBasketRequest(w, r)
	if !ok {
		return
	}

	row, err := s.replaceBasket(r.Context(), id, userID, name, basket)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such basket")
			return
		}
		if takenName(err) {
			respondError(w, r, http.StatusConflict, "a basket named "+name+" already exists")
			return
		}
		s.logError(r, "saving basket", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respondJSON(w, r, http.StatusOK, basketFrom(row, symbolBodies(basket)))
}

func (s *Server) handleDeleteBasket(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		respondUnauthorized(w, r, msgBadAccess)
		return
	}

	id, ok := basketID(w, r)
	if !ok {
		return
	}

	deleted, err := s.queries.DeleteSymbolBasket(r.Context(), database.DeleteSymbolBasketParams{ID: id, UserID: userID})
	if err != nil {
		s.logError(r, "deleting basket", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}
	if deleted == 0 {
		respondError(w, r, http.StatusNotFound, "no such basket")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// A basket row with no symbols is not a basket, so the name and the membership are written
// together and replaced together.
func (s *Server) createBasket(ctx context.Context, userID uuid.UUID, name string, basket []database.Symbol) (database.SymbolBasket, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return database.SymbolBasket{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inTx := s.queries.WithTx(tx)

	row, err := inTx.CreateSymbolBasket(ctx, database.CreateSymbolBasketParams{UserID: userID, Name: name})
	if err != nil {
		return database.SymbolBasket{}, err
	}
	if _, err := inTx.CreateSymbolBasketSymbols(ctx, basketMemberParams(row.ID, basket)); err != nil {
		return database.SymbolBasket{}, err
	}

	return row, tx.Commit(ctx)
}

func (s *Server) replaceBasket(ctx context.Context, id, userID uuid.UUID, name string, basket []database.Symbol) (database.SymbolBasket, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return database.SymbolBasket{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inTx := s.queries.WithTx(tx)

	row, err := inTx.UpdateSymbolBasket(ctx, database.UpdateSymbolBasketParams{ID: id, UserID: userID, Name: name})
	if err != nil {
		return database.SymbolBasket{}, err
	}
	if err := inTx.DeleteSymbolBasketSymbols(ctx, id); err != nil {
		return database.SymbolBasket{}, err
	}
	if _, err := inTx.CreateSymbolBasketSymbols(ctx, basketMemberParams(id, basket)); err != nil {
		return database.SymbolBasket{}, err
	}

	return row, tx.Commit(ctx)
}

func (s *Server) readBasketRequest(w http.ResponseWriter, r *http.Request) (string, []database.Symbol, bool) {
	var body basketRequest
	if err := decodeJSON(w, r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return "", nil, false
	}

	name, err := normalizeBasketName(body.Name)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return "", nil, false
	}

	tickers, err := readSavedBasket(body.Symbols)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return "", nil, false
	}

	basket, ok := s.findBasket(w, r, tickers)
	if !ok {
		return "", nil, false
	}

	return name, basket, true
}

// One query for every basket on the page rather than one per basket, the same shape the
// backtest list uses to fill in its runs' symbols.
func (s *Server) basketMembers(w http.ResponseWriter, r *http.Request, rows []database.SymbolBasket) (map[uuid.UUID][]symbolBody, bool) {
	members := map[uuid.UUID][]symbolBody{}
	if len(rows) == 0 {
		return members, true
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	held, err := s.queries.ListSymbolBasketMembers(r.Context(), ids)
	if err != nil {
		s.logError(r, "reading basket symbols", err)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return nil, false
	}

	for _, row := range held {
		members[row.BasketID] = append(members[row.BasketID], symbolBody{
			Ticker:   row.Ticker,
			Name:     pickName(row.Ticker, row.ShortName, row.LongName),
			Kind:     row.Kind,
			Currency: row.Currency,
			LotSize:  row.LotSize,
			TickSize: row.TickSize,
			Active:   row.Active,
			Tracked:  row.Tracked,
		})
	}

	return members, true
}

func basketMemberParams(id uuid.UUID, basket []database.Symbol) []database.CreateSymbolBasketSymbolsParams {
	members := make([]database.CreateSymbolBasketSymbolsParams, 0, len(basket))
	for i, symbol := range basket {
		members = append(members, database.CreateSymbolBasketSymbolsParams{
			BasketID: id,
			Ord:      int32(i),
			SymbolID: symbol.ID,
		})
	}
	return members
}

func basketFrom(row database.SymbolBasket, symbols []symbolBody) basketBody {
	if symbols == nil {
		symbols = []symbolBody{}
	}

	return basketBody{
		ID:        row.ID,
		Name:      row.Name,
		Count:     len(symbols),
		Symbols:   symbols,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func symbolBodies(basket []database.Symbol) []symbolBody {
	bodies := make([]symbolBody, 0, len(basket))
	for _, row := range basket {
		bodies = append(bodies, symbolBody{
			Ticker:   row.Ticker,
			Name:     displayName(row),
			Kind:     row.Kind,
			Currency: row.Currency,
			LotSize:  row.LotSize,
			TickSize: row.TickSize,
			Active:   row.Active,
			Tracked:  row.Tracked,
		})
	}
	return bodies
}

func basketID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "basket id must be a UUID")
		return uuid.UUID{}, false
	}
	return id, true
}

func normalizeBasketName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return "", errors.New("a basket needs a name")
	case len(trimmed) > maxBasketName:
		return "", fmt.Errorf("a basket name must be at most %d characters", maxBasketName)
	default:
		return trimmed, nil
	}
}

// A saved basket is capped at the same size a run is: one you cannot backtest is a list the
// rest of the API would refuse.
func readSavedBasket(many []string) ([]string, error) {
	tickers, err := normalizeTickers(many)
	if err != nil {
		return nil, err
	}
	if len(tickers) == 0 {
		return nil, errors.New("a basket needs at least one symbol, as in PETR4")
	}
	return tickers, nil
}
