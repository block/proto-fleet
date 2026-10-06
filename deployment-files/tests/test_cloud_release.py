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
            migrations = root / "server/migrations/current"
            migrations.mkdir(parents=True)
            up = migrations / "000001_initial.up.sql"
            up.write_text("CREATE TABLE example (id int);\n")
            down = migrations / "000001_initial.down.sql"
            down.write_text("DROP TABLE example;\n")
            initial = cloud.metadata(root)
            self.assertEqual(initial["schema_version"], 1)
            self.assertEqual(set(initial), {"schema_version", "compatibility_sha256"})
            (root / "server/main.go").write_text("changed app\n")
            self.assertEqual(cloud.metadata(root), initial)
            down.write_text("DROP TABLE example CASCADE;\n")
            changed = cloud.metadata(root)
            self.assertNotEqual(
                changed["compatibility_sha256"], initial["compatibility_sha256"]
            )
            for name in cloud.DATABASE_FILES:
                with self.subTest(input=name):
                    path = root / name
                    original = path.read_text()
                    path.write_text("changed database input\n")
                    self.assertNotEqual(cloud.metadata(root), changed)
                    path.write_text(original)
            legacy = root / "server/migrations/000999_retained.up.sql"
            legacy.write_text("legacy SQL\n")
            with_legacy = cloud.metadata(root)
            self.assertEqual(with_legacy["schema_version"], 1)
            self.assertNotEqual(with_legacy, changed)
            legacy.unlink()
            assertion = (
                root / "server/internal/infrastructure/db/baseline/assertions.json"
            )
            assertion.parent.mkdir(parents=True)
            assertion.write_text('{"schema": "checked"}\n')
            self.assertNotEqual(cloud.metadata(root), changed)
            assertion.unlink()
            bridge = migrations.parent / "bridges/000001_bridge.sql"
            bridge.parent.mkdir()
            bridge.write_text("SELECT 1;\n")
            with_bridge = cloud.metadata(root)
            self.assertNotEqual(with_bridge, changed)
            bridge.write_text("SELECT 2;\n")
            self.assertNotEqual(cloud.metadata(root), with_bridge)
            bridge.unlink()
            self.assertEqual(cloud.metadata(root), changed)
            up.unlink()
            with self.assertRaises(ValueError):
                cloud.metadata(root)

    def test_real_tree_inputs_exist(self):
        result = cloud.metadata(SCRIPT.parents[2])
        self.assertGreater(result["schema_version"], 0)
        self.assertEqual(len(result["compatibility_sha256"]), 64)


if __name__ == "__main__":
    unittest.main()
