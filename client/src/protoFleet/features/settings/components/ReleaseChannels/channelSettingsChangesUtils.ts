import { behaviorForComparison } from "./behaviorUtils";
import {
  capitalize,
  type DeviceNouns,
  formatDurationSeconds,
  methodLabels,
  MINER_NOUNS,
  nounCountLabel,
  orderLabels,
} from "./rolloutStatus";
import { sameSelection, scopeSelectionFields } from "./scopeUtils";
import { type RolloutBehavior, RolloutMethod } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { gatesAfterBatch, hasSampledLimit } from "@/protoFleet/api/rolloutBehavior";
import type { ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";
import { trimMinerTarget } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";

// Keep the normal duration vocabulary while making sub-minute differences
// visible even when the shared formatter rounds them to the same minute.
const duration = (seconds: number): string =>
  `${formatDurationSeconds(seconds)}${seconds >= 60 && seconds % 60 !== 0 ? ` (${seconds.toLocaleString()}s)` : ""}`;
const optionalLimit = (value: number | undefined, unit = ""): string =>
  value === undefined ? "No limit" : `${value}${unit}`;

function behaviorValues(draft: RolloutBehavior, nouns: DeviceNouns): Record<string, string> {
  const devices = (count: number): string => nounCountLabel(nouns, count);
  const behavior = behaviorForComparison(draft);
  const batched = behavior.method === RolloutMethod.BATCHED;
  const autoContinue = behavior.autoContinueOnHealthyTelemetry;
  const thresholds = behavior.thresholds;
  return {
    "Update method": methodLabels[behavior.method],
    "Update order": orderLabels[behavior.order],
    "Batch size": batched ? devices(behavior.batchSize) : "Not used",
    "Pilot size": behavior.method === RolloutMethod.PILOT_THEN_CONTINUE ? devices(behavior.pilotSize) : "Not used",
    "Review gates":
      behavior.method === RolloutMethod.PILOT_THEN_CONTINUE
        ? "After the pilot batch"
        : behavior.reviewAfterEachBatch
          ? "After each batch"
          : "None",
    "Wait between batches":
      batched && !behavior.reviewAfterEachBatch ? duration(behavior.waitBetweenBatchesSeconds) : "Not used",
    "Continue automatically": gatesAfterBatch(behavior)
      ? autoContinue
        ? "When telemetry is healthy"
        : "Off"
      : "Not used",
    "Stabilization time": autoContinue ? duration(behavior.stabilizationSeconds) : "Not used",
    "Offline limit": behavior.maxConcurrentOffline === 0 ? "Unlimited" : devices(behavior.maxConcurrentOffline),
    "Controller timeout":
      behavior.method === RolloutMethod.DELEGATED
        ? behavior.controllerTimeoutSeconds === 0
          ? "Never"
          : duration(behavior.controllerTimeoutSeconds)
        : "Not used",
    "Maximum hashrate drop": autoContinue ? optionalLimit(thresholds?.maxHashrateDropPercent, "%") : "Not used",
    "Maximum efficiency increase": autoContinue
      ? optionalLimit(thresholds?.maxEfficiencyIncreasePercent, "%")
      : "Not used",
    "Maximum temperature increase": autoContinue
      ? optionalLimit(thresholds?.maxTemperatureIncreaseCelsius, "°C")
      : "Not used",
    "Maximum new errors": autoContinue ? optionalLimit(thresholds?.maxNewErrors) : "Not used",
    "Minimum sample coverage":
      autoContinue && hasSampledLimit(thresholds)
        ? thresholds?.minSampleCoveragePercent === undefined
          ? "100% (default)"
          : `${thresholds.minSampleCoveragePercent}%`
        : "Not used",
  };
}

const selectorLabels = {
  siteIds: ["Sites", "Site"],
  buildingIds: ["Buildings", "Building"],
  rackIds: ["Racks", "Rack"],
  groupIds: ["Groups", "Group"],
} as const;

export interface MinerScopeChanges {
  // What the directly selected devices are called in this channel.
  nouns: DeviceNouns;
  originalCount: number;
  targetCount: number;
  addedCount: number;
  removedCount: number;
  miners: { identifier: string; name: string; original: boolean; target: boolean }[];
}

function compareMiners(
  before: string[],
  after: string[],
  minerNames: Record<string, string>,
  nouns: DeviceNouns,
): MinerScopeChanges | null {
  if (sameSelection(before, after)) return null;
  const oldIds = new Set(before);
  const newIds = new Set(after);
  const selections = [...new Set([...oldIds, ...newIds])].sort().map((identifier) => ({
    identifier,
    name: (Object.prototype.hasOwnProperty.call(minerNames, identifier) && minerNames[identifier]) || identifier,
    original: oldIds.has(identifier),
    target: newIds.has(identifier),
  }));
  return {
    nouns,
    originalCount: oldIds.size,
    targetCount: newIds.size,
    addedCount: selections.filter((miner) => !miner.original).length,
    removedCount: selections.filter((miner) => !miner.target).length,
    miners: selections,
  };
}

export function getChannelSettingsChanges(
  before: ReleaseChannelDraft,
  after: ReleaseChannelDraft,
  minerNames: Record<string, string>,
  // What the channel's devices are called: miners unless it holds containers.
  nouns: DeviceNouns = MINER_NOUNS,
) {
  const previous: Record<string, string> = {
    Name: trimMinerTarget(before.name),
    Description: trimMinerTarget(before.description),
    ...behaviorValues(before.behavior, nouns),
  };
  const next: Record<string, string> = {
    Name: trimMinerTarget(after.name),
    Description: trimMinerTarget(after.description),
    ...behaviorValues(after.behavior, nouns),
  };
  const changes = Object.entries(next)
    .filter(([label, value]) => previous[label] !== value)
    .map(([label, value]) => ({ label, before: previous[label] || "None", after: value || "None" }));
  const minerChanges = compareMiners(before.scope.deviceIdentifiers, after.scope.deviceIdentifiers, minerNames, nouns);
  const scopeChanges = scopeSelectionFields.flatMap((field) => {
    if (sameSelection(before.scope[field], after.scope[field])) return [];
    const oldIds = new Set(before.scope[field].map(String));
    const newIds = new Set(after.scope[field].map(String));
    if (field === "deviceIdentifiers") {
      return [
        {
          label: capitalize(nouns.plural),
          before: nounCountLabel(nouns, oldIds.size),
          after: nounCountLabel(nouns, newIds.size),
        },
      ];
    }
    const [label, singular] = selectorLabels[field];
    const describe = (id: string): string => `${singular} ${id}`;
    const selection = (ids: Set<string>): string => [...ids].sort().map(describe).join("\n") || "None";
    return [{ label, before: selection(oldIds), after: selection(newIds) }];
  });
  return { changes, scopeChanges, minerChanges };
}
