#!/usr/bin/env python3
"""Observe and validate the HA-07 writer-generation contract."""

from __future__ import annotations

import argparse
import base64
import json
import os
import socket
import sys
from typing import Any, Callable
from urllib import error, parse, request


DEFAULT_CLUSTER_PATH = "/service/proto-fleet-ha07"


def prefix_range_end(prefix: bytes) -> bytes:
    """Return etcd's exclusive range end for a byte prefix."""
    value = bytearray(prefix)
    for index in range(len(value) - 1, -1, -1):
        if value[index] < 0xFF:
            value[index] += 1
            return bytes(value[: index + 1])
    return b"\0"


def _decode(value: str) -> str:
    return base64.b64decode(value, validate=True).decode()


def extract_dcs_observation(snapshot: dict[str, Any], cluster_path: str) -> dict[str, Any]:
    """Extract one Patroni leader term from a single etcd prefix snapshot."""
    normalized_path = cluster_path.rstrip("/")
    leader_path = f"{normalized_path}/leader"
    nodes: dict[str, dict[str, Any]] = {}

    for node in snapshot.get("kvs", []):
        key = _decode(node["key"])
        if key in nodes:
            raise ValueError(f"duplicate DCS key in snapshot: {key}")
        nodes[key] = node

    leader_node = nodes.get(leader_path)
    if leader_node is None:
        raise ValueError(f"leader key is absent: {leader_path}")

    leader_name = _decode(leader_node["value"])
    if not leader_name:
        raise ValueError("leader key has an empty member name")

    try:
        writer_generation = int(leader_node["create_revision"])
        leader_lease_id = int(leader_node["lease"])
    except (KeyError, TypeError, ValueError) as exc:
        raise ValueError("leader create_revision or lease is missing or invalid") from exc
    if writer_generation <= 0:
        raise ValueError("leader create_revision must be positive")
    if leader_lease_id <= 0:
        raise ValueError("leader lease must be positive")

    member_path = f"{normalized_path}/members/{leader_name}"
    member_node = nodes.get(member_path)
    if member_node is None:
        raise ValueError(f"matching member key is absent: {member_path}")

    try:
        member = json.loads(_decode(member_node["value"]))
    except (json.JSONDecodeError, KeyError, TypeError, ValueError) as exc:
        raise ValueError(f"member key is not valid JSON: {member_path}") from exc
    if not isinstance(member, dict):
        raise ValueError(f"member key must contain an object: {member_path}")
    for required_field in ("api_url", "conn_url"):
        if not isinstance(member.get(required_field), str) or not member[required_field]:
            raise ValueError(f"member key is missing {required_field}: {member_path}")

    try:
        dcs_revision = int(snapshot["header"]["revision"])
        leader_mod_revision = int(leader_node["mod_revision"])
    except (KeyError, TypeError, ValueError) as exc:
        raise ValueError("DCS revision metadata is missing or invalid") from exc

    return {
        "dcs_revision": dcs_revision,
        "leader_mod_revision": leader_mod_revision,
        "leader_lease_id": leader_lease_id,
        "leader_name": leader_name,
        "member": member,
        "writer_generation": writer_generation,
    }


