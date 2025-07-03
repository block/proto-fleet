# Use the official Go image as a base image
FROM golang:1.24.2-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go module files
COPY server/go.mod server/go.sum ./

# Download Go dependencies
RUN go mod download

# Copy the source code into the container
COPY server ./

# Build the Go application
RUN go build -o out/fleetd -v ./cmd/fleetd

FROM alpine:3.22.0 AS runner

# Install Nmap
RUN apk add --no-cache nmap

# Set the working directory
WORKDIR /app

# Copy the binary from builder and set permissions
COPY --from=builder /app/out/fleetd /app/fleetd
COPY --from=builder /app/static /app/static

RUN chmod 755 /app/fleetd

# Expose the port your application listens on
EXPOSE 8080

# Command to run when the container starts
CMD ["/app/fleetd"]
