package config

import (
	"testing"
	"time"
)

func base(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/alvo")
	t.Setenv("JWT_SECRET", "secret")
}

func TestLoadDefaultsTheIngestScheduler(t *testing.T) {
	base(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.IngestEnabled {
		t.Error("INGEST_ENABLED defaulted on; a dev box must not start calling brapi by itself")
	}
	if !cfg.IngestIntraday {
		t.Error("INGEST_INTRADAY defaulted off; the 5m pass is the Pro default")
	}
	if !cfg.IngestFutures {
		t.Error("INGEST_FUTURES defaulted off; the tail is a no-op until futures are backfilled, so it costs nothing to leave on")
	}
	if cfg.IngestFillAt != DefaultIngestFillAt {
		t.Errorf("IngestFillAt = %s, want %s", cfg.IngestFillAt, DefaultIngestFillAt)
	}
}

func TestLoadReadsIngestSettings(t *testing.T) {
	base(t)
	t.Setenv("INGEST_ENABLED", "true")
	t.Setenv("INGEST_INTRADAY", "false")
	t.Setenv("INGEST_FUTURES", "false")
	t.Setenv("INGEST_FILL_AT", "21:30")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.IngestEnabled {
		t.Error("INGEST_ENABLED=true was not read")
	}
	if cfg.IngestIntraday {
		t.Error("INGEST_INTRADAY=false was not read")
	}
	if cfg.IngestFutures {
		t.Error("INGEST_FUTURES=false was not read")
	}
	if want := 21*time.Hour + 30*time.Minute; cfg.IngestFillAt != want {
		t.Errorf("IngestFillAt = %s, want %s", cfg.IngestFillAt, want)
	}
}

func TestLoadRejectsMalformedIngestSettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"INGEST_ENABLED", "yes please"},
		{"INGEST_INTRADAY", "sometimes"},
		{"INGEST_FUTURES", "maybe"},
		{"INGEST_FILL_AT", "half past eight"},
		{"INGEST_FILL_AT", "-5m"},
		{"INGEST_FILL_AT", "20h"},
		{"INGEST_FILL_AT", "25:00"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			base(t)
			t.Setenv(tc.key, tc.value)

			if _, err := Load(); err == nil {
				t.Errorf("Load accepted %s=%q", tc.key, tc.value)
			}
		})
	}
}

func TestEnvBoolAndClockFallBackWhenUnset(t *testing.T) {
	got, err := envBool("ALVO_MISSING_BOOL", true)
	if err != nil || !got {
		t.Errorf("envBool fallback = %v, %v", got, err)
	}

	clock, err := envClock("ALVO_MISSING_CLOCK", 20*time.Hour)
	if err != nil || clock != 20*time.Hour {
		t.Errorf("envClock fallback = %v, %v", clock, err)
	}
}

func TestEnvClockReadsAnHourOfTheDay(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"20:00": 20 * time.Hour,
		"08:30": 8*time.Hour + 30*time.Minute,
		"00:00": 0,
		"23:59": 23*time.Hour + 59*time.Minute,
	} {
		t.Setenv("ALVO_CLOCK", raw)
		got, err := envClock("ALVO_CLOCK", time.Hour)
		if err != nil || got != want {
			t.Errorf("envClock(%q) = %v, %v, want %v", raw, got, err, want)
		}
	}
}

func TestEnvBoolAcceptsTheUsualSpellings(t *testing.T) {
	for _, raw := range []string{"true", "TRUE", "1", " t ", "True"} {
		t.Setenv("ALVO_BOOL", raw)
		got, err := envBool("ALVO_BOOL", false)
		if err != nil || !got {
			t.Errorf("envBool(%q) = %v, %v", raw, got, err)
		}
	}
}
