import { RefObject, useCallback, useEffect, useRef, useState } from "react";

import { type DropdownOption } from "./DropdownFilter";
import DropdownFilterPopover from "./DropdownFilterPopover";
import { DismissTiny } from "@/shared/assets/icons";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import { minimalMargin } from "@/shared/components/Popover/constants";
import { type Position, positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const popoverViewportPadding = minimalMargin * 2;
const POPOVER_CHROME_BASE = 56;

export type FilterChipProps = {
  filterValue: string;
  title: string;
  pluralTitle?: string;
  options: DropdownOption[];
  selectedIds: string[];
  onChange: (selectedIds: string[]) => void;
  onClear: () => void;
  onOpenChange?: (open: boolean) => void;
};

const FilterChipContent = ({
  filterValue,
  title,
  pluralTitle,
  options,
  selectedIds,
  onChange,
  onClear,
  onOpenChange,
}: FilterChipProps) => {
  const [showPopover, setShowPopoverState] = useState(false);
  const { triggerRef } = usePopover();
  const setShowPopover = useCallback(
    (next: boolean | ((prev: boolean) => boolean)) => {
      setShowPopoverState((prev) => {
        const value = typeof next === "function" ? next(prev) : next;
        if (value !== prev) onOpenChange?.(value);
        return value;
      });
    },
    [onOpenChange],
  );
  const { height: windowHeight } = useWindowDimensions();
  const popoverRef = useRef<HTMLDivElement>(null) as RefObject<HTMLDivElement>;
  const [optionsMaxHeight, setOptionsMaxHeight] = useState<number | undefined>();
  const [popoverPosition, setPopoverPosition] = useState<Position>(positions["bottom right"]);

  useEffect(() => {
    if (!showPopover || !triggerRef.current) return;

    const updatePopoverLayout = () => {
      if (!triggerRef.current) return;
      const rect = triggerRef.current.getBoundingClientRect();
      const viewportHeight = window.visualViewport?.height ?? windowHeight;
      const spaceAbove = rect.top - popoverViewportPadding;
      const spaceBelow = viewportHeight - rect.bottom - popoverViewportPadding;
      const shouldOpenAbove = spaceAbove > spaceBelow;
      const available = (shouldOpenAbove ? spaceAbove : spaceBelow) - POPOVER_CHROME_BASE;
      setPopoverPosition(shouldOpenAbove ? positions["top right"] : positions["bottom right"]);
      setOptionsMaxHeight(Math.max(available, 0));
    };

    updatePopoverLayout();
    window.visualViewport?.addEventListener("resize", updatePopoverLayout);
    return () => {
      window.visualViewport?.removeEventListener("resize", updatePopoverLayout);
    };
  }, [showPopover, triggerRef, windowHeight]);

  useClickOutside({
    ref: triggerRef,
    onClickOutside: () => setShowPopover(false),
    ignoreSelectors: [".popover-content"],
  });

  const handleToggleItem = useCallback(
    (itemId: string) => {
      const next = selectedIds.includes(itemId) ? selectedIds.filter((id) => id !== itemId) : [...selectedIds, itemId];
      onChange(next);
    },
    [selectedIds, onChange],
  );

  const handleSelectAll = useCallback(() => {
    onChange(selectedIds.length === options.length ? [] : options.map((option) => option.id));
  }, [selectedIds, options, onChange]);

  const allSelected = selectedIds.length > 0 && selectedIds.length === options.length;
  const partiallySelected = selectedIds.length > 0 && selectedIds.length < options.length;

  const plural = pluralTitle ?? `${title}s`;
  let summary: string;
  if (selectedIds.length === 1) {
    const match = options.find((option) => option.id === selectedIds[0]);
    summary = match?.label ?? "";
  } else {
    summary = `${selectedIds.length} ${plural}`;
  }

  return (
    <div ref={triggerRef} className="relative inline-flex" data-testid={`active-filter-${filterValue}`}>
      <div className="inline-flex items-stretch overflow-hidden rounded-3xl text-emphasis-300">
        <span className="flex items-center bg-intent-warning-fill px-3 py-1 text-text-base-contrast-static">
          {title}
        </span>
        <button
          type="button"
          onClick={() => setShowPopover((prev) => !prev)}
          className="flex cursor-pointer items-center bg-surface-20 px-3 py-1 text-text-primary hover:opacity-80"
          data-testid={`active-filter-${filterValue}-edit`}
          aria-haspopup="dialog"
          aria-expanded={showPopover}
        >
          {summary}
        </button>
        <button
          type="button"
          onClick={onClear}
          className="flex cursor-pointer items-center bg-surface-20 py-1 pr-3 pl-1 text-text-primary hover:opacity-80"
          data-testid={`active-filter-${filterValue}-clear`}
          aria-label={`Clear ${title} filter`}
        >
          <DismissTiny />
        </button>
      </div>

      {showPopover ? (
        <DropdownFilterPopover
          options={options}
          displaySelectedItems={selectedIds}
          allSelected={allSelected}
          partiallySelected={partiallySelected}
          handleSelectAll={handleSelectAll}
          handleToggleItem={handleToggleItem}
          withButtons={false}
          showSelectAll={false}
          handleReset={() => onChange([])}
          handleApply={() => setShowPopover(false)}
          popoverRef={popoverRef}
          optionsMaxHeight={optionsMaxHeight}
          position={popoverPosition}
        />
      ) : null}
    </div>
  );
};

const FilterChip = (props: FilterChipProps) => (
  <PopoverProvider>
    <FilterChipContent {...props} />
  </PopoverProvider>
);

export default FilterChip;
