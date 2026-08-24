import type { RolloutGroup, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

const acknowledgedRolloutResultIdStorageKey = "protoFleet.acknowledgedRolloutResultId";
const acknowledgedRolloutGroupResultStorageKey = "protoFleet.acknowledgedRolloutGroupResult";

export interface AcknowledgedRolloutGroupResult {
  parentId: string;
  resultRevision: string;
}

export function useAcknowledgedRolloutResultId() {
  return useReactiveLocalStorage<string | undefined>(acknowledgedRolloutResultIdStorageKey, undefined);
}

export function useAcknowledgedRolloutGroupResult() {
  return useReactiveLocalStorage<AcknowledgedRolloutGroupResult | undefined>(
    acknowledgedRolloutGroupResultStorageKey,
    undefined,
  );
}

export function isRolloutGroupResultAcknowledged(
  group: RolloutGroup,
  acknowledged?: AcknowledgedRolloutGroupResult,
): boolean {
  return (
    group.resultReady &&
    acknowledged?.parentId === group.id &&
    acknowledged.resultRevision === group.resultRevision.toString()
  );
}

export function rolloutGroupAcknowledgement(group: RolloutGroup): AcknowledgedRolloutGroupResult | undefined {
  return group.resultReady
    ? {
        parentId: group.id,
        resultRevision: group.resultRevision.toString(),
      }
    : undefined;
}

export function isCompletedRolloutResult(rollout: RolloutRecord): boolean {
  return rollout.state === "completed" || rollout.state === "completedWithFailures" || rollout.state === "reverted";
}

function completedAtMillis(rollout: RolloutRecord): number {
  const timestamp = rollout.revertedAt ?? rollout.completedAt ?? rollout.updatedAt ?? rollout.createdAt;
  if (!timestamp) {
    return Number.NEGATIVE_INFINITY;
  }
  const milliseconds = Date.parse(timestamp);
  return Number.isNaN(milliseconds) ? Number.NEGATIVE_INFINITY : milliseconds;
}

export function latestCompletedRolloutResult(rollouts: RolloutRecord[]): RolloutRecord | undefined {
  return rollouts.filter(isCompletedRolloutResult).reduce<RolloutRecord | undefined>((latest, rollout) => {
    if (!latest || completedAtMillis(rollout) > completedAtMillis(latest)) {
      return rollout;
    }
    return latest;
  }, undefined);
}
