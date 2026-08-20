package api

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhetem/ALVO-Backtester/internal/config"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

type Server struct {
	cfg     config.Config
	db      *pgxpool.Pool
	queries *database.Queries
	log     *slog.Logger
	static  fs.FS
}

func NewServer(cfg config.Config, db *pgxpool.Pool, log *slog.Logger, static fs.FS) *Server {
	return &Server{
		cfg:     cfg,
		db:      db,
		queries: database.New(db),
		log:     log,
		static:  static,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/v1/admin/brapi-usage", s.handleBrapiUsage)

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
