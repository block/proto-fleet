# Production stage using Nginx
FROM nginx:1.29.0-alpine

# Copy the prebuilt client files
COPY protoFleet /usr/share/nginx/html

# Copy custom nginx config for streams support
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
