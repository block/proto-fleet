#!/usr/bin/env python3
"""Plan at most two architecture-aware Codex review shards for an exact diff."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Any

MAX_PACKET_BYTES = 500_000
MAX_PACKET_LINES = 12_500
SHARD_IDS = ("shard-1", "shard-2")


@dataclass(frozen=True)
class FileDiff:
    path: str
    domain: str
    unit: str
    shared: bool
    patch: bytes

    @property
    def bytes(self) -> int:
        return len(self.patch)

    @property
    def lines(self) -> int:
        return len(self.patch.splitlines())


@dataclass(frozen=True)
class Unit:
    key: str
    files: tuple[FileDiff, ...]

    @property
    def bytes(self) -> int:
        return sum(file.bytes for file in self.files)

    @property
    def lines(self) -> int:
        return sum(file.lines for file in self.files)


def is_generated(path: str) -> bool:
    name = PurePosixPath(path).name
    return (
        "/generated/" in f"/{path}"
        or name.endswith((".pb.go", ".pb.ts", ".pb.py"))
        or path == "client/src/protoOS/api/generatedApi.ts"
    )


def is_delivery_path(path: str) -> bool:
    pure = PurePosixPath(path)
    name = pure.name.lower()
    return (
        path.startswith(
            (".github/", "deployment-files/", "docs/", "scripts/", "server/monitoring/")
        )
        or name.startswith(("dockerfile", "docker-compose", "compose."))
        or "/" not in path
    )


def classify_path(path: str) -> str:
    if is_delivery_path(path):
        return "delivery"
    if path.startswith(
        ("proto/", "server/migrations/", "server/sqlc/", "server/generated/sqlc/")
    ) or is_generated(path):
        return "contracts"
    if path.startswith("client/src/protoFleet/"):
        return "protofleet"
    if path.startswith("client/src/protoOS/"):
        return "protoos"
    if path.startswith("client/"):
        return "client-shared"
    if path.startswith("plugin/asicrs/"):
        return "asicrs"
    if path.startswith(("plugin/", "packages/proto-python-gen/")):
        return "plugins"
    if path.startswith("server/"):
        return "server"
    return "cross-cutting"


def is_shared_contract(path: str, domain: str) -> bool:
    if path.startswith("proto/") and not is_generated(path):
        return True
    if path.startswith(("server/migrations/", "server/sqlc/queries/")):
        return True
    if domain == "client-shared":
        return True
    return path in {
        "go.work",
        "go.work.sum",
        "package.json",
        "package-lock.json",
        "justfile",
    }


def semantic_unit(path: str, domain: str, shared: bool) -> str:
    parts = PurePosixPath(path).parts
    prefix = "shared" if shared else "primary"
    if domain in {"protofleet", "protoos"} and "features" in parts:
        index = parts.index("features")
        root = parts[: min(len(parts), index + 2)]
    elif domain in {"plugins", "asicrs"}:
        root = parts[: min(len(parts), 2)]
    elif domain == "delivery":
        if path.startswith("server/monitoring/"):
            root = parts[: min(len(parts), 3)]
        elif parts and parts[0] in {".github", "deployment-files", "docs", "scripts"}:
            root = parts[: min(len(parts), 2)]
        else:
            root = parts[:1]
    else:
        root = parts[:-1] or parts
    return f"{prefix}:{domain}:{'/'.join(root)}"


def group_units(files: list[FileDiff]) -> list[Unit]:
    grouped: dict[str, list[FileDiff]] = {}
    for file in files:
        grouped.setdefault(file.unit, []).append(file)
    return [
        Unit(key, tuple(sorted(group, key=lambda item: item.path)))
        for key, group in sorted(grouped.items())
    ]


def _ordered_units(units: list[Unit]) -> list[Unit]:
    return sorted(units, key=lambda item: (-item.bytes, -item.lines, item.key))


def _is_client_app_unit(unit: Unit) -> bool:
    return any(file.domain in {"protofleet", "protoos"} for file in unit.files)


def shared_context_for_units(
    shared_files: list[FileDiff], units: list[Unit]
) -> list[FileDiff]:
    if not units:
        return []
    has_client_app = any(_is_client_app_unit(unit) for unit in units)
    return [
        file
        for file in shared_files
        if file.domain != "client-shared" or has_client_app
    ]


def find_bounded_assignment(
    units: list[Unit], shared_files: list[FileDiff]
) -> list[list[Unit]] | None:
    ordered = _ordered_units(units)
    shared_bytes = sum(file.bytes for file in shared_files)
    shared_lines = sum(file.lines for file in shared_files)
    if (
        sum(unit.bytes for unit in ordered) + shared_bytes > 2 * MAX_PACKET_BYTES
        or sum(unit.lines for unit in ordered) + shared_lines > 2 * MAX_PACKET_LINES
    ):
        return None

    global_context = [file for file in shared_files if file.domain != "client-shared"]
    client_context = [file for file in shared_files if file.domain == "client-shared"]
    global_bytes = sum(file.bytes for file in global_context)
    global_lines = sum(file.lines for file in global_context)
    client_bytes = sum(file.bytes for file in client_context)
    client_lines = sum(file.lines for file in client_context)
    failed: set[tuple[int, int, int, int, int, bool, bool]] = set()

    def search(
        position: int,
        bytes_0: int,
        lines_0: int,
        bytes_1: int,
        lines_1: int,
        shard_1_active: bool,
        shard_1_has_client_app: bool,
    ) -> tuple[int, ...] | None:
        if position == len(ordered):
            return ()
        state = (
            position,
            bytes_0,
            lines_0,
            bytes_1,
            lines_1,
            shard_1_active,
            shard_1_has_client_app,
        )
        if state in failed:
            return None
        unit = ordered[position]
        loads = ((bytes_0, lines_0), (bytes_1, lines_1))
        candidates = sorted(range(2), key=lambda index: (*loads[index], index))
        for index in candidates:
            current_bytes, current_lines = loads[index]
            extra_bytes = unit.bytes
            extra_lines = unit.lines
            next_active = shard_1_active
            next_has_client_app = shard_1_has_client_app
            if index == 1:
                if not shard_1_active:
                    extra_bytes += global_bytes
                    extra_lines += global_lines
                    next_active = True
                if _is_client_app_unit(unit) and not shard_1_has_client_app:
                    extra_bytes += client_bytes
                    extra_lines += client_lines
                    next_has_client_app = True
            if (
                current_bytes + extra_bytes > MAX_PACKET_BYTES
                or current_lines + extra_lines > MAX_PACKET_LINES
            ):
                continue
            next_loads = [list(loads[0]), list(loads[1])]
            next_loads[index][0] += extra_bytes
            next_loads[index][1] += extra_lines
            suffix = search(
                position + 1,
                next_loads[0][0],
                next_loads[0][1],
                next_loads[1][0],
                next_loads[1][1],
                next_active,
                next_has_client_app,
            )
            if suffix is not None:
                return (index, *suffix)
        failed.add(state)
        return None

    assignment = search(0, shared_bytes, shared_lines, 0, 0, False, False)
    if assignment is None:
        return None
    bins: list[list[Unit]] = [[], []]
    for unit, index in zip(ordered, assignment, strict=True):
        bins[index].append(unit)
    return bins


def _assign_without_limits(units: list[Unit]) -> list[list[Unit]]:
    bins: list[list[Unit]] = [[], []]
    loads = [[0, 0], [0, 0]]
    for unit in _ordered_units(units):
        index = min(
            range(2),
            key=lambda candidate: (loads[candidate][0], loads[candidate][1], candidate),
        )
        bins[index].append(unit)
        loads[index][0] += unit.bytes
        loads[index][1] += unit.lines
    return bins


def plan_files(files: list[FileDiff]) -> dict[str, Any]:
    shared_files = sorted(
        (file for file in files if file.shared), key=lambda item: item.path
    )
    primary_units = [unit for unit in group_units(files) if not unit.files[0].shared]
    shared_bytes = sum(file.bytes for file in shared_files)
    shared_lines = sum(file.lines for file in shared_files)
    reasons: list[str] = []

    if shared_bytes > MAX_PACKET_BYTES or shared_lines > MAX_PACKET_LINES:
        reasons.append("replicated shared context exceeds a packet limit")

    bins = find_bounded_assignment(primary_units, shared_files) if not reasons else None
    if bins is None:
        reasons.append("semantic units do not fit in two bounded review-wide packets")
        bins = _assign_without_limits(primary_units)

    status = "planned" if not reasons else "oversized"

    # Shared files have one primary owner. They are context in shard 2 only when
    # the second review-wide packet has primary work.
    primary_by_shard: list[list[FileDiff]] = [list(shared_files), []]
    for index, units in enumerate(bins):
        for unit in units:
            primary_by_shard[index].extend(unit.files)
    active = [bool(primary_by_shard[0]), bool(primary_by_shard[1])]
    context_by_shard = [[], shared_context_for_units(shared_files, bins[1])]

    owners: dict[str, str] = {}
    for index, owned in enumerate(primary_by_shard):
        for file in owned:
            if file.path in owners:
                raise ValueError(
                    f"changed file has multiple primary owners: {file.path}"
                )
            owners[file.path] = SHARD_IDS[index]
    missing = sorted({file.path for file in files} - set(owners))
    if missing:
        raise ValueError(f"changed files have no primary owner: {missing}")

    shard_records = []
    for index, shard_id in enumerate(SHARD_IDS):
        primary = sorted(primary_by_shard[index], key=lambda item: item.path)
        context = sorted(context_by_shard[index], key=lambda item: item.path)
        packet_files = sorted(
            {file.path: file for file in primary + context}.values(),
            key=lambda item: item.path,
        )
        shard_records.append(
            {
                "id": shard_id,
                "active": active[index],
                "primary_files": [file.path for file in primary],
                "shared_files": [file.path for file in context],
                "packet_bytes": sum(file.bytes for file in packet_files),
                "packet_lines": sum(file.lines for file in packet_files),
                "packet_files": len(packet_files),
            }
        )

    file_records = [
        {
            "path": file.path,
            "domain": file.domain,
            "semantic_unit": file.unit,
            "shared_contract": file.shared,
            "primary_shard": owners[file.path],
            "diff_bytes": file.bytes,
            "diff_lines": file.lines,
        }
        for file in sorted(files, key=lambda item: item.path)
    ]
    return {
        "status": status,
        "oversized_reasons": reasons,
        "limits": {
            "max_packets": 2,
            "max_packet_bytes": MAX_PACKET_BYTES,
            "max_packet_lines": MAX_PACKET_LINES,
        },
        "files": file_records,
        "shards": shard_records,
    }


def git(*args: str) -> bytes:
    return subprocess.check_output(("git", *args))


def changed_file_patches(
    commit_range: str, unified: int, inter_hunk: int
) -> list[tuple[str, bytes]]:
    raw_status = git("diff", "--name-status", "-z", "--find-renames", commit_range)
    tokens = [token for token in raw_status.split(b"\0") if token]
    paths: list[str] = []
    index = 0
    while index < len(tokens):
        status = tokens[index].decode("ascii")
        index += 1
        if status.startswith(("R", "C")):
            if index + 1 >= len(tokens):
                raise ValueError("rename/copy status is missing a path pair")
            # The new path owns a rename or copy, while the full patch below
            # retains both sides of the pair.
            index += 1
            path = tokens[index]
            index += 1
        else:
            if index >= len(tokens):
                raise ValueError("change status is missing a path")
            path = tokens[index]
            index += 1
        paths.append(path.decode("utf-8", "surrogateescape"))

    args = ["diff", "--find-renames", "--submodule=diff", f"--unified={unified}"]
    if inter_hunk:
        args.append(f"--inter-hunk-context={inter_hunk}")
    args.append(commit_range)
    full_patch = git(*args)
    starts = [match.start() for match in re.finditer(rb"(?m)^diff --git ", full_patch)]
    sections = [
        full_patch[
            start : starts[position + 1]
            if position + 1 < len(starts)
            else len(full_patch)
        ]
        for position, start in enumerate(starts)
    ]
    if len(paths) != len(sections):
        raise ValueError(
            f"full diff has {len(sections)} file sections for {len(paths)} changed paths"
        )
    return list(zip(paths, sections, strict=True))


def write_output(name: str, value: str) -> None:
    output = os.environ.get("GITHUB_OUTPUT")
    if output:
        with open(output, "a", encoding="utf-8") as handle:
            handle.write(f"{name}={value}\n")


def build_plan(args: argparse.Namespace) -> tuple[dict[str, Any], dict[str, bytes]]:
    files = []
    patches: dict[str, bytes] = {}
    for path, patch in changed_file_patches(
        args.commit_range, args.unified, args.inter_hunk
    ):
        domain = classify_path(path)
        shared = is_shared_contract(path, domain)
        patches[path] = patch
        files.append(
            FileDiff(path, domain, semantic_unit(path, domain, shared), shared, patch)
        )

    plan = plan_files(files)
    manifest = {
        "schema_version": 1,
        "case": args.case,
        "variant": args.variant,
        "repeat": args.repeat,
        "base_sha": args.base_sha,
        "head_sha": args.head_sha,
        "commit_range": args.commit_range,
        "unified": args.unified,
        "inter_hunk_context": args.inter_hunk,
        "variant_metadata": json.loads(args.variant_metadata_json),
        **plan,
    }
    canonical = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
    manifest["manifest_digest"] = hashlib.sha256(canonical).hexdigest()
    return manifest, patches


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True)
    parser.add_argument("--variant", required=True)
    parser.add_argument("--repeat", required=True)
    parser.add_argument("--base-sha", required=True)
    parser.add_argument("--head-sha", required=True)
    parser.add_argument("--commit-range", required=True)
    parser.add_argument("--shard", choices=SHARD_IDS, required=True)
    parser.add_argument("--unified", type=int, required=True)
    parser.add_argument("--inter-hunk", type=int, default=0)
    parser.add_argument("--variant-metadata-json", required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()

    if args.commit_range != f"{args.base_sha}...{args.head_sha}":
        raise SystemExit("commit range does not match the exact base/head SHAs")
    if git("rev-parse", "HEAD").decode().strip() != args.head_sha:
        raise SystemExit("historical checkout is not pinned to the requested head SHA")

    manifest, patches = build_plan(args)
    shard = next(item for item in manifest["shards"] if item["id"] == args.shard)
    packet_paths = sorted(set(shard["primary_files"] + shard["shared_files"]))
    packet = (
        b"".join(patches[path] for path in packet_paths)
        if manifest["status"] == "planned"
        else b""
    )

    args.output_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = args.output_dir / "codex-shard-manifest.json"
    packet_path = args.output_dir / "codex-review.diff"
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    packet_path.write_bytes(packet)

    write_output("planner_status", manifest["status"])
    write_output("manifest_digest", manifest["manifest_digest"])
    write_output(
        "active",
        "true" if manifest["status"] == "planned" and shard["active"] else "false",
    )
    write_output("bytes", str(shard["packet_bytes"]))
    write_output("lines", str(shard["packet_lines"]))
    write_output("files", str(shard["packet_files"]))
    write_output(
        "hunks", str(sum(1 for line in packet.splitlines() if line.startswith(b"@@")))
    )
    write_output(
        "context_lines",
        str(sum(1 for line in packet.splitlines() if line.startswith(b" "))),
    )
    write_output("started_at", str(int(__import__("time").time())))


if __name__ == "__main__":
    main()
