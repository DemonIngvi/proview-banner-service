package pool

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/DemonIngvi/proview-banner-service/internal/banner"
	"github.com/prometheus/client_golang/prometheus"
)

type fakeLoader struct {
	mu    sync.Mutex
	fail  bool
	calls int
	ids   []int64
}

func (f *fakeLoader) Load(context.Context) ([]banner.Placement, []banner.Banner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return nil, nil, errors.New("db down")
	}
	var bs []banner.Banner
	for _, id := range f.ids {
		bs = append(bs, banner.Banner{ID: id, Active: true, Weight: 1, Placements: []string{"p"}})
	}
	return []banner.Placement{{ID: "p"}}, bs, nil
}

func (f *fakeLoader) set(fail bool, ids ...int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail, f.ids = fail, ids
}

func newPool(l Loader, interval time.Duration) *Pool {
	return New(l, interval, slog.New(slog.NewTextHandler(io.Discard, nil)), prometheus.NewRegistry())
}

func TestReloadKeepsPreviousSnapshotOnError(t *testing.T) {
	l := &fakeLoader{}
	l.set(false, 1, 2)
	p := newPool(l, time.Hour)
	if err := p.Ready(); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("Ready before load = %v", err)
	}
	if err := p.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := p.Snapshot()
	if first.Len() != 2 || p.Ready() != nil {
		t.Fatalf("unexpected snapshot: len=%d ready=%v", first.Len(), p.Ready())
	}
	l.set(true)
	if err := p.Reload(context.Background()); err == nil {
		t.Fatal("expected reload error")
	}
	if p.Snapshot() != first {
		t.Fatal("snapshot replaced after failed reload")
	}
}

func TestRunRetriesInitialLoadAndRefreshes(t *testing.T) {
	l := &fakeLoader{}
	l.set(true)
	p := newPool(l, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	time.Sleep(50 * time.Millisecond)
	if p.Snapshot() != nil {
		t.Fatal("snapshot loaded while loader fails")
	}
	l.set(false, 1)
	waitFor(t, func() bool { s := p.Snapshot(); return s != nil && s.Len() == 1 })
	l.set(false, 1, 2, 3)
	waitFor(t, func() bool { return p.Snapshot().Len() == 3 })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancel")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
