# MQTT Curtailment Simulator

This devtool runs two local Mosquitto brokers and a small browser UI for
publishing MaestroOS-compatible MQTT curtailment targets.

The Docker Compose ports are bound to host loopback only. The brokers are still
reachable from other Docker Compose services on `fleet-network` through their
static container IPs, but they are not exposed to other machines on the LAN by
default.

From the repository root:

```sh
just mqtt-sim-up
```

Then open:

```text
http://localhost:4183
```

Use this source configuration when `fleet-api` is running in Docker Compose:

| Field | Value |
| --- | --- |
| Primary broker host | `192.168.2.240` |
| Secondary broker host | `192.168.2.241` |
| Broker port | `1883` |
| Broker transport | `tcp` |
| Topic | `maestro/target` |
| Payload format | `target_timestamp` |
| MQTT username | `proto-fleet` |
| MQTT password | `proto-fleet` |

The local Mosquitto brokers allow anonymous connections. Proto Fleet source
settings still require non-empty credentials, so the simulator UI and this table
use stable development-only placeholder values.

The simulator header links to the Proto Fleet curtailment settings page. Override
the destination without editing code when your frontend runs somewhere else:

```sh
PROTO_FLEET_BASE_URL=http://localhost:5174 just mqtt-sim-up
```

The simulator publishes this payload shape:

```json
{"target":100,"timestamp":1778538975}
```

`target: 100` means ON/full power. `target: 0` means OFF/curtail. The loop mode
publishes the selected target every 30 seconds by default to match the MaestroOS
MQTT publish contract.

Useful commands:

```sh
just mqtt-sim-up
just mqtt-sim-logs
just mqtt-sim-down
```
