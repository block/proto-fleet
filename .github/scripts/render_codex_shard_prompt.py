#!/usr/bin/env python3
"""Render the trusted sharded-review prompt without evaluating repository text."""

from __future__ import annotations

import argparse
import os
import secrets
from pathlib import Path

PLACEHOLDERS = (
    "REVIEW_MANIFEST_FILE",
    "REVIEW_DIFF_FILE",
    "REVIEW_HEAD_SHA",
    "REVIEW_COMMIT_RANGE",
    "SHARD_ID",
    "REVIEW_BLOB_BASE_URL",
    "REVIEW_MERGE_BASE_BLOB_URL",
)


def render(template: str, values: dict[str, str]) -> str:
    rendered = template
    for name in PLACEHOLDERS:
        value = values.get(name, "")
        if not value or any(character in value for character in "\r\n"):
            raise ValueError(f"invalid trusted prompt value for {name}")
        rendered = rendered.replace(f"{{{{{name}}}}}", value)
    if "{{" in rendered or "}}" in rendered:
        raise ValueError("trusted prompt contains an unresolved placeholder")
    return rendered


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--template", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()

    values = {name: os.environ.get(name, "") for name in PLACEHOLDERS}
    rendered = render(args.template.read_text(encoding="utf-8"), values)
    if args.output:
        args.output.write_text(rendered, encoding="utf-8")
    github_output = os.environ.get("GITHUB_OUTPUT")
    if github_output:
        delimiter = f"codex_prompt_{secrets.token_hex(16)}"
        with open(github_output, "a", encoding="utf-8") as handle:
            handle.write(f"prompt<<{delimiter}\n{rendered}\n{delimiter}\n")


if __name__ == "__main__":
    main()
