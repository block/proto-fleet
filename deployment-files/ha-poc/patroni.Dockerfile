FROM postgres:17-bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        gettext-base \
        python3 \
        python3-pip \
        python3-venv \
    && python3 -m venv /opt/patroni \
    && /opt/patroni/bin/pip install --no-cache-dir "patroni[etcd3]" psycopg2-binary \
    && rm -rf /var/lib/apt/lists/*

ENV PATH="/opt/patroni/bin:${PATH}"

COPY deployment-files/ha-poc/patroni.template.yml /etc/patroni/patroni.template.yml
COPY deployment-files/ha-poc/scripts/render-and-run-patroni.sh /usr/local/bin/render-and-run-patroni
RUN chmod +x /usr/local/bin/render-and-run-patroni \
    && mkdir -p /etc/patroni \
    && chown -R postgres:postgres /etc/patroni /opt/patroni /var/lib/postgresql

USER postgres
ENTRYPOINT ["/usr/local/bin/render-and-run-patroni"]
