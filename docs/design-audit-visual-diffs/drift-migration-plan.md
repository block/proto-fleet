# Proto Fleet Design-System Drift Migration Plan

This document records the high-confidence drift found in production Proto Fleet UI code and pairs each drift with a recommended migration, risk level, rationale, and verification path.

The baseline is Storybook-backed shared UI in `client/src/shared/components` and design tokens in `client/src/shared/styles/theme.css`.

## Coverage Status

This is not a closed inventory yet. It is a source-assisted, high-confidence first pass over production `client/src/protoFleet` TSX, excluding tests and stories. The visual diff Storybook story shows representative specimens, not one specimen for every occurrence.

Covered with exact source locations:

- Undefined or legacy token aliases found by targeted class scans.
- The highest-confidence primitive drift: visible firmware actions, copy icon buttons, inline editable input, `SinglePickerField`, row action menus, assignment-cell popovers, typography scale drift, and the custom curtailment history table.
- Follow-up primitive drift from broader scans: custom tooltip shells, interactive hover list, rack zone typeahead dropdown, scan camera command button, and link-like table buttons.
- Default Tailwind color leakage and hard-coded status colors found by strict color/class scans.
- Native form-control sweep: `ChannelEditableCell` is the only production bare text input that looks like UI drift; firmware upload and scan QR file inputs are hidden/native exceptions.
- Rejected/constrained migrations where the first visual proposal proved worse than the current UI.

Not yet covered:

- Full route-by-route visual QA across every screen, viewport, theme, and interaction state.
- Every raw `<button>` occurrence. Many are valid navigation, domain grid, drag, or compact table controls and need semantic triage before being called drift.
- Generated files and tests.
- Semantic/content anomalies that are not detectable from class names.

## Risk Scale

| Risk | Meaning | Verification |
| --- | --- | --- |
| Low | Token or class migration with no intended behavior change. | Typecheck plus targeted screenshot. |
| Medium | Primitive migration, spacing change, keyboard/focus behavior change, or reusable abstraction. | Typecheck, targeted component test if behavior changes, Storybook screenshot. |
| High | Table/list rewrite, shared primitive API change, or cross-feature behavior change. | Tests plus broad visual QA across affected flows. |

## Recommended Migrations

### Undefined or Legacy Token Aliases

| Migration | Risk | Where | Rationale |
| --- | --- | --- | --- |
| `text-text-secondary` -> `text-text-primary-70` | Low | `client/src/protoFleet/components/FirmwareUpload/FirmwareUploadComponents.tsx:97`, `:110`, `:140`, `:161`; `client/src/protoFleet/features/settings/components/FirmwareUploadDialog.tsx:53`; `client/src/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManageSecurity/ManageSecurityModal.tsx:107`; `client/src/protoFleet/features/fleetManagement/components/MinerActionsMenu/CoolingModeModal/CoolingModeModal.tsx:39`, `:133`; `client/src/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManagePowerModal/ManagePowerModal.tsx:23`; `client/src/protoFleet/features/fleetManagement/components/MinerActionsMenu/FirmwareUpdateModal/FirmwareUpdateModal.tsx:112`, `:158`; `client/src/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage/PoolsList/PoolsList.tsx:123`, `:128`, `:140`; `client/src/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage/PoolSelectionPage.tsx:418`; `client/src/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage/PoolSelectionModal/PoolSelectionModal.tsx:305` | `text-text-secondary` is not defined in `theme.css`. Most usages read as secondary/help text, which matches `text-text-primary-70`. For empty-state or quieter metadata, review whether `text-text-primary-50` is better. |
| `text-body-200` -> `text-200` and `text-text-secondary` -> `text-text-primary-70` | Low | `client/src/protoFleet/features/groupManagement/pages/GroupsPage.tsx:254` | `text-body-200` and `text-text-secondary` are not theme tokens. The visual intent is small secondary error copy. |
| `bg-surface-1` -> `bg-surface-base` or shared `Input` | Medium | `client/src/protoFleet/features/alerts/components/ChannelEditableCell.tsx:42` | `bg-surface-1` is not defined. This is also a bare input, so token replacement is only a partial fix. Prefer shared `Input` or a reusable inline edit primitive. |
| `hover:bg-surface-base-hover` -> `hover:bg-core-primary-5` | Low | `client/src/protoFleet/components/PageHeader/SitePicker/SitePicker.tsx:141`, `:221`; `client/src/protoFleet/features/energy/CurtailmentStartModal.tsx:761` | `surface-base-hover` is not defined. `core-primary-5` is the established hover background in shared row/select/list patterns. For the SitePicker trigger, also make the trigger `inline-flex self-start` so the hover background hugs the label and chevron instead of stretching inside flex-column parents. |
| `hover:bg-surface-secondary` -> `hover:bg-core-primary-5` | Low | `client/src/protoFleet/components/FirmwareUpload/FirmwareUploadComponents.tsx:101` | `surface-secondary` is not defined. The hover should align with secondary button and row hover behavior. |
| `ring-border-focus` -> `ring-border-primary` | Low | `client/src/protoFleet/components/FirmwareUpload/FirmwareUploadComponents.tsx:85` | `border-focus` is not defined, but the strong black drag-active ring is useful and should remain. `border-primary` preserves that high-contrast affordance with a defined light/dark token. |
| `border-border-focus` -> `border-border-20` plus existing selected indicator | Low | `client/src/protoFleet/features/fleetManagement/components/MinerActionsMenu/FirmwareUpdateModal/FirmwareUpdateModal.tsx:139`, `:149` | `border-focus` is not defined. The selected radio dot already carries the stronger state. If the selected card still needs emphasis, pair `border-border-20` with `ring-4 ring-core-primary-5`. |
| `text-text-link` -> `Button variant={variants.textOnly}` or `text-text-emphasis` | Low | `client/src/protoFleet/components/FirmwareUpload/FirmwareUploadComponents.tsx:176` | `text-text-link` is not defined. If it is an action, prefer `Button textOnly`; if it remains text styling, use a defined emphasis token. |

