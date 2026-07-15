#!/usr/bin/env python3

import io
import json
import subprocess
import unittest
import tempfile
import zipfile
from pathlib import Path

import evaluate_review_policy as policy


WORKFLOWS_DIR = Path(__file__).resolve().parents[1] / "workflows"
CONFIG_PATH = Path(__file__).resolve().parents[1] / "review-policy.json"


def read_workflow(name):
    return (WORKFLOWS_DIR / name).read_text(encoding="utf-8")


def load_workflow(name):
    result = subprocess.run(
        [
            "ruby",
            "-ryaml",
            "-rjson",
            "-e",
            "puts JSON.generate(YAML.load_file(ARGV.fetch(0)))",
            str(WORKFLOWS_DIR / name),
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    return json.loads(result.stdout)


def load_policy_config():
    return json.loads(CONFIG_PATH.read_text(encoding="utf-8"))


class ReviewPolicyTest(unittest.TestCase):
    def test_pr_gate_runs_review_policy_tests(self):
        workflow = load_workflow("pr-gate.yml")
        jobs = workflow["jobs"]

        self.assertIn("review-policy-tests", jobs)
        self.assertIn("review-policy-tests", jobs["gate"]["needs"])
        self.assertEqual(
            jobs["review-policy-tests"]["steps"][1]["run"],
            "python3 .github/scripts/evaluate_review_policy_test.py",
        )

    def test_workflow_ignored_events_do_not_cancel_active_evaluations(self):
        workflow = load_workflow("review-policy.yml")
        group_expression = workflow["concurrency"]["group"]
        cancel_expression = workflow["concurrency"]["cancel-in-progress"]

        self.assertIn("format('ignored-{0}', github.run_id)", group_expression)
        self.assertIn("github.event.pull_request.head.sha || github.event.workflow_run.head_sha", group_expression)
        self.assertIn("github.event.check_run.app.slug == 'github-actions'", cancel_expression)
        self.assertIn("github.event.action == 'rerequested'", cancel_expression)
        self.assertIn("github.event.check_suite.app.slug == 'github-actions'", cancel_expression)
        self.assertIn("github.event.context == 'Review Policy'", cancel_expression)
        self.assertIn("github.event.context == 'Review Policy Advisory'", cancel_expression)
        self.assertIn("github.event.review.state == 'commented'", cancel_expression)

    def test_workflow_publishes_pending_status_for_cancelled_evaluation(self):
        workflow = load_workflow("review-policy.yml")
        publish_job = workflow["jobs"]["publish-status"]
        publish_step = publish_job["steps"][0]
        script = publish_step["with"]["script"]

        self.assertEqual(publish_job["if"], "always() && needs.evaluate.outputs.found == 'true'")
        self.assertEqual(
            publish_job["concurrency"]["group"],
            "review-policy-publish-${{ needs.evaluate.outputs.head_sha }}",
        )
        self.assertEqual(publish_step["env"]["EVALUATE_RESULT"], "${{ needs.evaluate.result }}")
        self.assertIn("existingRunId > currentRunId", script)
        self.assertIn("evaluationCancelled ? 'pending'", script)
        self.assertIn(
            "Review policy evaluation was cancelled; waiting for a fresh decision.",
            script,
        )

    def test_workflow_serializes_and_authorizes_label_sync(self):
        workflow = load_workflow("review-policy.yml")
        label_job = workflow["jobs"]["sync-label"]

        self.assertEqual(
            label_job["if"],
            "always() && needs.evaluate.outputs.found == 'true' && needs.evaluate.result != 'cancelled'",
        )
        self.assertEqual(
            label_job["concurrency"]["group"],
            "review-policy-label-${{ needs.evaluate.outputs.head_sha }}",
        )
        self.assertEqual(label_job["permissions"], {"issues": "write", "pull-requests": "write"})

    def test_workflow_keeps_fail_closed_non_cancelled_failures(self):
        workflow_text = read_workflow("review-policy.yml")
        workflow = load_workflow("review-policy.yml")
        publish_env = workflow["jobs"]["publish-status"]["steps"][0]["env"]
        classifier_step = next(
            step
            for step in workflow["jobs"]["evaluate"]["steps"]
            if step.get("id") == "ai_classifier"
        )

        self.assertIn('exit 1', workflow_text)
        self.assertIn("got base ref ${BASE_REF}.", workflow_text)
        self.assertEqual(publish_env["DECISION"], "${{ needs.evaluate.outputs.decision || 'needs-human-review' }}")
        self.assertEqual(publish_env["PASSED"], "${{ needs.evaluate.outputs.passed || 'false' }}")
        self.assertEqual(publish_env["ENFORCED"], "${{ needs.evaluate.outputs.enforced || 'true' }}")
        self.assertEqual(classifier_step["timeout-minutes"], 10)

    def test_path_matches_double_star_root_file(self):
        self.assertTrue(policy.path_matches("package.json", "**/package.json"))
        self.assertTrue(policy.path_matches("client/package.json", "**/package.json"))

    def test_path_matches_directory_prefix(self):
        self.assertTrue(policy.path_matches(".github/workflows/review-policy.yml", ".github/**"))
        self.assertTrue(policy.path_matches("server", "server/**"))
        self.assertTrue(policy.path_matches("server/main.go", "server/**"))

    def test_denied_paths(self):
        files = [
            {"filename": "client/src/foo.ts"},
            {"filename": "server/main.go"},
            {"filename": "client/package.json"},
        ]
        self.assertEqual(
            policy.denied_paths(files, ["server/**", "**/package.json"]),
            ["client/package.json", "server/main.go"],
        )

    def test_denied_paths_checks_previous_filename_for_renames(self):
        files = [
            {
                "filename": "docs/workflows/review-policy.yml",
                "previous_filename": ".github/workflows/review-policy.yml",
            },
            {
                "filename": "server/main.go",
                "previous_filename": "docs/main.go",
            },
        ]
        self.assertEqual(
            policy.denied_paths(files, [".github/**", "server/**"]),
            [".github/workflows/review-policy.yml", "server/main.go"],
        )

    def test_low_risk_config_allows_small_server_app_changes_to_reach_classifier(self):
        deny_paths = load_policy_config()["low_risk"]["deny_paths"]
        files = [
            {"filename": "server/devtools/hapoc/main.go"},
            {"filename": "server/fake-proto-rig/rest_api_handler.go"},
            {"filename": "server/internal/domain/telemetry/scheduler/scheduler.go"},
            {"filename": "server/internal/domain/collection/service_test.go"},
        ]

        self.assertEqual(policy.denied_paths(files, deny_paths), [])

    def test_low_risk_config_keeps_sensitive_server_paths_denied(self):
        deny_paths = load_policy_config()["low_risk"]["deny_paths"]
        files = [
            {"filename": "server/cmd/fleetd/main.go"},
            {"filename": "server/generated/grpc/device/v1/device.pb.go"},
            {"filename": "server/internal/domain/authz/catalog.go"},
            {"filename": "server/internal/domain/stores/sqlstores/device.go"},
            {"filename": "server/internal/handlers/middleware/permission.go"},
            {"filename": "server/internal/infrastructure/db/config.go"},
            {"filename": "server/migrations/000123_example.up.sql"},
            {"filename": "server/monitoring/grafana/grafana.ini"},
            {"filename": "server/sqlc/queries/device.sql"},
            {"filename": "server/tools/generate-fleet-cli/main.go"},
        ]

        self.assertEqual(
            policy.denied_paths(files, deny_paths),
            [
                "server/cmd/fleetd/main.go",
                "server/generated/grpc/device/v1/device.pb.go",
                "server/internal/domain/authz/catalog.go",
                "server/internal/domain/stores/sqlstores/device.go",
                "server/internal/handlers/middleware/permission.go",
                "server/internal/infrastructure/db/config.go",
                "server/migrations/000123_example.up.sql",
                "server/monitoring/grafana/grafana.ini",
                "server/sqlc/queries/device.sql",
                "server/tools/generate-fleet-cli/main.go",
            ],
        )

    def test_classifier_allows_low_risk(self):
        classifier = {
            "risk": "low",
            "confidence": 0.91,
            "requires_human_review": False,
            "reasons": ["small localized change"],
        }
        allowed, reasons = policy.classifier_allows_low_risk(classifier, 0.85)
        self.assertTrue(allowed)
        self.assertEqual(reasons, ["small localized change"])

    def test_classifier_fails_closed(self):
        classifier = {
            "risk": "medium",
            "confidence": 0.84,
            "requires_human_review": True,
        }
        allowed, reasons = policy.classifier_allows_low_risk(classifier, 0.85)
        self.assertFalse(allowed)
        self.assertIn("AI classifier risk is medium, not low", reasons)
        self.assertIn("AI classifier requires human review", reasons)
        self.assertIn("AI classifier confidence 0.84 is below 0.85", reasons)

    def test_classifier_rejects_embedded_json(self):
        with self.assertRaisesRegex(policy.PolicyError, "exactly one JSON object"):
            policy.load_classifier('warning\n{"risk":"low","confidence":0.9,"requires_human_review":false,"reasons":[]}')

    def test_classifier_rejects_non_finite_confidence(self):
        classifier = policy.load_classifier('{"risk":"low","confidence":NaN,"requires_human_review":false,"reasons":[]}')
        allowed, reasons = policy.classifier_allows_low_risk(classifier, 0.85)
        self.assertFalse(allowed)
        self.assertIn("AI classifier confidence must be a finite number", reasons)

    def test_deterministic_content_blockers_catch_shellouts_and_file_size(self):
        files = [
            {
                "filename": "client/src/foo.ts",
                "additions": 81,
                "deletions": 0,
                "patch": "@@\n+import child_process from 'child_process'\n+child_process.exec('npm run surprise')",
            },
            {
                "filename": "client/src/bar.ts",
                "additions": 1,
                "deletions": 0,
                "patch": "@@\n+console.log('boring')",
            },
            {
                "filename": "client/src/opaque.bin",
                "additions": 0,
                "deletions": 0,
            },
        ]
        blockers = policy.deterministic_content_blockers(
            files,
            {
                "max_file_changes": 80,
                "content_deny_added_patterns": [
                    {
                        "pattern": "\\b(child_process\\.|exec\\s*\\()",
                        "reason": "adds process execution or shell-out code",
                    }
                ],
            },
        )

        self.assertIn("client/src/foo.ts has 81 changed lines, exceeds per-file limit 80", blockers)
        self.assertIn("client/src/foo.ts adds blocked content: adds process execution or shell-out code", blockers)
        self.assertIn("client/src/opaque.bin diff content is unavailable for deterministic content checks", blockers)
        self.assertEqual(len(blockers), 3)

    def test_low_risk_preflight_blocks_before_classifier(self):
        original_paginate = policy.github_paginate
        original_request = policy.github_request
        original_trusted_author_reasons = policy.trusted_author_reasons
        try:
            def fake_paginate(path, token):
                if path.endswith("/files"):
                    return [
                        {"filename": ".github/workflows/review-policy.yml", "additions": 2, "deletions": 1},
                        {"filename": "docs/readme.md", "additions": 300, "deletions": 0},
                    ]
                if path.endswith("/commits"):
                    return [{"sha": "abc123", "author": {"login": "author"}, "committer": {"login": "author"}}]
                if path.endswith("/commits/abc123/pulls"):
                    return [{"number": 123, "state": "open", "head": {"sha": "abc123"}}]
                return []

            policy.github_paginate = fake_paginate
            policy.github_request = lambda method, path, token, body=None: {
                "state": "open",
                "head": {"sha": "abc123"},
            }
            policy.trusted_author_reasons = lambda author, trusted_authors, owner, token: (
                False,
                [f"author @{author} is not in trusted_authors"],
            )
            result = policy.evaluate_low_risk_preflight(
                config={
                    "trusted_authors": ["trusted"],
                    "low_risk": {
                        "max_changed_files": 10,
                        "max_total_changes": 200,
                        "deny_paths": [".github/**"],
                    },
                },
                owner="block",
                repo="proto-fleet",
                pr_number=123,
                author="author",
                head_sha="abc123",
                token="token",
            )
        finally:
            policy.github_paginate = original_paginate
            policy.github_request = original_request
            policy.trusted_author_reasons = original_trusted_author_reasons

        self.assertFalse(result["eligible"])
        self.assertIn("author @author is not in trusted_authors", result["blockers"])
        self.assertIn("303 changed lines exceeds limit 200", result["blockers"])
        self.assertIn("denied paths changed: .github/workflows/review-policy.yml", result["blockers"])

    def test_shared_head_pr_blockers_fail_closed_for_multiple_open_prs(self):
        original = policy.github_paginate
        try:
            policy.github_paginate = lambda path, token: [
                {"number": 123, "state": "open", "head": {"sha": "abc123"}},
                {"number": 456, "state": "open", "head": {"sha": "abc123"}},
                {"number": 789, "state": "closed", "head": {"sha": "abc123"}},
            ]
            blockers = policy.shared_head_pr_blockers("block", "proto-fleet", 123, "abc123", "token")
        finally:
            policy.github_paginate = original

        self.assertEqual(blockers, ["current head SHA is shared by multiple open PRs: #123, #456"])

    def test_trusted_author_reasons_accepts_team_membership(self):
        original = policy.is_team_member
        try:
            policy.is_team_member = lambda owner, team_slug, username, token: (
                owner == "block" and team_slug == "proto-fleet-dev" and username == "member"
            )
            trusted, reasons = policy.trusted_author_reasons("member", ["@block/proto-fleet-dev"], "block", "token")
        finally:
            policy.is_team_member = original

        self.assertTrue(trusted)
        self.assertEqual(reasons, ["author @member is a member of @block/proto-fleet-dev"])

    def test_trusted_author_reasons_accepts_case_insensitive_login(self):
        trusted, reasons = policy.trusted_author_reasons("AnkitGoswami", ["ankitgoswami"], "block", "token")

        self.assertTrue(trusted)
        self.assertEqual(reasons, ["author @AnkitGoswami is explicitly trusted"])

    def test_trusted_head_contributor_reasons_blocks_untrusted_committers(self):
        original = policy.trusted_author_reasons
        try:
            policy.trusted_author_reasons = lambda author, trusted_authors, owner, token: (
                author == "trusted",
                [f"author @{author} is explicitly trusted"] if author == "trusted" else [f"author @{author} is not in trusted_authors"],
            )
            ok, reasons, blockers = policy.trusted_head_contributor_reasons(
                [
                    {"sha": "abc123", "author": {"login": "trusted"}, "committer": {"login": "untrusted"}},
                    {"sha": "def456", "author": None, "committer": None},
                ],
                ["trusted"],
                "block",
                "token",
            )
        finally:
            policy.trusted_author_reasons = original

        self.assertFalse(ok)
        self.assertEqual(reasons, ["head contributor @trusted is trusted"])
        self.assertIn("head contributor @untrusted is not in trusted_authors", blockers)
        self.assertIn("current head has commits without GitHub-linked authors or committers: def456", blockers)

    def test_trusted_workflow_actor_reasons_requires_trusted_authenticated_actor(self):
        original_workflow_runs = policy.latest_workflow_runs
        original_trusted_author_reasons = policy.trusted_author_reasons
        try:
            policy.latest_workflow_runs = lambda owner, repo, head_sha, event, token: {
                ".github/workflows/pr-gate.yml": {"actor": {"login": "untrusted"}},
            }
            policy.trusted_author_reasons = lambda author, trusted_authors, owner, token: (
                author == "trusted",
                [f"author @{author} trust checked"],
            )
            ok, reasons, blockers = policy.trusted_workflow_actor_reasons(
                "block",
                "proto-fleet",
                "abc123",
                ["trusted"],
                {"workflow_path": ".github/workflows/pr-gate.yml", "event": "pull_request"},
                "token",
            )
        finally:
            policy.latest_workflow_runs = original_workflow_runs
            policy.trusted_author_reasons = original_trusted_author_reasons

        self.assertFalse(ok)
        self.assertEqual(reasons, [])
        self.assertEqual(blockers, ["authenticated workflow actor @untrusted is not in trusted_authors"])

    def test_latest_check_runs_tie_breaks_on_id(self):
        original = policy.github_paginate_key
        try:
            policy.github_paginate_key = lambda path, token, key: [
                {"name": "Gate", "started_at": "2026-01-01T00:00:00Z", "id": 1, "conclusion": "failure"},
                {"name": "Gate", "started_at": "2026-01-01T00:00:00Z", "id": 2, "conclusion": "success"},
            ]
            latest = policy.latest_check_runs("block", "proto-fleet", "abc123", "token")
        finally:
            policy.github_paginate_key = original

        self.assertEqual(latest["Gate"]["id"], 2)

    def test_check_statuses_requires_successful_completed_runs(self):
        original = policy.latest_check_runs
        original_statuses = policy.latest_commit_statuses
        original_request = policy.github_request
        original_workflow_runs = policy.latest_workflow_runs
        try:
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {
                "security-review": {
                    "status": "completed",
                    "conclusion": "failure",
                    "app": {"slug": "github-actions"},
                    "details_url": "https://github.com/block/proto-fleet/actions/runs/124/job/456",
                },
            }
            policy.latest_commit_statuses = lambda owner, repo, head_sha, token: {}
            policy.latest_workflow_runs = lambda owner, repo, head_sha, event, token: {
                ".github/workflows/pr-gate.yml": {
                    "name": "PR Gate",
                    "status": "completed",
                    "conclusion": "success",
                },
            }
            policy.github_request = lambda method, path, token, body=None: {
                "path": ".github/workflows/pr-gate.yml",
                "head_sha": "abc123",
                "event": "pull_request",
            }
            ok, blockers = policy.check_statuses(
                "block",
                "proto-fleet",
                "abc123",
                [
                    {
                        "name": "PR Gate",
                        "type": "github_actions_workflow",
                        "workflow_path": ".github/workflows/pr-gate.yml",
                        "workflow_name": "PR Gate",
                        "event": "pull_request",
                    },
                    {
                        "name": "security-review",
                        "type": "github_actions",
                        "workflow_path": ".github/workflows/codex-security-review.yml",
                        "event": "pull_request",
                    },
                    {"name": "missing", "type": "check_run", "app_slug": "trusted-app"},
                ],
                "token",
            )
        finally:
            policy.latest_check_runs = original
            policy.latest_commit_statuses = original_statuses
            policy.github_request = original_request
            policy.latest_workflow_runs = original_workflow_runs

        self.assertFalse(ok)
        self.assertIn("required check 'security-review' is completed/failure", blockers)
        self.assertIn("required check 'missing' is missing", blockers)

    def test_check_statuses_accepts_typed_commit_statuses(self):
        original = policy.latest_check_runs
        original_statuses = policy.latest_commit_statuses
        try:
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {
                "DCO Check": {"status": "completed", "conclusion": "success", "app": {"slug": "block-dco-check"}},
            }
            policy.latest_commit_statuses = lambda owner, repo, head_sha, token: {
                "Legacy": {"state": "success", "creator": {"login": "trusted-bot"}},
                "External": {"state": "pending", "creator": {"login": "external-ci"}},
            }
            ok, blockers = policy.check_statuses(
                "block",
                "proto-fleet",
                "abc123",
                [
                    {"name": "DCO Check", "type": "check_run", "app_slug": "block-dco-check"},
                    {"name": "Legacy", "type": "commit_status", "creator": "trusted-bot"},
                    {"name": "External", "type": "commit_status", "creator": "external-ci"},
                ],
                "token",
            )
        finally:
            policy.latest_check_runs = original
            policy.latest_commit_statuses = original_statuses

        self.assertFalse(ok)
        self.assertIn("required status 'External' is pending", blockers)
        self.assertNotIn("required check 'DCO Check' is missing", blockers)

    def test_check_statuses_rejects_spoofed_github_actions_workflow(self):
        original = policy.latest_check_runs
        original_statuses = policy.latest_commit_statuses
        original_request = policy.github_request
        try:
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {
                "Gate": {
                    "status": "completed",
                    "conclusion": "success",
                    "app": {"slug": "github-actions"},
                    "details_url": "https://github.com/block/proto-fleet/actions/runs/123/job/456",
                },
            }
            policy.latest_commit_statuses = lambda owner, repo, head_sha, token: {}
            policy.github_request = lambda method, path, token, body=None: {
                "path": ".github/workflows/attacker.yml",
                "head_sha": "abc123",
                "event": "pull_request",
            }
            ok, blockers = policy.check_statuses(
                "block",
                "proto-fleet",
                "abc123",
                [
                    {
                        "name": "Gate",
                        "type": "github_actions",
                        "workflow_path": ".github/workflows/pr-gate.yml",
                        "event": "pull_request",
                    }
                ],
                "token",
            )
        finally:
            policy.latest_check_runs = original
            policy.latest_commit_statuses = original_statuses
            policy.github_request = original_request

        self.assertFalse(ok)
        self.assertIn(
            "required check 'Gate' workflow path is '.github/workflows/attacker.yml', expected '.github/workflows/pr-gate.yml'",
            blockers,
        )

    def test_check_statuses_requires_successful_workflow_run(self):
        original = policy.latest_check_runs
        original_statuses = policy.latest_commit_statuses
        original_workflow_runs = policy.latest_workflow_runs
        try:
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {}
            policy.latest_commit_statuses = lambda owner, repo, head_sha, token: {}
            policy.latest_workflow_runs = lambda owner, repo, head_sha, event, token: {
                ".github/workflows/pr-gate.yml": {
                    "name": "Attacker Gate",
                    "status": "completed",
                    "conclusion": "success",
                },
            }
            ok, blockers = policy.check_statuses(
                "block",
                "proto-fleet",
                "abc123",
                [
                    {
                        "name": "PR Gate",
                        "type": "github_actions_workflow",
                        "workflow_path": ".github/workflows/pr-gate.yml",
                        "workflow_name": "PR Gate",
                        "event": "pull_request",
                    }
                ],
                "token",
            )
        finally:
            policy.latest_check_runs = original
            policy.latest_commit_statuses = original_statuses
            policy.latest_workflow_runs = original_workflow_runs

        self.assertFalse(ok)
        self.assertIn("required workflow 'PR Gate' name is 'Attacker Gate', expected 'PR Gate'", blockers)

    def test_latest_workflow_runs_tie_breaks_on_id(self):
        original = policy.github_paginate_key
        try:
            policy.github_paginate_key = lambda path, token, key: [
                {
                    "path": ".github/workflows/pr-gate.yml",
                    "head_sha": "abc123",
                    "event": "pull_request",
                    "run_started_at": "2026-01-01T00:00:00Z",
                    "id": 1,
                    "conclusion": "failure",
                },
                {
                    "path": ".github/workflows/pr-gate.yml",
                    "head_sha": "abc123",
                    "event": "pull_request",
                    "run_started_at": "2026-01-01T00:00:00Z",
                    "id": 2,
                    "conclusion": "success",
                },
                {
                    "path": ".github/workflows/pr-gate.yml",
                    "head_sha": "def456",
                    "event": "pull_request",
                    "run_started_at": "2026-01-02T00:00:00Z",
                    "id": 3,
                    "conclusion": "success",
                },
            ]
            latest = policy.latest_workflow_runs("block", "proto-fleet", "abc123", "pull_request", "token")
        finally:
            policy.github_paginate_key = original

        self.assertEqual(latest[".github/workflows/pr-gate.yml"]["id"], 2)

    def test_check_statuses_requires_trusted_sources_in_config(self):
        original = policy.latest_check_runs
        original_statuses = policy.latest_commit_statuses
        try:
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {
                "DCO Check": {"status": "completed", "conclusion": "success", "app": {"slug": "block-dco-check"}},
            }
            policy.latest_commit_statuses = lambda owner, repo, head_sha, token: {
                "Legacy": {"state": "success", "creator": {"login": "trusted-bot"}},
            }
            ok, blockers = policy.check_statuses(
                "block",
                "proto-fleet",
                "abc123",
                [
                    "Gate",
                    {"name": "DCO Check", "type": "check_run"},
                    {"name": "security-review", "type": "github_actions", "event": "pull_request"},
                    {
                        "name": "PR Gate",
                        "type": "github_actions_workflow",
                        "workflow_path": ".github/workflows/pr-gate.yml",
                    },
                    {"name": "Legacy", "type": "commit_status"},
                ],
                "token",
            )
        finally:
            policy.latest_check_runs = original
            policy.latest_commit_statuses = original_statuses

        self.assertFalse(ok)
        self.assertIn("required check 'Gate' uses legacy unvalidated config", blockers)
        self.assertIn("required check 'DCO Check' is missing trusted app_slug", blockers)
        self.assertIn("required check 'security-review' is missing trusted workflow_path", blockers)
        self.assertIn("required workflow 'PR Gate' is missing trusted event", blockers)
        self.assertIn("required status 'Legacy' is missing trusted creator", blockers)

    def test_latest_commit_statuses_tie_breaks_on_id(self):
        original = policy.github_paginate
        try:
            policy.github_paginate = lambda path, token: [
                {"context": "DCO Check", "created_at": "2026-01-01T00:00:00Z", "id": 1, "state": "failure"},
                {"context": "DCO Check", "created_at": "2026-01-01T00:00:00Z", "id": 2, "state": "success"},
            ]
            latest = policy.latest_commit_statuses("block", "proto-fleet", "abc123", "token")
        finally:
            policy.github_paginate = original

        self.assertEqual(latest["DCO Check"]["id"], 2)

    def test_extract_run_id(self):
        self.assertEqual(
            policy.extract_run_id("https://github.com/block/proto-fleet/actions/runs/123/job/456"),
            "123",
        )
        self.assertIsNone(policy.extract_run_id("https://github.com/block/proto-fleet/runs/123"))

    def test_extract_security_risk_validates_workflow_run_identity(self):
        original_paginate_key = policy.github_paginate_key
        original_request = policy.github_request
        original_download = policy.github_download
        archive_bytes = io.BytesIO()
        with zipfile.ZipFile(archive_bytes, "w") as archive:
            archive.writestr(
                "codex-security-review-result.json",
                json.dumps({
                    "head_sha": "abc123",
                    "commit_range": "base123...abc123",
                    "run_id": "123",
                    "overall_risk": "LOW",
                }),
            )
        try:
            def fake_paginate_key(path, token, key):
                if "/commits/" in path:
                    return [
                        {
                            "name": "security-review",
                            "started_at": "2026-01-01T00:00:00Z",
                            "details_url": "https://github.com/block/proto-fleet/actions/runs/123/job/456",
                        }
                    ]
                if "/actions/runs/123/artifacts" in path:
                    return [{"id": 999, "name": "codex-security-review-result", "expired": False}]
                return []

            policy.github_paginate_key = fake_paginate_key
            policy.github_request = lambda method, path, token, body=None: {
                "path": ".github/workflows/codex-security-review.yml",
                "head_sha": "abc123",
                "event": "pull_request",
            }
            policy.github_download = lambda path, token: archive_bytes.getvalue()
            risk, blockers = policy.extract_security_risk(
                "block",
                "proto-fleet",
                "base123",
                "abc123",
                "token",
                "security-review",
                ".github/workflows/codex-security-review.yml",
                "codex-security-review-result",
            )
        finally:
            policy.github_paginate_key = original_paginate_key
            policy.github_request = original_request
            policy.github_download = original_download

        self.assertEqual(risk, "LOW")
        self.assertEqual(blockers, [])

    def test_extract_security_risk_rejects_stale_commit_range(self):
        original_paginate_key = policy.github_paginate_key
        original_request = policy.github_request
        original_download = policy.github_download
        archive_bytes = io.BytesIO()
        with zipfile.ZipFile(archive_bytes, "w") as archive:
            archive.writestr(
                "codex-security-review-result.json",
                json.dumps({
                    "head_sha": "abc123",
                    "commit_range": "oldbase...abc123",
                    "run_id": "123",
                    "overall_risk": "LOW",
                }),
            )
        try:
            def fake_paginate_key(path, token, key):
                if "/commits/" in path:
                    return [
                        {
                            "name": "security-review",
                            "started_at": "2026-01-01T00:00:00Z",
                            "details_url": "https://github.com/block/proto-fleet/actions/runs/123/job/456",
                        }
                    ]
                if "/actions/runs/123/artifacts" in path:
                    return [{"id": 999, "name": "codex-security-review-result", "expired": False}]
                return []

            policy.github_paginate_key = fake_paginate_key
            policy.github_request = lambda method, path, token, body=None: {
                "path": ".github/workflows/codex-security-review.yml",
                "head_sha": "abc123",
                "event": "pull_request",
            }
            policy.github_download = lambda path, token: archive_bytes.getvalue()
            risk, blockers = policy.extract_security_risk(
                "block",
                "proto-fleet",
                "base123",
                "abc123",
                "token",
                "security-review",
                ".github/workflows/codex-security-review.yml",
                "codex-security-review-result",
            )
        finally:
            policy.github_paginate_key = original_paginate_key
            policy.github_request = original_request
            policy.github_download = original_download

        self.assertIsNone(risk)
        self.assertIn("Codex security-review result artifact is stale for this PR base/head range", blockers)

    def test_extract_security_risk_rejects_non_string_risk(self):
        original_paginate_key = policy.github_paginate_key
        original_request = policy.github_request
        original_download = policy.github_download
        archive_bytes = io.BytesIO()
        with zipfile.ZipFile(archive_bytes, "w") as archive:
            archive.writestr(
                "codex-security-review-result.json",
                json.dumps({
                    "head_sha": "abc123",
                    "commit_range": "base123...abc123",
                    "run_id": "123",
                    "overall_risk": None,
                }),
            )
        try:
            def fake_paginate_key(path, token, key):
                if "/commits/" in path:
                    return [
                        {
                            "name": "security-review",
                            "started_at": "2026-01-01T00:00:00Z",
                            "details_url": "https://github.com/block/proto-fleet/actions/runs/123/job/456",
                        }
                    ]
                if "/actions/runs/123/artifacts" in path:
                    return [{"id": 999, "name": "codex-security-review-result", "expired": False}]
                return []

            policy.github_paginate_key = fake_paginate_key
            policy.github_request = lambda method, path, token, body=None: {
                "path": ".github/workflows/codex-security-review.yml",
                "head_sha": "abc123",
                "event": "pull_request",
            }
            policy.github_download = lambda path, token: archive_bytes.getvalue()
            risk, blockers = policy.extract_security_risk(
                "block",
                "proto-fleet",
                "base123",
                "abc123",
                "token",
                "security-review",
                ".github/workflows/codex-security-review.yml",
                "codex-security-review-result",
            )
        finally:
            policy.github_paginate_key = original_paginate_key
            policy.github_request = original_request
            policy.github_download = original_download

        self.assertIsNone(risk)
        self.assertIn("Codex security-review result artifact is missing or invalid overall_risk", blockers)

    def test_extract_security_risk_rejects_forged_workflow_run(self):
        original_paginate_key = policy.github_paginate_key
        original_request = policy.github_request
        try:
            def fake_paginate_key(path, token, key):
                if "/commits/" in path:
                    return [
                        {
                            "name": "security-review",
                            "started_at": "2026-01-01T00:00:00Z",
                            "details_url": "https://github.com/block/proto-fleet/actions/runs/123/job/456",
                        }
                    ]
                return []

            policy.github_paginate_key = fake_paginate_key
            policy.github_request = lambda method, path, token, body=None: {
                "path": ".github/workflows/attacker.yml",
                "head_sha": "abc123",
                "event": "pull_request",
            }
            risk, blockers = policy.extract_security_risk(
                "block",
                "proto-fleet",
                "base123",
                "abc123",
                "token",
                "security-review",
                ".github/workflows/codex-security-review.yml",
                "codex-security-review-result",
            )
        finally:
            policy.github_paginate_key = original_paginate_key
            policy.github_request = original_request

        self.assertIsNone(risk)
        self.assertIn(
            "Codex security-review run path is '.github/workflows/attacker.yml', expected '.github/workflows/codex-security-review.yml'",
            blockers,
        )

    def test_evaluate_policy_allows_trusted_low_risk_pr(self):
        original_paginate = policy.github_paginate
        original_request = policy.github_request
        original_trusted_author_reasons = policy.trusted_author_reasons
        original_check_statuses = policy.check_statuses
        original_extract_security_risk = policy.extract_security_risk
        original_latest_check_runs = policy.latest_check_runs
        original_workflow_runs = policy.latest_workflow_runs
        try:
            def fake_paginate(path, token):
                if path.endswith("/commits/abc123/pulls"):
                    return [{"number": 123, "state": "open", "head": {"sha": "abc123"}}]
                if path.endswith("/files"):
                    return [
                        {
                            "filename": "client/src/foo.ts",
                            "additions": 2,
                            "deletions": 1,
                            "patch": "@@\n+const x = 1",
                        }
                    ]
                if path.endswith("/commits"):
                    return [{"sha": "abc123", "author": {"login": "author"}, "committer": {"login": "author"}}]
                if path.endswith("/reviews"):
                    return []
                return []

            policy.github_paginate = fake_paginate
            policy.github_request = lambda method, path, token, body=None: {
                "state": "open",
                "head": {"sha": "abc123"},
                "base": {"sha": "base123"},
            }
            policy.trusted_author_reasons = lambda author, trusted_authors, owner, token: (
                True,
                [f"author @{author} is explicitly trusted"],
            )
            policy.check_statuses = lambda owner, repo, head_sha, required_checks, token, latest_by_name=None: (True, [])
            policy.extract_security_risk = lambda owner, repo, base_sha, head_sha, token, check_name, workflow_path, artifact_name, latest_by_name=None: (
                "LOW",
                [],
            )
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {}
            policy.latest_workflow_runs = lambda owner, repo, head_sha, event, token: {
                ".github/workflows/pr-gate.yml": {"actor": {"login": "author"}},
            }
            result = policy.evaluate_policy(
                config={
                    "trusted_authors": ["author"],
                    "minimum_human_approvals": 1,
                    "security_review_check": "security-review",
                    "security_review_workflow_path": ".github/workflows/codex-security-review.yml",
                    "security_review_artifact": "codex-security-review-result",
                    "low_risk": {
                        "max_changed_files": 10,
                        "max_file_changes": 80,
                        "max_total_changes": 200,
                        "minimum_ai_confidence": 0.85,
                        "trusted_actor_workflow": {
                            "workflow_path": ".github/workflows/pr-gate.yml",
                            "event": "pull_request",
                        },
                        "allowed_security_risks": ["LOW", "NONE"],
                        "required_checks": ["Gate"],
                        "deny_paths": [".github/**"],
                        "content_deny_added_patterns": [],
                    },
                },
                owner="block",
                repo="proto-fleet",
                pr_number=123,
                author="author",
                base_sha="base123",
                head_sha="abc123",
                token="token",
                classifier_output='{"risk":"low","confidence":0.95,"requires_human_review":false,"reasons":["small"]}',
            )
        finally:
            policy.github_paginate = original_paginate
            policy.github_request = original_request
            policy.trusted_author_reasons = original_trusted_author_reasons
            policy.check_statuses = original_check_statuses
            policy.extract_security_risk = original_extract_security_risk
            policy.latest_check_runs = original_latest_check_runs
            policy.latest_workflow_runs = original_workflow_runs

        self.assertTrue(result.passed)
        self.assertEqual(result.decision, "trusted-author-low-risk")
        self.assertEqual(result.reasons, [])

    def test_evaluate_policy_prefers_low_risk_when_human_approval_also_exists(self):
        original_paginate = policy.github_paginate
        original_request = policy.github_request
        original_reviewer_has_authority = policy.reviewer_has_authority
        original_trusted_author_reasons = policy.trusted_author_reasons
        original_check_statuses = policy.check_statuses
        original_extract_security_risk = policy.extract_security_risk
        original_latest_check_runs = policy.latest_check_runs
        original_workflow_runs = policy.latest_workflow_runs
        try:
            def fake_paginate(path, token):
                if path.endswith("/commits/abc123/pulls"):
                    return [{"number": 123, "state": "open", "head": {"sha": "abc123"}}]
                if path.endswith("/files"):
                    return [
                        {
                            "filename": "client/src/foo.ts",
                            "additions": 2,
                            "deletions": 1,
                            "patch": "@@\n+const x = 1",
                        }
                    ]
                if path.endswith("/commits"):
                    return [{"sha": "abc123", "author": {"login": "author"}, "committer": {"login": "author"}}]
                if path.endswith("/reviews"):
                    return [
                        {
                            "user": {"login": "reviewer", "type": "User"},
                            "state": "APPROVED",
                            "commit_id": "abc123",
                            "submitted_at": "2026-01-01T00:00:00Z",
                        }
                    ]
                return []

            policy.github_paginate = fake_paginate
            policy.github_request = lambda method, path, token, body=None: {
                "state": "open",
                "head": {"sha": "abc123"},
                "base": {"sha": "base123"},
            }
            policy.reviewer_has_authority = lambda owner, repo, username, token: True
            policy.trusted_author_reasons = lambda author, trusted_authors, owner, token: (
                True,
                [f"author @{author} is explicitly trusted"],
            )
            policy.check_statuses = lambda owner, repo, head_sha, required_checks, token, latest_by_name=None: (True, [])
            policy.extract_security_risk = lambda owner, repo, base_sha, head_sha, token, check_name, workflow_path, artifact_name, latest_by_name=None: (
                "LOW",
                [],
            )
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {}
            policy.latest_workflow_runs = lambda owner, repo, head_sha, event, token: {
                ".github/workflows/pr-gate.yml": {"actor": {"login": "author"}},
            }
            result = policy.evaluate_policy(
                config={
                    "trusted_authors": ["author"],
                    "minimum_human_approvals": 1,
                    "security_review_check": "security-review",
                    "security_review_workflow_path": ".github/workflows/codex-security-review.yml",
                    "security_review_artifact": "codex-security-review-result",
                    "low_risk": {
                        "max_changed_files": 10,
                        "max_file_changes": 80,
                        "max_total_changes": 200,
                        "minimum_ai_confidence": 0.85,
                        "trusted_actor_workflow": {
                            "workflow_path": ".github/workflows/pr-gate.yml",
                            "event": "pull_request",
                        },
                        "allowed_security_risks": ["LOW", "NONE"],
                        "required_checks": [],
                        "deny_paths": [".github/**"],
                        "content_deny_added_patterns": [],
                    },
                },
                owner="block",
                repo="proto-fleet",
                pr_number=123,
                author="author",
                base_sha="base123",
                head_sha="abc123",
                token="token",
                classifier_output='{"risk":"low","confidence":0.95,"requires_human_review":false,"reasons":["small"]}',
            )
        finally:
            policy.github_paginate = original_paginate
            policy.github_request = original_request
            policy.reviewer_has_authority = original_reviewer_has_authority
            policy.trusted_author_reasons = original_trusted_author_reasons
            policy.check_statuses = original_check_statuses
            policy.extract_security_risk = original_extract_security_risk
            policy.latest_check_runs = original_latest_check_runs
            policy.latest_workflow_runs = original_workflow_runs

        self.assertTrue(result.passed)
        self.assertEqual(result.decision, "trusted-author-low-risk")
        self.assertIn("current authorized human approvals: reviewer", result.human_review_reasons)

    def test_evaluate_policy_blocks_human_approval_with_unknown_commit_identity(self):
        original_paginate = policy.github_paginate
        original_request = policy.github_request
        original_reviewer_has_authority = policy.reviewer_has_authority
        original_trusted_author_reasons = policy.trusted_author_reasons
        original_check_statuses = policy.check_statuses
        original_extract_security_risk = policy.extract_security_risk
        original_latest_check_runs = policy.latest_check_runs
        original_workflow_runs = policy.latest_workflow_runs
        try:
            def fake_paginate(path, token):
                if path.endswith("/commits/abc123/pulls"):
                    return [{"number": 123, "state": "open", "head": {"sha": "abc123"}}]
                if path.endswith("/files"):
                    return [{"filename": "client/src/foo.ts", "additions": 1, "deletions": 0, "patch": "@@\n+const x = 1"}]
                if path.endswith("/commits"):
                    return [{"sha": "def456", "author": None, "committer": None}]
                if path.endswith("/reviews"):
                    return [
                        {
                            "user": {"login": "reviewer", "type": "User"},
                            "state": "APPROVED",
                            "commit_id": "abc123",
                            "submitted_at": "2026-01-01T00:00:00Z",
                            "author_association": "MEMBER",
                        }
                    ]
                return []

            policy.github_paginate = fake_paginate
            policy.github_request = lambda method, path, token, body=None: {
                "state": "open",
                "head": {"sha": "abc123"},
                "base": {"sha": "base123"},
            }
            policy.reviewer_has_authority = lambda owner, repo, username, token: True
            policy.trusted_author_reasons = lambda author, trusted_authors, owner, token: (
                True,
                [f"author @{author} is explicitly trusted"],
            )
            policy.check_statuses = lambda owner, repo, head_sha, required_checks, token, latest_by_name=None: (True, [])
            policy.extract_security_risk = lambda owner, repo, base_sha, head_sha, token, check_name, workflow_path, artifact_name, latest_by_name=None: (
                "LOW",
                [],
            )
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {}
            policy.latest_workflow_runs = lambda owner, repo, head_sha, event, token: {
                ".github/workflows/pr-gate.yml": {"actor": {"login": "author"}},
            }
            result = policy.evaluate_policy(
                config={
                    "trusted_authors": ["author"],
                    "minimum_human_approvals": 1,
                    "security_review_check": "security-review",
                    "security_review_workflow_path": ".github/workflows/codex-security-review.yml",
                    "security_review_artifact": "codex-security-review-result",
                    "low_risk": {
                        "max_changed_files": 10,
                        "max_file_changes": 80,
                        "max_total_changes": 200,
                        "minimum_ai_confidence": 0.85,
                        "trusted_actor_workflow": {
                            "workflow_path": ".github/workflows/pr-gate.yml",
                            "event": "pull_request",
                        },
                        "allowed_security_risks": ["LOW", "NONE"],
                        "required_checks": [],
                        "deny_paths": [],
                        "content_deny_added_patterns": [],
                    },
                },
                owner="block",
                repo="proto-fleet",
                pr_number=123,
                author="author",
                base_sha="base123",
                head_sha="abc123",
                token="token",
                classifier_output='{"risk":"low","confidence":0.95,"requires_human_review":false,"reasons":["small"]}',
            )
        finally:
            policy.github_paginate = original_paginate
            policy.github_request = original_request
            policy.reviewer_has_authority = original_reviewer_has_authority
            policy.trusted_author_reasons = original_trusted_author_reasons
            policy.check_statuses = original_check_statuses
            policy.extract_security_risk = original_extract_security_risk
            policy.latest_check_runs = original_latest_check_runs
            policy.latest_workflow_runs = original_workflow_runs

        self.assertFalse(result.passed)
        self.assertEqual(result.decision, "needs-human-review")
        self.assertIn("current head has commits without GitHub-linked authors or committers: def456", result.reasons)

    def test_evaluate_policy_blocks_stale_pr_head(self):
        original_request = policy.github_request
        try:
            policy.github_request = lambda method, path, token, body=None: {
                "state": "open",
                "head": {"sha": "newhead"},
                "base": {"sha": "base123"},
            }
            result = policy.evaluate_policy(
                config={
                    "trusted_authors": ["author"],
                    "minimum_human_approvals": 1,
                    "low_risk": {
                        "max_changed_files": 10,
                        "max_file_changes": 80,
                        "max_total_changes": 200,
                        "minimum_ai_confidence": 0.85,
                        "trusted_actor_workflow": {
                            "workflow_path": ".github/workflows/pr-gate.yml",
                            "event": "pull_request",
                        },
                        "allowed_security_risks": ["LOW", "NONE"],
                        "required_checks": [],
                        "deny_paths": [],
                        "content_deny_added_patterns": [],
                    },
                },
                owner="block",
                repo="proto-fleet",
                pr_number=123,
                author="author",
                base_sha="base123",
                head_sha="abc123",
                token="token",
                classifier_output='{"risk":"low","confidence":0.95,"requires_human_review":false,"reasons":["small"]}',
            )
        finally:
            policy.github_request = original_request

        self.assertFalse(result.passed)
        self.assertEqual(result.decision, "needs-human-review")
        self.assertEqual(result.reasons, ["pull request #123 head is newhead, expected abc123"])

    def test_evaluate_policy_ignores_authenticated_actor_approval(self):
        original_paginate = policy.github_paginate
        original_request = policy.github_request
        original_reviewer_has_authority = policy.reviewer_has_authority
        original_trusted_author_reasons = policy.trusted_author_reasons
        original_check_statuses = policy.check_statuses
        original_extract_security_risk = policy.extract_security_risk
        original_latest_check_runs = policy.latest_check_runs
        original_workflow_runs = policy.latest_workflow_runs
        try:
            def fake_paginate(path, token):
                if path.endswith("/commits/abc123/pulls"):
                    return [{"number": 123, "state": "open", "head": {"sha": "abc123"}}]
                if path.endswith("/files"):
                    return [{"filename": "client/src/foo.ts", "additions": 1, "deletions": 0, "patch": "@@\n+const x = 1"}]
                if path.endswith("/commits"):
                    return [{"sha": "abc123", "author": {"login": "author"}, "committer": {"login": "author"}}]
                if path.endswith("/reviews"):
                    return [
                        {
                            "user": {"login": "pusher", "type": "User"},
                            "state": "APPROVED",
                            "commit_id": "abc123",
                            "submitted_at": "2026-01-01T00:00:00Z",
                            "author_association": "MEMBER",
                        }
                    ]
                return []

            policy.github_paginate = fake_paginate
            policy.github_request = lambda method, path, token, body=None: {
                "state": "open",
                "head": {"sha": "abc123"},
                "base": {"sha": "base123"},
            }
            policy.reviewer_has_authority = lambda owner, repo, username, token: True
            policy.trusted_author_reasons = lambda author, trusted_authors, owner, token: (
                True,
                [f"author @{author} is explicitly trusted"],
            )
            policy.check_statuses = lambda owner, repo, head_sha, required_checks, token, latest_by_name=None: (True, [])
            policy.extract_security_risk = lambda owner, repo, base_sha, head_sha, token, check_name, workflow_path, artifact_name, latest_by_name=None: (
                "LOW",
                [],
            )
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {}
            policy.latest_workflow_runs = lambda owner, repo, head_sha, event, token: {
                ".github/workflows/pr-gate.yml": {"actor": {"login": "pusher"}},
            }
            result = policy.evaluate_policy(
                config={
                    "trusted_authors": ["author", "pusher"],
                    "minimum_human_approvals": 1,
                    "security_review_check": "security-review",
                    "security_review_workflow_path": ".github/workflows/codex-security-review.yml",
                    "security_review_artifact": "codex-security-review-result",
                    "low_risk": {
                        "max_changed_files": 10,
                        "max_file_changes": 80,
                        "max_total_changes": 200,
                        "minimum_ai_confidence": 0.85,
                        "trusted_actor_workflow": {
                            "workflow_path": ".github/workflows/pr-gate.yml",
                            "event": "pull_request",
                        },
                        "allowed_security_risks": ["LOW", "NONE"],
                        "required_checks": [],
                        "deny_paths": [],
                        "content_deny_added_patterns": [],
                    },
                },
                owner="block",
                repo="proto-fleet",
                pr_number=123,
                author="author",
                base_sha="base123",
                head_sha="abc123",
                token="token",
                classifier_output='{"risk":"medium","confidence":0.95,"requires_human_review":true,"reasons":["not low"]}',
            )
        finally:
            policy.github_paginate = original_paginate
            policy.github_request = original_request
            policy.reviewer_has_authority = original_reviewer_has_authority
            policy.trusted_author_reasons = original_trusted_author_reasons
            policy.check_statuses = original_check_statuses
            policy.extract_security_risk = original_extract_security_risk
            policy.latest_check_runs = original_latest_check_runs
            policy.latest_workflow_runs = original_workflow_runs

        self.assertFalse(result.passed)
        self.assertEqual(result.decision, "needs-human-review")
        self.assertIn("0 current human approval(s), need 1", result.reasons)
        self.assertIn("ignored approvals from PR contributors: pusher", result.human_review_reasons)

    def test_human_review_state_ignores_unauthorized_approvals(self):
        original = policy.reviewer_has_authority
        try:
            policy.reviewer_has_authority = lambda owner, repo, username, token: username == "member"
            reviews = [
                {
                    "user": {"login": "outsider", "type": "User"},
                    "state": "APPROVED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:00Z",
                    "author_association": "NONE",
                },
                {
                    "user": {"login": "member", "type": "User"},
                    "state": "APPROVED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:01Z",
                    "author_association": "MEMBER",
                },
            ]
            ok, reasons, blockers = policy.human_review_state(
                reviews,
                "abc123",
                "author",
                1,
                "block",
                "proto-fleet",
                "token",
            )
        finally:
            policy.reviewer_has_authority = original

        self.assertTrue(ok)
        self.assertEqual(blockers, [])
        self.assertIn("current authorized human approvals: member", reasons)
        self.assertIn("ignored unauthorized review states from: outsider", reasons)

    def test_human_review_state_ignores_head_contributor_approvals(self):
        original = policy.reviewer_has_authority
        try:
            policy.reviewer_has_authority = lambda owner, repo, username, token: True
            reviews = [
                {
                    "user": {"login": "contributor", "type": "User"},
                    "state": "APPROVED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:00Z",
                    "author_association": "MEMBER",
                },
                {
                    "user": {"login": "independent", "type": "User"},
                    "state": "APPROVED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:01Z",
                    "author_association": "MEMBER",
                },
            ]
            ok, reasons, blockers = policy.human_review_state(
                reviews,
                "abc123",
                "author",
                1,
                "block",
                "proto-fleet",
                "token",
                {"contributor"},
            )
        finally:
            policy.reviewer_has_authority = original

        self.assertTrue(ok)
        self.assertEqual(blockers, [])
        self.assertIn("current authorized human approvals: independent", reasons)
        self.assertIn("ignored approvals from PR contributors: contributor", reasons)

    def test_human_review_state_keeps_change_request_after_comment(self):
        original = policy.reviewer_has_authority
        try:
            policy.reviewer_has_authority = lambda owner, repo, username, token: True
            reviews = [
                {
                    "user": {"login": "reviewer", "type": "User"},
                    "state": "CHANGES_REQUESTED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:00Z",
                },
                {
                    "user": {"login": "reviewer", "type": "User"},
                    "state": "COMMENTED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:01Z",
                },
            ]
            ok, _reasons, blockers = policy.human_review_state(
                reviews, "abc123", "author", 1, "block", "proto-fleet", "token"
            )
        finally:
            policy.reviewer_has_authority = original

        self.assertFalse(ok)
        self.assertIn("changes requested by reviewer", blockers)

    def test_human_review_state_caches_reviewer_authority_by_login(self):
        original = policy.reviewer_has_authority
        calls = []
        try:
            def fake_reviewer_has_authority(owner, repo, username, token):
                calls.append(username)
                return True

            policy.reviewer_has_authority = fake_reviewer_has_authority
            reviews = [
                {
                    "user": {"login": "Reviewer", "type": "User"},
                    "state": "CHANGES_REQUESTED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:00Z",
                },
                {
                    "user": {"login": "reviewer", "type": "User"},
                    "state": "APPROVED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:01Z",
                },
            ]
            ok, reasons, blockers = policy.human_review_state(
                reviews, "abc123", "author", 1, "block", "proto-fleet", "token"
            )
        finally:
            policy.reviewer_has_authority = original

        self.assertTrue(ok)
        self.assertEqual(blockers, [])
        self.assertEqual(calls, ["Reviewer"])
        self.assertIn("current authorized human approvals: Reviewer", reasons)

    def test_human_review_state_clears_change_request_on_approval_or_dismissal(self):
        original = policy.reviewer_has_authority
        try:
            policy.reviewer_has_authority = lambda owner, repo, username, token: True
            approved_reviews = [
                {
                    "user": {"login": "reviewer", "type": "User"},
                    "state": "CHANGES_REQUESTED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:00Z",
                },
                {
                    "user": {"login": "reviewer", "type": "User"},
                    "state": "APPROVED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:01Z",
                },
            ]
            dismissed_reviews = [
                {
                    "user": {"login": "reviewer", "type": "User"},
                    "state": "CHANGES_REQUESTED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:00Z",
                },
                {
                    "user": {"login": "reviewer", "type": "User"},
                    "state": "DISMISSED",
                    "commit_id": "abc123",
                    "submitted_at": "2026-01-01T00:00:01Z",
                },
            ]
            approved_ok, _approved_reasons, approved_blockers = policy.human_review_state(
                approved_reviews, "abc123", "author", 1, "block", "proto-fleet", "token"
            )
            dismissed_ok, _dismissed_reasons, dismissed_blockers = policy.human_review_state(
                dismissed_reviews, "abc123", "author", 1, "block", "proto-fleet", "token"
            )
        finally:
            policy.reviewer_has_authority = original

        self.assertTrue(approved_ok)
        self.assertEqual(approved_blockers, [])
        self.assertFalse(dismissed_ok)
        self.assertNotIn("changes requested by reviewer", dismissed_blockers)

    def test_write_result(self):
        result = policy.PolicyResult(
            passed=True,
            decision="trusted-author-low-risk",
            low_risk_reasons=["small change"],
        )
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir) / "result.json"
            policy.write_result(result, str(path))
            self.assertEqual(
                path.read_text(encoding="utf-8"),
                '{\n  "passed": true,\n  "decision": "trusted-author-low-risk",\n  "enforced": true,\n  "reasons": [],\n  "low_risk_reasons": [\n    "small change"\n  ],\n  "human_review_reasons": []\n}\n',
            )


if __name__ == "__main__":
    unittest.main()
