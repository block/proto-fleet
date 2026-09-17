import type { ReleaseChannelScope } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";

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
