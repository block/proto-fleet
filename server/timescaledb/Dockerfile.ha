# Proto Fleet TimescaleDB HA image.
#
# This intentionally layers only pg_auto_failover onto the existing lightweight
# Proto Fleet TimescaleDB image. The single-node image and entrypoint stay
# unchanged; HA containers use docker-entrypoint-ha.sh and run pg_autoctl.

FROM proto-fleet-timescaledb:latest

ARG DEBIAN_FRONTEND=noninteractive
ARG PG_MAJOR=18

USER root

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        pg-auto-failover-cli \
        postgresql-${PG_MAJOR}-auto-failover; \
    sed -i "s/^shared_preload_libraries = 'timescaledb'/shared_preload_libraries = 'timescaledb,pgautofailover'/" \
        /usr/share/postgresql/${PG_MAJOR}/postgresql.conf.sample; \
    apt-get clean; \
    rm -rf /var/lib/apt/lists/*

COPY docker-entrypoint-ha.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint-ha.sh

ENTRYPOINT ["docker-entrypoint-ha.sh"]
CMD ["pg_autoctl", "run", "--pgdata", "/home/postgres/pgdata/data"]
