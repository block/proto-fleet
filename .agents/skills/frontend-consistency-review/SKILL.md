---
name: frontend-consistency-review
description: Review ProtoFleet and ProtoOS frontend changes for existing design patterns, shared code reuse, and unnecessary UI content. Use for frontend reviews and before completing client features or UI changes.
---

# Frontend consistency review

Review each changed frontend surface through three questions:

1. Are we aligning with design and implementation patterns already used here?
2. Are we rebuilding something that exists, or missing a useful shared extraction?
3. Does every added label, data point, control, and explanation serve the user's task?

Keep explicit review requests read-only. During implementation, fix findings within
the authorized scope. Do not turn a focused review into a redesign or repo-wide
deduplication project.

## Establish the comparison

- Identify the intended user task and acceptance criteria from the request, issue,
  or PR. Distinguish requested behavior from additions invented during implementation.
  If intent is unavailable, state assumptions and flag uncertain product choices as
  questions rather than declaring them defects.
- Review the requested diff. For this workspace, the default branch comparison is
  `git diff origin/main...HEAD`; include staged, unstaged, and untracked files for
  local work. Use the actual PR base for a PR review, especially a stacked PR.
- Read [client guidance](../../../client/AGENTS.md) and the relevant architecture
  sections of the [client README](../../../client/README.md). Search
  `docs/solutions/` for the affected subsystem when applicable.
- Find the closest existing screen or interaction in the same app, then inspect
  relevant shared implementations, stories, tests, and callers. Search by behavior
  as well as component name; a differently named component may already solve it.
  Inspect the implementation before claiming it is reusable.

## 1. Align with established patterns

Compare hierarchy, page layout, spacing, typography, colors, icons, action placement,
selection, navigation, dialogs, and loading/empty/error states with the closest
relevant existing flow. Check light/dark and responsive behavior where affected.
Prefer semantic theme tokens and existing component variants over bespoke styling
or overrides that undo the shared component's design.

Use these starting points, then follow current callers; this is not a complete catalog:

| Concern | Where to look under `client/src/` |
| --- | --- |
| Layout and actions | `shared/components/{Card,Header,ContentHeader,Row,List,Button,RowActionsMenu,ResponsiveActionGroup}`, each app's `components/` and neighboring feature pages |
| Forms and overlays | `shared/components/{Input,Select,Checkbox,Dialog,Modal,ActionSheet,Popover,Tooltip}` and `shared/hooks/` for dismissal and positioning |
| Metrics, charts, and states | `shared/components/{Metric,Stat,Stats,Chart,LineChart,HashRateValue,PowerValue,TemperatureValue,DataNullState,Callout}`, plus the app's existing dashboard and KPI features |
| Visual language | `shared/styles/theme.css`, `shared/assets/icons/`, `shared/constants/breakpoints.ts`, and relevant `*.stories.tsx` |
| Behavior and data | `shared/hooks/`, `shared/utils/`, `shared/features/`, and the owning app's `hooks/`, `store/`, `api/`, and feature utilities |

`client/src/shared/stories/design-drift.stories.tsx` (`Foundation/Design Drift`) contains
examples of bespoke controls compared with shared primitives. Its proposed
specimens are suggestions, not proof of adopted behavior. Likewise, an existing
screen may contain legacy drift: prefer documented conventions and current shared
contracts over copying a nearby inconsistency.

Check implementation patterns too: reuse the owning app's data access, store
selectors, polling, preferences, and route conventions when applicable. New routes
must follow `client/AGENTS.md` and the app's `routePrefetch.ts` runbook. Intentional
departures can be appropriate; explain the requirement the existing pattern cannot
meet instead of enforcing visual sameness without regard to behavior.

## 2. Reuse before rebuilding

For new components, hooks, formatters, and substantial copied JSX or logic:

- Look in the feature, app-level shared code, and `src/shared/`. Inspect the other
  app for possible extraction without importing across app boundaries.
- Prefer an existing component as-is, then composition or a small compatible
  extension. Check its props, interaction semantics, and current consumers before
  recommending it. Similar appearance alone does not establish compatibility.
- When existing feature components could serve this use case, name the concrete
  consumers and the common behavior to extract. Keep app-specific API, routing,
  and store dependencies in the owning app; pass neutral data and callbacks into
  shared presentation.
- Put reuse at the narrowest useful scope: feature-local for one feature,
  `src/{app}/components/` for multiple features in one app, and
  `src/shared/components/` for both apps. Apply the same ownership principle to
  hooks and utilities. Shared code cannot import either app, and the apps cannot
  import each other.
- Challenge abstractions with speculative consumers, many mode flags, or unrelated
  responsibilities. Keep distinct implementations when semantics differ enough
  that sharing would obscure behavior. Do not extract solely to reduce line count.

