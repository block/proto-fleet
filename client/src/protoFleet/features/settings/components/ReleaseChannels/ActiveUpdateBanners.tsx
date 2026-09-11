import { activeUpdateSummary, hasFailures, needsManualReview } from "./rolloutStatus";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { Download } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";

interface ActiveUpdateBannersProps {
  // Ongoing rollouts, most recently started first.
  rollouts: Rollout[];
  onViewUpdate: (rollout: Rollout) => void;
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
            intent={hasFailures(rollout) ? intents.danger : intents.warning}
            prefixIcon={<Download className="text-text-primary" />}
            title={`${rollout.channelName}, ${rollout.model} firmware update`}
            subtitle={activeUpdateSummary(rollout)}
            buttonText={needsManualReview(rollout) ? "Review update" : "View update"}
            buttonOnClick={() => onViewUpdate(rollout)}
            testId={`update-banner-${rollout.id.toString()}`}
          />
        </div>
      ))}
    </div>
  );
};

export default ActiveUpdateBanners;