### Default Tailwind and Hard-coded Colors

| Migration | Risk | Where | Rationale |
| --- | --- | --- | --- |
| `hover:bg-gray-50` -> `hover:bg-core-primary-5` and `dark:hover:bg-gray-700/50` -> `dark:hover:bg-core-primary-5` | Low | `client/src/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage/PoolSelectionModal/PoolSelectionModal.tsx:39` | The Proto theme resets Tailwind's default palette, so default `gray-*` classes can either miss the intended token system or become no-op utilities. The row hover should use the same subtle primary hover used by shared rows/select lists. |
| `hover:bg-black/[0.06] dark:hover:bg-white/[0.06]` -> `hover:bg-core-primary-5 dark:hover:bg-core-primary-5` or `Button textOnly` with underline disabled | Low | `client/src/protoFleet/features/buildings/components/BuildingCard.tsx:243` | The menu trigger duplicates a tokenized subtle hover with arbitrary black/white opacity. This is also part of the hand-built row-action menu cleanup, so the preferred end state is a shared icon button/menu trigger. |
| `border-gray-200` -> `border-border-5` or `border-border-10` | Low | `client/src/protoFleet/components/Footer/Footer.tsx:9` | Footer border uses a default Tailwind gray instead of Proto border tokens. Use `border-border-5` if the border should stay quiet, or `border-border-10` if the footer needs a stronger divider. |
| `text-red-500` -> `text-intent-critical-fill` | Low | `client/src/protoFleet/features/fleetManagement/components/MinerList/MinerName.tsx:61` | The alert icon is a critical status affordance and should use the intent token that already maps to Proto's critical red across themes. |
| `#ef4444` -> `bg-intent-critical-fill`, `#f97316` -> `bg-core-accent-fill`, `#d4d4d8` -> `bg-core-primary-20` | Medium | `client/src/protoFleet/features/fleetManagement/components/RackDetailGrid/RackDetailSlot.tsx:14-16` | These hard-coded rack status dots visually map to existing tokens. Risk is medium because the colors carry status semantics; confirm offline should remain orange/accent before applying. |

### Primitive Migrations

