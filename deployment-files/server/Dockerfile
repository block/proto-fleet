FROM alpine:3.22.0

# Install nmap and gcompat(for glibc compatibility)
RUN apk add --no-cache nmap gcompat

# Set the working directory
WORKDIR /app

# Copy the prebuilt binaries (server and plugins)
COPY fleetd-amd64 fleetd-arm64 /app/
COPY proto-plugin-amd64 proto-plugin-arm64 antminer-plugin-amd64 antminer-plugin-arm64 /app/plugins-temp/
COPY pyasic-plugin-amd64 pyasic-plugin-arm64 pyasic-config.yaml /app/plugins-temp/

ARG TARGETARCH=amd64

# Select the correct binaries based on architecture
RUN mkdir -p /app/plugins && \
    if [ "$TARGETARCH" = "amd64" ]; then \
      cp /app/fleetd-amd64 /app/fleetd && \
      cp /app/plugins-temp/proto-plugin-amd64 /app/plugins/proto-plugin && \
      cp /app/plugins-temp/antminer-plugin-amd64 /app/plugins/antminer-plugin && \
      cp /app/plugins-temp/pyasic-plugin-amd64 /app/plugins/pyasic-plugin; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
      cp /app/fleetd-arm64 /app/fleetd && \
      cp /app/plugins-temp/proto-plugin-arm64 /app/plugins/proto-plugin && \
      cp /app/plugins-temp/antminer-plugin-arm64 /app/plugins/antminer-plugin && \
      cp /app/plugins-temp/pyasic-plugin-arm64 /app/plugins/pyasic-plugin; \
    else \
      echo "Unsupported architecture: $TARGETARCH"; \
      exit 1; \
    fi && \
    cp /app/plugins-temp/pyasic-config.yaml /app/plugins/pyasic-config.yaml && \
    chmod 755 /app/fleetd /app/plugins/* && \
    rm -rf /app/fleetd-amd64 /app/fleetd-arm64 /app/plugins-temp

# Expose the port your application listens on
EXPOSE 4000

# Command to run when the container starts
CMD ["/app/fleetd"]
