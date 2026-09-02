import { type ReactElement, useState } from "react";

import ActiveRolloutStatus from "./ActiveRolloutStatus";
import { rolloutMinerRowsForEvent } from "./rollout.fixtures";
import { rolloutLifecycleActions } from "./rolloutDisplayUtils";
import RolloutMinersModal, { type RolloutMinerFilter } from "./RolloutMinersModal";
import type { RolloutEvent } from "./rolloutTypes";
import FullScreenModalHeaderActions from "@/protoFleet/components/FullScreenModalHeaderActions";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import { Dismiss } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import { type ButtonProps } from "@/shared/components/ButtonGroup";
import Header from "@/shared/components/Header";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface ViewRolloutModalProps {
  /** The rollout to show; when null the modal is closed. */
  event: RolloutEvent | null;
  onDismiss: () => void;
  onManage?: () => void;
  onPause?: () => void;
  onResume?: () => void;
  onCancelRemaining?: () => void;
  onContinueFromReview?: () => void;
  onRetryFailed?: () => void;
  onViewMiners?: () => void;
  onViewErrors?: () => void;
}

type ModalButton = ButtonProps;

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
  onManage,
  onPause,
  onResume,
  onCancelRemaining,
  onContinueFromReview,
  onRetryFailed,
  onViewMiners,
}: ViewRolloutModalProps & { event: RolloutEvent }): ModalActions {
  const lifecycleActions = rolloutLifecycleActions(event, {
    onManage,
    onPause,
    onResume,
    onCancelRemaining,
    onContinueFromReview,
    onRetryFailed,
  });

  const visibleLifecycleActions = lifecycleActions.filter((action) => action.key !== "cancel");
  const overflowLifecycleActions = lifecycleActions.filter((action) => action.key === "cancel");
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

/** Full-screen rollout detail with miner drill-downs presented as standalone modals. */
function ViewRolloutModal({
  event,
  onDismiss,
  onManage,
  onPause,
  onResume,
  onCancelRemaining,
  onContinueFromReview,
  onRetryFailed,
  onViewMiners,
  onViewErrors,
}: ViewRolloutModalProps): ReactElement | null {
  const [minerModalFilter, setMinerModalFilter] = useState<RolloutMinerFilter | null>(null);
  const { isPhone, isTablet } = useWindowDimensions();
  const useCompactHeaderActions = isPhone || isTablet;

  if (!event) {
    return null;
  }

  const actions = modalActions({
    event,
    onDismiss,
    onManage,
    onPause,
    onResume,
    onCancelRemaining,
    onContinueFromReview,
    onRetryFailed,
    onViewMiners: onViewMiners ? () => setMinerModalFilter("all") : undefined,
  });

  return (
    <>
      <Modal
        open
        onDismiss={onDismiss}
        testId="view-rollout-modal"
        size={modalSizes.fullscreen}
        showHeader={false}
        className="!p-0"
        bodyClassName="flex h-full min-h-0 w-full flex-col overflow-auto bg-surface-base pb-6"
      >
        <div
          className="sticky top-0 z-10 mb-0 bg-surface-base px-6 pt-6 pb-4 laptop:static laptop:mb-6"
          data-testid="view-rollout-header"
        >
          <Header
            title={event.title}
            titleSize="text-heading-200"
            icon={<Dismiss />}
            iconAriaLabel="Close rollout details"
            iconOnClick={onDismiss}
            inline
            centerButton
            stackButtonsOnPhone={false}
            buttonsWrapperClassName={useCompactHeaderActions ? undefined : "hidden laptop:block"}
            buttons={useCompactHeaderActions ? undefined : actions.visibleButtons}
          >
            {useCompactHeaderActions ? (
              <FullScreenModalHeaderActions
                buttons={actions.compactButtons}
                renderWhen="phone-tablet"
                triggerTestId="view-rollout-more-actions-trigger"
              />
            ) : actions.overflowActions.length > 0 ? (
              <RowActionsMenu
                actions={actions.overflowActions}
                ariaLabel={`More actions for ${event.title}`}
                popoverTestId="view-rollout-more-actions-menu"
                testIdPrefix="view-rollout-more-actions"
                triggerClassName="!h-10 !w-10 !px-0 !py-0"
                triggerVariant={variants.secondary}
              />
            ) : null}
          </Header>
        </div>
        <div className="mx-auto w-full max-w-[800px] px-6 pb-6" data-testid="view-rollout-content">
          <ActiveRolloutStatus
            event={event}
            embedded
            hideActions
            onManage={onManage}
            onPause={onPause}
            onResume={onResume}
            onCancelRemaining={onCancelRemaining}
            onContinueFromReview={onContinueFromReview}
            onRetryFailed={onRetryFailed}
            onViewMiners={onViewMiners ? () => setMinerModalFilter("all") : undefined}
            onViewErrors={onViewErrors ? () => setMinerModalFilter("errors") : undefined}
          />
        </div>
      </Modal>
      <RolloutMinersModal
        key={minerModalFilter ?? "closed"}
        open={minerModalFilter !== null}
        event={event}
        miners={rolloutMinerRowsForEvent(event)}
        initialFilter={minerModalFilter ?? "all"}
        onDismiss={() => setMinerModalFilter(null)}
      />
    </>
  );
}

export default ViewRolloutModal;
