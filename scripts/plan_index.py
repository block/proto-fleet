#!/usr/bin/env python3
"""Generate and validate the docs/plans index from plan frontmatter."""

from __future__ import annotations

import argparse
import difflib
import json
import re
import sys
from dataclasses import dataclass
from datetime import date
from pathlib import Path

VALID_STATUSES = (
    "draft",
    "proposed",
    "accepted",
    "implementing",
    "completed",
    "cancelled",
)
ACTIVE_STATUSES = set(VALID_STATUSES[:4])
ARCHIVED_STATUSES = set(VALID_STATUSES[4:])
VALID_TYPES = {"tdd", "prd", "plan"}
DISPLAY_ORDER = {
    "implementing": 0,
    "accepted": 1,
    "proposed": 2,
    "draft": 3,
    "completed": 4,
    "cancelled": 5,
}


class PlanIndexError(ValueError):
    """Raised when plan metadata violates the lifecycle contract."""


@dataclass(frozen=True)
class Plan:
    path: Path
    title: str
    plan_date: str
    status: str
    plan_type: str
    tracker: str
    archived: bool


def _unquote(value: str) -> str:
    if len(value) >= 2 and value[0] == value[-1] == '"':
        try:
            return json.loads(value)
        except json.JSONDecodeError as error:
            raise PlanIndexError(f"invalid double-quoted value {value!r}") from error
    if len(value) >= 2 and value[0] == value[-1] == "'":
        return value[1:-1].replace("''", "'")
    return value


def parse_plan(path: Path, plans_dir: Path) -> Plan:
    lines = path.read_text(encoding="utf-8").splitlines()
    if not lines or lines[0] != "---":
        raise PlanIndexError(f"{path}: missing YAML frontmatter")
    try:
        end = lines.index("---", 1)
    except ValueError as error:
        raise PlanIndexError(f"{path}: unterminated YAML frontmatter") from error

    metadata: dict[str, str] = {}
    for line in lines[1:end]:
        if ":" not in line:
            continue
        key, value = line.split(":", 1)
        metadata[key.strip()] = _unquote(value.strip())

    missing = [
        key for key in ("title", "date", "status", "type") if not metadata.get(key)
    ]
    if missing:
        raise PlanIndexError(
            f"{path}: missing frontmatter fields: {', '.join(missing)}"
        )

    plan_date = metadata["date"]
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", plan_date):
        raise PlanIndexError(f"{path}: invalid date {plan_date!r}")
    try:
        date.fromisoformat(plan_date)
    except ValueError as error:
        raise PlanIndexError(f"{path}: invalid date {plan_date!r}") from error
    if not path.name.startswith(f"{plan_date}-"):
        raise PlanIndexError(f"{path}: frontmatter date does not match filename")

    status = metadata["status"]
    if status not in VALID_STATUSES:
        raise PlanIndexError(f"{path}: invalid status {status!r}")
    plan_type = metadata["type"]
    if plan_type not in VALID_TYPES:
        raise PlanIndexError(f"{path}: invalid type {plan_type!r}")

    filename_match = re.fullmatch(
        rf"{re.escape(plan_date)}-([a-z0-9]+(?:-[a-z0-9]+)*)-{plan_type}\.md",
        path.name,
    )
    if not filename_match:
        raise PlanIndexError(
            f"{path}: filename must match YYYY-MM-DD-<slug>-{plan_type}.md"
        )

    if path.parent == plans_dir:
        archived = False
    elif path.parent == plans_dir / "archive":
        archived = True
    else:
        raise PlanIndexError(
            f"{path}: plans must live directly in docs/plans or docs/plans/archive"
        )
    if archived and status not in ARCHIVED_STATUSES:
        raise PlanIndexError(f"{path}: archived plans must be completed or cancelled")
    if not archived and status not in ACTIVE_STATUSES:
        raise PlanIndexError(f"{path}: completed or cancelled plans must be archived")

    return Plan(
        path=path,
        title=metadata["title"],
        plan_date=plan_date,
        status=status,
        plan_type=plan_type,
        tracker=metadata.get("tracker", ""),
        archived=archived,
    )


def load_plans(plans_dir: Path) -> list[Plan]:
    paths = sorted(path for path in plans_dir.rglob("*.md") if path.name != "README.md")
    return [parse_plan(path, plans_dir) for path in paths]


def _cell(value: str) -> str:
    return value.replace("|", "\\|")


def _tracker_link(tracker: str) -> str:
    if not tracker:
        return "—"
    match = re.fullmatch(
        r"https://github\.com/[^/]+/[^/]+/(issues|pull)/(\d+)", tracker
    )
    if not match:
        return _cell(tracker)
    kind, number = match.groups()
    label = f"issue #{number}" if kind == "issues" else f"PR #{number}"
    return f"[{label}]({tracker})"


def _sort_key(plan: Plan) -> tuple[int, int, str]:
    return (
        DISPLAY_ORDER[plan.status],
        -date.fromisoformat(plan.plan_date).toordinal(),
        plan.title.lower(),
    )


def _table(plans: list[Plan], plans_dir: Path) -> list[str]:
    rows = [
        "| Status | Date | Plan | Type | Tracker |",
        "| --- | --- | --- | --- | --- |",
    ]
    for plan in sorted(plans, key=_sort_key):
        relative_path = plan.path.relative_to(plans_dir)
        rows.append(
            f"| {plan.status} | {plan.plan_date} | "
            f"[{_cell(plan.title)}]({relative_path.as_posix()}) | {plan.plan_type} | "
            f"{_tracker_link(plan.tracker)} |"
        )
    return rows


def render_index(plans: list[Plan], plans_dir: Path) -> str:
    active = [plan for plan in plans if not plan.archived]
    archived = [plan for plan in plans if plan.archived]
    lines = [
        "<!-- Generated by `scripts/plan_index.py --write`; do not edit by hand. -->",
        "# Plan index",
        "",
        "Active plans stay in this directory. Completed and cancelled plans move to",
        "`archive/` in the same change that closes the work. Update frontmatter, then run",
        "`scripts/plan_index.py --write`.",
        "",
        f"## Active plans ({len(active)})",
        "",
        *_table(active, plans_dir),
        "",
        f"## Archived plans ({len(archived)})",
        "",
        *_table(archived, plans_dir),
        "",
    ]
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    action = parser.add_mutually_exclusive_group(required=True)
    action.add_argument("--write", action="store_true")
    action.add_argument("--check", action="store_true")
    parser.add_argument(
        "--root", type=Path, default=Path(__file__).resolve().parents[1]
    )
    args = parser.parse_args(argv)

    plans_dir = args.root / "docs" / "plans"
    index_path = plans_dir / "README.md"
    try:
        rendered = render_index(load_plans(plans_dir), plans_dir)
    except (OSError, PlanIndexError) as error:
        print(error, file=sys.stderr)
        return 1

    if args.write:
        if (
            not index_path.exists()
            or index_path.read_text(encoding="utf-8") != rendered
        ):
            index_path.write_text(rendered, encoding="utf-8")
            print(f"updated {index_path}")
        return 0

    existing = index_path.read_text(encoding="utf-8") if index_path.exists() else ""
    if existing == rendered:
        print("plan index is current")
        return 0
    print("plan index is stale; run scripts/plan_index.py --write", file=sys.stderr)
    print(
        "".join(
            difflib.unified_diff(
                existing.splitlines(keepends=True),
                rendered.splitlines(keepends=True),
                fromfile=str(index_path),
                tofile="generated plan index",
            )
        ),
        file=sys.stderr,
    )
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
