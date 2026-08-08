package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type errorBody struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

func respondJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		slog.ErrorContext(r.Context(), "marshalling response", slog.Any("err", err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal server error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func respondError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	respondJSON(w, r, status, errorBody{Error: msg, RequestID: RequestIDFrom(r.Context())})
}
