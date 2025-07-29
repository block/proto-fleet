FROM alpine:3.22.0

# Install nmap and gcompat(for glibc compatibility)
RUN apk add --no-cache nmap gcompat

# Set the working directory
WORKDIR /app

# Copy the prebuilt binary
COPY fleetd-amd64 fleetd-arm64 /app/

ARG TARGETARCH=amd64

RUN if [ "$TARGETARCH" = "amd64" ]; then \
      cp /app/fleetd-amd64 /app/fleetd; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
      cp /app/fleetd-arm64 /app/fleetd; \
    else \
      echo "Unsupported architecture: $TARGETARCH"; \
      exit 1; \
    fi && \
    chmod 755 /app/fleetd && \
    rm /app/fleetd-amd64 /app/fleetd-arm64

# Expose the port your application listens on
EXPOSE 4000

# Command to run when the container starts
CMD ["/app/fleetd"]
