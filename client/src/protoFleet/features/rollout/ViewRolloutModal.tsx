import type { ReactElement } from "react";

import ActiveRolloutStatus from "./ActiveRolloutStatus";
import type { RolloutEvent } from "./rolloutTypes";
import Modal from "@/shared/components/Modal";

interface ViewRolloutModalProps {
  /** The rollout to show; when null the modal is closed. */
  event: RolloutEvent | null;
  onDismiss: () => void;
  onPause?: () => void;
  onResume?: () => void;
  onCancelRemaining?: () => void;
  onContinueFromPilot?: () => void;
  onRetryFailed?: () => void;
}

/**
 * "View rollout" surface — summons the progress-against-plan card in a centered
 * modal so an operator can check a rollout from anywhere (a page banner, the
 * header pill, an activity row) without navigating away and losing context.
 * Same pattern as `ActivityDetailModal`: the shared `Modal` over a page overlay,
 * click-outside / Escape to dismiss. Uses the `large` size so the stat grid and
 * progress bar have room.
 *
 * The card's own section header carries the title/scope, so the modal chrome is
 * headerless — just the dismiss affordance from the overlay.
 */
function ViewRolloutModal({
  event,
  onDismiss,
  onPause,
  onResume,
  onCancelRemaining,
  onContinueFromPilot,
  onRetryFailed,
}: ViewRolloutModalProps): ReactElement | null {
  if (!event) {
    return null;
  }

  return (
    <Modal
      size="large"
      showHeader={false}
      onDismiss={onDismiss}
      testId="view-rollout-modal"
      bodyClassName="text-text-primary"
    >
      <ActiveRolloutStatus
        event={event}
        embedded
        onPause={onPause}
        onResume={onResume}
        onCancelRemaining={onCancelRemaining}
        onContinueFromPilot={onContinueFromPilot}
        onRetryFailed={onRetryFailed}
      />
    </Modal>
  );
}

export default ViewRolloutModal;
