#!/usr/bin/env python3
"""Offline regression tests for the installed-compose adoption helper."""

import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "scripts/configure-etcd-retention.py"
COMMAND = "    command:\n      - /usr/local/bin/etcd\n"
FLAGS = (
    "      - --auto-compaction-mode=periodic\n      - --auto-compaction-retention=1h\n"
)
INSTALLED = (
    "name: proto-fleet-ha\nservices:\n  etcd:\n"
    "    image: gcr.io/etcd-development/etcd:v3.5.18@sha256:installed-image\n"
    + COMMAND
    + "      - --name=${HA_NODE_NAME}\n"
    "      - --data-dir=/var/lib/etcd\n"
    "      - --initial-cluster-state=new\n"
    "    volumes:\n      - ${HA_DATA_DIR}/etcd:/var/lib/etcd\n"
    "  patroni:\n    image: installed-patroni:unchanged\n"
)


class EtcdRetentionTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / "compose.yaml"

    def run_helper(self):
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(self.path)],
            capture_output=True,
            text=True,
        )

    def test_adds_only_retention_flags_preserving_image_and_permissions(self):
        self.path.write_text(INSTALLED)
        self.path.chmod(0o640)
        before = self.path.stat()
        result = self.run_helper()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.path.read_text(), INSTALLED.replace(COMMAND, COMMAND + FLAGS)
        )
        after = self.path.stat()
        self.assertEqual(stat.S_IMODE(after.st_mode), 0o640)
        self.assertEqual((after.st_uid, after.st_gid), (before.st_uid, before.st_gid))

    def test_idempotent_without_rewriting_file(self):
        content = INSTALLED.replace(COMMAND, COMMAND + FLAGS)
        self.path.write_text(content)
        before = self.path.stat()
        result = self.run_helper()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.path.read_text(), content)
        self.assertEqual(self.path.stat().st_mtime_ns, before.st_mtime_ns)

    def test_refuses_ambiguous_or_unexpected_configuration_without_changes(self):
        variants = [
            INSTALLED.replace(
                COMMAND, COMMAND + "      - --auto-compaction-mode=periodic\n"
            ),
            INSTALLED.replace(COMMAND, COMMAND + FLAGS.replace("1h", "24h")),
            INSTALLED.replace(COMMAND, COMMAND + FLAGS + FLAGS),
            INSTALLED.replace(
                COMMAND, COMMAND + "      - --auto-compaction-retention\n      - 1h\n"
            ),
            INSTALLED.replace(COMMAND, COMMAND + "      - --config-file=/custom.yml\n"),
            INSTALLED.replace(
                COMMAND,
                COMMAND
                + "    environment:\n      ETCD_AUTO_COMPACTION_RETENTION: 24h\n",
            ),
            INSTALLED.replace(COMMAND, COMMAND + COMMAND),
            INSTALLED + "  etcd:\n" + COMMAND,
            INSTALLED.replace("  etcd:\n", "  other:\n"),
        ]
        for content in variants:
            with self.subTest(content=content):
                self.path.write_text(content)
                result = self.run_helper()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("refusing", result.stderr)
                self.assertEqual(self.path.read_text(), content)

    def test_refuses_symlink(self):
        target = self.path.with_name("original.yaml")
        target.write_text(INSTALLED)
        self.path.symlink_to(target)
        result = self.run_helper()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refusing", result.stderr)
        self.assertTrue(self.path.is_symlink())
        self.assertEqual(target.read_text(), INSTALLED)


if __name__ == "__main__":
    unittest.main()
