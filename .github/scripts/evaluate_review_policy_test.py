#!/usr/bin/env python3

import functools
import io
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import unittest
import zipfile
from pathlib import Path

import evaluate_review_policy as policy

GITHUB_DIR = Path(__file__).resolve().parents[1]
WORKFLOWS_DIR = GITHUB_DIR / "workflows"
CONFIG_PATH = GITHUB_DIR / "review-policy.json"
BENCHMARK_CORPUS_PATH = GITHUB_DIR / "codex-benchmark-corpus.json"
FULL_SHA_PATTERN = re.compile(r"\A[0-9a-f]{40}\Z")

# Every reason the bounded review can report for producing no usable output. Both
# workflows classify independently, so each one's tests assert the full set and a
# dropped or renamed reason in either workflow fails.
INCOMPLETE_REASONS = (
    "codex-job-timeout",
    "codex-step-timeout",
    "empty-model-output",
    "invalid-model-output",
)


@functools.cache
def read_workflow(name):
    return (WORKFLOWS_DIR / name).read_text(encoding="utf-8")


# Workflow files do not change during a run, and each parse costs a Ruby subprocess.
@functools.cache
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


def workflow_triggers(workflow):
    # A YAML 1.1 loader resolves the bare `on:` key to the boolean true.
    return workflow.get("on", workflow.get("true"))


def load_benchmark_corpus():
    return json.loads(BENCHMARK_CORPUS_PATH.read_text(encoding="utf-8"))


def find_step(workflow, job_id, step_id):
    return next(
        step for step in workflow["jobs"][job_id]["steps"] if step.get("id") == step_id
    )


def extract_python_heredoc(run_block):
    """Return the `python3 - <<'PY' ... PY` body embedded in a workflow run block.

    Workflow-embedded Python is only trustworthy if it can be executed, so the
    tests below run these bodies directly instead of asserting on substrings.
    """
    marker = "python3 - <<'PY'\n"
    found = run_block.count(marker)
    if found != 1:
        raise AssertionError(f"expected exactly one Python heredoc, found {found}")
    body = run_block.split(marker, 1)[1]
    if "\nPY\n" not in body:
        raise AssertionError("Python heredoc is not terminated by a PY delimiter")
    return body.split("\nPY\n", 1)[0] + "\n"


def valid_review_markdown(risk="LOW"):
    findings = "No material findings."
    if risk != "NONE":
        findings = "\n".join(
            [
                f"#### [{risk}] Material finding",
                "- **Category**: Reliability",
                "- **Location**: [`path/file.go:1`](https://example.invalid/path/file.go#L1)",
                "- **Description**: Concrete changed behavior.",
                "- **Impact**: Material impact.",
                "- **Recommendation**: Apply the mitigation.",
            ]
        )
    return (
        "## Review Summary\n\n"
        f"**Overall Risk**: {risk}\n\n"
        "### Findings\n\n"
        f"{findings}\n\n"
        "### Notes\n\n"
        "No additional notes.\n"
    )


def run_python_heredoc(body, env, cwd):
    script = Path(cwd) / "heredoc.py"
    script.write_text(body, encoding="utf-8")
    child_env = dict(os.environ)
    child_env.update(env)
    return subprocess.run(
        [sys.executable, str(script)],
        cwd=cwd,
        env=child_env,
        check=False,
        capture_output=True,
        text=True,
    )


def run_github_script(body, scenario, cwd):
    rendered = body.replace("${{ github.repository }}", "block/proto-fleet").replace(
        "${{ github.run_id }}", str(scenario.get("run_id", 100))
    )
    harness = (
        """
const scenario = JSON.parse(process.env.SCENARIO_JSON);
const calls = [];
const notices = [];
const failures = [];
const outputs = {};
const maybeFail = (name) => {
  if (scenario.fail_action === name) throw new Error(`forced ${name} failure`);
};
const context = {
  runId: scenario.run_id || 100,
  sha: scenario.head_sha || 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
  repo: {owner: 'block', repo: 'proto-fleet'},
};
const core = {
  info: message => calls.push({type: 'info', message}),
  notice: message => notices.push(message),
  warning: message => calls.push({type: 'warning', message}),
  setFailed: message => failures.push(message),
  setOutput: (name, value) => { outputs[name] = String(value); },
};
const github = {
  paginate: async (method, args) => {
    const response = await method(args);
    return response.data || response;
  },
  rest: {
    pulls: {
      get: async () => {
        maybeFail('pulls.get');
        return {data: scenario.pull_request};
      },
    },
    actions: {
      getWorkflowRun: async () => {
        maybeFail('actions.getWorkflowRun');
        return {data: {workflow_id: 42}};
      },
      listWorkflowRuns: async () => {
        maybeFail('actions.listWorkflowRuns');
        return {data: scenario.workflow_runs || []};
      },
      listJobsForWorkflowRun: async () => {
        maybeFail('actions.listJobsForWorkflowRun');
        return {data: scenario.jobs || []};
      },
      listWorkflowRunArtifacts: async () => {
        maybeFail('actions.listWorkflowRunArtifacts');
        return {data: scenario.artifacts || []};
      },
    },
    issues: {
      listComments: async () => {
        maybeFail('issues.listComments');
        return {data: scenario.comments || []};
      },
      updateComment: async args => {
        maybeFail('issues.updateComment');
        calls.push({type: 'update', args});
        return {data: {}};
      },
      createComment: async args => {
        maybeFail('issues.createComment');
        calls.push({type: 'create', args});
        return {data: {}};
      },
    },
  },
};
(async () => {
"""
        + rendered
        + """
})().then(() => {
  console.log(JSON.stringify({calls, notices, failures, outputs}));
  if (failures.length) process.exitCode = 1;
}).catch(error => {
  console.error(error.stack || String(error));
  process.exitCode = 2;
});
"""
    )
    script = Path(cwd) / "github-script.js"
    script.write_text(harness, encoding="utf-8")
    (Path(cwd) / "codex-security-review.md").write_text(
        "## Review Summary\n\n**Overall Risk**: HIGH\n", encoding="utf-8"
    )
    child_env = {
        **os.environ,
        "SCENARIO_JSON": json.dumps(scenario),
        "REVIEW_MARKDOWN_FILE": "codex-security-review.md",
        "REVIEW_HEAD_SHA": scenario.get("head_sha", "b" * 40),
        "REVIEW_PR_NUMBER": "965",
        "REVIEW_COMMIT_RANGE": f"{'a' * 40}...{'b' * 40}",
        "CODEX_MODEL": "gpt-5.6-sol",
        "REVIEW_AGENT_RESULT": scenario.get("agent_result", "success"),
        "REVIEW_AGENT_JOB_NAME": "Run bounded Codex reviewer",
        "CODEX_TIMEOUT_MINUTES": scenario.get("timeout_minutes", "9"),
        "CODEX_CANCELLATION_CLEANUP_SECONDS": scenario.get("cleanup_seconds", "300"),
        "AGENT_JOB_NAME": scenario.get("agent_job_name", "Run bounded Codex reviewer"),
        "SHARD_JOB_ID": str(scenario.get("shard_job_id", 456)),
        "RESULT_ARTIFACT_NAME": "benchmark-result-test",
    }
    completed = subprocess.run(
        ["node", str(script)],
        cwd=cwd,
        env=child_env,
        check=False,
        capture_output=True,
        text=True,
    )
    output = json.loads(completed.stdout.splitlines()[-1]) if completed.stdout else {}
    return completed, output


