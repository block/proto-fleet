import type { ComponentProps, ReactElement } from "react";

import ActiveRolloutStatus from "./ActiveRolloutStatus";
import { rolloutLifecycleActions } from "./rolloutDisplayUtils";
import type { RolloutEvent } from "./rolloutTypes";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";

interface ViewRolloutModalProps {
  /** The rollout to show; when null the modal is closed. */
  event: RolloutEvent | null;
  onDismiss: () => void;
  canManage?: boolean;
  canControl?: boolean;
  onManage?: () => void;
  onPause?: () => void;
  onResume?: () => void;
  onAbort?: () => void;
  onRevert?: () => void;
  onCancelRemaining?: () => void;
  onContinueFromReview?: () => void;
  onRetryFailed?: () => void;
  onViewMiners?: () => void;
  onViewErrors?: () => void;
}

type ModalButton = NonNullable<ComponentProps<typeof Modal>["buttons"]>[number];

const lifecycleButtonVariant = {
  primary: variants.primary,
  secondary: variants.secondary,
  danger: variants.secondaryDanger,
} as const;

interface ModalActions {
  visibleButtons: ModalButton[];
  compactButtons: ModalButton[];
  overflowActions: RowAction[];
}

function modalActions({
  event,
  canManage,
  canControl,
  onManage,
  onPause,
  onResume,
  onAbort,
  onRevert,
  onCancelRemaining,
  onContinueFromReview,
  onRetryFailed,
  onViewMiners,
}: ViewRolloutModalProps & { event: RolloutEvent }): ModalActions {
  const lifecycleActions = rolloutLifecycleActions(
    event,
    {
      onManage,
      onPause,
      onResume,
      onAbort,
      onRevert,
      onCancelRemaining,
      onContinueFromReview,
      onRetryFailed,
    },
    { canManage, canControl },
  );

  const visibleLifecycleActions = lifecycleActions.filter(
    (action) => action.key !== "cancel" && action.key !== "abort",
  );
  const overflowLifecycleActions = lifecycleActions.filter(
    (action) => action.key === "cancel" || action.key === "abort",
  );
  const visibleButtons = visibleLifecycleActions.map((action) => ({
    text: action.text,
    variant: lifecycleButtonVariant[action.variant],
    onClick: action.onClick,
    dismissModalOnClick: false,
    testId: `view-rollout-${action.key}-action`,
  }));
  const overflowButtons: ModalButton[] = [
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
    ...overflowLifecycleActions.map((action) => ({
      text: action.text,
      variant: lifecycleButtonVariant[action.variant],
      onClick: action.onClick,
      dismissModalOnClick: false,
      testId: `view-rollout-${action.key}-action`,
    })),
  ];

  return {
    visibleButtons,
    compactButtons: [...overflowButtons, ...visibleButtons],
    overflowActions: overflowButtons.map((button) => ({
      label: button.text ?? button.ariaLabel ?? "Action",
      onClick: button.onClick ?? (() => undefined),
      danger: button.variant === variants.danger || button.variant === variants.secondaryDanger,
      testId: button.testId,
    })),
  };
}

/** Centered rollout detail modal. */
function ViewRolloutModal({
  event,
  onDismiss,
  canManage = true,
  canControl = true,
  onManage,
  onPause,
  onResume,
  onAbort,
  onRevert,
  onCancelRemaining,
  onContinueFromReview,
  onRetryFailed,
  onViewMiners,
  onViewErrors,
}: ViewRolloutModalProps): ReactElement | null {
  if (!event) {
    return null;
  }

  const actions = modalActions({
    event,
    onDismiss,
    canManage,
    canControl,
    onManage,
    onPause,
    onResume,
    onAbort,
    onRevert,
    onCancelRemaining,
    onContinueFromReview,
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
      buttons={actions.visibleButtons}
      compactHeaderButtons={actions.compactButtons}
      headerLeadingAction={
        actions.overflowActions.length > 0 ? (
          <RowActionsMenu
            actions={actions.overflowActions}
            ariaLabel={`More actions for ${event.title}`}
            popoverTestId="view-rollout-more-actions-menu"
            testIdPrefix="view-rollout-more-actions"
            triggerClassName="!h-8 !w-8 !px-0 !py-0"
            triggerVariant={variants.secondary}
          />
        ) : undefined
      }
      // Pin the title in the sticky top bar (rather than only collapsing there
      // on scroll), so the rollout context stays visible while the body scrolls.
      forceTitleCollapsed
    >
      <ActiveRolloutStatus
        event={event}
        embedded
        hideActions
        canManage={canManage}
        canControl={canControl}
        onManage={onManage}
        onPause={onPause}
        onResume={onResume}
        onAbort={onAbort}
        onRevert={onRevert}
        onCancelRemaining={onCancelRemaining}
        onContinueFromReview={onContinueFromReview}
        onRetryFailed={onRetryFailed}
        onViewMiners={onViewMiners}
        onViewErrors={onViewErrors}
      />
    </Modal>
  );
}

export default ViewRolloutModal;
