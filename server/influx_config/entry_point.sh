#!/bin/sh 
# Start token management script, it will wait for InfluxDB to be healthy and then create or reuse a token.
/var/lib/influxdb3/start/manage_token.sh &
# Start InfluxDB server
influxdb3 serve
