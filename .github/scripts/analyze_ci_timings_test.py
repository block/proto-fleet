#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import json
import sys
import unittest
from pathlib import Path

SCRIPT_PATH = Path(__file__).with_name("analyze_ci_timings.py")
BASELINE_PATH = Path(__file__).with_name("ci_timing_baseline.json")
SPEC = importlib.util.spec_from_file_location("analyze_ci_timings", SCRIPT_PATH)
assert SPEC is not None and SPEC.loader is not None
timings = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = timings
SPEC.loader.exec_module(timings)


def timestamp(seconds: int) -> str:
    return f"2026-01-01T00:{seconds // 60:02d}:{seconds % 60:02d}Z"


def step(name: str, start: int, end: int) -> dict:
    return {
        "name": name,
        "started_at": timestamp(start),
        "completed_at": timestamp(end),
    }


def job(job_id: int, name: str, start: int, end: int, steps: list[dict]) -> dict:
    return {
        "id": job_id,
        "name": name,
        "started_at": timestamp(start),
        "completed_at": timestamp(end),
        "steps": steps,
    }


class ClassificationTest(unittest.TestCase):
    def test_normalizes_matrix_shards(self):
        self.assertEqual(
            timings.normalize_step_name("Run Playwright tests (mobile - shard 14)"),
            "Run Playwright tests (mobile - shard N)",
        )

    def test_classifies_representative_steps(self):
        cases = {
            "Set up job": timings.CATEGORY_PLATFORM,
            "Install client dependencies": timings.CATEGORY_SETUP,
            "Build E2E Docker images": timings.CATEGORY_SETUP,
            "Export pre-baked Docker images": timings.CATEGORY_SETUP,
            "Run Playwright tests (desktop - shard 3)": timings.CATEGORY_TEST,
            "Run onboarding visual spec (mobile)": timings.CATEGORY_TEST,
            "Generate code": timings.CATEGORY_VALIDATION,
            "Detect changed areas": timings.CATEGORY_ORCHESTRATION,
            "Upload blob report": timings.CATEGORY_REPORTING,
            "A new unexplained phase": timings.CATEGORY_UNCLASSIFIED,
        }
        for name, expected in cases.items():
            with self.subTest(name=name):
                self.assertEqual(timings.classify_step(name), expected)


class SelectionTest(unittest.TestCase):
    def test_associates_empty_run_pull_lists_by_branch(self):
        runs = [
            {
                "id": 1,
                "head_branch": "feature/a",
                "pull_requests": [],
                "conclusion": "success",
                "run_number": 10,
            }
        ]
        pulls = [{"number": 42, "title": "Feature A", "head": {"ref": "feature/a"}}]
        timings.associate_runs_with_pull_requests(runs, pulls)
        self.assertEqual(runs[0]["_pr_number"], 42)
        self.assertEqual(runs[0]["_group_key"], "pr:42")

    def test_limits_detailed_runs_per_pr(self):
        runs = []
        for number in range(1, 5):
            runs.append(
                {
                    "id": number,
                    "run_number": number,
                    "conclusion": "success",
                    "_group_key": "pr:42",
                }
            )
        selected = timings.select_detailed_runs(runs, {"success"}, 2, 0)
        self.assertEqual([run["run_number"] for run in selected], [4, 3])


class AnalysisTest(unittest.TestCase):
    def make_fixture(self):
        run = {
            "id": 100,
            "run_number": 7,
            "run_attempt": 1,
            "run_started_at": timestamp(0),
            "updated_at": timestamp(200),
            "head_branch": "feature/ci",
            "html_url": "https://example.invalid/run/100",
            "conclusion": "success",
            "_pr_number": 9,
            "_pr_title": "CI change",
            "_group_key": "pr:9",
        }
        jobs = [
            job(
                1,
                "ProtoFleet E2E Tests / e2e-tests (0, mobile)",
                0,
                100,
                [
                    step("Set up job", 0, 10),
                    step("Install client dependencies", 10, 30),
                    step("Run Playwright tests (mobile - shard 0)", 30, 90),
                ],
            ),
            job(
                2,
                "Client Checks / Build",
                0,
                50,
                [step("Checkout code", 0, 5), step("Build", 5, 45)],
            ),
        ]
        for job_id in range(3, 41):
            jobs.append(
                job(job_id, f"small-{job_id}", 0, 1, [step("Set up job", 0, 1)])
            )
        return run, jobs

    def test_analyzes_runner_time_and_e2e_overhead(self):
        run, jobs = self.make_fixture()
        analysis = timings.analyze(
            "block/proto-fleet", "pr-gate.yml", [run], [run], {run["id"]: jobs}
        )
        headline = analysis["detail"]["headline"]
        self.assertEqual(analysis["detail"]["jobs"], 40)
        self.assertAlmostEqual(analysis["detail"]["runner_minutes"], 3.13)
        self.assertAlmostEqual(headline["test_minutes"], 1.0)
        self.assertAlmostEqual(headline["validation_minutes"], 0.67)
        self.assertAlmostEqual(headline["overhead_minutes"], 1.47)
        self.assertEqual(analysis["detail"]["profiles"]["full"]["runs"], 1)
        protofleet = analysis["detail"]["e2e"]["suites"]["ProtoFleet E2E Tests"]
        self.assertAlmostEqual(protofleet["test_percent"], 60.0)
        self.assertAlmostEqual(protofleet["overhead_percent"], 40.0)
        self.assertAlmostEqual(protofleet["mean_overhead_minutes"], 0.67)

    def test_compares_and_renders_baseline(self):
        run, jobs = self.make_fixture()
        current = timings.analyze(
            "block/proto-fleet", "pr-gate.yml", [run], [run], {run["id"]: jobs}
        )
        current["selection"] = {"history_runs": 1}
        baseline = {**current, "metrics": dict(current["metrics"])}
        baseline["metrics"]["full_mean_overhead_minutes"] = 1.0
        comparison = timings.compare_with_baseline(current, baseline)
        metric = next(
            item
            for item in comparison["metrics"]
            if item["metric"] == "full_mean_overhead_minutes"
        )
        self.assertEqual(metric["delta_percent"], 47.0)
        report = timings.render_markdown(current, 5, comparison)
        self.assertIn("Runner-time split", report)
        self.assertIn("Baseline comparison", report)
        self.assertIn("Run Playwright tests (mobile - shard N)", report)

    def test_checked_in_baseline_matches_current_schema(self):
        baseline = json.loads(BASELINE_PATH.read_text(encoding="utf-8"))
        self.assertEqual(baseline["schema_version"], timings.SCHEMA_VERSION)
        self.assertEqual(set(baseline["metrics"]), set(timings.COMPARISON_METRICS))


if __name__ == "__main__":
    unittest.main()
