package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DemonIngvi/proview-banner-service/internal/banner"
	"github.com/DemonIngvi/proview-banner-service/internal/store"
	"github.com/DemonIngvi/proview-banner-service/internal/token"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	testNow = time.Unix(1794000000, 0)
	discard = slog.New(slog.NewTextHandler(io.Discard, nil))
)

type staticPool struct{ s *banner.Snapshot }

func (p staticPool) Snapshot() *banner.Snapshot { return p.s }

func newPublic(t *testing.T, snap *banner.Snapshot) http.Handler {
	t.Helper()
	signer, err := token.NewSigner("k1", []byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatal(err)
	}
	return NewPublic(&Public{
		Pool: staticPool{snap}, Signer: signer, CountryHeader: "X-Country", Log: discard,
		Now: func() time.Time { return testNow }, Intn: func(int) int { return 0 }, NewID: func() string { return "imp-1" },
	}, prometheus.NewRegistry())
}

func demoSnapshot() *banner.Snapshot {
	return banner.NewSnapshot(
		[]banner.Placement{{ID: "home_top", Width: 728, Height: 90}, {ID: "empty", Width: 1, Height: 1}},
		[]banner.Banner{
			{ID: 7, Name: "secret name", ImageURL: "/b.svg", ClickURL: "https://e.com", Alt: "alt", Width: 728, Height: 90, Weight: 1, Active: true, Placements: []string{"home_top"}, Countries: []string{"RU"}},
		}, testNow)
}

func TestPublicServesBanner(t *testing.T) {
	h := newPublic(t, demoSnapshot())
	req := httptest.NewRequest(http.MethodGet, "/v1/banner?placement=home_top", nil)
	req.Header.Set("X-Country", "ru")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0) Chrome/130")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("missing Cache-Control: no-store")
	}
	var resp BannerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ImpressionID != "imp-1" || resp.Banner.ID != 7 || resp.Placement != "home_top" || resp.Token == "" {
		t.Fatalf("unexpected response %+v", resp)
	}
	if strings.Contains(rec.Body.String(), "secret name") {
		t.Error("internal banner name leaked to public response")
	}
}

func TestPublicStatusCodes(t *testing.T) {
	cases := []struct {
		name string
		snap *banner.Snapshot
		url  string
		hdr  string
		want int
	}{
		{"bad placement", demoSnapshot(), "/v1/banner?placement=Bad%20Id", "RU", http.StatusBadRequest},
		{"missing placement", demoSnapshot(), "/v1/banner", "RU", http.StatusBadRequest},
		{"unknown placement", demoSnapshot(), "/v1/banner?placement=nope", "RU", http.StatusNotFound},
		{"no fill: targeting", demoSnapshot(), "/v1/banner?placement=home_top", "DE", http.StatusNoContent},
		{"no fill: empty", demoSnapshot(), "/v1/banner?placement=empty", "RU", http.StatusNoContent},
		{"not loaded", nil, "/v1/banner?placement=home_top", "RU", http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			req.Header.Set("X-Country", tc.hdr)
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0) Chrome/130")
			rec := httptest.NewRecorder()
			newPublic(t, tc.snap).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// memStore is an in-memory AdminStore.
type memStore struct {
	mu         sync.Mutex
	placements []banner.Placement
	banners    map[int64]banner.Banner
	next       int64
}

func newMemStore() *memStore {
	return &memStore{placements: []banner.Placement{{ID: "home_top", Width: 728, Height: 90}}, banners: map[int64]banner.Banner{}, next: 1}
}

func (m *memStore) ListPlacements(context.Context) ([]banner.Placement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]banner.Placement(nil), m.placements...), nil
}
func (m *memStore) UpsertPlacement(_ context.Context, p banner.Placement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.placements = append(m.placements, p)
	return nil
}
func (m *memStore) ListBanners(context.Context) ([]banner.Banner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []banner.Banner
	for _, b := range m.banners {
		out = append(out, b)
	}
	return out, nil
}
func (m *memStore) GetBanner(_ context.Context, id int64) (banner.Banner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.banners[id]
	if !ok {
		return b, store.ErrNotFound
	}
	return b, nil
}
func (m *memStore) CreateBanner(_ context.Context, b banner.Banner) (banner.Banner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b.ID = m.next
	m.next++
	m.banners[b.ID] = b
	return b, nil
}
func (m *memStore) UpdateBanner(_ context.Context, b banner.Banner) (banner.Banner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.banners[b.ID]; !ok {
		return b, store.ErrNotFound
	}
	m.banners[b.ID] = b
	return b, nil
}
func (m *memStore) DeactivateBanner(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.banners[id]
	if !ok {
		return store.ErrNotFound
	}
	b.Active = false
	m.banners[id] = b
	return nil
}

