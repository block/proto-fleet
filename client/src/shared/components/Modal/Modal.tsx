import { motion } from "motion/react";
import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { sizes } from "./constants";
import { Dismiss } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import { ButtonProps } from "@/shared/components/ButtonGroup";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";
import { useClickOutsideDismiss } from "@/shared/hooks/useClickOutsideDismiss";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
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
  headerSpacingClassName = "mt-4",
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
  const modalRef = useRef<HTMLDivElement>(null);
  const headerRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const [isTitleCollapsed, setIsTitleCollapsed] = useState(false);
  const isFullscreen = size === sizes.fullscreen;
  const showTitleInHeader = isFullscreen || isTitleCollapsed;
  const slideUpAnimation = useSlideUpAnimation();
  const hasPhoneFooterButtons = (phoneFooterButtons?.length ?? 0) > 0;

  useEffect(() => {
    if (!title || !sentinelRef.current || !modalRef.current) {
      setIsTitleCollapsed(false);
      return;
    }

    const headerHeight = headerRef.current?.offsetHeight ?? 0;

    const observer = new IntersectionObserver(
      ([entry]) => {
        setIsTitleCollapsed(!entry.isIntersecting);
      },
      {
        root: modalRef.current,
        rootMargin: `-${headerHeight}px 0px 0px 0px`,
        threshold: 0,
      },
    );

    observer.observe(sentinelRef.current);

    return () => observer.disconnect();
  }, [title, showHeader]);

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

  useEscapeDismiss(open === false ? undefined : dismissModal);

  useClickOutsideDismiss({
    ref: modalRef,
    onDismiss: open === false ? undefined : dismissModal,
    ignoreSelectors: [".popover-content"],
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
      <div
        className={clsx("h-fit overflow-hidden rounded-3xl bg-surface-elevated-base shadow-300", sizeClasses[size], {
          "mt-16 max-h-[calc(100vh-(--spacing(32)))]": size !== sizes.fullscreen,
          "phone:mt-auto phone:mb-3 phone:w-[calc(100vw-theme(spacing.6))] phone:max-w-none phone:min-w-[calc(100vw-theme(spacing.6))] phone:rounded-[16px]":
            phoneSheet && size !== sizes.fullscreen,
        })}
      >
        <motion.div
          {...slideUpAnimation}
          className={clsx(
            "relative p-6",
            {
              "max-h-[calc(100vh-(--spacing(32)))] overflow-auto": size !== sizes.fullscreen,
              "h-full": isFullscreen,
              "pt-0": showHeader,
              "phone:pt-6": hideHeaderOnPhone,
            },
            className,
          )}
          ref={modalRef}
          data-testid="modal"
        >
          {showHeader && (
            <div
              ref={headerRef}
              className={clsx("sticky top-0 z-10 bg-surface-elevated-base pt-6", { "phone:hidden": hideHeaderOnPhone })}
            >
              <Header
                title={showTitleInHeader ? title : undefined}
                titleSize="text-heading-200"
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
              {divider && showTitleInHeader ? (
                <Divider className={headerSpacingClassName} />
              ) : (
                <div className={headerSpacingClassName} />
              )}
            </div>
          )}
          {title && !isFullscreen && (
            <>
              <div ref={sentinelRef} className="h-0 w-0" />
              <div
                className={clsx("text-heading-300 text-text-primary", description ? "mb-1" : "mb-4", {
                  "phone:mb-0": hideHeaderOnPhone,
                })}
              >
                {title}
              </div>
            </>
          )}
          {description && <div className="mb-4 max-w-[600px] text-300 text-text-primary-70">{description}</div>}
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
      </div>
    </PageOverlay>
  );
};

export default Modal;
