#!/usr/bin/env python3
"""Executable tests for trusted Codex benchmark sharding."""

from __future__ import annotations

import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parents[1]
sys.path.insert(0, str(SCRIPT_DIR))

import aggregate_codex_shard_results as aggregate
import evaluate_review_policy_test as workflow_test_helpers
import plan_codex_review_shards as planner
import render_codex_shard_prompt as prompt_renderer
import verify_codex_shard_plan as verifier
import write_codex_shard_result as writer


def file_diff(
    path: str,
    size: int = 100,
    lines: int = 4,
    *,
    shared: bool | None = None,
) -> planner.FileDiff:
    domain = planner.classify_path(path)
    is_shared = planner.is_shared_contract(path, domain) if shared is None else shared
    line_count = min(max(lines, 1), max(1, (size + 1) // 2))
    prefix = b"+\n" * (line_count - 1)
    patch = prefix + b"+" + b"x" * (size - len(prefix) - 1)
    return planner.FileDiff(
        path,
        domain,
        planner.semantic_unit(path, domain, is_shared),
        is_shared,
        patch,
    )


def case_metadata() -> dict:
    return {
        "pr": 1,
        "purpose": "test",
        "expected": "NONE",
        "source-run": "https://example.test/run",
        "source-comment": "",
    }


def signed_manifest(*, active_second: bool = True, status: str = "planned") -> dict:
    shards = [
        {
            "id": "shard-1",
            "active": True,
            "primary_files": ["server/a/a.go"],
            "shared_files": [],
            "packet_bytes": 100,
            "packet_lines": 4,
            "packet_files": 1,
        },
        {
            "id": "shard-2",
            "active": active_second,
            "primary_files": ["client/src/protoFleet/a.ts"] if active_second else [],
            "shared_files": [],
            "packet_bytes": 100 if active_second else 0,
            "packet_lines": 4 if active_second else 0,
            "packet_files": 1 if active_second else 0,
        },
    ]
    files = [
        {
            "path": "server/a/a.go",
            "domain": "server",
            "semantic_unit": "primary:server:server/a",
            "shared_contract": False,
            "primary_shard": "shard-1",
            "diff_bytes": 100,
            "diff_lines": 4,
            "changed_line_ranges": [[1, 4]],
            "citation_side": "head",
        }
    ]
    if active_second:
        files.append(
            {
                "path": "client/src/protoFleet/a.ts",
                "domain": "protofleet",
                "semantic_unit": "primary:protofleet:client/src/protoFleet",
                "shared_contract": False,
                "primary_shard": "shard-2",
                "diff_bytes": 100,
                "diff_lines": 4,
                "changed_line_ranges": [[1, 4]],
                "citation_side": "head",
            }
        )
    manifest = {
        "schema_version": 1,
        "case": "pr-test",
        "variant": "unified-40",
        "repeat": "initial",
        "base_sha": "a" * 40,
        "merge_base_sha": "c" * 40,
        "head_sha": "b" * 40,
        "commit_range": f"{'a' * 40}...{'b' * 40}",
        "unified": 40,
        "inter_hunk_context": 0,
        "variant_metadata": {"id": "unified-40", "unified": 40, "inter-hunk": 0},
        "status": status,
        "oversized_reasons": ["test"] if status == "oversized" else [],
        "limits": {
            "max_packets": 2,
            "max_packet_bytes": 500_000,
            "max_packet_lines": 12_500,
            "max_semantic_units": 750,
            "max_search_states": 2_000,
        },
        "files": files,
        "shards": shards,
    }
    canonical = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
    manifest["manifest_digest"] = hashlib.sha256(canonical).hexdigest()
    return manifest


def completed_result(manifest: dict, shard_id: str, risk: str = "NONE") -> dict:
    shard = next(item for item in manifest["shards"] if item["id"] == shard_id)
    location_path = shard["primary_files"][0] if shard["primary_files"] else ""
    location_url = (
        f"https://github.com/block/proto-fleet/blob/{manifest['head_sha']}/"
        f"{location_path}#L1"
    )
    if risk == "NONE":
        findings = "No concrete security, correctness, or reliability issues found."
    else:
        findings = "\n".join(
            (
                f"#### [{risk}] Test issue",
                "- **Category**: Reliability",
                f"- **Location**: [`{location_path}:1`]({location_url})",
                "- **Description**: Concrete test issue.",
                "- **Impact**: Concrete test impact.",
                "- **Recommendation**: Fix the test issue.",
            )
        )
    markdown = (
        "## Review Summary\n\n"
        f"**Overall Risk**: {risk}\n\n"
        "### Findings\n\n"
        f"{findings}\n\n"
        "### Notes\n\nTest note.\n"
    )
    return {
        "schema_version": 1,
        "case": manifest["case"],
        "variant": manifest["variant"],
        "repeat": manifest["repeat"],
        "base_sha": manifest["base_sha"],
        "head_sha": manifest["head_sha"],
        "commit_range": manifest["commit_range"],
        "manifest_digest": manifest["manifest_digest"],
        "run_id": 123,
        "run_attempt": 1,
        "shard_id": shard_id,
        "status": "completed",
        "incomplete_reason": None,
        "elapsed_seconds": 30,
        "primary_files": shard["primary_files"],
        "shared_files": shard["shared_files"],
        "packet_bytes": shard["packet_bytes"],
        "packet_lines": shard["packet_lines"],
        "review": {"overall_risk": risk, "review_markdown": markdown},
    }


class PlannerTest(unittest.TestCase):
    def test_changed_ranges_include_only_added_lines(self):
        patch = b"""diff --git a/server/a.go b/server/a.go
--- a/server/a.go
+++ b/server/a.go
@@ -10,4 +10,5 @@
 context
-old
+replacement
 context
+separate
"""
        diff = planner.FileDiff(
            "server/a.go",
            "server",
            "primary:server:server",
            False,
            patch,
        )
        self.assertEqual(diff.changed_line_ranges, [[11, 11], [13, 13]])
        self.assertFalse(
            any(start <= 10 <= end for start, end in diff.changed_line_ranges)
        )
        self.assertFalse(
            any(start <= 12 <= end for start, end in diff.changed_line_ranges)
        )

    def test_hunkless_diff_has_no_valid_finding_location(self):
        diff = planner.FileDiff(
            "server/renamed.go",
            "server",
            "primary:server:server",
            False,
            b"""diff --git a/server/old.go b/server/renamed.go
similarity index 100%
rename from server/old.go
rename to server/renamed.go
""",
        )
        self.assertEqual(diff.changed_line_ranges, [])

    def test_deletion_only_hunk_uses_nearest_surviving_line(self):
        patch = b"""diff --git a/server/a.go b/server/a.go
--- a/server/a.go
+++ b/server/a.go
@@ -10,3 +10,2 @@
 context before
-deleted
 context after
"""
        diff = planner.FileDiff(
            "server/a.go",
            "server",
            "primary:server:server",
            False,
            patch,
        )
        self.assertEqual(diff.changed_line_ranges, [[11, 11]])

        no_surviving_lines = planner.FileDiff(
            "server/deleted.go",
            "server",
            "primary:server:server",
            False,
            b"@@ -1 +0,0 @@\n-deleted\n",
        )
        self.assertEqual(no_surviving_lines.changed_line_ranges, [[1, 1]])

    def test_truncation_to_empty_tracks_merge_base_removed_lines(self):
        patch = b"""diff --git a/server/empty.go b/server/empty.go
--- a/server/empty.go
+++ b/server/empty.go
@@ -1,2 +0,0 @@
-package server
-var Removed = true
"""
        diff = planner.FileDiff(
            "server/empty.go",
            "server",
            "primary:server:server",
            False,
            patch,
        )
        self.assertTrue(diff.is_truncated_to_empty)
        self.assertEqual(diff.citation_side, "merge-base")
        self.assertEqual(diff.changed_line_ranges, [[1, 2]])

    def test_whole_file_deletion_tracks_base_side_removed_lines(self):
        patch = b"""diff --git a/server/deleted.go b/server/deleted.go
--- a/server/deleted.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package server
-
-var Deleted = true
"""
        diff = planner.FileDiff(
            "server/deleted.go",
            "server",
            "primary:server:server",
            False,
            patch,
        )
        self.assertTrue(diff.is_whole_file_deletion)
        self.assertEqual(diff.citation_side, "merge-base")
        self.assertEqual(diff.changed_line_ranges, [[1, 3]])

    def test_path_ownership_is_first_match_and_disjoint(self):
        cases = {
            "server/monitoring/grafana/rule.yml": "delivery",
            "server/internal/domain/service.go": "server",
            "plugin/asicrs/Dockerfile": "delivery",
            "plugin/asicrs/src/main.rs": "asicrs",
            "plugin/driver/generated/device.pb.go": "contracts",
            "server/sdk/v1/pb/driver.proto": "contracts",
            "sdk/rust/proto-fleet-plugin/src/lib.rs": "rust-sdk",
            "client/src/shared/api.ts": "client-shared",
            "client/src/protoFleet/page.tsx": "protofleet",
            "unknown/thing.txt": "cross-cutting",
        }
        self.assertEqual({path: planner.classify_path(path) for path in cases}, cases)

    def test_all_client_shared_contracts_are_replicated(self):
        files = [
            file_diff("client/package.json", 100, 1),
            file_diff("client/src/protoFleet/page.tsx", 300_000, 10),
            file_diff("client/src/protoOS/page.tsx", 300_000, 10),
        ]
        self.assertTrue(
            planner.is_shared_contract(
                "client/package.json", planner.classify_path("client/package.json")
            )
        )
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "planned")
        self.assertIn("client/package.json", plan["shards"][0]["primary_files"])
        self.assertIn("client/package.json", plan["shards"][1]["shared_files"])

    def test_github_workflows_and_scripts_are_one_automation_surface(self):
        workflow_path = ".github/workflows/review.yml"
        script_path = ".github/scripts/review.py"
        files = [
            file_diff(workflow_path, 200_000, 100),
            file_diff(script_path, 200_000, 100),
            file_diff("server/a/a.go", 300_000, 100),
        ]
        self.assertEqual(files[0].unit, files[1].unit)
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "planned")
        owner = next(
            shard["id"]
            for shard in plan["shards"]
            if workflow_path in shard["primary_files"]
        )
        self.assertIn(
            script_path,
            next(shard for shard in plan["shards"] if shard["id"] == owner)[
                "primary_files"
            ],
        )

    def test_deployment_and_root_runtime_contracts_are_replicated(self):
        files = [
            file_diff("deployment-files/docker-compose.yaml", 100, 1),
            file_diff("server/a/a.go", 300_000, 10),
            file_diff("client/src/protoFleet/page.tsx", 300_000, 10),
        ]
        self.assertTrue(
            planner.is_shared_contract(
                "deployment-files/docker-compose.yaml", "delivery"
            )
        )
        self.assertTrue(planner.is_shared_contract(".dockerignore", "delivery"))
        self.assertTrue(planner.is_shared_contract("lefthook.yml", "delivery"))
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "planned")
        self.assertIn(
            "deployment-files/docker-compose.yaml", plan["shards"][1]["shared_files"]
        )

    def test_generated_api_contract_stays_with_affected_consumer_packet(self):
        generated_path = "client/src/protoFleet/api/generated/example/v1/example.pb.ts"
        consumer_path = "client/src/protoFleet/page.tsx"
        files = [
            file_diff(generated_path, 100, 1),
            file_diff(consumer_path, 300_000, 10),
            file_diff("server/a/a.go", 300_000, 10),
        ]
        self.assertTrue(
            planner.is_shared_contract(
                generated_path, planner.classify_path(generated_path)
            )
        )
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "planned")
        consumer_shard = next(
            shard for shard in plan["shards"] if consumer_path in shard["primary_files"]
        )
        self.assertIn(
            generated_path,
            consumer_shard["primary_files"] + consumer_shard["shared_files"],
        )

    def test_asicrs_packet_receives_proto_and_rust_sdk_contracts(self):
        proto_path = "server/sdk/v1/pb/driver.proto"
        rust_path = "sdk/rust/proto-fleet-plugin/src/lib.rs"
        asicrs_path = "plugin/asicrs/src/main.rs"
        files = [
            file_diff(proto_path, 100, 1),
            file_diff(rust_path, 100, 1),
            file_diff(asicrs_path, 300_000, 100),
            file_diff("server/internal/app/app.go", 300_000, 100),
        ]
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "planned")
        asicrs_shard = next(
            shard for shard in plan["shards"] if asicrs_path in shard["primary_files"]
        )
        self.assertIn(
            proto_path, asicrs_shard["primary_files"] + asicrs_shard["shared_files"]
        )
        self.assertIn(
            rust_path, asicrs_shard["primary_files"] + asicrs_shard["shared_files"]
        )

    def test_review_wide_plan_has_exactly_one_owner_and_replicates_shared_context(self):
        files = [
            file_diff("proto/fleet/v1/service.proto", shared=True),
            file_diff("server/a/a.go"),
            file_diff("client/src/protoFleet/features/fleet/page.tsx"),
        ]
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "planned")
        owners = [path for shard in plan["shards"] for path in shard["primary_files"]]
        self.assertCountEqual(owners, [file.path for file in files])
        self.assertEqual(len(owners), len(set(owners)))
        self.assertIn("proto/fleet/v1/service.proto", plan["shards"][1]["shared_files"])
        self.assertLessEqual(len(plan["shards"]), 2)
        for shard in plan["shards"]:
            self.assertLessEqual(shard["packet_bytes"], planner.MAX_PACKET_BYTES)
            self.assertLessEqual(shard["packet_lines"], planner.MAX_PACKET_LINES)

    def test_one_semantic_unit_uses_one_active_shard(self):
        plan = planner.plan_files([file_diff("server/a/a.go")])
        self.assertTrue(plan["shards"][0]["active"])
        self.assertFalse(plan["shards"][1]["active"])

    def test_single_oversized_unit_is_rejected(self):
        plan = planner.plan_files(
            [file_diff("server/a/a.go", planner.MAX_PACKET_BYTES + 1, 10)]
        )
        self.assertEqual(plan["status"], "oversized")
        self.assertIn("do not fit", plan["oversized_reasons"][-1])

    def test_complete_search_finds_non_greedy_two_packet_assignment(self):
        sizes = (266_666, 233_333, 200_000, 166_667, 133_333)
        files = [
            file_diff(f"server/package{index}/file.go", size, 1)
            for index, size in enumerate(sizes)
        ]
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "planned")
        self.assertEqual(
            sorted(shard["packet_bytes"] for shard in plan["shards"]),
            [499_999, 500_000],
        )

    def test_partition_search_state_limit_fails_closed(self):
        sizes = [
            52_027,
            31_068,
            42_390,
            68_417,
            35_777,
            53_823,
            30_840,
            53_063,
            45_016,
            56_589,
            64_864,
            62_834,
            33_914,
            48_854,
            62_425,
            45_294,
            65_662,
            61_707,
            41_638,
            43_798,
        ]
        files = [
            file_diff(f"server/package{index}/file.go", size, 1)
            for index, size in enumerate(sizes)
        ]
        with mock.patch.object(planner, "MAX_SEARCH_STATES", 100):
            plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "oversized")
        self.assertTrue(
            any(
                "planner search exceeds trusted state limit" in reason
                for reason in plan["oversized_reasons"]
            )
        )

    def test_more_than_two_bounded_units_is_rejected(self):
        files = [
            file_diff(f"server/package{index}/file.go", 300_000, 1_000)
            for index in range(3)
        ]
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "oversized")

    def test_impossible_global_shared_context_short_circuits(self):
        files = [
            file_diff(f"proto/package{index}/service.proto", 30_000, 100, shared=True)
            for index in range(20)
        ]
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "oversized")
        self.assertIn(
            "globally replicated shared context", plan["oversized_reasons"][0]
        )

    def test_impossible_audience_shared_context_short_circuits(self):
        files = [
            file_diff(
                f"client/src/shared/package{index}/api.ts",
                30_000,
                100,
                shared=True,
            )
            for index in range(17)
        ] + [file_diff("client/src/protoFleet/page.tsx", 1, 1)]
        with mock.patch.object(
            planner,
            "find_bounded_assignment",
            side_effect=AssertionError("bounded search must not run"),
        ):
            plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "oversized")
        self.assertIn(
            "shared context for protofleet audience",
            plan["oversized_reasons"][0],
        )

    def test_semantic_unit_safety_limit_fails_closed_without_recursion(self):
        files = [
            file_diff(f"server/package{index}/file.go", 1, 1)
            for index in range(planner.MAX_SEMANTIC_UNITS + 1)
        ]
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "oversized")
        self.assertTrue(
            any(
                "planner safety limit" in reason for reason in plan["oversized_reasons"]
            )
        )

    def test_replicated_shared_context_is_bounded(self):
        plan = planner.plan_files(
            [
                file_diff(
                    "proto/fleet/v1/service.proto",
                    planner.MAX_PACKET_BYTES + 1,
                    10,
                    shared=True,
                )
            ]
        )
        self.assertEqual(plan["status"], "oversized")
        self.assertIn("shared context", plan["oversized_reasons"][0])

    def test_exact_packet_limits_are_accepted(self):
        plan = planner.plan_files(
            [
                file_diff(
                    "server/a/a.go",
                    planner.MAX_PACKET_BYTES,
                    planner.MAX_PACKET_LINES,
                )
            ]
        )
        self.assertEqual(plan["status"], "planned")

    def test_downloaded_packet_is_remeasured_against_manifest(self):
        files = [file_diff("server/a/a.go")]
        plan = planner.plan_files(files)
        manifest = {
            "schema_version": 1,
            "case": "pr-test",
            "variant": "unified-40",
            "repeat": "initial",
            "base_sha": "a" * 40,
            "head_sha": "b" * 40,
            "commit_range": f"{'a' * 40}...{'b' * 40}",
            "unified": 40,
            "inter_hunk_context": 0,
            "variant_metadata": {},
            **plan,
        }
        canonical = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
        manifest["manifest_digest"] = hashlib.sha256(canonical).hexdigest()
        verifier.verify_plan(manifest, "shard-1", files[0].patch)
        with self.assertRaisesRegex(ValueError, "metrics disagree"):
            verifier.verify_plan(manifest, "shard-1", files[0].patch + b"tampered")

    def test_git_integration_writes_exact_shard_packet(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory)
            subprocess.run(("git", "init", "-q"), cwd=repo, check=True)
            subprocess.run(
                ("git", "config", "user.email", "test@example.test"),
                cwd=repo,
                check=True,
            )
            subprocess.run(("git", "config", "user.name", "Test"), cwd=repo, check=True)
            (repo / "server/a").mkdir(parents=True)
            (repo / "server/a/old.go").write_text("package a\n", encoding="utf-8")
            subprocess.run(("git", "add", "."), cwd=repo, check=True)
            subprocess.run(("git", "commit", "-qm", "base"), cwd=repo, check=True)
            base = subprocess.check_output(
                ("git", "rev-parse", "HEAD"), cwd=repo, text=True
            ).strip()
            subprocess.run(
                ("git", "mv", "server/a/old.go", "server/a/new.go"),
                cwd=repo,
                check=True,
            )
            (repo / "server/a/new.go").write_text(
                "package a\nvar A = 1\n", encoding="utf-8"
            )
            subprocess.run(("git", "commit", "-qam", "head"), cwd=repo, check=True)
            head = subprocess.check_output(
                ("git", "rev-parse", "HEAD"), cwd=repo, text=True
            ).strip()
            args = type(
                "Args",
                (),
                {
                    "case": "pr-test",
                    "variant": "unified-40",
                    "repeat": "initial",
                    "base_sha": base,
                    "head_sha": head,
                    "commit_range": f"{base}...{head}",
                    "unified": 40,
                    "inter_hunk": 0,
                    "variant_metadata_json": json.dumps(
                        {"id": "unified-40", "unified": 40, "inter-hunk": 0}
                    ),
                },
            )()
            previous = Path.cwd()
            try:
                os.chdir(repo)
                manifest, patches = planner.build_plan(args)
            finally:
                os.chdir(previous)
            self.assertEqual(manifest["status"], "planned")
            self.assertNotIn("case_metadata", manifest)
            self.assertNotIn("expected", json.dumps(manifest))
            self.assertIn("server/a/new.go", patches)
            patch = patches["server/a/new.go"]
            self.assertIn(b"rename from server/a/old.go", patch)
            self.assertIn(b"rename to server/a/new.go", patch)
            self.assertIn(b"+var A = 1", patch)
            self.assertNotIn(b"--- /dev/null", patch)
            file_record = next(
                file for file in manifest["files"] if file["path"] == "server/a/new.go"
            )
            self.assertTrue(
                any(
                    start <= 2 <= end
                    for start, end in file_record["changed_line_ranges"]
                )
            )

    def test_whole_file_deletion_uses_three_dot_merge_base(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory)
            subprocess.run(("git", "init", "-q"), cwd=repo, check=True)
            subprocess.run(
                ("git", "config", "user.email", "test@example.test"),
                cwd=repo,
                check=True,
            )
            subprocess.run(("git", "config", "user.name", "Test"), cwd=repo, check=True)
            (repo / "server").mkdir()
            deleted = repo / "server/deleted.go"
            deleted.write_text("package server\n\nvar Old = true\n", encoding="utf-8")
            subprocess.run(("git", "add", "."), cwd=repo, check=True)
            subprocess.run(("git", "commit", "-qm", "common"), cwd=repo, check=True)
            merge_base = subprocess.check_output(
                ("git", "rev-parse", "HEAD"), cwd=repo, text=True
            ).strip()

            deleted.unlink()
            subprocess.run(("git", "commit", "-qam", "delete"), cwd=repo, check=True)
            head = subprocess.check_output(
                ("git", "rev-parse", "HEAD"), cwd=repo, text=True
            ).strip()

            subprocess.run(
                ("git", "checkout", "-qb", "advanced-base", merge_base),
                cwd=repo,
                check=True,
            )
            deleted.write_text("package server\n\nvar New = true\n", encoding="utf-8")
            subprocess.run(
                ("git", "commit", "-qam", "advance base"), cwd=repo, check=True
            )
            base = subprocess.check_output(
                ("git", "rev-parse", "HEAD"), cwd=repo, text=True
            ).strip()

            args = type(
                "Args",
                (),
                {
                    "case": "pr-test",
                    "variant": "unified-40",
                    "repeat": "initial",
                    "base_sha": base,
                    "head_sha": head,
                    "commit_range": f"{base}...{head}",
                    "unified": 40,
                    "inter_hunk": 0,
                    "variant_metadata_json": json.dumps(
                        {"id": "unified-40", "unified": 40, "inter-hunk": 0}
                    ),
                },
            )()
            previous = Path.cwd()
            try:
                os.chdir(repo)
                manifest, _ = planner.build_plan(args)
            finally:
                os.chdir(previous)

            self.assertNotEqual(base, merge_base)
            self.assertEqual(manifest["merge_base_sha"], merge_base)
            record = manifest["files"][0]
            self.assertEqual(record["path"], "server/deleted.go")
            self.assertEqual(record["citation_side"], "merge-base")
            self.assertEqual(record["changed_line_ranges"], [[1, 3]])


