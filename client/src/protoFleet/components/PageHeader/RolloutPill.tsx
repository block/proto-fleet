import type { ReactElement } from "react";
import { Link } from "react-router-dom";

import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  rolloutDeviceCounts,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutProgressSummary,
} from "@/protoFleet/features/settings/components/RolloutLanes/rolloutStatus";
import CompositionBar from "@/shared/components/CompositionBar";

export const ROLLOUT_LANES_PATH = "/settings/firmware?tab=rollout-lanes";

interface RolloutPillProps {
  rollouts: Rollout[];
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
              const counts = rolloutDeviceCounts(rollout);
              return (
                <div key={rollout.id.toString()} className="min-w-0 space-y-1.5">
                  <div className="truncate text-heading-100 text-text-primary">{rollout.laneName}</div>
                  <div className="text-200 leading-snug text-text-primary-70">
                    {`${rollout.model} → ${rollout.firmwareVersion}`}
                  </div>
                  <div className="text-200 leading-snug text-text-primary-70">{rolloutProgressSummary(counts)}</div>
                  <CompositionBar
                    segments={rolloutProgressSegments(counts)}
                    height={6}
                    colorMap={rolloutProgressColorMap}
                  />
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
