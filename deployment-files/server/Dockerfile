FROM alpine:3.22.0

RUN apk add --no-cache nmap gcompat

WORKDIR /app

COPY fleetd /app/
COPY sv2-translator-helper /app/

RUN mkdir -p /app/plugins
COPY proto-plugin antminer-plugin asicrs-plugin asicrs-config.yaml /app/plugins/
COPY virtual-plugin virtual-plugin.json /app/optional-plugins/

RUN chmod 755 /app/fleetd /app/sv2-translator-helper /app/plugins/*-plugin /app/optional-plugins/virtual-plugin

EXPOSE 4000

CMD ["sh", "-c", "case \"${ENABLE_VIRTUAL_MINERS:-false}\" in true|1|yes|YES|on|ON) cp /app/optional-plugins/virtual-plugin /app/optional-plugins/virtual-plugin.json /app/plugins/ ;; esac; exec /app/fleetd"]
