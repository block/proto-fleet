#!/usr/bin/env python3
"""Evaluate whether a pull request satisfies Proto Fleet's review policy."""

from __future__ import annotations

import argparse
import fnmatch
import io
import json
import math
import os
import re
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
SECURITY_RISK_LEVELS = {"NONE", "LOW", "MEDIUM", "HIGH", "CRITICAL"}
INACCESSIBLE_HTTP_STATUSES = {403, 404}


class PolicyError(RuntimeError):
    def __init__(self, message: str, status_code: int | None = None):
        super().__init__(message)
        self.status_code = status_code


def normalize_login(login: str) -> str:
    return login.removeprefix("@").casefold()


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
        raise PolicyError(f"GitHub API {method} {path} failed: {error.code} {detail}", error.code) from error


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
        raise PolicyError(f"GitHub API download {path} failed: {error.code} {detail}", error.code) from error


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
        paths = [item.get("filename"), item.get("previous_filename")]
        for path in (value for value in paths if value):
            if any(path_matches(path, pattern) for pattern in deny_patterns):
                denied.append(path)
    return sorted(set(denied))


def deterministic_content_blockers(files: list[dict[str, Any]], low_config: dict[str, Any]) -> list[str]:
    blockers: list[str] = []
    max_file_changes = int(low_config.get("max_file_changes", 0) or 0)
    pattern_rules = low_config.get("content_deny_added_patterns", [])

    compiled_rules: list[tuple[re.Pattern[str], str]] = []
    for rule in pattern_rules:
        if not isinstance(rule, dict):
            continue
        pattern = rule.get("pattern")
        reason = rule.get("reason")
        if isinstance(pattern, str) and isinstance(reason, str):
            compiled_rules.append((re.compile(pattern), reason))

    for item in files:
        path = item.get("filename", "<unknown>")
        additions = int(item.get("additions", 0))
        deletions = int(item.get("deletions", 0))
        changed_lines = additions + deletions
        if max_file_changes and changed_lines > max_file_changes:
            blockers.append(f"{path} has {changed_lines} changed lines, exceeds per-file limit {max_file_changes}")

        patch = item.get("patch")
        if patch is None:
            blockers.append(f"{path} diff content is unavailable for deterministic content checks")
            continue

        matched_reasons: set[str] = set()
        for line in str(patch or "").splitlines():
            if not line.startswith("+") or line.startswith("+++"):
                continue
            added_line = line[1:]
            for pattern, reason in compiled_rules:
                if reason not in matched_reasons and pattern.search(added_line):
                    blockers.append(f"{path} adds blocked content: {reason}")
                    matched_reasons.add(reason)

    return sorted(set(blockers))


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
    ineligible_reviewers: set[str] | None = None,
) -> tuple[bool, list[str], list[str]]:
    reviewer_states: dict[str, dict[str, bool]] = {}
    reviewer_display_names: dict[str, str] = {}
    authority_cache: dict[str, bool] = {}
    ignored = []
    ignored_contributors = []
    ineligible_approvers = {normalize_login(author)}
    ineligible_approvers.update(normalize_login(login) for login in (ineligible_reviewers or set()))

    def has_authority(login: str) -> bool:
        key = normalize_login(login)
        if key not in authority_cache:
            authority_cache[key] = reviewer_has_authority(owner, repo, login, token)
        return authority_cache[key]

    sorted_reviews = sorted(reviews, key=lambda item: str(item.get("submitted_at") or ""))
    for review in sorted_reviews:
        user = review.get("user") or {}
        login = user.get("login")
        if not login:
            continue
        state = review.get("state")
        user_type = user.get("type")
        is_bot = login.casefold().endswith(BOT_SUFFIX) or user_type == "Bot"
        if is_bot:
            continue

        if state in {"APPROVED", "CHANGES_REQUESTED"} and not has_authority(login):
            ignored.append(login)
            continue

        reviewer_key = normalize_login(login)
        reviewer_display_names.setdefault(reviewer_key, login)
        reviewer_state = reviewer_states.setdefault(reviewer_key, {"approved": False, "changes_requested": False})
        if state == "DISMISSED":
            reviewer_state["approved"] = False
            reviewer_state["changes_requested"] = False
        elif state == "CHANGES_REQUESTED":
            reviewer_state["approved"] = False
            reviewer_state["changes_requested"] = True
        elif state == "APPROVED" and review.get("commit_id") == head_sha:
            if reviewer_key in ineligible_approvers:
                ignored_contributors.append(login)
                continue
            reviewer_state["approved"] = True
            reviewer_state["changes_requested"] = False

    requested_changes = sorted(
        reviewer_display_names[login] for login, state in reviewer_states.items() if state["changes_requested"]
    )
    approvals = sorted(reviewer_display_names[login] for login, state in reviewer_states.items() if state["approved"])

    blockers: list[str] = []
    if requested_changes:
        blockers.append(f"changes requested by {', '.join(sorted(requested_changes))}")
    if len(approvals) < minimum_approvals:
        blockers.append(f"{len(approvals)} current human approval(s), need {minimum_approvals}")

    reasons = [f"current authorized human approvals: {', '.join(sorted(approvals)) or 'none'}"]
    if ignored:
        reasons.append(f"ignored unauthorized review states from: {', '.join(sorted(set(ignored)))}")
    if ignored_contributors:
        reasons.append(f"ignored approvals from PR contributors: {', '.join(sorted(set(ignored_contributors)))}")
    return not blockers, reasons, blockers


