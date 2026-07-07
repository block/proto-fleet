#!/usr/bin/env python3
"""Evaluate whether a pull request satisfies Proto Fleet's review policy."""

from __future__ import annotations

import argparse
import fnmatch
import io
import json
import math
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from dataclasses import dataclass, field
from typing import Any


API_VERSION = "2022-11-28"
BOT_SUFFIX = "[bot]"
AUTHORIZED_REVIEW_PERMISSIONS = {"admin", "maintain", "write"}


class PolicyError(RuntimeError):
    pass


@dataclass
class PolicyResult:
    passed: bool
    decision: str
    enforced: bool = True
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


def github_download(path: str, token: str) -> bytes:
    class DropAuthOnRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
            if redirected is None:
                return None
            old_host = urllib.parse.urlparse(req.full_url).netloc
            new_host = urllib.parse.urlparse(newurl).netloc
            if old_host != new_host:
                redirected.remove_header("Authorization")
            return redirected

    request = urllib.request.Request(
        f"https://api.github.com{path}",
        method="GET",
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "X-GitHub-Api-Version": API_VERSION,
        },
    )
    try:
        opener = urllib.request.build_opener(DropAuthOnRedirect)
        with opener.open(request, timeout=30) as response:
            return response.read()
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", errors="replace")
        raise PolicyError(f"GitHub API download {path} failed: {error.code} {detail}") from error


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
        classifier = json.loads(text)
    except json.JSONDecodeError as error:
        raise PolicyError("AI classifier output must be exactly one JSON object") from error
    if not isinstance(classifier, dict):
        raise PolicyError("AI classifier output must be a JSON object")
    return classifier


def classifier_allows_low_risk(classifier: dict[str, Any] | None, minimum_confidence: float) -> tuple[bool, list[str]]:
    if classifier is None:
        return False, ["AI classifier output is missing"]

    risk = classifier.get("risk")
    requires_human_review = classifier.get("requires_human_review")
    confidence = classifier.get("confidence")
    reasons = classifier.get("reasons", [])

    blockers: list[str] = []
    if risk not in {"low", "medium", "high"}:
        blockers.append(f"AI classifier risk is invalid: {risk!r}")
    elif risk != "low":
        blockers.append(f"AI classifier risk is {risk or 'missing'}, not low")
    if not isinstance(requires_human_review, bool):
        blockers.append("AI classifier requires_human_review must be boolean")
        requires_human_review = True
    if requires_human_review:
        blockers.append("AI classifier requires human review")
    if not isinstance(confidence, (int, float)) or isinstance(confidence, bool) or not math.isfinite(confidence):
        blockers.append("AI classifier confidence must be a finite number")
        confidence = 0
    elif confidence < 0 or confidence > 1:
        blockers.append(f"AI classifier confidence {confidence:.2f} is outside 0.00..1.00")
    if confidence < minimum_confidence:
        blockers.append(f"AI classifier confidence {confidence:.2f} is below {minimum_confidence:.2f}")
    if not isinstance(reasons, list) or not all(isinstance(reason, str) for reason in reasons):
        blockers.append("AI classifier reasons must be a list of strings")
        reasons = []

    if blockers:
        return False, blockers
    return True, reasons


def human_review_state(
    reviews: list[dict[str, Any]],
    head_sha: str,
    author: str,
    minimum_approvals: int,
    owner: str,
    repo: str,
    token: str,
) -> tuple[bool, list[str], list[str]]:
    reviewer_states: dict[str, dict[str, bool]] = {}
    ignored = []

    sorted_reviews = sorted(reviews, key=lambda item: str(item.get("submitted_at") or ""))
    for review in sorted_reviews:
        user = review.get("user") or {}
        login = user.get("login")
        if not login:
            continue
        state = review.get("state")
        user_type = user.get("type")
        is_bot = login.endswith(BOT_SUFFIX) or user_type == "Bot"
        if is_bot:
            continue

        authorized = reviewer_has_authority(owner, repo, login, review.get("author_association"), token)
        if state in {"APPROVED", "CHANGES_REQUESTED"} and not authorized:
            ignored.append(login)
            continue

        reviewer_state = reviewer_states.setdefault(login, {"approved": False, "changes_requested": False})
        if state == "DISMISSED":
            reviewer_state["approved"] = False
            reviewer_state["changes_requested"] = False
        elif state == "CHANGES_REQUESTED":
            reviewer_state["approved"] = False
            reviewer_state["changes_requested"] = True
        elif state == "APPROVED" and review.get("commit_id") == head_sha and login != author:
            reviewer_state["approved"] = True
            reviewer_state["changes_requested"] = False

    requested_changes = sorted(login for login, state in reviewer_states.items() if state["changes_requested"])
    approvals = sorted(login for login, state in reviewer_states.items() if state["approved"])

    blockers: list[str] = []
    if requested_changes:
        blockers.append(f"changes requested by {', '.join(sorted(requested_changes))}")
    if len(approvals) < minimum_approvals:
        blockers.append(f"{len(approvals)} current human approval(s), need {minimum_approvals}")

    reasons = [f"current authorized human approvals: {', '.join(sorted(approvals)) or 'none'}"]
    if ignored:
        reasons.append(f"ignored unauthorized review states from: {', '.join(sorted(set(ignored)))}")
    return not blockers, reasons, blockers


