import { useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { DayOfWeek } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import { ChevronDown } from "@/shared/assets/icons";
import Checkbox from "@/shared/components/Checkbox";
import Popover, { PopoverProvider, usePopover } from "@/shared/components/Popover";
import { minimalMargin } from "@/shared/components/Popover/constants";
import { type Position, positions } from "@/shared/constants";

const popoverViewportPadding = minimalMargin * 2;

const weekdayMenuOptions: Array<{ value: DayOfWeek; label: string; shortLabel: string }> = [
  { value: DayOfWeek.SUNDAY, label: "Sunday", shortLabel: "Sun" },
  { value: DayOfWeek.MONDAY, label: "Monday", shortLabel: "Mon" },
  { value: DayOfWeek.TUESDAY, label: "Tuesday", shortLabel: "Tue" },
  { value: DayOfWeek.WEDNESDAY, label: "Wednesday", shortLabel: "Wed" },
  { value: DayOfWeek.THURSDAY, label: "Thursday", shortLabel: "Thu" },
  { value: DayOfWeek.FRIDAY, label: "Friday", shortLabel: "Fri" },
  { value: DayOfWeek.SATURDAY, label: "Saturday", shortLabel: "Sat" },
];

const formatSelectedDays = (selectedDays: DayOfWeek[]) => {
  if (selectedDays.length === 0) {
    return "Select days";
  }

  if (selectedDays.length === weekdayMenuOptions.length) {
    return "Every day";
  }

  const labelByDay = new Map(weekdayMenuOptions.map((option) => [option.value, option.shortLabel]));

  return [...selectedDays]
    .sort((left, right) => left - right)
    .map((day) => labelByDay.get(day))
    .filter(Boolean)
    .join(", ");
};

interface WeekdaySelectProps {
  id: string;
  value: DayOfWeek[];
  onChange: (value: DayOfWeek[]) => void;
  error?: boolean | string;
}

const WeekdaySelectContent = ({ id, value, onChange, error }: WeekdaySelectProps) => {
  const [open, setOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const listboxRef = useRef<HTMLDivElement>(null);
  const [popoverPosition, setPopoverPosition] = useState<Position>(positions["bottom right"]);
  const [triggerWidth, setTriggerWidth] = useState<number | undefined>();
  const [popoverMaxHeight, setPopoverMaxHeight] = useState<number | undefined>();
  const selectedLabel = formatSelectedDays(value);
  const hasValue = value.length > 0;

  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

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

    listboxRef.current.scrollTop = 0;
  }, [open]);

  const toggleDay = (day: DayOfWeek) => {
    onChange(
      value.includes(day)
        ? value.filter((currentDay) => currentDay !== day)
        : [...value, day].sort((left, right) => left - right),
    );
  };

  return (
    <div className="relative">
      <div ref={triggerRef}>
        <button
          id={id}
          type="button"
          aria-label="Days"
          aria-haspopup="listbox"
          aria-expanded={open}
          onClick={() => setOpen((prev) => !prev)}
          className={clsx(
            "peer flex h-14 w-full items-center justify-between rounded-lg pr-4 pl-4 text-left outline-hidden",
            "transition duration-200 ease-in-out",
            "bg-surface-base",
            { "border border-intent-critical-50": error && !open },
            { "border border-border-5": !open && !error },
            { "border border-border-20 ring-4 ring-core-primary-5": open && !error },
            { "border border-intent-critical-50 ring-4 ring-intent-critical-20": open && error },
          )}
        >
          <div className="flex min-w-0 flex-col pt-[18px]">
            <span className="absolute top-[7px] text-200 text-text-primary-50">Days</span>
            <span className={clsx("truncate text-300", hasValue ? "text-text-primary" : "text-text-primary-50")}>
              {selectedLabel}
            </span>
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
          closeIgnoreSelectors={[`#${id}`]}
        >
          <div
            ref={listboxRef}
            className="max-h-[calc(100vh-2rem)] overflow-y-auto overscroll-contain p-1.5"
            role="listbox"
            aria-label="Days options"
            style={{
              minWidth: triggerWidth,
              maxHeight: popoverMaxHeight,
            }}
          >
            {weekdayMenuOptions.map((option) => {
              const checked = value.includes(option.value);

              return (
                <div
                  key={option.value}
                  role="option"
                  aria-selected={checked}
                  className="flex cursor-pointer items-center gap-3 rounded-xl p-3 text-left text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
                  onClick={() => toggleDay(option.value)}
                >
                  <Checkbox checked={checked} />
                  <span className="text-emphasis-300">{option.label}</span>
                </div>
              );
            })}
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

const WeekdaySelect = (props: WeekdaySelectProps) => (
  <PopoverProvider>
    <WeekdaySelectContent {...props} />
  </PopoverProvider>
);

export default WeekdaySelect;
