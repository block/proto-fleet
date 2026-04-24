# Stratum V2 translator proxy

The bundled SRI translator proxy lets SV1-only miners mine SV2 pools
without running a separate piece of infrastructure. Fleet's URL
rewriter swaps the pool's SV2 URL for the proxy's LAN-facing URL at pool
assignment time so operators never need to keep their pool records in
sync with proxy configuration.

## Starting / stopping

The proxy runs as a profile-gated Docker Compose service. `docker compose
up` without the profile leaves it untouched; enable with either:

```
COMPOSE_PROFILES=sv2 docker compose up -d
# or
docker compose --profile sv2 up -d
```

Stop with:

```
docker compose --profile sv2 stop sv2-tproxy
```

## Configuration

Configuration lives in `tproxy.toml` (this directory) and is mounted
read-only into the container. The installer (`../install.sh`) renders
this file from your answers; operators who skipped the prompt can edit
directly and restart the container.

Changing any field requires a container restart — SRI does not hot-reload:

```
docker compose --profile sv2 restart sv2-tproxy
```

Fleet does not consult the TOML at runtime. It reads a small subset of
the same values from environment variables (`STRATUM_V2_PROXY_*`) so the
rewriter knows the LAN-facing URL to push to SV1 miners.

## Version pinning

The Compose service pins a specific `ghcr.io/stratum-mining/translator`
tag; upgrading SRI is a deliberate operation (pull the new tag, smoke
test, release). The current pin is set in `server/docker-compose.base.yaml`.

## Fleet integration

The Fleet API runs a background TCP probe against the proxy's health
address when `STRATUM_V2_PROXY_ENABLED=true`. Transitions surface in the
API logs as `sv2 proxy health: up/down`. Pool-assignment decisions do
NOT depend on the probe — they consult the static `ProxyEnabled` flag
only, so a flapping proxy does not flip commit-time routing. The probe
is purely informational.

## Air-gapped mirrors

If your deployment cannot reach ghcr.io, pull the image on a connected
host, re-tag it for your registry, push, and update the `image:` field
in `server/docker-compose.base.yaml`. The same pattern applies to
`proto-fleet-timescaledb`.

## Known limitations (v1)

See `docs/stratum-v2-plan.md` "Known limitations" for the full list.
The most important ones from an operator's perspective:

1. One upstream pool per proxy instance. Multi-pool fleets need either
   native-SV2 miners or a second proxy instance running out-of-band.
2. No runtime reconfiguration — restart after edits.
3. No Fleet-side ingestion of per-miner SV2 stats yet.
4. SV2 `ValidatePool` only TCP-dials; it does not speak the Noise
   handshake. A handshake probe is a v1.5 fast-follow.