def reviewer_has_authority(owner: str, repo: str, username: str, association: str | None, token: str) -> bool:
    quoted_user = urllib.parse.quote(username, safe="")
    try:
        permission = github_request("GET", f"/repos/{owner}/{repo}/collaborators/{quoted_user}/permission", token)
    except PolicyError as error:
        if " 403 " in str(error) or " 404 " in str(error):
            return False
        raise
    return permission.get("permission") in AUTHORIZED_REVIEW_PERMISSIONS


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


def latest_check_runs(owner: str, repo: str, head_sha: str, token: str) -> dict[str, dict[str, Any]]:
    runs = github_paginate_key(f"/repos/{owner}/{repo}/commits/{head_sha}/check-runs", token, "check_runs")
    latest_by_name: dict[str, dict[str, Any]] = {}
    for run in runs:
        name = run.get("name")
        if not name:
            continue
        current = latest_by_name.get(name)
        if current is None or str(run.get("started_at") or "") >= str(current.get("started_at") or ""):
            latest_by_name[name] = run
    return latest_by_name


def check_statuses(owner: str, repo: str, head_sha: str, required_checks: list[str], token: str) -> tuple[bool, list[str]]:
    latest_by_name = latest_check_runs(owner, repo, head_sha, token)
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


def extract_run_id(details_url: str | None) -> str | None:
    if not details_url:
        return None
    marker = "/actions/runs/"
    if marker not in details_url:
        return None
    tail = details_url.split(marker, 1)[1]
    run_id = tail.split("/", 1)[0]
    return run_id if run_id.isdigit() else None


def extract_security_risk(owner: str, repo: str, head_sha: str, token: str) -> tuple[str | None, list[str]]:
    security_run = latest_check_runs(owner, repo, head_sha, token).get("security-review")
    if not security_run:
        return None, ["Codex security-review check is missing"]

    run_id = extract_run_id(security_run.get("details_url") or security_run.get("html_url"))
    if not run_id:
        return None, ["Codex security-review run id was not found"]

    artifacts = github_paginate_key(f"/repos/{owner}/{repo}/actions/runs/{run_id}/artifacts", token, "artifacts")
    artifact = next(
        (
            item
            for item in artifacts
            if item.get("name") == "codex-security-review-result" and not item.get("expired", False)
        ),
        None,
    )
    if artifact is None:
        return None, ["Codex security-review result artifact is missing"]

    archive = github_download(
        f"/repos/{owner}/{repo}/actions/artifacts/{artifact['id']}/zip",
        token,
    )
    with zipfile.ZipFile(io.BytesIO(archive)) as archive_file:
        try:
            with archive_file.open("codex-security-review-result.json") as result_file:
                result = json.loads(result_file.read().decode("utf-8"))
        except KeyError as error:
            raise PolicyError("Codex security-review result artifact did not contain JSON") from error

    if result.get("head_sha") != head_sha:
        return None, ["Codex security-review result artifact is stale for this PR head"]
    if str(result.get("run_id")) != str(run_id):
        return None, ["Codex security-review result artifact does not match the workflow run"]
    risk = str(result.get("overall_risk", "")).upper()
    if not risk:
        return None, ["Codex security-review result artifact is missing overall_risk"]
    return risk, []


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

    human_ok, human_reasons, human_blockers = human_review_state(
        reviews,
        head_sha,
        author,
        int(config.get("minimum_human_approvals", 1)),
        owner,
        repo,
        token,
    )

    low_config = config["low_risk"]
    low_reasons: list[str] = []
    low_blockers: list[str] = []
    low_blockers.extend(blocker for blocker in human_blockers if blocker.startswith("changes requested by "))

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

    security_risk, security_blockers = extract_security_risk(owner, repo, head_sha, token)
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

    if human_ok:
        return PolicyResult(
            passed=True,
            decision="human-approved",
            reasons=[],
            low_risk_reasons=low_reasons,
            human_review_reasons=human_reasons,
        )

    if not low_blockers:
        return PolicyResult(
            passed=True,
            decision="trusted-author-low-risk",
            reasons=[],
            low_risk_reasons=low_reasons,
            human_review_reasons=human_reasons,
        )

    return PolicyResult(
        passed=False,
        decision="needs-human-review",
        reasons=human_blockers + low_blockers,
        low_risk_reasons=low_reasons,
        human_review_reasons=human_reasons,
    )


def write_summary(result: PolicyResult) -> None:
    lines = [
        "## Review Policy",
        "",
        f"**Decision:** `{result.decision}`",
        f"**Status:** {'pass' if result.passed else 'fail'}",
        f"**Mode:** {'enforced' if result.enforced else 'advisory'}",
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
        "enforced": result.enforced,
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

    enforced = bool(config.get("enforce", True))
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
    result.enforced = enforced
    write_summary(result)
    write_result(result, args.result_json)
    return 0 if result.passed or not enforced else 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except PolicyError as error:
        print(f"review-policy error: {error}", file=sys.stderr)
        raise SystemExit(1)
