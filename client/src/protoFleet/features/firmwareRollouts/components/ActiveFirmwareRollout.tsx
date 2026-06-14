import type { ReactElement, ReactNode } from "react";

import {
  type FirmwareRollout,
  FirmwareRolloutState,
} from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import FirmwareRolloutStatusPill from "@/protoFleet/features/firmwareRollouts/components/FirmwareRolloutStatusPill";
import { getRolloutProgress } from "@/protoFleet/features/firmwareRollouts/firmwareRolloutDisplayUtils";
import { Download, Pause } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";

export type ActiveRolloutAction = "pause" | "resume" | "abort" | "retry";

interface ActiveFirmwareRolloutProps {
  rollout: FirmwareRollout;
  pending?: boolean;
  onAction: (rollout: FirmwareRollout, action: ActiveRolloutAction) => void;
  onViewDetails: (rollout: FirmwareRollout) => void;
}

function StatBlock({ label, value }: { label: string; value: ReactNode }): ReactElement {
  return (
    <div className="min-w-0">
      <div className="text-200 text-text-primary-50">{label}</div>
      <div className="mt-1 truncate text-emphasis-300 text-text-primary">{value}</div>
    </div>
  );
}

const ActiveFirmwareRollout = ({
  rollout,
  pending = false,
  onAction,
  onViewDetails,
}: ActiveFirmwareRolloutProps): ReactElement => {
  const progress = getRolloutProgress(rollout);
  const isPaused = rollout.state === FirmwareRolloutState.PAUSED;
  const canRetry = progress.failure > 0 && isPaused;

  const statusIcon = isPaused ? (
    <Pause className="text-text-primary-70" />
  ) : (
    <ProgressCircular value={progress.percent} className="text-core-primary-fill" />
  );

  return (
    <div className="rounded-xl bg-surface-elevated-base p-6 shadow-100 tablet:p-10">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="grid min-w-0 gap-3">
          <div className="flex size-10 items-center justify-center rounded-lg bg-core-primary-5">{statusIcon}</div>
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-heading-50 text-text-primary-70">
              <Download className="size-4" />
              Firmware rollout
            </div>
            <div className="truncate text-heading-300 text-text-primary" title={rollout.name}>
              {rollout.name}
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <FirmwareRolloutStatusPill state={rollout.state} />
              <span className="text-200 text-text-primary-50">
                {rollout.minerModel || "—"} · batch {rollout.batchSize} every {rollout.batchIntervalSeconds}s
              </span>
            </div>
          </div>
        </div>

        <div className="flex flex-wrap gap-2">
          {rollout.state === FirmwareRolloutState.RUNNING ? (
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="Pause"
              disabled={pending}
              onClick={() => onAction(rollout, "pause")}
            />
          ) : null}
          {isPaused ? (
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="Resume"
              disabled={pending}
              onClick={() => onAction(rollout, "resume")}
            />
          ) : null}
          {canRetry ? (
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="Retry failed"
              disabled={pending}
              onClick={() => onAction(rollout, "retry")}
            />
          ) : null}
          <Button
            variant={variants.secondaryDanger}
            size={sizes.compact}
            text="Abort"
            disabled={pending}
            onClick={() => onAction(rollout, "abort")}
          />
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="View details"
            onClick={() => onViewDetails(rollout)}
          />
        </div>
      </div>

      <div className="mt-8 grid gap-2">
        <div className="flex items-center justify-between text-200 text-text-primary-70">
          <span>
            {progress.processed.toLocaleString()} of {progress.total.toLocaleString()} miners processed
          </span>
          <span>{Math.round(progress.percent)}%</span>
        </div>
        <div className="flex h-3 w-full overflow-hidden rounded-full bg-core-primary-10">
          <div className="bg-intent-success-fill" style={{ width: `${progress.successPercent}%` }} />
          <div className="bg-intent-critical-fill" style={{ width: `${progress.failurePercent}%` }} />
        </div>
      </div>

      <div className="mt-8 grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-4">
        <StatBlock label="Succeeded" value={progress.success.toLocaleString()} />
        <StatBlock label="Failed" value={progress.failure.toLocaleString()} />
        <StatBlock label="In progress" value={progress.inProgress.toLocaleString()} />
        <StatBlock label="Pending" value={progress.pending.toLocaleString()} />
      </div>
    </div>
  );
};

export default ActiveFirmwareRollout;
