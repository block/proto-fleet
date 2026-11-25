import { type KeyboardEvent, type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";
import clsx from "clsx";

import { ChevronDown } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
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
  variant?: "field" | "filter";
  showSelectedIndicator?: boolean;
  suffixAction?: ReactNode;
  /** Search control rendered above the options. The caller owns filtering. */
  search?: ReactNode;
  isLoading?: boolean;
  hasMore?: boolean;
  onLoadMore?: () => void;
  loadError?: string | null;
  onRetry?: () => void;
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
  variant = "field",
  showSelectedIndicator = true,
  suffixAction,
  search,
  isLoading = false,
  hasMore = false,
  onLoadMore,
  loadError,
  onRetry,
  forceBelow,
}: SelectProps) => {
  const [open, setOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const [popoverPosition, setPopoverPosition] = useState<Position>(positions["bottom right"]);
  const listboxRef = useRef<HTMLDivElement>(null);
  const loadMoreRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLDivElement>(null);

  const close = () => {
    setOpen(false);
    triggerRef.current?.querySelector("button")?.focus({ preventScroll: true });
  };

  const selectOption = (value: string) => {
    onChange(value);
    close();
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      close();
      return;
    }
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
    const items = Array.from(listboxRef.current?.querySelectorAll<HTMLElement>('[role="option"]') ?? []);
    if (!items.length) return;
    const current = items.findIndex((item) => item === document.activeElement);
    // Home and End keep their text-editing behavior in the search field.
    if (current < 0 && (event.key === "Home" || event.key === "End")) return;
    event.preventDefault();
    const lastIndex = items.length - 1;
    let nextIndex = current;
    switch (event.key) {
      case "Home":
        nextIndex = 0;
        break;
      case "End":
        nextIndex = lastIndex;
        break;
      case "ArrowDown":
        nextIndex = Math.min(current + 1, lastIndex);
        break;
      case "ArrowUp":
        nextIndex = current < 0 ? lastIndex : Math.max(current - 1, 0);
        break;
    }
    items[nextIndex].focus({ preventScroll: true });
    items[nextIndex].scrollIntoView?.({ block: "nearest" });
  };

  useEffect(() => {
    if (!open || !hasMore || isLoading || loadError || !onLoadMore || !loadMoreRef.current) return;
    let active = true;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (active && entry?.isIntersecting) {
          active = false;
          onLoadMore();
        }
      },
      { root: listboxRef.current, rootMargin: "0px 0px 80px 0px" },
    );
    observer.observe(loadMoreRef.current);
    return () => {
      active = false;
      observer.disconnect();
    };
  }, [open, hasMore, isLoading, loadError, onLoadMore]);

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
          aria-controls={open ? `${id}-options` : undefined}
          disabled={disabled}
          onClick={() => !disabled && setOpen((prev) => !prev)}
          className={clsx(
            "peer flex items-center justify-between text-left outline-hidden",
            variant === "filter"
              ? "h-8 gap-2 rounded-full px-3 text-emphasis-300 text-text-primary-70"
              : "h-14 w-full rounded-lg pr-4 pl-4",
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
          {variant === "filter" ? (
            <span className="truncate">{value ? displayLabel : label}</span>
          ) : (
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
          )}
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
            className="flex min-h-0 flex-col"
            onKeyDownCapture={handleKeyDown}
            style={{
              minWidth: triggerWidth,
              width: search ? triggerWidth : undefined,
              maxHeight: popoverMaxHeight,
            }}
          >
            {search ? (
              <div ref={searchRef} className="shrink-0 border-b border-border-5 p-3">
                {search}
              </div>
            ) : null}
            <div
              id={`${id}-options`}
              ref={listboxRef}
              className={clsx("relative min-h-0 overflow-y-auto overscroll-contain p-1.5", {
                "max-h-80": search || onLoadMore,
              })}
              role="listbox"
              aria-label={`${label} options`}
              aria-busy={isLoading || undefined}
            >
              {options.length === 0 && !isLoading && !loadError ? (
                <div className="rounded-xl p-3 text-300 text-text-primary-50">{emptyMessage}</div>
              ) : (
                options.map((opt) => (
                  <div
                    key={opt.value}
                    role="option"
                    tabIndex={0}
                    aria-selected={value === opt.value ? "true" : "false"}
                    className={clsx(
                      "flex w-full cursor-pointer items-center rounded-xl p-3 text-left select-none focus-visible:bg-core-primary-5 focus-visible:outline-none",
                      "transition-[background-color] duration-200 ease-in-out",
                      "text-text-primary hover:bg-core-primary-5",
                      { "gap-3": showSelectedIndicator },
                    )}
                    onClick={() => selectOption(opt.value)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        selectOption(opt.value);
                      }
                    }}
                  >
                    {showSelectedIndicator ? (
                      <div aria-hidden="true" inert className="pointer-events-none">
                        <Radio selected={value === opt.value} />
                      </div>
                    ) : null}
                    <div className="min-w-0 grow">
                      <div className="truncate text-emphasis-300">{opt.label}</div>
                      {opt.description ? <div className="text-200 text-text-primary-70">{opt.description}</div> : null}
                    </div>
                  </div>
                ))
              )}
              {hasMore && !loadError ? <div ref={loadMoreRef} className="h-px" /> : null}
            </div>
            {isLoading ? (
              <div role="status" className="shrink-0 p-3 text-300 text-text-primary-50">
                Loading…
              </div>
            ) : null}
            {loadError ? (
              <div className="flex shrink-0 items-center gap-3 p-3">
                <p role="alert" className="text-200 text-text-critical">
                  {loadError}
                </p>
                {onRetry ? (
                  <Button
                    variant="secondary"
                    size="compact"
                    text="Retry"
                    onClick={() => {
                      // Retry disappears while loading, so move focus to a stable control first.
                      const target =
                        searchRef.current?.querySelector("input") ??
                        listboxRef.current?.querySelector<HTMLElement>('[role="option"]') ??
                        triggerRef.current?.querySelector("button");
                      target?.focus({ preventScroll: true });
                      onRetry();
                    }}
                  />
                ) : null}
              </div>
            ) : null}
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
