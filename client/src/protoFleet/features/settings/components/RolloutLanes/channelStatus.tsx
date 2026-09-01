import clsx from "clsx";

import { isAwaitingReview, isPilotStage, pilotCohortCounts, rolloutDeviceCounts } from "./rolloutStatus";
import type { Rollout, RolloutLaneModelGroup } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

// Status language shared between the release channels overview table and
// the per-channel manage view.

const StatusDot = ({ className }: { className: string }) => (
  <span className={clsx("inline-block size-2 shrink-0 rounded-full", className)} />
);

export const StatusCell = ({ dotClassName, label }: { dotClassName: string; label: string }) => (
  <span className="inline-flex items-center gap-2 whitespace-nowrap">
    <StatusDot className={dotClassName} />
    {label}
  </span>
);

export const ModelStatusCell = ({
  group,
  activeRollout,
}: {
  group: RolloutLaneModelGroup;
  activeRollout?: Rollout;
}) => {
  if (activeRollout && isPilotStage(activeRollout)) {
    const counts = pilotCohortCounts(activeRollout);
    return (
      <StatusCell
        dotClassName="animate-pulse bg-intent-warning-fill"
        label={`Pilot: updating ${counts.updated} of ${counts.total}`}
      />
    );
  }
  if (activeRollout && isAwaitingReview(activeRollout)) {
    return <StatusCell dotClassName="bg-intent-warning-fill" label="Pilot complete — review needed" />;
  }
  if (activeRollout) {
    const counts = rolloutDeviceCounts(activeRollout);
    return (
      <StatusCell
        dotClassName="animate-pulse bg-intent-warning-fill"
        label={`Updating, ${counts.updated} of ${counts.total}`}
      />
    );
  }
  if (group.firmwareVersion === "") {
    return <StatusCell dotClassName="bg-core-primary-10" label="No firmware assigned" />;
  }
  if (group.miners.length === 0) {
    return <StatusCell dotClassName="bg-core-primary-10" label="No miners" />;
  }
  const onTarget = group.miners.filter((miner) => miner.firmwareVersion === group.firmwareVersion).length;
  if (onTarget === group.miners.length) {
    return <StatusCell dotClassName="bg-intent-healthy-fill" label="Up to date" />;
  }
  return <StatusCell dotClassName="bg-core-primary-10" label={`${onTarget} of ${group.miners.length} on target`} />;
};
