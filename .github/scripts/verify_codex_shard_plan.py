#!/usr/bin/env python3
"""Verify a downloaded trusted shard plan and remeasure its exact packet."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
from typing import Any


def validate_manifest_digest(manifest: dict[str, Any]) -> None:
    expected = manifest.get("manifest_digest")
    unsigned = dict(manifest)
    unsigned.pop("manifest_digest", None)
    actual = hashlib.sha256(
        json.dumps(unsigned, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()
    if expected != actual:
        raise ValueError("shard manifest digest is invalid")


def verify_plan(manifest: dict[str, Any], shard_id: str, packet: bytes) -> None:
    validate_manifest_digest(manifest)
    shards = manifest.get("shards", [])
    shard = next((item for item in shards if item.get("id") == shard_id), None)
    if shard is None:
        raise ValueError(f"manifest has no shard {shard_id}")
    if manifest.get("limits", {}).get("max_packets") != 2 or len(shards) != 2:
        raise ValueError("manifest does not enforce the review-wide two-packet cap")

    actual_bytes = len(packet)
    actual_lines = len(packet.splitlines())
    if manifest.get("status") == "planned" and shard.get("active"):
        if actual_bytes != shard.get("packet_bytes") or actual_lines != shard.get(
            "packet_lines"
        ):
            raise ValueError(
                "downloaded shard packet metrics disagree with the manifest"
            )
        if actual_bytes > manifest["limits"]["max_packet_bytes"]:
            raise ValueError("downloaded shard packet exceeds the byte limit")
        if actual_lines > manifest["limits"]["max_packet_lines"]:
            raise ValueError("downloaded shard packet exceeds the line limit")
    elif packet:
        raise ValueError("inactive or oversized shard unexpectedly contains a packet")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--packet", type=Path, required=True)
    parser.add_argument("--shard", choices=("shard-1", "shard-2"), required=True)
    args = parser.parse_args()

    manifest = json.loads(args.manifest.read_text(encoding="utf-8"))
    verify_plan(manifest, args.shard, args.packet.read_bytes())


if __name__ == "__main__":
    main()
