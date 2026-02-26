import { motion } from "motion/react";
import { ReactNode, useCallback, useRef } from "react";
import clsx from "clsx";

import { sizes } from "./constants";
import { Dismiss } from "@/shared/assets/icons";
import { sizes as buttonSizes, variants } from "@/shared/components/Button";

import { ButtonProps } from "@/shared/components/ButtonGroup";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useKeyDown } from "@/shared/hooks/useKeyDown";
import useSlideUpAnimation from "@/shared/hooks/useSlideUpAnimation";

// optional prop to delay close modal on clicking button and allow animations to finish
interface ModalButtonProps extends ButtonProps {
  dismissModalOnClick?: boolean;
}

interface ModalProps {
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
  contentHeader?: string;
  contentHeaderClassName?: string;
  onDismiss?: (buttonClicked?: boolean) => void;
  buttonSize?: keyof typeof buttonSizes;
  buttons?: ModalButtonProps[];
  icon?: ReactNode | null;
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
  contentHeader,
  contentHeaderClassName,
  icon = <Dismiss />,
  onIconClick,
  onDismiss,
  buttonSize,
  buttons,
  open,
  showHeader = true,
  title,
  description,
  divider = true,
  size = sizes.large,
  zIndex,
}: ModalProps) => {
  const ModalRef = useRef<HTMLDivElement>(null);
  const slideUpAnimation = useSlideUpAnimation();

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

  return (
    <PageOverlay open={open} position="top" {...(zIndex && { zIndex })}>
      <motion.div
        {...slideUpAnimation}
        className={clsx(
          "relative h-fit rounded-3xl bg-surface-elevated-base p-6 shadow-300",
          {
            "min-w-[min(calc(100vw-(--spacing(4))),360px)]": size === sizes.small,
            "min-w-[min(calc(100vw-(--spacing(4))),640px)]": size === sizes.large,
            "min-w-[min(calc(100vw-(--spacing(4))),1024px)]": size === sizes.extraLarge,
            "max-w-[640px]": size === sizes.small,
            "max-w-[1024px]": size === sizes.large,
            "max-w-[1280px]": size === sizes.extraLarge,
            "h-full w-full max-w-full overflow-y-auto rounded-none": size === sizes.fullscreen,
            "mt-16 max-h-[calc(100vh-(--spacing(32)))] overflow-auto": size !== sizes.fullscreen,
            "pt-0": showHeader,
          },
          className,
        )}
        ref={ModalRef}
        data-testid="modal"
      >
        {showHeader && (
          <div className="sticky top-0 z-10 bg-surface-elevated-base pt-6">
            <Header
              title={title}
              description={description}
              titleSize="text-heading-200"
              icon={icon == null ? undefined : icon}
              iconOnClick={icon === null ? undefined : onIconClick || dismissModal}
              buttonSize={buttonSize}
              buttons={buttons?.map((button) => ({
                ...button,
                onClick: onButtonClick(button),
              }))}
              inline
              centerButton
            />
            {divider ? <Divider className="mt-6" /> : <div className="mt-6" />}
          </div>
        )}
        {contentHeader && (
          <div className={clsx("mb-1 text-heading-200 text-text-primary", contentHeaderClassName)}>{contentHeader}</div>
        )}
        <div className={clsx("text-300 text-text-primary-70", bodyClassName)}>{children}</div>
      </motion.div>
    </PageOverlay>
  );
};

export default Modal;
