FROM golang:1.26.5-alpine AS builder

WORKDIR /app/server/fake-ct1

# Copy go.mod files for dependency caching
COPY server/fake-ct1/go.mod server/fake-ct1/go.sum ./
RUN go mod download

# Copy source and build. fake-ct1 is a standalone module (not in go.work),
# so the build runs with the workspace disabled.
COPY server/fake-ct1/ ./
RUN CGO_ENABLED=0 GOOS=linux GOWORK=off go build -o /fake-ct1 .

# Final stage - minimal runtime image
FROM alpine:3.21

RUN apk add --no-cache ca-certificates wget

COPY --from=builder /fake-ct1 /fake-ct1

EXPOSE 8080

ENTRYPOINT ["/fake-ct1"]
