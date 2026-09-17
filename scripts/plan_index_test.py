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
        plan_type: str = "plan",
        title: str = '"Example plan"',
    ) -> Path:
        directory = self.plans_dir / "archive" if archived else self.plans_dir
        path = directory / name
        path.write_text(
            "\n".join(
                (
                    "---",
                    f"title: {title}",
                    f"date: {name[:10]}",
                    f"status: {status}",
                    f"type: {plan_type}",
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

    def test_rejects_filename_that_does_not_end_with_plan_type(self) -> None:
        path = self.write_plan(
            "2026-09-17-example-plan.md", status="draft", plan_type="tdd"
        )

        with self.assertRaisesRegex(plan_index.PlanIndexError, "filename must match"):
            plan_index.parse_plan(path, self.plans_dir)

    def test_rejects_plan_in_nested_directory(self) -> None:
        path = self.write_plan("2026-09-17-example-plan.md", status="draft")
        nested_dir = self.plans_dir / "old"
        nested_dir.mkdir()
        nested_path = path.rename(nested_dir / path.name)

        with self.assertRaisesRegex(plan_index.PlanIndexError, "must live directly"):
            plan_index.load_plans(self.plans_dir)

        self.assertTrue(nested_path.exists())

    def test_decodes_escaped_double_quoted_title(self) -> None:
        path = self.write_plan(
            "2026-09-17-escaped-title-plan.md",
            status="draft",
            title='"Use \\"fast\\" mode at C:\\\\fleet"',
        )

        plan = plan_index.parse_plan(path, self.plans_dir)

        self.assertEqual(plan.title, 'Use "fast" mode at C:\\fleet')


if __name__ == "__main__":
    unittest.main()
