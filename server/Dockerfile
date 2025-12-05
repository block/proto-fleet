# Use the official Go image as a base image
FROM golang:1.25.4-alpine AS builder

RUN apk add git

RUN go install github.com/go-delve/delve/cmd/dlv@v1.25.2

# Set the working directory inside the container
WORKDIR /app

# Copy go module files
COPY server/go.mod server/go.sum ./

# Download Go dependencies
RUN go mod download

# Copy the source code into the container
COPY server/ ./

# Build the Go application
RUN go build -gcflags="all=-N -l" -o out/ -v ./...

FROM alpine:3.22.0 AS runner

# Install Nmap
RUN apk add --no-cache nmap

# Set the working directory
WORKDIR /app

# Copy the binary from builder and set permissions
COPY --from=builder /go/bin/dlv /app/dlv
COPY --from=builder /app/out/fleetd /app/fleetd

RUN chmod 755 /app/fleetd

# Create plugins directory
# Plugins are typically mounted via docker-compose volume for local dev
# For CI/E2E, plugins must be built before docker build and will be copied here
RUN mkdir -p /app/plugins

# Expose the port your application listens on
EXPOSE 4000

# Debugging port
EXPOSE 40000

# Command to run when the container starts
CMD ["/app/dlv", "--listen=:40000", "--headless=true", "--continue", "--api-version=2", "--accept-multiclient", "exec", "/app/fleetd"]
