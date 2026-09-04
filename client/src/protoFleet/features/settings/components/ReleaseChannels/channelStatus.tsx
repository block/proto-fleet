import clsx from "clsx";

import { modelUpdateStatus, type UpdateStatus, type UpdateTone } from "./rolloutStatus";
import type { ReleaseChannelModelGroup, Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

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

export const ModelStatusCell = ({
  group,
  activeRollout,
  lastFinished,
}: {
  group: ReleaseChannelModelGroup;
  activeRollout?: Rollout;
  // Most recent finished rollout for this model group, for "Updated <date>".
  lastFinished?: Rollout;
}) => <StatusCell status={modelUpdateStatus(group, activeRollout, lastFinished)} />;
