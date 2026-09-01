#!/usr/bin/env python3
"""Validate one Codex shard result and bind it to its trusted plan."""

from __future__ import annotations

import argparse
import json
import os
import re
import time
from pathlib import Path
from typing import Any
from urllib.parse import quote

SEVERITY_RANK = {"NONE": 0, "LOW": 1, "MEDIUM": 2, "HIGH": 3, "CRITICAL": 4}


def validate_review_markdown(
    markdown: str,
    risk: str,
    *,
    allowed_line_ranges: dict[str, list[list[int]]] | None = None,
    blob_base_url: str | None = None,
    blob_base_urls: dict[str, str] | None = None,
) -> None:
    section_names = ("## Review Summary", "### Findings", "### Notes")
    section_positions = []
    for section in section_names:
        matches = list(re.finditer(rf"(?m)^{re.escape(section)}[ \t]*$", markdown))
        if len(matches) != 1:
            raise ValueError(
                f"review Markdown must contain exactly one {section} section"
            )
        section_positions.append(matches[0].start())
    if section_positions != sorted(section_positions):
        raise ValueError("review Markdown sections are out of order")

    summary_risks = re.findall(
        r"(?m)^\*\*Overall Risk\*\*: (CRITICAL|HIGH|MEDIUM|LOW|NONE)[ \t]*$",
        markdown,
    )
    if summary_risks != [risk]:
        raise ValueError("model output risk disagrees with review summary")

    exact_findings = list(
        re.finditer(r"(?m)^#### \[(CRITICAL|HIGH|MEDIUM|LOW|NONE)\] [^\n]+$", markdown)
    )
    finding_like = list(
        re.finditer(
            r"(?mi)^[ \t]{0,3}#{1,6}[^\n]*\b(CRITICAL|HIGH|MEDIUM|LOW|NONE)\b[^\n]*$",
            markdown,
        )
    )
    if len(finding_like) != len(exact_findings) or any(
        broad.start() != exact.start()
        for broad, exact in zip(finding_like, exact_findings, strict=True)
    ):
        raise ValueError("review Markdown contains an unparseable severity heading")
    findings_start = section_positions[1]
    notes_start = section_positions[2]
    if any(
        finding.start() <= findings_start or finding.start() >= notes_start
        for finding in exact_findings
    ):
        raise ValueError(
            "review Markdown contains a finding outside the Findings section"
        )
    findings_section = markdown[findings_start:notes_start]
    section_headings = re.findall(r"(?m)^[ \t]{0,3}#{1,6}[ \t]+\S.*$", findings_section)
    if any(
        re.fullmatch(r"#### \[(CRITICAL|HIGH|MEDIUM|LOW|NONE)\] [^\n]+", heading)
        is None
        for heading in section_headings[1:]
    ):
        raise ValueError("review Markdown contains an invalid finding heading")
    if risk == "NONE" and exact_findings:
        raise ValueError("NONE review Markdown must not contain findings")
    if risk != "NONE" and not exact_findings:
        raise ValueError("non-NONE review Markdown must contain a finding")

    required_fields = (
        r"(?m)^- \*\*Category\*\*: \S.*$",
        r"(?m)^- \*\*Location\*\*: \[[^\]]+\]\([^\)]+\)[ \t]*$",
        r"(?m)^- \*\*Description\*\*: \S.*$",
        r"(?m)^- \*\*Impact\*\*: \S.*$",
        r"(?m)^- \*\*Recommendation\*\*: \S.*$",
    )
    for index, finding in enumerate(exact_findings):
        end = (
            exact_findings[index + 1].start()
            if index + 1 < len(exact_findings)
            else notes_start
        )
        block = markdown[finding.start() : end]
        if not all(re.search(pattern, block) for pattern in required_fields):
            raise ValueError("review Markdown finding is missing required fields")
        if allowed_line_ranges is not None:
            location = re.search(
                r"(?m)^- \*\*Location\*\*: \[`(.+):([1-9][0-9]*)`\]\(([^)\n]+)\)[ \t]*$",
                block,
            )
            if location is None:
                raise ValueError("review Markdown finding has an invalid location")
            path, line_text, url = location.groups()
            line = int(line_text)
            ranges = allowed_line_ranges.get(path)
            if ranges is None:
                raise ValueError("review Markdown finding is outside the shard packet")
            if not any(start <= line <= end for start, end in ranges):
                raise ValueError(
                    "review Markdown finding is outside changed hunk lines"
                )
            expected_base_url = (
                blob_base_urls.get(path)
                if blob_base_urls is not None
                else blob_base_url
            )
            if expected_base_url is not None:
                expected_url = f"{expected_base_url}/{quote(path, safe='/')}#L{line}"
                if url != expected_url:
                    raise ValueError(
                        "review Markdown finding has an invalid location URL"
                    )
        if SEVERITY_RANK[finding.group(1)] > SEVERITY_RANK[risk]:
            raise ValueError("model output risk is lower than a reported finding")


