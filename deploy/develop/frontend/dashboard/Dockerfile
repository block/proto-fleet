FROM debian:12.10 AS build

WORKDIR /build

SHELL ["/bin/bash", "-c"]

RUN apt-get update && apt-get install -y \
    git \
    curl

COPY bin bin/

RUN chmod +x bin/activate-hermit

COPY client/package*.json ./

RUN source bin/activate-hermit && npm clean-install

COPY client ./

RUN source bin/activate-hermit && npm run build:protoFleet

FROM httpd:2.4-alpine

WORKDIR /app

# Install additional packages
RUN apk add --no-cache \
    bash \
    gettext \
    openssl 

# Generate self-signed certificate for HTTP/2 (requires SSL)
RUN mkdir -p /usr/local/apache2/ssl && \
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
    -keyout /usr/local/apache2/ssl/server.key \
    -out /usr/local/apache2/ssl/server.crt \
    -subj "/C=US/ST=State/L=City/O=Organization/CN=localhost"

# Remove default httpd configuration
RUN rm -f /usr/local/apache2/conf/httpd.conf

# Create templates directory
RUN mkdir -p /usr/local/apache2/conf/templates/

# Copy the template file
COPY deploy/develop/frontend/dashboard/httpd.conf.template /usr/local/apache2/conf/templates/

# Copy startup script and explicitly set permissions
COPY deploy/develop/frontend/dashboard/docker-entrypoint.sh /app/
RUN chmod +x /app/docker-entrypoint.sh

# Copy website files to Apache's document root
COPY --from=build /build/dist/protoFleet /usr/local/apache2/htdocs/

# Expose both HTTP and HTTPS ports
EXPOSE 80 443

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["httpd-foreground"]
