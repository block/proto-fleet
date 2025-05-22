#!/bin/bash
set -e

# Check if the API_PROXY_URL environment variable is set
if [ -z "$API_PROXY_URL" ]; then
    echo "Error: API_PROXY_URL environment variable is not set. Exiting."
    exit 1
fi

# Process templates
envsubst '${API_PROXY_URL}' < /usr/local/apache2/conf/templates/httpd.conf.template > /usr/local/apache2/conf/httpd.conf

# Make sure Apache can read the SSL certificates
chmod 644 /usr/local/apache2/ssl/server.crt
chmod 600 /usr/local/apache2/ssl/server.key

# Execute the CMD
exec "$@"
