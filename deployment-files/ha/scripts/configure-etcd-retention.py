#!/usr/bin/env python3
"""Add retention to an installed HA compose file without upgrading or restarting."""

import os
import re
import stat
import sys
import tempfile
from pathlib import Path


COMMAND = "    command:\n      - /usr/local/bin/etcd\n"
FLAGS = (
    "      - --auto-compaction-mode=periodic\n      - --auto-compaction-retention=1h\n"
)


def configure(path: Path) -> bool:
    metadata = path.lstat()
    if not stat.S_ISREG(metadata.st_mode):
        raise ValueError("compose path must be a regular file, not a symlink")
    original = path.read_bytes()
    text = original.decode("utf-8")
    # Deliberately support only the shipped block-style service and command.
    # A YAML round trip could rewrite unrelated settings in an installed file.
    if text.count("\n  etcd:\n") != 1:
        raise ValueError("expected one etcd service in the shipped compose layout")
    start = text.index("\n  etcd:\n") + 1
    body_start = start + len("  etcd:\n")
    end_match = re.search(r"\n\S|\n  \S", text[body_start:])
    end = body_start + end_match.start() + 1 if end_match else len(text)
    service = text[start:end]
    if service.count("    command:\n") != 1 or service.count(COMMAND) != 1:
        raise ValueError("expected one explicit /usr/local/bin/etcd command")
    if re.search(r"--config-file|ETCD_CONFIG_FILE|ETCD_AUTO_COMPACTION", service):
        raise ValueError(
            "external or environment-based etcd configuration needs manual review"
        )
    command_start = service.index(COMMAND) + len("    command:\n")
    command_end = re.search(r"\n    \S", service[command_start:])
    end_offset = (
        command_start + command_end.start() + 1 if command_end else len(service)
    )
    command = service[command_start:end_offset]
    existing = [
        line
        for line in service.splitlines(keepends=True)
        if "--auto-compaction" in line
    ]
    if existing:
        if sorted(existing) != sorted(FLAGS.splitlines(keepends=True)) or not all(
            line in command for line in existing
        ):
            raise ValueError(
                "existing retention settings are partial, duplicated, or different"
            )
        return False
    updated = text[:start] + service.replace(COMMAND, COMMAND + FLAGS, 1) + text[end:]
    # Stage beside the destination so replacement is atomic on its filesystem.
    fd, temporary = tempfile.mkstemp(prefix=".compose-retention-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            output.write(updated.encode("utf-8"))
            output.flush()
            if os.geteuid() == 0:
                os.fchown(output.fileno(), metadata.st_uid, metadata.st_gid)
            elif (
                os.fstat(output.fileno()).st_uid,
                os.fstat(output.fileno()).st_gid,
            ) != (metadata.st_uid, metadata.st_gid):
                raise ValueError("run as the file owner or root to preserve ownership")
            os.fchmod(output.fileno(), stat.S_IMODE(metadata.st_mode))
            os.fsync(output.fileno())
        current = path.lstat()
        if (current.st_ino, current.st_mtime_ns, current.st_ctime_ns) != (
            metadata.st_ino,
            metadata.st_mtime_ns,
            metadata.st_ctime_ns,
        ) or path.read_bytes() != original:
            raise ValueError("compose file changed while preparing the update")
        os.replace(temporary, path)
    finally:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass
    return True


def main() -> None:
    if len(sys.argv) > 2:
        raise SystemExit("usage: configure-etcd-retention.py [COMPOSE_PATH]")
    path = Path(
        sys.argv[1] if len(sys.argv) == 2 else "/etc/proto-fleet/ha/compose.yaml"
    )
    try:
        changed = configure(path)
    except (OSError, ValueError) as error:
        raise SystemExit(f"refusing to change {path}: {error}") from error
    print(
        f"{'Updated' if changed else 'Already configured'}: {path}; no services restarted"
    )


if __name__ == "__main__":
    main()
