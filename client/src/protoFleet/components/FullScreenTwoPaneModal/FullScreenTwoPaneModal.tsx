import { type ReactNode, useCallback, useRef, useState } from "react";
import clsx from "clsx";

import { Dismiss, Ellipsis } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button";
import ButtonGroup, { type ButtonProps, groupVariants } from "@/shared/components/ButtonGroup";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import { useClickOutsideDismiss } from "@/shared/hooks/useClickOutsideDismiss";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";

const defaultPaneContainerClassName =
  "flex min-h-[calc(100dvh-200px)] w-full flex-1 flex-col laptop:grid laptop:min-h-0 laptop:grid-cols-2 laptop:px-10";
const defaultPrimaryPaneClassName =
  "order-2 flex flex-col pl-6 laptop:order-1 laptop:min-h-0 laptop:overflow-y-auto laptop:pl-1";
const defaultSecondaryPaneClassName =
  "order-1 flex max-h-[50vh] flex-col self-stretch overflow-y-auto bg-surface-overlay mb-6 laptop:order-2 laptop:min-h-0 laptop:max-h-none laptop:rounded-xl laptop:pl-6";

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

const isDangerVariant = (variant: string) => variant === variants.danger || variant === variants.secondaryDanger;

const OverflowActionSheet = ({ overflowButtons, onClose }: { overflowButtons: ButtonProps[]; onClose: () => void }) => {
  const sheetRef = useRef<HTMLDivElement>(null);
  useClickOutsideDismiss({ ref: sheetRef, onDismiss: onClose });
  useEscapeDismiss(onClose);

  const nonDangerItems = overflowButtons.filter((b) => !isDangerVariant(b.variant));
  const dangerItems = overflowButtons.filter((b) => isDangerVariant(b.variant));

  return (
    <div className="fixed inset-0 z-60 flex items-end bg-grayscale-gray-5">
      <div
        ref={sheetRef}
        className="w-full rounded-t-2xl bg-surface-elevated-base px-6 pt-2 pb-[max(env(safe-area-inset-bottom),16px)]"
      >
        {nonDangerItems.map((button, index) => (
          <Row
            key={`${button.text}-${index}`}
            className={clsx("text-emphasis-300 text-text-primary", button.disabled && "pointer-events-none opacity-40")}
            onClick={
              button.disabled
                ? undefined
                : () => {
                    button.onClick?.();
                    onClose();
                  }
            }
            divider={false}
          >
            {button.text}
          </Row>
        ))}

        {dangerItems.length > 0 && nonDangerItems.length > 0 ? <Divider /> : null}

        {dangerItems.map((button, index) => (
          <Row
            key={`danger-${button.text}-${index}`}
            className={clsx(
              "text-emphasis-300 text-intent-critical-fill",
              button.disabled && "pointer-events-none opacity-40",
            )}
            onClick={
              button.disabled
                ? undefined
                : () => {
                    button.onClick?.();
                    onClose();
                  }
            }
            divider={false}
          >
            {button.text}
          </Row>
        ))}
      </div>
    </div>
  );
};

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
  const [showOverflowSheet, setShowOverflowSheet] = useState(false);

  // Split buttons: primary CTA (last primary-variant button) vs overflow (rest)
  let primaryButton: ButtonProps | undefined;
  let overflowButtons: ButtonProps[] = [];

  if (buttons && buttons.length > 0) {
    if (buttons.length === 1) {
      primaryButton = buttons[0];
    } else {
      let primaryIndex = -1;
      for (let i = buttons.length - 1; i >= 0; i--) {
        if (buttons[i].variant === variants.primary) {
          primaryIndex = i;
          break;
        }
      }

      if (primaryIndex === -1) {
        primaryButton = buttons[buttons.length - 1];
        overflowButtons = buttons.slice(0, -1);
      } else {
        primaryButton = buttons[primaryIndex];
        overflowButtons = buttons.filter((_, i) => i !== primaryIndex);
      }
    }
  }

  const closeSheet = useCallback(() => setShowOverflowSheet(false), []);

  const mobileButtons: ButtonProps[] = [];

  if (overflowButtons.length > 0) {
    mobileButtons.push({
      variant: variants.secondary,
      onClick: () => setShowOverflowSheet(true),
      prefixIcon: <Ellipsis />,
      testId: "overflow-menu-trigger",
      ariaLabel: "More actions",
    });
  }

  if (primaryButton) {
    mobileButtons.push(primaryButton);
  }

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
          buttonsWrapperClassName="hidden laptop:block"
          buttons={buttons}
        >
          {/* Mobile buttons: ellipsis + primary CTA */}
          <div className="ml-3 shrink-0 laptop:hidden">
            <ButtonGroup buttons={mobileButtons} variant={groupVariants.rightAligned} size={sizes.base} />
          </div>
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

      {showOverflowSheet ? <OverflowActionSheet overflowButtons={overflowButtons} onClose={closeSheet} /> : null}
    </Modal>
  );
};

export default FullScreenTwoPaneModal;
export type { FullScreenTwoPaneModalProps };
