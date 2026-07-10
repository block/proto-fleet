import { type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";
import clsx from "clsx";

import { ChevronDown } from "@/shared/assets/icons";
import Popover, { PopoverProvider, usePopover } from "@/shared/components/Popover";
import { minimalMargin } from "@/shared/components/Popover/constants";
import Radio from "@/shared/components/Radio";
import { type Position, positions } from "@/shared/constants";

const popoverViewportPadding = minimalMargin * 2;

interface SelectOption {
  value: string;
  label: string;
  description?: string;
}

interface SelectProps {
  id: string;
  label: string;
  options: SelectOption[];
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  emptyMessage?: string;
  error?: boolean | string;
  placeholder?: string;
  testId?: string;
  className?: string;
  showSelectedIndicator?: boolean;
  suffixAction?: ReactNode;
  // Default behavior flips the popover above the trigger when more space is
  // available there. Set forceBelow when the caller knows the dropdown must
  // open downward (e.g. inside a modal whose footer would otherwise hide it).
  forceBelow?: boolean;
}

const SelectContent = ({
  id,
  label,
  options,
  value,
  onChange,
  disabled,
  emptyMessage = "No options",
  error,
  placeholder,
  testId,
  className,
  showSelectedIndicator = true,
  suffixAction,
  forceBelow,
}: SelectProps) => {
  const [open, setOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const [popoverPosition, setPopoverPosition] = useState<Position>(positions["bottom right"]);
  const listboxRef = useRef<HTMLDivElement>(null);

  // Portal to body so the dropdown escapes overflow-hidden/auto containers (e.g. modals)
  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  const selectedLabel = options.find((o) => o.value === value)?.label ?? "";
  const hasValue = selectedLabel.length > 0;
  const displayLabel = selectedLabel || placeholder || "";
  const hasDisplayValue = displayLabel.length > 0;

  // Track trigger width so the portal-rendered popover matches
  const [triggerWidth, setTriggerWidth] = useState<number | undefined>();
  const [popoverMaxHeight, setPopoverMaxHeight] = useState<number | undefined>();

  // useLayoutEffect (not useEffect) so the popover's max-height is applied
  // before the first paint. Otherwise the popover renders at its natural
  // height for one frame, Popover's overflow detector measures that height,
  // and a forceBelow request can still flip to the top.
  useLayoutEffect(() => {
    if (!open || !triggerRef.current) {
      // Reset on close so the next open recomputes against fresh trigger
      // coordinates instead of reusing stale state from the prior open.
      setPopoverMaxHeight(undefined);
      return;
    }

    const updatePopoverLayout = () => {
      if (!triggerRef.current) {
        return;
      }

      const triggerRect = triggerRef.current.getBoundingClientRect();
      const viewportHeight = window.visualViewport?.height ?? window.innerHeight;
      const spaceAbove = triggerRect.top - popoverViewportPadding;
      const spaceBelow = viewportHeight - triggerRect.bottom - popoverViewportPadding;
      const shouldOpenAbove = !forceBelow && spaceAbove > spaceBelow;

      setTriggerWidth(triggerRect.width);
      setPopoverPosition(shouldOpenAbove ? positions["top right"] : positions["bottom right"]);
      setPopoverMaxHeight(Math.max(Math.floor(shouldOpenAbove ? spaceAbove : spaceBelow), 0));
    };

    updatePopoverLayout();

    window.addEventListener("resize", updatePopoverLayout);
    window.visualViewport?.addEventListener("resize", updatePopoverLayout);

    return () => {
      window.removeEventListener("resize", updatePopoverLayout);
      window.visualViewport?.removeEventListener("resize", updatePopoverLayout);
    };
  }, [open, triggerRef, forceBelow]);

  useEffect(() => {
    if (!open || !listboxRef.current) {
      return;
    }

    const frameId = window.requestAnimationFrame(() => {
      const listbox = listboxRef.current;
      const selectedOption = listbox?.querySelector<HTMLElement>('[role="option"][aria-selected="true"]');

      if (!listbox || !selectedOption) {
        return;
      }

      listbox.scrollTop = Math.max(selectedOption.offsetTop - minimalMargin, 0);
    });

    return () => window.cancelAnimationFrame(frameId);
  }, [open, value]);

  return (
    <div className={clsx("relative", className)}>
      <div ref={triggerRef}>
        <button
          id={id}
          type="button"
          data-testid={testId}
          aria-label={label}
          aria-haspopup="listbox"
          aria-expanded={open}
          disabled={disabled}
          onClick={() => !disabled && setOpen((prev) => !prev)}
          className={clsx(
            "peer flex h-14 w-full items-center justify-between rounded-lg pr-4 pl-4 text-left outline-hidden",
            "transition duration-200 ease-in-out",
            { "bg-surface-base": !disabled },
            { "bg-core-primary-5": disabled },
            { "border border-intent-critical-50": error && !open },
            { "border border-border-5": !open && !error },
            { "border border-border-20 ring-4 ring-core-primary-5": open && !disabled && !error },
            { "border border-intent-critical-50 ring-4 ring-intent-critical-20": open && !disabled && error },
            { "cursor-pointer": !disabled },
            { "cursor-default": disabled },
          )}
        >
          <div className="flex min-w-0 flex-col pt-[18px]">
            <span
              className={clsx(
                "absolute text-text-primary-50",
                "transition-[top] duration-150 ease-in-out",
                hasDisplayValue || open ? "top-[7px] text-200" : "top-1/2 -translate-y-1/2 text-300",
              )}
            >
              {label}
            </span>
            {hasDisplayValue ? (
              <span className={clsx("truncate text-300", hasValue ? "text-text-primary" : "text-text-primary-50")}>
                {displayLabel}
              </span>
            ) : null}
          </div>
          <ChevronDown
            width="w-3"
            className={clsx("shrink-0 text-text-primary-70 transition-transform", {
              "mr-8": suffixAction,
              "rotate-180": open,
            })}
          />
        </button>
        {suffixAction ? <div className="absolute top-1/2 right-4 z-10 -translate-y-1/2">{suffixAction}</div> : null}
      </div>
      {open ? (
        <Popover
          position={popoverPosition}
          className="!w-auto !space-y-0 !rounded-xl border border-border-5 !bg-surface-elevated-base !p-0 !shadow-300 !backdrop-blur-none"
          closePopover={() => setOpen(false)}
          closeIgnoreSelectors={[`[data-testid='${testId}']`, `#${id}`]}
          // Select picks bottom vs top itself based on forceBelow + available
          // space; Popover's overflow-driven flip would override that decision
          // and could send a forceBelow dropdown back above the trigger.
          disableAutoFlip={forceBelow}
        >
          <div
            ref={listboxRef}
            className="max-h-[calc(100vh-2rem)] overflow-y-auto overscroll-contain p-1.5"
            role="listbox"
            aria-label={`${label} options`}
            style={{
              minWidth: triggerWidth,
              maxHeight: popoverMaxHeight,
            }}
          >
            {options.length === 0 ? (
              <div className="rounded-xl p-3 text-300 text-text-primary-50">{emptyMessage}</div>
            ) : (
              options.map((opt) => (
                <div
                  key={opt.value}
                  role="option"
                  aria-selected={value === opt.value ? "true" : "false"}
                  className={clsx(
                    "flex cursor-pointer items-center rounded-xl p-3 text-left select-none",
                    "transition-[background-color] duration-200 ease-in-out",
                    "text-text-primary hover:bg-core-primary-5",
                    { "gap-3": showSelectedIndicator },
                  )}
                  onClick={() => {
                    onChange(opt.value);
                    setOpen(false);
                  }}
                >
                  {showSelectedIndicator ? <Radio selected={value === opt.value} /> : null}
                  <div className="min-w-0 grow">
                    <div className="truncate text-emphasis-300">{opt.label}</div>
                    {opt.description ? <div className="text-200 text-text-primary-70">{opt.description}</div> : null}
                  </div>
                </div>
              ))
            )}
          </div>
        </Popover>
      ) : null}
      <div
        className={clsx(
          "text-200 text-intent-critical-fill",
          "transition-[opacity,max-height,margin-top] duration-200 ease-in-out",
          { "max-h-0 opacity-0": !error || error === true },
          { "mt-2 max-h-10 opacity-100": error && error !== true },
        )}
      >
        <div className="flex items-center space-x-1">
          <div className="h-1 w-2.5 rounded-full bg-intent-critical-20" />
          <div>{error !== true ? error : null}</div>
        </div>
      </div>
    </div>
  );
};

const Select = (props: SelectProps) => (
  <PopoverProvider>
    <SelectContent {...props} />
  </PopoverProvider>
);

export default Select;
export type { SelectOption, SelectProps };
