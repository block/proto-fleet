# Production stage using Nginx
FROM nginx:1.29.0-alpine

# Copy the prebuilt client files
COPY protoFleet /usr/share/nginx/html

# Copy custom nginx config for SSL and streams support
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Create directory for SSL certificates (mounted at runtime)
RUN mkdir -p /etc/nginx/ssl

EXPOSE 80 443

CMD ["nginx", "-g", "daemon off;"]