def reviewer_has_authority(owner: str, repo: str, username: str, token: str) -> bool:
    quoted_user = urllib.parse.quote(username, safe="")
    try:
        permission = github_request("GET", f"/repos/{owner}/{repo}/collaborators/{quoted_user}/permission", token)
    except PolicyError as error:
        if error.status_code in INACCESSIBLE_HTTP_STATUSES:
            return False
        raise
    return permission.get("permission") in AUTHORIZED_REVIEW_PERMISSIONS


def is_team_member(owner: str, team_slug: str, username: str, token: str) -> bool:
    quoted_user = urllib.parse.quote(username, safe="")
    quoted_team = urllib.parse.quote(team_slug, safe="")
    try:
        membership = github_request("GET", f"/orgs/{owner}/teams/{quoted_team}/memberships/{quoted_user}", token)
    except PolicyError as error:
        if error.status_code in INACCESSIBLE_HTTP_STATUSES:
            return False
        raise
    return membership.get("state") == "active"


def trusted_author_reasons(author: str, trusted_authors: list[str], owner: str, token: str) -> tuple[bool, list[str]]:
    normalized_author = normalize_login(author)
    normalized_owner = normalize_login(owner)
    for entry in trusted_authors:
        normalized = entry.removeprefix("@")
        if "/" in normalized:
            org, team_slug = normalized.split("/", 1)
            if normalize_login(org) == normalized_owner and is_team_member(owner, team_slug, author, token):
                return True, [f"author @{author} is a member of @{entry.removeprefix('@')}"]
        elif normalize_login(normalized) == normalized_author:
            return True, [f"author @{author} is explicitly trusted"]
    return False, [f"author @{author} is not in trusted_authors"]


def trusted_head_contributor_reasons(
    commits: list[dict[str, Any]],
    trusted_authors: list[str],
    owner: str,
    token: str,
) -> tuple[bool, list[str], list[str]]:
    contributors, unknown_commits = head_contributors(commits)
    reasons: list[str] = []
    blockers: list[str] = []

    if unknown_commits:
        blockers.append("current head has commits without GitHub-linked authors or committers: " + ", ".join(unknown_commits))
    if not contributors:
        blockers.append("current head has no GitHub-linked commit authors or committers")

    for contributor in sorted(contributors):
        trusted, _trust_reasons = trusted_author_reasons(contributor, trusted_authors, owner, token)
        if trusted:
            reasons.append(f"head contributor @{contributor} is trusted")
        else:
            blockers.append(f"head contributor @{contributor} is not in trusted_authors")

    return not blockers, reasons, blockers