class ReviewPolicyTest(unittest.TestCase):
    def test_pr_gate_runs_review_policy_tests(self):
        workflow = load_workflow("pr-gate.yml")
        jobs = workflow["jobs"]

        self.assertIn("review-policy-tests", jobs)
        self.assertIn("review-policy-tests", jobs["gate"]["needs"])
        self.assertEqual(
            jobs["review-policy-tests"]["steps"][1]["run"].splitlines(),
            [
                "python3 .github/scripts/evaluate_review_policy_test.py",
                "python3 .github/scripts/codex_sharding_test.py",
            ],
        )

    def test_codex_security_review_is_bounded_and_fail_closed(self):
        workflow = load_workflow("codex-security-review.yml")
        agent = workflow["jobs"]["review-agent"]
        finalizer = workflow["jobs"]["security-review"]
        codex = find_step(workflow, "review-agent", "run_codex")
        writer = find_step(workflow, "security-review", "write_review_result")

        self.assertEqual(agent["timeout-minutes"], 9)
        self.assertEqual(finalizer["timeout-minutes"], 5)
        self.assertEqual(finalizer["needs"], "review-agent")
        self.assertIn("always()", finalizer["if"])
        post_review = workflow["jobs"]["post-review"]
        self.assertEqual(post_review["needs"], "security-review")
        self.assertIn("always()", post_review["if"])
        self.assertIn("needs.security-review.result == 'success'", post_review["if"])
        self.assertFalse(post_review["concurrency"]["cancel-in-progress"])
        self.assertEqual(
            post_review["concurrency"]["group"],
            "codex-security-review-post-${{ github.event.pull_request.number }}",
        )
        self.assertEqual(codex["timeout-minutes"], 9)
        self.assertTrue(codex["continue-on-error"])
        self.assertIn('"additionalProperties": false', codex["with"]["output-schema"])
        self.assertIn("needs.review-agent.result == 'cancelled'", writer["if"])
        self.assertIn('risk = "HIGH"', writer["run"])
        self.assertIn(
            '"automation_completed": incomplete_reason is None', writer["run"]
        )
        self.assertIn("Human review is required", writer["run"])

        poster = find_step(workflow, "post-review", "post_review")
        post_script = poster["with"]["script"]
        self.assertNotIn("const supersedingRun", post_script)
        self.assertIn("existingRunId > Number(context.runId)", post_script)

        uploads = [
            step
            for step in finalizer["steps"]
            if str(step.get("uses", "")).startswith("actions/upload-artifact@")
        ]
        self.assertEqual(len(uploads), 2)
        self.assertTrue(
            all(
                "steps.write_review_result.outcome == 'success'" in step["if"]
                for step in uploads
            )
        )

    def test_post_review_script_executes_stale_and_failure_guards(self):
        workflow = load_workflow("codex-security-review.yml")
        script = find_step(workflow, "post-review", "post_review")["with"]["script"]
        head_sha = "b" * 40
        base = {
            "run_id": 100,
            "head_sha": head_sha,
            "pull_request": {
                "state": "open",
                "head": {"sha": head_sha},
                "user": {"login": "author"},
            },
            "workflow_runs": [],
            "comments": [],
        }

        scenarios = [
            ("current", base, 0, "create", None),
            (
                "superseded-same-pr",
                {
                    **base,
                    "workflow_runs": [
                        {
                            "id": 101,
                            "head_sha": head_sha,
                            "pull_requests": [{"number": 965}],
                        }
                    ],
                },
                0,
                "create",
                None,
            ),
            (
                "same-head-other-pr",
                {
                    **base,
                    "workflow_runs": [
                        {
                            "id": 101,
                            "head_sha": head_sha,
                            "pull_requests": [{"number": 966}],
                        }
                    ],
                },
                0,
                "create",
                None,
            ),
            (
                "newer-existing-comment",
                {
                    **base,
                    "comments": [
                        {
                            "id": 1,
                            "user": {"login": "github-actions[bot]", "type": "Bot"},
                            "body": (
                                "<!-- codex-security-review -->\n"
                                "<!-- codex-security-review-run:101 -->\n"
                            ),
                        }
                    ],
                },
                0,
                None,
                "existing comment came from newer run",
            ),
            (
                "legacy-comment-with-untrusted-marker",
                {
                    **base,
                    "comments": [
                        {
                            "id": 1,
                            "user": {"login": "github-actions[bot]", "type": "Bot"},
                            "body": (
                                "<!-- codex-security-review -->\n"
                                "## Legacy review\n"
                                "<!-- codex-security-review-run:999999 -->\n"
                            ),
                        }
                    ],
                },
                0,
                "update",
                None,
            ),
            (
                "api-failure",
                {**base, "fail_action": "issues.createComment"},
                1,
                None,
                None,
            ),
        ]
        for name, scenario, expected_code, expected_call, expected_notice in scenarios:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                completed, output = run_github_script(script, scenario, tmp)
                self.assertEqual(completed.returncode, expected_code, completed.stderr)
                call_types = [call["type"] for call in output.get("calls", [])]
                if expected_call:
                    self.assertIn(expected_call, call_types)
                else:
                    self.assertNotIn("create", call_types)
                    self.assertNotIn("update", call_types)
                if expected_notice:
                    self.assertTrue(
                        any(
                            expected_notice in notice
                            for notice in output.get("notices", [])
                        )
                    )
                if expected_code:
                    self.assertTrue(output.get("failures"))

    def test_review_agent_cancellation_classifier_requires_budget_evidence(self):
        workflow = load_workflow("codex-security-review.yml")
        script = find_step(workflow, "security-review", "classify_agent")["with"][
            "script"
        ]
        head_sha = "b" * 40
        successful_steps = [
            {"name": name, "conclusion": "success"}
            for name in (
                "Checkout PR head commit",
                "Fetch exact PR base and head commits",
                "Validate exact review scope and trusted configuration",
                "Write review diff",
                "Require review API key",
            )
        ]
        timeout_job = {
            "name": "Run bounded Codex reviewer",
            "conclusion": "cancelled",
            "started_at": "2026-08-26T00:00:00Z",
            "completed_at": "2026-08-26T00:14:00Z",
            "steps": [
                *successful_steps,
                {
                    "name": "Run Codex Security Review",
                    "status": "in_progress",
                    "conclusion": None,
                    "started_at": "2026-08-26T00:00:30Z",
                },
            ],
        }
        base = {
            "run_id": 100,
            "head_sha": head_sha,
            "pull_request": {
                "state": "open",
                "head": {"sha": head_sha},
                "user": {"login": "author"},
            },
            "agent_result": "cancelled",
            "workflow_runs": [],
            "jobs": [timeout_job],
        }
        scenarios = [
            ("verified-timeout", base, "budget-timeout"),
            (
                "post-codex-budget-timeout",
                {
                    **base,
                    "jobs": [
                        {
                            **timeout_job,
                            "steps": [
                                *successful_steps,
                                {
                                    "name": "Run Codex Security Review",
                                    "status": "completed",
                                    "conclusion": "success",
                                    "started_at": "2026-08-26T00:00:30Z",
                                },
                                {
                                    "name": "Write trusted raw review handoff",
                                    "status": "in_progress",
                                    "conclusion": None,
                                    "started_at": "2026-08-26T00:08:59Z",
                                },
                            ],
                        }
                    ],
                },
                "budget-timeout",
            ),
            (
                "setup-cancellation",
                {
                    **base,
                    "jobs": [
                        {
                            **timeout_job,
                            "steps": [
                                {
                                    "name": "Checkout PR head commit",
                                    "conclusion": "cancelled",
                                }
                            ],
                        }
                    ],
                },
                "unexpected-cancellation",
            ),
            (
                "early-manual-cancellation",
                {
                    **base,
                    "jobs": [
                        {
                            **timeout_job,
                            "completed_at": "2026-08-26T00:05:00Z",
                        }
                    ],
                },
                "unexpected-cancellation",
            ),
            (
                "manual-cancellation-plus-cleanup",
                {
                    **base,
                    "jobs": [
                        {
                            **timeout_job,
                            "completed_at": "2026-08-26T00:12:00Z",
                        }
                    ],
                },
                "unexpected-cancellation",
            ),
            (
                "superseded",
                {
                    **base,
                    "workflow_runs": [
                        {
                            "id": 101,
                            "head_sha": head_sha,
                            "pull_requests": [{"number": 965}],
                        }
                    ],
                },
                "superseded",
            ),
            ("completed", {**base, "agent_result": "success"}, "completed"),
            (
                "automation-failure",
                {**base, "agent_result": "failure"},
                "automation-failure",
            ),
        ]
        for name, scenario, expected_classification in scenarios:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                completed, output = run_github_script(script, scenario, tmp)
                self.assertEqual(completed.returncode, 0, completed.stderr)
                self.assertEqual(
                    output["outputs"]["classification"], expected_classification
                )

    def test_codex_benchmark_uses_fixed_bounded_non_production_matrix(self):
        workflow_text = read_workflow("codex-security-review-benchmark.yml")
        workflow = load_workflow("codex-security-review-benchmark.yml")
        production = load_workflow("codex-security-review.yml")
        job = workflow["jobs"]["benchmark-review"]
        corpus = load_benchmark_corpus()
        cases = corpus["cases"]
        variants = corpus["variants"]
        codex = find_step(workflow, "benchmark-review", "run_codex")
        benchmark_writer = find_step(workflow, "benchmark-review", "write_artifacts")

        self.assertEqual(
            workflow_triggers(workflow),
            {
                "repository_dispatch": {
                    "types": [
                        "codex-security-review-benchmark",
                        "codex-security-review-sharded-benchmark",
                        "codex-security-review-terra-benchmark",
                    ]
                }
            },
        )
        selector = find_step(workflow, "select-cases", "select")
        self.assertEqual(
            selector["env"]["BENCHMARK_PROFILE"],
            "${{ github.event.action == 'codex-security-review-terra-benchmark' && 'terra-high-default-low' || 'sol' }}",
        )
        self.assertIn(
            "github.event.action == 'codex-security-review-terra-benchmark' && 'high' || 'xhigh'",
            selector["env"]["REASONING_EFFORT"],
        )
        self.assertIn(
            "github.event.action == 'codex-security-review-terra-benchmark') && 'unified-40' || 'all'",
            selector["env"]["CONTEXT_VARIANT"],
        )
        self.assertEqual(
            workflow["jobs"]["sharded-benchmark-case"]["if"],
            "${{ github.event.action == 'codex-security-review-sharded-benchmark' }}",
        )
        unsharded_events = (
            "github.event.action == 'codex-security-review-benchmark' || "
            "github.event.action == 'codex-security-review-terra-benchmark'"
        )
        self.assertEqual(job["if"], f"${{{{ {unsharded_events} }}}}")
        self.assertIn(unsharded_events, workflow["jobs"]["benchmark-finalize"]["if"])
        self.assertNotIn("workflow_dispatch:", workflow_text)
        self.assertNotIn("${{ inputs", workflow_text)
        self.assertEqual(
            workflow["permissions"], {"actions": "read", "contents": "read"}
        )
        self.assertFalse(job["strategy"]["fail-fast"])
        self.assertEqual(job["strategy"]["max-parallel"], 2)
        self.assertEqual(codex["timeout-minutes"], 12)
        self.assertTrue(codex["continue-on-error"])
        self.assertIn("!cancelled()", benchmark_writer["if"])
        self.assertEqual(
            {variant["id"] for variant in variants},
            {"unified-40", "unified-10", "compact"},
        )
        self.assertEqual(
            {case["pr"] for case in cases if case["corpus"] == "adjudicated"},
            {944, 948, 953, 954, 956, 961},
        )
        for case in cases:
            self.assertRegex(case["base"], FULL_SHA_PATTERN)
            self.assertRegex(case["head"], FULL_SHA_PATTERN)
        production_codex = find_step(production, "review-agent", "run_codex")
        production_prompt = production_codex["with"]["prompt"]
        benchmark_prompt = codex["with"]["prompt"]
        self.assertTrue(benchmark_prompt.startswith(production_prompt.rstrip()))
        self.assertNotIn(
            "Return no more than five material findings", production_prompt
        )
        self.assertIn("return no more than five material findings", benchmark_prompt)
        uploads = [
            step
            for step in job["steps"]
            if str(step.get("uses", "")).startswith("actions/upload-artifact@")
        ]
        self.assertEqual(len(uploads), 1)
        result_artifact_name = uploads[0]["with"]["name"]
        timeout_artifact_name = workflow["jobs"]["benchmark-finalize"]["env"][
            "TIMEOUT_ARTIFACT_NAME"
        ]
        self.assertTrue(result_artifact_name.startswith("benchmark-result-"))
        self.assertTrue(timeout_artifact_name.startswith("benchmark-timeout-"))
        self.assertNotEqual(result_artifact_name, timeout_artifact_name)
        self.assertIn("github.run_attempt", result_artifact_name)
        self.assertIn("github.run_attempt", timeout_artifact_name)
        self.assertNotIn("codex-security-review-result", result_artifact_name)
        self.assertNotIn("issues: write", workflow_text)
        self.assertNotIn("pull-requests: write", workflow_text)

    def test_codex_benchmark_matrix_comes_from_needs_not_the_matrix_context(self):
        workflow = load_workflow("codex-security-review-benchmark.yml")
        job = workflow["jobs"]["benchmark-review"]

        # `matrix` is not one of the contexts GitHub provides to a job-level `if`, so a
        # matrix-dependent filter there silently drops every job. The corpus is filtered
        # in a preceding job and consumed through `needs`, which `strategy` does support.
        self.assertNotIn("matrix", str(job.get("if", "")))
        self.assertNotIn("matrix", str(workflow["jobs"]["select-cases"].get("if", "")))
        self.assertEqual(job["needs"], "select-cases")
        self.assertEqual(
            job["strategy"]["matrix"],
            "${{ fromJSON(needs.select-cases.outputs.matrix) }}",
        )
        self.assertEqual(
            workflow["jobs"]["select-cases"]["outputs"],
            {
                "matrix": "${{ steps.select.outputs.matrix }}",
                "model": "${{ steps.select.outputs.model }}",
                "reasoning_effort": "${{ steps.select.outputs.reasoning_effort }}",
                "service_tier": "${{ steps.select.outputs.service_tier }}",
                "verbosity": "${{ steps.select.outputs.verbosity }}",
                "codex_args": "${{ steps.select.outputs.codex_args }}",
                "prompt_profile": "${{ steps.select.outputs.prompt_profile }}",
                "repeat": "${{ steps.select.outputs.repeat }}",
            },
        )

    def test_codex_benchmark_selection_filters_the_requested_corpus_and_variant(self):
        workflow = load_workflow("codex-security-review-benchmark.yml")
        body = extract_python_heredoc(
            find_step(workflow, "select-cases", "select")["run"]
        )
        corpus = load_benchmark_corpus()
        adjudicated = [
            case["id"] for case in corpus["cases"] if case["corpus"] == "adjudicated"
        ]
        large_pr = [
            case["id"] for case in corpus["cases"] if case["corpus"] == "large-pr"
        ]

        def select(
            corpus_name,
            variant_name,
            review_mode="unsharded",
            benchmark_profile="sol",
            reasoning_effort="xhigh",
            prompt_profile="baseline",
        ):
            with tempfile.TemporaryDirectory() as tmp:
                target = Path(tmp) / ".github" / BENCHMARK_CORPUS_PATH.name
                target.parent.mkdir()
                target.write_bytes(BENCHMARK_CORPUS_PATH.read_bytes())
                output = Path(tmp) / "step-output.txt"
                output.write_text("", encoding="utf-8")
                result = run_python_heredoc(
                    body,
                    {
                        "REVIEW_MODE": review_mode,
                        "BENCHMARK_PROFILE": benchmark_profile,
                        "CORPUS": corpus_name,
                        "CONTEXT_VARIANT": variant_name,
                        "REASONING_EFFORT": reasoning_effort,
                        "PROMPT_PROFILE": prompt_profile,
                        "REPEAT": "initial",
                        "GITHUB_OUTPUT": str(output),
                    },
                    tmp,
                )
                return result, output.read_text(encoding="utf-8")

        result, emitted = select("adjudicated", "all")
        self.assertEqual(result.returncode, 0, result.stderr)
        outputs = dict(line.split("=", 1) for line in emitted.splitlines())
        include = json.loads(outputs["matrix"])["include"]
        self.assertEqual(outputs["model"], "gpt-5.6-sol")
        self.assertEqual(outputs["reasoning_effort"], "xhigh")
        self.assertEqual(outputs["service_tier"], "unspecified")
        self.assertEqual(outputs["verbosity"], "unspecified")
        self.assertEqual(
            json.loads(outputs["codex_args"]),
            ["-c", "model_reasoning_effort=xhigh"],
        )
        self.assertEqual(outputs["prompt_profile"], "baseline")
        self.assertEqual(outputs["repeat"], "initial")
        self.assertEqual(len(include), len(adjudicated) * len(corpus["variants"]))
        self.assertEqual({entry["case"]["id"] for entry in include}, set(adjudicated))
        self.assertEqual(
            {entry["variant"]["id"] for entry in include},
            {variant["id"] for variant in corpus["variants"]},
        )

        result, emitted = select("large-pr", "compact")
        self.assertEqual(result.returncode, 0, result.stderr)
        outputs = dict(line.split("=", 1) for line in emitted.splitlines())
        include = json.loads(outputs["matrix"])["include"]
        self.assertEqual(
            [(entry["case"]["id"], entry["variant"]["id"]) for entry in include],
            [(case_id, "compact") for case_id in large_pr],
        )

        result, emitted = select("adjudicated", "does-not-exist")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does-not-exist", result.stderr)
        self.assertEqual(emitted, "")

        result, emitted = select("adjudicated", "unified-40", "sharded")
        self.assertEqual(result.returncode, 0, result.stderr)
        outputs = dict(line.split("=", 1) for line in emitted.splitlines())
        include = json.loads(outputs["matrix"])["include"]
        self.assertEqual(len(include), len(adjudicated))
        self.assertEqual({entry["variant"]["id"] for entry in include}, {"unified-40"})

        result, emitted = select("adjudicated", "compact", "sharded")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("compact", result.stderr)
        self.assertEqual(emitted, "")

        result, emitted = select(
            "adjudicated",
            "unified-40",
            benchmark_profile="terra-high-default-low",
            reasoning_effort="high",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        outputs = dict(line.split("=", 1) for line in emitted.splitlines())
        include = json.loads(outputs["matrix"])["include"]
        self.assertEqual(len(include), len(adjudicated))
        self.assertEqual({entry["variant"]["id"] for entry in include}, {"unified-40"})
        self.assertEqual(outputs["model"], "gpt-5.6-terra")
        self.assertEqual(outputs["reasoning_effort"], "high")
        self.assertEqual(outputs["service_tier"], "default")
        self.assertEqual(outputs["verbosity"], "low")
        self.assertEqual(
            json.loads(outputs["codex_args"]),
            [
                "-c",
                "model_reasoning_effort=high",
                "-c",
                "service_tier=default",
                "-c",
                "model_verbosity=low",
            ],
        )

        for variant, effort, prompt, rejected in (
            ("unified-40", "xhigh", "baseline", "xhigh"),
            ("compact", "high", "baseline", "compact"),
            ("unified-40", "high", "bounded", "bounded"),
        ):
            with self.subTest(rejected=rejected):
                result, emitted = select(
                    "adjudicated",
                    variant,
                    benchmark_profile="terra-high-default-low",
                    reasoning_effort=effort,
                    prompt_profile=prompt,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(rejected, result.stderr)
                self.assertEqual(emitted, "")

    def test_codex_review_workflows_share_bounded_review_configuration(self):
        production = load_workflow("codex-security-review.yml")
        benchmark = load_workflow("codex-security-review-benchmark.yml")
        production_job = production["jobs"]["review-agent"]
        production_finalizer = production["jobs"]["security-review"]
        production_poster = production["jobs"]["post-review"]
        benchmark_job = benchmark["jobs"]["benchmark-review"]
        benchmark_finalizer = benchmark["jobs"]["benchmark-finalize"]
        production_codex = find_step(production, "review-agent", "run_codex")
        benchmark_codex = find_step(benchmark, "benchmark-review", "run_codex")

        # The benchmark only predicts production behavior while the sandbox, safety, and
        # output contract are held constant; only the deliberately varied knobs may drift.
        for key in ("output-schema", "safety-strategy", "sandbox"):
            self.assertEqual(
                production_codex["with"][key].strip()
                if isinstance(production_codex["with"][key], str)
                else production_codex["with"][key],
                benchmark_codex["with"][key].strip()
                if isinstance(benchmark_codex["with"][key], str)
                else benchmark_codex["with"][key],
                f"{key} drifted between the production and benchmark reviews",
            )
        self.assertEqual(production_job["env"]["CODEX_MODEL"], "gpt-5.6-sol")
        self.assertEqual(production_job["env"]["CODEX_REASONING_EFFORT"], "xhigh")
        self.assertEqual(
            production_poster["env"]["CODEX_MODEL"],
            production_job["env"]["CODEX_MODEL"],
        )
        self.assertEqual(
            benchmark_codex["with"]["codex-args"],
            "${{ needs.select-cases.outputs.codex_args }}",
        )
        for name, output in (
            ("CODEX_MODEL", "model"),
            ("CODEX_REASONING_EFFORT", "reasoning_effort"),
            ("CODEX_SERVICE_TIER", "service_tier"),
            ("CODEX_VERBOSITY", "verbosity"),
        ):
            expected = f"${{{{ needs.select-cases.outputs.{output} }}}}"
            self.assertEqual(benchmark_job["env"][name], expected)
            self.assertEqual(benchmark_finalizer["env"][name], expected)

        production_writer = extract_python_heredoc(
            find_step(production, "security-review", "write_review_result")["run"]
        )
        benchmark_writer = extract_python_heredoc(
            find_step(benchmark, "benchmark-review", "write_artifacts")["run"]
        )
        production_validator = production_writer.split(
            "def validate_review_markdown", 1
        )[1].split("\nbase_sha =", 1)[0]
        benchmark_validator = benchmark_writer.split("def validate_review_markdown", 1)[
            1
        ].split("\nraw =", 1)[0]
        self.assertEqual(production_validator, benchmark_validator)

        # The benchmark checks out historical commits, so the review-packet body cannot
        # live in a local composite action; keeping the two copies byte-identical is what
        # makes a benchmark measurement describe the production packet.
        self.assertEqual(
            find_step(production, "review-agent", "write_diff")["run"],
            find_step(benchmark, "benchmark-review", "packet")["run"],
        )
        self.assertEqual(
            find_step(production, "review-agent", "write_diff")["env"],
            {"UNIFIED": "40", "INTER_HUNK": "0"},
        )

        # A step budget that drifts from the budget reported in the artifact would
        # misclassify a timeout as a hard failure.
        self.assertEqual(
            production_codex["timeout-minutes"],
            int(production_job["env"]["CODEX_TIMEOUT_MINUTES"]),
        )
        self.assertEqual(
            benchmark_codex["timeout-minutes"],
            int(benchmark_job["env"]["CODEX_TIMEOUT_MINUTES"]),
        )
        self.assertEqual(
            production_finalizer["env"]["CODEX_TIMEOUT_MINUTES"],
            production_job["env"]["CODEX_TIMEOUT_MINUTES"],
        )
        self.assertEqual(
            production_finalizer["env"]["CODEX_CANCELLATION_CLEANUP_SECONDS"],
            production_job["env"]["CODEX_CANCELLATION_CLEANUP_SECONDS"],
        )
        self.assertEqual(
            production_finalizer["env"]["CODEX_CANCELLATION_CLEANUP_SECONDS"],
            "300",
        )
        self.assertEqual(
            benchmark_finalizer["env"]["CODEX_TIMEOUT_MINUTES"],
            benchmark_job["env"]["CODEX_TIMEOUT_MINUTES"],
        )
        self.assertEqual(
            benchmark_finalizer["env"]["CODEX_CANCELLATION_CLEANUP_SECONDS"],
            "300",
        )
        self.assertLessEqual(
            production_codex["timeout-minutes"], production_job["timeout-minutes"]
        )
        self.assertLessEqual(
            benchmark_codex["timeout-minutes"], benchmark_job["timeout-minutes"]
        )

    def test_codex_benchmark_records_timeouts_distinctly_from_failures(self):
        # The timeout classification recorded here is the evidence used to decide whether
        # a step-level budget actually bounds the pinned composite action, so it has to be
        # executed rather than assumed.
        body = extract_python_heredoc(
            find_step(
                load_workflow("codex-security-review-benchmark.yml"),
                "benchmark-review",
                "write_artifacts",
            )["run"]
        )
        valid_output = json.dumps(
            {
                "overall_risk": "MEDIUM",
                "review_markdown": valid_review_markdown("MEDIUM"),
            }
        )
        cases = [
            (None, "success", valid_output, 30),
            ("codex-step-cancelled", "cancelled", "", 45),
            ("codex-step-timeout", "failure", "", 12 * 60),
            ("codex-step-failed", "failure", "", 45),
            ("empty-model-output", "success", "", 45),
            ("invalid-model-output", "success", "{", 45),
            (
                "invalid-model-output",
                "success",
                json.dumps(
                    {
                        "overall_risk": "LOW",
                        "review_markdown": "## Review Summary\n\n**Overall Risk**: HIGH\n",
                    }
                ),
                45,
            ),
        ]
        for expected_reason, outcome, output, age in cases:
            with self.subTest(reason=expected_reason):
                with tempfile.TemporaryDirectory() as tmp:
                    result = run_python_heredoc(
                        body,
                        {
                            "CODEX_OUTCOME": outcome,
                            "REVIEW_OUTPUT": output,
                            "CODEX_TIMEOUT_MINUTES": "12",
                            "STARTED_AT": str(int(time.time()) - age),
                            "PACKET_BYTES": "10",
                            "PACKET_LINES": "2",
                            "PACKET_FILES": "1",
                            "PACKET_HUNKS": "1",
                            "PACKET_CONTEXT_LINES": "1",
                            "CASE_ID": "pr-957",
                            "CASE_PR": "957",
                            "CASE_PURPOSE": "Historical production timeout",
                            "EXPECTED_RESULT": "adjudicate manually",
                            "SOURCE_RUN": "https://example.invalid/run",
                            "SOURCE_COMMENT": "",
                            "VARIANT_ID": "unified-40",
                            "UNIFIED": "40",
                            "INTER_HUNK": "0",
                            "REPEAT": "initial",
                            "PROMPT_PROFILE": "baseline",
                            "SERVICE_TIER": "default",
                            "VERBOSITY": "low",
                            "REVIEW_BASE_SHA": "a" * 40,
                            "REVIEW_HEAD_SHA": "b" * 40,
                            "REVIEW_COMMIT_RANGE": f"{'a' * 40}...{'b' * 40}",
                            "CODEX_MODEL": "gpt-5.6-terra",
                            "CODEX_REASONING_EFFORT": "high",
                        },
                        tmp,
                    )
                    self.assertEqual(result.returncode, 0, result.stderr)
                    review = json.loads(
                        (Path(tmp) / "benchmark-review.json").read_text(
                            encoding="utf-8"
                        )
                    )
                    scope = json.loads(
                        (Path(tmp) / "benchmark-scope.json").read_text(encoding="utf-8")
                    )

                self.assertEqual(review["incomplete_reason"], expected_reason)
                self.assertEqual(scope["incomplete_reason"], expected_reason)
                self.assertEqual(scope["timeout_budget_seconds"], 12 * 60)
                self.assertEqual(
                    review["benchmark_status"],
                    "completed" if expected_reason is None else "incomplete",
                )
                self.assertEqual(scope["completed"], expected_reason is None)
                self.assertEqual(scope["model"], "gpt-5.6-terra")
                self.assertEqual(scope["reasoning_effort"], "high")
                self.assertEqual(scope["service_tier"], "default")
                self.assertEqual(scope["verbosity"], "low")

        self.assertEqual(
            {reason for reason, _, _, _ in cases if reason},
            {
                "codex-step-cancelled",
                "codex-step-timeout",
                "codex-step-failed",
                "empty-model-output",
                "invalid-model-output",
            },
        )

    def test_codex_benchmark_finalizes_cancelled_matrix_jobs(self):
        workflow = load_workflow("codex-security-review-benchmark.yml")
        review_job = workflow["jobs"]["benchmark-review"]
        finalizer = workflow["jobs"]["benchmark-finalize"]

        self.assertEqual(review_job["timeout-minutes"], 12)
        self.assertEqual(finalizer["needs"], ["select-cases", "benchmark-review"])
        self.assertIn("always()", finalizer["if"])
        self.assertEqual(
            finalizer["strategy"]["matrix"], review_job["strategy"]["matrix"]
        )
        self.assertEqual(finalizer["strategy"]["max-parallel"], 2)

        inspect_script = find_step(workflow, "benchmark-finalize", "inspect")["with"][
            "script"
        ]
        successful_steps = [
            {"name": name, "conclusion": "success"}
            for name in (
                "Checkout fixed historical head",
                "Fetch and validate fixed historical range",
                "Write review packet and scope metrics",
                "Require benchmark API key",
            )
        ]
        timeout_job = {
            "name": "pr-957 / unified-40 / initial",
            "conclusion": "cancelled",
            "started_at": "2026-08-26T00:00:00Z",
            "completed_at": "2026-08-26T00:17:00Z",
            "steps": [
                *successful_steps,
                {
                    "name": "Run bounded benchmark review",
                    "status": "in_progress",
                    "conclusion": None,
                    "started_at": "2026-08-26T00:00:30Z",
                },
            ],
        }
        inspect_cases = [
            ("budget-timeout", timeout_job, "budget-timeout"),
            (
                "early-cancellation",
                {**timeout_job, "completed_at": "2026-08-26T00:05:00Z"},
                "unexpected-cancellation",
            ),
            (
                "cancellation-plus-cleanup",
                {**timeout_job, "completed_at": "2026-08-26T00:12:00Z"},
                "unexpected-cancellation",
            ),
            (
                "setup-cancellation",
                {**timeout_job, "steps": successful_steps[:1]},
                "unexpected-cancellation",
            ),
        ]
        for name, job, expected_classification in inspect_cases:
            with self.subTest(inspect=name), tempfile.TemporaryDirectory() as tmp:
                completed, output = run_github_script(
                    inspect_script,
                    {
                        "jobs": [job],
                        "artifacts": [],
                        "agent_job_name": timeout_job["name"],
                        "timeout_minutes": "12",
                    },
                    tmp,
                )
                self.assertEqual(completed.returncode, 0, completed.stderr)
                self.assertEqual(
                    output["outputs"]["classification"], expected_classification
                )

        timeout_writer = find_step(workflow, "benchmark-finalize", "write_timeout")
        self.assertEqual(
            timeout_writer["if"],
            "${{ steps.inspect.outputs.conclusion == 'cancelled' && steps.inspect.outputs.classification == 'budget-timeout' }}",
        )

        gate = find_step(workflow, "benchmark-finalize", "validate")["run"]
        cases = [
            ("success", "completed", "true", 0),
            ("success", "completed", "false", 1),
            ("cancelled", "budget-timeout", "false", 0),
            ("cancelled", "unexpected-cancellation", "false", 1),
            ("failure", "automation-failure", "false", 1),
        ]
        for conclusion, classification, artifact_exists, expected_code in cases:
            with self.subTest(
                conclusion=conclusion,
                classification=classification,
                artifact_exists=artifact_exists,
            ):
                result = subprocess.run(
                    ["bash", "-c", gate],
                    env={
                        **os.environ,
                        "AGENT_CONCLUSION": conclusion,
                        "AGENT_CLASSIFICATION": classification,
                        "AGENT_CLASSIFICATION_DETAIL": "test detail",
                        "RESULT_ARTIFACT_EXISTS": artifact_exists,
                        "RESULT_ARTIFACT_NAME": "benchmark-result-pr-957-unified-40-initial-123-1",
                    },
                    check=False,
                    capture_output=True,
                    text=True,
                )
                self.assertEqual(result.returncode, expected_code)

        body = extract_python_heredoc(
            find_step(workflow, "benchmark-finalize", "write_timeout")["run"]
        )
        with tempfile.TemporaryDirectory() as tmp:
            result = run_python_heredoc(
                body,
                {
                    "CODEX_TIMEOUT_MINUTES": "12",
                    "CODEX_MODEL": "gpt-5.6-terra",
                    "CODEX_REASONING_EFFORT": "high",
                    "REVIEW_BASE_SHA": "a" * 40,
                    "REVIEW_HEAD_SHA": "b" * 40,
                    "REVIEW_COMMIT_RANGE": f"{'a' * 40}...{'b' * 40}",
                    "CASE_ID": "pr-957",
                    "CASE_PR": "957",
                    "CASE_PURPOSE": "Historical production timeout",
                    "EXPECTED_RESULT": "adjudicate manually",
                    "SOURCE_RUN": "https://example.invalid/run",
                    "SOURCE_COMMENT": "",
                    "VARIANT_ID": "unified-40",
                    "UNIFIED": "40",
                    "INTER_HUNK": "0",
                    "REPEAT": "initial",
                    "PROMPT_PROFILE": "baseline",
                    "SERVICE_TIER": "default",
                    "VERBOSITY": "low",
                },
                tmp,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            review = json.loads(
                (Path(tmp) / "benchmark-review.json").read_text(encoding="utf-8")
            )
            scope = json.loads(
                (Path(tmp) / "benchmark-scope.json").read_text(encoding="utf-8")
            )
            elapsed_text = (Path(tmp) / "benchmark-elapsed-time.txt").read_text(
                encoding="utf-8"
            )
        self.assertEqual(review["incomplete_reason"], "codex-job-timeout")
        self.assertEqual(scope["incomplete_reason"], "codex-job-timeout")
        self.assertIsNone(scope["elapsed_seconds"])
        self.assertEqual(scope["timeout_budget_seconds"], 720)
        self.assertEqual(scope["model"], "gpt-5.6-terra")
        self.assertEqual(scope["reasoning_effort"], "high")
        self.assertEqual(scope["service_tier"], "default")
        self.assertEqual(scope["verbosity"], "low")
        self.assertEqual(elapsed_text, "unknown\n")

    def test_codex_benchmark_serializes_repeat_dispatches(self):
        workflow = load_workflow("codex-security-review-benchmark.yml")

        # Repeat dispatches are variance measurements, not replacements for each other,
        # so the group has to include every knob that distinguishes one run from another
        # while still queueing an accidental duplicate of the same configuration.
        group = workflow["concurrency"]["group"]
        self.assertFalse(workflow["concurrency"]["cancel-in-progress"])
        self.assertTrue(group.startswith("codex-security-review-benchmark-"))
        for name in (
            "corpus",
            "context-variant",
            "reasoning-effort",
            "prompt-profile",
            "repeat",
        ):
            self.assertIn(name, group)

    def test_incomplete_codex_review_cannot_take_approval_free_path(self):
        workflow = load_workflow("codex-security-review.yml")
        writer = find_step(workflow, "security-review", "write_review_result")
        self.assertIn('risk = "HIGH"', writer["run"])

        config = load_policy_config()
        allowed = {
            risk.upper() for risk in config["low_risk"]["allowed_security_risks"]
        }
        self.assertEqual(allowed, {"LOW", "NONE"})
        self.assertNotIn("HIGH", allowed)
        post_requirements = [
            requirement
            for requirement in config["low_risk"]["required_checks"]
            if requirement["name"] == "Post Codex Security Review"
        ]
        self.assertEqual(
            post_requirements,
            [
                {
                    "name": "Post Codex Security Review",
                    "type": "github_actions",
                    "workflow_path": ".github/workflows/codex-security-review.yml",
                    "event": "pull_request",
                }
            ],
        )

    def _run_review_result_writer(
        self,
        *,
        agent_result,
        review_output="",
        incomplete_reason=None,
        metadata_overrides=None,
        environment_overrides=None,
        expect_success=True,
    ):
        body = extract_python_heredoc(
            find_step(
                load_workflow("codex-security-review.yml"),
                "security-review",
                "write_review_result",
            )["run"]
        )
        base_sha = "a" * 40
        head_sha = "b" * 40
        with tempfile.TemporaryDirectory() as tmp:
            if agent_result == "success":
                metadata = {
                    "base_sha": base_sha,
                    "head_sha": head_sha,
                    "commit_range": f"{base_sha}...{head_sha}",
                    "run_id": "12345",
                    "elapsed_seconds": 545 if incomplete_reason else 30,
                    "timeout_budget_seconds": 540,
                    "incomplete_reason": incomplete_reason,
                    "review_packet": {
                        "context_variant": "unified-40",
                        "bytes": 4096,
                        "lines": 120,
                        "files": 3,
                        "hunks": 7,
                        "unchanged_context_lines": 80,
                    },
                }
                metadata.update(metadata_overrides or {})
                (Path(tmp) / "codex-security-review-raw-metadata.json").write_text(
                    json.dumps(metadata), encoding="utf-8"
                )
                (Path(tmp) / "codex-security-review-raw-output.txt").write_text(
                    review_output, encoding="utf-8"
                )
            environment = {
                "CODEX_TIMEOUT_MINUTES": "9",
                "REVIEW_BASE_SHA": base_sha,
                "REVIEW_HEAD_SHA": head_sha,
                "REVIEW_COMMIT_RANGE": f"{base_sha}...{head_sha}",
                "REVIEW_RUN_ID": "12345",
                "REVIEW_AGENT_RESULT": agent_result,
            }
            environment.update(environment_overrides or {})
            completed = run_python_heredoc(body, environment, tmp)
            if not expect_success:
                self.assertNotEqual(completed.returncode, 0)
                return None, completed.stderr
            self.assertEqual(completed.returncode, 0, completed.stderr)
            return (
                json.loads(
                    (Path(tmp) / "codex-security-review-result.json").read_text(
                        encoding="utf-8"
                    )
                ),
                (Path(tmp) / "codex-security-review.md").read_text(encoding="utf-8"),
            )

    def test_review_result_writer_emits_fail_closed_high_for_every_failure_mode(self):
        cases = [
            ("codex-job-timeout", "cancelled", "", None),
            ("codex-step-timeout", "success", "", "codex-step-timeout"),
            ("empty-model-output", "success", "   ", None),
            ("invalid-model-output", "success", "not json at all", None),
            (
                "invalid-model-output",
                "success",
                json.dumps(
                    {
                        "overall_risk": "LOW",
                        "review_markdown": "## Review Summary\n\n**Overall Risk**: HIGH\n",
                    }
                ),
                None,
            ),
            (
                "invalid-model-output",
                "success",
                json.dumps(
                    {
                        "overall_risk": "LOW",
                        "review_markdown": (
                            "## Review Summary\n\n**Overall Risk**: LOW\n\n"
                            "### Findings\n\n#### [HIGH] Contradictory finding\n"
                        ),
                    }
                ),
                None,
            ),
            (
                "invalid-model-output",
                "success",
                json.dumps(
                    {
                        "overall_risk": "LOW",
                        "review_markdown": "## Review Summary\n\n**Overall Risk**: LOW\n",
                    }
                ),
                None,
            ),
            (
                "invalid-model-output",
                "success",
                json.dumps(
                    {
                        "overall_risk": "LOW",
                        "review_markdown": valid_review_markdown("LOW").replace(
                            "#### [LOW]", "#### **[LOW]**"
                        ),
                    }
                ),
                None,
            ),
            (
                "invalid-model-output",
                "success",
                json.dumps(
                    {
                        "overall_risk": "LOW",
                        "review_markdown": valid_review_markdown("LOW").replace(
                            "- **Recommendation**: Apply the mitigation.\n", ""
                        ),
                    }
                ),
                None,
            ),
        ]
        for expected_reason, agent_result, output, handoff_reason in cases:
            with self.subTest(reason=expected_reason):
                result, markdown = self._run_review_result_writer(
                    agent_result=agent_result,
                    review_output=output,
                    incomplete_reason=handoff_reason,
                )
                self.assertEqual(result["overall_risk"], "HIGH")
                self.assertFalse(result["automation_completed"])
                self.assertEqual(result["incomplete_reason"], expected_reason)
                self.assertEqual(result["timeout_budget_seconds"], 540)
                if expected_reason == "codex-job-timeout":
                    self.assertIsNone(result["elapsed_seconds"])
                    self.assertIn("elapsed: unknown", markdown)
                self.assertIn("Human review is required", markdown)
                self.assertIn("Automated review incomplete", markdown)

        self.assertEqual({reason for reason, _, _, _ in cases}, set(INCOMPLETE_REASONS))
        allowed = {
            risk.upper()
            for risk in load_policy_config()["low_risk"]["allowed_security_risks"]
        }
        self.assertNotIn("HIGH", allowed)

    def test_review_result_writer_passes_through_a_completed_review(self):
        result, markdown = self._run_review_result_writer(
            agent_result="success",
            review_output=json.dumps(
                {
                    "overall_risk": "LOW",
                    "review_markdown": valid_review_markdown("LOW"),
                }
            ),
        )

        self.assertEqual(result["overall_risk"], "LOW")
        self.assertTrue(result["automation_completed"])
        self.assertNotIn("incomplete_reason", result)
        self.assertEqual(result["review_packet"]["context_variant"], "unified-40")
        self.assertEqual(result["review_packet"]["bytes"], 4096)
        self.assertIn("**Overall Risk**: LOW", markdown)

    def test_review_result_writer_rejects_identity_and_budget_drift(self):
        valid_output = json.dumps(
            {
                "overall_risk": "LOW",
                "review_markdown": valid_review_markdown("LOW"),
            }
        )
        metadata_cases = {
            "base_sha": "c" * 40,
            "head_sha": "c" * 40,
            "commit_range": f"{'c' * 40}...{'b' * 40}",
            "run_id": "99999",
        }
        for key, value in metadata_cases.items():
            with self.subTest(metadata=key):
                _, stderr = self._run_review_result_writer(
                    agent_result="success",
                    review_output=valid_output,
                    metadata_overrides={key: value},
                    expect_success=False,
                )
                self.assertIn("identity does not match", stderr)

        _, stderr = self._run_review_result_writer(
            agent_result="success",
            review_output=valid_output,
            metadata_overrides={"timeout_budget_seconds": 539},
            expect_success=False,
        )
        self.assertIn("timeout budget is malformed", stderr)

        trusted_identity_cases = [
            ({"REVIEW_BASE_SHA": "a" * 39}, "full lowercase commit IDs"),
            ({"REVIEW_COMMIT_RANGE": "malformed"}, "identity is malformed"),
            ({"REVIEW_RUN_ID": "not-a-run"}, "identity is malformed"),
        ]
        for overrides, expected_message in trusted_identity_cases:
            with self.subTest(environment=overrides):
                _, stderr = self._run_review_result_writer(
                    agent_result="success",
                    review_output=valid_output,
                    environment_overrides=overrides,
                    expect_success=False,
                )
                self.assertIn(expected_message, stderr)

    def test_broken_review_automation_remains_a_hard_failure(self):
        workflow = load_workflow("codex-security-review.yml")
        finalizer = workflow["jobs"]["security-review"]
        gate = find_step(workflow, "security-review", "validate_agent")["run"]
        self.assertEqual(finalizer["needs"], "review-agent")
        self.assertIn("always()", finalizer["if"])

        cases = [
            ("success", "completed", 0),
            ("cancelled", "budget-timeout", 0),
            ("cancelled", "unexpected-cancellation", 1),
            ("cancelled", "superseded", 1),
            ("failure", "automation-failure", 1),
        ]
        for result, classification, expected_code in cases:
            with self.subTest(result=result, classification=classification):
                completed = subprocess.run(
                    ["bash", "-c", gate],
                    env={
                        **os.environ,
                        "REVIEW_AGENT_RESULT": result,
                        "AGENT_CLASSIFICATION": classification,
                        "AGENT_CLASSIFICATION_DETAIL": "test detail",
                    },
                    check=False,
                    capture_output=True,
                    text=True,
                )
                self.assertEqual(completed.returncode, expected_code)

        raw_writer = extract_python_heredoc(
            find_step(workflow, "review-agent", "write_raw_review")["run"]
        )
        with tempfile.TemporaryDirectory() as tmp:
            completed = run_python_heredoc(
                raw_writer,
                {
                    "CODEX_OUTCOME": "failure",
                    "REVIEW_OUTPUT": "",
                    "CODEX_TIMEOUT_MINUTES": "9",
                    "REVIEW_BASE_SHA": "a" * 40,
                    "REVIEW_HEAD_SHA": "b" * 40,
                    "REVIEW_COMMIT_RANGE": f"{'a' * 40}...{'b' * 40}",
                    "REVIEW_RUN_ID": "12345",
                    "REVIEW_STARTED_AT": str(int(time.time()) - 30),
                    "PACKET_BYTES": "1",
                    "PACKET_LINES": "1",
                    "PACKET_FILES": "1",
                    "PACKET_HUNKS": "1",
                    "PACKET_CONTEXT_LINES": "1",
                },
                tmp,
            )
        self.assertNotEqual(completed.returncode, 0)
        self.assertIn("failed before its budget", completed.stderr)

    def test_workflow_ignored_events_do_not_cancel_active_evaluations(self):
        workflow = load_workflow("review-policy.yml")
        group_expression = workflow["concurrency"]["group"]
        cancel_expression = workflow["concurrency"]["cancel-in-progress"]

        self.assertIn("format('ignored-{0}', github.run_id)", group_expression)
        self.assertIn(
            "github.event.pull_request.head.sha || github.event.workflow_run.head_sha",
            group_expression,
        )
        self.assertIn(
            "github.event.check_run.app.slug == 'github-actions'", cancel_expression
        )
        self.assertIn("github.event.action == 'rerequested'", cancel_expression)
        self.assertIn(
            "github.event.check_suite.app.slug == 'github-actions'", cancel_expression
        )
        self.assertIn("github.event.context == 'Review Policy'", cancel_expression)
        self.assertIn(
            "github.event.context == 'Review Policy Advisory'", cancel_expression
        )
        self.assertIn("github.event.review.state == 'commented'", cancel_expression)

    def test_workflow_publishes_pending_status_for_cancelled_evaluation(self):
        workflow = load_workflow("review-policy.yml")
        publish_job = workflow["jobs"]["publish-status"]
        publish_step = publish_job["steps"][0]
        script = publish_step["with"]["script"]

        self.assertEqual(
            publish_job["if"], "always() && needs.evaluate.outputs.found == 'true'"
        )
        self.assertEqual(
            publish_job["concurrency"]["group"],
            "review-policy-publish-${{ needs.evaluate.outputs.head_sha }}",
        )
        self.assertEqual(
            publish_step["env"]["EVALUATE_RESULT"], "${{ needs.evaluate.result }}"
        )
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
        self.assertEqual(
            label_job["permissions"], {"issues": "write", "pull-requests": "write"}
        )

    def test_workflow_uses_default_branch_policy_for_stacked_prs(self):
        workflow_text = read_workflow("review-policy.yml")
        workflow = load_workflow("review-policy.yml")
        publish_env = workflow["jobs"]["publish-status"]["steps"][0]["env"]
        evaluate_outputs = workflow["jobs"]["evaluate"]["outputs"]
        evaluate_steps = workflow["jobs"]["evaluate"]["steps"]
        classifier_step = next(
            step for step in evaluate_steps if step.get("id") == "ai_classifier"
        )
        mode_index, mode_step = next(
            (index, step)
            for index, step in enumerate(evaluate_steps)
            if step.get("id") == "policy_mode"
        )
        checkout_step = next(
            step
            for step in evaluate_steps
            if step.get("name") == "Checkout trusted default-branch policy"
        )
        checkout_index = evaluate_steps.index(checkout_step)
        config_index, config_step = next(
            (index, step)
            for index, step in enumerate(evaluate_steps)
            if step.get("id") == "policy_config"
        )

        self.assertIn(
            "core.setOutput('default_branch', pr.base.repo.default_branch)",
            workflow_text,
        )
        self.assertNotIn("core.setOutput('trusted_base'", workflow_text)
        self.assertLess(mode_index, checkout_index)
        self.assertGreater(config_index, checkout_index)
        self.assertEqual(mode_step["if"], "steps.pr.outputs.found == 'true'")
        self.assertNotIn("POLICY_ROOT", mode_step["env"])
        self.assertEqual(
            config_step["env"]["STACKED_ADVISORY"],
            "${{ steps.policy_mode.outputs.stacked_advisory || 'false' }}",
        )
        self.assertEqual(
            checkout_step["with"]["ref"],
            "${{ github.event.repository.default_branch }}",
        )
        self.assertNotIn(
            "Review policy only evaluates PRs based on the repository default branch",
            workflow_text,
        )
        self.assertIn(
            "Using review policy code from the trusted default branch", workflow_text
        )
        self.assertIn(
            "REVIEW_BASE_SHA: ${{ steps.pr.outputs.base_sha }}", workflow_text
        )
        self.assertIn(
            "REVIEW_POLICY_ENFORCED: ${{ steps.policy_config.outputs.enforced || steps.policy_mode.outputs.enforced || 'true' }}",
            workflow_text,
        )
        self.assertNotIn('--base-ref "$BASE_REF"', workflow_text)
        self.assertNotIn('--default-branch "$DEFAULT_BRANCH"', workflow_text)
        self.assertIn('enforced="${REVIEW_POLICY_ENFORCED}"', workflow_text)
        self.assertEqual(
            evaluate_outputs["enforced"],
            "${{ steps.evaluate_policy.outputs.enforced || steps.policy_config.outputs.enforced || steps.bootstrap_policy.outputs.enforced || steps.policy_mode.outputs.enforced || 'true' }}",
        )
        self.assertEqual(
            evaluate_outputs["stacked_advisory"],
            "${{ steps.policy_mode.outputs.stacked_advisory || 'false' }}",
        )
        self.assertEqual(
            evaluate_outputs["config_enforced"],
            "${{ steps.policy_config.outputs.config_enforced || 'true' }}",
        )
        self.assertEqual(
            evaluate_outputs["policy_source_available"],
            "${{ steps.policy_source.outputs.available || 'false' }}",
        )
        self.assertIn("stacked_advisory=true", workflow_text)
        self.assertEqual(
            publish_env["DECISION"],
            "${{ needs.evaluate.outputs.decision || 'needs-human-review' }}",
        )
        self.assertEqual(
            publish_env["PASSED"], "${{ needs.evaluate.outputs.passed || 'false' }}"
        )
        self.assertEqual(
            publish_env["ENFORCED"], "${{ needs.evaluate.outputs.enforced || 'true' }}"
        )
        self.assertEqual(classifier_step["timeout-minutes"], 10)
        self.assertIn(
            "Tiny non-sensitive server utility changes are eligible",
            classifier_step["with"]["prompt"],
        )

    def test_workflow_invalidates_enforced_status_for_enforced_stacked_advisory(self):
        workflow_text = read_workflow("review-policy.yml")
        workflow = load_workflow("review-policy.yml")
        publish_env = workflow["jobs"]["publish-status"]["steps"][0]["env"]

        self.assertEqual(
            publish_env["CONFIG_ENFORCED"],
            "${{ needs.evaluate.outputs.config_enforced || 'true' }}",
        )
        self.assertEqual(
            publish_env["STACKED_ADVISORY"],
            "${{ needs.evaluate.outputs.stacked_advisory || 'false' }}",
        )
        self.assertEqual(
            publish_env["POLICY_SOURCE_AVAILABLE"],
            "${{ needs.evaluate.outputs.policy_source_available || 'false' }}",
        )
        self.assertIn("function hasNewerStatusRun(contextName)", workflow_text)
        self.assertIn("if (!hasNewerStatusRun('Review Policy'))", workflow_text)
        self.assertIn("context: 'Review Policy'", workflow_text)
        self.assertIn("state: enforcedAdvisoryState", workflow_text)
        self.assertIn(
            "Stacked PR result is advisory; default-branch PR must pass Review Policy.",
            workflow_text,
        )
        self.assertIn(
            "enforcedAdvisoryState = stackedAdvisory ? 'pending' : 'failure'",
            workflow_text,
        )
        self.assertIn(
            "Stacked PR policy source unavailable; default-branch PR must pass Review Policy.",
            workflow_text,
        )
        self.assertIn(
            "Review Policy is advisory; see Review Policy Advisory.", workflow_text
        )
        self.assertIn(
            "Trusted Review Policy source unavailable; human review required.",
            workflow_text,
        )

    def test_workflow_label_sync_is_bound_to_base_and_head(self):
        workflow_text = read_workflow("review-policy.yml")
        workflow = load_workflow("review-policy.yml")
        label_env = workflow["jobs"]["sync-label"]["steps"][0]["env"]

        self.assertEqual(
            label_env["BASE_SHA"], "${{ needs.evaluate.outputs.base_sha }}"
        )
        self.assertEqual(
            label_env["HEAD_SHA"], "${{ needs.evaluate.outputs.head_sha }}"
        )
        self.assertIn(
            "Missing evaluated base SHA for review policy label sync", workflow_text
        )
        self.assertIn(
            "pr.head.sha !== headSha || pr.base.sha !== baseSha", workflow_text
        )
        self.assertIn(
            "Skipping stale Review Policy label sync for ${baseSha}...${headSha}",
            workflow_text,
        )

    def test_path_matches_double_star_root_file(self):
        self.assertTrue(policy.path_matches("package.json", "**/package.json"))
        self.assertTrue(policy.path_matches("client/package.json", "**/package.json"))

    def test_path_matches_directory_prefix(self):
        self.assertTrue(
            policy.path_matches(".github/workflows/review-policy.yml", ".github/**")
        )
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

    def test_denied_paths_honors_explicit_exceptions(self):
        files = [
            {"filename": "deployment-files/README.md"},
            {"filename": "deployment-files/ha/QUALIFICATION.md"},
            {"filename": "deployment-files/docker-compose.yaml"},
            {
                "filename": "deployment-files/ha/README.md",
                "previous_filename": "deployment-files/ha/install.sh",
            },
        ]

        self.assertEqual(
            policy.denied_paths(
                files,
                ["deployment-files/**"],
                ["deployment-files/*.md", "deployment-files/**/*.md"],
            ),
            ["deployment-files/docker-compose.yaml", "deployment-files/ha/install.sh"],
        )

    def test_effective_enforcement_honors_workflow_override(self):
        self.assertFalse(policy.effective_enforcement(False, None))
        self.assertTrue(policy.effective_enforcement(True, None))
        self.assertFalse(policy.effective_enforcement(True, "false"))
        self.assertTrue(policy.effective_enforcement(False, "true"))

    def test_low_risk_config_allows_small_server_app_changes_to_reach_classifier(self):
        deny_paths = load_policy_config()["low_risk"]["deny_paths"]
        files = [
            {"filename": "server/devtools/hapoc/main.go"},
            {"filename": "server/fake-proto-rig/rest_api_handler.go"},
            {"filename": "server/internal/domain/telemetry/scheduler/scheduler.go"},
            {"filename": "server/internal/domain/collection/service_test.go"},
        ]

        self.assertEqual(policy.denied_paths(files, deny_paths), [])

    def test_low_risk_config_allows_deployment_markdown_docs_to_reach_classifier(self):
        low_risk = load_policy_config()["low_risk"]
        files = [
            {"filename": "deployment-files/README.md"},
            {"filename": "deployment-files/ha/QUALIFICATION.md"},
        ]

        self.assertEqual(
            policy.denied_paths(
                files, low_risk["deny_paths"], low_risk["deny_path_exceptions"]
            ),
            [],
        )

    def test_low_risk_config_keeps_deployment_runtime_files_denied(self):
        low_risk = load_policy_config()["low_risk"]
        files = [
            {"filename": "deployment-files/docker-compose.yaml"},
            {"filename": "deployment-files/ha/scripts/install.sh"},
        ]

        self.assertEqual(
            policy.denied_paths(
                files, low_risk["deny_paths"], low_risk["deny_path_exceptions"]
            ),
            [
                "deployment-files/docker-compose.yaml",
                "deployment-files/ha/scripts/install.sh",
            ],
        )

    def test_low_risk_config_keeps_sensitive_server_paths_denied(self):
        deny_paths = load_policy_config()["low_risk"]["deny_paths"]
        files = [
            {"filename": "server/cmd/fleetd/main.go"},
            {"filename": "server/generated/grpc/device/v1/device.pb.go"},
            {"filename": "server/internal/domain/apikey/service.go"},
            {"filename": "server/internal/domain/auth/service.go"},
            {"filename": "server/internal/domain/authz/catalog.go"},
            {"filename": "server/internal/domain/command/service.go"},
            {"filename": "server/internal/domain/commandtype/metadata.go"},
            {"filename": "server/internal/domain/deviceresolver/resolver.go"},
            {"filename": "server/internal/domain/discoverylimits/service.go"},
            {"filename": "server/internal/domain/fleetnode/control/service.go"},
            {"filename": "server/internal/domain/ipscanner/scanner.go"},
            {"filename": "server/internal/domain/minerdiscovery/service.go"},
            {"filename": "server/internal/domain/nmaptarget/parser.go"},
            {"filename": "server/internal/domain/pairing/service.go"},
            {"filename": "server/internal/domain/pools/service.go"},
            {"filename": "server/internal/domain/session/service.go"},
            {"filename": "server/internal/domain/stores/sqlstores/device.go"},
            {"filename": "server/internal/domain/token/service.go"},
            {"filename": "server/internal/handlers/pools/handler.go"},
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
                "server/internal/domain/apikey/service.go",
                "server/internal/domain/auth/service.go",
                "server/internal/domain/authz/catalog.go",
                "server/internal/domain/command/service.go",
                "server/internal/domain/commandtype/metadata.go",
                "server/internal/domain/deviceresolver/resolver.go",
                "server/internal/domain/discoverylimits/service.go",
                "server/internal/domain/fleetnode/control/service.go",
                "server/internal/domain/ipscanner/scanner.go",
                "server/internal/domain/minerdiscovery/service.go",
                "server/internal/domain/nmaptarget/parser.go",
                "server/internal/domain/pairing/service.go",
                "server/internal/domain/pools/service.go",
                "server/internal/domain/session/service.go",
                "server/internal/domain/stores/sqlstores/device.go",
                "server/internal/domain/token/service.go",
                "server/internal/handlers/pools/handler.go",
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
            policy.load_classifier(
                'warning\n{"risk":"low","confidence":0.9,"requires_human_review":false,"reasons":[]}'
            )

    def test_classifier_rejects_non_finite_confidence(self):
        classifier = policy.load_classifier(
            '{"risk":"low","confidence":NaN,"requires_human_review":false,"reasons":[]}'
        )
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

        self.assertIn(
            "client/src/foo.ts has 81 changed lines, exceeds per-file limit 80",
            blockers,
        )
        self.assertIn(
            "client/src/foo.ts adds blocked content: adds process execution or shell-out code",
            blockers,
        )
        self.assertIn(
            "client/src/opaque.bin diff content is unavailable for deterministic content checks",
            blockers,
        )
        self.assertEqual(len(blockers), 3)

    def test_deterministic_content_blockers_allows_larger_test_files(self):
        files = [
            {
                "filename": "server/internal/domain/alerts/grafana_client.go",
                "additions": 95,
                "deletions": 0,
                "patch": "@@\n+const safe = true",
            },
            {
                "filename": "server/internal/domain/alerts/grafana_client_test.go",
                "additions": 95,
                "deletions": 0,
                "patch": "@@\n+func TestSafe(t *testing.T) {}",
            },
            {
                "filename": "test/helpers/policy_fixture.ts",
                "additions": 95,
                "deletions": 0,
                "patch": "@@\n+export const fixture = true;",
            },
            {
                "filename": "tests/helpers/policy_fixture.ts",
                "additions": 95,
                "deletions": 0,
                "patch": "@@\n+export const fixture = true;",
            },
            {
                "filename": "__tests__/policy_fixture.ts",
                "additions": 95,
                "deletions": 0,
                "patch": "@@\n+export const fixture = true;",
            },
            {
                "filename": "e2etests/policy_fixture.ts",
                "additions": 95,
                "deletions": 0,
                "patch": "@@\n+export const fixture = true;",
            },
        ]

        blockers = policy.deterministic_content_blockers(
            files,
            {
                "max_file_changes": 80,
                "max_test_file_changes": 120,
                "content_deny_added_patterns": [],
            },
        )

        self.assertEqual(
            blockers,
            [
                "server/internal/domain/alerts/grafana_client.go has 95 changed lines, exceeds per-file limit 80"
            ],
        )

    def test_low_risk_preflight_blocks_before_classifier(self):
        original_paginate = policy.github_paginate
        original_request = policy.github_request
        original_trusted_author_reasons = policy.trusted_author_reasons
        try:

            def fake_paginate(path, token):
                if path.endswith("/files"):
                    return [
                        {
                            "filename": ".github/workflows/review-policy.yml",
                            "additions": 2,
                            "deletions": 1,
                        },
                        {
                            "filename": "docs/readme.md",
                            "additions": 300,
                            "deletions": 0,
                        },
                    ]
                if path.endswith("/commits"):
                    return [
                        {
                            "sha": "abc123",
                            "author": {"login": "author"},
                            "committer": {"login": "author"},
                        }
                    ]
                if path.endswith("/commits/abc123/pulls"):
                    return [{"number": 123, "state": "open", "head": {"sha": "abc123"}}]
                return []

            policy.github_paginate = fake_paginate
            policy.github_request = lambda method, path, token, body=None: {
                "state": "open",
                "head": {"sha": "abc123"},
            }
            policy.trusted_author_reasons = (
                lambda author, trusted_authors, owner, token: (
                    False,
                    [f"author @{author} is not in trusted_authors"],
                )
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
        self.assertIn(
            "denied paths changed: .github/workflows/review-policy.yml",
            result["blockers"],
        )

    def test_shared_head_pr_blockers_fail_closed_for_multiple_open_prs(self):
        original = policy.github_paginate
        try:
            policy.github_paginate = lambda path, token: [
                {"number": 123, "state": "open", "head": {"sha": "abc123"}},
                {"number": 456, "state": "open", "head": {"sha": "abc123"}},
                {"number": 789, "state": "closed", "head": {"sha": "abc123"}},
            ]
            blockers = policy.shared_head_pr_blockers(
                "block", "proto-fleet", 123, "abc123", "token"
            )
        finally:
            policy.github_paginate = original

        self.assertEqual(
            blockers, ["current head SHA is shared by multiple open PRs: #123, #456"]
        )

    def test_trusted_author_reasons_accepts_team_membership(self):
        original = policy.is_team_member
        try:
            policy.is_team_member = lambda owner, team_slug, username, token: (
                owner == "block"
                and team_slug == "proto-fleet-dev"
                and username == "member"
            )
            trusted, reasons = policy.trusted_author_reasons(
                "member", ["@block/proto-fleet-dev"], "block", "token"
            )
        finally:
            policy.is_team_member = original

        self.assertTrue(trusted)
        self.assertEqual(
            reasons, ["author @member is a member of @block/proto-fleet-dev"]
        )

    def test_trusted_author_reasons_accepts_case_insensitive_login(self):
        trusted, reasons = policy.trusted_author_reasons(
            "AnkitGoswami", ["ankitgoswami"], "block", "token"
        )

        self.assertTrue(trusted)
        self.assertEqual(reasons, ["author @AnkitGoswami is explicitly trusted"])

    def test_trusted_head_contributor_reasons_blocks_untrusted_committers(self):
        original = policy.trusted_author_reasons
        try:
            policy.trusted_author_reasons = (
                lambda author, trusted_authors, owner, token: (
                    author == "trusted",
                    [f"author @{author} is explicitly trusted"]
                    if author == "trusted"
                    else [f"author @{author} is not in trusted_authors"],
                )
            )
            ok, reasons, blockers = policy.trusted_head_contributor_reasons(
                [
                    {
                        "sha": "abc123",
                        "author": {"login": "trusted"},
                        "committer": {"login": "untrusted"},
                    },
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
        self.assertIn(
            "current head has commits without GitHub-linked authors or committers: def456",
            blockers,
        )

    def test_trusted_workflow_actor_reasons_requires_trusted_authenticated_actor(self):
        original_workflow_runs = policy.latest_workflow_runs
        original_trusted_author_reasons = policy.trusted_author_reasons
        try:
            policy.latest_workflow_runs = lambda owner, repo, head_sha, event, token: {
                ".github/workflows/pr-gate.yml": {"actor": {"login": "untrusted"}},
            }
            policy.trusted_author_reasons = (
                lambda author, trusted_authors, owner, token: (
                    author == "trusted",
                    [f"author @{author} trust checked"],
                )
            )
            ok, reasons, blockers = policy.trusted_workflow_actor_reasons(
                "block",
                "proto-fleet",
                "abc123",
                ["trusted"],
                {
                    "workflow_path": ".github/workflows/pr-gate.yml",
                    "event": "pull_request",
                },
                "token",
            )
        finally:
            policy.latest_workflow_runs = original_workflow_runs
            policy.trusted_author_reasons = original_trusted_author_reasons

        self.assertFalse(ok)
        self.assertEqual(reasons, [])
        self.assertEqual(
            blockers,
            ["authenticated workflow actor @untrusted is not in trusted_authors"],
        )

    def test_latest_check_runs_tie_breaks_on_id(self):
        original = policy.github_paginate_key
        try:
            policy.github_paginate_key = lambda path, token, key: [
                {
                    "name": "Gate",
                    "started_at": "2026-01-01T00:00:00Z",
                    "id": 1,
                    "conclusion": "failure",
                },
                {
                    "name": "Gate",
                    "started_at": "2026-01-01T00:00:00Z",
                    "id": 2,
                    "conclusion": "success",
                },
            ]
            latest = policy.latest_check_runs("block", "proto-fleet", "abc123", "token")
        finally:
            policy.github_paginate_key = original

        self.assertEqual(latest["Gate"]["id"], 2)

    def test_latest_check_runs_prefers_newer_workflow_over_later_finalizer(self):
        original = policy.github_paginate_key
        original_request = policy.github_request
        try:
            policy.github_paginate_key = lambda path, token, key: [
                {
                    "name": "security-review",
                    "started_at": "2026-01-01T00:10:00Z",
                    "id": 10,
                    "details_url": "https://github.com/block/proto-fleet/actions/runs/100/job/10",
                    "conclusion": "success",
                },
                {
                    "name": "security-review",
                    "started_at": "2026-01-01T00:05:00Z",
                    "id": 11,
                    "details_url": "https://github.com/block/proto-fleet/actions/runs/101/job/11",
                    "conclusion": "success",
                },
            ]
            policy.github_request = lambda method, path, token, body=None: {
                "run_started_at": (
                    "2026-01-01T00:00:00Z"
                    if path.endswith("/100")
                    else "2026-01-01T00:05:00Z"
                ),
                "run_attempt": 1,
            }
            latest = policy.latest_check_runs("block", "proto-fleet", "abc123", "token")
        finally:
            policy.github_paginate_key = original
            policy.github_request = original_request

        self.assertEqual(latest["security-review"]["id"], 11)

    def test_latest_check_runs_allows_new_attempt_of_older_run_id(self):
        original = policy.github_paginate_key
        original_request = policy.github_request
        try:
            policy.github_paginate_key = lambda path, token, key: [
                {
                    "name": "security-review",
                    "started_at": "2026-01-01T00:20:00Z",
                    "id": 12,
                    "details_url": "https://github.com/block/proto-fleet/actions/runs/100/job/12",
                    "conclusion": "success",
                },
                {
                    "name": "security-review",
                    "started_at": "2026-01-01T00:05:00Z",
                    "id": 11,
                    "details_url": "https://github.com/block/proto-fleet/actions/runs/101/job/11",
                    "conclusion": "failure",
                },
            ]
            policy.github_request = lambda method, path, token, body=None: {
                "run_started_at": (
                    "2026-01-01T00:20:00Z"
                    if path.endswith("/100")
                    else "2026-01-01T00:05:00Z"
                ),
                "run_attempt": 2 if path.endswith("/100") else 1,
            }
            latest = policy.latest_check_runs("block", "proto-fleet", "abc123", "token")
        finally:
            policy.github_paginate_key = original
            policy.github_request = original_request

        self.assertEqual(latest["security-review"]["id"], 12)

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
                "Post Codex Security Review": {
                    "status": "completed",
                    "conclusion": "failure",
                    "app": {"slug": "github-actions"},
                    "details_url": "https://github.com/block/proto-fleet/actions/runs/124/job/789",
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
                    {
                        "name": "Post Codex Security Review",
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
        self.assertIn(
            "required check 'Post Codex Security Review' is completed/failure",
            blockers,
        )
        self.assertIn("required check 'missing' is missing", blockers)

    def test_check_statuses_accepts_typed_commit_statuses(self):
        original = policy.latest_check_runs
        original_statuses = policy.latest_commit_statuses
        try:
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {
                "DCO Check": {
                    "status": "completed",
                    "conclusion": "success",
                    "app": {"slug": "block-dco-check"},
                },
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
                    {
                        "name": "DCO Check",
                        "type": "check_run",
                        "app_slug": "block-dco-check",
                    },
                    {
                        "name": "Legacy",
                        "type": "commit_status",
                        "creator": "trusted-bot",
                    },
                    {
                        "name": "External",
                        "type": "commit_status",
                        "creator": "external-ci",
                    },
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
        self.assertIn(
            "required workflow 'PR Gate' name is 'Attacker Gate', expected 'PR Gate'",
            blockers,
        )

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
            latest = policy.latest_workflow_runs(
                "block", "proto-fleet", "abc123", "pull_request", "token"
            )
        finally:
            policy.github_paginate_key = original

        self.assertEqual(latest[".github/workflows/pr-gate.yml"]["id"], 2)

    def test_check_statuses_requires_trusted_sources_in_config(self):
        original = policy.latest_check_runs
        original_statuses = policy.latest_commit_statuses
        try:
            policy.latest_check_runs = lambda owner, repo, head_sha, token: {
                "DCO Check": {
                    "status": "completed",
                    "conclusion": "success",
                    "app": {"slug": "block-dco-check"},
                },
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
                    {
                        "name": "security-review",
                        "type": "github_actions",
                        "event": "pull_request",
                    },
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
        self.assertIn(
            "required check 'DCO Check' is missing trusted app_slug", blockers
        )
        self.assertIn(
            "required check 'security-review' is missing trusted workflow_path",
            blockers,
        )
        self.assertIn("required workflow 'PR Gate' is missing trusted event", blockers)
        self.assertIn("required status 'Legacy' is missing trusted creator", blockers)

    def test_latest_commit_statuses_tie_breaks_on_id(self):
        original = policy.github_paginate
        try:
            policy.github_paginate = lambda path, token: [
                {
                    "context": "DCO Check",
                    "created_at": "2026-01-01T00:00:00Z",
                    "id": 1,
                    "state": "failure",
                },
                {
                    "context": "DCO Check",
                    "created_at": "2026-01-01T00:00:00Z",
                    "id": 2,
                    "state": "success",
                },
            ]
            latest = policy.latest_commit_statuses(
                "block", "proto-fleet", "abc123", "token"
            )
        finally:
            policy.github_paginate = original

        self.assertEqual(latest["DCO Check"]["id"], 2)

    def test_extract_run_id(self):
        self.assertEqual(
            policy.extract_run_id(
                "https://github.com/block/proto-fleet/actions/runs/123/job/456"
            ),
            "123",
        )
        self.assertIsNone(
            policy.extract_run_id("https://github.com/block/proto-fleet/runs/123")
        )

    def test_security_review_result_contract_gates_automation_completion(self):
        identity = {
            "head_sha": "abc123",
            "commit_range": "base123...abc123",
            "run_id": "123",
        }
        cases = [
            (
                {**identity, "overall_risk": "LOW", "automation_completed": True},
                "LOW",
                [],
            ),
            (
                {
                    **identity,
                    "overall_risk": "HIGH",
                    "automation_completed": False,
                    "incomplete_reason": "codex-job-timeout",
                },
                "HIGH",
                [],
            ),
            (
                {**identity, "overall_risk": "NONE"},
                None,
                ["missing or invalid automation_completed"],
            ),
            (
                {
                    **identity,
                    "overall_risk": "NONE",
                    "automation_completed": False,
                    "incomplete_reason": "codex-job-timeout",
                },
                None,
                ["not a fail-closed HIGH result"],
            ),
            (
                {
                    **identity,
                    "overall_risk": "HIGH",
                    "automation_completed": False,
                },
                None,
                ["not a fail-closed HIGH result"],
            ),
            (
                {
                    **identity,
                    "overall_risk": "LOW",
                    "automation_completed": True,
                    "incomplete_reason": "empty-model-output",
                },
                None,
                ["completed artifact has an incomplete_reason"],
            ),
        ]
        for result, expected_risk, blocker_fragments in cases:
            with self.subTest(result=result):
                risk, blockers = policy.validate_security_review_result(
                    result, "base123", "abc123", "123"
                )
                self.assertEqual(risk, expected_risk)
                self.assertEqual(len(blockers), len(blocker_fragments))
                for fragment in blocker_fragments:
                    self.assertTrue(any(fragment in blocker for blocker in blockers))

    def test_extract_security_risk_validates_workflow_run_identity(self):
        original_paginate_key = policy.github_paginate_key
        original_request = policy.github_request
        original_download = policy.github_download
        archive_bytes = io.BytesIO()
        with zipfile.ZipFile(archive_bytes, "w") as archive:
            archive.writestr(
                "codex-security-review-result.json",
                json.dumps(
                    {
                        "head_sha": "abc123",
                        "commit_range": "base123...abc123",
                        "run_id": "123",
                        "overall_risk": "LOW",
                        "automation_completed": True,
                    }
                ),
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
                    return [
                        {
                            "id": 999,
                            "name": "codex-security-review-result",
                            "expired": False,
                        }
                    ]
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
                json.dumps(
                    {
                        "head_sha": "abc123",
                        "commit_range": "oldbase...abc123",
                        "run_id": "123",
                        "overall_risk": "LOW",
                        "automation_completed": True,
                    }
                ),
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
                    return [
                        {
                            "id": 999,
                            "name": "codex-security-review-result",
                            "expired": False,
                        }
                    ]
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
        self.assertIn(
            "Codex security-review result artifact is stale for this PR base/head range",
            blockers,
        )

    def test_extract_security_risk_rejects_non_string_risk(self):
        original_paginate_key = policy.github_paginate_key
        original_request = policy.github_request
        original_download = policy.github_download
        archive_bytes = io.BytesIO()
        with zipfile.ZipFile(archive_bytes, "w") as archive:
            archive.writestr(
                "codex-security-review-result.json",
                json.dumps(
                    {
                        "head_sha": "abc123",
                        "commit_range": "base123...abc123",
                        "run_id": "123",
                        "overall_risk": None,
                        "automation_completed": True,
                    }
                ),
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
                    return [
                        {
                            "id": 999,
                            "name": "codex-security-review-result",
                            "expired": False,
                        }
                    ]
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
        self.assertIn(
            "Codex security-review result artifact is missing or invalid overall_risk",
            blockers,
        )

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
                    return [
                        {
                            "sha": "abc123",
                            "author": {"login": "author"},
                            "committer": {"login": "author"},
                        }
                    ]
                if path.endswith("/reviews"):
                    return []
                return []

            policy.github_paginate = fake_paginate
            policy.github_request = lambda method, path, token, body=None: {
                "state": "open",
                "head": {"sha": "abc123"},
                "base": {"sha": "base123"},
            }
            policy.trusted_author_reasons = (
                lambda author, trusted_authors, owner, token: (
                    True,
                    [f"author @{author} is explicitly trusted"],
                )
            )
            policy.check_statuses = (
                lambda owner, repo, head_sha, required_checks, token, latest_by_name=None: (
                    True,
                    [],
                )
            )
            policy.extract_security_risk = (
                lambda owner, repo, base_sha, head_sha, token, check_name, workflow_path, artifact_name, latest_by_name=None: (
                    "LOW",
                    [],
                )
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

    def test_evaluate_policy_blocks_fail_closed_high_risk_for_a_trusted_author(self):
        # The writer emits HIGH whenever the bounded review does not produce usable
        # output; this is the other half of that contract, proving the same pull request
        # that merges approval-free at LOW is blocked once the review is incomplete.
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
                    return [
                        {
                            "sha": "abc123",
                            "author": {"login": "author"},
                            "committer": {"login": "author"},
                        }
                    ]
                if path.endswith("/reviews"):
                    return []
                return []

            policy.github_paginate = fake_paginate
            policy.github_request = lambda method, path, token, body=None: {
                "state": "open",
                "head": {"sha": "abc123"},
                "base": {"sha": "base123"},
            }
            policy.trusted_author_reasons = (
                lambda author, trusted_authors, owner, token: (
                    True,
                    [f"author @{author} is explicitly trusted"],
                )
            )
            policy.check_statuses = (
                lambda owner, repo, head_sha, required_checks, token, latest_by_name=None: (
                    True,
                    [],
                )
            )
            policy.extract_security_risk = (
                lambda owner, repo, base_sha, head_sha, token, check_name, workflow_path, artifact_name, latest_by_name=None: (
                    "HIGH",
                    [],
                )
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

        self.assertFalse(result.passed)
        self.assertEqual(result.decision, "needs-human-review")
        self.assertIn(
            "Codex security review risk is HIGH, not one of ['LOW', 'NONE']",
            result.reasons,
        )

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
                    return [
                        {
                            "sha": "abc123",
                            "author": {"login": "author"},
                            "committer": {"login": "author"},
                        }
                    ]
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
            policy.trusted_author_reasons = (
                lambda author, trusted_authors, owner, token: (
                    True,
                    [f"author @{author} is explicitly trusted"],
                )
            )
            policy.check_statuses = (
                lambda owner, repo, head_sha, required_checks, token, latest_by_name=None: (
                    True,
                    [],
                )
            )
            policy.extract_security_risk = (
                lambda owner, repo, base_sha, head_sha, token, check_name, workflow_path, artifact_name, latest_by_name=None: (
                    "LOW",
                    [],
                )
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
        self.assertIn(
            "current authorized human approvals: reviewer", result.human_review_reasons
        )

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
                    return [
                        {
                            "filename": "client/src/foo.ts",
                            "additions": 1,
                            "deletions": 0,
                            "patch": "@@\n+const x = 1",
                        }
                    ]
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
            policy.trusted_author_reasons = (
                lambda author, trusted_authors, owner, token: (
                    True,
                    [f"author @{author} is explicitly trusted"],
                )
            )
            policy.check_statuses = (
                lambda owner, repo, head_sha, required_checks, token, latest_by_name=None: (
                    True,
                    [],
                )
            )
            policy.extract_security_risk = (
                lambda owner, repo, base_sha, head_sha, token, check_name, workflow_path, artifact_name, latest_by_name=None: (
                    "LOW",
                    [],
                )
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
        self.assertIn(
            "current head has commits without GitHub-linked authors or committers: def456",
            result.reasons,
        )

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
        self.assertEqual(
            result.reasons, ["pull request #123 head is newhead, expected abc123"]
        )

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
                    return [
                        {
                            "filename": "client/src/foo.ts",
                            "additions": 1,
                            "deletions": 0,
                            "patch": "@@\n+const x = 1",
                        }
                    ]
                if path.endswith("/commits"):
                    return [
                        {
                            "sha": "abc123",
                            "author": {"login": "author"},
                            "committer": {"login": "author"},
                        }
                    ]
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
            policy.trusted_author_reasons = (
                lambda author, trusted_authors, owner, token: (
                    True,
                    [f"author @{author} is explicitly trusted"],
                )
            )
            policy.check_statuses = (
                lambda owner, repo, head_sha, required_checks, token, latest_by_name=None: (
                    True,
                    [],
                )
            )
            policy.extract_security_risk = (
                lambda owner, repo, base_sha, head_sha, token, check_name, workflow_path, artifact_name, latest_by_name=None: (
                    "LOW",
                    [],
                )
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
        self.assertIn(
            "ignored approvals from PR contributors: pusher",
            result.human_review_reasons,
        )

    def test_human_review_state_ignores_unauthorized_approvals(self):
        original = policy.reviewer_has_authority
        try:
            policy.reviewer_has_authority = lambda owner, repo, username, token: (
                username == "member"
            )
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
            approved_ok, _approved_reasons, approved_blockers = (
                policy.human_review_state(
                    approved_reviews,
                    "abc123",
                    "author",
                    1,
                    "block",
                    "proto-fleet",
                    "token",
                )
            )
            dismissed_ok, _dismissed_reasons, dismissed_blockers = (
                policy.human_review_state(
                    dismissed_reviews,
                    "abc123",
                    "author",
                    1,
                    "block",
                    "proto-fleet",
                    "token",
                )
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
