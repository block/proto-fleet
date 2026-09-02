import {
  batchLabel,
  isAwaitingReview,
  isBatchStage,
  isPaused,
  rolloutProgressSummary,
  scopeCounts,
} from "./rolloutStatus";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import Button, { sizes, variants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface ActiveUpdatesSectionProps {
  // Ongoing rollouts, most recently started first.
  rollouts: Rollout[];
  onViewUpdate: (rollout: Rollout) => void;
}

// Compact always-visible rows for ongoing firmware updates, one per rollout,
// each deep-linking into the full update detail modal.
const ActiveUpdatesSection = ({ rollouts, onViewUpdate }: ActiveUpdatesSectionProps) => (
  <section className="grid gap-3" data-testid="active-updates-section">
    <div className="flex items-end justify-between gap-4">
      <h2 className="text-heading-200 text-text-primary">Active updates</h2>
      <span className="text-200 text-text-primary-50">
        {rollouts.length === 1 ? "1 update running" : `${rollouts.length} updates running`}
      </span>
    </div>
    {rollouts.map((rollout) => {
      const awaitingReview = isAwaitingReview(rollout);
      const paused = isPaused(rollout);
      const counts = scopeCounts(rollout);
      const progress = isBatchStage(rollout)
        ? `${batchLabel(rollout)}: ${rolloutProgressSummary(counts)}`
        : rolloutProgressSummary(counts);
      const summary = awaitingReview
        ? `${batchLabel(rollout)} complete — review needed · ${rollout.firmwareVersion}`
        : paused
          ? `Paused · ${progress} · ${rollout.firmwareVersion}`
          : `${progress} · ${rollout.firmwareVersion}`;
      return (
        <div
          key={rollout.id.toString()}
          className="flex items-center gap-3 rounded-xl bg-surface-elevated-base px-4 py-3 shadow-100"
          data-testid={`active-update-${rollout.id.toString()}`}
        >
          <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-core-primary-5">
            {awaitingReview ? (
              <span className="size-2.5 rounded-full bg-intent-warning-fill" />
            ) : paused ? (
              <span className="bg-core-primary-30 size-2.5 rounded-full" />
            ) : (
              <ProgressCircular indeterminate />
            )}
          </div>
          <div className="min-w-0 grow">
            <div className="truncate text-heading-100 text-text-primary">
              {`${rollout.laneName}, ${rollout.model} firmware update`}
            </div>
            <div className="truncate text-200 text-text-primary-70">{summary}</div>
          </div>
          <Button
            variant={awaitingReview ? variants.primary : variants.secondary}
            size={sizes.compact}
            text={awaitingReview ? "Review update" : "View update"}
            onClick={() => onViewUpdate(rollout)}
            testId={`view-update-${rollout.id.toString()}`}
          />
        </div>
      );
    })}
  </section>
);

export default ActiveUpdatesSection;