class PromptTest(unittest.TestCase):
    def test_sharded_prompt_preserves_exact_baseline_guidance(self):
        workflow_lines = (
            (REPO_ROOT / ".github/workflows/codex-security-review-benchmark.yml")
            .read_text()
            .splitlines()
        )
        start = workflow_lines.index("          prompt: |") + 1
        end = next(
            index
            for index, line in enumerate(workflow_lines)
            if index > start and "prompt_profile == 'bounded'" in line
        )
        baseline = "\n".join(
            line.removeprefix("            ") for line in workflow_lines[start:end]
        ).rstrip()
        replacements = {
            "${{ env.REVIEW_DIFF_FILE }}": "{{REVIEW_DIFF_FILE}}",
            "${{ env.REVIEW_HEAD_SHA }}": "{{REVIEW_HEAD_SHA}}",
            "${{ env.REVIEW_COMMIT_RANGE }}": "{{REVIEW_COMMIT_RANGE}}",
            "${{ env.REVIEW_BLOB_BASE_URL }}": "{{REVIEW_BLOB_BASE_URL}}",
        }
        for original, placeholder in replacements.items():
            baseline = baseline.replace(original, placeholder)
        template = (REPO_ROOT / ".github/codex-sharded-review-prompt.md").read_text()
        common, marker, _ = template.partition("## Trusted Shard Scope")
        self.assertEqual(marker, "## Trusted Shard Scope")
        self.assertEqual(common.rstrip(), baseline)

    def test_shard_prompt_is_complete_and_prohibits_full_diff_regeneration(self):
        template = (REPO_ROOT / ".github/codex-sharded-review-prompt.md").read_text()
        values = {
            name: f"trusted-{name.lower()}" for name in prompt_renderer.PLACEHOLDERS
        }
        rendered = prompt_renderer.render(template, values)
        self.assertNotIn("{{", rendered)
        self.assertIn("primary_files", rendered)
        self.assertIn("shared_files", rendered)
        self.assertIn("citation_side", rendered)
        self.assertIn(values["REVIEW_MERGE_BASE_BLOB_URL"], rendered)
        self.assertIn(
            "Do not regenerate or read the complete pull-request diff", rendered
        )

    def test_prompt_rejects_multiline_trusted_values(self):
        values = {name: "value" for name in prompt_renderer.PLACEHOLDERS}
        values["SHARD_ID"] = "shard-1\nuntrusted"
        with self.assertRaises(ValueError):
            prompt_renderer.render("{{SHARD_ID}}", values)


