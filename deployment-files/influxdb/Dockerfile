FROM debian:bullseye-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Copy the pre-built binary
COPY influxdb3 /usr/local/bin/influxdb3
RUN chmod +x /usr/local/bin/influxdb3

ENV INFLUXDB_HTTP_PORT=8181
ENV INFLUXDB3_DB_DIR=/var/lib/influxdb3
ENV INFLUXDB3_OBJECT_STORE=file

VOLUME /var/lib/influxdb3
VOLUME /etc/influxdb3

EXPOSE 8181

ENTRYPOINT ["influxdb3"]
