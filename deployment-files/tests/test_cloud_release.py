"""The cloud compatibility identity changes with schemas/dependencies, not app code."""

import importlib.util
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "scripts/create-cloud-release.py"
spec = importlib.util.spec_from_file_location("cloud_release", SCRIPT)
cloud = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cloud)


class CompatibilityTests(unittest.TestCase):
    def test_schema_and_database_inputs_are_bound_but_app_changes_are_not(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in cloud.DATABASE_FILES:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("database dependency\n")
            migrations = root / "server/migrations"
            migrations.mkdir(parents=True)
            up = migrations / "000001_initial.up.sql"
            up.write_text("CREATE TABLE example (id int);\n")
            down = migrations / "000001_initial.down.sql"
            down.write_text("DROP TABLE example;\n")
            initial = cloud.metadata(root)
            self.assertEqual(initial["schema_version"], 1)
            self.assertEqual(initial["format"], 1)
            (root / "server/main.go").write_text("changed app\n")
            self.assertEqual(cloud.metadata(root), initial)
            down.write_text("DROP TABLE example CASCADE;\n")
            changed = cloud.metadata(root)
            self.assertNotEqual(
                changed["migrations_sha256"], initial["migrations_sha256"]
            )
            self.assertEqual(
                changed["database_profile_sha256"], initial["database_profile_sha256"]
            )
            (root / cloud.DATABASE_FILES[0]).write_text("new postgres\n")
            self.assertNotEqual(
                cloud.metadata(root)["database_profile_sha256"],
                initial["database_profile_sha256"],
            )
            up.unlink()
            with self.assertRaises(ValueError):
                cloud.metadata(root)


if __name__ == "__main__":
    unittest.main()
