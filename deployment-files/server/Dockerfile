FROM alpine:3.22.0

# Install Nmap
RUN apk add --no-cache nmap

# Set the working directory
WORKDIR /app

# Copy the prebuilt binary and static assets
COPY fleetd /app/fleetd
COPY static /app/static

RUN chmod 755 /app/fleetd

# Expose the port your application listens on
EXPOSE 8080

# Command to run when the container starts
CMD ["/app/fleetd"]
