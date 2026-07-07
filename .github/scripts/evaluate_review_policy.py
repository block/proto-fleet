#!/usr/bin/env python3
"""Evaluate whether a pull request satisfies Proto Fleet's review policy."""

from __future__ import annotations

import argparse
import fnmatch
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from typing import Any


API_VERSION = "2022-11-28"
BOT_SUFFIX = "[bot]"


class PolicyError(RuntimeError):
    pass


@dataclass
class PolicyResult:
    passed: bool
    decision: str
    reasons: list[str] = field(default_factory=list)
    low_risk_reasons: list[str] = field(default_factory=list)
    human_review_reasons: list[str] = field(default_factory=list)


def github_request(method: str, path: str, token: str, body: dict[str, Any] | None = None) -> Any:
    encoded = json.dumps(body).encode("utf-8") if body is not None else None
    request = urllib.request.Request(
        f"https://api.github.com{path}",
        data=encoded,
        method=method,
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "X-GitHub-Api-Version": API_VERSION,
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            content = response.read().decode("utf-8")
            if not content:
                return None
            return json.loads(content)
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", errors="replace")
        raise PolicyError(f"GitHub API {method} {path} failed: {error.code} {detail}") from error


def github_paginate(path: str, token: str) -> list[Any]:
    items: list[Any] = []
    separator = "&" if "?" in path else "?"
    page = 1
    while True:
        page_path = f"{path}{separator}per_page=100&page={page}"
        batch = github_request("GET", page_path, token)
        if not batch:
            return items
        if not isinstance(batch, list):
            raise PolicyError(f"Expected list response from {path}")
        items.extend(batch)
        if len(batch) < 100:
            return items
        page += 1


def github_paginate_key(path: str, token: str, key: str) -> list[Any]:
    items: list[Any] = []
    separator = "&" if "?" in path else "?"
    page = 1
    while True:
        page_path = f"{path}{separator}per_page=100&page={page}"
        response = github_request("GET", page_path, token)
        if not isinstance(response, dict) or key not in response:
            raise PolicyError(f"Expected object response with {key!r} from {path}")
        batch = response[key]
        if not batch:
            return items
        items.extend(batch)
        if len(batch) < 100:
            return items
        page += 1


def path_matches(path: str, pattern: str) -> bool:
    if fnmatch.fnmatchcase(path, pattern):
        return True
    if pattern.startswith("**/") and fnmatch.fnmatchcase(path, pattern[3:]):
        return True
    if pattern.endswith("/**"):
        prefix = pattern[:-3]
        return path == prefix or path.startswith(prefix + "/")
    return False


def denied_paths(files: list[dict[str, Any]], deny_patterns: list[str]) -> list[str]:
    denied: list[str] = []
    for item in files:
        path = item["filename"]
        if any(path_matches(path, pattern) for pattern in deny_patterns):
            denied.append(path)
    return sorted(set(denied))


def load_classifier(raw_output: str) -> dict[str, Any] | None:
    text = raw_output.strip()
    if not text:
        return None
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        pass

    fenced = re.search(r"```(?:json)?\s*(\{.*?\})\s*```", text, re.DOTALL)
    if fenced:
        return json.loads(fenced.group(1))

    start = text.find("{")
    end = text.rfind("}")
    if start != -1 and end > start:
        return json.loads(text[start : end + 1])
    raise PolicyError("AI classifier output did not contain a JSON object")


def classifier_allows_low_risk(classifier: dict[str, Any] | None, minimum_confidence: float) -> tuple[bool, list[str]]:
    if classifier is None:
        return False, ["AI classifier output is missing"]

    risk = str(classifier.get("risk", "")).lower()
    requires_human_review = bool(classifier.get("requires_human_review", True))
    try:
        confidence = float(classifier.get("confidence", 0))
    except (TypeError, ValueError):
        confidence = 0

    reasons = classifier.get("reasons", [])
    if not isinstance(reasons, list):
        reasons = [str(reasons)]

    blockers: list[str] = []
    if risk != "low":
        blockers.append(f"AI classifier risk is {risk or 'missing'}, not low")
    if requires_human_review:
        blockers.append("AI classifier requires human review")
    if confidence < minimum_confidence:
        blockers.append(f"AI classifier confidence {confidence:.2f} is below {minimum_confidence:.2f}")

    if blockers:
        return False, blockers
    return True, [str(reason) for reason in reasons]


def latest_reviews(reviews: list[dict[str, Any]]) -> dict[str, dict[str, Any]]:
    latest: dict[str, dict[str, Any]] = {}
    for review in reviews:
        user = review.get("user") or {}
        login = user.get("login")
        if not login:
            continue
        current = latest.get(login)
        if current is None or str(review.get("submitted_at") or "") >= str(current.get("submitted_at") or ""):
            latest[login] = review
    return latest


def human_review_state(
    reviews: list[dict[str, Any]],
    head_sha: str,
    author: str,
    minimum_approvals: int,
) -> tuple[bool, list[str], list[str]]:
    latest = latest_reviews(reviews)
    requested_changes = []
    approvals = []

    for login, review in latest.items():
        state = review.get("state")
        commit_id = review.get("commit_id")
        user_type = (review.get("user") or {}).get("type")
        is_bot = login.endswith(BOT_SUFFIX) or user_type == "Bot"
        if state == "CHANGES_REQUESTED":
            requested_changes.append(login)
        if state == "APPROVED" and commit_id == head_sha and login != author and not is_bot:
            approvals.append(login)

    blockers: list[str] = []
    if requested_changes:
        blockers.append(f"changes requested by {', '.join(sorted(requested_changes))}")
    if len(approvals) < minimum_approvals:
        blockers.append(f"{len(approvals)} current human approval(s), need {minimum_approvals}")

    reasons = [f"current human approvals: {', '.join(sorted(approvals)) or 'none'}"]
    return not blockers, reasons, blockers


def is_team_member(owner: str, team_slug: str, username: str, token: str) -> bool:
    quoted_user = urllib.parse.quote(username, safe="")
    quoted_team = urllib.parse.quote(team_slug, safe="")
    try:
        membership = github_request("GET", f"/orgs/{owner}/teams/{quoted_team}/memberships/{quoted_user}", token)
    except PolicyError as error:
        if " 403 " in str(error) or " 404 " in str(error):
            return False
        raise
    return membership.get("state") == "active"


def trusted_author_reasons(author: str, trusted_authors: list[str], owner: str, token: str) -> tuple[bool, list[str]]:
    for entry in trusted_authors:
        normalized = entry.removeprefix("@")
        if "/" in normalized:
            org, team_slug = normalized.split("/", 1)
            if org == owner and is_team_member(owner, team_slug, author, token):
                return True, [f"author @{author} is a member of @{entry.removeprefix('@')}"]
        elif normalized == author:
            return True, [f"author @{author} is explicitly trusted"]
    return False, [f"author @{author} is not in trusted_authors"]


def check_statuses(owner: str, repo: str, head_sha: str, required_checks: list[str], token: str) -> tuple[bool, list[str]]:
    runs = github_paginate_key(f"/repos/{owner}/{repo}/commits/{head_sha}/check-runs", token, "check_runs")
    latest_by_name: dict[str, dict[str, Any]] = {}
    for run in runs:
        name = run.get("name")
        if not name:
            continue
        current = latest_by_name.get(name)
        if current is None or str(run.get("started_at") or "") >= str(current.get("started_at") or ""):
            latest_by_name[name] = run

    blockers: list[str] = []
    for name in required_checks:
        run = latest_by_name.get(name)
        if run is None:
            blockers.append(f"required check {name!r} is missing")
            continue
        status = run.get("status")
        conclusion = run.get("conclusion")
        if status != "completed" or conclusion != "success":
            blockers.append(f"required check {name!r} is {status}/{conclusion}")
    return not blockers, blockers


def extract_security_risk(comments: list[dict[str, Any]], head_sha: str) -> tuple[str | None, list[str]]:
    marker = "<!-- codex-security-review -->"
    matching = [
        comment
        for comment in comments
        if comment.get("user", {}).get("login") == "github-actions[bot]" and marker in (comment.get("body") or "")
    ]
    if not matching:
        return None, ["Codex security review comment is missing"]

    comment = matching[-1]
    body = comment.get("body") or ""
    if head_sha not in body:
        return None, ["Codex security review comment is stale for this PR head"]

    risk_match = re.search(r"\*\*Overall Risk\*\*:\s*\[?([A-Z]+)\]?", body)
    if not risk_match:
        return None, ["Codex security review Overall Risk was not found"]
    return risk_match.group(1).upper(), []


def evaluate_policy(
    *,
    config: dict[str, Any],
    owner: str,
    repo: str,
    pr_number: int,
    author: str,
    head_sha: str,
    token: str,
    classifier_output: str,
) -> PolicyResult:
    files = github_paginate(f"/repos/{owner}/{repo}/pulls/{pr_number}/files", token)
    reviews = github_paginate(f"/repos/{owner}/{repo}/pulls/{pr_number}/reviews", token)
    comments = github_paginate(f"/repos/{owner}/{repo}/issues/{pr_number}/comments", token)

    human_ok, human_reasons, human_blockers = human_review_state(
        reviews,
        head_sha,
        author,
        int(config.get("minimum_human_approvals", 1)),
    )

    low_config = config["low_risk"]
    low_reasons: list[str] = []
    low_blockers: list[str] = []

    trusted, trust_reasons = trusted_author_reasons(author, config.get("trusted_authors", []), owner, token)
    (low_reasons if trusted else low_blockers).extend(trust_reasons)

    changed_files = len(files)
    total_changes = sum(int(item.get("additions", 0)) + int(item.get("deletions", 0)) for item in files)
    if changed_files > int(low_config["max_changed_files"]):
        low_blockers.append(f"{changed_files} changed files exceeds limit {low_config['max_changed_files']}")
    else:
        low_reasons.append(f"{changed_files} changed files within limit")
    if total_changes > int(low_config["max_total_changes"]):
        low_blockers.append(f"{total_changes} changed lines exceeds limit {low_config['max_total_changes']}")
    else:
        low_reasons.append(f"{total_changes} changed lines within limit")

    denied = denied_paths(files, low_config.get("deny_paths", []))
    if denied:
        low_blockers.append("denied paths changed: " + ", ".join(denied))
    else:
        low_reasons.append("no denied paths changed")

    checks_ok, check_blockers = check_statuses(owner, repo, head_sha, low_config.get("required_checks", []), token)
    if checks_ok:
        low_reasons.append("required checks are successful")
    else:
        low_blockers.extend(check_blockers)

    security_risk, security_blockers = extract_security_risk(comments, head_sha)
    allowed_security_risks = {risk.upper() for risk in low_config.get("allowed_security_risks", [])}
    if security_blockers:
        low_blockers.extend(security_blockers)
    elif security_risk in allowed_security_risks:
        low_reasons.append(f"Codex security review risk is {security_risk}")
    else:
        low_blockers.append(f"Codex security review risk is {security_risk}, not one of {sorted(allowed_security_risks)}")

    try:
        classifier = load_classifier(classifier_output)
        ai_ok, ai_reasons = classifier_allows_low_risk(classifier, float(low_config["minimum_ai_confidence"]))
    except (json.JSONDecodeError, PolicyError) as error:
        ai_ok, ai_reasons = False, [str(error)]
    if ai_ok:
        low_reasons.extend("AI: " + reason for reason in ai_reasons)
    else:
        low_blockers.extend(ai_reasons)

    if latest_reviews(reviews):
        latest = latest_reviews(reviews)
        changers = sorted(login for login, review in latest.items() if review.get("state") == "CHANGES_REQUESTED")
        if changers:
            low_blockers.append(f"changes requested by {', '.join(changers)}")

    if human_ok:
        return PolicyResult(True, "human-approved", [], low_reasons, human_reasons)

    if not low_blockers:
        return PolicyResult(True, "trusted-author-low-risk", [], low_reasons, human_reasons)

    return PolicyResult(
        False,
        "needs-human-review",
        human_blockers + low_blockers,
        low_reasons,
        human_reasons,
    )


def write_summary(result: PolicyResult) -> None:
    lines = [
        "## Review Policy",
        "",
        f"**Decision:** `{result.decision}`",
        f"**Status:** {'pass' if result.passed else 'fail'}",
        "",
    ]
    if result.low_risk_reasons:
        lines.extend(["### Low-risk path signals", ""])
        lines.extend(f"- {reason}" for reason in result.low_risk_reasons)
        lines.append("")
    if result.human_review_reasons:
        lines.extend(["### Human review signals", ""])
        lines.extend(f"- {reason}" for reason in result.human_review_reasons)
        lines.append("")
    if result.reasons:
        lines.extend(["### Blocking reasons", ""])
        lines.extend(f"- {reason}" for reason in result.reasons)
        lines.append("")

    summary = "\n".join(lines)
    summary_path = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary_path:
        with open(summary_path, "a", encoding="utf-8") as handle:
            handle.write(summary)
    print(summary)


def write_result(result: PolicyResult, path: str | None) -> None:
    if not path:
        return
    payload = {
        "passed": result.passed,
        "decision": result.decision,
        "reasons": result.reasons,
        "low_risk_reasons": result.low_risk_reasons,
        "human_review_reasons": result.human_review_reasons,
    }
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, indent=2)
        handle.write("\n")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--classifier-output", default="")
    parser.add_argument("--result-json")
    parser.add_argument("--owner", required=True)
    parser.add_argument("--repo", required=True)
    parser.add_argument("--pr-number", required=True, type=int)
    parser.add_argument("--author", required=True)
    parser.add_argument("--head-sha", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    token = os.environ.get("GITHUB_TOKEN")
    if not token:
        raise PolicyError("GITHUB_TOKEN is required")

    with open(args.config, encoding="utf-8") as handle:
        config = json.load(handle)

    result = evaluate_policy(
        config=config,
        owner=args.owner,
        repo=args.repo,
        pr_number=args.pr_number,
        author=args.author,
        head_sha=args.head_sha,
        token=token,
        classifier_output=args.classifier_output,
    )
    write_summary(result)
    write_result(result, args.result_json)
    return 0 if result.passed else 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except PolicyError as error:
        print(f"review-policy error: {error}", file=sys.stderr)
        raise SystemExit(1)
