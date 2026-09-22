import type { ReactElement } from "react";
import { Link } from "react-router-dom";

import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import { type Rollout, RolloutState } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  activeUpdateSummary,
  isPaused,
  needsManualReview,
  pairLabel,
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
function triggerLabel(rollouts: Rollout[], attentionCount: number, pausedCount: number, waitingCount: number): string {
  if (attentionCount === 1) return "Firmware update needs attention";
  if (attentionCount > 1) return `${attentionCount} firmware updates need attention`;
  const states = [
    { count: rollouts.length - pausedCount - waitingCount, label: "in progress" },
    { count: pausedCount, label: "paused" },
    { count: waitingCount, label: "waiting for controller" },
  ].filter(({ count }) => count > 0);
  return states
    .map(({ count, label }, index) => {
      if (index > 0) return `${count} ${label}`;
      const subject =
        count === 1 && states.length === 1
          ? "Firmware update"
          : `${count} firmware ${count === 1 ? "update" : "updates"}`;
      return `${subject} ${label}`;
    })
    .join(", ");
}

function RolloutPill({ rollouts }: RolloutPillProps): ReactElement {
  const attentionCount = rollouts.filter(rolloutNeedsAttention).length;
  const pausedCount = rollouts.filter(isPaused).length;
  const waitingCount = rollouts.filter((rollout) => rollout.state === RolloutState.WAITING_FOR_CONTROLLER).length;
  const isProgressing = attentionCount === 0 && pausedCount + waitingCount < rollouts.length;
  return (
    <PageHeaderPopoverPill
      ariaLabel="View ongoing firmware updates"
      constrainHeightToViewport
      // Solid while work waits on an operator or controller; pulse only for
      // progressing work when nothing needs attention.
      dotClassName={isProgressing ? "animate-pulse bg-intent-warning-fill" : "bg-intent-warning-fill"}
      triggerClassName="rollout-pill-trigger"
      triggerLabel={triggerLabel(rollouts, attentionCount, pausedCount, waitingCount)}
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
                    {`${pairLabel(rollout)} → ${rollout.firmwareVersion}`}
                  </div>
                  <div
                    className={
                      attention
                        ? "text-200 leading-snug text-intent-warning-text"
                        : "text-200 leading-snug text-text-primary-70"
                    }
                  >
                    {needsManualReview(rollout) ||
                    isPaused(rollout) ||
                    rollout.state === RolloutState.WAITING_FOR_CONTROLLER
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