def workflow_actor_for_head(
    owner: str,
    repo: str,
    head_sha: str,
    actor_config: dict[str, Any],
    token: str,
) -> tuple[str | None, list[str]]:
    workflow_path = actor_config.get("workflow_path") if isinstance(actor_config, dict) else None
    event = actor_config.get("event") if isinstance(actor_config, dict) else None
    if not isinstance(workflow_path, str) or not workflow_path:
        return None, ["trusted actor workflow config is missing workflow_path"]
    if not isinstance(event, str) or not event:
        return None, ["trusted actor workflow config is missing event"]

    workflow_run = latest_workflow_runs(owner, repo, head_sha, event, token).get(workflow_path)
    if workflow_run is None:
        return None, [f"trusted actor workflow {workflow_path!r} is missing for this PR head"]
    actor = workflow_run.get("actor") or {}
    login = actor.get("login") if isinstance(actor, dict) else None
    if not login:
        return None, [f"trusted actor workflow {workflow_path!r} is missing an authenticated actor"]
    return login, []


def trusted_workflow_actor_reasons(
    owner: str,
    repo: str,
    head_sha: str,
    trusted_authors: list[str],
    actor_config: dict[str, Any],
    token: str,
) -> tuple[bool, list[str], list[str]]:
    login, blockers = workflow_actor_for_head(owner, repo, head_sha, actor_config, token)
    if blockers or not login:
        return False, [], blockers

    trusted, _trust_reasons = trusted_author_reasons(login, trusted_authors, owner, token)
    if trusted:
        return True, [f"authenticated workflow actor @{login} is trusted"], []
    return False, [], [f"authenticated workflow actor @{login} is not in trusted_authors"]


def head_contributors(commits: list[dict[str, Any]]) -> tuple[set[str], list[str]]:
    contributors: set[str] = set()
    unknown_commits: list[str] = []

    for commit in commits:
        sha = str(commit.get("sha") or "")[:12] or "<unknown>"
        linked = False
        for role in ("author", "committer"):
            user = commit.get(role)
            login = user.get("login") if isinstance(user, dict) else None
            if login:
                contributors.add(login)
                linked = True
        if not linked:
            unknown_commits.append(sha)

    return contributors, unknown_commits


def shared_head_pr_blockers(owner: str, repo: str, pr_number: int, head_sha: str, token: str) -> list[str]:
    pulls = github_paginate(f"/repos/{owner}/{repo}/commits/{head_sha}/pulls", token)
    open_pr_numbers = sorted(
        int(item["number"])
        for item in pulls
        if item.get("state") == "open"
        and (item.get("head") or {}).get("sha") == head_sha
        and item.get("number") is not None
    )
    if len(open_pr_numbers) <= 1:
        return []
    if pr_number not in open_pr_numbers:
        return [f"current head SHA is shared by open PRs: {', '.join(f'#{number}' for number in open_pr_numbers)}"]
    return [
        "current head SHA is shared by multiple open PRs: "
        + ", ".join(f"#{number}" for number in open_pr_numbers)
    ]


def current_pr_head_blockers(
    owner: str,
    repo: str,
    pr_number: int,
    head_sha: str,
    token: str,
    base_sha: str | None = None,
) -> list[str]:
    pull = github_request("GET", f"/repos/{owner}/{repo}/pulls/{pr_number}", token)
    if not isinstance(pull, dict):
        return [f"pull request #{pr_number} could not be loaded"]
    if pull.get("state") != "open":
        return [f"pull request #{pr_number} is not open"]

    current_head = ((pull.get("head") or {}).get("sha"))
    if current_head != head_sha:
        return [f"pull request #{pr_number} head is {current_head}, expected {head_sha}"]

    if base_sha is not None:
        current_base = ((pull.get("base") or {}).get("sha"))
        if current_base != base_sha:
            return [f"pull request #{pr_number} base is {current_base}, expected {base_sha}"]

    return []


