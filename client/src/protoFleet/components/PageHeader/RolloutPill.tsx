import type { ReactElement } from "react";
import { Link } from "react-router-dom";

import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  isAwaitingReview,
  isPaused,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutProgressSummary,
  rolloutStatusHeadline,
  scopeCounts,
} from "@/protoFleet/features/settings/components/RolloutLanes/rolloutStatus";
import CompositionBar from "@/shared/components/CompositionBar";

export const ROLLOUT_LANES_PATH = "/settings/firmware?tab=rollout-lanes";

interface RolloutPillProps {
  rollouts: Rollout[];
}

// Trigger copy leads with what needs a human: a rollout parked at its review
// gate outranks rollouts that are merely running.
function triggerLabel(rollouts: Rollout[], reviewCount: number): string {
  if (reviewCount === 1) return "Firmware update needs review";
  if (reviewCount > 1) return `${reviewCount} firmware updates need review`;
  return rollouts.length === 1 ? "Firmware update in progress" : `${rollouts.length} firmware updates in progress`;
}

function RolloutPill({ rollouts }: RolloutPillProps): ReactElement {
  const reviewCount = rollouts.filter(isAwaitingReview).length;
  return (
    <PageHeaderPopoverPill
      ariaLabel="View ongoing firmware rollouts"
      // Solid while a review is needed (waiting on you), pulsing while the
      // fleet is still being worked on.
      dotClassName={reviewCount > 0 ? "bg-intent-warning-fill" : "animate-pulse bg-intent-warning-fill"}
      triggerClassName="rollout-pill-trigger"
      triggerLabel={triggerLabel(rollouts, reviewCount)}
    >
      {({ closePopover }) => (
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-3">
            {rollouts.map((rollout) => {
              const counts = scopeCounts(rollout);
              const needsReview = isAwaitingReview(rollout);
              return (
                <div
                  key={rollout.id.toString()}
                  className="min-w-0 space-y-1.5"
                  data-testid={`rollout-pill-entry-${rollout.id.toString()}`}
                >
                  <div className="truncate text-heading-100 text-text-primary">{rollout.laneName}</div>
                  <div className="text-200 leading-snug text-text-primary-70">
                    {`${rollout.model} → ${rollout.firmwareVersion}`}
                  </div>
                  {needsReview || isPaused(rollout) ? (
                    <div
                      className={
                        needsReview
                          ? "text-200 leading-snug text-intent-warning-text"
                          : "text-200 leading-snug text-text-primary-70"
                      }
                    >
                      {rolloutStatusHeadline(rollout)}
                    </div>
                  ) : (
                    <div className="text-200 leading-snug text-text-primary-70">{rolloutProgressSummary(counts)}</div>
                  )}
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
              View release channels
            </Link>
          </div>
        </div>
      )}
    </PageHeaderPopoverPill>
  );
}

export default RolloutPill;
