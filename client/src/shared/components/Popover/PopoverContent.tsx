import { type KeyboardEvent, MouseEvent, useEffect, useLayoutEffect, useRef } from "react";
import clsx from "clsx";
import ButtonGroup, { groupVariants, sizes } from "@/shared/components/ButtonGroup";
import Header from "@/shared/components/Header";
import { popoverSizes } from "@/shared/components/Popover/constants.ts";
import { PopoverContentProps } from "@/shared/components/Popover/types";
import { usePopover } from "@/shared/components/Popover/usePopover";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";

const FOCUSABLE_SELECTOR =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

// TODO content of this component can be moved to Popover when ThemeSwitcher does not use this component anymore
const PopoverContent = ({
  buttonGroupVariant = groupVariants.fill,
  buttons,
  children,
  className,
  size = popoverSizes.normal,
  subtitle,
  testId,
  title,
  closePopover,
  titleSize = "text-heading-200",
  closeIgnoreSelectors,
  closeShouldIgnore,
  isPositioned = true,
}: PopoverContentProps & { isPositioned?: boolean }) => {
  const popoverRef = useRef<HTMLDivElement>(null);
  const { triggerRef, renderMode } = usePopover();

  useClickOutside({
    ref: popoverRef,
    onClickOutside: closePopover ?? (() => {}),
    ignoreSelectors: closeIgnoreSelectors,
    shouldIgnore: closeShouldIgnore,
  });
  useEscapeDismiss(closePopover);

  useLayoutEffect(() => {
    if (renderMode !== "portal-scrolling" || !isPositioned) {
      return;
    }

    if (popoverRef.current?.contains(document.activeElement)) {
      return;
    }

    const firstFocusableElement = popoverRef.current?.querySelector<HTMLElement>(FOCUSABLE_SELECTOR);
    firstFocusableElement?.focus();
  }, [renderMode, isPositioned]);

  useEffect(() => {
    const handleFocusIn = (event: FocusEvent) => {
      if (!(event.target instanceof Node)) {
        return;
      }

      const isInsidePopover = popoverRef.current?.contains(event.target) ?? false;
      const isInsideTrigger = triggerRef.current?.contains(event.target) ?? false;

      if (!isInsidePopover && !isInsideTrigger) {
        closePopover?.();
      }
    };

    document.addEventListener("focusin", handleFocusIn);

    return () => {
      document.removeEventListener("focusin", handleFocusIn);
    };
  }, [closePopover, triggerRef]);

  // Stop propagation to prevent modal close
  const handleClick = (e: MouseEvent) => {
    e.stopPropagation();
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "Tab") return;
    const popover = popoverRef.current;
    const trigger = triggerRef.current;
    const dialog = trigger?.closest<HTMLElement>('[role="dialog"]');
    if (!popover || !trigger || !dialog || dialog.contains(popover)) return;
    const dialogs = document.querySelectorAll('[role="dialog"]');
    if (dialogs[dialogs.length - 1] !== dialog) return;

    const focusableElements = (root: HTMLElement) =>
      [...root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)].filter(
        (element) =>
          !element.matches(':disabled, [tabindex="-1"]') &&
          !element.closest("[inert]") &&
          element.getClientRects().length > 0,
      );
    // A body portal belongs immediately after its trigger in the dialog's Tab order.
    const targets = focusableElements(dialog);
    const triggerIndex = targets.reduce((last, element, index) => (trigger.contains(element) ? index : last), -1);
    if (triggerIndex < 0) return;
    targets.splice(triggerIndex + 1, 0, ...focusableElements(popover));
    const current = targets.findIndex((element) => element === document.activeElement);
    if (current < 0) return;
    const next = targets[(current + (event.shiftKey ? -1 : 1) + targets.length) % targets.length];
    event.preventDefault();
    event.stopPropagation();
    next.focus();
    if (!popover.contains(next)) closePopover?.();
  };

  return (
    <div
      ref={popoverRef}
      className={clsx(
        "popover-content z-20 space-y-4 rounded-3xl bg-surface-elevated-base/85 p-6 shadow-200 backdrop-blur-[7px] transition-opacity duration-200",
        {
          "w-60": size === popoverSizes.small,
          "w-72": size === popoverSizes.medium,
          "w-80": size === popoverSizes.normal,
          // Twice the medium panel, for content that reads as rows rather than a paragraph. Capped to the
          // viewport: the positioner pins an over-wide panel to the left margin and lets the rest run off-screen.
          "w-144 max-w-[calc(100vw-1rem)]": size === popoverSizes.wide,
        },
        className,
      )}
      data-testid={testId}
      onClick={handleClick}
      onKeyDownCapture={handleKeyDown}
    >
      {title || subtitle ? (
        <Header title={title} titleSize={titleSize} subtitle={subtitle} subtitleSize="text-300" />
      ) : null}
      {children}
      {buttons ? <ButtonGroup buttons={buttons} variant={buttonGroupVariant} size={sizes.base} /> : null}
    </div>
  );
};

export default PopoverContent;
