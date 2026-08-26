package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/indicator"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const firstHistoryYear = 1990

type candlesBody struct {
	Symbol     string          `json:"symbol"`
	Timeframe  string          `json:"timeframe"`
	Base       string          `json:"base"`
	Count      int             `json:"count"`
	TS         []int64         `json:"ts"`
	Open       []float64       `json:"o"`
	High       []float64       `json:"h"`
	Low        []float64       `json:"l"`
	Close      []float64       `json:"c"`
	Volume     []int64         `json:"v"`
	NextCursor string          `json:"next_cursor,omitempty"`
	Future     []int64         `json:"future,omitempty"`
	Indicators []indicatorBody `json:"indicators,omitempty"`
}

func (s *Server) handleCandles(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	if ticker == "" {
		respondError(w, r, http.StatusBadRequest, "symbol is required, as in ?symbol=PETR4")
		return
	}

	timeframe := market.TF1d
	if value := strings.TrimSpace(r.URL.Query().Get("timeframe")); value != "" {
		parsed, err := market.ParseTimeframe(value)
		if err != nil {
			respondError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		timeframe = parsed
	}

	loc := s.cal.Location()
	to, err := dayParam(r, "to", loc, s.cal.Date(time.Now().In(loc).Date()))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	from, err := dayParam(r, "from", loc, s.cal.Date(firstHistoryYear, time.January, 1))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if from.After(to) {
		respondError(w, r, http.StatusBadRequest, "from must not be after to")
		return
	}

	limit, err := intParam(r, "limit", market.DefaultPageLimit)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	cursor, err := timestampParam(r, "cursor")
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	instances, err := indicator.ParseList(r.URL.Query().Get("indicators"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	symbol, ok := s.findSymbol(w, r, ticker)
	if !ok {
		return
	}

	request := market.PageRequest{
		SymbolID:  symbol.ID,
		Timeframe: timeframe,
		From:      from,
		To:        to,
		Before:    cursor,
		Limit:     limit,
	}

	page, err := s.candlePage(r.Context(), symbol, request)
	if err != nil {
		s.log.ErrorContext(r.Context(), "reading candles",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.String("ticker", ticker),
			slog.String("timeframe", timeframe.String()),
			slog.Any("err", err),
		)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	body := newCandlesBody(symbol.Ticker, page)

	if len(instances) > 0 {
		prime, err := s.primeFor(r.Context(), symbol, market.PrimeRequest{
			SymbolID:  symbol.ID,
			Timeframe: timeframe,
			Before:    page.Oldest(),
			Bars:      indicator.PrimeBars(instances),
		})
		if err != nil {
			s.log.ErrorContext(r.Context(), "priming indicators",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("ticker", ticker),
				slog.String("timeframe", timeframe.String()),
				slog.Any("err", err),
			)
			respondError(w, r, http.StatusInternalServerError, "internal server error")
			return
		}
		body.Indicators = computeIndicators(instances, prime, page.Candles)
		body.Future = s.futureBuckets(timeframe, page, cursor, indicator.MaxOffset(instances))
	}

	cacheControl := cacheOpenRange
	if !page.End.After(time.Now()) {
		cacheControl = cacheClosedRange
	}

	respondCached(w, r, body, cacheControl)
}

func (s *Server) futureBuckets(tf market.Timeframe, page market.Page, cursor time.Time, ahead int) []int64 {
	if ahead < 1 || len(page.Candles) == 0 || !cursor.IsZero() {
		return nil
	}

	newest := page.Candles[len(page.Candles)-1].TS
	stamps := make([]int64, 0, ahead)
	for _, bucket := range s.cal.FutureBuckets(tf, newest, ahead) {
		stamps = append(stamps, bucket.Unix())
	}

	return stamps
}

func newCandlesBody(ticker string, page market.Page) candlesBody {
	count := len(page.Candles)
	body := candlesBody{
		Symbol:    ticker,
		Timeframe: page.Timeframe.String(),
		Base:      page.Base.String(),
		Count:     count,
		TS:        make([]int64, 0, count),
		Open:      make([]float64, 0, count),
		High:      make([]float64, 0, count),
		Low:       make([]float64, 0, count),
		Close:     make([]float64, 0, count),
		Volume:    make([]int64, 0, count),
	}

	for _, candle := range page.Candles {
		body.TS = append(body.TS, candle.TS.Unix())
		body.Open = append(body.Open, candle.Open)
		body.High = append(body.High, candle.High)
		body.Low = append(body.Low, candle.Low)
		body.Close = append(body.Close, candle.Close)
		body.Volume = append(body.Volume, candle.Volume)
	}

	if page.HasMore && !page.Cursor.IsZero() {
		body.NextCursor = page.Cursor.UTC().Format(time.RFC3339)
	}

	return body
}

func (s *Server) candlePage(ctx context.Context, symbol database.Symbol, req market.PageRequest) (market.Page, error) {
	if market.Kind(symbol.Kind) != market.KindFuture {
		return s.candles.Page(ctx, req)
	}
	return s.candles.PageContinuous(ctx, symbol.Ticker, req, market.ContinuousOptions{BackAdjust: true})
}

func (s *Server) primeFor(ctx context.Context, symbol database.Symbol, req market.PrimeRequest) ([]market.Candle, error) {
	if market.Kind(symbol.Kind) != market.KindFuture {
		return s.candles.Prime(ctx, req)
	}
	if req.Bars < 1 || req.Before.IsZero() {
		return nil, nil
	}

	series, err := s.candles.LoadContinuous(ctx, symbol.Ticker, req.Timeframe,
		req.Before.AddDate(-2, 0, 0), req.Before, market.ContinuousOptions{BackAdjust: true})
	if err != nil {
		return nil, err
	}

	candles := series.Candles
	for len(candles) > 0 && !candles[len(candles)-1].TS.Before(req.Before) {
		candles = candles[:len(candles)-1]
	}
	if len(candles) > req.Bars {
		candles = candles[len(candles)-req.Bars:]
	}

	return candles, nil
}
