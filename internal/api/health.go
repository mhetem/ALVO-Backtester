package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

const healthzTimeout = 2 * time.Second

type healthBody struct {
	Status string `json:"status"`
	DB     string `json:"db"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthzTimeout)
	defer cancel()

	if err := s.db.Ping(ctx); err != nil {
		s.log.ErrorContext(ctx, "healthz database ping failed",
			slog.String("request_id", RequestIDFrom(ctx)),
			slog.Any("err", err),
		)
		respondJSON(w, r, http.StatusServiceUnavailable, healthBody{Status: "degraded", DB: "down"})
		return
	}

	respondJSON(w, r, http.StatusOK, healthBody{Status: "ok", DB: "up"})
}
