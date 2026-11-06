// Package banner contains the banner pool domain model and the selection
// algorithm. It has no I/O dependencies.
package banner

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Errors returned by Snapshot.Select.
var (
	ErrUnknownPlacement = errors.New("unknown placement")
	ErrNoBanner         = errors.New("no eligible banner")
)

// Device classes used for targeting.
const (
	DeviceDesktop = "desktop"
	DeviceMobile  = "mobile"
	DeviceTablet  = "tablet"
	DeviceBot     = "bot"
)

var placementIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidPlacementID reports whether id is a valid placement slug.
func ValidPlacementID(id string) bool { return placementIDRe.MatchString(id) }

// Placement is a preset describing a slot on a page: its size and identity.
type Placement struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

// Banner is a creative in the pool together with its targeting rules.
type Banner struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	ImageURL   string     `json:"image_url"`
	ClickURL   string     `json:"click_url"`
	Alt        string     `json:"alt"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	Weight     int        `json:"weight"`
	Active     bool       `json:"active"`
	StartsAt   *time.Time `json:"starts_at,omitempty"`
	EndsAt     *time.Time `json:"ends_at,omitempty"`
	Placements []string   `json:"placements"`
	Countries  []string   `json:"countries"`
	Devices    []string   `json:"devices"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Validate checks a banner submitted through the management API.
func (b *Banner) Validate() error {
	var errs []error
	if strings.TrimSpace(b.Name) == "" {
		errs = append(errs, errors.New("name is required"))
	}
	if !validURL(b.ImageURL) {
		errs = append(errs, errors.New("image_url must be an http(s) URL or an absolute path"))
	}
	if !validURL(b.ClickURL) {
		errs = append(errs, errors.New("click_url must be an http(s) URL or an absolute path"))
	}
	if b.Weight < 1 || b.Weight > 10000 {
		errs = append(errs, errors.New("weight must be within 1..10000"))
	}
	if b.Width < 1 || b.Height < 1 {
		errs = append(errs, errors.New("width and height must be positive"))
	}
	if len(b.Placements) == 0 {
		errs = append(errs, errors.New("at least one placement is required"))
	}
	for _, p := range b.Placements {
		if !ValidPlacementID(p) {
			errs = append(errs, errors.New("invalid placement id: "+p))
		}
	}
	for _, d := range b.Devices {
		if !slices.Contains([]string{DeviceDesktop, DeviceMobile, DeviceTablet}, d) {
			errs = append(errs, errors.New("invalid device: "+d))
		}
	}
	for _, c := range b.Countries {
		if len(c) != 2 || strings.ToUpper(c) != c {
			errs = append(errs, errors.New("country must be ISO 3166-1 alpha-2 in upper case: "+c))
		}
	}
	if b.StartsAt != nil && b.EndsAt != nil && !b.EndsAt.After(*b.StartsAt) {
		errs = append(errs, errors.New("ends_at must be after starts_at"))
	}
	return errors.Join(errs...)
}

func validURL(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") ||
		(strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//"))
}

// Request describes the context of a banner request.
type Request struct {
	Placement string
	Country   string // ISO alpha-2, upper case, empty when unknown
	Device    string
	Now       time.Time
}

func (b *Banner) eligible(r Request) bool {
	if !b.Active {
		return false
	}
	if b.StartsAt != nil && r.Now.Before(*b.StartsAt) {
		return false
	}
	if b.EndsAt != nil && !r.Now.Before(*b.EndsAt) {
		return false
	}
	if len(b.Countries) > 0 && !slices.Contains(b.Countries, r.Country) {
		return false
	}
	if len(b.Devices) > 0 && !slices.Contains(b.Devices, r.Device) {
		return false
	}
	return true
}

// Snapshot is an immutable, indexed view of the banner pool. It is built once
// per refresh and shared by all request goroutines without locking.
type Snapshot struct {
	placements  map[string]Placement
	byPlacement map[string][]*Banner
	banners     int
	LoadedAt    time.Time
}

// NewSnapshot indexes banners by placement. Banners referencing unknown
// placements are ignored for those placements.
func NewSnapshot(placements []Placement, banners []Banner, loadedAt time.Time) *Snapshot {
	s := &Snapshot{
		placements:  make(map[string]Placement, len(placements)),
		byPlacement: make(map[string][]*Banner, len(placements)),
		LoadedAt:    loadedAt,
	}
	for _, p := range placements {
		s.placements[p.ID] = p
	}
	for i := range banners {
		b := &banners[i]
		if !b.Active {
			continue
		}
		s.banners++
		for _, p := range b.Placements {
			if _, ok := s.placements[p]; ok {
				s.byPlacement[p] = append(s.byPlacement[p], b)
			}
		}
	}
	return s
}

// Len returns the number of active banners in the snapshot.
func (s *Snapshot) Len() int { return s.banners }

// Placement returns the placement preset by id.
func (s *Snapshot) Placement(id string) (Placement, bool) {
	p, ok := s.placements[id]
	return p, ok
}

// Select picks an eligible banner for r using weighted random choice. intn
// must return a uniformly distributed integer in [0, n).
func (s *Snapshot) Select(r Request, intn func(n int) int) (*Banner, error) {
	if _, ok := s.placements[r.Placement]; !ok {
		return nil, ErrUnknownPlacement
	}
	list := s.byPlacement[r.Placement]
	total := 0
	for _, b := range list {
		if b.eligible(r) {
			total += b.Weight
		}
	}
	if total == 0 {
		return nil, ErrNoBanner
	}
	n := intn(total)
	for _, b := range list {
		if !b.eligible(r) {
			continue
		}
		if n < b.Weight {
			return b, nil
		}
		n -= b.Weight
	}
	return nil, ErrNoBanner
}
