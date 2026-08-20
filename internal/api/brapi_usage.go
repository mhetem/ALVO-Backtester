package api

import (
	"log/slog"
	"net/http"
	"time"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

type brapiUsageDay struct {
	Day      string `json:"day"`
	Requests int32  `json:"requests"`
}

type brapiUsageBody struct {
	From     string          `json:"from"`
	To       string          `json:"to"`
	Requests int64           `json:"requests"`
	Days     []brapiUsageDay `json:"days"`
}

func (s *Server) handleBrapiUsage(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, -1)

	if value := r.URL.Query().Get("from"); value != "" {
		parsed, err := time.Parse(time.DateOnly, value)
		if err != nil {
			respondError(w, r, http.StatusBadRequest, "from must be a YYYY-MM-DD date")
			return
		}
		from = parsed
	}
	if value := r.URL.Query().Get("to"); value != "" {
		parsed, err := time.Parse(time.DateOnly, value)
		if err != nil {
			respondError(w, r, http.StatusBadRequest, "to must be a YYYY-MM-DD date")
			return
		}
		to = parsed
	}
	if to.Before(from) {
		respondError(w, r, http.StatusBadRequest, "to must not be before from")
		return
	}

	rows, err := s.queries.ListBrapiUsage(r.Context(), database.ListBrapiUsageParams{Day: from, Day_2: to})
	if err != nil {
		s.log.ErrorContext(r.Context(), "listing brapi usage",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.Any("err", err),
		)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	total, err := s.queries.SumBrapiUsage(r.Context(), database.SumBrapiUsageParams{Day: from, Day_2: to})
	if err != nil {
		s.log.ErrorContext(r.Context(), "summing brapi usage",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.Any("err", err),
		)
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	body := brapiUsageBody{
		From:     from.Format(time.DateOnly),
		To:       to.Format(time.DateOnly),
		Requests: total,
		Days:     make([]brapiUsageDay, 0, len(rows)),
	}
	for _, row := range rows {
		body.Days = append(body.Days, brapiUsageDay{
			Day:      row.Day.Format(time.DateOnly),
			Requests: row.Requests,
		})
	}

	respondJSON(w, r, http.StatusOK, body)
}