| Migration | Risk | Where | Rationale |
| --- | --- | --- | --- |
| Visible firmware choose/retry actions -> `Button` | Low | `client/src/protoFleet/components/FirmwareUpload/FirmwareUploadComponents.tsx:98`, `:176` | The visible controls are normal commands and should inherit shared Button focus, hover, disabled, and loading affordances. Hidden file inputs should remain native. |
| Copy secret icon buttons -> `Button textOnly` icon buttons | Low | `client/src/protoFleet/features/settings/components/CreateApiKeyModal.tsx:227`; `client/src/protoFleet/features/settings/components/ResetPasswordModal.tsx:106` | Keeps the same compact visual footprint while gaining shared focus and disabled behavior. |
| `ChannelEditableCell` bare input -> shared `Input` or `InlineEditableField` | Medium | `client/src/protoFleet/features/alerts/components/ChannelEditableCell.tsx:36` | This field hand-rolls value state, focus state, input styling, and edit icon behavior. A direct `Input` migration may alter density, so a compact inline-edit primitive may be cleaner. |
| `SinglePickerField` -> shared `Select` | Medium | `client/src/protoFleet/features/alerts/components/SinglePickerField.tsx:81`; used by `client/src/protoFleet/features/alerts/components/AddMaintenanceWindowModal.tsx:197`, `:208`, `:223` | It duplicates `Select` behavior: floating label, popover sizing, radio option rows, focus/open state, and scroll bounds. Verify empty-message behavior before replacing. |
| Hand-built action menus -> `RowActionsMenu` pattern | Medium | See "Hand-built Popover Locations" below. | Several menus duplicate trigger, overlay, popover shell, and menu-item styling. Use the existing `RowActionsMenu` pattern where the interaction is a standard action menu. |
| Hand-built assignment-cell popovers -> shared `Popover` shell | Medium | See "Hand-built Popover Locations" below. | These are not row action menus. Preserve the grid-cell selection workflow and edge-aware anchoring, but migrate duplicated shell/item styling to the shared `Popover`/`Row` vocabulary or a small assignment-popover primitive. |
| Custom hover tooltip shells -> shared `Tooltip` | Medium | `client/src/protoFleet/components/DeviceSetList/StatCell.tsx:7-39`; `client/src/protoFleet/features/fleetManagement/components/MinerList/UnsupportedMetric.tsx:7-24`; `client/src/protoFleet/features/buildings/components/BuildingRackGrid/BuildingRackGrid.tsx:307-341`; `client/src/protoFleet/features/buildings/components/BuildingSummaryCard.tsx:103-136` | Shared `Tooltip` exists and is already used by `MinerWorkerName`, `Input`, `Textarea`, and `Stat`. These instances hand-roll fixed positioning, tooltip shell, type color, shadow, and radius. Cursor-following rack/building popovers may need a chart-style Tooltip variant rather than the simple icon Tooltip. |
| Interactive hover list -> shared `Popover` or documented exception | Medium | `client/src/protoFleet/features/fleetManagement/components/MinerList/MinerGroups.tsx:14-84` | This is not a tooltip because it contains links and hover persistence. Use `Popover` positioning/shell if the interaction remains hover-open, or document why the current floating-link menu is an exception. |
| Rack zone suggestion dropdown -> shared `Popover`/`Row` shell or typeahead primitive | Medium | `client/src/protoFleet/features/fleetManagement/components/RackSettingsModal.tsx:316-360` | This should remain free-text typeahead, not plain `Select`, but the dropdown shell, row density, highlighted state, and keyboard/focus behavior should be shared or extracted. |
| Visible scan camera action -> `Button secondary` | Low | `client/src/protoFleet/features/fleetManagement/components/ManageRackModal/ScanMinerQrModalView.tsx:218-231` | Hidden file input is a valid native exception. The visible "Open camera" command is a normal action and should inherit Button focus, hover, disabled, and loading behavior. |
| Link-like table buttons -> real `Link` or `Button textOnly` | Medium | `client/src/protoFleet/features/fleetManagement/components/MinerList/MinerIssues.tsx:123`; `client/src/protoFleet/features/fleetManagement/components/MinerList/MinerStatus.tsx:22`; `client/src/protoFleet/features/fleetManagement/components/MinerList/MinerName.tsx:56` | These raw buttons use underline/opacity affordances but do not inherit shared focus, disabled, or hit-area behavior. Navigation should be real links; in-place actions should use `Button textOnly` with the intended underline setting. |

