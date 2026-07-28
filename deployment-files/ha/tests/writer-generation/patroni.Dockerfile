ARG GO_IMAGE=golang:1.26.5-bookworm
ARG BASE_IMAGE=proto-fleet-timescaledb:ha-writer-test

FROM ${GO_IMAGE} AS observer-builder

WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN go test -c -tags=ha_fixture -o /out/ha-observer.test ./internal/ha

FROM ${BASE_IMAGE}

ARG PATRONI_VERSION=4.1.4

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        gettext-base \
        python3 \
        python3-venv \
    && python3 -m venv /opt/patroni \
    && /opt/patroni/bin/pip install --no-cache-dir \
        "patroni[etcd3,psycopg2-binary]==${PATRONI_VERSION}" \
    && rm -rf /var/lib/apt/lists/*

ENV PATH="/opt/patroni/bin:${PATH}"

COPY deployment-files/ha/tests/writer-generation/patroni.template.yml /etc/patroni/patroni.template.yml
COPY deployment-files/ha/tests/writer-generation/patroni-entrypoint.sh /usr/local/bin/patroni-entrypoint
COPY --from=observer-builder /out/ha-observer.test /opt/ha/ha-observer.test

RUN chmod +x /usr/local/bin/patroni-entrypoint /opt/ha/ha-observer.test \
    && mkdir -p /etc/patroni /opt/ha \
    && chown -R postgres:postgres /etc/patroni /opt/ha /home/postgres

ENTRYPOINT ["/usr/local/bin/patroni-entrypoint"]
