import clsx from "clsx";

import { hasUnavailableAssignedFirmware, modelUpdateStatus, type UpdateStatus, type UpdateTone } from "./rolloutStatus";
import type { ChannelHistoryState } from "./useChannelHistory";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

import type { ChannelModelGroupView as ReleaseChannelModelGroup } from "@/protoFleet/api/useReleaseChannels";

// Status language shared between the release channels overview table and
// the per-channel manage view. Tones follow the reference design: attention
// is critical red, active is primary, completed is success, none is muted.

const updateToneDotClasses: Record<UpdateTone, string> = {
  attention: "bg-intent-critical-fill",
  active: "bg-core-primary-fill",
  completed: "bg-intent-success-fill",
  none: "bg-core-primary-20",
};

const StatusDot = ({ className }: { className: string }) => (
  <span className={clsx("inline-block size-2 shrink-0 rounded-full", className)} />
);

export const StatusCell = ({
  status,
  emphasized = false,
  testId,
}: {
  status: UpdateStatus;
  // Channel rows read heavier than their model rows.
  emphasized?: boolean;
  testId?: string;
}) => (
  <span
    className={clsx(
      "inline-flex items-center gap-2 whitespace-nowrap text-text-primary",
      emphasized && "text-emphasis-300",
    )}
    data-testid={testId}
  >
    <StatusDot className={updateToneDotClasses[status.tone]} />
    {status.label}
  </span>
);

function modelStatusWithHistory(
  group: ReleaseChannelModelGroup,
  activeRollout: Rollout | undefined,
  lastFinished: Rollout | undefined,
  historyState?: ChannelHistoryState,
): UpdateStatus {
  if (
    !group.rollbackPending &&
    historyState &&
    historyState.status !== "ready" &&
    !activeRollout &&
    group.activeRolloutId === 0n &&
    !hasUnavailableAssignedFirmware(group) &&
    group.firmwareVersion !== "" &&
    group.minerCount > 0
  ) {
    return historyState.status === "error"
      ? { label: "Update history unavailable", tone: "attention" }
      : { label: "Loading update history", tone: "none" };
  }
  return modelUpdateStatus(group, activeRollout, lastFinished);
}

export const ModelStatusCell = ({
  group,
  activeRollout,
  lastFinished,
  historyState,
  testId,
}: {
  group: ReleaseChannelModelGroup;
  activeRollout?: Rollout;
  // Most recent finished rollout for this model group, for "Updated <date>".
  lastFinished?: Rollout;
  historyState?: ChannelHistoryState;
  testId?: string;
}) => <StatusCell status={modelStatusWithHistory(group, activeRollout, lastFinished, historyState)} testId={testId} />;
