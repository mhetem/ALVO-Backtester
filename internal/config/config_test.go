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
	if cfg.IngestDelay != DefaultIngestDelay {
		t.Errorf("IngestDelay = %s, want %s", cfg.IngestDelay, DefaultIngestDelay)
	}
}

func TestLoadReadsIngestSettings(t *testing.T) {
	base(t)
	t.Setenv("INGEST_ENABLED", "true")
	t.Setenv("INGEST_INTRADAY", "false")
	t.Setenv("INGEST_FUTURES", "false")
	t.Setenv("INGEST_CLOSE_DELAY", "45m")

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
	if cfg.IngestDelay != 45*time.Minute {
		t.Errorf("IngestDelay = %s, want 45m", cfg.IngestDelay)
	}
}

func TestLoadRejectsMalformedIngestSettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"INGEST_ENABLED", "yes please"},
		{"INGEST_INTRADAY", "sometimes"},
		{"INGEST_FUTURES", "maybe"},
		{"INGEST_CLOSE_DELAY", "half an hour"},
		{"INGEST_CLOSE_DELAY", "-5m"},
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

func TestEnvBoolAndDurationFallBackWhenUnset(t *testing.T) {
	got, err := envBool("ALVO_MISSING_BOOL", true)
	if err != nil || !got {
		t.Errorf("envBool fallback = %v, %v", got, err)
	}

	dur, err := envDuration("ALVO_MISSING_DURATION", 90*time.Second)
	if err != nil || dur != 90*time.Second {
		t.Errorf("envDuration fallback = %v, %v", dur, err)
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