def evaluate_low_risk_preflight(
    *,
    config: dict[str, Any],
    owner: str,
    repo: str,
    pr_number: int,
    author: str,
    head_sha: str,
    token: str,
) -> dict[str, Any]:
    low_config = config["low_risk"]
    reasons: list[str] = []
    blockers: list[str] = []

    blockers.extend(current_pr_head_blockers(owner, repo, pr_number, head_sha, token))
    if blockers:
        return {
            "eligible": False,
            "reasons": reasons,
            "blockers": blockers,
        }

    files = github_paginate(f"/repos/{owner}/{repo}/pulls/{pr_number}/files", token)
    commits = github_paginate(f"/repos/{owner}/{repo}/pulls/{pr_number}/commits", token)
    blockers.extend(current_pr_head_blockers(owner, repo, pr_number, head_sha, token))
    if blockers:
        return {
            "eligible": False,
            "reasons": reasons,
            "blockers": blockers,
        }

    blockers.extend(shared_head_pr_blockers(owner, repo, pr_number, head_sha, token))

    trusted, trust_reasons = trusted_author_reasons(author, config.get("trusted_authors", []), owner, token)
    (reasons if trusted else blockers).extend(trust_reasons)
    head_trusted, head_trust_reasons, head_trust_blockers = trusted_head_contributor_reasons(
        commits,
        config.get("trusted_authors", []),
        owner,
        token,
    )
    (reasons if head_trusted else blockers).extend(head_trust_reasons if head_trusted else head_trust_blockers)
    actor_login, actor_blockers = workflow_actor_for_head(
        owner, repo, head_sha, low_config.get("trusted_actor_workflow", {}), token
    )
    if actor_login:
        actor_trusted, _trust_reasons = trusted_author_reasons(
            actor_login, config.get("trusted_authors", []), owner, token
        )
        if actor_trusted:
            reasons.append(f"authenticated workflow actor @{actor_login} is trusted")
        else:
            blockers.append(f"authenticated workflow actor @{actor_login} is not in trusted_authors")
    else:
        blockers.extend(actor_blockers)

    changed_files = len(files)
    total_changes = sum(int(item.get("additions", 0)) + int(item.get("deletions", 0)) for item in files)
    if changed_files > int(low_config["max_changed_files"]):
        blockers.append(f"{changed_files} changed files exceeds limit {low_config['max_changed_files']}")
    else:
        reasons.append(f"{changed_files} changed files within limit")
    if total_changes > int(low_config["max_total_changes"]):
        blockers.append(f"{total_changes} changed lines exceeds limit {low_config['max_total_changes']}")
    else:
        reasons.append(f"{total_changes} changed lines within limit")

    denied = denied_paths(files, low_config.get("deny_paths", []))
    if denied:
        blockers.append("denied paths changed: " + ", ".join(denied))
    else:
        reasons.append("no denied paths changed")

    content_blockers = deterministic_content_blockers(files, low_config)
    if content_blockers:
        blockers.extend(content_blockers)
    else:
        reasons.append("deterministic content checks passed")

    return {
        "eligible": not blockers,
        "reasons": reasons,
        "blockers": blockers,
    }


def latest_check_runs(owner: str, repo: str, head_sha: str, token: str) -> dict[str, dict[str, Any]]:
    runs = github_paginate_key(f"/repos/{owner}/{repo}/commits/{head_sha}/check-runs", token, "check_runs")
    latest_by_name: dict[str, dict[str, Any]] = {}
    for run in runs:
        name = run.get("name")
        if not name:
            continue
        current = latest_by_name.get(name)
        run_key = (str(run.get("started_at") or ""), int(run.get("id", 0) or 0))
        current_key = (str(current.get("started_at") or ""), int(current.get("id", 0) or 0)) if current else ("", 0)
        if current is None or run_key > current_key:
            latest_by_name[name] = run
    return latest_by_name


