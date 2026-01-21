## Influx_config
This directory provides the scripts used to initialize an InfluxDB 3-core container for the fleet server. It ensures the service is started and that the admin token is generated. Users can access the token through the generated `.env` file.

## Contents

### entry_point.sh

This script is the entry point for bootstrapping and running the InfluxDB 3-core container. It runs initialization scripts in the background while starting the InfluxDB server.

### manage_token.sh

Handles the creation of the InfluxDB admin token. Waits for InfluxDB to be healthy, creates or reuses an existing token, and writes it to the `.env` file for downstream scripts.

### init_database.sh

Creates the database and default measurement tables (power_w, hashrate_mhs, temperature_c, efficiency_jth, device_metrics) if they don't exist.

### init_last_cache.sh

Creates a Last Value Cache (LVC) for the `device_metrics` table to enable fast latest-value queries. Caches all value columns automatically, so schema changes are reflected without reconfiguration.

### .env

This file is generated automatically after the first `docker compose up`. It contains the admin token under the `INFLUXDB3_AUTH_TOKEN` variable. Do not commit this file to version control.
