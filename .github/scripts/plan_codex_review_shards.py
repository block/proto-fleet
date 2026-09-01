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
MAX_SEMANTIC_UNITS = 750
MAX_SEARCH_STATES = 2_000
SHARD_IDS = ("shard-1", "shard-2")
HUNK_HEADER = re.compile(rb"^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@")


def _collapse_line_numbers(lines: list[int]) -> list[list[int]]:
    if not lines:
        return []
    ranges = []
    start = previous = lines[0]
    for line in lines[1:]:
        if line != previous + 1:
            ranges.append([start, previous])
            start = line
        previous = line
    ranges.append([start, previous])
    return ranges


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

    @property
    def is_whole_file_deletion(self) -> bool:
        return b"+++ /dev/null" in self.patch.splitlines()

    @property
    def citation_side(self) -> str:
        return "base" if self.is_whole_file_deletion else "head"

    @property
    def changed_line_ranges(self) -> list[list[int]]:
        ranges = []
        added_lines: list[int] = []
        removed_lines: list[int] = []
        deletion_anchor: int | None = None
        hunk_start = 0
        hunk_end: int | None = None
        old_line: int | None = None
        new_line: int | None = None

        def finish_hunk() -> None:
            if self.is_whole_file_deletion and removed_lines:
                ranges.extend(_collapse_line_numbers(removed_lines))
            elif added_lines:
                ranges.extend(_collapse_line_numbers(added_lines))
            elif deletion_anchor is not None:
                # A deleted line in a surviving file cannot be linked in the head
                # revision. Anchor a deletion-only hunk to its nearest surviving
                # new-side line, or line 1 when no hunk lines survive.
                anchor = max(deletion_anchor, 1)
                ranges.append([anchor, anchor])

        for line in self.patch.splitlines():
            if match := HUNK_HEADER.match(line):
                finish_hunk()
                added_lines = []
                removed_lines = []
                deletion_anchor = None
                old_line = int(match.group(1))
                hunk_start = int(match.group(3))
                count = int(match.group(4)) if match.group(4) is not None else 1
                hunk_end = hunk_start + count - 1 if count else None
                new_line = hunk_start
                continue
            if new_line is None or old_line is None:
                continue
            if line.startswith(b"+"):
                added_lines.append(max(new_line, 1))
                new_line += 1
            elif line.startswith(b"-"):
                removed_lines.append(max(old_line, 1))
                old_line += 1
                if deletion_anchor is None:
                    deletion_anchor = (
                        min(max(new_line, hunk_start), hunk_end)
                        if hunk_end is not None
                        else max(hunk_start, 1)
                    )
            elif line.startswith(b" "):
                old_line += 1
                new_line += 1
            elif not line.startswith(b"\\ No newline at end of file"):
                old_line = None
                new_line = None

        finish_hunk()
        return ranges or [[1, 1]]


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


class PlannerSearchLimitExceeded(RuntimeError):
    """The deterministic partition search exhausted its trusted state budget."""


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
    name = PurePosixPath(path).name.lower()
    if path.startswith("proto/") and not is_generated(path):
        return True
    if is_generated(path):
        return True
    if path.startswith(("server/migrations/", "server/sqlc/queries/")):
        return True
    if domain == "client-shared":
        return True
    if path.startswith(("deployment-files/", "server/monitoring/")):
        return True
    if name.startswith(("dockerfile", "docker-compose", "compose.")):
        return True
    if "/" not in path:
        return path not in {
            "AGENTS.md",
            "CLAUDE.md",
            "CODE_OF_CONDUCT.md",
            "CONTRIBUTING.md",
            "GOVERNANCE.md",
            "LICENSE",
            "README.md",
            "SECURITY.md",
        }
    return False


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
        elif path.startswith(".github/"):
            root = parts[:1]
        elif parts and parts[0] in {"deployment-files", "docs", "scripts"}:
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


def unit_domains(units: list[Unit]) -> set[str]:
    return {file.domain for unit in units for file in unit.files}


def shared_audiences(file: FileDiff) -> set[str] | None:
    path = file.path
    if file.domain == "client-shared":
        return {"protofleet", "protoos"}
    if is_generated(path):
        if path.startswith("client/src/protoFleet/"):
            return {"protofleet"}
        if path.startswith("client/src/protoOS/"):
            return {"protoos"}
        if path.startswith("server/"):
            return {"server"}
        if path.startswith("plugin/asicrs/"):
            return {"asicrs"}
        if path.startswith(("plugin/", "packages/proto-python-gen/")):
            return {"plugins"}
    return None


def shared_context_for_units(
    shared_files: list[FileDiff], units: list[Unit]
) -> list[FileDiff]:
    domains = unit_domains(units)
    if not domains:
        return []
    return [
        file
        for file in shared_files
        if (audiences := shared_audiences(file)) is None or audiences & domains
    ]


