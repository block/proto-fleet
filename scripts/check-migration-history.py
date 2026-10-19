#!/usr/bin/env python3
"""Check immutable SQL history and unchanged, ordered shared promotion."""

import argparse
import hashlib
import pathlib
import re
import subprocess
import sys

MIGRATIONS = "server/migrations/"
CURRENT = MIGRATIONS + "current/"
NAME = re.compile(r"([0-9]{6})_(shared|internal)_[a-z0-9_]+\.(up|down)\.sql")
REQUIRES = re.compile(rb"^-- requires-shared: ([0-9]+)\r?$", re.MULTILINE)

# Issue #221: startup cannot reach a corrective migration after 1016 fails.
# Permit only the reviewed original -> repaired bytes, never arbitrary edits to
# this path. Context: https://github.com/btc-mining/proto-fleet-internal/pull/222.
REPAIRED_MIGRATIONS = {
    CURRENT + "001016_internal_ct1_metric_summaries.up.sql": (
        "8268ab02c4d7bcaefe299b13fcf9153edf08ed17664c052547f1e69642d9c63c",
        "505a166f956ee2c12e4d23b3f3bb6129224c23917db1589e7aa8686b62c94484",
    ),
}

# PRs #229 and #232 both merged version 1026. The duplicate prevents loading
# any migrations, so a later corrective migration cannot run. Preserve #229's
# checkpoint and allow only #232's exact SQL bytes to move to 1027.
RENAMED_MIGRATIONS = {
    CURRENT + "001026_internal_container_telemetry_compression.up.sql": (
        CURRENT + "001027_internal_container_telemetry_compression.up.sql",
        "85c6e743d79f3fd9ccd2f293c239ad4d428d762e6860b81f44ba0d1a99012e0e",
    ),
    CURRENT + "001026_internal_container_telemetry_compression.down.sql": (
        CURRENT + "001027_internal_container_telemetry_compression.down.sql",
        "8ab20fb3ec8d6de03966b3e2601e83b0f03eb16cde4e28b723f55be6cba9c0e2",
    ),
}


def git(repo, *args):
    result = subprocess.run(
        ["git", "-C", str(repo), *args], capture_output=True, check=False
    )
    if result.returncode:
        raise ValueError("cannot read Git revision: " + result.stderr.decode().strip())
    return result.stdout


def revision_sql(repo, ref):
    commit = (
        git(repo, "rev-parse", "--verify", "--end-of-options", ref + "^{commit}")
        .decode()
        .strip()
    )
    paths = (
        git(repo, "ls-tree", "-rz", "--name-only", commit, "--", MIGRATIONS)
        .decode()
        .split("\0")
    )
    return {
        path: git(repo, "show", f"{commit}:{path}")
        for path in paths
        if path.endswith(".sql")
    }


def active_migrations(files, public_only=False):
    versions = {}
    for path, body in sorted(files.items()):
        if not path.startswith(CURRENT):
            continue
        match = NAME.fullmatch(path[len(CURRENT) :])
        if not match:
            raise ValueError(f"invalid active migration filename: {path}")
        version, kind, direction = match.groups()
        version = int(version)
        if version < 1000:
            raise ValueError(
                f"migration {version}: numbers below 1000 are reserved for legacy history"
            )
        if public_only and kind == "internal":
            raise ValueError(f"private migration in public source: {path}")
        pair = versions.setdefault(version, {})
        if direction in pair:
            raise ValueError(f"duplicate migration version {version}: {path}")
        pair[direction] = (path, body, kind)
    for version, pair in versions.items():
        if set(pair) != {"up", "down"} or pair["up"][0].removesuffix(".up.sql") != pair[
            "down"
        ][0].removesuffix(".down.sql"):
            raise ValueError(
                f"migration {version} requires paired up/down files with identical names"
            )
        path, body, kind = pair["up"]
        if kind == "internal":
            requirements = REQUIRES.findall(body)
            required = int(requirements[0]) if len(requirements) == 1 else -1
            shared = versions.get(required, {}).get("up")
            if not shared or shared[2] != "shared" or required >= version:
                raise ValueError(
                    f"{path}: requires-shared must name one earlier included shared migration"
                )
    return versions


def check(repo, base_ref, public_ref=None, public_only=False):
    base = revision_sql(repo, base_ref)
    local = {}
    for path in (repo / MIGRATIONS).rglob("*.sql"):
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"migration must be a regular file: {path}")
        local[path.relative_to(repo).as_posix()] = path.read_bytes()
    # Normalize only an exact repair, including the base used for ordering:
    # the merged base itself has duplicate versions and cannot be parsed as-is.
    for source, (target, checksum) in RENAMED_MIGRATIONS.items():
        if (
            source in base
            and source not in local
            and target not in base
            and local.get(target) == base[source]
            and hashlib.sha256(base[source]).hexdigest() == checksum
        ):
            base[target] = base.pop(source)
    for path, body in base.items():
        if local.get(path) != body:
            repair = REPAIRED_MIGRATIONS.get(path)
            if (
                repair is None
                or path not in local
                or (
                    hashlib.sha256(body).hexdigest(),
                    hashlib.sha256(local[path]).hexdigest(),
                )
                != repair
            ):
                raise ValueError(
                    f"immutable migration changed, deleted or renamed: {path}"
                )
    for path in local.keys() - base.keys():
        if pathlib.PurePosixPath(path).parent.as_posix() == MIGRATIONS.rstrip("/"):
            raise ValueError(f"new migrations belong in {CURRENT}: {path}")
    current = active_migrations(local, public_only)
    if not current:
        raise ValueError("active migration source is empty")
    if (
        any(pair["up"][2] == "internal" for pair in current.values())
        and current[max(current)]["up"][2] != "internal"
    ):
        raise ValueError("internal releases must end at a private checkpoint")
    previous = active_migrations(base, public_only)
    high_water = max(previous, default=-1)
    for version in current.keys() - previous.keys():
        if version <= high_water:
            raise ValueError(
                f"new migration {version} is below merged high-water {high_water}"
            )
    if public_ref:
        public = active_migrations(revision_sql(repo, public_ref), public_only=True)
        shared = sorted(
            version for version, pair in current.items() if pair["up"][2] == "shared"
        )
        promoted = sorted(public)
        if promoted != shared[: len(promoted)]:
            raise ValueError(
                "public shared migrations must be an identical ordered prefix"
            )
        for version in promoted:
            if public[version] != current[version]:
                raise ValueError(f"public shared migration differs: {version}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--repo", type=pathlib.Path, default=pathlib.Path(__file__).resolve().parents[1]
    )
    parser.add_argument(
        "--base-ref",
        required=True,
        help="merged/released Git revision whose SQL is immutable",
    )
    parser.add_argument(
        "--public-ref", help="exact fetched public commit to compare; internal CI only"
    )
    parser.add_argument(
        "--public-only",
        action="store_true",
        help="reject private SQL in the active source",
    )
    args = parser.parse_args()
    try:
        check(args.repo.resolve(), args.base_ref, args.public_ref, args.public_only)
    except ValueError as error:
        print(f"Migration history check failed: {error}", file=sys.stderr)
        return 1
    print("Migration history is immutable, paired and ordered.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
