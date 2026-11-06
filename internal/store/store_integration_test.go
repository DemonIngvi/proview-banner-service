//go:build integration

package store

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/DemonIngvi/proview-banner-service/internal/banner"
)

// Requires a disposable database: PROVIEW_TEST_PG_DSN=postgres://... go test -tags integration ./...
func openTest(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("PROVIEW_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PROVIEW_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if _, err := s.db.Exec(ctx, `DROP TABLE IF EXISTS banners, placements, schema_migrations`); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMigrateIsIdempotentAndSeeds(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	for range 2 {
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	placements, banners, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(placements) != 3 || len(banners) != 6 {
		t.Fatalf("seed: %d placements, %d banners", len(placements), len(banners))
	}
}

func TestBannerCRUD(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateBanner(ctx, banner.Banner{Name: "t", ImageURL: "/x.svg", ClickURL: "https://e.com", Width: 1, Height: 1,
		Weight: 5, Active: true, Placements: []string{"sidebar"}, Countries: []string{"RU"}})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == 0 || b.Devices == nil || b.Countries[0] != "RU" {
		t.Fatalf("created: %+v", b)
	}
	b.Weight = 9
	if b, err = s.UpdateBanner(ctx, b); err != nil || b.Weight != 9 {
		t.Fatalf("update: %+v %v", b, err)
	}
	if err := s.DeactivateBanner(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	_, active, _ := s.Load(ctx)
	for _, a := range active {
		if a.ID == b.ID {
			t.Fatal("deactivated banner still loaded")
		}
	}
	if _, err := s.GetBanner(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetBanner missing: %v", err)
	}
	if err := s.DeactivateBanner(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Deactivate missing: %v", err)
	}
}
