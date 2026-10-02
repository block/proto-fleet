"""Package a conservative cloud application-replacement compatibility identity.

Matching identities guard source compatibility, not installed dependency versions.
Floating image/package versions can differ; manual release qualification is still required.
"""

import hashlib
import json
import re
import sys
from pathlib import Path

DATABASE_FILES = (
    "server/timescaledb/Dockerfile",
    "server/timescaledb/docker-entrypoint.sh",
    "deployment-files/ha/compose.yaml",
    "deployment-files/ha/patroni.Dockerfile",
    "deployment-files/ha/patroni.yml.tmpl",
    "deployment-files/ha/patroni-build-requirements.txt",
    "deployment-files/ha/patroni-requirements.txt",
    "deployment-files/ha/scripts/patroni-entrypoint.sh",
    "deployment-files/ha/scripts/patroni-post-bootstrap.sh",
    "deployment-files/ha/scripts/render-patroni-config.py",
)


def fingerprint(root, names):
    digest = hashlib.sha256()
    for name in sorted(names):
        path = root / name
        if path.is_symlink() or not path.is_file():
            raise ValueError("Missing or unsafe compatibility input: " + name)
        digest.update(
            name.encode() + b"\0" + hashlib.sha256(path.read_bytes()).digest()
        )
    return digest.hexdigest()


def metadata(root):
    migrations = sorted((root / "server/migrations").glob("*.sql"))
    versions = {"up": set(), "down": set()}
    for path in migrations:
        match = re.fullmatch(r"([0-9]+)_.+\.(up|down)\.sql", path.name)
        if not match or int(match[1]) in versions[match[2]]:
            raise ValueError("Invalid or duplicate migration: " + path.name)
        versions[match[2]].add(int(match[1]))
    if not versions["up"] or versions["up"] != versions["down"]:
        raise ValueError("Require nonempty matching up/down migrations")
    bridge_sql = sorted((root / "server/migrations/bridges").rglob("*.sql"))
    return {
        "schema_version": max(versions["up"]),
        "compatibility_sha256": fingerprint(
            root,
            [str(path.relative_to(root)) for path in migrations + bridge_sql]
            + list(DATABASE_FILES),
        ),
    }


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit("Usage: create-cloud-release.py SOURCE_ROOT DEPLOYMENT_ROOT")
    destination = Path(sys.argv[2]) / "cloud-release.json"
    destination.write_text(
        json.dumps(metadata(Path(sys.argv[1])), sort_keys=True, indent=2) + "\n"
    )
