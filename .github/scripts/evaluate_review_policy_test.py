#!/usr/bin/env python3

import unittest
import tempfile
from pathlib import Path

import evaluate_review_policy as policy


class ReviewPolicyTest(unittest.TestCase):
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

    def test_low_risk_preflight_blocks_before_classifier(self):
        original_paginate = policy.github_paginate
        original_trusted_author_reasons = policy.trusted_author_reasons
        try:
            policy.github_paginate = lambda path, token: [
                {"filename": ".github/workflows/review-policy.yml", "additions": 2, "deletions": 1},
                {"filename": "docs/readme.md", "additions": 300, "deletions": 0},
            ]
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
                token="token",
            )
        finally:
            policy.github_paginate = original_paginate
            policy.trusted_author_reasons = original_trusted_author_reasons

        self.assertFalse(result["eligible"])
        self.assertIn("author @author is not in trusted_authors", result["blockers"])
        self.assertIn("303 changed lines exceeds limit 200", result["blockers"])
        self.assertIn("denied paths changed: .github/workflows/review-policy.yml", result["blockers"])

    def test_extract_run_id(self):
        self.assertEqual(
            policy.extract_run_id("https://github.com/block/proto-fleet/actions/runs/123/job/456"),
            "123",
        )
        self.assertIsNone(policy.extract_run_id("https://github.com/block/proto-fleet/runs/123"))

    def test_human_review_state_ignores_unauthorized_approvals(self):
        original = policy.reviewer_has_authority
        try:
            policy.reviewer_has_authority = lambda owner, repo, username, association, token: username == "member"
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

    def test_human_review_state_keeps_change_request_after_comment(self):
        original = policy.reviewer_has_authority
        try:
            policy.reviewer_has_authority = lambda owner, repo, username, association, token: True
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

    def test_human_review_state_clears_change_request_on_approval_or_dismissal(self):
        original = policy.reviewer_has_authority
        try:
            policy.reviewer_has_authority = lambda owner, repo, username, association, token: True
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
