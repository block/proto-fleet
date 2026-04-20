FROM golang:1.25.4-alpine AS builder

WORKDIR /app

# Install ca-certificates for SSL/TLS
RUN apk --no-cache add ca-certificates

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .

# Build with optimizations for smaller binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-extldflags "-static"' -o /app/fake-antminer

FROM alpine:latest

# Add security updates and create non-root user
RUN apk --no-cache add ca-certificates wget && \
    addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

WORKDIR /app

# Copy binary, entrypoint script and set ownership
COPY --from=builder /app/fake-antminer /app/fake-antminer
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh && \
    chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

EXPOSE 4028 80

# Add health check
HEALTHCHECK --interval=2s --timeout=2s --start-period=2s --retries=2 \
    CMD wget -qO- http://127.0.0.1:80/health || exit 1

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/fake-antminer"]
