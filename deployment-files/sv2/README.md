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

### Listener bind address

The compose service publishes the downstream listener on
`${STRATUM_V2_PROXY_DOWNSTREAM_HOST:-0.0.0.0}:${STRATUM_V2_PROXY_DOWNSTREAM_PORT:-34255}`.
The default `0.0.0.0` exposes the unauthenticated Stratum listener on
every interface; on a multi-homed or internet-facing host, scope it to
a private/LAN IP by setting `STRATUM_V2_PROXY_DOWNSTREAM_HOST` in
`.env` (e.g. `STRATUM_V2_PROXY_DOWNSTREAM_HOST=192.0.2.10`). The host
portion of `STRATUM_V2_PROXY_MINER_URL` is what Fleet pushes to miners
and is independent of this binding — operators are responsible for
keeping the two consistent.

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
API logs as `sv2 proxy health: up/down`.

Pool-assignment preflight consults the probe and **fails closed** for
proxied routes when the bundled translator is down or has not yet
responded to its first probe. Concretely: assignments that would push
the miner-facing proxy URL to an SV1-only miner are rejected with
`SLOT_WARNING_SV2_NOT_SUPPORTED` while the proxy is unhealthy, so a
healthy fleet doesn't get knocked off-pool by an outage on the
translator container. Native-SV2 assignments are unaffected.

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
4. SV2 `ValidatePool` does a Noise NX handshake when the operator
   supplies the pool's authority public key (32 raw bytes); otherwise it
   falls back to a plain TCP dial. The handshake mode reports
   `Reachable=true, CredentialsVerified=false, Mode=SV2_HANDSHAKE` —
   credentials are unverified because no SV2 channel is opened.
