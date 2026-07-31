import type { ReactElement } from "react";

import type { RolloutTargetPhase } from "./rolloutTypes";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";

interface RolloutColumnStateProps {
  phase: RolloutTargetPhase;
  /** Value shown for a settled target, e.g. the new firmware version "5.1.0".
   * Falls back to a phase word when omitted. */
  doneLabel?: string;
  /** Pre-rollout value echoed while a target is still queued, e.g. the current
   * firmware version. */
  idleLabel?: string;
}

/** Map a rollout phase to the shared StatusCircle status color. */
const phaseStatus: Record<RolloutTargetPhase, keyof typeof statuses> = {
  done: statuses.normal,
  inProgress: statuses.error, // amber/attention dot, matching MinerStatus "Updating firmware"
  retrying: statuses.warning,
  queued: statuses.inactive,
  failed: statuses.error,
  excluded: statuses.inactive,
};

/**
 * Per-miner state cell for the process column (e.g. the Firmware column in the
 * fleet table). Built from the same primitives as `MinerStatus` — a simple
 * `StatusCircle` dot, an optional inline `ProgressCircular` spinner, and plain
 * text — so a rollout's per-row state reads identically to native miner status
 * rather than as a bespoke chip.
 */
function RolloutColumnState({ phase, doneLabel, idleLabel }: RolloutColumnStateProps): ReactElement {
  const dot = (
    <StatusCircle status={phaseStatus[phase]} variant="simple" width="w-[6px]" testId="rollout-column-status" />
  );

  if (phase === "inProgress") {
    return (
      <div className="flex items-center gap-2 text-text-primary">
        {dot}
        <ProgressCircular size={14} indeterminate />
        Updating
      </div>
    );
  }

  if (phase === "retrying") {
    return (
      <div className="flex items-center gap-2 text-text-primary">
        {dot}
        <ProgressCircular size={14} indeterminate />
        Retrying
      </div>
    );
  }

  if (phase === "done") {
    return (
      <div className="flex items-center gap-2 text-text-primary">
        {dot}
        {doneLabel ?? "Done"}
      </div>
    );
  }

  if (phase === "failed") {
    return (
      <div className="flex items-center gap-2 text-text-primary">
        {dot}
        Failed
      </div>
    );
  }

  if (phase === "excluded") {
    return (
      <div className="flex items-center gap-2 text-text-primary-50">
        {dot}
        Excluded
      </div>
    );
  }

  // queued
  return (
    <div className="flex items-center gap-2 text-text-primary-70">
      {dot}
      {idleLabel ? `Queued (${idleLabel})` : "Queued"}
    </div>
  );
}

export default RolloutColumnState;
