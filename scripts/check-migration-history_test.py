#!/usr/bin/env python3
"""Exercise migration policy against real, disposable Git histories; no database."""

import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

CHECKER = pathlib.Path(__file__).with_name("check-migration-history.py")
CURRENT = "server/migrations/current/"
ASSERTIONS = "server/internal/infrastructure/db/baseline/assertions.json"


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

    def test_public_gaps_are_valid_without_private_repository(self):
        self.pair("001003_shared_next")
        self.check("--public-only")

    def test_internal_prerequisite_and_public_prefix(self):
        self.pair("001001_internal_private", "-- requires-shared: 1000\nSELECT 1;\n")
        self.pair("001002_shared_pending")
        self.check("--public-ref", self.base)

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
        ):
            with self.subTest(declaration=declaration):
                self.pair("001001_internal_next", declaration)
                self.check(error="requires-shared")

    def test_public_rejects_private_sql(self):
        self.pair("001001_internal_next", "-- requires-shared: 1000\n")
        self.check("--public-only", error="private migration")

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

    def test_public_shared_sql_must_match_bytes(self):
        self.pair("001002_shared_promoted", "SELECT 2;\n")
        public = self.commit()
        self.write(CURRENT + "001002_shared_promoted.up.sql", "SELECT 3;\n")
        self.check("--public-ref", public, error="public shared migration differs")

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

    def test_duplicate_prerequisite_is_refused(self):
        self.pair(
            "001001_internal_next",
            "-- requires-shared: 1000\n-- requires-shared: 1000\n",
        )
        self.check(error="requires-shared")

    def assertions(self, target, admissions=None, targets=None):
        self.write(
            ASSERTIONS,
            json.dumps(
                {
                    "target": target,
                    "admissions": admissions or {},
                    "targets": targets or ["fixture-catalog"],
                }
            ),
        )

    def test_initial_assertions_match_active_target(self):
        self.assertions(1000)
        self.check()

    def test_new_migration_requires_updated_assertion_target(self):
        self.assertions(1000)
        self.base = self.commit()
        self.pair("001003_shared_next")
        self.check(error="assertion target must equal active migration version 1003")

    def test_next_assertions_retain_previous_release_catalog_admission(self):
        self.assertions(1000, targets=["fresh", "upgraded"])
        self.base = self.commit()
        self.pair("001003_shared_next")
        self.assertions(1003, {"1000": ["fresh", "upgraded"]})
        self.check()

    def test_missing_previous_release_admission_is_refused(self):
        self.assertions(1000, targets=["fresh", "upgraded"])
        self.base = self.commit()
        self.pair("001003_shared_next")
        for admissions in ({}, {"1000": ["fresh"]}, {"1000": ["wrong"]}):
            with self.subTest(admissions=admissions):
                self.assertions(1003, admissions)
                self.check(
                    error="retain previous release catalog admissions for version 1000"
                )

    def test_existing_assertions_cannot_be_deleted(self):
        self.assertions(1000)
        self.base = self.commit()
        (self.repo / ASSERTIONS).unlink()
        self.check(error="schema assertions cannot be deleted")


if __name__ == "__main__":
    unittest.main()
