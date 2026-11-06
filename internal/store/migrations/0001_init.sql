-- Placement presets: a slot on a page with a fixed size.
CREATE TABLE IF NOT EXISTS placements (
    id          text PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9_-]{0,63}$'),
    description text        NOT NULL DEFAULT '',
    width       integer     NOT NULL CHECK (width > 0),
    height      integer     NOT NULL CHECK (height > 0),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Banner pool with targeting rules. Empty arrays mean "no restriction".
CREATE TABLE IF NOT EXISTS banners (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        text        NOT NULL,
    image_url   text        NOT NULL,
    click_url   text        NOT NULL,
    alt         text        NOT NULL DEFAULT '',
    width       integer     NOT NULL CHECK (width > 0),
    height      integer     NOT NULL CHECK (height > 0),
    weight      integer     NOT NULL DEFAULT 100 CHECK (weight BETWEEN 1 AND 10000),
    active      boolean     NOT NULL DEFAULT true,
    starts_at   timestamptz,
    ends_at     timestamptz,
    placements  text[]      NOT NULL CHECK (cardinality(placements) > 0),
    countries   text[]      NOT NULL DEFAULT '{}',
    devices     text[]      NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX IF NOT EXISTS banners_active_idx ON banners (active) WHERE active;
