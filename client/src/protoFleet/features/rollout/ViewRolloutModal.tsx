import type { ReactElement } from "react";

import ActiveRolloutStatus from "./ActiveRolloutStatus";
import type { RolloutEvent } from "./rolloutTypes";
import Modal from "@/shared/components/Modal";

interface ViewRolloutModalProps {
  /** The rollout to show; when null the modal is closed. */
  event: RolloutEvent | null;
  onDismiss: () => void;
  onManage?: () => void;
  onPause?: () => void;
  onResume?: () => void;
  onCancelRemaining?: () => void;
  onContinueFromPilot?: () => void;
  onRetryFailed?: () => void;
  onViewMiners?: () => void;
}

/**
 * "View rollout" surface — summons the progress-against-plan card in a centered
 * modal so an operator can check a rollout from anywhere (a page banner, the
 * header pill, an activity row) without navigating away and losing context.
 * Same pattern as `ActivityDetailModal`: the shared `Modal` over a page overlay,
 * click-outside / Escape to dismiss. Uses the `large` size so the stat grid and
 * progress bar have room; the body scrolls under the sticky header when tall.
 *
 * The Modal owns the title bar (title + close), while the embedded status card
 * owns the lifecycle action bar so the standalone and modal presentations keep
 * the same Manage / current action / overflow treatment.
 */
function ViewRolloutModal({
  event,
  onDismiss,
  onManage,
  onPause,
  onResume,
  onCancelRemaining,
  onContinueFromPilot,
  onRetryFailed,
  onViewMiners,
}: ViewRolloutModalProps): ReactElement | null {
  if (!event) {
    return null;
  }

  return (
    <Modal
      title={event.title}
      onDismiss={onDismiss}
      testId="view-rollout-modal"
      bodyClassName="text-text-primary"
      // Pin the title in the sticky top bar (rather than only collapsing there
      // on scroll), so the rollout context stays visible while the body scrolls.
      forceTitleCollapsed
    >
      <ActiveRolloutStatus
        event={event}
        embedded
        onManage={onManage}
        onPause={onPause}
        onResume={onResume}
        onCancelRemaining={onCancelRemaining}
        onContinueFromPilot={onContinueFromPilot}
        onRetryFailed={onRetryFailed}
        onViewMiners={onViewMiners}
      />
    </Modal>
  );
}

export default ViewRolloutModal;
