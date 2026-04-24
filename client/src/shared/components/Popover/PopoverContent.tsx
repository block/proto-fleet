import { MouseEvent, useEffect, useLayoutEffect, useRef } from "react";
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
}: PopoverContentProps) => {
  const popoverRef = useRef<HTMLDivElement>(null);
  const { triggerRef, renderMode } = usePopover();

  useClickOutside({
    ref: popoverRef,
    onClickOutside: closePopover ?? (() => {}),
    ignoreSelectors: closeIgnoreSelectors,
  });
  useEscapeDismiss(closePopover);

  useLayoutEffect(() => {
    if (renderMode !== "portal-scrolling") {
      return;
    }

    if (popoverRef.current?.contains(document.activeElement)) {
      return;
    }

    const firstFocusableElement = popoverRef.current?.querySelector<HTMLElement>(FOCUSABLE_SELECTOR);
    firstFocusableElement?.focus();
  }, [renderMode]);

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

  return (
    <div
      ref={popoverRef}
      className={clsx(
        "popover-content z-20 space-y-4 rounded-3xl bg-surface-elevated-base/85 p-6 shadow-200 backdrop-blur-[7px] transition-opacity duration-200",
        {
          "w-60": size === popoverSizes.small,
          "w-72": size === popoverSizes.medium,
          "w-80": size === popoverSizes.normal,
        },
        className,
      )}
      data-testid={testId}
      onClick={handleClick}
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
