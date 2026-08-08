# Production stage using Nginx
FROM nginx:1.29.0-alpine@sha256:d67ea0d64d518b1bb04acde3b00f722ac3e9764b3209a9b0a98924ba35e4b779

# Copy the prebuilt client files
COPY protoFleet /usr/share/nginx/html

# Copy custom nginx config for SSL and streams support
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Render window.__RUNTIME_CONFIG__ into config.js from environment on container
# start (nginx runs /docker-entrypoint.d/*.sh before starting). Lets an operator
# enable observability via env vars without rebuilding the client.
COPY docker-entrypoint.d/40-render-runtime-config.sh /docker-entrypoint.d/40-render-runtime-config.sh
RUN chmod +x /docker-entrypoint.d/40-render-runtime-config.sh

# Create directory for SSL certificates (mounted at runtime)
RUN mkdir -p /etc/nginx/ssl

EXPOSE 80 443

CMD ["nginx", "-g", "daemon off;"]
