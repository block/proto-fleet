import type { ReleaseChannelScope } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";

// Repeated-field bounds from ReleaseChannelScope apply to both preview and
// writes. Keep every selected ID so operators can reduce an oversized draft.
export function scopeValidationErrors(scope: ReleaseChannelScope): string[] {
  const dimensions = [
    { ids: scope.siteIds, limit: 100, label: "sites" },
    { ids: scope.buildingIds, limit: 100, label: "buildings" },
    { ids: scope.rackIds, limit: 500, label: "racks" },
    { ids: scope.groupIds, limit: 100, label: "groups" },
    { ids: scope.deviceIdentifiers, limit: 10000, label: "miners" },
  ];
  return dimensions
    .filter(({ ids, limit }) => ids.length > limit)
    .map(
      ({ ids, limit, label }) =>
        `Select no more than ${limit.toLocaleString()} ${label} (${ids.length.toLocaleString()} selected).`,
    );
}

// One label per populated dimension ("2 racks, 5 miners"); "No miners
// selected" when the scope is empty.
export function scopeSummary(scope: ReleaseChannelScope): string {
  const parts: string[] = [];
  if (scope.siteIds.length > 0) parts.push(getTargetButtonLabel(scope.siteIds.length, "site"));
  if (scope.buildingIds.length > 0) parts.push(getTargetButtonLabel(scope.buildingIds.length, "building"));
  if (scope.rackIds.length > 0) parts.push(getTargetButtonLabel(scope.rackIds.length, "rack"));
  if (scope.groupIds.length > 0) parts.push(getTargetButtonLabel(scope.groupIds.length, "group"));
  if (scope.deviceIdentifiers.length > 0) parts.push(getTargetButtonLabel(scope.deviceIdentifiers.length, "miner"));
  return parts.length > 0 ? parts.join(", ") : "No miners selected";
}

export const isScopeEmpty = (scope: ReleaseChannelScope): boolean =>
  scope.siteIds.length +
    scope.buildingIds.length +
    scope.rackIds.length +
    scope.groupIds.length +
    scope.deviceIdentifiers.length ===
  0;
