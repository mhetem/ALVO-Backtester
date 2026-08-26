package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhetem/ALVO-Backtester/internal/config"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	ipRateBurst    = 20
	ipRatePeriod   = time.Minute
	userRateBurst  = 30
	userRatePeriod = time.Minute
)

type Server struct {
	cfg     config.Config
	db      *pgxpool.Pool
	queries *database.Queries
	log     *slog.Logger
	static  fs.FS
	cal     *market.Calendar
	candles *market.CandleService
	queue   Queue

	ipLimit   *limiter
	userLimit *limiter
}

func NewServer(cfg config.Config, db *pgxpool.Pool, log *slog.Logger, static fs.FS, cal *market.Calendar, queue Queue) *Server {
	return &Server{
		cfg:     cfg,
		db:      db,
		queries: database.New(db),
		log:     log,
		static:  static,
		cal:     cal,
		candles: market.NewCandleService(db, cal),
		queue:   queue,

		ipLimit:   newLimiter(ipRateBurst, ipRatePeriod),
		userLimit: newLimiter(userRateBurst, userRatePeriod),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/v1/symbols", s.handleSymbols)
	mux.HandleFunc("GET /api/v1/candles", s.handleCandles)
	mux.HandleFunc("GET /api/v1/indicators", s.handleIndicators)
	mux.HandleFunc("GET /api/v1/shared/strategies/{token}", s.handleGetSharedStrategy)

	mux.Handle("POST /api/v1/auth/register", s.limitByIP(http.HandlerFunc(s.handleRegister)))
	mux.Handle("POST /api/v1/auth/login", s.limitByIP(http.HandlerFunc(s.handleLogin)))
	mux.Handle("POST /api/v1/auth/refresh", s.limitByIP(http.HandlerFunc(s.handleRefresh)))
	mux.Handle("POST /api/v1/auth/revoke", s.limitByIP(http.HandlerFunc(s.handleRevoke)))

	mux.Handle("GET /api/v1/chart-layouts", s.requireAuth(http.HandlerFunc(s.handleListChartLayouts)))
	mux.Handle("POST /api/v1/chart-layouts", s.requireAuth(http.HandlerFunc(s.handleCreateChartLayout)))
	mux.Handle("PUT /api/v1/chart-layouts/{id}", s.requireAuth(http.HandlerFunc(s.handleUpdateChartLayout)))
	mux.Handle("DELETE /api/v1/chart-layouts/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteChartLayout)))

	mux.Handle("POST /api/v1/strategies/validate", s.requireAuth(http.HandlerFunc(s.handleValidateStrategy)))
	mux.Handle("GET /api/v1/strategies", s.requireAuth(http.HandlerFunc(s.handleListStrategies)))
	mux.Handle("POST /api/v1/strategies", s.requireAuth(http.HandlerFunc(s.handleCreateStrategy)))
	mux.Handle("GET /api/v1/strategies/{id}", s.requireAuth(http.HandlerFunc(s.handleGetStrategy)))
	mux.Handle("PUT /api/v1/strategies/{id}", s.requireAuth(http.HandlerFunc(s.handleUpdateStrategy)))
	mux.Handle("DELETE /api/v1/strategies/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteStrategy)))
	mux.Handle("POST /api/v1/strategies/{id}/share", s.requireAuth(http.HandlerFunc(s.handleShareStrategy)))
	mux.Handle("DELETE /api/v1/strategies/{id}/share", s.requireAuth(http.HandlerFunc(s.handleUnshareStrategy)))

	mux.Handle("GET /api/v1/backtests", s.requireAuth(http.HandlerFunc(s.handleListBacktests)))
	mux.Handle("POST /api/v1/backtests", s.requireAuth(s.limitByUser(http.HandlerFunc(s.handleCreateBacktest))))
	mux.Handle("GET /api/v1/backtests/{id}", s.requireAuth(http.HandlerFunc(s.handleGetBacktest)))
	mux.Handle("GET /api/v1/backtests/{id}/trades", s.requireAuth(http.HandlerFunc(s.handleBacktestTrades)))
	mux.Handle("GET /api/v1/backtests/{id}/equity", s.requireAuth(http.HandlerFunc(s.handleBacktestEquity)))

	mux.Handle("GET /api/v1/sweeps", s.requireAuth(http.HandlerFunc(s.handleListSweeps)))
	mux.Handle("POST /api/v1/sweeps", s.requireAuth(s.limitByUser(http.HandlerFunc(s.handleCreateSweep))))
	mux.Handle("GET /api/v1/sweeps/{id}", s.requireAuth(http.HandlerFunc(s.handleGetSweep)))
	mux.Handle("DELETE /api/v1/sweeps/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteSweep)))

	mux.Handle("GET /api/v1/admin/brapi-usage", s.requireAuth(http.HandlerFunc(s.handleBrapiUsage)))
	mux.Handle("GET /api/v1/admin/stats", s.requireAuth(http.HandlerFunc(s.handleStats)))

	s.registerDebug(mux)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, http.StatusNotFound, "no such endpoint")
	})
	mux.Handle("/", s.spaHandler())

	return chain(mux,
		requestID,
		logRequests(s.log),
		recoverPanics(s.log),
	)
}
