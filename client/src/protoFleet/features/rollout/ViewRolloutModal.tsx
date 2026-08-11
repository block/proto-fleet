import type { ComponentProps, ReactElement } from "react";

import ActiveRolloutStatus from "./ActiveRolloutStatus";
import { rolloutLifecycleActions } from "./rolloutDisplayUtils";
import type { RolloutEvent } from "./rolloutTypes";
import { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";

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

type ModalButton = NonNullable<ComponentProps<typeof Modal>["buttons"]>[number];

const lifecycleButtonVariant = {
  primary: variants.primary,
  secondary: variants.secondary,
  danger: variants.secondaryDanger,
} as const;

function modalActionButtons({
  event,
  onManage,
  onPause,
  onResume,
  onCancelRemaining,
  onContinueFromPilot,
  onRetryFailed,
  onViewMiners,
}: ViewRolloutModalProps & { event: RolloutEvent }): ModalButton[] {
  const lifecycleActions = rolloutLifecycleActions(event, {
    onManage,
    onPause,
    onResume,
    onCancelRemaining,
    onContinueFromPilot,
    onRetryFailed,
  });

  return [
    ...(onViewMiners
      ? [
          {
            text: "View miners",
            variant: variants.secondary,
            onClick: onViewMiners,
            dismissModalOnClick: false,
            testId: "view-rollout-view-miners-action",
          },
        ]
      : []),
    ...lifecycleActions.map((action) => ({
      text: action.text,
      variant: lifecycleButtonVariant[action.variant],
      onClick: action.onClick,
      dismissModalOnClick: false,
      testId: `view-rollout-${action.key}-action`,
    })),
  ];
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

  const buttons = modalActionButtons({
    event,
    onDismiss,
    onManage,
    onPause,
    onResume,
    onCancelRemaining,
    onContinueFromPilot,
    onRetryFailed,
    onViewMiners,
  });

  return (
    <Modal
      title={event.title}
      onDismiss={onDismiss}
      testId="view-rollout-modal"
      size={modalSizes.large}
      surfaceClassName="max-w-[960px]"
      bodyClassName="text-text-primary"
      buttonSize={buttonSizes.compact}
      buttons={buttons}
      // Pin the title in the sticky top bar (rather than only collapsing there
      // on scroll), so the rollout context stays visible while the body scrolls.
      forceTitleCollapsed
    >
      <ActiveRolloutStatus
        event={event}
        embedded
        hideActions
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
