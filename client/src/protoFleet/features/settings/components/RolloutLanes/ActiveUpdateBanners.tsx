import {
  attentionDevices,
  isAwaitingReview,
  isBatchStage,
  isPaused,
  rolloutDeviceCounts,
  rolloutStageLabel,
} from "./rolloutStatus";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { Download } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";

interface ActiveUpdateBannersProps {
  // Ongoing rollouts, most recently started first.
  rollouts: Rollout[];
  onViewUpdate: (rollout: Rollout) => void;
}

// "74 of 87 miners updated, 2 need attention, Batch 5 of 6"
function bannerSubtitle(rollout: Rollout): string {
  const counts = rolloutDeviceCounts(rollout);
  const attention = attentionDevices(rollout).length;
  const parts = [`${counts.updated.toLocaleString()} of ${counts.total.toLocaleString()} miners updated`];
  if (attention > 0) parts.push(attention === 1 ? "1 needs attention" : `${attention.toLocaleString()} need attention`);
  if (isPaused(rollout) || isAwaitingReview(rollout) || isBatchStage(rollout)) parts.push(rolloutStageLabel(rollout));
  return parts.join(", ");
}

// Inline progress banners for ongoing firmware updates, one per rollout,
// stacked above the firmware page tabs so they are visible from Files and
// Release channels alike. Each opens the full update detail.
const ActiveUpdateBanners = ({ rollouts, onViewUpdate }: ActiveUpdateBannersProps) => {
  if (rollouts.length === 0) return null;
  return (
    <div className="flex flex-col gap-3" data-testid="active-updates-section">
      {rollouts.map((rollout) => (
        <div key={rollout.id.toString()} data-testid={`active-update-${rollout.id.toString()}`}>
          <Callout
            intent={intents.warning}
            prefixIcon={<Download className="text-text-primary" />}
            title={`${rollout.laneName}, ${rollout.model} firmware update`}
            subtitle={bannerSubtitle(rollout)}
            buttonText={isAwaitingReview(rollout) ? "Review update" : "View update"}
            buttonOnClick={() => onViewUpdate(rollout)}
            testId={`update-banner-${rollout.id.toString()}`}
          />
        </div>
      ))}
    </div>
  );
};

export default ActiveUpdateBanners;
