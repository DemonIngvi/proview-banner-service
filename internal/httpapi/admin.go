package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/DemonIngvi/proview-banner-service/internal/banner"
	"github.com/DemonIngvi/proview-banner-service/internal/store"
)

// AdminStore is the persistence used by the management API.
type AdminStore interface {
	ListPlacements(ctx context.Context) ([]banner.Placement, error)
	UpsertPlacement(ctx context.Context, p banner.Placement) error
	ListBanners(ctx context.Context) ([]banner.Banner, error)
	GetBanner(ctx context.Context, id int64) (banner.Banner, error)
	CreateBanner(ctx context.Context, b banner.Banner) (banner.Banner, error)
	UpdateBanner(ctx context.Context, b banner.Banner) (banner.Banner, error)
	DeactivateBanner(ctx context.Context, id int64) error
}

// Reloader refreshes the in-memory pool after a write.
type Reloader interface {
	Reload(ctx context.Context) error
}

// Admin is the internal management API. It is served on a separate port that
// must not be exposed publicly, and every request requires a bearer token.
type Admin struct {
	Store AdminStore
	Pool  Reloader
	Token string
	Log   *slog.Logger
}

const maxAdminBody = 64 << 10

// Handler returns the routed management API.
func (a *Admin) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/v1/placements", a.listPlacements)
	mux.HandleFunc("PUT /admin/v1/placements/{id}", a.putPlacement)
	mux.HandleFunc("GET /admin/v1/banners", a.listBanners)
	mux.HandleFunc("GET /admin/v1/banners/{id}", a.getBanner)
	mux.HandleFunc("POST /admin/v1/banners", a.createBanner)
	mux.HandleFunc("PUT /admin/v1/banners/{id}", a.updateBanner)
	mux.HandleFunc("DELETE /admin/v1/banners/{id}", a.deleteBanner)
	return a.auth(mux)
}

func (a *Admin) auth(next http.Handler) http.Handler {
	want := []byte("Bearer " + a.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if a.Token == "" || subtle.ConstantTimeCompare(got, want) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Admin) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	a.Log.Error("admin request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func (a *Admin) reload(r *http.Request) {
	if err := a.Pool.Reload(r.Context()); err != nil {
		a.Log.Warn("reload after write failed; periodic refresh will retry", "err", err)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAdminBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (a *Admin) listPlacements(w http.ResponseWriter, r *http.Request) {
	ps, err := a.Store.ListPlacements(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": ps})
}

func (a *Admin) putPlacement(w http.ResponseWriter, r *http.Request) {
	var p banner.Placement
	if !decode(w, r, &p) {
		return
	}
	p.ID = r.PathValue("id")
	if !banner.ValidPlacementID(p.ID) || p.Width < 1 || p.Height < 1 {
		writeError(w, http.StatusUnprocessableEntity, "invalid placement: id must be a slug, width/height positive")
		return
	}
	if err := a.Store.UpsertPlacement(r.Context(), p); err != nil {
		a.fail(w, err)
		return
	}
	a.reload(r)
	writeJSON(w, http.StatusOK, p)
}

func (a *Admin) listBanners(w http.ResponseWriter, r *http.Request) {
	bs, err := a.Store.ListBanners(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": bs})
}

func (a *Admin) getBanner(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, err := a.Store.GetBanner(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// validatePlacements checks that every referenced placement exists.
func (a *Admin) validatePlacements(r *http.Request, b *banner.Banner) error {
	ps, err := a.Store.ListPlacements(r.Context())
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(ps))
	for _, p := range ps {
		known[p.ID] = true
	}
	var missing []string
	for _, p := range b.Placements {
		if !known[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return &validationError{"unknown placements: " + strings.Join(missing, ", ")}
	}
	return nil
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }

func (a *Admin) validate(w http.ResponseWriter, r *http.Request, b *banner.Banner) bool {
	if err := b.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	if err := a.validatePlacements(r, b); err != nil {
		var ve *validationError
		if errors.As(err, &ve) {
			writeError(w, http.StatusUnprocessableEntity, ve.msg)
		} else {
			a.fail(w, err)
		}
		return false
	}
	return true
}

func (a *Admin) createBanner(w http.ResponseWriter, r *http.Request) {
	b := banner.Banner{Active: true, Weight: 100}
	if !decode(w, r, &b) || !a.validate(w, r, &b) {
		return
	}
	out, err := a.Store.CreateBanner(r.Context(), b)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.reload(r)
	writeJSON(w, http.StatusCreated, out)
}

func (a *Admin) updateBanner(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var b banner.Banner
	if !decode(w, r, &b) || !a.validate(w, r, &b) {
		return
	}
	b.ID = id
	out, err := a.Store.UpdateBanner(r.Context(), b)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.reload(r)
	writeJSON(w, http.StatusOK, out)
}

func (a *Admin) deleteBanner(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.Store.DeactivateBanner(r.Context(), id); err != nil {
		a.fail(w, err)
		return
	}
	a.reload(r)
	w.WriteHeader(http.StatusNoContent)
}
