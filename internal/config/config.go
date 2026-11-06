// Package config loads service configuration from environment variables
// (12-factor). Secrets are expected to come from Kubernetes Secrets.
package config

import (
	"errors"
	"time"

	"github.com/DemonIngvi/proview-banner-service/internal/platform"
)

// Config is the banner-service configuration.
type Config struct {
	HTTPAddr        string
	AdminAddr       string
	OpsAddr         string
	PostgresDSN     string
	RefreshInterval time.Duration
	TokenKeyID      string
	TokenSecret     string
	AdminToken      string
	AllowedOrigins  []string
	CountryHeader   string
	ShutdownDelay   time.Duration
	ShutdownTimeout time.Duration
	LogLevel        string
}

// Load reads configuration using lookup (os.LookupEnv in production).
func Load(lookup func(string) (string, bool)) (Config, error) {
	c := Config{
		HTTPAddr:       platform.Env(lookup, "HTTP_ADDR", ":8080"),
		AdminAddr:      platform.Env(lookup, "ADMIN_ADDR", ":8081"),
		OpsAddr:        platform.Env(lookup, "OPS_ADDR", ":9090"),
		PostgresDSN:    platform.Env(lookup, "POSTGRES_DSN", ""),
		TokenKeyID:     platform.Env(lookup, "TOKEN_KEY_ID", "k1"),
		TokenSecret:    platform.Env(lookup, "TOKEN_SECRET", ""),
		AdminToken:     platform.Env(lookup, "ADMIN_TOKEN", ""),
		AllowedOrigins: platform.EnvList(lookup, "CORS_ALLOWED_ORIGINS", []string{"*"}),
		CountryHeader:  platform.Env(lookup, "COUNTRY_HEADER", "X-Country"),
		LogLevel:       platform.Env(lookup, "LOG_LEVEL", "info"),
	}
	var errs []error
	var err error
	if c.RefreshInterval, err = platform.EnvDuration(lookup, "REFRESH_INTERVAL", 10*time.Second); err != nil {
		errs = append(errs, err)
	}
	if c.ShutdownDelay, err = platform.EnvDuration(lookup, "SHUTDOWN_DELAY", 5*time.Second); err != nil {
		errs = append(errs, err)
	}
	if c.ShutdownTimeout, err = platform.EnvDuration(lookup, "SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		errs = append(errs, err)
	}
	if c.PostgresDSN == "" {
		errs = append(errs, errors.New("POSTGRES_DSN is required"))
	}
	if len(c.TokenSecret) < 32 {
		errs = append(errs, errors.New("TOKEN_SECRET must be at least 32 characters"))
	}
	if c.RefreshInterval < time.Second {
		errs = append(errs, errors.New("REFRESH_INTERVAL must be at least 1s"))
	}
	return c, errors.Join(errs...)
}
