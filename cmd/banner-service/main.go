// Command banner-service serves banners from an in-memory pool.
//
// Usage:
//
//	banner-service          run the HTTP servers (default)
//	banner-service migrate  apply database migrations and exit
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/DemonIngvi/proview-banner-service/internal/config"
	"github.com/DemonIngvi/proview-banner-service/internal/httpapi"
	"github.com/DemonIngvi/proview-banner-service/internal/platform"
	"github.com/DemonIngvi/proview-banner-service/internal/pool"
	"github.com/DemonIngvi/proview-banner-service/internal/store"
	"github.com/DemonIngvi/proview-banner-service/internal/token"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		return migrate(ctx)
	}

	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return err
	}
	log := platform.NewLogger(cfg.LogLevel, "banner-service").With("version", version)

	st, err := store.Open(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer st.Close()

	signer, err := token.NewSigner(cfg.TokenKeyID, []byte(cfg.TokenSecret))
	if err != nil {
		return err
	}

	reg := platform.NewRegistry()
	p := pool.New(st, cfg.RefreshInterval, log, reg)
	go p.Run(ctx)

	ready := &platform.Readiness{}
	ready.AddCheck(p.Ready)

	metrics := platform.NewHTTPMetrics(reg, "proview_banner")
	public := httpapi.NewPublic(&httpapi.Public{Pool: p, Signer: signer, CountryHeader: cfg.CountryHeader, Log: log}, reg)
	mux := http.NewServeMux()
	mux.Handle("/v1/banner", metrics.Wrap("/v1/banner", public))
	publicHandler := platform.Recover(log, platform.CORS(cfg.AllowedOrigins, "GET, OPTIONS", mux))

	admin := &httpapi.Admin{Store: st, Pool: p, Token: cfg.AdminToken, Log: log}
	if cfg.AdminToken == "" {
		log.Warn("ADMIN_TOKEN is empty: management API rejects every request")
	}

	return platform.Serve(ctx, log, ready, cfg.ShutdownDelay, cfg.ShutdownTimeout,
		platform.NewServer(cfg.HTTPAddr, publicHandler),
		platform.NewServer(cfg.AdminAddr, platform.Recover(log, metrics.Wrap("/admin", admin.Handler()))),
		platform.NewServer(cfg.OpsAddr, platform.OpsHandler(reg, ready)),
	)
}

// migrate needs only the database DSN so that the migration Job does not
// require application secrets.
func migrate(ctx context.Context) error {
	log := platform.NewLogger(platform.Env(os.LookupEnv, "LOG_LEVEL", "info"), "banner-service").With("version", version)
	dsn := platform.Env(os.LookupEnv, "POSTGRES_DSN", "")
	if dsn == "" {
		return fmt.Errorf("POSTGRES_DSN is required")
	}
	st, err := store.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	log.Info("migrations applied")
	return nil
}
