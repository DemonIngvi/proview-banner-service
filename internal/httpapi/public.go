// Package httpapi exposes the public banner endpoint and the internal
// management API.
package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	randv2 "math/rand/v2"
	"net/http"
	"time"

	"github.com/DemonIngvi/proview-banner-service/internal/banner"
	"github.com/DemonIngvi/proview-banner-service/internal/token"
	"github.com/DemonIngvi/proview-banner-service/internal/useragent"
	"github.com/prometheus/client_golang/prometheus"
)

// SnapshotSource returns the current pool snapshot (nil before first load).
type SnapshotSource interface {
	Snapshot() *banner.Snapshot
}

// Public serves GET /v1/banner.
type Public struct {
	Pool          SnapshotSource
	Signer        *token.Signer
	CountryHeader string
	Log           *slog.Logger
	Now           func() time.Time
	Intn          func(int) int
	NewID         func() string

	served *prometheus.CounterVec
}

// BannerResponse is the JSON body returned to the SDK.
type BannerResponse struct {
	ImpressionID string       `json:"impression_id"`
	Token        string       `json:"token"`
	Placement    string       `json:"placement"`
	Banner       PublicBanner `json:"banner"`
}

// PublicBanner is the subset of banner fields safe to expose publicly.
type PublicBanner struct {
	ID       int64  `json:"id"`
	ImageURL string `json:"image_url"`
	ClickURL string `json:"click_url"`
	Alt      string `json:"alt"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// NewPublic fills defaults and registers metrics.
func NewPublic(p *Public, reg prometheus.Registerer) *Public {
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.Intn == nil {
		p.Intn = randv2.IntN
	}
	if p.NewID == nil {
		p.NewID = rand.Text
	}
	p.served = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "proview_banner", Name: "selections_total", Help: "Banner selections by placement and result.",
	}, []string{"placement", "result"})
	reg.MustRegister(p.served)
	return p
}

func (p *Public) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	placement := r.URL.Query().Get("placement")
	if !banner.ValidPlacementID(placement) {
		writeError(w, http.StatusBadRequest, "invalid placement")
		return
	}
	snap := p.Pool.Snapshot()
	if snap == nil {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "banner pool is not loaded")
		return
	}
	device := useragent.Device(r.UserAgent())
	now := p.Now()
	b, err := snap.Select(banner.Request{
		Placement: placement,
		Country:   useragent.Country(r.Header.Get(p.CountryHeader)),
		Device:    device,
		Now:       now,
	}, p.Intn)
	switch {
	case errors.Is(err, banner.ErrUnknownPlacement):
		p.served.WithLabelValues("unknown", "unknown_placement").Inc()
		writeError(w, http.StatusNotFound, "unknown placement")
		return
	case errors.Is(err, banner.ErrNoBanner):
		p.served.WithLabelValues(placement, "no_fill").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	case err != nil:
		p.Log.Error("select banner", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	p.served.WithLabelValues(placement, "filled").Inc()

	impressionID := p.NewID()
	resp := BannerResponse{
		ImpressionID: impressionID,
		Token: p.Signer.Sign(token.Claims{
			ImpressionID: impressionID, BannerID: b.ID, Placement: placement, IssuedAt: now,
		}),
		Placement: placement,
		Banner: PublicBanner{
			ID: b.ID, ImageURL: b.ImageURL, ClickURL: b.ClickURL, Alt: b.Alt, Width: b.Width, Height: b.Height,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
