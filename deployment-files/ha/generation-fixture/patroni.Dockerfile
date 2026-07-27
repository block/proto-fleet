ARG BASE_IMAGE=proto-fleet-timescaledb:ha07
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

COPY patroni.template.yml /etc/patroni/patroni.template.yml
COPY scripts/patroni-entrypoint.sh /usr/local/bin/patroni-entrypoint
COPY scripts/observe_generation.py /opt/ha07/observe_generation.py

RUN chmod +x /usr/local/bin/patroni-entrypoint /opt/ha07/observe_generation.py \
    && mkdir -p /etc/patroni /opt/ha07 \
    && chown -R postgres:postgres /etc/patroni /opt/ha07 /home/postgres

ENTRYPOINT ["/usr/local/bin/patroni-entrypoint"]