### Hand-built Popover Locations

The visual diff specimen is representative. Final migration verification must open the owning route/story for each implementation because placement depends on its anchor, overflow container, and viewport edge behavior.

| Kind | Where | Drift | Migration target |
| --- | --- | --- | --- |
| Building row remove menu | `client/src/protoFleet/features/sites/components/ManageSiteModal/ManageSiteModal.tsx:104-128` | Local ellipsis trigger, fixed backdrop, absolute menu shell, and raw button item. | `RowActionsMenu` with a single Remove building action. |
| Building card action menu | `client/src/protoFleet/features/buildings/components/BuildingCard.tsx:233-348`, `:370-379` | Local trigger, `BuildingCardMenu`, fixed backdrop, absolute menu shell, and local menu-item component. | `RowActionsMenu` with View details, View racks, and View miners actions. |
| Rack-modal miner action menu | `client/src/protoFleet/features/fleetManagement/components/ManageRackModal/MinersPane.tsx:174-204` | Local ellipsis trigger, fixed backdrop, absolute menu shell, and raw button items. | `RowActionsMenu` with Remove miner and Blink LEDs actions. |
| Building-grid assignment popover | `client/src/protoFleet/features/buildings/components/ManageBuildingModal/BuildingGridPane.tsx:62-100` | Local fixed backdrop, absolute centered popover, and raw button items for rack assignment. | Shared `Popover` shell plus `Row` items, or an assignment-popover primitive. Not `RowActionsMenu`. |
| Rack-slot assignment popover | `client/src/protoFleet/features/fleetManagement/components/ManageRackModal/RackPane.tsx:37-112`, used at `:182-189` with edge anchoring at `:264-265` | Local fixed backdrop, absolute popover, custom `anchorX` logic, and raw button items. | Shared `Popover` shell plus `Row` items while preserving left/center/right edge anchoring. Not `RowActionsMenu`. |

Positioning note: the first Storybook menu specimen incorrectly anchored `top-full` to the full demo container height, which made the popover appear detached from the ellipsis. The specimen has been corrected to anchor the menu to an inline trigger wrapper; production cleanup should rely on `Popover` positioning, not on the static specimen's coordinates.

### Additional Raw Controls to Triage

These were found by broader source scans but should not be called drift until inspected in the owning UI.

| Candidate | Risk | Where | Rationale / next check |
| --- | --- | --- | --- |
| Native form controls | Low | Drift: `client/src/protoFleet/features/alerts/components/ChannelEditableCell.tsx:36`. Valid hidden-native exceptions: `client/src/protoFleet/components/FirmwareUpload/FirmwareUploadComponents.tsx:111`; `client/src/protoFleet/features/fleetManagement/components/ManageRackModal/ScanMinerQrModalView.tsx:225`. Test-only stub: `client/src/protoFleet/features/settings/components/__testHelpers__/selectStub.tsx:29` | The visible text input should migrate to shared `Input`/inline edit. Hidden file inputs should stay native while their visible triggers migrate to `Button`. The select stub is test infrastructure, not production UI drift. |
| Raw navigation controls | Medium | `client/src/protoFleet/components/NavigationMenu/Navigation.tsx:190`, `:269`; `client/src/protoFleet/components/NavigationMenu/FloatingNavigation.tsx:33` | These are likely valid navigation primitives. Verify focus, aria-current/selected state, hit area, and whether they should remain bespoke because the nav has special layout/state needs. |
| Domain grid controls | Medium | Rack/building grid controls across `BuildingRackGrid`, `BuildingGridPane`, `RackPane`, `MinersPane`, and `RackDetailSlot` | Many raw buttons are domain cells, drag handles, grid slots, or compact table controls. Do not migrate blindly; inspect keyboard behavior, sizing constraints, and shared component fit. |
| Dynamic inline styles | Medium | Layout/geometry examples: `BuildingRackGrid.tsx`, `BuildingGridPane.tsx`, `RackDetailGrid.tsx`, `RackSlotGrid.tsx`, `MiniRackGrid.tsx`, `CurtailmentStartModal.tsx`, `SegmentedMetricPanel.tsx`, `SiteResourcePanel.tsx` | Most inline styles are dynamic geometry, measured popover sizing, carousel transforms, or slot dimensions. They are not drift by default. Keep them unless a shared primitive can own the measurement or a tokenized CSS variable would improve reuse. |

