import { motion } from "motion/react";
import { ReactNode, useCallback, useRef } from "react";
import clsx from "clsx";

import { sizes } from "./constants";
import { Dismiss } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import { ButtonProps } from "@/shared/components/ButtonGroup";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useKeyDown } from "@/shared/hooks/useKeyDown";
import useSlideUpAnimation from "@/shared/hooks/useSlideUpAnimation";

const sizeClasses: Record<keyof typeof sizes, string> = {
  standard: "w-[min(calc(100vw-(--spacing(4))),640px)]",
  large: "w-[min(calc(100vw-(--spacing(4))),1280px)]",
  fullscreen: "h-full w-full max-w-full overflow-y-auto rounded-none",
};

// optional prop to delay close modal on clicking button and allow animations to finish
interface ModalButtonProps extends ButtonProps {
  dismissModalOnClick?: boolean;
}

interface ModalProps {
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
  hideHeaderOnPhone?: boolean;
  headerSpacingClassName?: string;
  contentHeader?: string;
  contentHeaderClassName?: string;
  onDismiss?: (buttonClicked?: boolean) => void;
  buttonSize?: keyof typeof buttonSizes;
  buttons?: ModalButtonProps[];
  phoneFooterButtons?: ModalButtonProps[];
  phoneSheet?: boolean;
  icon?: ReactNode | null;
  iconAriaLabel?: string;
  onIconClick?: () => void;
  open?: boolean;
  showHeader?: boolean;
  title?: string;
  description?: string;
  divider?: boolean;
  size?: keyof typeof sizes;
  zIndex?: string;
}

const Modal = ({
  children,
  className,
  bodyClassName,
  hideHeaderOnPhone = false,
  headerSpacingClassName = "mt-6",
  contentHeader,
  contentHeaderClassName,
  icon = <Dismiss />,
  onIconClick,
  onDismiss,
  buttonSize,
  buttons,
  phoneFooterButtons,
  phoneSheet = false,
  open,
  showHeader = true,
  title,
  description,
  divider = true,
  size = sizes.standard,
  zIndex,
  iconAriaLabel = "Close dialog",
}: ModalProps) => {
  const ModalRef = useRef<HTMLDivElement>(null);
  const slideUpAnimation = useSlideUpAnimation();
  const hasPhoneFooterButtons = (phoneFooterButtons?.length ?? 0) > 0;

  const dismissModal = useCallback(() => {
    onDismiss?.();
  }, [onDismiss]);

  const onButtonClick = useCallback(
    (button?: ModalButtonProps) => () => {
      button?.onClick?.();
      if (button?.variant === variants.primary && button?.dismissModalOnClick !== false) {
        onDismiss?.(true);
      }
    },
    [onDismiss],
  );

  const handleEscape = useCallback(() => {
    if (open !== false) {
      dismissModal();
    }
  }, [open, dismissModal]);
  useKeyDown({ key: "Escape", onKeyDown: handleEscape });

  const shouldIgnoreClickOutside = useCallback(() => open === false, [open]);
  useClickOutside({
    ref: ModalRef,
    onClickOutside: dismissModal,
    ignoreSelectors: [".popover-content"],
    shouldIgnore: shouldIgnoreClickOutside,
  });
  const headerIconProps =
    icon === null
      ? {}
      : {
          icon,
          iconAriaLabel,
          iconOnClick: onIconClick || dismissModal,
        };

  return (
    <PageOverlay open={open} position="top" {...(zIndex && { zIndex })}>
      <motion.div
        {...slideUpAnimation}
        className={clsx(
          "relative h-fit rounded-3xl bg-surface-elevated-base p-6 shadow-300",
          sizeClasses[size],
          {
            "mt-16 max-h-[calc(100vh-(--spacing(32)))] overflow-auto": size !== sizes.fullscreen,
            "pt-0": showHeader,
            "phone:pt-6": hideHeaderOnPhone,
            "phone:mt-auto phone:mb-3 phone:w-[calc(100vw-theme(spacing.6))] phone:max-w-none phone:min-w-[calc(100vw-theme(spacing.6))] phone:rounded-[16px]":
              phoneSheet && size !== sizes.fullscreen,
          },
          className,
        )}
        ref={ModalRef}
        data-testid="modal"
      >
        {showHeader && (
          <div
            className={clsx("sticky top-0 z-10 bg-surface-elevated-base pt-6", { "phone:hidden": hideHeaderOnPhone })}
          >
            <Header
              title={title}
              description={description}
              titleSize="text-heading-300"
              {...headerIconProps}
              buttonSize={buttonSize}
              buttonsWrapperClassName={hasPhoneFooterButtons ? "phone:hidden" : undefined}
              buttons={buttons?.map((button) => ({
                ...button,
                onClick: onButtonClick(button),
              }))}
              inline
              centerButton
            />
            {divider ? <Divider className={headerSpacingClassName} /> : <div className={headerSpacingClassName} />}
          </div>
        )}
        {contentHeader && (
          <div
            className={clsx(
              "mb-1 text-heading-300 text-text-primary",
              { "phone:mb-0": hideHeaderOnPhone },
              contentHeaderClassName,
            )}
          >
            {contentHeader}
          </div>
        )}
        <div className={clsx("text-300 text-text-primary-70", bodyClassName)}>{children}</div>
        {hasPhoneFooterButtons ? (
          <div className="mt-6 hidden w-full gap-3 phone:flex">
            {phoneFooterButtons?.map((button, index) => (
              <Button
                key={button.testId ?? index}
                {...button}
                size={buttonSize}
                className={clsx("grow", button.className)}
                onClick={onButtonClick(button)}
              />
            ))}
          </div>
        ) : null}
      </motion.div>
    </PageOverlay>
  );
};

export default Modal;