def check_statuses(
    owner: str,
    repo: str,
    head_sha: str,
    required_checks: list[Any],
    token: str,
    latest_by_name: dict[str, dict[str, Any]] | None = None,
) -> tuple[bool, list[str]]:
    if latest_by_name is None:
        latest_by_name = latest_check_runs(owner, repo, head_sha, token)
    needs_commit_statuses = any(
        isinstance(requirement, dict) and requirement.get("type") == "commit_status" for requirement in required_checks
    )
    latest_by_context = latest_commit_statuses(owner, repo, head_sha, token) if needs_commit_statuses else {}
    blockers: list[str] = []
    for requirement in required_checks:
        if isinstance(requirement, str):
            blockers.append(f"required check {requirement!r} uses legacy unvalidated config")
            continue
        if not isinstance(requirement, dict):
            blockers.append(f"required check config is invalid: {requirement!r}")
            continue

        name = requirement.get("name")
        kind = requirement.get("type")
        if not isinstance(name, str) or not name:
            blockers.append(f"required check config is missing a valid name: {requirement!r}")
            continue
        if kind == "github_actions":
            blocker = github_actions_check_blocker(owner, repo, head_sha, latest_by_name.get(name), requirement, token)
            if blocker:
                blockers.append(blocker)
        elif kind == "github_actions_workflow":
            blocker = github_actions_workflow_blocker(owner, repo, head_sha, requirement, token)
            if blocker:
                blockers.append(blocker)
        elif kind == "check_run":
            blocker = check_run_blocker(latest_by_name.get(name), requirement)
            if blocker:
                blockers.append(blocker)
        elif kind == "commit_status":
            blocker = commit_status_blocker(latest_by_context.get(name), requirement)
            if blocker:
                blockers.append(blocker)
        else:
            blockers.append(f"required check {name!r} has unsupported type {kind!r}")
    return not blockers, blockers


def check_run_blocker(run: dict[str, Any] | None, requirement: dict[str, Any]) -> str | None:
    name = str(requirement["name"])
    expected_app_slug = requirement.get("app_slug")
    if not isinstance(expected_app_slug, str) or not expected_app_slug:
        return f"required check {name!r} is missing trusted app_slug"
    if run is None:
        return f"required check {name!r} is missing"
    app = run.get("app") or {}
    if app.get("slug") != expected_app_slug:
        return f"required check {name!r} app is {app.get('slug')!r}, expected {expected_app_slug!r}"
    status = run.get("status")
    conclusion = run.get("conclusion")
    if status != "completed" or conclusion != "success":
        return f"required check {name!r} is {status}/{conclusion}"
    return None


def github_actions_check_blocker(
    owner: str,
    repo: str,
    head_sha: str,
    run: dict[str, Any] | None,
    requirement: dict[str, Any],
    token: str,
) -> str | None:
    name = str(requirement["name"])
    expected_path = requirement.get("workflow_path")
    if not isinstance(expected_path, str) or not expected_path:
        return f"required check {name!r} is missing trusted workflow_path"
    expected_event = requirement.get("event")
    if not isinstance(expected_event, str) or not expected_event:
        return f"required check {name!r} is missing trusted event"

    base_blocker = check_run_blocker(run, {**requirement, "app_slug": "github-actions"})
    if base_blocker:
        return base_blocker

    run_id = extract_run_id(run.get("details_url") or run.get("html_url"))
    if not run_id:
        return f"required check {name!r} workflow run id was not found"

    workflow_run = github_request("GET", f"/repos/{owner}/{repo}/actions/runs/{run_id}", token)
    if workflow_run.get("path") != expected_path:
        return f"required check {name!r} workflow path is {workflow_run.get('path')!r}, expected {expected_path!r}"
    if workflow_run.get("head_sha") != head_sha:
        return f"required check {name!r} workflow run is stale for this PR head"
    if workflow_run.get("event") != expected_event:
        return f"required check {name!r} workflow event is {workflow_run.get('event')!r}, expected {expected_event!r}"
    return None


