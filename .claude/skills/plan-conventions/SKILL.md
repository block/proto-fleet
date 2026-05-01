---
name: plan-conventions
description: Use when editing or creating files under `docs/plans/` (TDDs, PRDs, lightweight plans). Enforces frontmatter (`title`, `date`, `status`, `type`) and the `draft → proposed → accepted → implementing → completed | cancelled` status lifecycle.
---

# plan-conventions

## What to do

1. **Frontmatter.** Each plan must have:
   - `title:` — short noun phrase
   - `date:` — `YYYY-MM-DD`, matching the filename's date prefix; stays
     fixed for the life of the doc
   - `status:` — `draft` | `proposed` | `accepted` | `implementing` |
     `completed` | `cancelled`
   - `type:` — `tdd` | `prd` | `plan`
   - `tracker:` — optional GitHub issue/PR URL; expected once status reaches
     `accepted` or beyond
2. **Don't change `status:` without the user asking.** When the user does:
   - To `accepted` or `implementing`: prompt for `tracker:` if empty.
   - To `completed` or `cancelled`: remind the user to `git mv` the file
     to `docs/plans/archive/`. Don't move it yourself.
3. **Don't introduce new status values or `type:` values** — the vocabulary
   is intentionally small.
