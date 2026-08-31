#!/usr/bin/env python3
r"""Analyze GitHub Actions runner time by step category.

The default selection reproduces the CI investigation performed on 2026-08-31:
the latest 100 completed PR Gate runs, with detailed job data for up to the
newest three successful runs per pull request. GitHub's API returns rerun jobs
separately, so the analyzer requests only the latest attempt for each run.

Examples:

    GITHUB_TOKEN=... python3 .github/scripts/analyze_ci_timings.py \
      --json-out /tmp/proto-fleet-ci-baseline.json

    GITHUB_TOKEN=... python3 .github/scripts/analyze_ci_timings.py \
      --baseline .github/scripts/ci_timing_baseline.json \
      --json-out /tmp/proto-fleet-ci-after.json \
      --fail-on-regression-pct 10

Use ``--max-runs-per-pr 0 --detail-limit 100 --conclusions
success,failure,cancelled,startup_failure`` for job-level detail across every
attempt. That mode requires an authenticated token because it makes at least
one API request per workflow run.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import datetime as dt
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import defaultdict
from pathlib import Path
from typing import Any, Iterable

API_ROOT = "https://api.github.com"
API_VERSION = "2022-11-28"
SCHEMA_VERSION = 1
TRANSIENT_HTTP_STATUSES = {429, 500, 502, 503, 504}

CATEGORY_PLATFORM = "platform"
CATEGORY_SETUP = "setup"
CATEGORY_TEST = "test"
CATEGORY_VALIDATION = "validation_build"
CATEGORY_ORCHESTRATION = "orchestration"
CATEGORY_REPORTING = "reporting_teardown"
CATEGORY_UNCLASSIFIED = "unclassified"

OVERHEAD_CATEGORIES = {
    CATEGORY_PLATFORM,
    CATEGORY_SETUP,
    CATEGORY_ORCHESTRATION,
    CATEGORY_REPORTING,
}

COMPARISON_METRICS = {
    "full_mean_runner_minutes": "Full-run mean runner minutes",
    "full_mean_overhead_minutes": "Full-run mean overhead minutes",
    "full_median_wall_minutes": "Full-run median wall minutes",
    "overall_overhead_percent": "Overall overhead share",
    "protofleet_e2e_mean_job_minutes": "ProtoFleet E2E mean shard minutes",
    "protofleet_e2e_mean_overhead_minutes": (
        "ProtoFleet E2E mean shard overhead minutes"
    ),
}


class AnalysisError(RuntimeError):
    """A user-actionable error while collecting or analyzing timings."""


class GitHubClient:
    def __init__(self, token: str | None, timeout: float = 30.0) -> None:
        self.token = token
        self.timeout = timeout

    def get_json(self, path: str) -> Any:
        url = path if path.startswith("https://") else API_ROOT + path
        headers = {
            "Accept": "application/vnd.github+json",
            "User-Agent": "proto-fleet-ci-timing-analyzer",
            "X-GitHub-Api-Version": API_VERSION,
        }
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"

        for attempt in range(4):
            request = urllib.request.Request(url, headers=headers, method="GET")
            try:
                with urllib.request.urlopen(request, timeout=self.timeout) as response:
                    return json.loads(response.read().decode("utf-8"))
            except urllib.error.HTTPError as error:
                body = error.read().decode("utf-8", errors="replace")
                if error.code in TRANSIENT_HTTP_STATUSES and attempt < 3:
                    time.sleep(2**attempt)
                    continue
                if error.code == 403 and "rate limit" in body.casefold():
                    reset = error.headers.get("X-RateLimit-Reset")
                    reset_hint = ""
                    if reset and reset.isdigit():
                        instant = dt.datetime.fromtimestamp(
                            int(reset), tz=dt.timezone.utc
                        ).isoformat()
                        reset_hint = f" The anonymous quota resets at {instant}."
                    auth_hint = (
                        " Set GITHUB_TOKEN or GH_TOKEN to raise the API limit."
                        if not self.token
                        else " The configured token also exhausted its API quota."
                    )
                    raise AnalysisError(
                        f"GitHub API rate limit exceeded.{reset_hint}{auth_hint}"
                    ) from error
                raise AnalysisError(
                    f"GitHub API GET {url} failed with HTTP {error.code}: {body}"
                ) from error
            except urllib.error.URLError as error:
                if attempt < 3:
                    time.sleep(2**attempt)
                    continue
                raise AnalysisError(f"GitHub API GET {url} failed: {error}") from error
        raise AssertionError("unreachable")


def paginate_key(
    client: GitHubClient, path: str, key: str, limit: int
) -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    page = 1
    separator = "&" if "?" in path else "?"
    while len(items) < limit:
        response = client.get_json(
            f"{path}{separator}per_page={min(100, limit - len(items))}&page={page}"
        )
        if not isinstance(response, dict) or not isinstance(response.get(key), list):
            raise AnalysisError(f"Expected a {key!r} list from GitHub API path {path}")
        batch = response[key]
        items.extend(batch)
        if len(batch) < 100:
            break
        page += 1
    return items[:limit]


def paginate_list(client: GitHubClient, path: str, limit: int) -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    page = 1
    separator = "&" if "?" in path else "?"
    while len(items) < limit:
        response = client.get_json(
            f"{path}{separator}per_page={min(100, limit - len(items))}&page={page}"
        )
        if not isinstance(response, list):
            raise AnalysisError(f"Expected a list from GitHub API path {path}")
        items.extend(response)
        if len(response) < 100:
            break
        page += 1
    return items[:limit]


def infer_repository() -> str:
    result = subprocess.run(
        ["git", "config", "--get", "remote.origin.url"],
        check=False,
        capture_output=True,
        text=True,
    )
    remote = result.stdout.strip()
    patterns = (
        r"git@github\.com:([^/]+/[^/]+?)(?:\.git)?$",
        r"https://github\.com/([^/]+/[^/]+?)(?:\.git)?$",
        r"ssh://git@github\.com/([^/]+/[^/]+?)(?:\.git)?$",
    )
    for pattern in patterns:
        match = re.fullmatch(pattern, remote)
        if match:
            return match.group(1)
    raise AnalysisError(
        "Could not infer owner/repository from origin; pass --repo OWNER/REPO"
    )


def parse_timestamp(value: str | None) -> dt.datetime | None:
    if not value:
        return None
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    return dt.datetime.fromisoformat(value)


def duration_seconds(start: str | None, end: str | None) -> float:
    started = parse_timestamp(start)
    completed = parse_timestamp(end)
    if started is None or completed is None:
        return 0.0
    return max(0.0, (completed - started).total_seconds())


def quantile(values: Iterable[float], percentile: float) -> float:
    ordered = sorted(values)
    if not ordered:
        return 0.0
    index = (len(ordered) - 1) * percentile
    lower = int(index)
    upper = min(lower + 1, len(ordered) - 1)
    fraction = index - lower
    return ordered[lower] + (ordered[upper] - ordered[lower]) * fraction


def normalize_step_name(name: str) -> str:
    normalized = re.sub(r"shard\s+\d+", "shard N", name, flags=re.IGNORECASE)
    normalized = re.sub(
        r"\(\d+,\s*(mobile|desktop)\)",
        r"(N, \1)",
        normalized,
        flags=re.IGNORECASE,
    )
    return re.sub(r"\s+", " ", normalized).strip()


def classify_step(name: str) -> str:
    lowered = name.casefold()
    if re.search(r"^(set up job|complete job)$", lowered):
        return CATEGORY_PLATFORM
    if re.search(
        r"^post |upload|merge .*report|submit .*result|dump .*log|cleanup|"
        r"tear.?down|^stop |archive|note report|collect .*log|publish .*report",
        lowered,
    ):
        return CATEGORY_REPORTING
    if re.search(r"build e2e docker images|build fake-proto-rig simulator", lowered):
        return CATEGORY_SETUP
    if re.search(
        r"checkout|setup |set up |cache|restore|save|install|configure|download|"
        r"load .*image|export .*image|start |make .*executable|compute .*cache|"
        r"expose .*runtime|wait for|prepare|initiali[sz]e|create .*database|"
        r"seed |copy .*artifact|fetch|pull |chmod|enable .*cache|activate|"
        r"add .*path|show \.net sdk info",
        lowered,
    ):
        return CATEGORY_SETUP
    if re.search(
        r"detect changed|evaluate gate|skip on|set output|determine .*changed|"
        r"gate input|changed areas|detect migration changes",
        lowered,
    ):
        return CATEGORY_ORCHESTRATION
    if re.search(
        r"test|playwright|pytest|nextest|contract|integration|unit suite|e2e|"
        r"smoke|visual spec|spec suite",
        lowered,
    ):
        return CATEGORY_TEST
    if re.search(
        r"lint|format|type.?check|validate|verify|check|review|scan|zizmor|"
        r"semgrep|build|generate|compile|vet|clippy|audit|benchmark|"
        r"shell syntax|analyzer rule|parse powershell|guard |enforce ",
        lowered,
    ):
        return CATEGORY_VALIDATION
    return CATEGORY_UNCLASSIFIED


def associate_runs_with_pull_requests(
    runs: list[dict[str, Any]], pulls: list[dict[str, Any]]
) -> None:
    pulls_by_number = {pull["number"]: pull for pull in pulls}
    pulls_by_branch = {
        pull.get("head", {}).get("ref"): pull
        for pull in pulls
        if pull.get("head", {}).get("ref")
    }
    for run in runs:
        direct_numbers = [
            item.get("number") for item in run.get("pull_requests", []) if item
        ]
        direct_number = next((number for number in direct_numbers if number), None)
        pull = pulls_by_number.get(direct_number) or pulls_by_branch.get(
            run.get("head_branch")
        )
        number = direct_number or (pull or {}).get("number")
        run["_pr_number"] = number
        run["_pr_title"] = (pull or {}).get("title")
        run["_group_key"] = (
            f"pr:{number}"
            if number
            else f"branch:{run.get('head_branch', '<unknown>')}"
        )


def select_detailed_runs(
    runs: list[dict[str, Any]],
    conclusions: set[str],
    max_runs_per_pr: int,
    detail_limit: int,
) -> list[dict[str, Any]]:
    groups: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for run in runs:
        if run.get("conclusion") in conclusions:
            groups[run["_group_key"]].append(run)

    ordered_groups = sorted(
        groups.values(),
        key=lambda group: max(item.get("run_number", 0) for item in group),
        reverse=True,
    )
    selected: list[dict[str, Any]] = []
    for group in ordered_groups:
        ordered = sorted(
            group, key=lambda item: item.get("run_number", 0), reverse=True
        )
        selected.extend(ordered[:max_runs_per_pr] if max_runs_per_pr else ordered)
    selected.sort(key=lambda item: item.get("run_number", 0), reverse=True)
    return selected[:detail_limit] if detail_limit else selected


def jobs_cache_path(cache_dir: Path, run: dict[str, Any]) -> Path:
    return cache_dir / (
        f"run-{run['id']}-attempt-{run.get('run_attempt', 1)}-jobs.json"
    )


def fetch_jobs(
    client: GitHubClient,
    repository: str,
    run: dict[str, Any],
    cache_dir: Path | None,
    refresh: bool,
) -> list[dict[str, Any]]:
    cache_path = jobs_cache_path(cache_dir, run) if cache_dir else None
    if cache_path and cache_path.exists() and not refresh:
        cached = json.loads(cache_path.read_text(encoding="utf-8"))
        if isinstance(cached, list):
            return cached

    jobs = paginate_key(
        client,
        f"/repos/{repository}/actions/runs/{run['id']}/jobs?filter=latest",
        "jobs",
        500,
    )
    if cache_path:
        cache_path.parent.mkdir(parents=True, exist_ok=True)
        cache_path.write_text(json.dumps(jobs, indent=2) + "\n", encoding="utf-8")
    return jobs


def summarize_profile(rows: list[dict[str, Any]]) -> dict[str, Any]:
    if not rows:
        return {
            "runs": 0,
            "median_jobs": 0.0,
            "mean_runner_minutes": 0.0,
            "median_runner_minutes": 0.0,
            "median_wall_minutes": 0.0,
            "mean_test_minutes": 0.0,
            "mean_validation_minutes": 0.0,
            "mean_overhead_minutes": 0.0,
            "test_percent": 0.0,
            "validation_percent": 0.0,
            "overhead_percent": 0.0,
        }

    runner = sum(row["runner_seconds"] for row in rows)
    test = sum(row["test_seconds"] for row in rows)
    validation = sum(row["validation_seconds"] for row in rows)
    overhead = sum(row["overhead_seconds"] for row in rows)
    count = len(rows)
    return {
        "runs": count,
        "median_jobs": round(quantile((row["jobs"] for row in rows), 0.5), 1),
        "mean_runner_minutes": round(runner / count / 60, 2),
        "median_runner_minutes": round(
            quantile((row["runner_seconds"] for row in rows), 0.5) / 60, 2
        ),
        "median_wall_minutes": round(
            quantile((row["wall_seconds"] for row in rows), 0.5) / 60, 2
        ),
        "mean_test_minutes": round(test / count / 60, 2),
        "mean_validation_minutes": round(validation / count / 60, 2),
        "mean_overhead_minutes": round(overhead / count / 60, 2),
        "test_percent": round(100 * test / runner, 2) if runner else 0.0,
        "validation_percent": round(100 * validation / runner, 2) if runner else 0.0,
        "overhead_percent": round(100 * overhead / runner, 2) if runner else 0.0,
    }


def summarize_e2e_jobs(
    job_records: list[dict[str, Any]], step_records: list[dict[str, Any]]
) -> dict[str, Any]:
    e2e_jobs = [
        job for job in job_records if re.search(r"E2E Tests / e2e-tests", job["name"])
    ]
    e2e_job_ids = {job["id"] for job in e2e_jobs}
    steps_by_job: dict[int, list[dict[str, Any]]] = defaultdict(list)
    for step in step_records:
        if step["job_id"] in e2e_job_ids:
            steps_by_job[step["job_id"]].append(step)

    suites: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for job in e2e_jobs:
        suites[job["name"].split(" / ", 1)[0]].append(job)

    def summarize(jobs: list[dict[str, Any]]) -> dict[str, Any]:
        runner = sum(job["duration_seconds"] for job in jobs)
        test = 0.0
        validation = 0.0
        overhead = sum(job["unattributed_seconds"] for job in jobs)
        for job in jobs:
            for step in steps_by_job[job["id"]]:
                if step["category"] == CATEGORY_TEST:
                    test += step["duration_seconds"]
                elif step["category"] == CATEGORY_VALIDATION:
                    validation += step["duration_seconds"]
                elif step["category"] in OVERHEAD_CATEGORIES:
                    overhead += step["duration_seconds"]
        return {
            "jobs": len(jobs),
            "runner_minutes": round(runner / 60, 2),
            "mean_job_minutes": round(runner / len(jobs) / 60, 2) if jobs else 0.0,
            "test_minutes": round(test / 60, 2),
            "validation_minutes": round(validation / 60, 2),
            "overhead_minutes": round(overhead / 60, 2),
            "mean_overhead_minutes": round(overhead / len(jobs) / 60, 2)
            if jobs
            else 0.0,
            "test_percent": round(100 * test / runner, 2) if runner else 0.0,
            "overhead_percent": round(100 * overhead / runner, 2) if runner else 0.0,
        }

    return {
        "all": summarize(e2e_jobs),
        "suites": {name: summarize(jobs) for name, jobs in sorted(suites.items())},
    }


def analyze(
    repository: str,
    workflow: str,
    history_runs: list[dict[str, Any]],
    selected_runs: list[dict[str, Any]],
    jobs_by_run: dict[int, list[dict[str, Any]]],
) -> dict[str, Any]:
    job_records: list[dict[str, Any]] = []
    step_records: list[dict[str, Any]] = []
    run_rows: list[dict[str, Any]] = []

    for run in selected_runs:
        current_jobs: list[dict[str, Any]] = []
        current_steps: list[dict[str, Any]] = []
        for job_index, job in enumerate(jobs_by_run[run["id"]]):
            job_duration = duration_seconds(
                job.get("started_at"), job.get("completed_at")
            )
            if job_duration <= 0:
                continue
            job_id = int(job.get("id") or (run["id"] * 1000 + job_index))
            step_sum = 0.0
            for step in job.get("steps", []):
                step_duration = duration_seconds(
                    step.get("started_at"), step.get("completed_at")
                )
                if step_duration <= 0:
                    continue
                step_sum += step_duration
                record = {
                    "run_id": run["id"],
                    "job_id": job_id,
                    "job_name": job.get("name", "<unnamed job>"),
                    "name": step.get("name", "<unnamed step>"),
                    "normalized_name": normalize_step_name(
                        step.get("name", "<unnamed step>")
                    ),
                    "category": classify_step(step.get("name", "")),
                    "duration_seconds": step_duration,
                }
                current_steps.append(record)
                step_records.append(record)
            job_record = {
                "id": job_id,
                "run_id": run["id"],
                "name": job.get("name", "<unnamed job>"),
                "duration_seconds": job_duration,
                "unattributed_seconds": max(0.0, job_duration - step_sum),
                "started_at": job.get("started_at"),
                "completed_at": job.get("completed_at"),
            }
            current_jobs.append(job_record)
            job_records.append(job_record)

        category_seconds: dict[str, float] = defaultdict(float)
        for step in current_steps:
            category_seconds[step["category"]] += step["duration_seconds"]
        runner_seconds = sum(job["duration_seconds"] for job in current_jobs)
        unattributed = sum(job["unattributed_seconds"] for job in current_jobs)
        overhead = unattributed + sum(
            category_seconds[category] for category in OVERHEAD_CATEGORIES
        )
        completed = [
            parse_timestamp(job["completed_at"])
            for job in current_jobs
            if job["completed_at"]
        ]
        run_started = parse_timestamp(run.get("run_started_at"))
        wall_seconds = 0.0
        if completed and run_started:
            wall_seconds = max(0.0, (max(completed) - run_started).total_seconds())
        run_rows.append(
            {
                "run_id": run["id"],
                "run_number": run.get("run_number"),
                "run_url": run.get("html_url"),
                "pr_number": run.get("_pr_number"),
                "pr_title": run.get("_pr_title"),
                "branch": run.get("head_branch"),
                "conclusion": run.get("conclusion"),
                "jobs": len(current_jobs),
                "runner_seconds": runner_seconds,
                "wall_seconds": wall_seconds,
                "test_seconds": category_seconds[CATEGORY_TEST],
                "validation_seconds": category_seconds[CATEGORY_VALIDATION],
                "overhead_seconds": overhead,
                "unclassified_seconds": category_seconds[CATEGORY_UNCLASSIFIED],
            }
        )

    total_runner = sum(job["duration_seconds"] for job in job_records)
    total_unattributed = sum(job["unattributed_seconds"] for job in job_records)
    category_seconds: dict[str, float] = defaultdict(float)
    for step in step_records:
        category_seconds[step["category"]] += step["duration_seconds"]
    overhead_seconds = total_unattributed + sum(
        category_seconds[category] for category in OVERHEAD_CATEGORIES
    )

    categories = {
        category: {
            "minutes": round(seconds / 60, 2),
            "percent_runner": round(100 * seconds / total_runner, 2)
            if total_runner
            else 0.0,
        }
        for category, seconds in sorted(category_seconds.items())
    }
    categories["unattributed"] = {
        "minutes": round(total_unattributed / 60, 2),
        "percent_runner": round(100 * total_unattributed / total_runner, 2)
        if total_runner
        else 0.0,
    }

    step_groups: dict[str, dict[str, Any]] = {}
    for step in step_records:
        key = step["normalized_name"]
        if key not in step_groups:
            step_groups[key] = {
                "name": key,
                "category": step["category"],
                "durations": [],
            }
        step_groups[key]["durations"].append(step["duration_seconds"])
    step_stats = []
    for item in step_groups.values():
        durations = item.pop("durations")
        total = sum(durations)
        step_stats.append(
            {
                **item,
                "count": len(durations),
                "total_minutes": round(total / 60, 2),
                "mean_seconds": round(total / len(durations), 2),
                "p50_seconds": round(quantile(durations, 0.5), 2),
                "p95_seconds": round(quantile(durations, 0.95), 2),
            }
        )
    step_stats.sort(key=lambda item: item["total_minutes"], reverse=True)

    profiles = {
        "light": summarize_profile([row for row in run_rows if row["jobs"] < 10]),
        "partial": summarize_profile(
            [row for row in run_rows if 10 <= row["jobs"] < 40]
        ),
        "full": summarize_profile([row for row in run_rows if row["jobs"] >= 40]),
    }
    e2e = summarize_e2e_jobs(job_records, step_records)

    outcomes: dict[str, dict[str, Any]] = {}
    history_by_conclusion: dict[str, list[float]] = defaultdict(list)
    for run in history_runs:
        history_by_conclusion[run.get("conclusion") or "unknown"].append(
            duration_seconds(run.get("run_started_at"), run.get("updated_at"))
        )
    for conclusion, durations in sorted(history_by_conclusion.items()):
        outcomes[conclusion] = {
            "runs": len(durations),
            "median_wall_minutes": round(quantile(durations, 0.5) / 60, 2),
            "p95_wall_minutes": round(quantile(durations, 0.95) / 60, 2),
        }

    protofleet = e2e["suites"].get("ProtoFleet E2E Tests", {})
    full = profiles["full"]
    metrics = {
        "full_mean_runner_minutes": full["mean_runner_minutes"],
        "full_mean_overhead_minutes": full["mean_overhead_minutes"],
        "full_median_wall_minutes": full["median_wall_minutes"],
        "overall_overhead_percent": round(100 * overhead_seconds / total_runner, 2)
        if total_runner
        else 0.0,
        "protofleet_e2e_mean_job_minutes": protofleet.get("mean_job_minutes", 0.0),
        "protofleet_e2e_mean_overhead_minutes": protofleet.get(
            "mean_overhead_minutes", 0.0
        ),
    }

    started = [
        run.get("run_started_at") for run in history_runs if run.get("run_started_at")
    ]
    return {
        "schema_version": SCHEMA_VERSION,
        "generated_at": dt.datetime.now(tz=dt.timezone.utc).isoformat(),
        "repository": repository,
        "workflow": workflow,
        "history": {
            "runs": len(history_runs),
            "pull_requests_or_branches": len(
                {run["_group_key"] for run in history_runs}
            ),
            "from": min(started) if started else None,
            "to": max(started) if started else None,
            "outcomes": outcomes,
        },
        "detail": {
            "runs": len(selected_runs),
            "pull_requests_or_branches": len(
                {run["_group_key"] for run in selected_runs}
            ),
            "jobs": len(job_records),
            "positive_duration_steps": len(step_records),
            "runner_minutes": round(total_runner / 60, 2),
            "categories": categories,
            "headline": {
                "test_minutes": round(category_seconds[CATEGORY_TEST] / 60, 2),
                "test_percent": round(
                    100 * category_seconds[CATEGORY_TEST] / total_runner, 2
                )
                if total_runner
                else 0.0,
                "validation_minutes": round(
                    category_seconds[CATEGORY_VALIDATION] / 60, 2
                ),
                "validation_percent": round(
                    100 * category_seconds[CATEGORY_VALIDATION] / total_runner, 2
                )
                if total_runner
                else 0.0,
                "overhead_minutes": round(overhead_seconds / 60, 2),
                "overhead_percent": metrics["overall_overhead_percent"],
                "unclassified_minutes": round(
                    category_seconds[CATEGORY_UNCLASSIFIED] / 60, 2
                ),
            },
            "profiles": profiles,
            "e2e": e2e,
            "steps": step_stats,
            "runs_detail": run_rows,
        },
        "metrics": metrics,
    }


def compare_with_baseline(
    current: dict[str, Any], baseline: dict[str, Any]
) -> dict[str, Any]:
    rows = []
    for key, label in COMPARISON_METRICS.items():
        before = baseline.get("metrics", {}).get(key)
        after = current.get("metrics", {}).get(key)
        if not isinstance(before, (int, float)) or not isinstance(after, (int, float)):
            continue
        delta = after - before
        delta_percent = 100 * delta / before if before else None
        rows.append(
            {
                "metric": key,
                "label": label,
                "baseline": round(before, 2),
                "current": round(after, 2),
                "delta": round(delta, 2),
                "delta_percent": round(delta_percent, 2)
                if delta_percent is not None
                else None,
            }
        )
    return {
        "same_repository": baseline.get("repository") == current.get("repository"),
        "same_workflow": baseline.get("workflow") == current.get("workflow"),
        "same_selection": baseline.get("selection") == current.get("selection"),
        "metrics": rows,
    }


def markdown_table(headers: list[str], rows: list[list[Any]]) -> list[str]:
    def cell(value: Any) -> str:
        return str(value).replace("|", r"\|").replace("\n", " ")

    lines = [
        "| " + " | ".join(cell(header) for header in headers) + " |",
        "| " + " | ".join("---" for _ in headers) + " |",
    ]
    lines.extend("| " + " | ".join(cell(value) for value in row) + " |" for row in rows)
    return lines


def render_markdown(
    analysis: dict[str, Any], top_steps: int, comparison: dict[str, Any] | None
) -> str:
    history = analysis["history"]
    detail = analysis["detail"]
    headline = detail["headline"]
    lines = [
        "# GitHub Actions timing analysis",
        "",
        f"Repository: `{analysis['repository']}`  ",
        f"Workflow: `{analysis['workflow']}`  ",
        (
            f"History: {history['runs']} completed runs across "
            f"{history['pull_requests_or_branches']} PRs/branches "
            f"({history['from']} to {history['to']})  "
        ),
        (
            f"Detailed sample: {detail['runs']} runs, {detail['jobs']} jobs, "
            f"{detail['positive_duration_steps']} positive-duration steps"
        ),
        "",
        "## Outcome history",
        "",
    ]
    outcome_rows = [
        [
            conclusion,
            values["runs"],
            f"{values['median_wall_minutes']:.1f}",
            f"{values['p95_wall_minutes']:.1f}",
        ]
        for conclusion, values in history["outcomes"].items()
    ]
    lines.extend(
        markdown_table(
            ["Conclusion", "Runs", "Median wall min", "p95 wall min"], outcome_rows
        )
    )
    lines.extend(["", "## Runner-time split", ""])
    lines.extend(
        markdown_table(
            ["Bucket", "Runner minutes", "Share"],
            [
                [
                    "Actual tests",
                    f"{headline['test_minutes']:.1f}",
                    f"{headline['test_percent']:.1f}%",
                ],
                [
                    "Build/static validation",
                    f"{headline['validation_minutes']:.1f}",
                    f"{headline['validation_percent']:.1f}%",
                ],
                [
                    "Overhead",
                    f"{headline['overhead_minutes']:.1f}",
                    f"{headline['overhead_percent']:.1f}%",
                ],
                [
                    "Unclassified",
                    f"{headline['unclassified_minutes']:.1f}",
                    "review",
                ],
            ],
        )
    )
    lines.extend(["", "## Run profiles", ""])
    profile_rows = []
    for name, profile in detail["profiles"].items():
        profile_rows.append(
            [
                name,
                profile["runs"],
                f"{profile['mean_runner_minutes']:.1f}",
                f"{profile['median_wall_minutes']:.1f}",
                f"{profile['mean_test_minutes']:.1f}",
                f"{profile['mean_overhead_minutes']:.1f}",
            ]
        )
    lines.extend(
        markdown_table(
            [
                "Profile",
                "Runs",
                "Mean runner min",
                "Median wall min",
                "Mean test min",
                "Mean overhead min",
            ],
            profile_rows,
        )
    )
    lines.extend(["", "## E2E shard jobs", ""])
    e2e_rows = []
    for name, suite in detail["e2e"]["suites"].items():
        e2e_rows.append(
            [
                name,
                suite["jobs"],
                f"{suite['runner_minutes']:.1f}",
                f"{suite['mean_job_minutes']:.2f}",
                f"{suite['test_percent']:.1f}%",
                f"{suite['overhead_percent']:.1f}%",
            ]
        )
    lines.extend(
        markdown_table(
            ["Suite", "Jobs", "Runner min", "Mean job min", "Test", "Overhead"],
            e2e_rows,
        )
    )
    lines.extend(["", f"## Top {top_steps} steps by runner time", ""])
    step_rows = [
        [
            step["name"],
            step["category"],
            step["count"],
            f"{step['total_minutes']:.1f}",
            f"{step['mean_seconds']:.1f}",
            f"{step['p95_seconds']:.1f}",
        ]
        for step in detail["steps"][:top_steps]
    ]
    lines.extend(
        markdown_table(
            ["Step", "Category", "Count", "Total min", "Mean sec", "p95 sec"],
            step_rows,
        )
    )

    unclassified = [
        step for step in detail["steps"] if step["category"] == CATEGORY_UNCLASSIFIED
    ]
    if unclassified:
        lines.extend(["", "## Unclassified steps", ""])
        lines.extend(
            f"- `{step['name']}`: {step['total_minutes']:.2f} runner-minutes"
            for step in unclassified
        )

    if comparison:
        lines.extend(["", "## Baseline comparison", ""])
        if (
            not comparison["same_repository"]
            or not comparison["same_workflow"]
            or not comparison["same_selection"]
        ):
            lines.append(
                "> Warning: the baseline repository, workflow, or selection does not "
                "match this run."
            )
            lines.append("")
        comparison_rows = []
        for item in comparison["metrics"]:
            delta_percent = item["delta_percent"]
            comparison_rows.append(
                [
                    item["label"],
                    f"{item['baseline']:.2f}",
                    f"{item['current']:.2f}",
                    f"{item['delta']:+.2f}",
                    f"{delta_percent:+.1f}%" if delta_percent is not None else "n/a",
                ]
            )
        lines.extend(
            markdown_table(
                ["Metric", "Baseline", "Current", "Delta", "Delta %"],
                comparison_rows,
            )
        )
    lines.append("")
    return "\n".join(lines)


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--repo", help="GitHub repository as OWNER/REPO")
    parser.add_argument("--workflow", default="pr-gate.yml")
    parser.add_argument("--history-runs", type=int, default=100)
    parser.add_argument(
        "--conclusions",
        default="success",
        help="Comma-separated conclusions to fetch detailed jobs for",
    )
    parser.add_argument(
        "--max-runs-per-pr",
        type=int,
        default=3,
        help="Newest detailed runs per PR/branch; 0 means unlimited",
    )
    parser.add_argument(
        "--detail-limit",
        type=int,
        default=0,
        help="Total detailed run cap; 0 unlimited",
    )
    parser.add_argument("--workers", type=int, default=6)
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--cache-dir", type=Path)
    parser.add_argument("--refresh", action="store_true")
    parser.add_argument("--top-steps", type=int, default=20)
    parser.add_argument("--json-out", type=Path)
    parser.add_argument("--markdown-out", type=Path)
    parser.add_argument("--baseline", type=Path)
    parser.add_argument(
        "--fail-on-regression-pct",
        type=float,
        help="Exit 3 if any lower-is-better comparison metric regresses by this percent",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    if args.history_runs <= 0:
        raise AnalysisError("--history-runs must be positive")
    if args.max_runs_per_pr < 0 or args.detail_limit < 0:
        raise AnalysisError("run limits cannot be negative")
    if args.workers <= 0:
        raise AnalysisError("--workers must be positive")
    if args.timeout <= 0:
        raise AnalysisError("--timeout must be positive")
    if args.top_steps <= 0:
        raise AnalysisError("--top-steps must be positive")
    if args.fail_on_regression_pct is not None and args.fail_on_regression_pct < 0:
        raise AnalysisError("--fail-on-regression-pct cannot be negative")

    repository = args.repo or infer_repository()
    workflow = urllib.parse.quote(args.workflow, safe="")
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    client = GitHubClient(token, timeout=args.timeout)

    runs = paginate_key(
        client,
        (
            f"/repos/{repository}/actions/workflows/{workflow}/runs"
            "?event=pull_request&status=completed"
        ),
        "workflow_runs",
        args.history_runs,
    )
    pull_limit = max(100, min(500, args.history_runs * 2))
    pulls = paginate_list(
        client,
        f"/repos/{repository}/pulls?state=all&sort=updated&direction=desc",
        pull_limit,
    )
    associate_runs_with_pull_requests(runs, pulls)
    conclusions = {
        value.strip() for value in args.conclusions.split(",") if value.strip()
    }
    selected = select_detailed_runs(
        runs, conclusions, args.max_runs_per_pr, args.detail_limit
    )
    if not selected:
        raise AnalysisError("No workflow runs matched the detailed-run selection")
    if not token and len(selected) > 50:
        print(
            "warning: this selection is likely to exceed GitHub's anonymous API limit; "
            "set GITHUB_TOKEN or GH_TOKEN",
            file=sys.stderr,
        )
    print(
        f"Fetching jobs for {len(selected)} of {len(runs)} runs with "
        f"{args.workers} workers...",
        file=sys.stderr,
    )

    jobs_by_run: dict[int, list[dict[str, Any]]] = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as executor:
        futures = {
            executor.submit(
                fetch_jobs,
                client,
                repository,
                run,
                args.cache_dir,
                args.refresh,
            ): run
            for run in selected
        }
        completed_count = 0
        for future in concurrent.futures.as_completed(futures):
            run = futures[future]
            jobs_by_run[run["id"]] = future.result()
            completed_count += 1
            if completed_count % 10 == 0 or completed_count == len(selected):
                print(
                    f"Fetched {completed_count}/{len(selected)} runs",
                    file=sys.stderr,
                )

    analysis = analyze(repository, args.workflow, runs, selected, jobs_by_run)
    analysis["selection"] = {
        "history_runs": args.history_runs,
        "event": "pull_request",
        "status": "completed",
        "conclusions": sorted(conclusions),
        "max_runs_per_pr": args.max_runs_per_pr,
        "detail_limit": args.detail_limit,
    }
    comparison = None
    if args.baseline:
        baseline = json.loads(args.baseline.read_text(encoding="utf-8"))
        comparison = compare_with_baseline(analysis, baseline)
        analysis["comparison"] = comparison

    if args.json_out:
        args.json_out.parent.mkdir(parents=True, exist_ok=True)
        args.json_out.write_text(
            json.dumps(analysis, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )
    markdown = render_markdown(analysis, args.top_steps, comparison)
    if args.markdown_out:
        args.markdown_out.parent.mkdir(parents=True, exist_ok=True)
        args.markdown_out.write_text(markdown, encoding="utf-8")
    else:
        print(markdown, end="")

    if args.fail_on_regression_pct is not None and comparison:
        regressions = [
            item
            for item in comparison["metrics"]
            if item["delta_percent"] is not None
            and item["delta_percent"] > args.fail_on_regression_pct
        ]
        if regressions:
            labels = ", ".join(item["label"] for item in regressions)
            print(
                f"regression threshold exceeded ({args.fail_on_regression_pct}%): "
                f"{labels}",
                file=sys.stderr,
            )
            return 3
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except AnalysisError as error:
        print(f"error: {error}", file=sys.stderr)
        raise SystemExit(2) from error
