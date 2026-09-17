# Planning documents

Plans live under `docs/plans/YYYY-MM-DD-<slug>-<type>.md`. Types are `tdd`,
`prd`, and `plan`. Required frontmatter: `title`, `date`, `status`, and `type`.
The date matches the filename prefix and stays fixed for the document's life.
`tracker` is optional initially and expected at `accepted` or later; use an
existing issue/PR reference when known, otherwise flag the missing reference
without inventing one or blocking an authorized status change.

Status lifecycle: `draft → proposed → accepted → implementing → completed |
cancelled`. Do not change status unless requested by the user. When an
authorized transition reaches `completed` or `cancelled`, move the document
to `docs/plans/archive/` using `git mv` (a normal move for an untracked file).
Do not ask the user to perform the associated move. Preserve inbound links.

After creating, changing, or archiving a plan, run `just plan-index` and
inspect the regenerated `docs/plans/README.md`. Verify the lifecycle rules and
generated inventory with `just check-plan-hygiene`; commit the index with the
plan change.

For new documents, select only the matching template:

- [TDD](templates/tdd.md): technical design, alternatives, risks, test plan.
- [PRD](templates/prd.md): problem, users, success criteria, scope.
- [Plan](templates/plan.md): lightweight approach and acceptance criteria.

Infer the type from the request; use `plan` for an unspecified lightweight
plan. Keep meaningful acceptance criteria rather than filling every section
with boilerplate. Do not add new status or type values.
