---
description: Draft release notes for a tag by grouping commits since the previous tag using Conventional Commit prefixes.
argument-hint: <version> (e.g. v0.2.0)
---

Draft release notes for `$ARGUMENTS`. The repo's `release.yml` workflow
fires on `v*` tags matching `vMAJOR.MINOR.PATCH(-prerelease)?`.

## Steps

1. Determine the previous tag with `git describe --tags --abbrev=0 --match 'v*' HEAD`.
   If `$ARGUMENTS` is the previous tag's successor on `main`, use the previous
   tag as the range start; otherwise ask the user for the comparison base.
2. List commits in range with
   `git log --pretty=format:"%h %s" <previous>..HEAD`.
3. Group by Conventional Commit type:
   - **Features** — `feat:`, `feat(scope):`
   - **Fixes** — `fix:`
   - **Refactors** — `refactor:`
   - **Docs** — `docs:`
   - **Chores / CI / deps** — `chore:`, `ci:`, `build:`, `chore(deps):`
   - **Tests** — `test:`
   - **Other** — anything not matching the above
4. For each entry, keep the short description; strip the prefix and scope.
   Append the PR number if present (`(#123)`) as a link target in the
   final draft.
5. Surface anything that looks load-bearing for users — schema migrations,
   CLI flag changes, breaking proto changes, deprecation removals — in a
   **Breaking changes** block at the top of the draft if applicable.
6. Output a markdown draft suitable for the GitHub release body. Do NOT
   create the tag, push it, or call `gh release create` — stop after
   presenting the draft.

## Notes

- Full releases must be on `main` per `release.yml`. Prereleases (with
  `-rc`, `-beta`, etc.) can come from any commit.
- If the diff between the previous tag and HEAD is empty, say so and stop.
