import { create } from "@bufbuild/protobuf";

import { type ReleaseChannelScope, ReleaseChannelScopeSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";

// Scope selectors are sets; the server deduplicates and sorts their IDs.
export const scopeSelectionFields = ["siteIds", "buildingIds", "rackIds", "groupIds", "deviceIdentifiers"] as const;

export const sameSelection = (left: readonly (bigint | string)[], right: readonly (bigint | string)[]): boolean => {
  const leftIds = new Set(left);
  const rightIds = new Set(right);
  return leftIds.size === rightIds.size && [...leftIds].every((id) => rightIds.has(id));
};

export function scopeSelectionsEqual(left: ReleaseChannelScope, right: ReleaseChannelScope): boolean {
  return scopeSelectionFields.every((field) => sameSelection(left[field], right[field]));
}

// Preserve each locally edited selector while accepting remote changes to the
// others. A concurrent edit to the same selector leaves that local choice intact.
export function rebaseScope(
  draft: ReleaseChannelScope,
  previous: ReleaseChannelScope,
  incoming: ReleaseChannelScope,
): ReleaseChannelScope {
  const keepLocal = <T extends bigint | string>(local: T[], base: T[], remote: T[]): T[] =>
    sameSelection(local, base) ? remote : local;
  const merged = create(ReleaseChannelScopeSchema, {
    siteIds: keepLocal(draft.siteIds, previous.siteIds, incoming.siteIds),
    buildingIds: keepLocal(draft.buildingIds, previous.buildingIds, incoming.buildingIds),
    rackIds: keepLocal(draft.rackIds, previous.rackIds, incoming.rackIds),
    groupIds: keepLocal(draft.groupIds, previous.groupIds, incoming.groupIds),
    deviceIdentifiers: keepLocal(draft.deviceIdentifiers, previous.deviceIdentifiers, incoming.deviceIdentifiers),
  });
  // Equivalent polls should not invalidate or restart an in-flight scope preview.
  return scopeSelectionsEqual(draft, merged) ? draft : merged;
}

// Applies a picker's edits to the current identifiers. One the picker was
// offered and no longer returns is removed, and one it returns beyond those is
// added. Every other identifier is left as it is, so the other kind of device
// and a change rebased in while the picker was open both survive its save.
export function applyPickerEdits(current: string[], offered: string[], picked: string[]): string[] {
  const offeredIds = new Set(offered);
  const pickedIds = new Set(picked);
  return [
    ...new Set([
      ...current.filter((id) => pickedIds.has(id) || !offeredIds.has(id)),
      ...picked.filter((id) => !offeredIds.has(id)),
    ]),
  ];
}

// Repeated-field bounds from ReleaseChannelScope apply to both preview and
// writes. Keep every selected ID so operators can reduce an oversized draft.
export function scopeValidationErrors(scope: ReleaseChannelScope): string[] {
  const dimensions = [
    { ids: scope.siteIds, limit: 100, label: "sites" },
    { ids: scope.buildingIds, limit: 100, label: "buildings" },
    { ids: scope.rackIds, limit: 500, label: "racks" },
    { ids: scope.groupIds, limit: 100, label: "groups" },
    { ids: scope.deviceIdentifiers, limit: 10000, label: "miners and containers" },
  ];
  return dimensions
    .filter(({ ids, limit }) => ids.length > limit)
    .map(
      ({ ids, limit, label }) =>
        `Select no more than ${limit.toLocaleString()} ${label} (${ids.length.toLocaleString()} selected).`,
    );
}

// One label per populated dimension ("2 racks, 5 miners"); "Nothing
// selected" when the scope is empty. Containers share deviceIdentifiers with
// miners, so the caller says how many of those identifiers are containers.
export function scopeSummary(scope: ReleaseChannelScope, containerCount = 0): string {
  const parts: string[] = [];
  if (scope.siteIds.length > 0) parts.push(getTargetButtonLabel(scope.siteIds.length, "site"));
  if (scope.buildingIds.length > 0) parts.push(getTargetButtonLabel(scope.buildingIds.length, "building"));
  if (scope.rackIds.length > 0) parts.push(getTargetButtonLabel(scope.rackIds.length, "rack"));
  if (scope.groupIds.length > 0) parts.push(getTargetButtonLabel(scope.groupIds.length, "group"));
  const minerCount = scope.deviceIdentifiers.length - containerCount;
  if (minerCount > 0) parts.push(getTargetButtonLabel(minerCount, "miner"));
  if (containerCount > 0) parts.push(getTargetButtonLabel(containerCount, "container"));
  return parts.length > 0 ? parts.join(", ") : "Nothing selected";
}

export const isScopeEmpty = (scope: ReleaseChannelScope): boolean =>
  scope.siteIds.length +
    scope.buildingIds.length +
    scope.rackIds.length +
    scope.groupIds.length +
    scope.deviceIdentifiers.length ===
  0;
