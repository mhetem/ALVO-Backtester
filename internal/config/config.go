package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	PlatformDev  = "dev"
	PlatformProd = "prod"
)

type Config struct {
	DatabaseURL string
	Port        string
	BrapiToken  string
	JWTSecret   string
	Platform    string
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