### Rejected or Constrained Migrations

| Proposal | Risk | Where | Decision | Rationale |
| --- | --- | --- | --- | --- |
| Site picker trigger -> `Button textOnly` | Medium | `client/src/protoFleet/components/PageHeader/SitePicker/SitePicker.tsx:138` | Reject | `Button textOnly` intentionally renders a hover underline by default. That creates the faint underline seen in the visual proposal and reads like a link, not a picker trigger. Keep the existing trigger structure, migrate invalid tokens, and make the trigger content-sized with `inline-flex self-start`. |
| Site picker options -> `Row` | Medium | `client/src/protoFleet/components/PageHeader/SitePicker/SitePicker.tsx:215` | Reject for now | The current row density and radio alignment are better. `Row` changes the internal padding and content model. Keep the bespoke option rows unless a shared picker-option primitive is created. |
| Rack slots and drag handles -> `Button` everywhere | Medium | `client/src/protoFleet/features/fleetManagement/components/RackDetailGrid/RackDetailSlot.tsx:35`; `client/src/protoFleet/features/fleetManagement/components/ManageRackModal/RackPane.tsx:156`; `client/src/protoFleet/features/infrastructure/components/ManageColumnsModal.tsx:56`; `client/src/protoFleet/features/fleetManagement/components/MinerList/ManageColumnsModal.tsx:58`; `client/src/protoFleet/features/fleetManagement/components/MinerActionsMenu/BulkRenamePropertyForm.tsx:63` | Constrain | These are domain controls, not generic app buttons. Keep bespoke structure where sizing/drag behavior is domain-specific; normalize tokens, focus styles, and type scale. |
| Heat-map cells -> generic primitives | Medium | `client/src/protoFleet/features/buildings/components/BuildingCard.tsx:122-123` | Constrain | Keep the compact heat-map grammar. Extract/document the intensity constants and `rounded-[3px]` cell geometry instead of turning this into generic list/card chrome. |
| Mini rack cells -> generic buttons | Medium | `client/src/protoFleet/features/fleetManagement/components/RackCard/MiniRackGrid.tsx:46` | Constrain | Keep the dense mini-grid grammar. Verify token names, dot contrast, and whether the status cells need a shared legend; do not migrate each cell to `Button`. |
| Camera viewfinder overlay -> surface tokens/generic frame | Medium | `client/src/protoFleet/features/fleetManagement/components/ManageRackModal/ScanMinerQrModalView.tsx:176-183` | Constrain | The black/white overlay is functional camera framing, not app chrome. Keep the media contrast treatment; the nearby visible camera command is covered by the `Button` migration. |
| Action bar `bg-black` -> neutral surface token | Medium | `client/src/protoFleet/features/fleetManagement/components/ActionBar/ActionBar.tsx:84` | Inspect before changing | The selected-miner action bar intentionally uses an inverse treatment in light mode. It may be valid, but should be checked in the app for contrast, dark-mode parity, and whether `bg-core-primary-fill` is the better semantic token. |

### Typography Scale Drift

| Migration | Risk | Where | Rationale |
| --- | --- | --- | --- |
| `text-[14px]` -> `text-300` | Low | `client/src/protoFleet/features/buildings/components/BuildingRackGrid/BuildingRackGrid.tsx:218`, `:254`, `:316`, `:318`, `:324`, `:330`, `:336`; `client/src/protoFleet/features/buildings/components/BuildingSummaryCard.tsx:111`, `:113`, `:119`, `:125`, `:131`; `client/src/protoFleet/features/fleetManagement/components/RackSlotGrid/RackSlot.tsx:17`; `client/src/protoFleet/features/settings/components/Curtailment/CurtailmentSettingsPage.tsx:822` | `text-300` is the defined 14px body token. Avoid arbitrary text sizes when an exact token exists. |
| `text-sm` -> `text-300` | Low | `client/src/protoFleet/features/groupManagement/components/FleetHealth/FleetHealth.tsx:272`; `client/src/protoFleet/features/dashboard/components/FleetHealthSection/FleetHealthSection.tsx:136` | `text-sm` bypasses the Storybook typography scale. |
| `text-base` / `text-xs` -> `text-emphasis-300` or `text-200` | Low | `client/src/protoFleet/features/settings/components/Team.tsx:135`; `client/src/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage/FleetPoolRow.tsx:34` | Avatar initials and pool row badges should use existing text tokens. |
| `font-semibold`, `tracking-[0.08em]`, custom leading -> `text-emphasis-*` tokens | Low | `client/src/protoFleet/features/settings/components/Curtailment/CurtailmentAutomations.tsx:424`, `:447`, `:462`; `client/src/protoFleet/features/settings/components/Curtailment/CurtailmentSettingsPage.tsx:808`, `:822` | The typography tokens already encode font weight and line height. Custom tracking/weight should be reserved for true exceptions. |

