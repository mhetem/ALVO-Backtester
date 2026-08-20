package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

const (
	cacheClosedRange = "public, max-age=86400, immutable"
	cacheOpenRange   = "no-cache"
	cacheSymbols     = "public, max-age=300"
)

func respondCached(w http.ResponseWriter, r *http.Request, payload any, cacheControl string) {
	body, err := json.Marshal(payload)
	if err != nil {
		slog.ErrorContext(r.Context(), "marshalling response", slog.Any("err", err))
		respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`

	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", cacheControl)

	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func etagMatches(header, etag string) bool {
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}