type countingReloader struct{ n int }

func (c *countingReloader) Reload(context.Context) error { c.n++; return nil }

func doAdmin(h http.Handler, method, path, body, tok string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminAuth(t *testing.T) {
	h := (&Admin{Store: newMemStore(), Pool: &countingReloader{}, Token: "t0ken", Log: discard}).Handler()
	if rec := doAdmin(h, "GET", "/admin/v1/banners", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
	if rec := doAdmin(h, "GET", "/admin/v1/banners", "", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	empty := (&Admin{Store: newMemStore(), Pool: &countingReloader{}, Token: "", Log: discard}).Handler()
	if rec := doAdmin(empty, "GET", "/admin/v1/banners", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty configured token must deny: %d", rec.Code)
	}
}

func TestAdminBannerLifecycle(t *testing.T) {
	st, rl := newMemStore(), &countingReloader{}
	h := (&Admin{Store: st, Pool: rl, Token: "t", Log: discard}).Handler()

	body := `{"name":"n","image_url":"/a.svg","click_url":"https://e.com","width":728,"height":90,"placements":["home_top"]}`
	rec := doAdmin(h, "POST", "/admin/v1/banners", body, "t")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created banner.Banner
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if !created.Active || created.Weight != 100 {
		t.Fatalf("defaults not applied: %+v", created)
	}

	if rec := doAdmin(h, "POST", "/admin/v1/banners", `{"name":"n","image_url":"/a.svg","click_url":"https://e.com","width":1,"height":1,"placements":["missing"]}`, "t"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown placement: %d", rec.Code)
	}
	if rec := doAdmin(h, "POST", "/admin/v1/banners", `{"name":"n","unknown":1}`, "t"); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}
	if rec := doAdmin(h, "PUT", "/admin/v1/banners/1", strings.Replace(body, `"name":"n"`, `"name":"m","weight":5,"active":true`, 1), "t"); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	if rec := doAdmin(h, "PUT", "/admin/v1/banners/99", strings.Replace(body, `"name":"n"`, `"name":"m","weight":5`, 1), "t"); rec.Code != http.StatusNotFound {
		t.Fatalf("update missing: %d", rec.Code)
	}
	if rec := doAdmin(h, "GET", "/admin/v1/banners/abc", "", "t"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", rec.Code)
	}
	if rec := doAdmin(h, "DELETE", "/admin/v1/banners/1", "", "t"); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if b, _ := st.GetBanner(context.Background(), 1); b.Active {
		t.Fatal("banner still active after delete")
	}
	if rl.n != 3 {
		t.Fatalf("reloads = %d, want 3 (create, update, delete)", rl.n)
	}
}

func TestAdminPlacements(t *testing.T) {
	h := (&Admin{Store: newMemStore(), Pool: &countingReloader{}, Token: "t", Log: discard}).Handler()
	if rec := doAdmin(h, "PUT", "/admin/v1/placements/footer", `{"width":970,"height":90}`, "t"); rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	if rec := doAdmin(h, "PUT", "/admin/v1/placements/Bad", `{"width":970,"height":90}`, "t"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad id: %d", rec.Code)
	}
	rec := doAdmin(h, "GET", "/admin/v1/placements", "", "t")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "footer") {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
}

func BenchmarkPublic(b *testing.B) {
	signer, _ := token.NewSigner("k1", []byte(strings.Repeat("s", 32)))
	h := NewPublic(&Public{Pool: staticPool{demoSnapshot()}, Signer: signer, CountryHeader: "X-Country", Log: discard}, prometheus.NewRegistry())
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/v1/banner?placement=home_top", nil)
			req.Header.Set("X-Country", "RU")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				b.Fatal(rec.Code)
			}
		}
	})
}