def github_actions_workflow_blocker(
    owner: str,
    repo: str,
    head_sha: str,
    requirement: dict[str, Any],
    token: str,
) -> str | None:
    name = str(requirement["name"])
    expected_path = requirement.get("workflow_path")
    if not isinstance(expected_path, str) or not expected_path:
        return f"required workflow {name!r} is missing trusted workflow_path"
    expected_event = requirement.get("event")
    if not isinstance(expected_event, str) or not expected_event:
        return f"required workflow {name!r} is missing trusted event"
    expected_name = requirement.get("workflow_name")
    if not isinstance(expected_name, str) or not expected_name:
        return f"required workflow {name!r} is missing trusted workflow_name"

    workflow_run = latest_workflow_runs(owner, repo, head_sha, expected_event, token).get(expected_path)
    if workflow_run is None:
        return f"required workflow {name!r} is missing"
    if workflow_run.get("name") != expected_name:
        return f"required workflow {name!r} name is {workflow_run.get('name')!r}, expected {expected_name!r}"
    status = workflow_run.get("status")
    conclusion = workflow_run.get("conclusion")
    if status != "completed" or conclusion != "success":
        return f"required workflow {name!r} is {status}/{conclusion}"
    return None


def commit_status_blocker(status_context: dict[str, Any] | None, requirement: dict[str, Any]) -> str | None:
    name = str(requirement["name"])
    expected_creator = requirement.get("creator")
    if not isinstance(expected_creator, str) or not expected_creator:
        return f"required status {name!r} is missing trusted creator"
    if status_context is None:
        return f"required check {name!r} is missing"
    state = status_context.get("state")
    if state != "success":
        return f"required status {name!r} is {state}"
    creator = status_context.get("creator") or {}
    if creator.get("login") != expected_creator:
        return f"required status {name!r} creator is {creator.get('login')!r}, expected {expected_creator!r}"
    return None


def latest_workflow_runs(
    owner: str, repo: str, head_sha: str, event: str | None, token: str
) -> dict[str, dict[str, Any]]:
    params = {"head_sha": head_sha}
    if event:
        params["event"] = event
    query = urllib.parse.urlencode(params)
    runs = github_paginate_key(f"/repos/{owner}/{repo}/actions/runs?{query}", token, "workflow_runs")
    latest_by_path: dict[str, dict[str, Any]] = {}
    for run in runs:
        if run.get("head_sha") != head_sha:
            continue
        if event and run.get("event") != event:
            continue
        path = run.get("path")
        if not path:
            continue
        current = latest_by_path.get(path)
        run_key = (str(run.get("run_started_at") or ""), int(run.get("id", 0) or 0))
        current_key = (str(current.get("run_started_at") or ""), int(current.get("id", 0) or 0)) if current else ("", 0)
        if current is None or run_key > current_key:
            latest_by_path[path] = run
    return latest_by_path


def latest_commit_statuses(owner: str, repo: str, head_sha: str, token: str) -> dict[str, dict[str, Any]]:
    statuses = github_paginate(f"/repos/{owner}/{repo}/commits/{head_sha}/statuses", token)
    latest_by_context: dict[str, dict[str, Any]] = {}
    for status in statuses:
        context = status.get("context")
        if not context:
            continue
        current = latest_by_context.get(context)
        status_key = (str(status.get("created_at") or ""), int(status.get("id", 0) or 0))
        current_key = (str(current.get("created_at") or ""), int(current.get("id", 0) or 0)) if current else ("", 0)
        if current is None or status_key > current_key:
            latest_by_context[context] = status
    return latest_by_context


def extract_run_id(details_url: str | None) -> str | None:
    if not details_url:
        return None
    marker = "/actions/runs/"
    if marker not in details_url:
        return None
    tail = details_url.split(marker, 1)[1]
    run_id = tail.split("/", 1)[0]
    return run_id if run_id.isdigit() else None


