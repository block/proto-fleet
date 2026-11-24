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
LATEST = MIGRATIONS + "LATEST"
CONSOLIDATION_VERSION = 1115
CONSOLIDATION = f"{CURRENT}{CONSOLIDATION_VERSION:06d}_shared_repository_consolidation"
CONSOLIDATION_UP = CONSOLIDATION + ".up.sql"
CONSOLIDATION_DOWN = CONSOLIDATION + ".down.sql"
LEGACY_FIXTURE = MIGRATIONS + "legacy_internal.fixture"
SECTION_MARKER = b"\n-- fleet-migration: "
DOWN_GUARD = b"DO $$ BEGIN RAISE EXCEPTION 'Repository consolidation cannot be reversed'; END $$;"
PUBLIC_LEGACY_VERSIONS = {150: 151, 151: 155, 152: 156, 153: 157}
NAME = re.compile(r"([0-9]{6})_(shared|internal)_[a-z0-9_]+\.(up|down)\.sql")
REQUIRES = re.compile(rb"^-- requires-shared: ([0-9]+)\r?$", re.MULTILINE)

# The first public merge has no internal files to compare with the archive.
CONSOLIDATION_SHA256 = {
    CONSOLIDATION_UP: "f25c537604b111073f5229cf4adbea3e43115b9065d2c7ab6246c0b146c4d8f3",
    CONSOLIDATION_DOWN: "4d952cd0ff3106b79e2c54b18463581cd8f1f5b5838a640262ad7f21653e1bdb",
    LEGACY_FIXTURE: "8bcc7788d7919fe049655444c43c1339d2fdf752d0faa882bcfbc2e10b7c8b1c",
}

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
    # Device-write outcomes merged first at 1057. Preserve that checkpoint and
    # move only the unchanged topology-reference pair past the collision.
    CURRENT + "001057_internal_bound_topology_reference_planning.up.sql": (
        CURRENT + "001058_internal_bound_topology_reference_planning.up.sql",
        "d00fd55b0aaf5ed1ce0c31b5993412fdae83fc170682f404be3ee8e3ed0cce00",
    ),
    CURRENT + "001057_internal_bound_topology_reference_planning.down.sql": (
        CURRENT + "001058_internal_bound_topology_reference_planning.down.sql",
        "7ab7b00e6956fdf83b8552ad6eba2b5912ee4705a886b4a80363c9ab8968f00d",
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
        if path.endswith(".sql") or path == LEGACY_FIXTURE
    }


def active_migrations(files):
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


def archived_sql(body, directory):
    sections = {}
    for section in body.split(SECTION_MARKER)[1:]:
        filename, sep, sql = section.partition(b"\n")
        name = filename.decode()
        path = directory + name
        if not sep or not re.fullmatch(r"[0-9]{6}_[a-z0-9_]+\.(up|down)\.sql", name):
            raise ValueError(f"invalid archived migration section: {name}")
        if path in sections:
            raise ValueError(f"duplicate archived migration section: {path}")
        sections[path] = sql
    return sections


def consolidation_archive(base, local):
    if CONSOLIDATION_UP in base or CONSOLIDATION_UP not in local:
        return {}

    up_sql = local[CONSOLIDATION_UP]
    up_header = up_sql.partition(SECTION_MARKER)[0]
    if any(line and not line.startswith(b"--") for line in up_header.splitlines()):
        raise ValueError("consolidation up header must contain only comments")
    down_sql = local.get(CONSOLIDATION_DOWN, b"")
    down_header = down_sql.partition(SECTION_MARKER)[0]
    if not down_header.endswith(DOWN_GUARD + b"\n/*\n") or not down_sql.endswith(
        b"\n*/\n"
    ):
        raise ValueError(
            "consolidation down must refuse rollback before the archive comment"
        )
    if any(
        line and not line.startswith(b"--") for line in down_header.splitlines()[:-2]
    ):
        raise ValueError("consolidation down header must contain only comments")
    ups = archived_sql(up_sql, CURRENT)
    downs = archived_sql(down_sql.removesuffix(b"\n*/\n"), CURRENT)
    if list(ups) != sorted(ups) or list(downs) != sorted(downs, reverse=True):
        raise ValueError("consolidation sections must be in migration order")
    if any(not path.endswith(".up.sql") for path in ups) or any(
        not path.endswith(".down.sql") for path in downs
    ):
        raise ValueError("consolidation sections must match their file direction")
    current_archive = ups | downs
    baseline = {
        path: sql for path, sql in local.items() if path.startswith(CURRENT + "001000_")
    }
    versions = active_migrations(baseline | current_archive)
    if min(versions, default=0) != 1000 or max(versions) != CONSOLIDATION_VERSION - 1:
        raise ValueError(
            "consolidation must cover the migrations after 1000 through 1114"
        )
    legacy_archive = archived_sql(local.get(LEGACY_FIXTURE, b""), MIGRATIONS)
    if not legacy_archive:
        raise ValueError("consolidation is missing the internal legacy fixture")
    archive = current_archive | legacy_archive
    if archive.keys() & local.keys():
        raise ValueError("archived migrations must be absent from the normal source")
    if any(path.startswith(CURRENT + "001001_internal_") for path in base):
        removed = {path: sql for path, sql in base.items() if path not in local}
        if removed != archive:
            raise ValueError(
                "consolidation must preserve exactly the removed migration bytes"
            )
    else:
        for path, expected in CONSOLIDATION_SHA256.items():
            if hashlib.sha256(local[path]).hexdigest() != expected:
                raise ValueError(f"consolidation archive checksum differs: {path}")
    return archive


