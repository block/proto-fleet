#!/usr/bin/env python3
"""Exercise migration policy against real, disposable Git histories; no database."""

import hashlib
import importlib.util
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

CHECKER = pathlib.Path(__file__).with_name("check-migration-history.py")
CURRENT = "server/migrations/current/"
LATEST = "server/migrations/LATEST"
SPEC = importlib.util.spec_from_file_location("migration_history", CHECKER)
HISTORY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HISTORY)


class MigrationHistoryTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.repo = pathlib.Path(self.temp.name)
        self.git("init", "-q")
        self.git("config", "user.name", "Migration test")
        self.git("config", "user.email", "migration-test@example.invalid")
        self.pair("000149_legacy", directory="server/migrations/")
        self.pair("001000_shared_baseline")
        self.base = self.commit()

    def git(self, *args):
        return subprocess.check_output(
            ["git", "-C", str(self.repo), *args], text=True
        ).strip()

    def write(self, path, body):
        target = self.repo / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(body)

    def pair(self, name, body="SELECT 1;\n", directory=CURRENT):
        for direction in ("up", "down"):
            self.write(f"{directory}{name}.{direction}.sql", body)
        if directory == CURRENT:
            latest = max((self.repo / CURRENT).glob("*.up.sql"))
            self.write(LATEST, latest.name.removesuffix(".up.sql") + "\n")

    def commit(self):
        self.git("add", ".")
        tree = self.git("write-tree")
        commit = self.git("commit-tree", tree, "-m", "fixture")
        self.git("update-ref", "HEAD", commit)
        return commit

    def check(self, *args, error=None):
        result = subprocess.run(
            [
                sys.executable,
                str(CHECKER),
                "--repo",
                str(self.repo),
                "--base-ref",
                self.base,
                *args,
            ],
            text=True,
            capture_output=True,
            check=False,
        )
        if error is None:
            self.assertEqual(result.returncode, 0, result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(error, result.stderr)

    def test_version_gaps_are_valid(self):
        self.pair("001003_shared_next")
        self.check()

    def test_internal_prerequisite_and_public_prefix(self):
        self.pair("001001_internal_private", "-- requires-shared: 1000\nSELECT 1;\n")
        self.pair("001002_shared_pending")
        self.pair("001003_internal_checkpoint", "-- requires-shared: 1002\nSELECT 1;\n")
        self.check("--public-ref", self.base)

    def test_latest_marker_missing_stale_or_wrong_name(self):
        self.pair("001001_shared_next")
        for marker in (None, "001000_shared_baseline\n", "001001_shared_other\n"):
            with self.subTest(marker=marker):
                if marker is None:
                    (self.repo / LATEST).unlink()
                else:
                    self.write(LATEST, marker)
                self.check(error="LATEST")
        self.write(LATEST, "001001_shared_next\n")
        self.check()

    def test_missing_direction(self):
        self.write(CURRENT + "001001_shared_next.up.sql", "SELECT 1;")
        self.check(error="paired up/down")

    def test_duplicate_number_with_different_ownership(self):
        self.pair("001001_shared_next")
        self.pair("001001_internal_other", "-- requires-shared: 1000\n")
        self.check(error="duplicate migration version")

    def test_mismatched_pair_names(self):
        self.write(CURRENT + "001001_shared_a.up.sql", "SELECT 1;")
        self.write(CURRENT + "001001_shared_b.down.sql", "SELECT 1;")
        self.check(error="paired up/down")

    def test_private_requires_existing_earlier_shared(self):
        for declaration in (
            "",
            "-- requires-shared: 1002\n",
            "-- requires-shared: 999\n",
            "-- requires-shared: 1000\n-- requires-shared: 1000\n",
        ):
            with self.subTest(declaration=declaration):
                self.pair("001001_internal_next", declaration)
                self.check(error="requires-shared")

    def test_immutable_sql_changed_deleted_or_renamed(self):
        for relative in (
            "server/migrations/000149_legacy.up.sql",
            CURRENT + "001000_shared_baseline.up.sql",
        ):
            original = (self.repo / relative).read_text()
            with self.subTest(relative=relative, mutation="change"):
                self.write(relative, original + "-- changed\n")
                self.check(error="immutable migration")
            self.write(relative, original)
            with self.subTest(relative=relative, mutation="delete"):
                (self.repo / relative).unlink()
                self.check(error="immutable migration")
            self.write(relative, original)
            with self.subTest(relative=relative, mutation="rename"):
                (self.repo / relative).rename(self.repo / (relative + ".renamed"))
                self.check(error="immutable migration")
            self.write(relative, original)

    def test_retroactive_insertion_below_merged_high_water(self):
        self.pair("001005_shared_merged")
        self.base = self.commit()
        self.pair("001004_shared_late")
        self.check(error="high-water")

    def test_repair_exception_requires_exact_path_and_both_revisions(self):
        name = "001016_internal_ct1_metric_summaries"
        path = CURRENT + name + ".up.sql"
        original = "-- requires-shared: 1000\nSELECT 1;\n"
        repaired = "-- requires-shared: 1000\nSELECT 2;\n"
        self.pair(name, original)
        self.base = self.commit()
        exception = (
            hashlib.sha256(original.encode()).hexdigest(),
            hashlib.sha256(repaired.encode()).hexdigest(),
        )
        with mock.patch.dict(
            HISTORY.REPAIRED_MIGRATIONS, {path: exception}, clear=True
        ):
            self.write(path, repaired)
            HISTORY.check(self.repo, self.base)
            for body in (original + "-- unexpected\n", repaired + "-- unexpected\n"):
                self.write(path, body)
                with self.assertRaisesRegex(ValueError, "immutable migration"):
                    HISTORY.check(self.repo, self.base)
            self.write(path, repaired)
            (self.repo / path).unlink()
            with self.assertRaisesRegex(ValueError, "immutable migration"):
                HISTORY.check(self.repo, self.base)
            self.write(path, repaired)
            down = CURRENT + name + ".down.sql"
            self.write(down, repaired)
            with self.assertRaisesRegex(ValueError, "immutable migration"):
                HISTORY.check(self.repo, self.base)
            self.write(down, original)

            # A previously repaired base cannot be reverted or edited again.
            self.base = self.commit()
            HISTORY.check(self.repo, self.base)
            self.write(path, original)
            with self.assertRaisesRegex(ValueError, "immutable migration"):
                HISTORY.check(self.repo, self.base)

            # The exception cannot replace an unknown earlier revision.
            self.write(path, original + "-- different history\n")
            self.base = self.commit()
            self.write(path, repaired)
            with self.assertRaisesRegex(ValueError, "immutable migration"):
                HISTORY.check(self.repo, self.base)

    def test_public_shared_sql_must_match_bytes(self):
        self.pair("001002_shared_promoted", "SELECT 2;\n")
        public = self.commit()
        self.write(CURRENT + "001002_shared_promoted.up.sql", "SELECT 3;\n")
        self.check("--public-ref", public, error="public shared migration differs")

    def test_collision_repair_allows_only_exact_renamed_pair(self):
        old = CURRENT + "001026_internal_container_telemetry_compression"
        new = CURRENT + "001027_internal_container_telemetry_compression"
        body = "-- requires-shared: 1000\nSELECT 1;\n"
        self.pair("001026_internal_fleet_node_details", body)
        before_collision = self.commit()
        self.pair("001026_internal_container_telemetry_compression", body)
        self.base = self.commit()
        renames = {
            f"{old}.{direction}.sql": (
                f"{new}.{direction}.sql",
                hashlib.sha256(body.encode()).hexdigest(),
            )
            for direction in ("up", "down")
        }
        with mock.patch.object(HISTORY, "RENAMED_MIGRATIONS", renames, create=True):
            # The broken merged history still fails until both files move.
            with self.assertRaisesRegex(ValueError, "duplicate migration version"):
                HISTORY.check(self.repo, self.base)
            for source, (target, _) in renames.items():
                (self.repo / source).rename(self.repo / target)
            self.write(LATEST, new.removeprefix(CURRENT) + "\n")
            HISTORY.check(self.repo, self.base)
            HISTORY.check(self.repo, before_collision)

            for source, (target, _) in renames.items():
                with self.subTest(target=target, mutation="change"):
                    self.write(target, body + "-- changed\n")
                    with self.assertRaisesRegex(ValueError, "immutable migration"):
                        HISTORY.check(self.repo, self.base)
                self.write(target, body)
                with self.subTest(target=target, mutation="delete"):
                    (self.repo / target).unlink()
                    with self.assertRaisesRegex(ValueError, "immutable migration"):
                        HISTORY.check(self.repo, self.base)
                self.write(target, body)
                with self.subTest(target=target, mutation="retain original"):
                    self.write(source, body)
                    with self.assertRaisesRegex(
                        ValueError, "duplicate migration version"
                    ):
                        HISTORY.check(self.repo, self.base)
                (self.repo / source).unlink()

            # Once repaired, the renamed files are immutable as usual.
            repaired_base = self.commit()
            HISTORY.check(self.repo, repaired_base)
            self.write(new + ".up.sql", body + "-- changed again\n")
            with self.assertRaisesRegex(ValueError, "immutable migration"):
                HISTORY.check(self.repo, repaired_base)
            self.write(new + ".up.sql", body)

            # Never accept unknown original SQL under the allowlisted name.
            for source, (target, _) in renames.items():
                (self.repo / target).rename(self.repo / source)
            self.write(old + ".up.sql", body + "-- unknown original\n")
            unknown_base = self.commit()
            for source, (target, _) in renames.items():
                (self.repo / source).rename(self.repo / target)
                self.write(target, body)
            with self.assertRaisesRegex(ValueError, "immutable migration"):
                HISTORY.check(self.repo, unknown_base)

    def test_topology_collision_repair_uses_pinned_repository_sql(self):
        old = CURRENT + "001057_internal_bound_topology_reference_planning"
        new = CURRENT + "001058_internal_bound_topology_reference_planning"
        self.pair(
            "001057_internal_device_write_outcomes",
            "-- requires-shared: 1000\nSELECT 1;\n",
        )
        before_collision = self.commit()
        for direction in ("up", "down"):
            target = f"{new}.{direction}.sql"
            self.write(
                f"{old}.{direction}.sql", (CHECKER.parent.parent / target).read_text()
            )
        self.base = self.commit()
        self.check(error="duplicate migration version")

        for direction in ("up", "down"):
            (self.repo / f"{old}.{direction}.sql").rename(
                self.repo / f"{new}.{direction}.sql"
            )
        self.write(LATEST, new.removeprefix(CURRENT) + "\n")
        self.check()
        HISTORY.check(self.repo, before_collision)

        for direction in ("up", "down"):
            path = f"{new}.{direction}.sql"
            original = (self.repo / path).read_text()
            with self.subTest(direction=direction):
                self.write(path, original + "-- unapproved change\n")
                self.check(error="immutable migration")
                self.write(path, original)

    def test_public_shared_files_cannot_skip_internal_shared_migration(self):
        self.pair("001002_shared_promoted")
        public = self.commit()
        self.pair("001001_shared_missing_public")
        self.check("--public-ref", public, error="ordered prefix")

    def test_missing_base_fails_closed(self):
        self.base = "nonexistent-base"
        self.check(error="cannot read Git revision")

    def test_new_legacy_migrations_are_refused(self):
        self.pair("000150_new_legacy", directory="server/migrations/")
        self.check(error="new migrations belong in")

    def test_active_numbers_cannot_overlap_legacy_range(self):
        self.pair("000150_shared_wrong_range")
        self.check(error="reserved for legacy")

    def test_empty_active_source_is_refused(self):
        for path in (self.repo / CURRENT).glob("*.sql"):
            path.unlink()
        self.base = self.commit()
        self.check(error="active migration source is empty")

    def test_internal_release_requires_private_checkpoint(self):
        self.pair("001001_internal_private", "-- requires-shared: 1000\nSELECT 1;\n")
        self.pair("001002_shared_next")
        self.check(error="private checkpoint")
        self.pair("001003_internal_checkpoint", "-- requires-shared: 1002\nSELECT 1;\n")
        self.check()


if __name__ == "__main__":
    unittest.main()
