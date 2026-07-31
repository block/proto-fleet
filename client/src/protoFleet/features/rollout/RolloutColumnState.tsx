import type { ReactElement } from "react";
import clsx from "clsx";

import type { RolloutTargetPhase } from "./rolloutTypes";
import { Alert, Checkmark } from "@/shared/assets/icons";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface RolloutColumnStateProps {
  phase: RolloutTargetPhase;
  /** The value shown for a settled target, e.g. the firmware version "5.1.0".
   * When omitted, a phase label is shown instead. */
  doneLabel?: string;
  /** The plain value shown when the target is untouched (e.g. current version).
   * Only used for the "queued" phase to echo the pre-rollout value. */
  idleLabel?: string;
}

const dotClass: Record<RolloutTargetPhase, string> = {
  done: "bg-intent-success-fill",
  inProgress: "bg-intent-warning-fill",
  queued: "bg-grayscale-gray-50",
  failed: "bg-intent-critical-fill",
  excluded: "bg-grayscale-gray-50",
};

/**
 * Per-miner state cell for the process column (e.g. the Firmware column in the
 * fleet table): a small status chip that reads the target's phase within an
 * active rollout. Done shows the achieved value (new firmware version); the
 * other phases show a labeled dot.
 */
function RolloutColumnState({ phase, doneLabel, idleLabel }: RolloutColumnStateProps): ReactElement {
  if (phase === "inProgress") {
    return (
      <span className="inline-flex items-center gap-1.5 text-200 text-text-primary-70">
        <ProgressCircular indeterminate size={14} className="text-core-accent-fill" />
        Updating
      </span>
    );
  }

  if (phase === "done") {
    return (
      <span className="inline-flex items-center gap-1.5 text-200 text-intent-success-fill">
        <Checkmark width="w-3" />
        {doneLabel ?? "Done"}
      </span>
    );
  }

  if (phase === "failed") {
    return (
      <span className="inline-flex items-center gap-1.5 text-200 text-intent-critical-fill">
        <Alert width="w-3" />
        Failed
      </span>
    );
  }

  if (phase === "excluded") {
    return <span className="text-200 text-text-primary-50">Excluded</span>;
  }

  // queued
  return (
    <span className="inline-flex items-center gap-1.5 text-200 text-text-primary-70">
      <span className={clsx("h-2 w-2 rounded-full", dotClass.queued)} />
      {idleLabel ? `Queued (${idleLabel})` : "Queued"}
    </span>
  );
}

export default RolloutColumnState;