class ResultTest(unittest.TestCase):
    def setUp(self):
        self.original_env = {
            name: os.environ.get(name)
            for name in (
                "GITHUB_RUN_ID",
                "GITHUB_RUN_ATTEMPT",
                "GITHUB_REPOSITORY",
                "CODEX_MODEL",
                "CODEX_TIMEOUT_MINUTES",
                "CODEX_REASONING_EFFORT",
                "PROMPT_PROFILE",
            )
        }
        os.environ.update(
            GITHUB_RUN_ID="123",
            GITHUB_RUN_ATTEMPT="1",
            GITHUB_REPOSITORY="block/proto-fleet",
            CODEX_MODEL="gpt-test",
            CODEX_TIMEOUT_MINUTES="6",
            CODEX_REASONING_EFFORT="xhigh",
            PROMPT_PROFILE="baseline",
        )

    def tearDown(self):
        for name, value in self.original_env.items():
            if value is None:
                os.environ.pop(name, None)
            else:
                os.environ[name] = value

    def test_aggregate_preserves_completed_high_severity(self):
        manifest = signed_manifest()
        results = [
            completed_result(manifest, "shard-1", "HIGH"),
            completed_result(manifest, "shard-2", "NONE"),
        ]
        result, markdown, _ = aggregate.aggregate(manifest, results, case_metadata())
        self.assertEqual(result["benchmark_status"], "completed")
        self.assertEqual(result["review"]["overall_risk"], "HIGH")
        self.assertIn("#### [HIGH] Test issue", markdown)

    def test_validated_timeout_adds_synthetic_high(self):
        manifest = signed_manifest()
        results = [
            completed_result(manifest, "shard-1", "NONE"),
            completed_result(manifest, "shard-2", "NONE"),
        ]
        results[1].update(
            status="incomplete",
            incomplete_reason="codex-job-timeout",
            elapsed_seconds=None,
            review=None,
        )
        result, markdown, _ = aggregate.aggregate(manifest, results, case_metadata())
        self.assertEqual(result["benchmark_status"], "incomplete")
        self.assertEqual(result["review"]["overall_risk"], "HIGH")
        self.assertIn("Automated review incomplete for shard-2", markdown)

    def test_success_after_model_budget_becomes_incomplete(self):
        manifest = signed_manifest(active_second=False)
        review = completed_result(manifest, "shard-1", "HIGH")["review"]
        result, _ = writer.build_result(
            manifest, "shard-1", "success", json.dumps(review), 361
        )
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["incomplete_reason"], "codex-budget-exceeded")

    def test_trusted_corpus_metadata_is_reattached_after_review(self):
        manifest = signed_manifest()
        corpus = {
            "cases": [
                {
                    "id": manifest["case"],
                    "base": manifest["base_sha"],
                    "head": manifest["head_sha"],
                    **case_metadata(),
                }
            ]
        }
        self.assertEqual(
            aggregate.load_case_metadata(corpus, manifest)["expected"], "NONE"
        )
        corpus["cases"][0]["head"] = "c" * 40
        with self.assertRaisesRegex(ValueError, "reviewed range"):
            aggregate.load_case_metadata(corpus, manifest)

    def test_cross_run_result_hard_fails(self):
        manifest = signed_manifest()
        results = [
            completed_result(manifest, "shard-1"),
            completed_result(manifest, "shard-2"),
        ]
        results[1]["run_id"] = 999
        with self.assertRaisesRegex(ValueError, "run_id"):
            aggregate.aggregate(manifest, results, case_metadata())

    def test_manifest_digest_tampering_hard_fails(self):
        manifest = signed_manifest()
        manifest["files"][0]["path"] = "tampered"
        with self.assertRaisesRegex(ValueError, "digest"):
            aggregate.validate_manifest(manifest)

    def test_inconsistent_primary_owner_hard_fails(self):
        manifest = signed_manifest()
        manifest["files"][0]["primary_shard"] = "shard-2"
        unsigned = dict(manifest)
        unsigned.pop("manifest_digest")
        manifest["manifest_digest"] = hashlib.sha256(
            json.dumps(unsigned, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
        with self.assertRaisesRegex(ValueError, "ownership metadata"):
            aggregate.validate_manifest(manifest)

    def test_oversized_plan_creates_incomplete_and_inactive_results(self):
        manifest = signed_manifest(active_second=False, status="oversized")
        first, _ = writer.build_result(manifest, "shard-1", "", "", 0)
        second, _ = writer.build_result(manifest, "shard-2", "", "", 0)
        self.assertEqual(
            (first["status"], first["incomplete_reason"]),
            ("incomplete", "oversized-review"),
        )
        self.assertEqual(second["status"], "inactive")

    def test_severity_less_finding_heading_is_rejected(self):
        markdown = (
            "## Review Summary\n\n"
            "**Overall Risk**: NONE\n\n"
            "### Findings\n\n"
            "#### Missing severity\n"
            "- **Category**: Reliability\n"
            "- **Location**: [`server/a/a.go:1`](https://example.test/a#L1)\n"
            "- **Description**: Description.\n"
            "- **Impact**: Impact.\n"
            "- **Recommendation**: Recommendation.\n\n"
            "### Notes\n\nNote.\n"
        )
        with self.assertRaisesRegex(ValueError, "invalid finding heading"):
            writer.validate_review_markdown(markdown, "NONE")

    def test_whole_file_deletion_requires_base_revision_location(self):
        manifest = signed_manifest(active_second=False)
        manifest["files"][0]["citation_side"] = "merge-base"
        unsigned = dict(manifest)
        unsigned.pop("manifest_digest")
        manifest["manifest_digest"] = hashlib.sha256(
            json.dumps(unsigned, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
        review = completed_result(manifest, "shard-1", "HIGH")["review"]
        head_url = f"/blob/{manifest['head_sha']}/"
        base_url = f"/blob/{manifest['merge_base_sha']}/"
        review["review_markdown"] = review["review_markdown"].replace(
            head_url, base_url
        )
        result, _ = writer.build_result(
            manifest, "shard-1", "success", json.dumps(review), 30
        )
        self.assertEqual(result["status"], "completed")

        review["review_markdown"] = review["review_markdown"].replace(
            base_url, head_url
        )
        result, _ = writer.build_result(
            manifest, "shard-1", "success", json.dumps(review), 30
        )
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["incomplete_reason"], "invalid-model-output")

    def test_undeclared_finding_category_is_rejected(self):
        markdown = completed_result(
            signed_manifest(active_second=False), "shard-1", "HIGH"
        )["review"]["review_markdown"].replace(
            "**Category**: Reliability", "**Category**: banana"
        )
        with self.assertRaisesRegex(ValueError, "missing required fields"):
            writer.validate_review_markdown(markdown, "HIGH")

    def test_none_finding_heading_is_rejected(self):
        markdown = (
            "## Review Summary\n\n"
            "**Overall Risk**: HIGH\n\n"
            "### Findings\n\n"
            "#### [NONE] No issue\n"
            "- **Category**: Reliability\n"
            "- **Location**: [`server/a/a.go:1`](https://example.test/a#L1)\n"
            "- **Description**: Description.\n"
            "- **Impact**: Impact.\n"
            "- **Recommendation**: Recommendation.\n\n"
            "### Notes\n\nNote.\n"
        )
        with self.assertRaisesRegex(ValueError, "unparseable severity heading"):
            writer.validate_review_markdown(markdown, "HIGH")

    def test_finding_outside_changed_hunk_becomes_invalid_output(self):
        manifest = signed_manifest(active_second=False)
        review = completed_result(manifest, "shard-1", "HIGH")["review"]
        review["review_markdown"] = (
            review["review_markdown"]
            .replace("a.go:1", "a.go:999999")
            .replace("a.go#L1", "a.go#L999999")
        )
        result, _ = writer.build_result(
            manifest, "shard-1", "success", json.dumps(review), 30
        )
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["incomplete_reason"], "invalid-model-output")

    def test_finding_in_hunkless_primary_file_becomes_invalid_output(self):
        manifest = signed_manifest(active_second=False)
        manifest["files"][0]["changed_line_ranges"] = []
        unsigned = dict(manifest)
        unsigned.pop("manifest_digest")
        manifest["manifest_digest"] = hashlib.sha256(
            json.dumps(unsigned, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
        aggregate.validate_manifest(manifest)
        review = completed_result(manifest, "shard-1", "HIGH")["review"]
        result, _ = writer.build_result(
            manifest, "shard-1", "success", json.dumps(review), 30
        )
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["incomplete_reason"], "invalid-model-output")

    def test_finding_in_shared_context_becomes_invalid_output(self):
        manifest = signed_manifest()
        shared_path = manifest["shards"][1]["primary_files"][0]
        manifest["shards"][0]["shared_files"] = [shared_path]
        unsigned = dict(manifest)
        unsigned.pop("manifest_digest")
        manifest["manifest_digest"] = hashlib.sha256(
            json.dumps(unsigned, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
        review = completed_result(manifest, "shard-1", "HIGH")["review"]
        review["review_markdown"] = review["review_markdown"].replace(
            "server/a/a.go", shared_path
        )
        result, _ = writer.build_result(
            manifest, "shard-1", "success", json.dumps(review), 30
        )
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["incomplete_reason"], "invalid-model-output")

    def test_finding_outside_shard_packet_becomes_invalid_output(self):
        manifest = signed_manifest(active_second=False)
        review = completed_result(manifest, "shard-1", "HIGH")["review"]
        review["review_markdown"] = review["review_markdown"].replace(
            "server/a/a.go",
            "client/src/protoOS/outside.ts",
        )
        result, _ = writer.build_result(
            manifest, "shard-1", "success", json.dumps(review), 30
        )
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["incomplete_reason"], "invalid-model-output")

    def test_finding_outside_findings_section_is_rejected(self):
        finding = (
            "#### [HIGH] Misplaced issue\n"
            "- **Category**: Reliability\n"
            "- **Location**: [`server/a/a.go:1`](https://example.test/a#L1)\n"
            "- **Description**: Description.\n"
            "- **Impact**: Impact.\n"
            "- **Recommendation**: Recommendation."
        )
        markdown = (
            "## Review Summary\n\n"
            "**Overall Risk**: HIGH\n\n"
            f"{finding}\n\n"
            "### Findings\n\nNo findings here.\n\n"
            "### Notes\n\nNote.\n"
        )
        with self.assertRaisesRegex(ValueError, "outside the Findings section"):
            writer.validate_review_markdown(markdown, "HIGH")

    def test_early_codex_failure_hard_fails_writer(self):
        manifest = signed_manifest(active_second=False)
        with self.assertRaisesRegex(ValueError, "unexpectedly"):
            writer.build_result(manifest, "shard-1", "failure", "", 10)


class WorkflowInvariantTest(unittest.TestCase):
    def test_default_branch_dispatch_and_reusable_case_bounds(self):
        parent = (
            REPO_ROOT / ".github/workflows/codex-security-review-benchmark.yml"
        ).read_text()
        called = (
            REPO_ROOT
            / ".github/workflows/codex-security-review-sharded-benchmark-case.yml"
        ).read_text()
        self.assertIn("codex-security-review-sharded-benchmark", parent)
        self.assertIn("max-parallel: 1", parent)
        self.assertIn("max-parallel: 2", called)
        self.assertIn("timeout-minutes: 6", called)
        self.assertIn("needs: prepare-shard", called)
        self.assertIn('CODEX_CANCELLATION_CLEANUP_SECONDS: "300"', called)
        self.assertIn("github.workflow_sha", called)
        self.assertIn("sharded-benchmark-result-", called)
        self.assertIn("SHARD_JOB_ID: ${{ steps.identity.outputs.job_id }}", called)
        self.assertIn("String(job.id) === process.env.SHARD_JOB_ID", called)
        self.assertIn("git archive --format=tar.gz", called)
        self.assertIn("':(glob,exclude)**/*codex*benchmark*'", called)
        self.assertIn("test ! -e model-worktree/.git", called)
        self.assertIn(
            "actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1",
            called,
        )
        self.assertNotIn('--started-at "${{', called)
        self.assertIn("STARTED_AT: ${{ steps.codex_start.outputs.started_at }}", called)
        self.assertIn("Record trusted Codex start", called)
        self.assertNotIn("case-json", called)
        self.assertNotIn("case-metadata-json", called)
        self.assertIn(
            "--corpus-file .codex-trusted/.github/codex-benchmark-corpus.json", called
        )
        self.assertIn("codex-security-review-terra-benchmark", parent)
        self.assertIn("&& 'unified-40' || 'all'", parent)

    def test_shard_prompt_comes_from_trusted_checkout(self):
        called = (
            REPO_ROOT
            / ".github/workflows/codex-security-review-sharded-benchmark-case.yml"
        ).read_text()
        self.assertIn("Checkout trusted sharding tools", called)
        self.assertIn("render_codex_shard_prompt.py", called)
        self.assertIn("prompt: ${{ steps.prompt.outputs.prompt }}", called)

    def test_sharded_codex_step_matches_baseline_contract(self):
        baseline = workflow_test_helpers.load_workflow(
            "codex-security-review-benchmark.yml"
        )
        sharded = workflow_test_helpers.load_workflow(
            "codex-security-review-sharded-benchmark-case.yml"
        )
        baseline_step = workflow_test_helpers.find_step(
            baseline, "benchmark-review", "run_codex"
        )
        sharded_step = workflow_test_helpers.find_step(
            sharded, "shard-review", "run_codex"
        )
        self.assertEqual(sharded_step["uses"], baseline_step["uses"])
        self.assertEqual(
            sharded_step["continue-on-error"], baseline_step["continue-on-error"]
        )
        for field in (
            "model",
            "output-schema",
            "safety-strategy",
            "sandbox",
        ):
            with self.subTest(field=field):
                self.assertEqual(
                    sharded_step["with"][field], baseline_step["with"][field]
                )
        # The unsharded benchmark now selects Codex arguments through a trusted
        # profile. The policy tests execute that selector and lock its Sol output to
        # the sharded baseline while independently locking the Terra candidate.
        self.assertEqual(
            baseline_step["with"]["codex-args"],
            "${{ needs.select-cases.outputs.codex_args }}",
        )
        self.assertEqual(
            sharded_step["with"]["codex-args"],
            '["-c","model_reasoning_effort=${{ env.CODEX_REASONING_EFFORT }}"]',
        )

    def test_model_job_has_no_checkout_or_corpus_labels(self):
        workflow = workflow_test_helpers.load_workflow(
            "codex-security-review-sharded-benchmark-case.yml"
        )
        preparation = workflow["jobs"]["prepare-shard"]
        historical_checkout = next(
            step
            for step in preparation["steps"]
            if step.get("name") == "Checkout fixed historical head"
        )
        self.assertEqual(historical_checkout["with"]["fetch-depth"], 0)
        model = workflow["jobs"]["shard-review"]
        self.assertEqual(model["timeout-minutes"], 6)
        self.assertEqual(model["needs"], "prepare-shard")
        self.assertFalse(
            any(
                str(step.get("uses", "")).startswith("actions/checkout@")
                for step in model["steps"]
            )
        )
        called = (
            REPO_ROOT
            / ".github/workflows/codex-security-review-sharded-benchmark-case.yml"
        ).read_text()
        self.assertIn("model-worktree.tar.gz", called)
        self.assertIn(
            "test ! -e model-worktree/.github/codex-benchmark-corpus.json", called
        )

    def test_incomplete_aggregate_fails_after_artifact_upload(self):
        workflow = workflow_test_helpers.load_workflow(
            "codex-security-review-sharded-benchmark-case.yml"
        )
        steps = workflow["jobs"]["aggregate"]["steps"]
        upload_index = next(
            index
            for index, step in enumerate(steps)
            if step.get("name") == "Upload aggregate benchmark result"
        )
        gate_index = next(
            index
            for index, step in enumerate(steps)
            if step.get("name") == "Require completed aggregate"
        )
        self.assertGreater(gate_index, upload_index)
        self.assertIn("benchmark_status", steps[gate_index]["run"])

    def test_timeout_classifier_requires_budget_plus_cleanup(self):
        workflow = workflow_test_helpers.load_workflow(
            "codex-security-review-sharded-benchmark-case.yml"
        )
        called = (
            REPO_ROOT
            / ".github/workflows/codex-security-review-sharded-benchmark-case.yml"
        ).read_text()
        self.assertIn(
            "observedJobAndCleanupSeconds >= budgetSeconds + cleanupSeconds", called
        )
        self.assertIn("Date.parse(codexStep?.started_at || '')", called)
        self.assertIn("prerequisitesSucceeded", called)
        self.assertIn("codexWasCancelledInReview", called)
        self.assertIn('--elapsed-seconds "$TIMEOUT_ELAPSED_SECONDS"', called)
        self.assertNotIn("--elapsed-seconds -1", called)

        script = workflow_test_helpers.find_step(workflow, "finalize-shard", "inspect")[
            "with"
        ]["script"]
        job_name = "Sharded pr-953 / unified-40 / initial / Model shard-1"
        prerequisite_names = (
            "Download prepared shard",
            "Materialize isolated model workspace",
            "Bind trusted model job identity",
            "Upload trusted model job identity",
            "Require benchmark API key",
            "Load trusted shard prompt",
            "Record trusted Codex start",
        )
        successful_steps = [
            {"name": name, "conclusion": "success"} for name in prerequisite_names
        ]
        timeout_job = {
            "id": 456,
            "name": job_name,
            "conclusion": "cancelled",
            "started_at": "2026-08-28T00:00:00Z",
            "completed_at": "2026-08-28T00:11:30Z",
            "steps": [
                *successful_steps,
                {
                    "name": "Run bounded shard review",
                    "status": "in_progress",
                    "conclusion": None,
                    "started_at": "2026-08-28T00:00:30Z",
                },
            ],
        }
        post_model_job = {
            **timeout_job,
            "steps": [
                *successful_steps,
                {
                    "name": "Run bounded shard review",
                    "status": "completed",
                    "conclusion": "success",
                    "started_at": "2026-08-28T00:00:30Z",
                },
            ],
        }
        cases = (
            ("verified-timeout", timeout_job, "budget-timeout"),
            (
                "too-early",
                {**timeout_job, "completed_at": "2026-08-28T00:10:59Z"},
                "unexpected-cancellation",
            ),
            (
                "missing-prerequisites",
                {**timeout_job, "steps": successful_steps[:2]},
                "unexpected-cancellation",
            ),
            ("post-model-cancellation", post_model_job, "unexpected-cancellation"),
        )
        for name, job, expected in cases:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                completed, output = workflow_test_helpers.run_github_script(
                    script,
                    {
                        "jobs": [job],
                        "artifacts": [],
                        "agent_job_name": job_name,
                        "shard_job_id": 456,
                        "timeout_minutes": "6",
                        "cleanup_seconds": "300",
                    },
                    tmp,
                )
                self.assertEqual(completed.returncode, 0, completed.stderr)
                self.assertEqual(output["outputs"]["classification"], expected)
                if expected == "budget-timeout":
                    self.assertEqual(output["outputs"]["elapsed_seconds"], "360")


if __name__ == "__main__":
    unittest.main()