def restored_public_legacy(path, body, archive):
    name = pathlib.PurePosixPath(path).name
    version = PUBLIC_LEGACY_VERSIONS.get(int(name[:6])) if name[:6].isdigit() else None
    return (
        version is not None
        and archive.get(MIGRATIONS + f"{version:06d}_" + name[7:]) == body
    )


def check(repo, base_ref, public_ref=None):
    base = revision_sql(repo, base_ref)
    local = {}
    for path in (repo / MIGRATIONS).rglob("*.sql"):
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"migration must be a regular file: {path}")
        local[path.relative_to(repo).as_posix()] = path.read_bytes()
    fixture = repo / LEGACY_FIXTURE
    if fixture.exists() or fixture.is_symlink():
        if fixture.is_symlink() or not fixture.is_file():
            raise ValueError(
                f"migration fixture must be a regular file: {LEGACY_FIXTURE}"
            )
        local[LEGACY_FIXTURE] = fixture.read_bytes()
    archive = consolidation_archive(base, local)
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
        if local.get(path) != body and archive.get(path) != body:
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
        if pathlib.PurePosixPath(path).parent.as_posix() != MIGRATIONS.rstrip("/"):
            continue
        if archive and (
            path == LEGACY_FIXTURE or restored_public_legacy(path, local[path], archive)
        ):
            continue
        raise ValueError(f"new migrations belong in {CURRENT}: {path}")
    current = active_migrations(local)
    if not current:
        raise ValueError("active migration source is empty")
    latest_path, _, latest_kind = current[max(current)]["up"]
    consolidated = CONSOLIDATION_UP in local
    if consolidated:
        for version in current:
            if 1000 < version < CONSOLIDATION_VERSION:
                raise ValueError(
                    f"migration {version}: number is reserved for consolidation"
                )
    has_internal = any(pair["up"][2] == "internal" for pair in current.values())
    if has_internal:
        if consolidated:
            raise ValueError("use shared migrations after consolidation")
        if latest_kind != "internal":
            raise ValueError("internal releases must end at a private checkpoint")
    latest = repo / LATEST
    expected = pathlib.PurePosixPath(latest_path).name.removesuffix(".up.sql")
    if not latest.is_file() or latest.is_symlink():
        raise ValueError(f"{LATEST} must be a regular file containing {expected}")
    if latest.read_text() != expected + "\n":
        raise ValueError(
            f"{LATEST} must contain exactly {expected} followed by a newline"
        )
    previous = active_migrations(base)
    high_water = max(previous, default=-1)
    for version in current.keys() - previous.keys():
        if version <= high_water:
            raise ValueError(
                f"new migration {version} is below merged high-water {high_water}"
            )
    if public_ref:
        public_files = revision_sql(repo, public_ref)
        if consolidated:
            for path, body in public_files.items():
                if not path.startswith(CURRENT) and local.get(path) != body:
                    raise ValueError(f"public legacy migration differs: {path}")
        public = active_migrations(public_files)
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
        "--public-ref", help="frozen public migration history to compare (optional)"
    )
    args = parser.parse_args()
    try:
        check(args.repo.resolve(), args.base_ref, args.public_ref)
    except ValueError as error:
        print(f"Migration history check failed: {error}", file=sys.stderr)
        return 1
    print("Migration history is immutable, paired and ordered.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
