package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	PlatformDev  = "dev"
	PlatformProd = "prod"

	DefaultIngestFillAt = 20 * time.Hour
)

type Config struct {
	DatabaseURL    string
	Port           string
	BrapiToken     string
	JWTSecret      string
	Platform       string
	TrustProxy     bool
	IngestEnabled  bool
	IngestIntraday bool
	IngestFutures  bool
	IngestFillAt   time.Duration
}

func (c Config) IsDev() bool { return c.Platform == PlatformDev }

func (c Config) Addr() string { return ":" + c.Port }

func Load() (Config, error) {
	var missing []string

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        envOr("PORT", "8080"),
		BrapiToken:  os.Getenv("BRAPI_TOKEN"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		Platform:    envOr("PLATFORM", PlatformProd),
	}

	var err error
	if cfg.TrustProxy, err = envBool("TRUST_PROXY", false); err != nil {
		return Config{}, err
	}
	if cfg.IngestEnabled, err = envBool("INGEST_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.IngestIntraday, err = envBool("INGEST_INTRADAY", true); err != nil {
		return Config{}, err
	}
	if cfg.IngestFutures, err = envBool("INGEST_FUTURES", true); err != nil {
		return Config{}, err
	}
	if cfg.IngestFillAt, err = envClock("INGEST_FILL_AT", DefaultIngestFillAt); err != nil {
		return Config{}, err
	}

	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	switch cfg.Platform {
	case PlatformDev, PlatformProd:
	default:
		return Config{}, fmt.Errorf("PLATFORM must be %q or %q, got %q", PlatformDev, PlatformProd, cfg.Platform)
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false, got %q", key, raw)
	}
	return value, nil
}

// A time of day rather than an offset from the close: the candle fill waits for brapi to
// finish revising the session, which has nothing to do with when the bell rang.
func envClock(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.Parse("15:04", raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a HH:MM time of day such as 20:00, got %q", key, raw)
	}
	return time.Duration(value.Hour())*time.Hour + time.Duration(value.Minute())*time.Minute, nil
}