def read_linearizable_snapshot(
    endpoint: str, cluster_path: str, timeout_seconds: float
) -> dict[str, Any]:
    """Read all Patroni keys at one linearizable etcd revision."""
    prefix = f"{cluster_path.rstrip('/')}/".encode()
    payload = json.dumps(
        {
            "key": base64.b64encode(prefix).decode(),
            "range_end": base64.b64encode(prefix_range_end(prefix)).decode(),
            "serializable": False,
        }
    ).encode()
    range_request = request.Request(
        f"{endpoint.rstrip('/')}/v3/kv/range",
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with request.urlopen(range_request, timeout=timeout_seconds) as response:
            return json.load(response)
    except (error.URLError, TimeoutError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"linearizable DCS read failed via {endpoint}") from exc


def read_lease_ttl(
    endpoint: str, lease_id: int, timeout_seconds: float
) -> dict[str, int]:
    """Read the remaining lifetime of the Patroni leader lease."""
    payload = json.dumps({"ID": str(lease_id), "keys": False}).encode()
    ttl_request = request.Request(
        f"{endpoint.rstrip('/')}/v3/lease/timetolive",
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with request.urlopen(ttl_request, timeout=timeout_seconds) as response:
            lease = json.load(response)
        returned_lease_id = int(lease["ID"])
        ttl = int(lease["TTL"])
        granted_ttl = int(lease["grantedTTL"])
    except (
        error.URLError,
        TimeoutError,
        json.JSONDecodeError,
        KeyError,
        TypeError,
        ValueError,
    ) as exc:
        raise RuntimeError(f"leader lease TTL read failed via {endpoint}") from exc

    if returned_lease_id != lease_id:
        raise RuntimeError("leader lease TTL response returned a different lease")
    if ttl <= 0 or granted_ttl <= 0:
        raise RuntimeError("leader lease is expired or has an invalid TTL")
    return {"granted_ttl": granted_ttl, "ttl": ttl}


def writer_address_matches(
    server_address: str,
    server_port: int,
    member_conn_url: str,
    *,
    resolver: Callable[..., list[Any]] = socket.getaddrinfo,
) -> bool:
    """Return whether Patroni's leader member resolves to the SQL server."""
    member_url = parse.urlparse(member_conn_url)
    if not member_url.hostname:
        raise ValueError("leader member conn_url has no hostname")
    member_port = member_url.port or 5432
    if member_port != server_port:
        return False

    resolved_addresses = {
        result[4][0]
        for result in resolver(member_url.hostname, member_port, type=socket.SOCK_STREAM)
    }
    return server_address in resolved_addresses


def query_writable_postgres(dsn: str) -> dict[str, Any]:
    """Connect through the multi-host DSN and identify its writable server."""
    try:
        import psycopg2
    except ImportError as exc:  # pragma: no cover - exercised in the fixture image
        raise RuntimeError("psycopg2 is required inside the fixture image") from exc

    with psycopg2.connect(dsn) as connection:
        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT
                    host(inet_server_addr()),
                    inet_server_port(),
                    pg_is_in_recovery(),
                    (pg_control_checkpoint()).timeline_id
                """
            )
            row = cursor.fetchone()

    if row is None:
        raise RuntimeError("writable Postgres identity query returned no row")
    server_address, server_port, in_recovery, timeline = row
    if in_recovery:
        raise RuntimeError("multi-host DSN selected a Postgres replica")
    return {
        "server_address": str(server_address),
        "server_port": int(server_port),
        "timeline": int(timeline),
    }


def query_patroni(member_api_url: str, timeout_seconds: float) -> dict[str, Any]:
    """Verify the DCS leader's REST identity still owns the primary lock."""
    patroni_url = member_api_url.rstrip("/")
    if not patroni_url.endswith("/patroni"):
        patroni_url = f"{patroni_url}/patroni"
    primary_url = f"{patroni_url.removesuffix('/patroni')}/primary"

    try:
        with request.urlopen(primary_url, timeout=timeout_seconds) as response:
            if response.status != 200:
                raise RuntimeError(f"Patroni primary check returned {response.status}")
            state = json.load(response)
    except (error.URLError, TimeoutError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"Patroni leader validation failed via {patroni_url}") from exc

    if state.get("role") not in ("primary", "master"):
        raise RuntimeError(f"DCS leader reports non-primary role: {state.get('role')}")
    try:
        timeline = int(state["timeline"])
    except (KeyError, TypeError, ValueError) as exc:
        raise RuntimeError("Patroni leader response has no valid timeline") from exc
    return {"role": state["role"], "timeline": timeline}


def observe(
    *,
    dcs_endpoint: str,
    db_dsn: str,
    cluster_path: str,
    timeout_seconds: float,
) -> dict[str, Any]:
    """Build a fail-closed writer-generation observation."""
    snapshot = read_linearizable_snapshot(dcs_endpoint, cluster_path, timeout_seconds)
    dcs = extract_dcs_observation(snapshot, cluster_path)
    writer = query_writable_postgres(db_dsn)
    member = dcs["member"]
    patroni = query_patroni(member["api_url"], timeout_seconds)

    address_matches = writer_address_matches(
        writer["server_address"], writer["server_port"], member["conn_url"]
    )
    timeline_matches = writer["timeline"] == patroni["timeline"]
    if not address_matches:
        raise RuntimeError(
            "DCS leader member does not match the writable Postgres server address: "
            f"SQL selected {writer['server_address']}:{writer['server_port']}, "
            f"DCS advertised {member['conn_url']}"
        )
    if not timeline_matches:
        raise RuntimeError(
            "DCS leader Patroni timeline does not match writable Postgres timeline"
        )

    verified_snapshot = read_linearizable_snapshot(
        dcs_endpoint, cluster_path, timeout_seconds
    )
    verified_dcs = extract_dcs_observation(verified_snapshot, cluster_path)
    if (
        verified_dcs["leader_name"] != dcs["leader_name"]
        or verified_dcs["writer_generation"] != dcs["writer_generation"]
    ):
        raise RuntimeError(
            "DCS leader term changed during writer validation: "
            f"started with {dcs['leader_name']}@{dcs['writer_generation']}, "
            "finished with "
            f"{verified_dcs['leader_name']}@{verified_dcs['writer_generation']}"
        )
    dcs = verified_dcs
    lease_ttl = read_lease_ttl(
        dcs_endpoint, dcs["leader_lease_id"], timeout_seconds
    )

    return {
        "binding": {
            "address_matches": address_matches,
            "patroni_primary": True,
            "timeline_matches": timeline_matches,
        },
        "contract_version": 1,
        "dcs": {
            "cluster_path": cluster_path.rstrip("/"),
            "leader_create_revision": dcs["writer_generation"],
            "leader_lease_granted_ttl": lease_ttl["granted_ttl"],
            "leader_lease_id": dcs["leader_lease_id"],
            "leader_lease_ttl": lease_ttl["ttl"],
            "leader_mod_revision": dcs["leader_mod_revision"],
            "read_consistency": "linearizable",
            "revision": dcs["dcs_revision"],
        },
        "writer": {
            "name": dcs["leader_name"],
            **writer,
        },
        "writer_generation": dcs["writer_generation"],
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Observe the HA-07 Patroni writer-generation contract"
    )
    parser.add_argument(
        "--dcs-endpoint",
        default=os.environ.get("HA_GENERATION_DCS_ENDPOINT", "http://etcd-a:2379"),
    )
    parser.add_argument(
        "--db-dsn",
        default=os.environ.get("HA_GENERATION_DB_DSN"),
        required=os.environ.get("HA_GENERATION_DB_DSN") is None,
    )
    parser.add_argument(
        "--cluster-path",
        default=os.environ.get("HA_GENERATION_CLUSTER_PATH", DEFAULT_CLUSTER_PATH),
    )
    parser.add_argument(
        "--timeout-seconds",
        type=float,
        default=float(os.environ.get("HA_GENERATION_TIMEOUT_SECONDS", "3")),
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        observation = observe(
            dcs_endpoint=args.dcs_endpoint,
            db_dsn=args.db_dsn,
            cluster_path=args.cluster_path,
            timeout_seconds=args.timeout_seconds,
        )
    except Exception as exc:
        print(f"writer-generation observation failed: {exc}", file=sys.stderr)
        return 1

    json.dump(observation, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
