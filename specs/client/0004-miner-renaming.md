---
id: "0004"
title: "Miner Renaming"
status: spec-ahead
related:
  - specs/server/0017-miner-renaming.md
last-verified: ""
code-refs:
  - client/src/protoFleet/features/
---

# Miner Renaming

> Client-side UI for assigning user-defined display names to miners, covering the options component, single-miner rename flow, and bulk rename flow.

## Overview

**Purpose**: Allow operators to assign human-readable names to one or many miners through the ProtoFleet UI. Single miner rename uses a free-text field; bulk rename uses a property-based system. Collision avoidance is handled client-side via property uniqueness markers.

**Scope**: Covers the naming options component, single-miner rename flow, and bulk rename modal.

**Audience**: Developers working on ProtoFleet UI features.

## Context & Background

Miners currently have no user-defined display name — they are shown as `manufacturer + model` (e.g., "Bitmain Antminer S19"). Renaming gives operators a flexible labelling system that can encode physical location, role, or any custom string on top of that identity.

Duplicate names are allowed — the UI warns but does not block.

## Architecture

### Components

| Component | Description |
|-----------|-------------|
| Single miner rename flow | Action menu entry → free-text rename dialog for one miner |
| Options component | Property-based name configuration UI, used only in the bulk rename modal |
| Name preview area | Shared component (`shared/components/`) rendering a name visually; used in single rename dialog, bulk rename side panel, and all property options modals, with different layouts per context |
| Bulk rename modal | Fullscreen modal launched from bulk actions menu |
| Uniqueness warning | Dialog shown on submit when no uniqueness-guaranteeing property is selected |

### Patterns

- Single and bulk rename flows are distinct UIs — the options component is not shared.
- All flows must be usable on mobile screen sizes. The bulk rename modal's two-panel layout stacks vertically on mobile — name properties above, preview below.
- The name preview area is shared but renders differently per context:
  - **Single rename dialog**: shows current name only on open; updates to `current name → new name` with a debounce once the user starts typing
  - **Bulk rename side panel**: `current name → new name` rows (first 3, "...", last 3); updates with a debounce on every property configuration change
  - **Property options modal** (per-property configuration within bulk rename): new name only; updates with a debounce
  - All preview updates use the shared `debounce` utility (`shared/utils/utility.ts`, default 500ms delay).
- Bulk flow persists the user's last name configuration across sessions and restores it on reopen; falls back to defaults on first use (all properties deselected, separator set to dash). The following are persisted: property order, which properties are enabled, and the property separator. Stored under the `proto-ui-preferences` localStorage key via Zustand `persist` middleware in `useFleetStore`.
- If no uniqueness-guaranteeing property is selected in the bulk rename flow, a warning dialog is shown on submit. This is evaluated client-side and does not block submission.


## Interfaces & Contracts

### Single Miner Rename UI

A free-text input field pre-populated with the miner's current display name. Below the field, a preview area initially shows the current name only. Once the user begins typing, the preview updates to `current name → new name` (debounced). Submitted via a "Save" button.

The submitted value maps to a `StringProperty` in `MinerNameConfig` — the server API is the same as bulk rename.

### Bulk Rename Name Properties

In the bulk rename modal, a miner name is composed from a configured set of properties:

| Category | Properties |
|----------|-----------|
| **Custom** | Custom string + counter ✦, counter ✦, string only |
| **Fixed values** | MAC address ✦, serial number ✦, worker name, model, manufacturer, location (reserved, not yet implemented) |
| **Qualifiers** | Building, rack, rack position (reserved, not yet implemented) |

✦ Uniqueness-guaranteeing property. If no ✦ property is enabled, a warning dialog is shown on submit that names may not be unique across the fleet.

The options component lets the user select, order, and configure properties to form the final name pattern. Each property row has a drag handle on the left — rows can be reordered via drag-and-drop to control the order properties appear in the generated name. The display order maps directly to the order of items in `MinerNameConfig.properties` sent to the API. When a property is enabled, a settings icon appears next to its toggle — clicking it opens that property's options modal.

**Qualifier options modal**: Shows Prefix (optional) and Suffix (optional) text fields, followed by a name preview area (new name only).

**Custom property options modal**: When configuring a custom property, a type dropdown determines which fields are shown:

| Type | Fields shown |
|------|-------------|
| Custom string + counter | Prefix (optional), Suffix (optional), Counter start number, Counter scale (1–6) |
| Counter only | Counter start number, Counter scale (1–6) |
| String only | String (required) |

All three types show a name preview area (new name only) below the fields.

**Fixed value options modal**: Shows the following fields:
- **Number of characters**: radio buttons — All, 1, 2, 3, 4, 5, 6
- **String section**: radio buttons — "First N characters" / "Last N characters" (where N reflects the selected count); hidden when "All" is selected
- Name preview area (new name only)

### Bulk Rename Name Preview

