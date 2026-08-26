import type { ReactElement } from "react";
import { Link } from "react-router-dom";

import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import { type Rollout, RolloutDeviceState } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

export const ROLLOUT_LANES_PATH = "/settings/firmware?tab=rollout-lanes";

interface RolloutPillProps {
  rollouts: Rollout[];
}

function rolloutProgress(rollout: Rollout): { updated: number; total: number; percent: number } {
  const total = rollout.devices.length;
  const updated = rollout.devices.filter((device) => device.state === RolloutDeviceState.UPDATED).length;
  return { updated, total, percent: total === 0 ? 0 : Math.round((updated / total) * 100) };
}

function RolloutPill({ rollouts }: RolloutPillProps): ReactElement {
  return (
    <PageHeaderPopoverPill
      ariaLabel="View ongoing firmware rollouts"
      dotClassName="animate-pulse bg-intent-warning-fill"
      triggerClassName="rollout-pill-trigger"
      triggerLabel={rollouts.length === 1 ? "Rollout in progress" : `${rollouts.length} rollouts in progress`}
    >
      {({ closePopover }) => (
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-3">
            {rollouts.map((rollout) => {
              const { updated, total, percent } = rolloutProgress(rollout);
              return (
                <div key={rollout.id.toString()} className="min-w-0 space-y-1.5">
                  <div className="truncate text-heading-100 text-text-primary">{rollout.laneName}</div>
                  <div className="text-200 leading-snug text-text-primary-70">
                    {`${rollout.model} → ${rollout.firmwareVersion} — ${updated}/${total} updated`}
                  </div>
                  <div className="h-1.5 w-full overflow-hidden rounded-full bg-core-primary-5">
                    <div
                      className="h-full rounded-full bg-intent-success-fill transition-all"
                      style={{ width: `${percent}%` }}
                    />
                  </div>
                </div>
              );
            })}
          </div>

          <div className="border-t border-border-5 pt-3">
            <Link
              to={ROLLOUT_LANES_PATH}
              onClick={closePopover}
              className="block rounded-xl px-3 py-2.5 text-emphasis-300 text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
            >
              View rollout lanes
            </Link>
          </div>
        </div>
      )}
    </PageHeaderPopoverPill>
  );
}

export default RolloutPill;
