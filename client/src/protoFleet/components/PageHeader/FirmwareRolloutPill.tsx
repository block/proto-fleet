import type { ReactElement } from "react";
import { Link } from "react-router-dom";

import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import {
  type FirmwareRollout,
  FirmwareRolloutState,
} from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import {
  firmwareRolloutStateConfig,
  getRolloutProgress,
} from "@/protoFleet/features/firmwareRollouts/firmwareRolloutDisplayUtils";

const firmwareRolloutsPath = "/firmware-rollouts";

interface FirmwareRolloutPillProps {
  rollouts: FirmwareRollout[];
}

function pickPrimary(rollouts: FirmwareRollout[]): FirmwareRollout {
  return rollouts.find((rollout) => rollout.state === FirmwareRolloutState.RUNNING) ?? rollouts[0];
}

function FirmwareRolloutPill({ rollouts }: FirmwareRolloutPillProps): ReactElement | null {
  if (rollouts.length === 0) return null;

  const primary = pickPrimary(rollouts);
  const primaryConfig = firmwareRolloutStateConfig(primary.state);
  const triggerLabel =
    rollouts.length > 1
      ? `${rollouts.length} firmware rollouts active`
      : `Firmware rollout ${primaryConfig.label.toLowerCase()}`;

  return (
    <PageHeaderPopoverPill
      ariaLabel="View firmware rollout details"
      dotClassName={primaryConfig.dotClassName}
      triggerClassName="firmware-rollout-pill-trigger"
      triggerLabel={triggerLabel}
    >
      {({ closePopover }) => (
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-3">
            {rollouts.map((rollout) => {
              const config = firmwareRolloutStateConfig(rollout.state);
              const progress = getRolloutProgress(rollout);
              return (
                <div key={rollout.rolloutId} className="min-w-0 space-y-1">
                  <div className="truncate text-heading-100 text-text-primary" title={rollout.name}>
                    {rollout.name}
                  </div>
                  <div className="text-200 leading-snug text-text-primary-70">
                    {rollout.minerModel || "—"} · {config.label}
                  </div>
                  <div className="text-200 leading-snug text-text-primary-70">
                    {progress.processed.toLocaleString()} / {progress.total.toLocaleString()} miners ·{" "}
                    {Math.round(progress.percent)}%
                  </div>
                </div>
              );
            })}
          </div>

          <div className="border-t border-border-5 pt-3">
            <Link
              to={firmwareRolloutsPath}
              onClick={closePopover}
              className="block rounded-xl px-3 py-2.5 text-emphasis-300 text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
            >
              View rollouts
            </Link>
          </div>
        </div>
      )}
    </PageHeaderPopoverPill>
  );
}

export default FirmwareRolloutPill;
