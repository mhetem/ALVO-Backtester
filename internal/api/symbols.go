package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	defaultSymbolLimit = 20
	maxSymbolLimit     = 100
)

type symbolBody struct {
	Ticker   string  `json:"ticker"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Currency string  `json:"currency"`
	LotSize  int32   `json:"lot_size"`
	TickSize float64 `json:"tick_size"`
	Active   bool    `json:"active"`
	Tracked  bool    `json:"tracked"`
}

type symbolsBody struct {
	Query   string       `json:"query"`
	Kind    string       `json:"kind,omitempty"`
	Count   int          `json:"count"`
	Symbols []symbolBody `json:"symbols"`
}

func (s *Server) handleSymbols(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	kind := ""
	if value := strings.TrimSpace(r.URL.Query().Get("kind")); value != "" {
		parsed, err := market.ParseKind(value)
		if err != nil {
			respondError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		kind = parsed.String()
	}

	limit, err := intParam(r, "limit", defaultSymbolLimit)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	limit = clamp(limit, 1, maxSymbolLimit)

	rows, err := s.queries.SearchSymbols(r.Context(), database.SearchSymbolsParams{
		Column1: query,
		Column2: kind,
		Limit:   int32Of(limit),
	})
	if err != nil {
		s.log.ErrorContext(r.Context(), "searching symbols",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.Any("err", err),
		)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	body := symbolsBody{
		Query:   query,
		Kind:    kind,
		Count:   len(rows),
		Symbols: make([]symbolBody, 0, len(rows)),
	}
	for _, row := range rows {
		body.Symbols = append(body.Symbols, symbolBody{
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

	respondCached(w, r, body, cacheSymbols)
}

func (s *Server) findSymbol(w http.ResponseWriter, r *http.Request, ticker string) (database.Symbol, bool) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))

	row, err := s.queries.GetSymbolByTicker(r.Context(), ticker)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, r, http.StatusNotFound, "no such symbol: "+ticker)
			return database.Symbol{}, false
		}
		s.log.ErrorContext(r.Context(), "looking up symbol",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.String("ticker", ticker),
			slog.Any("err", err),
		)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return database.Symbol{}, false
	}

	return row, true
}

func displayName(row database.Symbol) string {
	if row.LongName != nil {
		if name := strings.TrimSpace(*row.LongName); name != "" {
			return name
		}
	}
	if row.ShortName != nil {
		if name := strings.TrimSpace(*row.ShortName); name != "" && !strings.EqualFold(name, row.Ticker) {
			return name
		}
	}
	return row.Ticker
}
