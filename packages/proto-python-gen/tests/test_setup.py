"""Exercise setup through an interpreter symlink without accessing a package index."""

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


class SetupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin_dir = self.root / "link-farm"
        self.bin_dir.mkdir()
        self.python = self.bin_dir / "python3"
        self.python.symlink_to(sys._base_executable)
        self.real_python = Path(
            subprocess.check_output(
                [
                    self.python,
                    "-c",
                    "import os, sys; print(os.path.realpath(sys.executable))",
                ],
                text=True,
            ).strip()
        )

        # Redirect discovery in the fixture so a host system Python cannot hide
        # the Hermit-style symlink. The setup logic itself remains unchanged.
        source = Path(__file__).resolve().parents[1] / "setup.sh"
        script = source.read_text()
        system_paths = 'SYSTEM_PATHS="/usr/bin /usr/local/bin /opt/homebrew/bin"'
        self.assertIn(system_paths, script)
        script = script.replace(
            system_paths,
            f'SYSTEM_PATHS="{self.bin_dir}"',
        )
        self.setup = self.root / "setup.sh"
        self.setup.write_text(script)
        (self.root / "pip-config.sh").write_text("")
        (self.root / "requirements.txt").write_text("")
        self.env = os.environ.copy()
        self.env["PIP_NO_INDEX"] = "1"
        # Reuse the test runner's installed dependencies for setup's final
        # import checks; the fixture only exercises interpreter/venv bootstrap.
        dependencies = self.root / "dependencies"
        dependencies.mkdir()
        for path in sys.path:
            if path.endswith("site-packages"):
                for package in Path(path).iterdir():
                    if package.name.startswith(("grpc", "google", "protobuf")):
                        (dependencies / package.name).symlink_to(package)
        self.env["PYTHONPATH"] = str(dependencies)

    def run_setup(self):
        result = subprocess.run(
            ["bash", self.setup],
            check=False,
            env=self.env,
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        config = dict(
            line.split(" = ", 1)
            for line in (self.root / ".venv/pyvenv.cfg").read_text().splitlines()
            if " = " in line
        )
        self.assertEqual(Path(config["home"]).resolve(), self.real_python.parent)
        subprocess.run(
            [self.root / ".venv/bin/python", "-I", "-c", "import encodings, pip"],
            check=True,
        )

    def test_interpreter_symlink_uses_physical_home(self):
        self.run_setup()

    def test_failed_venv_bootstrap_is_repaired(self):
        bin_dir = self.root / ".venv/bin"
        bin_dir.mkdir(parents=True)
        python = bin_dir / "python"
        python.write_text("#!/bin/sh\nexit 1\n")
        python.chmod(0o755)
        self.run_setup()


if __name__ == "__main__":
    unittest.main()
