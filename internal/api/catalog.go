package api

import (
	"log/slog"
	"net/http"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
)

type paramBody struct {
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	Default float64 `json:"default"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
}

type catalogEntryBody struct {
	Name    string      `json:"name"`
	Title   string      `json:"title"`
	Group   string      `json:"group"`
	Overlay bool        `json:"overlay"`
	Sourced bool        `json:"sourced"`
	Key     string      `json:"key"`
	Warmup  int         `json:"warmup"`
	Params  []paramBody `json:"params"`
	Outputs []string    `json:"outputs"`
	Offsets []int       `json:"offsets"`
}

type catalogBody struct {
	Count         int                `json:"count"`
	MaxPerRequest int                `json:"max_per_request"`
	Groups        []string           `json:"groups"`
	Sources       []string           `json:"sources"`
	Indicators    []catalogEntryBody `json:"indicators"`
}

func (s *Server) handleIndicators(w http.ResponseWriter, r *http.Request) {
	specs := indicator.Catalog()

	body := catalogBody{
		Count:         len(specs),
		MaxPerRequest: indicator.MaxInstances,
		Groups:        make([]string, 0, len(indicator.Groups)),
		Sources:       make([]string, 0, len(indicator.Sources)),
		Indicators:    make([]catalogEntryBody, 0, len(specs)),
	}
	for _, group := range indicator.Groups {
		body.Groups = append(body.Groups, group.String())
	}
	for _, source := range indicator.Sources {
		body.Sources = append(body.Sources, source.String())
	}

	for _, spec := range specs {
		instance, err := indicator.New(spec.Name, nil, "")
		if err != nil {
			s.log.ErrorContext(r.Context(), "building a catalogue entry",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("indicator", spec.Name),
				slog.Any("err", err),
			)
			respondError(w, r, http.StatusInternalServerError, "internal server error")
			return
		}

		entry := catalogEntryBody{
			Name:    spec.Name,
			Title:   spec.Title,
			Group:   spec.Group.String(),
			Overlay: spec.Overlay,
			Sourced: spec.Sourced,
			Key:     instance.Key,
			Warmup:  instance.Indicator.Warmup(),
			Params:  make([]paramBody, 0, len(spec.Params)),
			Outputs: spec.Outputs,
			Offsets: instance.Offsets,
		}
		for _, param := range spec.Params {
			entry.Params = append(entry.Params, paramBody{
				Name:    param.Name,
				Kind:    string(param.Kind),
				Default: param.Default,
				Min:     param.Min,
				Max:     param.Max,
			})
		}

		body.Indicators = append(body.Indicators, entry)
	}

	respondCached(w, r, body, cacheCatalog)
}
