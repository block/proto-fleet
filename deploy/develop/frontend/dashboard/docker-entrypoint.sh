#!/bin/bash
set -e

# Process templates
envsubst '${API_PROXY_URL}' < /etc/nginx/templates/nginx.conf.template > /etc/nginx/conf.d/default.conf

# Execute the CMD
exec "$@"
