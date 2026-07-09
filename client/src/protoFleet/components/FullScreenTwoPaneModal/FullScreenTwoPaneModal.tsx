import { type ReactNode } from "react";
import clsx from "clsx";

import FullScreenModalHeaderActions from "@/protoFleet/components/FullScreenModalHeaderActions";
import { Dismiss } from "@/shared/assets/icons";
import { type ButtonProps } from "@/shared/components/ButtonGroup";
import Header from "@/shared/components/Header";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const defaultPaneContainerClassName =
  "flex min-h-[calc(100dvh-200px)] w-full flex-1 flex-col laptop:grid laptop:min-h-0 laptop:grid-cols-2 laptop:px-10";
const defaultPrimaryPaneClassName =
  "order-2 flex flex-col pl-6 laptop:order-1 laptop:min-h-0 laptop:overflow-y-auto laptop:pl-1";
const defaultSecondaryPaneClassName =
  "order-1 mb-6 flex shrink-0 flex-col self-stretch overflow-visible bg-surface-overlay laptop:order-2 laptop:mb-0 laptop:min-h-0 laptop:shrink laptop:rounded-xl laptop:pl-6";

interface FullScreenTwoPaneModalProps {
  open: boolean;
  title: string;
  onDismiss?: () => void;
  isBusy?: boolean;
  closeAriaLabel?: string;
  buttons?: ButtonProps[];
  primaryPane: ReactNode;
  secondaryPane: ReactNode;
  abovePanes?: ReactNode;
  loadingState?: ReactNode;
  maxWidth?: string;
  paneContainerClassName?: string;
  primaryPaneClassName?: string;
  secondaryPaneClassName?: string;
  className?: string;
  zIndex?: string;
}

const FullScreenTwoPaneModal = ({
  open,
  title,
  onDismiss,
  isBusy = false,
  closeAriaLabel = "Close dialog",
  buttons,
  primaryPane,
  secondaryPane,
  abovePanes,
  loadingState,
  maxWidth = "none",
  paneContainerClassName,
  primaryPaneClassName,
  secondaryPaneClassName,
  className,
  zIndex,
}: FullScreenTwoPaneModalProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  const useCompactHeaderActions = isPhone || isTablet;
  const effectiveOnDismiss = isBusy ? undefined : onDismiss;

  return (
    <Modal
      open={open}
      onDismiss={effectiveOnDismiss}
      size={modalSizes.fullscreen}
      showHeader={false}
      zIndex={zIndex}
      testId="full-screen-two-pane-modal"
      className="!p-0"
      bodyClassName={clsx(
        "flex h-full min-h-0 w-full flex-col overflow-auto bg-surface-base pb-6 laptop:overflow-hidden",
        className,
      )}
    >
      <div className="sticky top-0 z-10 mb-0 bg-surface-base px-6 pt-6 pb-4 laptop:static laptop:mb-6">
        <Header
          title={title}
          titleSize="text-heading-200"
          stackButtonsOnPhone={false}
          iconAriaLabel={closeAriaLabel}
          icon={<Dismiss className={isBusy ? "cursor-default text-text-primary-30" : "cursor-pointer"} />}
          iconOnClick={() => {
            if (!isBusy) {
              onDismiss?.();
            }
          }}
          iconTextColor={isBusy ? "text-text-primary-30" : "text-text-primary"}
          inline
          centerButton
          buttonsWrapperClassName={useCompactHeaderActions ? undefined : "hidden laptop:block"}
          buttons={useCompactHeaderActions ? undefined : buttons}
        >
          {useCompactHeaderActions ? (
            <FullScreenModalHeaderActions buttons={buttons} renderWhen="phone-tablet" />
          ) : null}
        </Header>
      </div>

      {abovePanes}

      {loadingState ?? (
        <div className="mx-auto flex min-h-0 w-full flex-1" style={maxWidth !== "none" ? { maxWidth } : undefined}>
          <div className={paneContainerClassName ?? defaultPaneContainerClassName}>
            <div className={clsx(defaultPrimaryPaneClassName, primaryPaneClassName)}>{primaryPane}</div>
            <div className={clsx(defaultSecondaryPaneClassName, secondaryPaneClassName)}>{secondaryPane}</div>
          </div>
        </div>
      )}
    </Modal>
  );
};

export default FullScreenTwoPaneModal;
export type { FullScreenTwoPaneModalProps };
