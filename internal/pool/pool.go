// Package pool keeps the current banner snapshot in memory and refreshes it
// periodically from the source of truth. Request handling never touches the
// database: if the database becomes unavailable the last good snapshot keeps
// being served (stale-while-error).
package pool

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/DemonIngvi/proview-banner-service/internal/banner"
	"github.com/prometheus/client_golang/prometheus"
)

// Loader loads the full pool from the source of truth.
type Loader interface {
	Load(ctx context.Context) ([]banner.Placement, []banner.Banner, error)
}

// Pool holds an atomically swapped snapshot.
type Pool struct {
	loader   Loader
	interval time.Duration
	log      *slog.Logger
	now      func() time.Time
	cur      atomic.Pointer[banner.Snapshot]

	reloads     *prometheus.CounterVec
	bannerCount prometheus.Gauge
	loadedAt    prometheus.Gauge
}

// New creates a Pool. Call Run to start refreshing.
func New(loader Loader, interval time.Duration, log *slog.Logger, reg prometheus.Registerer) *Pool {
	p := &Pool{
		loader: loader, interval: interval, log: log, now: time.Now,
		reloads: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "proview_banner", Name: "pool_reloads_total", Help: "Banner pool reloads by result.",
		}, []string{"result"}),
		bannerCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "proview_banner", Name: "pool_banners", Help: "Active banners in the current snapshot.",
		}),
		loadedAt: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "proview_banner", Name: "pool_loaded_timestamp_seconds", Help: "Unix time of the current snapshot.",
		}),
	}
	reg.MustRegister(p.reloads, p.bannerCount, p.loadedAt)
	return p
}

// ErrNotLoaded is returned by Ready before the first successful load.
var ErrNotLoaded = errors.New("banner pool not loaded yet")

// Snapshot returns the current snapshot or nil.
func (p *Pool) Snapshot() *banner.Snapshot { return p.cur.Load() }

// Ready reports whether at least one snapshot has been loaded.
func (p *Pool) Ready() error {
	if p.cur.Load() == nil {
		return ErrNotLoaded
	}
	return nil
}

// Reload loads and swaps the snapshot once.
func (p *Pool) Reload(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	placements, banners, err := p.loader.Load(ctx)
	if err != nil {
		p.reloads.WithLabelValues("error").Inc()
		return err
	}
	s := banner.NewSnapshot(placements, banners, p.now())
	p.cur.Store(s)
	p.reloads.WithLabelValues("ok").Inc()
	p.bannerCount.Set(float64(s.Len()))
	p.loadedAt.Set(float64(s.LoadedAt.Unix()))
	return nil
}

// Run refreshes the snapshot until ctx is done. The first load is retried
// with exponential backoff; later failures keep the previous snapshot.
// A random jitter of up to 20% spreads reloads of many replicas over time.
func (p *Pool) Run(ctx context.Context) {
	backoff := 200 * time.Millisecond
	for p.cur.Load() == nil {
		if err := p.Reload(ctx); err != nil {
			p.log.Warn("initial pool load failed", "err", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 10*time.Second)
			continue
		}
		p.log.Info("pool loaded", "banners", p.cur.Load().Len())
	}
	for {
		jitter := time.Duration(rand.Int64N(int64(p.interval)/5 + 1))
		select {
		case <-ctx.Done():
			return
		case <-time.After(p.interval + jitter):
		}
		if err := p.Reload(ctx); err != nil {
			p.log.Warn("pool reload failed, serving previous snapshot", "err", err,
				"snapshot_age", p.now().Sub(p.cur.Load().LoadedAt).String())
		}
	}
}