def packet_files_for_units(
    owned_units: list[Unit], other_units: list[Unit]
) -> list[FileDiff]:
    primary = [file for unit in owned_units for file in unit.files]
    other_shared = [file for unit in other_units for file in unit.files if file.shared]
    context = shared_context_for_units(other_shared, owned_units)
    return sorted(
        {file.path: file for file in primary + context}.values(),
        key=lambda file: file.path,
    )


def packet_metrics(bins: list[list[Unit]], index: int) -> tuple[int, int]:
    files = packet_files_for_units(bins[index], bins[1 - index])
    return sum(file.bytes for file in files), sum(file.lines for file in files)


def shared_context_overflow_reasons(
    files: list[FileDiff], units: list[Unit]
) -> list[str]:
    global_shared = [
        file for file in files if file.shared and shared_audiences(file) is None
    ]
    if (
        sum(file.bytes for file in global_shared) > MAX_PACKET_BYTES
        or sum(file.lines for file in global_shared) > MAX_PACKET_LINES
    ):
        return ["globally replicated shared context exceeds a packet limit"]

    reasons = []
    for audience in sorted(unit_domains(units)):
        delivered = [
            file
            for file in files
            if file.shared
            and ((audiences := shared_audiences(file)) is None or audience in audiences)
        ]
        if (
            sum(file.bytes for file in delivered) > MAX_PACKET_BYTES
            or sum(file.lines for file in delivered) > MAX_PACKET_LINES
        ):
            reasons.append(
                f"shared context for {audience} audience exceeds a packet limit"
            )
    return reasons


def find_bounded_assignment(units: list[Unit]) -> list[list[Unit]] | None:
    ordered = _ordered_units(units)
    if (
        sum(unit.bytes for unit in ordered) > 2 * MAX_PACKET_BYTES
        or sum(unit.lines for unit in ordered) > 2 * MAX_PACKET_LINES
    ):
        return None

    bins: list[list[Unit]] = [[], []]
    failed: set[tuple[Any, ...]] = set()
    explored_states = 0

    def search(position: int) -> tuple[int, ...] | None:
        nonlocal explored_states
        explored_states += 1
        if explored_states > MAX_SEARCH_STATES:
            raise PlannerSearchLimitExceeded
        if position == len(ordered):
            return ()
        metrics = (packet_metrics(bins, 0), packet_metrics(bins, 1))
        state = (
            position,
            metrics,
            tuple(frozenset(unit_domains(items)) for items in bins),
            tuple(
                tuple(
                    unit.key
                    for unit in items
                    if any(file.shared for file in unit.files)
                )
                for items in bins
            ),
        )
        if state in failed:
            return None
        unit = ordered[position]
        candidates = (
            [0]
            if position == 0
            else sorted(range(2), key=lambda index: (*metrics[index], index))
        )
        for index in candidates:
            bins[index].append(unit)
            next_metrics = (packet_metrics(bins, 0), packet_metrics(bins, 1))
            if all(
                packet_bytes <= MAX_PACKET_BYTES and packet_lines <= MAX_PACKET_LINES
                for packet_bytes, packet_lines in next_metrics
            ):
                suffix = search(position + 1)
                if suffix is not None:
                    bins[index].pop()
                    return (index, *suffix)
            bins[index].pop()
        failed.add(state)
        return None

    assignment = search(0)
    if assignment is None:
        return None
    result: list[list[Unit]] = [[], []]
    for unit, index in zip(ordered, assignment, strict=True):
        result[index].append(unit)
    return result


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
    units = group_units(files)
    reasons = shared_context_overflow_reasons(files, units)

    if len(units) > MAX_SEMANTIC_UNITS:
        reasons.append(
            f"semantic unit count {len(units)} exceeds planner safety limit "
            f"{MAX_SEMANTIC_UNITS}"
        )

    try:
        bins = find_bounded_assignment(units) if not reasons else None
    except PlannerSearchLimitExceeded:
        reasons.append(
            f"planner search exceeds trusted state limit {MAX_SEARCH_STATES}"
        )
        bins = None
    if bins is None:
        if not reasons:
            reasons.append(
                "semantic units do not fit in two bounded review-wide packets"
            )
        bins = _assign_without_limits(units)

    status = "planned" if not reasons else "oversized"

    primary_by_shard = [
        [file for unit in owned_units for file in unit.files] for owned_units in bins
    ]
    active = [bool(primary_by_shard[0]), bool(primary_by_shard[1])]
    context_by_shard = [
        shared_context_for_units(
            [file for file in primary_by_shard[1] if file.shared], bins[0]
        ),
        shared_context_for_units(
            [file for file in primary_by_shard[0] if file.shared], bins[1]
        ),
    ]

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
        packet_files = packet_files_for_units(bins[index], bins[1 - index])
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
            "changed_line_ranges": file.changed_line_ranges,
            "citation_side": file.citation_side,
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
            "max_semantic_units": MAX_SEMANTIC_UNITS,
            "max_search_states": MAX_SEARCH_STATES,
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


if __name__ == "__main__":
    main()
