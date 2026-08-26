package api

import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/debug"
	"time"
)

var startedAt = time.Now()

type statsRuntime struct {
	Goroutines int    `json:"goroutines"`
	MaxProcs   int    `json:"max_procs"`
	MemLimit   int64  `json:"mem_limit_bytes"`
	HeapInUse  uint64 `json:"heap_in_use_bytes"`
	HeapAlloc  uint64 `json:"heap_alloc_bytes"`
	Sys        uint64 `json:"sys_bytes"`
	GCCycles   uint32 `json:"gc_cycles"`
}

type statsPool struct {
	Max      int32 `json:"max_conns"`
	Total    int32 `json:"total_conns"`
	Idle     int32 `json:"idle_conns"`
	Acquired int32 `json:"acquired_conns"`
	Waits    int64 `json:"empty_acquires"`
}

type statsQueue struct {
	Queued  int64 `json:"queued"`
	Running int64 `json:"running"`
}

type statsBody struct {
	UptimeSeconds int64        `json:"uptime_seconds"`
	Runtime       statsRuntime `json:"runtime"`
	Database      statsPool    `json:"database"`
	Backtests     statsQueue   `json:"backtests"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	stat := s.db.Stat()

	body := statsBody{
		UptimeSeconds: int64(time.Since(startedAt).Seconds()),
		Runtime: statsRuntime{
			Goroutines: runtime.NumGoroutine(),
			MaxProcs:   runtime.GOMAXPROCS(0),
			MemLimit:   debug.SetMemoryLimit(-1),
			HeapInUse:  mem.HeapInuse,
			HeapAlloc:  mem.HeapAlloc,
			Sys:        mem.Sys,
			GCCycles:   mem.NumGC,
		},
		Database: statsPool{
			Max:      stat.MaxConns(),
			Total:    stat.TotalConns(),
			Idle:     stat.IdleConns(),
			Acquired: stat.AcquiredConns(),
			Waits:    stat.EmptyAcquireCount(),
		},
	}

	rows, err := s.queries.CountBacktestRunsByStatus(r.Context())
	if err != nil {
		s.log.ErrorContext(r.Context(), "counting backtest runs by status",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.Any("err", err),
		)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	for _, row := range rows {
		switch row.Status {
		case "queued":
			body.Backtests.Queued = row.Runs
		case "running":
			body.Backtests.Running = row.Runs
		}
	}

	respondJSON(w, r, http.StatusOK, body)
}

func (s *Server) registerDebug(mux *http.ServeMux) {
	mux.Handle("GET /debug/pprof/", s.requireAuth(http.HandlerFunc(pprof.Index)))
	mux.Handle("GET /debug/pprof/cmdline", s.requireAuth(http.HandlerFunc(pprof.Cmdline)))
	mux.Handle("GET /debug/pprof/profile", s.requireAuth(http.HandlerFunc(pprof.Profile)))
	mux.Handle("GET /debug/pprof/symbol", s.requireAuth(http.HandlerFunc(pprof.Symbol)))
	mux.Handle("GET /debug/pprof/trace", s.requireAuth(http.HandlerFunc(pprof.Trace)))
}
