FROM alpine:3.22.0

RUN apk add --no-cache nmap gcompat

WORKDIR /app

COPY fleetd /app/

RUN mkdir -p /app/plugins
COPY proto-plugin antminer-plugin asicrs-plugin asicrs-config.yaml /app/plugins/

RUN chmod 755 /app/fleetd /app/plugins/*-plugin

EXPOSE 4000

CMD ["/app/fleetd"]
