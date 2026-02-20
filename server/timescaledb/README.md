# Custom TimescaleDB Docker Image

A lightweight alternative to `timescale/timescaledb-ha` that includes only what Proto Fleet needs:
PostgreSQL 18, TimescaleDB, timescaledb_toolkit, pgvector, and timescaledb-tune.

## Size Comparison

| Image | Content Size | Toolkit? | pgvector? | timescaledb-tune? |
|---|---|---|---|---|
| `timescale/timescaledb-ha:pg17-ts2.25` | **1.67 GB** | ✅ | ✅ | ✅ |
| `timescale/timescaledb:2.25.1-pg18` (Alpine) | **169 MB** | ❌ | ✅ | ✅ |
| `proto-fleet/timescaledb` (this image) | **~120 MB** | ✅ | ✅ | ✅ |

**93% smaller** than the HA image, and **29% smaller** than the standard Alpine
image — while including the toolkit that Alpine can't support.

## Comparison with the Standard Alpine Image

The standard `timescale/timescaledb:2.25.1-pg18` is Alpine-based and includes
timescaledb-tune, pgvector, and PL/Python3 (for PG < 18), but **cannot** include
the timescaledb_toolkit extension (requires glibc; Alpine uses musl libc).

| Feature | Alpine (`timescale/timescaledb`) | This Image |
|---|---|---|
| Base OS | Alpine (musl libc) | Ubuntu 24.04 (glibc) |
| PostgreSQL | 18 | 18 |
| TimescaleDB | ✅ | ✅ |
| timescaledb_toolkit | ❌ (requires glibc) | ✅ |
| pgvector | ✅ (built from source) | ✅ (pre-built PGDG package) |
| timescaledb-tune | ✅ (built from source) | ✅ (built from source) |
| PL/Python3 | ❌ (not available for PG18) | ❌ (not installed) |
| timescaledb-parallel-copy | ✅ | ❌ (not needed) |
| Auto-tune on first start | ✅ | ✅ |
| Image size | ~169 MB | ~120 MB |

The Alpine image builds both pgvector and TimescaleDB from source during the
Docker build, resulting in a larger image. This image uses pre-compiled `.deb`
packages from PGDG and Timescale's packagecloud repositories.

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
- **pgvector** from the PGDG apt repository — vector similarity search for
  embeddings and nearest-neighbor queries
- **timescaledb-tune** — auto-tunes PostgreSQL configuration based on container
  resources (memory, CPUs) on first start, with cgroups v1/v2 detection
- Minimal entrypoint handling database initialization, database creation, and
  privilege management via `gosu`

### Versions (as tested)

| Component | Version |
|---|---|
| PostgreSQL | 18.2 |
| TimescaleDB | 2.25.1 |
| timescaledb_toolkit | 1.22.0 |
| pgvector | 0.8.1 |
| timescaledb-tune | 0.18.1 |

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
| `NO_TS_TUNE` | (none) | Set to any value to skip timescaledb-tune on init |
| `TS_TUNE_MEMORY` | (auto-detected) | Override memory for tuning (e.g., `4GB`) |
| `TS_TUNE_NUM_CPUS` | (auto-detected) | Override CPU count for tuning |
| `TS_TUNE_MAX_CONNS` | (none) | Set max connections for tuning |
| `TS_TUNE_MAX_BG_WORKERS` | (none) | Set max background workers for tuning |

### Notes on timescaledb-tune

On first start, `timescaledb-tune` automatically optimizes `postgresql.conf`
based on the container's available resources. It detects memory and CPU limits
from cgroups (v1 and v2), so Docker `--memory` and `--cpus` flags are respected.

When using docker-compose with explicit `-c` flags (as Proto Fleet does), those
command-line settings take precedence over the tuned `postgresql.conf` values.
The tune is still useful for any settings not explicitly overridden.

## Volume Path

PGDATA is at `/home/postgres/pgdata/data`, matching the HA image layout for
volume compatibility. Existing named volumes (`timescaledb-data`) will work
without data migration.

## Architecture Support

Pre-compiled packages for TimescaleDB, the toolkit, and pgvector are available
for both `amd64` and `arm64` on Ubuntu Noble via the PGDG and Timescale
packagecloud repositories. The timescaledb-tune binary is compiled from Go
source during the Docker build, supporting any architecture Go targets.

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
