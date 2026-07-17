# Modbus TCP Simulator

This development-only listener provides a small, stateful Modbus TCP target for
local Proto Fleet testing. It is not a PLC emulator and must not be deployed to
production.

The simulator supports:

- FC5 Write Single Coil. `0xFF00` records ON and `0x0000` records OFF.
- FC6 Write Single Register. Any 16-bit register value is recorded.
- Protocol-compliant request echoes after successful writes.
- Modbus exception responses for unsupported functions, invalid lengths,
  invalid protocol IDs, and invalid FC5 values.

State is held only in memory. Successful writes are logged with the
simulator-local unit ID, wire address, and value.

## Start the Docker simulator

From the repository root:

```sh
just modbus-sim-up
```

This opt-in command starts the simulator and recreates `fleet-api` with the
overlay's deployment allowlist. It does not change the default `just dev`
startup.

| Client | Endpoint |
| --- | --- |
| `fleet-api` on `fleet-network` | `192.168.2.242:5502` |
| Host-only tools | `127.0.0.1:5502` |

The host port is deliberately bound to loopback. Useful commands:

```sh
just modbus-sim-up
just modbus-sim-rebuild
just modbus-sim-logs
just modbus-sim-down
```

`modbus-sim-down` also recreates `fleet-api` without the simulator overlay so
the development control-subnet allowlist does not remain active after the
simulator is removed.

The binary listens on `:5502` by default. For a native run, override the
address with `MODBUS_SIM_LISTEN_ADDRESS`:

```sh
cd server
MODBUS_SIM_LISTEN_ADDRESS=127.0.0.1:15502 go run ./devtools/modbussim
```

## Commission a site

Starting the container does not commission or modify any database site. Both
of the independent `fleet-api` allowlists must authorize an endpoint before a
real write is sent:

1. The Compose overlay sets the deployment-global
   `INFRASTRUCTURE_OT_CONTROL_SUBNETS=192.168.2.242/32`.
2. An ADMIN or SUPER_ADMIN with org-wide `site:manage` must explicitly
   commission the same CIDR on the target site. A site-scoped grant is
   insufficient.

The generated Fleet CLI exposes the session-only commissioning RPC:

```sh
printf '%s\n' "${FLEET_PASSWORD}" | fleetcli \
  --server http://localhost:4000 \
  --username "${FLEET_USERNAME}" \
  --password-stdin \
  sites set-infrastructure-control-subnets \
  --site-id 42 \
  --infrastructure-control-subnets 192.168.2.242/32
```

Replace `42` with the local site's ID. Omit every
`--infrastructure-control-subnets` flag to decommission the site. API keys
cannot call this session-only admin RPC.

## Configure an infrastructure device

Use `modbus_tcp`, not the log-only `sim` test adapter. For an FC5 coil at
application address 1:

```json
{
  "endpoint": "192.168.2.242",
  "port": 5502,
  "unit_id": 1,
  "register_address": 1,
  "write_mode": "coil"
}
```

Proto Fleet translates application address 1 to wire address 0. To exercise
FC6 instead, set `"write_mode": "holding_register"` and choose the desired
application register address. ON writes `1`; OFF writes `0`.

## Security

Modbus TCP has no authentication or transport security. The loopback-only host
port limits host exposure, but other services on `fleet-network` can reach the
simulator. Proto Fleet still enforces both the deployment-global and per-site
positive allowlists at write time.

Production Modbus endpoints require firewall rules, network segmentation, and
site-specific routing in addition to the application allowlists. Never expose
this simulator or a real unauthenticated Modbus listener to an untrusted
network.