def parse_review(
    raw: str,
    allowed_line_ranges: dict[str, list[list[int]]],
    blob_base_urls: dict[str, str] | None,
) -> tuple[dict[str, str] | None, str | None]:
    if not raw.strip():
        return None, "empty-model-output"
    try:
        candidate: Any = json.loads(raw)
    except (json.JSONDecodeError, TypeError):
        return None, "invalid-model-output"
    if not (
        isinstance(candidate, dict)
        and set(candidate) == {"overall_risk", "review_markdown"}
        and candidate.get("overall_risk") in SEVERITY_RANK
        and isinstance(candidate.get("review_markdown"), str)
        and candidate["review_markdown"].strip()
    ):
        return None, "invalid-model-output"
    try:
        validate_review_markdown(
            candidate["review_markdown"],
            candidate["overall_risk"],
            allowed_line_ranges=allowed_line_ranges,
            blob_base_urls=blob_base_urls,
        )
    except ValueError:
        return None, "invalid-model-output"
    return candidate, None


def review_location_contract(
    manifest: dict[str, Any], packet_paths: set[str], repository: str | None
) -> tuple[dict[str, list[list[int]]], dict[str, str] | None]:
    records = [file for file in manifest["files"] if file["path"] in packet_paths]
    allowed_line_ranges = {
        file["path"]: file["changed_line_ranges"] for file in records
    }
    if repository is None:
        return allowed_line_ranges, None
    blob_base_urls = {}
    for file in records:
        citation_sha = manifest[f"{file['citation_side']}_sha"]
        blob_base_urls[file["path"]] = (
            f"https://github.com/{repository}/blob/{citation_sha}"
        )
    return allowed_line_ranges, blob_base_urls


def build_result(
    manifest: dict[str, Any],
    shard_id: str,
    outcome: str,
    raw: str,
    elapsed: int | None,
    forced_reason: str | None = None,
) -> tuple[dict[str, Any], str]:
    shard = next((item for item in manifest["shards"] if item["id"] == shard_id), None)
    if shard is None:
        raise ValueError(f"manifest has no shard {shard_id}")

    review = None
    reason = forced_reason
    status = "incomplete" if forced_reason else "inactive"
    if forced_reason is None and manifest["status"] == "oversized":
        if shard_id == "shard-1":
            status = "incomplete"
            reason = "oversized-review"
    elif forced_reason is None and not shard["active"]:
        status = "inactive"
    elif forced_reason is None:
        if outcome != "success":
            raise ValueError(
                f"Codex action ended unexpectedly before the outer budget: {outcome}"
            )
        timeout_minutes = int(os.environ.get("CODEX_TIMEOUT_MINUTES", "0"))
        if elapsed is not None and timeout_minutes and elapsed > timeout_minutes * 60:
            status = "incomplete"
            reason = "codex-budget-exceeded"
        else:
            packet_paths = set(shard["primary_files"] + shard["shared_files"])
            allowed_line_ranges, blob_base_urls = review_location_contract(
                manifest, packet_paths, os.environ.get("GITHUB_REPOSITORY")
            )
            review, reason = parse_review(raw, allowed_line_ranges, blob_base_urls)
            status = "completed" if review else "incomplete"

    result = {
        "schema_version": 1,
        "case": manifest["case"],
        "variant": manifest["variant"],
        "repeat": manifest["repeat"],
        "base_sha": manifest["base_sha"],
        "head_sha": manifest["head_sha"],
        "commit_range": manifest["commit_range"],
        "manifest_digest": manifest["manifest_digest"],
        "run_id": int(os.environ.get("GITHUB_RUN_ID", "0")),
        "run_attempt": int(os.environ.get("GITHUB_RUN_ATTEMPT", "0")),
        "shard_id": shard_id,
        "status": status,
        "incomplete_reason": reason,
        "elapsed_seconds": elapsed,
        "primary_files": shard["primary_files"],
        "shared_files": shard["shared_files"],
        "packet_bytes": shard["packet_bytes"],
        "packet_lines": shard["packet_lines"],
        "review": review,
    }
    if review:
        markdown = review["review_markdown"]
    elif status == "inactive":
        markdown = "## Inactive benchmark shard\n\nThis review-wide plan did not require this shard.\n"
    else:
        markdown = (
            "## Benchmark shard incomplete\n\n"
            f"Shard `{shard_id}` produced no usable result (reason: `{reason}`).\n"
        )
    return result, markdown


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--shard", choices=("shard-1", "shard-2"), required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--started-at", type=int, default=0)
    parser.add_argument("--elapsed-seconds", type=int)
    parser.add_argument("--force-incomplete-reason")
    args = parser.parse_args()

    manifest = json.loads(args.manifest.read_text(encoding="utf-8"))
    started_at = args.started_at or int(time.time())
    elapsed = (
        None
        if args.elapsed_seconds is not None and args.elapsed_seconds < 0
        else (
            args.elapsed_seconds
            if args.elapsed_seconds is not None
            else max(0, int(time.time()) - started_at)
        )
    )
    result, markdown = build_result(
        manifest,
        args.shard,
        os.environ.get("CODEX_OUTCOME", ""),
        os.environ.get("REVIEW_OUTPUT", ""),
        elapsed,
        args.force_incomplete_reason,
    )

    args.output_dir.mkdir(parents=True, exist_ok=True)
    (args.output_dir / "shard-result.json").write_text(
        json.dumps(result, indent=2) + "\n", encoding="utf-8"
    )
    (args.output_dir / "shard-review.md").write_text(markdown, encoding="utf-8")
    (args.output_dir / "codex-shard-manifest.json").write_text(
        json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
    )


if __name__ == "__main__":
    main()
