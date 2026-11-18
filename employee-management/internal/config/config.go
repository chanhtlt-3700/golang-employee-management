// Package config reads the application settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Storage back ends.
const (
	StorageMemory   = "memory"
	StoragePostgres = "postgres"
)

// Config holds every setting. See README for the matching variables.
type Config struct {
	HTTPAddr        string
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration

	DeleteMode string // "soft" or "hard"

	Storage  string // StorageMemory or StoragePostgres
	DBDriver string // database/sql driver name, e.g. "pgx" or "postgres"
	DBDSN    string
	SeedData bool // load demo data at start-up (memory storage only)

	ExportDir      string
	ExportInterval time.Duration // 0 disables scheduled exports
}

// Load reads the configuration and reports every invalid value at once.
func Load() (Config, error) {
	var errs []error
	cfg := Config{
		HTTPAddr:        envString("HTTP_ADDR", ":8080"),
		RequestTimeout:  envDuration("REQUEST_TIMEOUT", 5*time.Second, &errs),
		ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second, &errs),
		DeleteMode:      strings.ToLower(envString("DELETE_MODE", "soft")),
		Storage:         strings.ToLower(envString("STORAGE", StorageMemory)),
		DBDriver:        envString("DB_DRIVER", "pgx"),
		DBDSN:           os.Getenv("DB_DSN"),
		SeedData:        envBool("SEED_DATA", true, &errs),
		ExportDir:       envString("EXPORT_DIR", "exports"),
		ExportInterval:  envDuration("EXPORT_INTERVAL", 5*time.Minute, &errs),
	}

	if cfg.DeleteMode != "soft" && cfg.DeleteMode != "hard" {
		errs = append(errs, fmt.Errorf(`DELETE_MODE must be "soft" or "hard", got %q`, cfg.DeleteMode))
	}
	switch cfg.Storage {
	case StorageMemory:
	case StoragePostgres:
		if cfg.DBDSN == "" {
			errs = append(errs, errors.New("DB_DSN is required when STORAGE=postgres"))
		}
	default:
		errs = append(errs, fmt.Errorf("STORAGE must be %q or %q, got %q", StorageMemory, StoragePostgres, cfg.Storage))
	}
	return cfg, errors.Join(errs...)
}

func envString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func envDuration(key string, fallback time.Duration, errs *[]error) time.Duration {
	raw := envString(key, "")
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		*errs = append(*errs, fmt.Errorf("%s must be a non-negative duration such as 30s or 5m, got %q", key, raw))
		return fallback
	}
	return d
}

func envBool(key string, fallback bool, errs *[]error) bool {
	raw := envString(key, "")
	if raw == "" {
		return fallback
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be true or false, got %q", key, raw))
		return fallback
	}
	return b
}
