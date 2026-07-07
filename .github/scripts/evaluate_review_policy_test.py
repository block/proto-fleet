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

    def test_extract_security_risk_requires_current_head(self):
        comments = [
            {
                "user": {"login": "github-actions[bot]"},
                "body": "<!-- codex-security-review -->\nabc123\n**Overall Risk**: LOW",
            }
        ]
        risk, blockers = policy.extract_security_risk(comments, "abc123")
        self.assertEqual(risk, "LOW")
        self.assertEqual(blockers, [])

        risk, blockers = policy.extract_security_risk(comments, "def456")
        self.assertIsNone(risk)
        self.assertEqual(blockers, ["Codex security review comment is stale for this PR head"])

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
