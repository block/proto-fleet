FROM golang:1.25.4-alpine AS builder

WORKDIR /app

# Copy go.mod files for dependency caching
COPY server/go.mod server/go.sum ./server/
COPY server/fake-proto-rig/go.mod ./server/fake-proto-rig/

# Download dependencies
WORKDIR /app/server
RUN go mod download

WORKDIR /app/server/fake-proto-rig
RUN go mod download

# Copy server code (needed for generated types)
WORKDIR /app
COPY server/ ./server/

# Build the fake-proto-rig binary
WORKDIR /app/server/fake-proto-rig
RUN CGO_ENABLED=0 GOOS=linux go build -o /fake-proto-rig .

# Final stage - minimal runtime image
FROM alpine:3.21

RUN apk add --no-cache ca-certificates wget

COPY --from=builder /fake-proto-rig /fake-proto-rig

EXPOSE 8080

ENTRYPOINT ["/fake-proto-rig"]
