import type { ReactElement } from "react";
import { Link } from "react-router-dom";

import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  activeUpdateSummary,
  isPaused,
  needsManualReview,
  rolloutNeedsAttention,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutStageLabel,
  scopeCounts,
} from "@/protoFleet/features/settings/components/ReleaseChannels/rolloutStatus";
import CompositionBar from "@/shared/components/CompositionBar";

export const RELEASE_CHANNELS_PATH = "/settings/firmware?tab=release-channels";

interface RolloutPillProps {
  rollouts: Rollout[];
}

// Pointer activation blurs the trigger in PageHeaderPopoverPill. Keyboard
// activation keeps it focused, so move into the portal after positioning.
// Focus the top of the content without scrolling a long list to its footer.
function focusKeyboardOpenedContent(content: HTMLDivElement | null) {
  const trigger = document.activeElement;
  if (!content || !(trigger instanceof HTMLElement) || !trigger.closest(".rollout-pill-trigger")) return;
  const frame = requestAnimationFrame(() => {
    if (content.isConnected && document.activeElement === trigger) content.focus({ preventScroll: true });
  });
  return () => {
    cancelAnimationFrame(frame);
    if (trigger.isConnected && content.contains(document.activeElement)) trigger.focus({ preventScroll: true });
  };
}

// Trigger copy leads with what needs a human: an update parked at a review
// gate or carrying failed miners outranks updates that are merely running.
function triggerLabel(rollouts: Rollout[], attentionCount: number, pausedCount: number): string {
  if (attentionCount === 1) return "Firmware update needs attention";
  if (attentionCount > 1) return `${attentionCount} firmware updates need attention`;
  if (pausedCount === rollouts.length) {
    return pausedCount === 1 ? "Firmware update paused" : `${pausedCount} firmware updates paused`;
  }
  if (pausedCount > 0) {
    const runningCount = rollouts.length - pausedCount;
    return `${runningCount} firmware ${runningCount === 1 ? "update" : "updates"} in progress, ${pausedCount} paused`;
  }
  return rollouts.length === 1 ? "Firmware update in progress" : `${rollouts.length} firmware updates in progress`;
}

function RolloutPill({ rollouts }: RolloutPillProps): ReactElement {
  const attentionCount = rollouts.filter(rolloutNeedsAttention).length;
  const pausedCount = rollouts.filter(isPaused).length;
  const isProgressing = attentionCount === 0 && pausedCount < rollouts.length;
  return (
    <PageHeaderPopoverPill
      ariaLabel="View ongoing firmware updates"
      constrainHeightToViewport
      // Solid while something waits on you, pulsing while the fleet is still
      // being worked on.
      dotClassName={isProgressing ? "animate-pulse bg-intent-warning-fill" : "bg-intent-warning-fill"}
      triggerClassName="rollout-pill-trigger"
      triggerLabel={triggerLabel(rollouts, attentionCount, pausedCount)}
    >
      {({ closePopover }) => (
        <div
          ref={focusKeyboardOpenedContent}
          tabIndex={-1}
          role="region"
          aria-label="Ongoing firmware updates"
          className="flex flex-col gap-3"
        >
          <div className="flex flex-col gap-3">
            {rollouts.map((rollout) => {
              const counts = scopeCounts(rollout);
              const attention = rolloutNeedsAttention(rollout);
              return (
                <div
                  key={rollout.id.toString()}
                  className="min-w-0 space-y-1.5"
                  data-testid={`rollout-pill-entry-${rollout.id.toString()}`}
                >
                  <div className="truncate text-heading-100 text-text-primary">{rollout.channelName}</div>
                  <div className="text-200 leading-snug text-text-primary-70">
                    {`${rollout.model} → ${rollout.firmwareVersion}`}
                  </div>
                  <div
                    className={
                      attention
                        ? "text-200 leading-snug text-intent-warning-text"
                        : "text-200 leading-snug text-text-primary-70"
                    }
                  >
                    {needsManualReview(rollout) || isPaused(rollout)
                      ? rolloutStageLabel(rollout)
                      : activeUpdateSummary(rollout)}
                  </div>
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
              to={RELEASE_CHANNELS_PATH}
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