The bulk rename modal shows a side panel with a live preview of the first 3 and last 3 miners from the selected set, sorted by their current display name (`custom_name` if set, otherwise `manufacturer + model`). Each row shows `current name → new name`, with "..." separating the two groups.

**Determining the preview miners**:
- *Specific selection*: miners are already in local client state — sort client-side by current display name and take the first 3 and last 3.
- *All miners (`all_devices`)*: two lightweight API calls against the existing miners list endpoint — ascending sort limit 3 (first 3), descending sort limit 3 then reversed (last 3).
- *Fewer than 6 miners total*: show all miners without deduplication.

**Generating the new name preview**:
Property values (MAC address, serial number, worker name, model, manufacturer) are fetched once for the 6 preview miners when the modal opens. The name generation logic runs client-side, mirroring the server's `MinerNameConfig` evaluation, and re-evaluates on every property configuration change to keep the preview live without additional API calls.

If a preview miner has no default pool configured, the worker name segment is omitted from its generated name (matching server behaviour).

## Data Flow

### Single Miner Rename

1. User opens miner action menu → selects "Rename".
2. Dialog opens with a text field pre-populated with the miner's current display name and a preview area below it showing the current name.
3. User edits the name → preview updates with a debounce.
4. On save: commit rename API call. UI reflects new name.

### Bulk Rename

1. User selects miners (a specific set or all miners in the fleet) → opens bulk actions menu → selects "Rename".
2. Fullscreen modal opens with settings restored from last session (or defaults on first use).
3. User configures name properties → previews resulting names across the visible selection.
4. On submit: commit bulk rename API call.

### Input Validation

- Trim whitespace before sending.
- Names exceeding 100 characters should be rejected client-side with an inline error before submission (matching the server-side limit).
- If the resulting name would be blank — empty text field (single rename) or no properties selected / all selected properties resolve to empty strings (bulk rename) — show a confirmation dialog before proceeding:
  - **Title**: "You haven't made any changes"
  - **Body**: "You can continue to retain your existing miner names, or keep editing. Do you want to continue anyway?"
  - **Actions**: "No, keep editing" (returns to editing) / "Yes, continue" (exits the rename flow without making any changes)

### Uniqueness Warning

Each bulk rename name property is classified as uniqueness-guaranteeing (✦) or not. The check is evaluated client-side at submit time — no API call is required.

| Property | Unique? | Reason |
|----------|---------|--------|
| MAC address | ✦ Yes | Globally unique hardware identifier |
| Serial number | ✦ Yes | Manufacturer-assigned unique ID |
| Counter | ✦ Yes | Increments per miner within the selected set |
| String + counter | ✦ Yes | Unique via the counter component |
| Worker name | No | Operators often configure the same value across many miners |
| Model | No | Shared across all miners of the same model |
| Manufacturer | No | Shared across all miners from the same manufacturer |
| String only | No | Static value, identical for all miners |
| Qualifiers | No | Shared across groups of miners by definition |

If none of the enabled properties are uniqueness-guaranteeing, a warning dialog is shown when the user submits:
- **Title**: "Duplicate names"
- **Body**: "Some miners may have duplicate names. Proceeding may impact accuracy in operations and reporting. Do you want to continue anyway?"
- **Actions**: "No, keep editing" (returns to editing) / "Yes, continue" (proceeds with submission)

### State Transitions

**Single rename:**
```
Idle
  → Dialog open (text field pre-populated with current name)
    → User edits name
      → Save pressed
        → Empty name warning shown (if text field is blank after trim)
            → "No, keep editing" → Back to editing
            → "Yes, continue" → Idle (no changes made)
        → Commit in progress → Success / Error
```

**Bulk rename:**
```
Idle
  → Modal open (settings restored from last session, or defaults on first use)
    → Properties configured
      → Submit pressed
        → Uniqueness warning dialog shown (if no ✦ property enabled)
            → "No, keep editing" → Back to properties configured
            → "Yes, continue" → proceed
        → Empty name warning shown (if name would be blank)
            → "No, keep editing" → Back to properties configured
            → "Yes, continue" → Idle (no changes made)
        → Commit in progress → Success / Error
```

## Testing

### Strategy

Unit tests for the single rename text field, the options component (property selection, name preview generation), and the uniqueness warning logic. Integration tests for both the single and bulk rename flows against a mocked API.

### Known Gaps

- End-to-end test covering the full rename flow including the empty name warning and uniqueness warning.
- Mobile layout testing for all rename flows.

## Verification

| Command | What it checks |
|---------|----------------|
| `npm test` | Unit and integration tests |

## Limitations & Known Issues

- Duplicate names are allowed by design; the UI warns but does not prevent submission.

## Related Specifications

| Spec | Relationship |
|------|-------------|
| [0017-miner-renaming (server)](../server/0017-miner-renaming.md) | Server-side API and persistence |

## Changelog

| Date | Author | Change | Reason |
|------|--------|--------|--------|
| 2026-02-26 | Negar | Initial draft | Initial feature development |
