import { type RefObject, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { type DropdownOption } from "./DropdownFilter";
import NestedSubmenu from "./NestedSubmenu";
import { POPOVER_VIEWPORT_PADDING, useFilterDropdownPosition } from "./useFilterDropdownPosition";
import { useNestedDropdownHoverState } from "./useNestedDropdownHoverState";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Popover, { PopoverProvider, usePopover } from "@/shared/components/Popover";
import { type Position, positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

// Height reserved for popover chrome (padding + footer button row) when sizing the
// scroll viewport so the panel fits inside the viewport edge.
const POPOVER_CHROME = 120;

export type FilterCategory = {
  key: string;
  label: string;
  options: DropdownOption[];
  selectedValues: string[];
};

type NestedDropdownFilterProps = {
  /** Trigger button label (e.g. "Filters", "More"). */
  label: string;
  categories: FilterCategory[];
  onChange: (key: string, selectedValues: string[]) => void;
  onClearAll: () => void;
  testId?: string;
};

type CategoryRowProps = {
  category: FilterCategory;
  onChange: (key: string, selectedValues: string[]) => void;
  parentPopoverRef: RefObject<HTMLDivElement | null>;
  isActive: boolean;
  onRowEnter: (key: string) => void;
  onRowLeave: () => void;
  onNestedEnter: () => void;
  onNestedLeave: () => void;
};

const CategoryRow = ({
  category,
  onChange,
  parentPopoverRef,
  isActive,
  onRowEnter,
  onRowLeave,
  onNestedEnter,
  onNestedLeave,
}: CategoryRowProps) => {
  const triggerRef = useRef<HTMLDivElement>(null);

  const isEmpty = category.options.length === 0;
  const selectedCount = category.selectedValues.length;
  const showNested = isActive && !isEmpty;

  const { position, nestedRef } = useFilterDropdownPosition({
    enabled: showNested,
    triggerRef,
    parentRef: parentPopoverRef,
  });

  const handleToggleItem = useCallback(
    (itemId: string) => {
      const next = category.selectedValues.includes(itemId)
        ? category.selectedValues.filter((id) => id !== itemId)
        : [...category.selectedValues, itemId];
      onChange(category.key, next);
    },
    [category.key, category.selectedValues, onChange],
  );

  return (
    <div
      ref={triggerRef}
      className="relative"
      onMouseEnter={() => {
        if (!isEmpty) onRowEnter(category.key);
      }}
      onMouseLeave={onRowLeave}
    >
      <button
        type="button"
        className={clsx(
          "flex w-full items-center gap-2 rounded-xl p-3 text-left select-none",
          "transition-[background-color] duration-200 ease-in-out",
          "text-text-primary hover:bg-core-primary-5 disabled:cursor-not-allowed disabled:opacity-50",
          { "bg-core-primary-5": showNested },
        )}
        onClick={() => {
          if (!isEmpty) onRowEnter(category.key);
        }}
        disabled={isEmpty}
        aria-haspopup="dialog"
        aria-expanded={showNested}
        data-testid={`nested-dropdown-filter-row-${category.key}`}
      >
        <span className="truncate text-emphasis-300">{category.label}</span>
        {!isEmpty && selectedCount > 0 ? (
          <span
            className={clsx(
              "relative inline-flex h-5 w-5 shrink-0 items-center justify-center text-200 text-intent-warning-fill",
              "before:absolute before:inset-0 before:-z-10 before:rounded-full before:bg-intent-warning-10 before:content-['']",
            )}
          >
            {selectedCount}
          </span>
        ) : null}
        <span className="grow" />
        {isEmpty ? <span className="text-300 text-text-primary-70">(no values)</span> : null}
        {!isEmpty ? <ChevronDown width="w-3" className="-rotate-90 opacity-60" /> : null}
      </button>

      {showNested ? (
        <NestedSubmenu
          categoryKey={category.key}
          options={category.options}
          selectedValues={category.selectedValues}
          onToggleItem={handleToggleItem}
          onMouseEnter={onNestedEnter}
          onMouseLeave={onNestedLeave}
          position={position}
          panelRef={nestedRef}
        />
      ) : null}
    </div>
  );
};

const NestedDropdownFilterContent = ({
  label,
  categories,
  onChange,
  onClearAll,
  testId,
}: NestedDropdownFilterProps) => {
  const [showPopover, setShowPopover] = useState(false);
  const { triggerRef } = usePopover();
  const parentPopoverRef = useRef<HTMLDivElement | null>(null);
  const { height: windowHeight } = useWindowDimensions();
  const [popoverPosition, setPopoverPosition] = useState<Position>(positions["bottom right"]);
  const [optionsMaxHeight, setOptionsMaxHeight] = useState<number | undefined>();

  const closeOuterPopover = useCallback(() => setShowPopover(false), []);
  const { activeRowKey, handleRowEnter, scheduleClose, cancelClose, closeAll } =
    useNestedDropdownHoverState(closeOuterPopover);

  useEffect(() => {
    if (!showPopover || !triggerRef.current) {
      return;
    }

    const updateLayout = () => {
      if (!triggerRef.current) return;
      const triggerRect = triggerRef.current.getBoundingClientRect();
      const viewportHeight = window.visualViewport?.height ?? windowHeight;
      const spaceAbove = triggerRect.top - POPOVER_VIEWPORT_PADDING;
      const spaceBelow = viewportHeight - triggerRect.bottom - POPOVER_VIEWPORT_PADDING;
      const shouldOpenAbove = spaceAbove > spaceBelow;
      const available = (shouldOpenAbove ? spaceAbove : spaceBelow) - POPOVER_CHROME;

      setPopoverPosition(shouldOpenAbove ? positions["top right"] : positions["bottom right"]);
      setOptionsMaxHeight(Math.max(available, 0));
    };

    updateLayout();
    window.visualViewport?.addEventListener("resize", updateLayout);
    return () => {
      window.visualViewport?.removeEventListener("resize", updateLayout);
    };
  }, [showPopover, triggerRef, windowHeight]);

  useClickOutside({
    ref: triggerRef,
    onClickOutside: closeAll,
    ignoreSelectors: [".popover-content"],
  });

  const activeCount = categories.reduce((acc, c) => acc + c.selectedValues.length, 0);

  return (
    <div ref={triggerRef} className="relative z-10">
      <Button
        variant={showPopover ? variants.secondary : variants.ghost}
        size={sizes.compact}
        textColor="text-text-primary"
        className="overflow-hidden !px-3"
        onClick={() => setShowPopover((prev) => !prev)}
        testId={testId ?? "nested-dropdown-filter"}
        suffixIcon={
          <div
            className={clsx("opacity-60 transition-transform duration-200", {
              "rotate-180": showPopover,
            })}
          >
            <ChevronDown width="w-3" />
          </div>
        }
      >
        <span>{label}</span>
      </Button>

      {showPopover ? (
        <Popover
          testId="nested-dropdown-filter-popover"
          position={popoverPosition}
          offset={8}
          buttons={
            activeCount > 0
              ? [
                  {
                    text: "Clear all",
                    variant: variants.secondary,
                    onClick: () => {
                      onClearAll();
                      closeAll();
                    },
                  },
                ]
              : undefined
          }
        >
          <div
            ref={(node) => {
              // The outer popover surface (with padding/shadow) is the `.popover-content` ancestor.
              // Anchor the side-rendered nested panel to its right edge, not the inner scroll area.
              // React 19 cycles ref callbacks (node → null → node) on each render — only update
              // on non-null nodes so transient nulls don't leave the ref stale during a re-render.
              if (node) {
                parentPopoverRef.current = (node.closest(".popover-content") as HTMLDivElement) ?? null;
              }
            }}
            className="space-y-0 overflow-y-auto overscroll-contain"
            style={{ maxHeight: optionsMaxHeight }}
          >
            {categories.map((category, index) => (
              <div key={category.key}>
                <CategoryRow
                  category={category}
                  onChange={onChange}
                  parentPopoverRef={parentPopoverRef}
                  isActive={activeRowKey === category.key}
                  onRowEnter={handleRowEnter}
                  onRowLeave={scheduleClose}
                  onNestedEnter={cancelClose}
                  onNestedLeave={scheduleClose}
                />
                {index < categories.length - 1 ? <Divider className="px-0" /> : null}
              </div>
            ))}
          </div>
        </Popover>
      ) : null}
    </div>
  );
};

const NestedDropdownFilter = (props: NestedDropdownFilterProps) => {
  return (
    <PopoverProvider>
      <NestedDropdownFilterContent {...props} />
    </PopoverProvider>
  );
};

export default NestedDropdownFilter;
