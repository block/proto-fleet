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
            }
        )
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
        "case_metadata": {
            "pr": 1,
            "purpose": "test",
            "expected": "NONE",
            "source-run": "https://example.test/run",
            "source-comment": "",
        },
        "variant_metadata": {"id": "unified-40", "unified": 40, "inter-hunk": 0},
        "status": status,
        "oversized_reasons": ["test"] if status == "oversized" else [],
        "limits": {
            "max_packets": 2,
            "max_packet_bytes": 500_000,
            "max_packet_lines": 12_500,
        },
        "files": files,
        "shards": shards,
    }
    canonical = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
    manifest["manifest_digest"] = hashlib.sha256(canonical).hexdigest()
    return manifest


def completed_result(manifest: dict, shard_id: str, risk: str = "NONE") -> dict:
    shard = next(item for item in manifest["shards"] if item["id"] == shard_id)
    if risk == "NONE":
        findings = "No concrete security, correctness, or reliability issues found."
    else:
        findings = "\n".join(
            (
                f"#### [{risk}] Test issue",
                "- **Category**: Reliability",
                "- **Location**: [`server/a/a.go:1`](https://example.test/server/a/a.go#L1)",
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
    def test_path_ownership_is_first_match_and_disjoint(self):
        cases = {
            "server/monitoring/grafana/rule.yml": "delivery",
            "server/internal/domain/service.go": "server",
            "plugin/asicrs/Dockerfile": "delivery",
            "plugin/asicrs/src/main.rs": "asicrs",
            "plugin/driver/generated/device.pb.go": "contracts",
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

    def test_more_than_two_bounded_units_is_rejected(self):
        files = [
            file_diff(f"server/package{index}/file.go", 300_000, 1_000)
            for index in range(3)
        ]
        plan = planner.plan_files(files)
        self.assertEqual(plan["status"], "oversized")

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
            "case_metadata": {},
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
                    "case_metadata_json": json.dumps(
                        {
                            "pr": 1,
                            "purpose": "test",
                            "expected": "NONE",
                            "source-run": "",
                            "source-comment": "",
                        }
                    ),
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
            self.assertIn("server/a/new.go", patches)
            patch = patches["server/a/new.go"]
            self.assertIn(b"rename from server/a/old.go", patch)
            self.assertIn(b"rename to server/a/new.go", patch)
            self.assertIn(b"+var A = 1", patch)
            self.assertNotIn(b"--- /dev/null", patch)


class PromptTest(unittest.TestCase):
    def test_shard_prompt_is_complete_and_prohibits_full_diff_regeneration(self):
        template = (REPO_ROOT / ".github/codex-sharded-review-prompt.md").read_text()
        values = {
            name: f"trusted-{name.lower()}" for name in prompt_renderer.PLACEHOLDERS
        }
        rendered = prompt_renderer.render(template, values)
        self.assertNotIn("{{", rendered)
        self.assertIn("primary_files", rendered)
        self.assertIn("shared_files", rendered)
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
                "CODEX_MODEL",
                "CODEX_REASONING_EFFORT",
                "PROMPT_PROFILE",
            )
        }
        os.environ.update(
            GITHUB_RUN_ID="123",
            GITHUB_RUN_ATTEMPT="1",
            CODEX_MODEL="gpt-test",
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
        result, markdown, _ = aggregate.aggregate(manifest, results)
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
        result, markdown, _ = aggregate.aggregate(manifest, results)
        self.assertEqual(result["benchmark_status"], "incomplete")
        self.assertEqual(result["review"]["overall_risk"], "HIGH")
        self.assertIn("Automated review incomplete for shard-2", markdown)

    def test_cross_run_result_hard_fails(self):
        manifest = signed_manifest()
        results = [
            completed_result(manifest, "shard-1"),
            completed_result(manifest, "shard-2"),
        ]
        results[1]["run_id"] = 999
        with self.assertRaisesRegex(ValueError, "run_id"):
            aggregate.aggregate(manifest, results)

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
        self.assertIn('CODEX_CANCELLATION_CLEANUP_SECONDS: "300"', called)
        self.assertIn("github.workflow_sha", called)
        self.assertIn("sharded-benchmark-result-", called)
        self.assertIn("SHARD_JOB_ID: ${{ steps.identity.outputs.job_id }}", called)
        self.assertIn("String(job.id) === process.env.SHARD_JOB_ID", called)
        self.assertIn("include-hidden-files: true", called)
        self.assertIn(
            "github.event.action == 'codex-security-review-sharded-benchmark' && 'unified-40' || 'all'",
            parent,
        )

    def test_shard_prompt_comes_from_trusted_checkout(self):
        called = (
            REPO_ROOT
            / ".github/workflows/codex-security-review-sharded-benchmark-case.yml"
        ).read_text()
        self.assertIn("Checkout trusted sharding tools", called)
        self.assertIn("render_codex_shard_prompt.py", called)
        self.assertIn("prompt: ${{ steps.prompt.outputs.prompt }}", called)

    def test_timeout_classifier_requires_budget_plus_cleanup(self):
        workflow = workflow_test_helpers.load_workflow(
            "codex-security-review-sharded-benchmark-case.yml"
        )
        called = (
            REPO_ROOT
            / ".github/workflows/codex-security-review-sharded-benchmark-case.yml"
        ).read_text()
        self.assertIn("elapsedSeconds >= budgetSeconds + cleanupSeconds", called)
        self.assertIn("prerequisitesSucceeded", called)
        self.assertIn("codexReachedReviewPhase", called)

        script = workflow_test_helpers.find_step(workflow, "finalize-shard", "inspect")[
            "with"
        ]["script"]
        job_name = "Sharded pr-953 / unified-40 / initial / Shard shard-1"
        prerequisite_names = (
            "Checkout fixed historical head",
            "Checkout trusted sharding tools",
            "Fetch and validate fixed historical range",
            "Plan bounded review shards",
            "Bind trusted shard job identity",
            "Upload trusted shard plan",
            "Require benchmark API key",
            "Render trusted shard prompt",
        )
        successful_steps = [
            {"name": name, "conclusion": "success"} for name in prerequisite_names
        ]
        timeout_job = {
            "id": 456,
            "name": job_name,
            "conclusion": "cancelled",
            "started_at": "2026-08-28T00:00:00Z",
            "completed_at": "2026-08-28T00:11:00Z",
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
        cases = (
            (timeout_job, "budget-timeout"),
            (
                {**timeout_job, "completed_at": "2026-08-28T00:10:59Z"},
                "unexpected-cancellation",
            ),
            ({**timeout_job, "steps": successful_steps[:2]}, "unexpected-cancellation"),
        )
        for job, expected in cases:
            with self.subTest(expected=expected), tempfile.TemporaryDirectory() as tmp:
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


if __name__ == "__main__":
    unittest.main()
