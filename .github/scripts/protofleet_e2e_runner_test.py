#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("restore_protofleet_e2e_runner.sh")


class RestoreRunnerImageTest(unittest.TestCase):
    def setUp(self):
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary_directory.name)
        self.install_root = self.root / "image"
        self.workspace = self.root / "workspace"
        self.fake_bin = self.root / "bin"
        self.output = self.root / "output"
        self.environment = self.root / "environment"
        (self.workspace / "client").mkdir(parents=True)
        self.fake_bin.mkdir()
        docker = self.fake_bin / "docker"
        docker.write_text(
            "#!/bin/sh\n"
            'if [ "$1" = image ] && [ "$2" = inspect ] && '
            '[ "$3" != missing:latest ]; then exit 0; fi\n'
            "exit 1\n",
            encoding="utf-8",
        )
        docker.chmod(0o755)
        self.server_hash = "a" * 64
        self.client_hash = "b" * 64

    def tearDown(self):
        self.temporary_directory.cleanup()

    def write_image(self, docker_images=None):
        dependencies = self.install_root / "client-node-modules" / "package"
        browser = self.install_root / "ms-playwright" / "chromium-1"
        dependencies.mkdir(parents=True)
        browser.mkdir(parents=True)
        (dependencies / "index.js").write_text("export {};\n", encoding="utf-8")
        (browser / "chrome").write_text("browser\n", encoding="utf-8")
        manifest = {
            "schema_version": 1,
            "source_sha": "c" * 40,
            "server_hash": self.server_hash,
            "client_lock_hash": self.client_hash,
            "docker_images": docker_images or ["present:latest"],
        }
        (self.install_root / "manifest.json").write_text(
            json.dumps(manifest), encoding="utf-8"
        )

    def run_restore(self, **overrides):
        environment = {
            **os.environ,
            "PATH": f"{self.fake_bin}:{os.environ['PATH']}",
            "GITHUB_WORKSPACE": str(self.workspace),
            "GITHUB_OUTPUT": str(self.output),
            "GITHUB_ENV": str(self.environment),
            "PROTOFLEET_E2E_INSTALL_ROOT": str(self.install_root),
            "PROTOFLEET_JQ": shutil.which("jq") or "jq",
            "PROTOFLEET_CURRENT_SERVER_HASH": self.server_hash,
            "PROTOFLEET_CURRENT_CLIENT_LOCK_HASH": self.client_hash,
            "PROTOFLEET_ALLOW_PREBAKED_DOCKER": "true",
            **overrides,
        }
        result = subprocess.run(
            ["bash", str(SCRIPT)],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )
        outputs = {}
        if self.output.exists():
            for line in self.output.read_text(encoding="utf-8").splitlines():
                key, value = line.split("=", 1)
                outputs[key] = value
        return result, outputs

    def test_restores_every_hash_matched_component(self):
        self.write_image()
        result, outputs = self.run_restore()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(outputs["client-dependencies"], "true")
        self.assertEqual(outputs["playwright-browsers"], "true")
        self.assertEqual(outputs["docker-images"], "true")
        self.assertTrue(
            (self.workspace / "client/node_modules/package/index.js").is_file()
        )
        self.assertIn(
            f"PLAYWRIGHT_BROWSERS_PATH={self.install_root}/ms-playwright",
            self.environment.read_text(encoding="utf-8"),
        )

    def test_lockfile_mismatch_falls_back_but_docker_can_still_hit(self):
        self.write_image()
        result, outputs = self.run_restore(PROTOFLEET_CURRENT_CLIENT_LOCK_HASH="d" * 64)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(outputs["client-dependencies"], "false")
        self.assertEqual(outputs["playwright-browsers"], "false")
        self.assertEqual(outputs["docker-images"], "true")

    def test_missing_docker_image_falls_back(self):
        self.write_image(["present:latest", "missing:latest"])
        result, outputs = self.run_restore()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(outputs["docker-images"], "false")
        self.assertIn("missing:latest", result.stdout)

    def test_scheduled_run_can_disable_prebaked_docker(self):
        self.write_image()
        result, outputs = self.run_restore(PROTOFLEET_ALLOW_PREBAKED_DOCKER="false")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(outputs["client-dependencies"], "true")
        self.assertEqual(outputs["docker-images"], "false")

    def test_missing_manifest_is_a_clean_miss(self):
        result, outputs = self.run_restore()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(outputs["client-dependencies"], "false")
        self.assertEqual(outputs["playwright-browsers"], "false")
        self.assertEqual(outputs["docker-images"], "false")


if __name__ == "__main__":
    unittest.main()
