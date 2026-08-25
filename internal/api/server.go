package api

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhetem/ALVO-Backtester/internal/config"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

type Server struct {
	cfg     config.Config
	db      *pgxpool.Pool
	queries *database.Queries
	log     *slog.Logger
	static  fs.FS
	cal     *market.Calendar
	candles *market.CandleService
}

func NewServer(cfg config.Config, db *pgxpool.Pool, log *slog.Logger, static fs.FS, cal *market.Calendar) *Server {
	return &Server{
		cfg:     cfg,
		db:      db,
		queries: database.New(db),
		log:     log,
		static:  static,
		cal:     cal,
		candles: market.NewCandleService(db, cal),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/v1/symbols", s.handleSymbols)
	mux.HandleFunc("GET /api/v1/candles", s.handleCandles)
	mux.HandleFunc("GET /api/v1/indicators", s.handleIndicators)

	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/v1/auth/revoke", s.handleRevoke)

	mux.Handle("GET /api/v1/chart-layouts", s.requireAuth(http.HandlerFunc(s.handleListChartLayouts)))
	mux.Handle("POST /api/v1/chart-layouts", s.requireAuth(http.HandlerFunc(s.handleCreateChartLayout)))
	mux.Handle("PUT /api/v1/chart-layouts/{id}", s.requireAuth(http.HandlerFunc(s.handleUpdateChartLayout)))
	mux.Handle("DELETE /api/v1/chart-layouts/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteChartLayout)))

	mux.Handle("GET /api/v1/admin/brapi-usage", s.requireAuth(http.HandlerFunc(s.handleBrapiUsage)))

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
