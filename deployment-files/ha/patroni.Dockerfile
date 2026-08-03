FROM proto-fleet-timescaledb:latest

ARG SOURCE_COMMIT=unknown

USER root

COPY patroni-requirements.txt /opt/patroni-requirements.txt

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        python3 \
        python3-venv \
    && python3 -m venv /opt/patroni \
    && /opt/patroni/bin/pip install \
        --no-cache-dir \
        --require-hashes \
        --requirement /opt/patroni-requirements.txt \
    && rm -rf /var/lib/apt/lists/*

ENV PATH="/opt/patroni/bin:${PATH}" \
    PROTO_FLEET_SOURCE_COMMIT="${SOURCE_COMMIT}"

LABEL org.opencontainers.image.revision="${SOURCE_COMMIT}"

COPY patroni.yml.tmpl /etc/patroni/patroni.yml.tmpl
COPY scripts/patroni-entrypoint.sh /usr/local/bin/patroni-entrypoint
COPY scripts/patroni-post-bootstrap.sh /usr/local/bin/patroni-post-bootstrap
COPY scripts/render-patroni-config.py /usr/local/bin/render-patroni-config

RUN chmod 0755 \
        /usr/local/bin/patroni-entrypoint \
        /usr/local/bin/patroni-post-bootstrap \
        /usr/local/bin/render-patroni-config

USER postgres

STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/patroni-entrypoint"]
