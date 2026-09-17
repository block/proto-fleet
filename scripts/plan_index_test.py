#!/usr/bin/env python3

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

import plan_index


class PlanIndexTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.root = Path(self.temp_dir.name)
        self.plans_dir = self.root / "docs" / "plans"
        (self.plans_dir / "archive").mkdir(parents=True)

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def write_plan(
        self,
        name: str,
        *,
        status: str,
        archived: bool = False,
        tracker: str = "",
    ) -> Path:
        directory = self.plans_dir / "archive" if archived else self.plans_dir
        path = directory / name
        path.write_text(
            "\n".join(
                (
                    "---",
                    'title: "Example plan"',
                    f"date: {name[:10]}",
                    f"status: {status}",
                    "type: plan",
                    f"tracker: {tracker}",
                    "---",
                    "",
                    "# Example plan",
                    "",
                )
            ),
            encoding="utf-8",
        )
        return path

    def test_renders_active_and_archived_plans(self) -> None:
        self.write_plan(
            "2026-09-17-active-plan.md",
            status="implementing",
            tracker="https://github.com/block/proto-fleet/issues/123",
        )
        self.write_plan("2026-09-16-done-plan.md", status="completed", archived=True)

        rendered = plan_index.render_index(
            plan_index.load_plans(self.plans_dir), self.plans_dir
        )

        self.assertIn("## Active plans (1)", rendered)
        self.assertIn("[issue #123]", rendered)
        self.assertIn("## Archived plans (1)", rendered)
        self.assertIn("archive/2026-09-16-done-plan.md", rendered)

    def test_rejects_terminal_plan_outside_archive(self) -> None:
        path = self.write_plan("2026-09-17-done-plan.md", status="completed")

        with self.assertRaisesRegex(plan_index.PlanIndexError, "must be archived"):
            plan_index.parse_plan(path, self.plans_dir)

    def test_rejects_active_plan_inside_archive(self) -> None:
        path = self.write_plan(
            "2026-09-17-active-plan.md", status="draft", archived=True
        )

        with self.assertRaisesRegex(
            plan_index.PlanIndexError, "must be completed or cancelled"
        ):
            plan_index.parse_plan(path, self.plans_dir)

    def test_lists_implementing_plans_before_drafts(self) -> None:
        self.write_plan("2026-09-17-draft-plan.md", status="draft")
        self.write_plan("2026-09-16-implementing-plan.md", status="implementing")

        rendered = plan_index.render_index(
            plan_index.load_plans(self.plans_dir), self.plans_dir
        )

        self.assertLess(rendered.index("implementing"), rendered.index("draft"))

    def test_rejects_impossible_dates(self) -> None:
        path = self.write_plan("2026-99-17-invalid-date-plan.md", status="draft")

        with self.assertRaisesRegex(plan_index.PlanIndexError, "invalid date"):
            plan_index.parse_plan(path, self.plans_dir)


if __name__ == "__main__":
    unittest.main()
