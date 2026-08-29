#!/usr/bin/env python3
"""Validate and deterministically aggregate two bounded Codex shard results."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
from pathlib import Path
from typing import Any

from write_codex_shard_result import SEVERITY_RANK, validate_review_markdown

ALLOWED_INCOMPLETE_REASONS = {
    "codex-job-timeout",
    "empty-model-output",
    "invalid-model-output",
    "oversized-review",
}


def validate_manifest(manifest: dict[str, Any]) -> None:
    digest = manifest.get("manifest_digest")
    unsigned = dict(manifest)
    unsigned.pop("manifest_digest", None)
    actual = hashlib.sha256(
        json.dumps(unsigned, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()
    if digest != actual:
        raise ValueError("shard manifest digest is invalid")
    if manifest.get("schema_version") != 1:
        raise ValueError("unsupported shard manifest schema")
    if [shard.get("id") for shard in manifest.get("shards", [])] != [
        "shard-1",
        "shard-2",
    ]:
        raise ValueError("manifest must define exactly shard-1 and shard-2")

    owner_map = {
        path: shard["id"]
        for shard in manifest["shards"]
        for path in shard["primary_files"]
    }
    owners = [path for shard in manifest["shards"] for path in shard["primary_files"]]
    file_records = manifest.get("files", [])
    changed = [file["path"] for file in file_records]
    if sorted(owners) != sorted(changed) or len(owners) != len(set(owners)):
        raise ValueError(
            "manifest does not give every changed file exactly one primary owner"
        )
    if any(
        file.get("primary_shard") != owner_map[file["path"]] for file in file_records
    ):
        raise ValueError("manifest file ownership metadata is inconsistent")


def validate_result(
    result: dict[str, Any], manifest: dict[str, Any], shard: dict[str, Any]
) -> None:
    expected = {
        "schema_version": 1,
        "case": manifest["case"],
        "variant": manifest["variant"],
        "repeat": manifest["repeat"],
        "base_sha": manifest["base_sha"],
        "head_sha": manifest["head_sha"],
        "commit_range": manifest["commit_range"],
        "manifest_digest": manifest["manifest_digest"],
        "run_id": int(os.environ["GITHUB_RUN_ID"]),
        "run_attempt": int(os.environ["GITHUB_RUN_ATTEMPT"]),
        "shard_id": shard["id"],
        "primary_files": shard["primary_files"],
        "shared_files": shard["shared_files"],
        "packet_bytes": shard["packet_bytes"],
        "packet_lines": shard["packet_lines"],
    }
    for key, value in expected.items():
        if result.get(key) != value:
            raise ValueError(f"{shard['id']} result has invalid {key}")

    status = result.get("status")
    reason = result.get("incomplete_reason")
    review = result.get("review")
    if manifest["status"] == "oversized":
        expected_status = "incomplete" if shard["id"] == "shard-1" else "inactive"
        if status != expected_status:
            raise ValueError(f"oversized plan has invalid {shard['id']} status")
        if status == "incomplete" and reason != "oversized-review":
            raise ValueError("oversized result has invalid reason")
    elif not shard["active"]:
        if status != "inactive" or reason is not None or review is not None:
            raise ValueError("inactive shard has an invalid result")
    elif status == "completed":
        if reason is not None or not isinstance(review, dict):
            raise ValueError("completed shard lacks a review")
        risk = review.get("overall_risk")
        markdown = review.get("review_markdown")
        if risk not in SEVERITY_RANK or not isinstance(markdown, str):
            raise ValueError("completed shard review has an invalid shape")
        validate_review_markdown(markdown, risk)
    elif status == "incomplete":
        if reason not in ALLOWED_INCOMPLETE_REASONS or review is not None:
            raise ValueError("incomplete shard result is not validated evidence")
    else:
        raise ValueError(f"unexpected shard status {status!r}")


def finding_blocks(markdown: str, shard_id: str) -> list[tuple[int, str, str, str]]:
    findings_match = re.search(r"(?m)^### Findings[ \t]*$", markdown)
    notes_match = re.search(r"(?m)^### Notes[ \t]*$", markdown)
    if not findings_match or not notes_match:
        raise ValueError("completed shard Markdown lacks findings/notes sections")
    section = markdown[findings_match.end() : notes_match.start()]
    headings = list(
        re.finditer(r"(?m)^#### \[(CRITICAL|HIGH|MEDIUM|LOW|NONE)\] [^\n]+$", section)
    )
    blocks = []
    for index, heading in enumerate(headings):
        end = headings[index + 1].start() if index + 1 < len(headings) else len(section)
        block = section[heading.start() : end].strip()
        location = re.search(r"(?m)^- \*\*Location\*\*: (.+)$", block)
        blocks.append(
            (
                -SEVERITY_RANK[heading.group(1)],
                shard_id,
                location.group(1) if location else "",
                block,
            )
        )
    return blocks


def load_case_metadata(
    corpus: dict[str, Any], manifest: dict[str, Any]
) -> dict[str, Any]:
    matches = [
        case for case in corpus.get("cases", []) if case.get("id") == manifest["case"]
    ]
    if len(matches) != 1:
        raise ValueError("trusted corpus does not contain exactly one matching case")
    case = matches[0]
    if (
        case.get("base") != manifest["base_sha"]
        or case.get("head") != manifest["head_sha"]
    ):
        raise ValueError("trusted corpus case does not match the reviewed range")
    required = {"pr", "purpose", "expected", "source-run", "source-comment"}
    if not required.issubset(case):
        raise ValueError("trusted corpus case is missing reporting metadata")
    return case


def aggregate(
    manifest: dict[str, Any],
    results: list[dict[str, Any]],
    case_metadata: dict[str, Any],
) -> tuple[dict[str, Any], str, dict[str, Any]]:
    validate_manifest(manifest)
    by_id = {result.get("shard_id"): result for result in results}
    if set(by_id) != {"shard-1", "shard-2"}:
        raise ValueError("expected exactly one result for each shard")

    for shard in manifest["shards"]:
        validate_result(by_id[shard["id"]], manifest, shard)

    findings: list[tuple[int, str, str, str]] = []
    notes = []
    risks = []
    incomplete = []
    for shard in manifest["shards"]:
        result = by_id[shard["id"]]
        notes.append(f"- `{shard['id']}`: {result['status']}")
        if result["status"] == "completed":
            review = result["review"]
            risks.append(review["overall_risk"])
            findings.extend(finding_blocks(review["review_markdown"], shard["id"]))
        elif result["status"] == "incomplete":
            incomplete.append((shard["id"], result["incomplete_reason"]))

    if incomplete:
        risks.append("HIGH")
        for shard_id, reason in incomplete:
            findings.append(
                (
                    -SEVERITY_RANK["HIGH"],
                    shard_id,
                    "",
                    "\n".join(
                        (
                            f"#### [HIGH] Automated review incomplete for {shard_id}",
                            "- **Category**: Reliability",
                            "- **Location**: [shard manifest](./codex-shard-manifest.json)",
                            f"- **Description**: The trusted shard result is incomplete (`{reason}`).",
                            "- **Impact**: Automated review cannot establish that this exact diff is safe.",
                            "- **Recommendation**: Require human review before merge.",
                        )
                    ),
                )
            )

    overall_risk = max(risks, key=SEVERITY_RANK.get) if risks else "NONE"
    ordered_findings = [entry[3] for entry in sorted(findings)]
    findings_markdown = (
        "\n\n".join(ordered_findings)
        if ordered_findings
        else "No concrete security, correctness, or reliability issues were found in the completed shards."
    )
    markdown = (
        "## Review Summary\n\n"
        f"**Overall Risk**: {overall_risk}\n\n"
        "### Findings\n\n"
        f"{findings_markdown}\n\n"
        "### Notes\n\n" + "\n".join(notes) + "\n"
    )
    review = {"overall_risk": overall_risk, "review_markdown": markdown}
    aggregate_result = {
        "benchmark_status": "incomplete" if incomplete else "completed",
        "action_outcome": "aggregate",
        "incomplete_reason": "one-or-more-shards-incomplete" if incomplete else None,
        "review": review,
    }
    scope = {
        "case": manifest["case"],
        "pull_request": int(case_metadata["pr"]),
        "base_sha": manifest["base_sha"],
        "head_sha": manifest["head_sha"],
        "commit_range": manifest["commit_range"],
        "purpose": case_metadata["purpose"],
        "adjudicated_result": case_metadata["expected"],
        "source_run": case_metadata["source-run"],
        "source_comment": case_metadata["source-comment"],
        "variant": manifest["variant"],
        "repeat": manifest["repeat"],
        "model": os.environ["CODEX_MODEL"],
        "reasoning_effort": os.environ["CODEX_REASONING_EFFORT"],
        "prompt_profile": os.environ["PROMPT_PROFILE"],
        "review_mode": "sharded",
        "manifest_digest": manifest["manifest_digest"],
        "limits": manifest["limits"],
        "planner_status": manifest["status"],
        "shards": [
            {
                "id": shard["id"],
                "status": by_id[shard["id"]]["status"],
                "incomplete_reason": by_id[shard["id"]]["incomplete_reason"],
                "primary_files": shard["primary_files"],
                "shared_files": shard["shared_files"],
                "packet_bytes": shard["packet_bytes"],
                "packet_lines": shard["packet_lines"],
                "elapsed_seconds": by_id[shard["id"]]["elapsed_seconds"],
            }
            for shard in manifest["shards"]
        ],
        "completed": not incomplete,
    }
    return aggregate_result, markdown, scope


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--corpus-file", type=Path, required=True)
    parser.add_argument("--shard-1-dir", type=Path, required=True)
    parser.add_argument("--shard-2-dir", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()

    directories = (args.shard_1_dir, args.shard_2_dir)
    manifests = [
        json.loads(
            (directory / "codex-shard-manifest.json").read_text(encoding="utf-8")
        )
        for directory in directories
    ]
    if manifests[0] != manifests[1]:
        raise SystemExit("shard result artifacts contain different manifests")
    results = [
        json.loads((directory / "shard-result.json").read_text(encoding="utf-8"))
        for directory in directories
    ]
    corpus = json.loads(args.corpus_file.read_text(encoding="utf-8"))
    case_metadata = load_case_metadata(corpus, manifests[0])
    aggregate_result, markdown, scope = aggregate(manifests[0], results, case_metadata)

    args.output_dir.mkdir(parents=True, exist_ok=True)
    (args.output_dir / "benchmark-review.json").write_text(
        json.dumps(aggregate_result, indent=2) + "\n", encoding="utf-8"
    )
    (args.output_dir / "benchmark-review.md").write_text(markdown, encoding="utf-8")
    (args.output_dir / "benchmark-scope.json").write_text(
        json.dumps(scope, indent=2) + "\n", encoding="utf-8"
    )
    (args.output_dir / "codex-shard-manifest.json").write_text(
        json.dumps(manifests[0], indent=2) + "\n", encoding="utf-8"
    )
    elapsed_values = [
        result["elapsed_seconds"]
        for result in results
        if isinstance(result.get("elapsed_seconds"), int)
    ]
    (args.output_dir / "benchmark-elapsed-time.txt").write_text(
        f"{max(elapsed_values)}\n" if elapsed_values else "unknown\n",
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
