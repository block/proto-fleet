import {
  type FirmwareRollout,
  FirmwareRolloutState,
  FirmwareRolloutTargetState,
} from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";

export const firmwareRolloutStateConfigs = {
  draft: { label: "Draft", dotClassName: "bg-core-accent-fill", order: 0 },
  running: { label: "Running", dotClassName: "bg-intent-warning-fill", order: 1 },
  paused: { label: "Paused", dotClassName: "bg-text-primary-30", order: 2 },
  completed: { label: "Completed", dotClassName: "bg-intent-success-fill", order: 3 },
  completedWithFailures: { label: "Completed with failures", dotClassName: "bg-intent-critical-fill", order: 4 },
  canceled: { label: "Aborted", dotClassName: "bg-intent-critical-fill", order: 5 },
} as const;

export type FirmwareRolloutStateKey = keyof typeof firmwareRolloutStateConfigs;

export function firmwareRolloutStateKey(state: FirmwareRolloutState): FirmwareRolloutStateKey {
  switch (state) {
    case FirmwareRolloutState.RUNNING:
      return "running";
    case FirmwareRolloutState.PAUSED:
      return "paused";
    case FirmwareRolloutState.COMPLETED:
      return "completed";
    case FirmwareRolloutState.COMPLETED_WITH_FAILURES:
      return "completedWithFailures";
    case FirmwareRolloutState.CANCELED:
      return "canceled";
    case FirmwareRolloutState.DRAFT:
    default:
      return "draft";
  }
}

export function firmwareRolloutStateConfig(state: FirmwareRolloutState) {
  return firmwareRolloutStateConfigs[firmwareRolloutStateKey(state)];
}

const activeStates = new Set<FirmwareRolloutState>([
  FirmwareRolloutState.DRAFT,
  FirmwareRolloutState.RUNNING,
  FirmwareRolloutState.PAUSED,
]);

/** A rollout is "active" while it can still dispatch (draft/running/paused). */
export function isActiveRolloutState(state: FirmwareRolloutState): boolean {
  return activeStates.has(state);
}

export function isTerminalRolloutState(state: FirmwareRolloutState): boolean {
  return !isActiveRolloutState(state);
}

export const firmwareRolloutTargetStateConfigs: Record<
  FirmwareRolloutTargetState,
  { label: string; dotClassName: string }
> = {
  [FirmwareRolloutTargetState.UNSPECIFIED]: { label: "Unknown", dotClassName: "bg-text-primary-30" },
  [FirmwareRolloutTargetState.PENDING]: { label: "Pending", dotClassName: "bg-core-accent-fill" },
  [FirmwareRolloutTargetState.DISPATCHING]: { label: "Dispatching", dotClassName: "bg-intent-warning-fill" },
  [FirmwareRolloutTargetState.DISPATCHED]: { label: "In progress", dotClassName: "bg-intent-warning-fill" },
  [FirmwareRolloutTargetState.SUCCEEDED]: { label: "Succeeded", dotClassName: "bg-intent-success-fill" },
  [FirmwareRolloutTargetState.FAILED]: { label: "Failed", dotClassName: "bg-intent-critical-fill" },
  [FirmwareRolloutTargetState.CANCELED]: { label: "Aborted", dotClassName: "bg-intent-critical-fill" },
};

export function firmwareRolloutTargetStateConfig(state: FirmwareRolloutTargetState) {
  return (
    firmwareRolloutTargetStateConfigs[state] ??
    firmwareRolloutTargetStateConfigs[FirmwareRolloutTargetState.UNSPECIFIED]
  );
}

export interface RolloutProgress {
  total: number;
  processed: number;
  success: number;
  failure: number;
  canceled: number;
  pending: number;
  inProgress: number;
  retried: number;
  /** Percent of targets that reached a terminal per-miner state. */
  percent: number;
  successPercent: number;
  failurePercent: number;
}

function clampPercent(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.min(Math.max(value, 0), 100);
}

export function getRolloutProgress(rollout: FirmwareRollout): RolloutProgress {
  const counts = rollout.counts;
  const total = counts?.totalCount ?? rollout.targetCount ?? 0;
  const success = counts?.successCount ?? 0;
  const failure = counts?.failureCount ?? 0;
  const canceled = counts?.canceledCount ?? 0;
  const pending = counts?.pendingCount ?? 0;
  const inProgress = counts?.inProgressCount ?? 0;
  const retried = counts?.retriedCount ?? 0;
  const processed = success + failure + canceled;
  return {
    total,
    processed,
    success,
    failure,
    canceled,
    pending,
    inProgress,
    retried,
    percent: total > 0 ? clampPercent((processed / total) * 100) : 0,
    successPercent: total > 0 ? clampPercent((success / total) * 100) : 0,
    failurePercent: total > 0 ? clampPercent(((failure + canceled) / total) * 100) : 0,
  };
}

export function formatRolloutTimestamp(seconds?: bigint): string {
  if (!seconds) return "—";
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(Number(seconds) * 1000));
}
