# Proto Fleet Custom TimescaleDB Image
#
# A lightweight alternative to timescale/timescaledb-ha (~1.6 GB) that includes
# PostgreSQL 18, TimescaleDB, timescaledb_toolkit, pgvector, and timescaledb-tune.
#
# What's excluded (and why this image is ~75% smaller):
#   - Patroni + Python 3 (~120 MB) — HA cluster manager, not needed for single-node
#   - PostGIS + GDAL/GEOS/PROJ (~130 MB) — geospatial, not used
#   - pgBackRest (~25 MB) — backup tool, not used
#   - WAL-G (~20 MB) — WAL archiving, not used
#   - PgBouncer (~5 MB) — connection pooler, not used
#   - Barman Cloud (~30 MB) — CloudNativePG backup, not used
#   - LLVM/Clang JIT (~80 MB) — query JIT compilation, marginal benefit for this workload
#   - System utils: awscli, gdb, vim, strace, htop, etc. (~30 MB)
#
# Base image choice: Ubuntu 24.04 (Noble)
#   - Matches the HA image's base (glibc compatibility, same collation behavior)
#   - Pre-compiled toolkit .deb packages available on packagecloud (Rust-compiled, no build step)
#   - Multi-arch support: amd64 + arm64 packages available
#
# PGDATA path: /home/postgres/pgdata/data
#   - Matches the HA image path for volume compatibility during migration
#   - Existing named volumes (timescaledb-data) will work without data migration

############################
# Build timescaledb-tune from source
############################
FROM golang:1.26.11-alpine AS tools

RUN CGO_ENABLED=0 go install github.com/timescale/timescaledb-tune/cmd/timescaledb-tune@v0.18.1

############################
# Main image
############################
FROM ubuntu:24.04

ARG DEBIAN_FRONTEND=noninteractive

# Versions — pin each package that the apt-get install command names.
# Change the pins together. Each version must exist for amd64 and arm64.
# Do not decrease TIMESCALEDB_VERSION or TOOLKIT_VERSION: a database needs the
# library for the extension version that created it. See README.md.
ARG PG_MAJOR=18
# PostgreSQL 18.7 makes TimescaleDB background jobs record a failure. Hold
# 18.6 until a TimescaleDB release supports 18.7.
ARG PG_VERSION=18.6-1.pgdg24.04+2
# The last pgvector build for PostgreSQL 18.6.
ARG PGVECTOR_VERSION=0.8.7-1.pgdg24.04+1
# The extension and its loader use this version. "-1806" is the 18.6 build.
ARG TIMESCALEDB_VERSION=2.32.1~ubuntu24.04-1806
ARG TOOLKIT_VERSION=1:1.28.0~ubuntu24.04

# Create postgres user/group with UID/GID 999 matching the official postgres Docker
# image convention. Note: the HA image used UID 1000 — the entrypoint handles
# ownership fixup via chown on startup, so existing volumes will work regardless.
RUN groupadd -r postgres --gid=999 \
    && useradd -r -g postgres --uid=999 --home-dir=/home/postgres --shell=/bin/bash postgres \
    && mkdir -p /home/postgres \
    && chown -R postgres:postgres /home/postgres

# Install base dependencies and set up package repositories
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        gnupg \
        gosu \
        locales \
        lsb-release; \
    # Generate locale (match HA image)
    locale-gen en_US.UTF-8; \
    \
    # --- PostgreSQL PGDG repository ---
    curl -fsSL https://apt.postgresql.org/pub/repos/apt/ACCC4CF8.asc \
        | gpg --dearmor -o /usr/share/keyrings/postgresql.gpg; \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/postgresql.gpg] \
        https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" \
        > /etc/apt/sources.list.d/pgdg.list; \
    \
    # --- Timescale packagecloud repository ---
    curl -fsSL https://packagecloud.io/timescale/timescaledb/gpgkey \
        | gpg --dearmor -o /usr/share/keyrings/timescaledb.gpg; \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/timescaledb.gpg] \
        https://packagecloud.io/timescale/timescaledb/ubuntu/ $(lsb_release -cs) main" \
        > /etc/apt/sources.list.d/timescaledb.list; \
    \
    # --- Install PostgreSQL + TimescaleDB + Toolkit + pgvector ---
    apt-get update; \
    apt-get install -y --no-install-recommends \
        postgresql-${PG_MAJOR}=${PG_VERSION} \
        postgresql-client-${PG_MAJOR}=${PG_VERSION} \
        timescaledb-2-postgresql-${PG_MAJOR}=${TIMESCALEDB_VERSION} \
        timescaledb-2-loader-postgresql-${PG_MAJOR}=${TIMESCALEDB_VERSION} \
        timescaledb-toolkit-postgresql-${PG_MAJOR}=${TOOLKIT_VERSION} \
        postgresql-${PG_MAJOR}-pgvector=${PGVECTOR_VERSION}; \
    \
    # --- Cleanup to minimize image size ---
    apt-get purge -y --auto-remove \
        lsb-release; \
    apt-get clean; \
    rm -rf \
        /var/lib/apt/lists/* \
        /usr/share/doc \
        /usr/share/man \
        /usr/share/locale/[a-d]* \
        /usr/share/locale/[f-z]* \
        /usr/share/locale/e[a-m]* \
        /usr/share/locale/en_[A-T]* \
        /usr/share/locale/en_[V-Z]*

# Copy timescaledb-tune from builder
COPY --from=tools /go/bin/timescaledb-tune /usr/local/bin/

# Configure shared_preload_libraries for TimescaleDB
RUN echo "shared_preload_libraries = 'timescaledb'" \
    >> /usr/share/postgresql/${PG_MAJOR}/postgresql.conf.sample

# Set up data directory structure matching HA image layout
ENV PGDATA=/home/postgres/pgdata/data \
    PATH=/usr/lib/postgresql/${PG_MAJOR}/bin:$PATH \
    LANG=en_US.UTF-8 \
    LC_ALL=en_US.UTF-8 \
    PG_MAJOR=${PG_MAJOR}

RUN mkdir -p "$PGDATA" /var/run/postgresql \
    && chown -R postgres:postgres "$PGDATA" /var/run/postgresql \
    && chmod 2777 /var/run/postgresql \
    && chmod 700 "$PGDATA"

COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

STOPSIGNAL SIGINT
EXPOSE 5432

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["postgres"]
