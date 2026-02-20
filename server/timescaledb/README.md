# Custom TimescaleDB Docker Image

A lightweight alternative to `timescale/timescaledb-ha` that includes only what Proto Fleet needs:
PostgreSQL 18, TimescaleDB, and the timescaledb_toolkit extension.

## Size Comparison

| Image | Content Size | Toolkit? |
|---|---|---|
| `timescale/timescaledb-ha:pg17-ts2.25` | **1.67 GB** | ✅ |
| `timescale/timescaledb:2.25.1-pg18` (Alpine, no toolkit) | **169 MB** | ❌ |
| `proto-fleet/timescaledb` (this image) | **123 MB** | ✅ |

**93% smaller** than the HA image, and **27% smaller** than the standard Alpine
image — while including the toolkit that Alpine can't support.

## Why This Image Exists

The only reason Proto Fleet used the `timescaledb-ha` image was to get the
`timescaledb_toolkit` extension (required by migration 000006). The HA image
bundles a large number of components for Kubernetes-based high availability that
Proto Fleet does not use:

| Component | ~Size | Purpose | Used by Proto Fleet? |
|---|---|---|---|
| PostgreSQL 17 + contrib | ~90 MB | Database server | ✅ Yes |
| TimescaleDB | ~20 MB | Time-series extension | ✅ Yes |
| timescaledb_toolkit | ~24 MB | Analytical hyperfunctions | ✅ Yes |
| Patroni + Python 3 | ~120 MB | HA cluster manager + runtime | ❌ No |
| PostGIS + GDAL/GEOS/PROJ | ~130 MB | Geospatial extensions | ❌ No |
| LLVM/Clang JIT | ~80 MB | Query JIT compilation | ❌ No |
| Barman Cloud | ~30 MB | CloudNativePG backup | ❌ No |
| pgBackRest | ~25 MB | Backup & WAL archiving | ❌ No |
| WAL-G | ~20 MB | WAL archiving to cloud | ❌ No |
| System utils (awscli, gdb, vim, strace, htop…) | ~30 MB | Debugging & ops tooling | ❌ No |
| PgBouncer | ~5 MB | Connection pooler | ❌ No |

## What's Included

- **Ubuntu 24.04** (Noble) base — matches the HA image's glibc for collation
  compatibility. The toolkit extension is a Rust-compiled binary distributed as
  a pre-compiled `.deb` package, and Ubuntu Noble is one of the supported targets.
- **PostgreSQL 18** from the official PGDG apt repository — async I/O (up to 3×
  read perf), `uuidv7()`, virtual generated columns, faster `pg_upgrade`
- **TimescaleDB 2.x** from Timescale's packagecloud apt repository
- **timescaledb_toolkit** from the same repository — provides `time_weight()`,
  `stats_agg()`, and other analytical hyperfunctions
- Minimal entrypoint handling database initialization, database creation, and
  privilege management via `gosu`

### Versions (as tested)

| Component | Version |
|---|---|
| PostgreSQL | 18.2 |
| TimescaleDB | 2.25.1 |
| timescaledb_toolkit | 1.22.0 |

Versions are not pinned in the Dockerfile by default, so rebuilding will pull
the latest compatible packages. Pin versions in the `apt-get install` line if
you need reproducible builds.

## Building

```bash
docker build -t proto-fleet/timescaledb:latest server/timescaledb/
```

## Usage

This image is referenced as a build target in `docker-compose.base.yaml`:

```yaml
timescaledb:
  build:
    context: ./timescaledb
  environment:
    POSTGRES_DB: "fleet"
    POSTGRES_USER: "fleet"
    POSTGRES_PASSWORD: "fleet"
  volumes:
    - timescaledb-data:/home/postgres/pgdata/data
```

Both `docker-compose.yaml` (dev) and `deployment-files/docker-compose.yaml`
(prod) extend from the base, so they pick up this image automatically.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `POSTGRES_DB` | (none) | Database to create on first run |
| `POSTGRES_USER` | `postgres` | Superuser name |
| `POSTGRES_PASSWORD` | (none) | Superuser password |

## Volume Path

PGDATA is at `/home/postgres/pgdata/data`, matching the HA image layout for
volume compatibility. Existing named volumes (`timescaledb-data`) will work
without data migration.

## Architecture Support

Pre-compiled packages for TimescaleDB and the toolkit are available for both
`amd64` and `arm64` on Ubuntu Noble via Timescale's packagecloud repository.

## Initialization Scripts

Mount `.sh` or `.sql` files into `/docker-entrypoint-initdb.d/` to run on
first start (before the main postgres process starts).

## Design Decisions

### Why Ubuntu instead of Alpine?

The standard `timescale/timescaledb` image is Alpine-based (~168 MB) but the
toolkit extension cannot be installed on Alpine — it requires glibc (Alpine uses
musl libc) and pre-compiled packages are only published for Debian and Ubuntu.
Building the toolkit from Rust source on Alpine is possible but adds significant
build complexity and time.

### Why not `postgres:17-bookworm` as the base?

Toolkit packages are available for Debian Bookworm, so this would also work.
Ubuntu was chosen because:
1. The HA image is already Ubuntu-based, so this is a known-good environment
2. Collation behavior matches what the existing data was created with
3. Both options produce similar image sizes

### Toolkit usage note

As of this writing, the `timescaledb_toolkit` extension is created in migration
000006 but no toolkit-specific functions (`time_weight`, `stats_agg`,
`counter_agg`, etc.) are currently called in any SQL queries. The continuous
aggregates only use standard TimescaleDB functions (`time_bucket`,
`add_continuous_aggregate_policy`). The extension is included for future use.
