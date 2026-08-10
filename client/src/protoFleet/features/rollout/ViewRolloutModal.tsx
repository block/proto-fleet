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

/** Centered rollout detail modal. */
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
