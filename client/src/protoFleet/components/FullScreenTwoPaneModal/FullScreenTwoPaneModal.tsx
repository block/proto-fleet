import { type ReactNode, useCallback, useRef, useState } from "react";
import clsx from "clsx";

import { DismissCircleDark, Ellipsis } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button";
import ButtonGroup, { type ButtonProps, groupVariants } from "@/shared/components/ButtonGroup";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";
import Row from "@/shared/components/Row";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useKeyDown } from "@/shared/hooks/useKeyDown";

const defaultPaneContainerClassName = "flex min-h-[calc(100dvh-104px)] w-full flex-1 flex-col lg:grid lg:grid-cols-2";

interface FullScreenTwoPaneModalProps {
  open: boolean;
  title: string;
  onDismiss?: () => void;
  isBusy?: boolean;
  buttons?: ButtonProps[];
  primaryPane: ReactNode;
  secondaryPane: ReactNode;
  abovePanes?: ReactNode;
  loadingState?: ReactNode;
  maxWidth?: string;
  paneContainerClassName?: string;
  className?: string;
  zIndex?: string;
}

const isDangerVariant = (variant: string) => variant === variants.danger || variant === variants.secondaryDanger;

const OverflowActionSheet = ({ overflowButtons, onClose }: { overflowButtons: ButtonProps[]; onClose: () => void }) => {
  const sheetRef = useRef<HTMLDivElement>(null);
  useClickOutside({ ref: sheetRef, onClickOutside: onClose });
  useKeyDown({
    key: "Escape",
    onKeyDown: () => onClose(),
  });

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

        {dangerItems.length > 0 && nonDangerItems.length > 0 && <Divider />}

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
  buttons,
  primaryPane,
  secondaryPane,
  abovePanes,
  loadingState,
  maxWidth = "none",
  paneContainerClassName,
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

  return (
    <PageOverlay open={open} {...(zIndex && { zIndex })}>
      <div className={clsx("h-full w-full overflow-auto bg-surface-base", className)}>
        <div className="flex min-h-full w-full flex-col pb-6 lg:px-6">
          <div className="sticky top-0 z-10 bg-surface-base px-6 pt-4 pb-4 lg:px-0">
            <Header
              title={title}
              titleSize="text-heading-100"
              stackButtonsOnPhone={false}
              icon={
                <DismissCircleDark
                  width="w-6"
                  className={isBusy ? "cursor-default text-text-primary-30" : "cursor-pointer"}
                />
              }
              iconOnClick={() => {
                if (!isBusy) {
                  onDismiss?.();
                }
              }}
              iconButtonClassName="!p-0"
              iconTextColor={isBusy ? "text-text-primary-30" : "text-text-primary"}
              iconVariant={variants.textOnly}
              inline
              buttonSize={sizes.base}
              buttonsWrapperClassName="hidden lg:block"
              buttons={buttons}
            >
              {/* Mobile buttons: ellipsis + primary CTA */}
              <div className="ml-3 lg:hidden">
                <ButtonGroup buttons={mobileButtons} variant={groupVariants.rightAligned} size={sizes.base} />
              </div>
            </Header>
          </div>

          {abovePanes}

          {loadingState ?? (
            <div className="mx-auto flex w-full flex-1" style={maxWidth !== "none" ? { maxWidth } : undefined}>
              <div className={paneContainerClassName ?? defaultPaneContainerClassName}>
                {primaryPane}
                {secondaryPane}
              </div>
            </div>
          )}
        </div>
      </div>

      {showOverflowSheet && <OverflowActionSheet overflowButtons={overflowButtons} onClose={closeSheet} />}
    </PageOverlay>
  );
};

export default FullScreenTwoPaneModal;
export type { FullScreenTwoPaneModalProps };
