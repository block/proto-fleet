import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { ChevronDown } from "@/shared/assets/icons";
import Popover, { PopoverProvider, usePopover } from "@/shared/components/Popover";
import { minimalMargin } from "@/shared/components/Popover/constants";
import Radio from "@/shared/components/Radio";
import { type Position, positions } from "@/shared/constants";

export interface PickerOption {
  id: string;
  label: string;
}

interface SinglePickerFieldProps {
  id: string;
  label: string;
  options: PickerOption[];
  value: string | null;
  placeholder?: string;
  emptyMessage?: string;
  onChange: (value: string) => void;
}

const popoverViewportPadding = minimalMargin * 2;
// Only flip above the trigger when the space below can't fit a reasonable list, to avoid overlapping fields.
const minOpenBelowHeight = 240;

const SinglePickerFieldContent = ({
  id,
  label,
  options,
  value,
  placeholder = "Pick one",
  emptyMessage = "No options",
  onChange,
}: SinglePickerFieldProps) => {
  const [open, setOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const listboxRef = useRef<HTMLDivElement>(null);
  const [popoverPosition, setPopoverPosition] = useState<Position>(positions["bottom right"]);
  const [triggerWidth, setTriggerWidth] = useState<number | undefined>();
  const [popoverMaxHeight, setPopoverMaxHeight] = useState<number | undefined>();

  const picked = options.find((o) => o.id === value) ?? null;
  const displayLabel = picked?.label ?? placeholder;

  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  useEffect(() => {
    if (!open || !triggerRef.current) return;
    const update = () => {
      if (!triggerRef.current) return;
      const rect = triggerRef.current.getBoundingClientRect();
      const viewportHeight = window.visualViewport?.height ?? window.innerHeight;
      const spaceAbove = rect.top - popoverViewportPadding;
      const spaceBelow = viewportHeight - rect.bottom - popoverViewportPadding;
      const openAbove = spaceBelow < minOpenBelowHeight && spaceAbove > spaceBelow;
      setTriggerWidth(rect.width);
      setPopoverPosition(openAbove ? positions["top right"] : positions["bottom right"]);
      setPopoverMaxHeight(Math.max(Math.floor(openAbove ? spaceAbove : spaceBelow), 0));
    };
    update();
    window.addEventListener("resize", update);
    window.visualViewport?.addEventListener("resize", update);
    return () => {
      window.removeEventListener("resize", update);
      window.visualViewport?.removeEventListener("resize", update);
    };
  }, [open, triggerRef]);

  const pickAndClose = (next: string) => {
    onChange(next);
    setOpen(false);
  };

  return (
    <div className="relative">
      <div ref={triggerRef}>
        <button
          id={id}
          type="button"
          aria-label={label}
          aria-haspopup="listbox"
          aria-expanded={open}
          onClick={() => setOpen((prev) => !prev)}
          className={clsx(
            "peer flex h-14 w-full items-center justify-between rounded-lg pr-4 pl-4 text-left outline-hidden",
            "transition duration-200 ease-in-out",
            "bg-surface-base",
            { "border border-border-5": !open },
            { "border border-border-20 ring-4 ring-core-primary-5": open },
          )}
        >
          <div className="flex min-w-0 flex-col pt-[18px]">
            <span className="absolute top-[7px] text-200 text-text-primary-50">{label}</span>
            <span className={clsx("truncate text-300", picked ? "text-text-primary" : "text-text-primary-50")}>
              {displayLabel}
            </span>
          </div>
          <ChevronDown
            width="w-3"
            className={clsx("shrink-0 text-text-primary-70 transition-transform", {
              "rotate-180": open,
            })}
          />
        </button>
      </div>

      {open ? (
        <Popover
          position={popoverPosition}
          className="!w-auto !space-y-0 !rounded-xl border border-border-5 !bg-surface-elevated-base !p-0 !shadow-300 !backdrop-blur-none"
          closePopover={() => setOpen(false)}
          closeIgnoreSelectors={[`#${id}`]}
        >
          <div
            ref={listboxRef}
            className="max-h-[calc(100vh-2rem)] overflow-y-auto overscroll-contain p-1.5"
            role="listbox"
            aria-label={label}
            style={{ minWidth: triggerWidth, maxHeight: popoverMaxHeight }}
          >
            {options.length === 0 ? (
              <div className="rounded-xl p-3 text-text-primary-50">{emptyMessage}</div>
            ) : (
              options.map((option) => {
                const active = option.id === value;
                return (
                  <div
                    key={option.id}
                    role="option"
                    aria-selected={active}
                    className={clsx(
                      "flex cursor-pointer items-center gap-3 rounded-xl p-3 text-left select-none",
                      "transition-[background-color] duration-200 ease-in-out",
                      "text-text-primary hover:bg-core-primary-5",
                    )}
                    onClick={() => pickAndClose(option.id)}
                  >
                    <Radio selected={active} />
                    <div className="min-w-0 grow">
                      <div className="truncate text-emphasis-300">{option.label}</div>
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </Popover>
      ) : null}
    </div>
  );
};

const SinglePickerField = (props: SinglePickerFieldProps) => (
  <PopoverProvider>
    <SinglePickerFieldContent {...props} />
  </PopoverProvider>
);

export default SinglePickerField;
