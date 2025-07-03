# Build stage
FROM node:22.14.0-alpine AS builder

WORKDIR /app

# Copy package files and install dependencies
COPY client/package.json client/package-lock.json ./
RUN echo "Attempting npm install - will fail fast on certificate errors" && \
    (npm config set fetch-retry-mintimeout 1 && \
     npm config set fetch-retry-maxtimeout 2 && \
     npm config set fetch-retries 1 && \
     npm ci --no-fund) || \
    (echo "ERROR: npm ci failed due to certificate issues. Are you connected to VPN? Try disconnecting VPN." && \
     exit 1)

# Copy the rest of the application code
COPY client ./

# Build the application
RUN PROXY_URL=/api-proxy npm run build:protoFleet

# Production stage using Nginx
FROM nginx:1.28.0-alpine

# Copy the build output to replace the default nginx contents
COPY --from=builder /app/dist/protoFleet /usr/share/nginx/html

# Copy custom nginx config for streams support
COPY publish/docker-image/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
