# Build context is the server directory: the bridge shares the generated
# stubs module at ../generated/grpc via a replace directive.
FROM golang:1.25.4-alpine AS build
WORKDIR /src
COPY generated/grpc/go.mod generated/grpc/go.sum ./generated/grpc/
COPY rig-otlp-bridge/go.mod rig-otlp-bridge/go.sum ./rig-otlp-bridge/
RUN cd rig-otlp-bridge && go mod download
COPY generated/grpc ./generated/grpc
COPY rig-otlp-bridge ./rig-otlp-bridge
RUN cd rig-otlp-bridge && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/otlp-bridge .

# alpine (Docker Hub) rather than upstream's gcr.io distroless base;
# non-root preserved via a dedicated user.
FROM alpine:3.22.0
RUN apk add --no-cache ca-certificates \
 && addgroup -S bridge && adduser -S -G bridge bridge
COPY --from=build /out/otlp-bridge /usr/local/bin/otlp-bridge
USER bridge
ENTRYPOINT ["/usr/local/bin/otlp-bridge"]