def extract_security_risk(
    owner: str,
    repo: str,
    base_sha: str,
    head_sha: str,
    token: str,
    check_name: str,
    workflow_path: str,
    artifact_name: str,
    latest_by_name: dict[str, dict[str, Any]] | None = None,
) -> tuple[str | None, list[str]]:
    if latest_by_name is None:
        latest_by_name = latest_check_runs(owner, repo, head_sha, token)
    security_run = latest_by_name.get(check_name)
    if not security_run:
        return None, [f"Codex security-review check {check_name!r} is missing"]

    run_id = extract_run_id(security_run.get("details_url") or security_run.get("html_url"))
    if not run_id:
        return None, ["Codex security-review run id was not found"]

    workflow_run = github_request("GET", f"/repos/{owner}/{repo}/actions/runs/{run_id}", token)
    if workflow_run.get("path") != workflow_path:
        return None, [f"Codex security-review run path is {workflow_run.get('path')!r}, expected {workflow_path!r}"]
    if workflow_run.get("head_sha") != head_sha:
        return None, ["Codex security-review workflow run is stale for this PR head"]
    if workflow_run.get("event") != "pull_request":
        return None, [f"Codex security-review workflow event is {workflow_run.get('event')!r}, expected 'pull_request'"]

    artifacts = github_paginate_key(f"/repos/{owner}/{repo}/actions/runs/{run_id}/artifacts", token, "artifacts")
    artifact = next(
        (
            item
            for item in artifacts
            if item.get("name") == artifact_name and not item.get("expired", False)
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
    expected_commit_range = f"{base_sha}...{head_sha}"
    if result.get("commit_range") != expected_commit_range:
        return None, ["Codex security-review result artifact is stale for this PR base/head range"]
    if str(result.get("run_id")) != str(run_id):
        return None, ["Codex security-review result artifact does not match the workflow run"]
    risk_value = result.get("overall_risk")
    if not isinstance(risk_value, str) or not risk_value:
        return None, ["Codex security-review result artifact is missing or invalid overall_risk"]
    risk = risk_value.upper()
    if risk not in SECURITY_RISK_LEVELS:
        return None, [f"Codex security-review result artifact has unknown overall_risk {risk!r}"]
    return risk, []


def evaluate_policy(
    *,
    config: dict[str, Any],
    owner: str,
    repo: str,
    pr_number: int,
    author: str,
    base_sha: str,
    head_sha: str,
    token: str,
    classifier_output: str,
) -> PolicyResult:
    stale_blockers = current_pr_head_blockers(owner, repo, pr_number, head_sha, token, base_sha)
    if stale_blockers:
        return PolicyResult(
            passed=False,
            decision="needs-human-review",
            reasons=stale_blockers,
        )

    files = github_paginate(f"/repos/{owner}/{repo}/pulls/{pr_number}/files", token)
    commits = github_paginate(f"/repos/{owner}/{repo}/pulls/{pr_number}/commits", token)
    reviews = github_paginate(f"/repos/{owner}/{repo}/pulls/{pr_number}/reviews", token)
    stale_blockers = current_pr_head_blockers(owner, repo, pr_number, head_sha, token, base_sha)
    if stale_blockers:
        return PolicyResult(
            passed=False,
            decision="needs-human-review",
            reasons=stale_blockers,
        )

    contributors, unknown_commits = head_contributors(commits)
    low_config = config["low_risk"]
    actor_login, actor_lookup_blockers = workflow_actor_for_head(
        owner, repo, head_sha, low_config.get("trusted_actor_workflow", {}), token
    )
    ineligible_reviewers = set(contributors)
    if actor_login:
        ineligible_reviewers.add(actor_login)

    human_ok, human_reasons, human_blockers = human_review_state(
        reviews,
        head_sha,
        author,
        int(config.get("minimum_human_approvals", 1)),
        owner,
        repo,
        token,
        ineligible_reviewers,
    )
    if unknown_commits:
        human_ok = False
        human_blockers.append(
            "current head has commits without GitHub-linked authors or committers: " + ", ".join(unknown_commits)
        )
    if not actor_login:
        human_ok = False
        human_blockers.extend(actor_lookup_blockers)

    low_reasons: list[str] = []
    low_blockers: list[str] = []
    shared_head_blockers = shared_head_pr_blockers(owner, repo, pr_number, head_sha, token)
    low_blockers.extend(blocker for blocker in human_blockers if blocker.startswith("changes requested by "))

    trusted, trust_reasons = trusted_author_reasons(author, config.get("trusted_authors", []), owner, token)
    (low_reasons if trusted else low_blockers).extend(trust_reasons)
    head_trusted, head_trust_reasons, head_trust_blockers = trusted_head_contributor_reasons(
        commits,
        config.get("trusted_authors", []),
        owner,
        token,
    )
    (low_reasons if head_trusted else low_blockers).extend(head_trust_reasons if head_trusted else head_trust_blockers)
    if actor_login:
        actor_trusted, _trust_reasons = trusted_author_reasons(
            actor_login, config.get("trusted_authors", []), owner, token
        )
        if actor_trusted:
            low_reasons.append(f"authenticated workflow actor @{actor_login} is trusted")
        else:
            low_blockers.append(f"authenticated workflow actor @{actor_login} is not in trusted_authors")
    else:
        low_blockers.extend(actor_lookup_blockers)

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

    content_blockers = deterministic_content_blockers(files, low_config)
    if content_blockers:
        low_blockers.extend(content_blockers)
    else:
        low_reasons.append("deterministic content checks passed")

    latest_by_name = latest_check_runs(owner, repo, head_sha, token)
    checks_ok, check_blockers = check_statuses(
        owner,
        repo,
        head_sha,
        low_config.get("required_checks", []),
        token,
        latest_by_name,
    )
    if checks_ok:
        low_reasons.append("required checks are successful")
    else:
        low_blockers.extend(check_blockers)

    security_risk, security_blockers = extract_security_risk(
        owner,
        repo,
        base_sha,
        head_sha,
        token,
        str(config.get("security_review_check", "security-review")),
        str(config.get("security_review_workflow_path", ".github/workflows/codex-security-review.yml")),
        str(config.get("security_review_artifact", "codex-security-review-result")),
        latest_by_name,
    )
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

    if shared_head_blockers:
        return PolicyResult(
            passed=False,
            decision="needs-human-review",
            reasons=shared_head_blockers + human_blockers + low_blockers,
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

    if human_ok:
        return PolicyResult(
            passed=True,
            decision="human-approved",
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


def write_preflight(preflight: dict[str, Any], path: str | None) -> None:
    if not path:
        return
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(preflight, handle, indent=2)
        handle.write("\n")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--classifier-output", default="")
    parser.add_argument("--result-json")
    parser.add_argument("--preflight-json")
    parser.add_argument("--owner", required=True)
    parser.add_argument("--repo", required=True)
    parser.add_argument("--pr-number", required=True, type=int)
    parser.add_argument("--author", required=True)
    parser.add_argument("--base-sha")
    parser.add_argument("--head-sha", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    token = os.environ.get("GITHUB_TOKEN")
    if not token:
        raise PolicyError("GITHUB_TOKEN is required")

    with open(args.config, encoding="utf-8") as handle:
        config = json.load(handle)

    if args.preflight_json:
        preflight = evaluate_low_risk_preflight(
            config=config,
            owner=args.owner,
            repo=args.repo,
            pr_number=args.pr_number,
            author=args.author,
            head_sha=args.head_sha,
            token=token,
        )
        write_preflight(preflight, args.preflight_json)
        status = "eligible" if preflight["eligible"] else "ineligible"
        print(f"review-policy preflight: {status}")
        for reason in preflight["reasons"]:
            print(f"- {reason}")
        for blocker in preflight["blockers"]:
            print(f"- {blocker}")
        return 0

    enforced = bool(config.get("enforce", True))
    if not args.base_sha:
        raise PolicyError("--base-sha is required when evaluating review policy")

    result = evaluate_policy(
        config=config,
        owner=args.owner,
        repo=args.repo,
        pr_number=args.pr_number,
        author=args.author,
        base_sha=args.base_sha,
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
