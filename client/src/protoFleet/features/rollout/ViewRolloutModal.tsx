import type { ReactElement } from "react";

import ActiveRolloutStatus from "./ActiveRolloutStatus";
import { rolloutLifecycleActions } from "./rolloutDisplayUtils";
import type { RolloutEvent } from "./rolloutTypes";
import { variants } from "@/shared/components/Button";
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

const buttonVariant = {
  primary: variants.primary,
  secondary: variants.secondary,
  danger: variants.danger,
} as const;

/**
 * "View rollout" surface — summons the progress-against-plan card in a centered
 * modal so an operator can check a rollout from anywhere (a page banner, the
 * header pill, an activity row) without navigating away and losing context.
 * Same pattern as `ActivityDetailModal`: the shared `Modal` over a page overlay,
 * click-outside / Escape to dismiss. Uses the `large` size so the stat grid and
 * progress bar have room; the body scrolls under the sticky header when tall.
 *
 * The Modal owns the title bar (title + scope + close) AND the lifecycle CTAs
 * (top-bar buttons), so the card renders `embedded` with its own header and
 * action row suppressed. The action set is derived from the same
 * `rolloutLifecycleActions` helper the card uses, so the two never drift.
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

  const actions = rolloutLifecycleActions(event, {
    onPause,
    onResume,
    onCancelRemaining,
    onContinueFromPilot,
    onRetryFailed,
  });

  return (
    <Modal
      size="large"
      title={event.title}
      description={event.scopeLabel ? `Applies to ${event.scopeLabel}` : undefined}
      onDismiss={onDismiss}
      testId="view-rollout-modal"
      bodyClassName="text-text-primary"
      buttons={actions.map((action) => ({
        text: action.text,
        variant: buttonVariant[action.variant],
        onClick: action.onClick,
        // Lifecycle actions don't dismiss the modal — the host decides when to
        // close after the action resolves.
        dismissModalOnClick: false,
      }))}
    >
      <ActiveRolloutStatus event={event} embedded hideActions />
    </Modal>
  );
}

export default ViewRolloutModal;