### Structural Drift

| Migration | Risk | Where | Rationale |
| --- | --- | --- | --- |
| Raw history table -> shared `List` or documented table exception | High | `client/src/protoFleet/features/energy/CurtailmentHistory.tsx:701` | Shared `List` owns table density, header, filtering, and row behavior. This table has expandable/detail behavior, so either migrate deliberately or document why it remains custom. |

## Suggested Fix Order

1. Replace undefined/legacy tokens.
2. Fix visible firmware buttons and copy icon buttons.
3. Correct site picker with token-only changes, not `Button textOnly` or `Row`.
4. Normalize typography clusters.
5. Consolidate action menus into `RowActionsMenu` where spacing still matches.
6. Replace default Tailwind palette leakage and hard-coded rack status colors after confirming status semantics.
7. Consolidate confirmed tooltip, typeahead, camera-action, and link-like table-button drift where shared primitives fit.
8. Decide whether `SinglePickerField` can become `Select` directly or needs a small shared extension.
9. Treat `CurtailmentHistory` table as a separate design/behavior migration.

## Design Polish Verification Loop

The goal of this workflow is to reduce the amount of manual confirmation needed after an agent proposes or applies a design cleanup. A change is not considered verified until it passes both code checks and rendered UI/UX checks.

### 1. Define the Change

Before editing, record the exact migration and its intent:

- Source location: file and line.
- Migration: for example, `text-text-secondary` -> `text-text-primary-70`.
- Intended visual outcome: for example, "secondary helper copy keeps the same hierarchy."
- Risk level: low, medium, or high.
- Explicit non-goals: for example, "do not soften the drag-active border" or "do not migrate SitePicker to `Button textOnly`."

### 2. Code Verification Gate

Run code checks that match the blast radius:

- Always run `npx tsc --noEmit --pretty false` for TypeScript/TSX changes.
- Run targeted tests when behavior changes, not just styling.
- Run or update component tests when changing keyboard, focus, selection, dismissal, or drag/drop behavior.
- Search for nearby repeated patterns before introducing a new class, primitive, or helper.

The code gate answers: "Does this compile, preserve behavior, and use existing system APIs correctly?"

### 3. Visual and UX Verification Gate

Inspect the rendered UI, not just the code diff:

- Open the relevant Storybook story or app route in the browser.
- Compare before/proposed states when possible.
- Verify the actual interaction state being changed: hover, focus, selected, disabled, drag-active, open popover, empty state, loading state, and error state as applicable.
- Check desktop and at least one constrained/narrow viewport for spacing, wrapping, truncation, overflow, and flex/grid stretch.
- Confirm hover/focus affordances hug the intended target and do not create misleading link or button semantics.
- Reject a primitive migration if the visual output is worse, even when the primitive is theoretically "more systemized."

The visual gate answers: "Does the rendered UI still feel right in the browser?"

### 4. Evidence Capture

Each design-polish patch should leave lightweight evidence:

- A Storybook story, screenshot, or app screenshot for user-facing visual changes.
- A short note in this document or the final response describing what was visually checked.
- If a recommendation changes after visual review, document the rejection and rationale. Example: SitePicker should keep its bespoke trigger; `Button textOnly` introduced an unwanted underline.

### 5. Report Back

When reporting completion, include:

- Files changed.
- Code checks run.
- Visual/UX checks run, including viewport and state.
- Any rejected migrations or unresolved visual risks.
