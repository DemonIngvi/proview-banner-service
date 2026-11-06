package banner

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func ptr(t time.Time) *time.Time { return &t }

func testSnapshot() *Snapshot {
	placements := []Placement{{ID: "home_top", Width: 728, Height: 90}, {ID: "sidebar", Width: 300, Height: 250}, {ID: "empty"}}
	banners := []Banner{
		{ID: 1, Active: true, Weight: 100, Placements: []string{"home_top"}},
		{ID: 2, Active: true, Weight: 300, Placements: []string{"home_top", "sidebar"}},
		{ID: 3, Active: false, Weight: 1000, Placements: []string{"home_top"}},
		{ID: 4, Active: true, Weight: 100, Placements: []string{"sidebar"}, Countries: []string{"RU"}},
		{ID: 5, Active: true, Weight: 100, Placements: []string{"sidebar"}, Devices: []string{DeviceMobile}},
		{ID: 6, Active: true, Weight: 100, Placements: []string{"sidebar"}, StartsAt: ptr(now.Add(time.Hour))},
		{ID: 7, Active: true, Weight: 100, Placements: []string{"sidebar"}, EndsAt: ptr(now)},
		{ID: 8, Active: true, Weight: 100, Placements: []string{"unknown"}},
	}
	return NewSnapshot(placements, banners, now)
}

func TestSnapshotLen(t *testing.T) {
	if got := testSnapshot().Len(); got != 7 {
		t.Fatalf("Len() = %d, want 7 active banners", got)
	}
}

func TestSelectErrors(t *testing.T) {
	s := testSnapshot()
	if _, err := s.Select(Request{Placement: "nope", Now: now}, rand.IntN); !errors.Is(err, ErrUnknownPlacement) {
		t.Fatalf("unknown placement: got %v", err)
	}
	if _, err := s.Select(Request{Placement: "empty", Now: now}, rand.IntN); !errors.Is(err, ErrNoBanner) {
		t.Fatalf("empty placement: got %v", err)
	}
}

func TestSelectTargeting(t *testing.T) {
	s := testSnapshot()
	tests := []struct {
		name string
		req  Request
		want map[int64]bool
	}{
		{"untargeted desktop", Request{Placement: "sidebar", Device: DeviceDesktop, Now: now}, map[int64]bool{2: true}},
		{"country RU", Request{Placement: "sidebar", Country: "RU", Device: DeviceDesktop, Now: now}, map[int64]bool{2: true, 4: true}},
		{"mobile", Request{Placement: "sidebar", Device: DeviceMobile, Now: now}, map[int64]bool{2: true, 5: true}},
		{"after start", Request{Placement: "sidebar", Device: DeviceDesktop, Now: now.Add(2 * time.Hour)}, map[int64]bool{2: true, 6: true}},
		{"before end", Request{Placement: "sidebar", Device: DeviceDesktop, Now: now.Add(-time.Minute)}, map[int64]bool{2: true, 7: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen := map[int64]bool{}
			for range 2000 {
				b, err := s.Select(tt.req, rand.IntN)
				if err != nil {
					t.Fatal(err)
				}
				if !tt.want[b.ID] {
					t.Fatalf("selected ineligible banner %d", b.ID)
				}
				seen[b.ID] = true
			}
			for id := range tt.want {
				if !seen[id] {
					t.Errorf("eligible banner %d never selected", id)
				}
			}
		})
	}
}

func TestSelectWeightedDistribution(t *testing.T) {
	s := testSnapshot()
	r := rand.New(rand.NewPCG(1, 2))
	counts := map[int64]int{}
	const n = 100000
	for range n {
		b, err := s.Select(Request{Placement: "home_top", Now: now}, r.IntN)
		if err != nil {
			t.Fatal(err)
		}
		counts[b.ID]++
	}
	// Weights 100 and 300 -> expected shares 0.25 and 0.75.
	share := float64(counts[1]) / n
	if math.Abs(share-0.25) > 0.01 {
		t.Fatalf("banner 1 share = %.3f, want ~0.25", share)
	}
	if counts[3] != 0 {
		t.Fatalf("inactive banner selected %d times", counts[3])
	}
}

func TestSelectDeterministicBoundaries(t *testing.T) {
	s := testSnapshot()
	req := Request{Placement: "home_top", Now: now}
	cases := map[int]int64{0: 1, 99: 1, 100: 2, 399: 2}
	for n, want := range cases {
		b, err := s.Select(req, func(int) int { return n })
		if err != nil || b.ID != want {
			t.Errorf("intn=%d: got %v,%v want %d", n, b, err, want)
		}
	}
}

func TestBannerValidate(t *testing.T) {
	valid := Banner{Name: "x", ImageURL: "/banners/a.svg", ClickURL: "https://example.com", Weight: 1, Width: 1, Height: 1, Placements: []string{"home_top"}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid banner: %v", err)
	}
	bad := []func(b *Banner){
		func(b *Banner) { b.Name = " " },
		func(b *Banner) { b.ImageURL = "javascript:alert(1)" },
		func(b *Banner) { b.ClickURL = "//evil.example" },
		func(b *Banner) { b.Weight = 0 },
		func(b *Banner) { b.Width = 0 },
		func(b *Banner) { b.Placements = nil },
		func(b *Banner) { b.Placements = []string{"Bad Id"} },
		func(b *Banner) { b.Devices = []string{"tv"} },
		func(b *Banner) { b.Countries = []string{"ru"} },
		func(b *Banner) { b.StartsAt, b.EndsAt = ptr(now), ptr(now) },
	}
	for i, mut := range bad {
		b := valid
		mut(&b)
		if err := b.Validate(); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func BenchmarkSelect(b *testing.B) {
	placements := []Placement{{ID: "home_top"}}
	var banners []Banner
	for i := range 200 {
		banners = append(banners, Banner{ID: int64(i), Active: true, Weight: 1 + i%50, Placements: []string{"home_top"}, Countries: []string{"RU", "DE"}[i%2 : i%2+1]})
	}
	s := NewSnapshot(placements, banners, now)
	req := Request{Placement: "home_top", Country: "RU", Device: DeviceDesktop, Now: now}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := s.Select(req, rand.IntN); err != nil {
				b.Fatal(err)
			}
		}
	})
}
