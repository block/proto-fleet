FROM golang:1.26.4-bookworm AS build

WORKDIR /src/server
ENV GOWORK=off
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY server ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /out/ha-poc-fake-fleet ./devtools/hapoc

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ha-poc-fake-fleet /usr/local/bin/ha-poc-fake-fleet
ENTRYPOINT ["/usr/local/bin/ha-poc-fake-fleet"]
