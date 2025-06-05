## Influx_config
This directory provides the scripts used to initialize an InfluxDB 3-core container for the fleet server. It ensures the service is started and that the admin token is generated. Users can access the token through the generated `.env` file.

## Contents
### entry_point.sh
This script is the entry point for bootstrapping and running the InfluxDB 3-core container. It performs the following steps:

1. Starts the InfluxDB server in the background.  
2. Polls the HTTP API until the service is healthy.  
3. Calls `manage_token.sh` to create or refresh the admin token and write it to `.env`.  
4. Exports the token for downstream processes.  
5. Tails the InfluxDB log to keep the container alive.

### manage_token.sh

This script handles the creation and renewal of the InfluxDB admin token. It checks for an existing token in the `.env` file and, if absent or forced, generates a new one via the `influx` CLI and updates the file.

### .env
This file is generated automatically after the first `docker compose up`. It contains the admin token under the `INFLUXDB3_AUTH_TOKEN` variable, created by `manage_token.sh` for downstream services. Do not commit this file to version control.
