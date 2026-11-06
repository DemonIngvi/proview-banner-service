// Package store persists the banner pool in PostgreSQL.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DemonIngvi/proview-banner-service/internal/banner"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps a pgx connection pool.
type Store struct {
	db *pgxpool.Pool
}

// Open connects to PostgreSQL and verifies connectivity.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if cfg.MaxConns > 8 {
		cfg.MaxConns = 8 // the hot path never queries the DB; only refreshes and admin writes do
	}
	cfg.MaxConnIdleTime = 5 * time.Minute
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.Ping(pctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.db.Close() }

// Ping checks the connection.
func (s *Store) Ping(ctx context.Context) error { return s.db.Ping(ctx) }

// Migrate applies embedded migrations in order. A session-level advisory
// lock makes concurrent runs (several pods, Helm hook retries) safe.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	const lockID = 0x70726f76 // "prov"
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return err
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", lockID) }()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		name := strings.TrimPrefix(f, "migrations/")
		version, err := strconv.ParseInt(strings.SplitN(name, "_", 2)[0], 10, 64)
		if err != nil {
			return fmt.Errorf("migration %s: bad version prefix", name)
		}
		var applied bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)", version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrationsFS.ReadFile(f)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			// No arguments: pgx uses the simple protocol, so a file may hold several statements.
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", version, name)
			return err
		})
		if err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

const bannerColumns = `id, name, image_url, click_url, alt, width, height, weight, active,
	starts_at, ends_at, placements, countries, devices, updated_at`

func scanBanner(row pgx.Row) (banner.Banner, error) {
	var b banner.Banner
	err := row.Scan(&b.ID, &b.Name, &b.ImageURL, &b.ClickURL, &b.Alt, &b.Width, &b.Height, &b.Weight, &b.Active,
		&b.StartsAt, &b.EndsAt, &b.Placements, &b.Countries, &b.Devices, &b.UpdatedAt)
	return b, err
}

// Load returns all placements and the banners that may still be served.
func (s *Store) Load(ctx context.Context) ([]banner.Placement, []banner.Banner, error) {
	placements, err := s.ListPlacements(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT `+bannerColumns+` FROM banners
		WHERE active AND (ends_at IS NULL OR ends_at > now()) ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	banners, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (banner.Banner, error) { return scanBanner(r) })
	return placements, banners, err
}

// ListPlacements returns all placement presets.
func (s *Store) ListPlacements(ctx context.Context) ([]banner.Placement, error) {
	rows, err := s.db.Query(ctx, `SELECT id, description, width, height FROM placements ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (banner.Placement, error) {
		var p banner.Placement
		err := r.Scan(&p.ID, &p.Description, &p.Width, &p.Height)
		return p, err
	})
}

// UpsertPlacement creates or updates a placement preset.
func (s *Store) UpsertPlacement(ctx context.Context, p banner.Placement) error {
	_, err := s.db.Exec(ctx, `INSERT INTO placements (id, description, width, height) VALUES ($1,$2,$3,$4)
		ON CONFLICT (id) DO UPDATE SET description=EXCLUDED.description, width=EXCLUDED.width,
		height=EXCLUDED.height, updated_at=now()`, p.ID, p.Description, p.Width, p.Height)
	return err
}

// ListBanners returns every banner including inactive ones.
func (s *Store) ListBanners(ctx context.Context) ([]banner.Banner, error) {
	rows, err := s.db.Query(ctx, `SELECT `+bannerColumns+` FROM banners ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (banner.Banner, error) { return scanBanner(r) })
}

// GetBanner returns a banner by id.
func (s *Store) GetBanner(ctx context.Context, id int64) (banner.Banner, error) {
	b, err := scanBanner(s.db.QueryRow(ctx, `SELECT `+bannerColumns+` FROM banners WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// CreateBanner inserts b and returns the stored row.
func (s *Store) CreateBanner(ctx context.Context, b banner.Banner) (banner.Banner, error) {
	return scanBanner(s.db.QueryRow(ctx, `INSERT INTO banners
		(name, image_url, click_url, alt, width, height, weight, active, starts_at, ends_at, placements, countries, devices)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+bannerColumns,
		b.Name, b.ImageURL, b.ClickURL, b.Alt, b.Width, b.Height, b.Weight, b.Active, b.StartsAt, b.EndsAt,
		b.Placements, nonNil(b.Countries), nonNil(b.Devices)))
}

// UpdateBanner replaces the mutable fields of banner b.ID.
func (s *Store) UpdateBanner(ctx context.Context, b banner.Banner) (banner.Banner, error) {
	out, err := scanBanner(s.db.QueryRow(ctx, `UPDATE banners SET name=$2, image_url=$3, click_url=$4, alt=$5,
		width=$6, height=$7, weight=$8, active=$9, starts_at=$10, ends_at=$11, placements=$12, countries=$13,
		devices=$14, updated_at=now() WHERE id=$1 RETURNING `+bannerColumns,
		b.ID, b.Name, b.ImageURL, b.ClickURL, b.Alt, b.Width, b.Height, b.Weight, b.Active, b.StartsAt, b.EndsAt,
		b.Placements, nonNil(b.Countries), nonNil(b.Devices)))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

// DeactivateBanner soft-deletes a banner so historical analytics keep a name.
func (s *Store) DeactivateBanner(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `UPDATE banners SET active=false, updated_at=now() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
