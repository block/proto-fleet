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

FROM nginx:1.27.5-alpine

WORKDIR /app

# Install envsubst if not already present
RUN apk add --no-cache bash gettext

# Remove default nginx configuration
RUN rm /etc/nginx/conf.d/default.conf

# Create templates directory
RUN mkdir -p /etc/nginx/templates/

# Copy the template file
COPY deploy/develop/frontend/dashboard/nginx.conf.template /etc/nginx/templates/

# Copy startup script and explicitly set permissions
COPY deploy/develop/frontend/dashboard/docker-entrypoint.sh /app/
RUN chmod +x /app/docker-entrypoint.sh

COPY --from=build /build/dist/protoFleet /usr/share/nginx/html

EXPOSE 80

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
