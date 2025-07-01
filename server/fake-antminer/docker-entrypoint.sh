#!/bin/sh
set -e

HOSTNAME=$(hostname)

if [ -z "$SERIAL_NUMBER" ]; then
  export SERIAL_NUMBER="fake-antminer-$HOSTNAME"
  echo "Setting SERIAL_NUMBER to $SERIAL_NUMBER"
fi

if [ -z "$MAC_ADDRESS" ]; then
  # Generate a unique MAC based on hostname (container ID)
  # Use hostname hash to generate last 4 bytes of MAC
  HOST_HASH=$(echo "$HOSTNAME" | md5sum | head -c 8)
  # Use a locally administered MAC address (starts with 02)
  export MAC_ADDRESS="02:42:$(echo $HOST_HASH | sed 's/\(..\)/\1:/g' | sed 's/:$//')"
  echo "Setting MAC_ADDRESS to $MAC_ADDRESS"
fi

exec "$@"
