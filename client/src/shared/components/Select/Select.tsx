import { useEffect, useRef, useState } from "react";
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
  error?: boolean | string;
  testId?: string;
  className?: string;
}

const SelectContent = ({ id, label, options, value, onChange, disabled, error, testId, className }: SelectProps) => {
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

  // Track trigger width so the portal-rendered popover matches
  const [triggerWidth, setTriggerWidth] = useState<number | undefined>();
  const [popoverMaxHeight, setPopoverMaxHeight] = useState<number | undefined>();

  useEffect(() => {
    if (!open || !triggerRef.current) {
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
      const shouldOpenAbove = spaceAbove > spaceBelow;

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
  }, [open, triggerRef]);

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
                hasValue || open ? "top-[7px] text-200" : "top-1/2 -translate-y-1/2 text-300",
              )}
            >
              {label}
            </span>
            {hasValue ? <span className="truncate text-300 text-text-primary">{selectedLabel}</span> : null}
          </div>
          <ChevronDown
            width="w-3"
            className={clsx("shrink-0 text-text-primary-70 transition-transform", { "rotate-180": open })}
          />
        </button>
      </div>
      {open ? (
        <Popover
          position={popoverPosition}
          className="!w-auto !space-y-0 !rounded-xl border border-border-5 !bg-surface-elevated-base !p-0 !shadow-300 !backdrop-blur-none"
          closePopover={() => setOpen(false)}
          closeIgnoreSelectors={[`[data-testid='${testId}']`, `#${id}`]}
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
            {options.map((opt) => (
              <div
                key={opt.value}
                role="option"
                aria-selected={value === opt.value ? "true" : "false"}
                className={clsx(
                  "flex cursor-pointer items-center gap-3 rounded-xl p-3 text-left select-none",
                  "transition-[background-color] duration-200 ease-in-out",
                  "text-text-primary hover:bg-core-primary-5",
                )}
                onClick={() => {
                  onChange(opt.value);
                  setOpen(false);
                }}
              >
                <Radio selected={value === opt.value} />
                <div className="min-w-0 grow">
                  <div className="truncate text-emphasis-300">{opt.label}</div>
                  {opt.description ? <div className="text-200 text-text-primary-70">{opt.description}</div> : null}
                </div>
              </div>
            ))}
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
