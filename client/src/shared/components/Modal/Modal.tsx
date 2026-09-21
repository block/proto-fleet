import { motion } from "motion/react";
import { ReactNode, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import clsx from "clsx";

import { sizes } from "./constants";
import { Dismiss } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import { ButtonProps } from "@/shared/components/ButtonGroup";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import ModalHeaderActions from "@/shared/components/ModalHeaderActions";
import PageOverlay from "@/shared/components/PageOverlay";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
import useSlideUpAnimation from "@/shared/hooks/useSlideUpAnimation";

const sizeClasses: Record<keyof typeof sizes, string> = {
  standard: "w-[min(calc(100vw-(--spacing(4))),640px)]",
  large: "w-[min(calc(100vw-(--spacing(4))),1280px)]",
  fullscreen: "h-full w-full max-w-full overflow-y-auto rounded-none",
};

const DEFAULT_FULLSCREEN_MAX_WIDTH = "1920px";

// optional prop to delay close modal on clicking button and allow animations to finish
interface ModalButtonProps extends ButtonProps {
  dismissModalOnClick?: boolean;
}

interface ModalProps {
  children: ReactNode;
  className?: string;
  surfaceClassName?: string;
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
  dialogLabel?: string;
  description?: string;
  divider?: boolean;
  size?: keyof typeof sizes;
  zIndex?: string;
  testId?: string;
  forceTitleCollapsed?: boolean;
  manageFocus?: boolean;
  /** Preserve scroll separately for each step of an in-modal navigation flow. */
  navigationKey?: string;
  fixedFooter?: ReactNode;
}

const Modal = ({
  children,
  className,
  surfaceClassName,
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
  dialogLabel,
  description,
  divider = true,
  size = sizes.standard,
  zIndex,
  iconAriaLabel = "Close dialog",
  testId = "modal",
  forceTitleCollapsed = false,
  manageFocus = false,
  navigationKey,
  fixedFooter,
}: ModalProps) => {
  const scrollRef = useRef<HTMLDivElement>(null);
  const scrollPositions = useRef(new Map<string, number>());
  const previousNavigationKey = useRef(navigationKey);
  useLayoutEffect(() => {
    if (navigationKey === undefined || !scrollRef.current) return;
    scrollRef.current.scrollTop = scrollPositions.current.get(navigationKey) ?? 0;
    if (manageFocus && previousNavigationKey.current !== navigationKey)
      scrollRef.current.querySelector<HTMLElement>("button")?.focus({ preventScroll: true });
    previousNavigationKey.current = navigationKey;
  }, [navigationKey, manageFocus]);
  const headerRef = useRef<HTMLDivElement>(null);
  const titleRef = useRef<HTMLDivElement>(null);
  const [isTitleCollapsed, setIsTitleCollapsed] = useState(false);
  const isFullscreen = size === sizes.fullscreen;
  const showTitleInHeader = isFullscreen || isTitleCollapsed || forceTitleCollapsed;
  const slideUpAnimation = useSlideUpAnimation();
  const hasPhoneFooterButtons = (phoneFooterButtons?.length ?? 0) > 0;
  const isPhoneSheet = phoneSheet && size !== sizes.fullscreen;

  useEffect(() => {
    if (!manageFocus || open === false) return;
    const dialog = scrollRef.current?.closest<HTMLElement>('[role="dialog"]');
    if (!dialog) return;
    const previous = document.activeElement;
    const focusableElements = () =>
      [...dialog.querySelectorAll<HTMLElement>("button, input, select, textarea, a[href], [tabindex]")].filter(
        (element) => !element.matches(':disabled, [tabindex="-1"]') && element.getClientRects().length > 0,
      );
    focusableElements()[0]?.focus({ preventScroll: true });
    const trapFocus = (event: KeyboardEvent) => {
      if (event.key !== "Tab" || [...document.querySelectorAll('[role="dialog"]')].slice(-1)[0] !== dialog) return;
      const targets = focusableElements();
      const index = targets.findIndex((button) => button === document.activeElement);
      if (
        targets.length &&
        (index < 0 || (event.shiftKey && index === 0) || (!event.shiftKey && index === targets.length - 1))
      ) {
        event.preventDefault();
        targets[event.shiftKey ? targets.length - 1 : 0].focus();
      }
    };
    document.addEventListener("keydown", trapFocus);
    return () => {
      document.removeEventListener("keydown", trapFocus);
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus({ preventScroll: true });
    };
  }, [manageFocus, open]);

  useEffect(() => {
    if (open === false || !title || !titleRef.current || !scrollRef.current) {
      setIsTitleCollapsed(false);
      return;
    }

    const headerHeight = headerRef.current?.offsetHeight ?? 0;
    const scroller = scrollRef.current;

    const observer = new IntersectionObserver(
      ([entry]) => {
        // Ignore edge rounding during the opening animation and stale observer
        // notifications after scrolling back to the top.
        setIsTitleCollapsed(scroller.scrollTop > 1 && !entry.isIntersecting);
      },
      {
        root: scroller,
        rootMargin: `-${headerHeight}px 0px 0px 0px`,
        threshold: 0,
      },
    );

    // Observe the full title: collapse only once it has passed under the header,
    // rather than testing a zero-size marker on the header's exact boundary.
    observer.observe(titleRef.current);

    return () => observer.disconnect();
  }, [title, showHeader, open, isFullscreen, forceTitleCollapsed]);

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
  const headerButtons = buttons?.map((button) => ({
    ...button,
    onClick: onButtonClick(button),
  }));

  useEscapeDismiss(open === false ? undefined : dismissModal);

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
        role={dialogLabel ? "dialog" : undefined}
        aria-modal={dialogLabel ? true : undefined}
        aria-label={dialogLabel}
        className={clsx(
          "h-fit overflow-hidden rounded-3xl bg-surface-elevated-base shadow-300",
          sizeClasses[size],
          {
            "flex flex-col": fixedFooter,
            "mt-16 max-h-[calc(100dvh-(--spacing(32)))]": size !== sizes.fullscreen,
            "phone:mt-10 phone:max-h-[calc(100dvh-theme(spacing.10))] phone:w-screen phone:max-w-none phone:min-w-[100vw] phone:rounded-[16px]":
              size !== sizes.fullscreen && !isPhoneSheet,
            "phone:mt-auto phone:mb-3 phone:w-[calc(100vw-theme(spacing.6))] phone:max-w-none phone:min-w-[calc(100vw-theme(spacing.6))] phone:rounded-[16px]":
              isPhoneSheet,
          },
          surfaceClassName,
        )}
        style={isFullscreen ? { maxWidth: DEFAULT_FULLSCREEN_MAX_WIDTH } : undefined}
      >
        <motion.div
          {...slideUpAnimation}
          className={clsx(
            "relative p-6",
            {
              "min-h-0 flex-1": fixedFooter,
              "max-h-[calc(100dvh-(--spacing(32)))] overflow-auto phone:max-h-[calc(100dvh-theme(spacing.10))]":
                size !== sizes.fullscreen,
              "h-full": isFullscreen,
              "pt-0": showHeader,
              "phone:pt-6": hideHeaderOnPhone,
            },
            className,
          )}
          ref={scrollRef}
          onScroll={(event) => {
            if (navigationKey !== undefined) scrollPositions.current.set(navigationKey, event.currentTarget.scrollTop);
            if (event.currentTarget.scrollTop <= 1) setIsTitleCollapsed(false);
          }}
          data-testid={testId}
        >
          {showHeader ? (
            <div
              ref={headerRef}
              // Two things keep the sticky background actually opaque:
              //
              // flex-col — headerSpacingClassName is a margin on an empty
              // trailing div, and in normal flow that margin collapses out of
              // this box. The header painted 16px shorter than it occupied, so
              // body content scrolled through the gap. Flex items don't collapse.
              //
              // -mx-6 px-6 — the scroll container's own px-6 would otherwise
              // leave a 24px gutter down each side of the header, where borders
              // and other body content stayed visible. The header has to reach
              // the container's padding edge; the padding puts its contents back.
              // Safe against the callers that pass `!p-0`, since those all set
              // showHeader={false}.
              className={clsx("sticky top-0 z-10 -mx-6 flex flex-col bg-surface-elevated-base px-6 pt-6", {
                "phone:hidden": hideHeaderOnPhone,
              })}
            >
              <div className="relative">
                <Header
                  title={showTitleInHeader ? title : undefined}
                  titleSize="truncate text-heading-200"
                  className={hasPhoneFooterButtons ? undefined : "phone:pr-40"}
                  {...headerIconProps}
                  buttonSize={buttonSize}
                  buttonsWrapperClassName="phone:hidden"
                  buttons={headerButtons}
                  inline
                  centerButton
                />
                {hasPhoneFooterButtons ? null : (
                  <ModalHeaderActions
                    className="absolute top-0 right-0 !ml-0"
                    buttons={headerButtons}
                    buttonSize={buttonSize}
                  />
                )}
              </div>
              {divider ? (
                // Reserve the divider's height so toggling the title cannot
                // move the content being observed and trigger another toggle.
                <Divider className={clsx(headerSpacingClassName, !showTitleInHeader && "invisible")} />
              ) : (
                <div className={headerSpacingClassName} />
              )}
            </div>
          ) : null}
          {title && !isFullscreen && !forceTitleCollapsed ? (
            <>
              <div
                ref={titleRef}
                className={clsx("text-heading-300 text-text-primary", description ? "mb-1" : "mb-4", {
                  "phone:mb-0": hideHeaderOnPhone,
                })}
              >
                {title}
              </div>
            </>
          ) : null}
          {description ? <div className="mb-4 max-w-[600px] text-300 text-text-primary-70">{description}</div> : null}
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
        {fixedFooter ? <div className="shrink-0 px-6 pb-6">{fixedFooter}</div> : null}
      </div>
    </PageOverlay>
  );
};

export default Modal;