For telemetry, inspect `shared/utils/telemetryFormat.ts` and existing value
components before introducing unit conversion, precision, or fallback logic.
Verify units and null/zero/stale semantics at the actual call sites; reuse must
not turn unavailable readings into a healthy-looking zero.

## 3. Audit UI content for necessity

For every added heading, subtitle, badge, metric, helper paragraph, tooltip,
status message, and action, ask: **What user decision or action does this support,
and what would the user lose if it disappeared?** Availability in an API response
is not a reason to display a field.

Look specifically for:

- Labels or subtitles that repeat the page title, breadcrumb, field name, or value.
- Extra KPI cards, counts, timestamps, IDs, and diagnostic details unrelated to
  the task; repeated summaries of information already visible nearby.
- Explanatory prose that narrates obvious controls or exposes implementation
  details such as polling intervals, API sources, or internal object structure.
- Decorative badges, callouts, empty sections, and future-feature placeholders
  that compete with the primary information or action.
- Additional actions or configuration introduced without a supported use case.

Recommend the smallest useful presentation: remove redundancy, shorten copy,
or move genuinely secondary details into an existing detail view when appropriate.
Do not invent another tooltip or panel just to relocate content with no purpose.
Do not add filler copy merely because an existing component has a subtitle prop;
consider an appropriate variant or a small compatible change.

Preserve accessible names and necessary visible field labels, units, error
recovery, destructive-action context, and explanations of partial or stale data.
For example, a subtitle saying “View tank details” below a tank title is a removal
candidate; reporting-module coverage that qualifies a partial power total prevents
misinterpretation. Judge usefulness in context, not by a blanket preference for
fewer words. Respect explicit product requirements and raise conflicts clearly.

## Evidence and report

For visible changes, inspect the rendered screen or relevant Storybook story when
available. Compare affected states, themes, and viewport sizes with the reference
flow; do not claim visual validation from source alone. Report unavailable runtime
or data prerequisites as gaps. If using Playwright, follow the
[E2E skill](../proto-fleet-playwright-e2e/SKILL.md); the root snapshot approval rule
still applies. For a shared change, inspect its existing consumers in both apps
and select checks for the affected behavior.

Lead with actionable findings, ordered by impact. Explain each finding fully enough
that the reader can understand the problem and implement the proposed correction
without asking for another investigation. Use this structure for each finding:

1. **Issue and impact.** Link to the changed file and line and name the affected
   component or function. Explain what the code currently does, how it diverges
   from the relevant pattern or requirement, and why that matters to the user or
   maintainability. Separate supported defects from optional simplifications and
   product questions; do not present taste as a blocker.
2. **Concrete example.** Walk through a specific interaction, input, UI state, or
   duplicated implementation that demonstrates the issue. Show the current result
   and the intended result. For excess UI content, quote the actual text or name
   the actual data points and explain what is redundant in that screen's context.
   For duplication, identify both implementations and the behavior they share.
   Distinguish observed behavior from an illustrative scenario inferred from source.
3. **Source-grounded solution.** Inspect the relevant source before recommending
   a fix. Link to the existing component, helper, hook, token, or reference caller
   that supports the recommendation. Describe the smallest concrete edits: what
   to replace or remove, where the change belongs, and which existing props or
   functions to use. Verify that their actual contracts support the example;
   do not invent an API or stop at “reuse a shared component.” If an extension or
   extraction is needed, identify the missing capability, proposed interface,
   actual consumers to migrate, and behavior that must remain app-specific.
   Clearly distinguish proposed new code from code that already exists. If no
   suitable implementation exists, say what was inspected and justify the new code.
4. **Verification.** State the focused interaction, story, or test that would
   demonstrate the fix and any existing consumers at risk. Distinguish suggested
   verification from checks actually run. For a simple copy removal, a visual
   check may suffice; do not prescribe new tests for every finding.

Use short before/after snippets when they make the edit clearer, grounded in the
actual symbols and types. Label sketches as proposals, not applied or validated
patches. Scale detail to the issue: a redundant label may need only a few sentences,
while a shared extraction needs enough detail to assess its boundaries and tradeoffs.
Favor one recommended solution with a reason; include alternatives only when a
material tradeoff remains. Keep findings self-contained and avoid generic advice
or repeated checklist text. If evidence is missing, name the gap instead of
presenting an unverified fix as ready to implement.

Finish with a brief verdict for **pattern alignment**, **reuse**, and **UI
necessity**, plus what was inspected or tested and any gaps. Explicitly say when
a lens has no findings. Do not manufacture issues to fill a checklist, and do not
claim broader functional correctness from this focused review.
