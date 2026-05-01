import { type ReactNode, type RefObject, useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { type DropdownOption } from "./DropdownFilter";
import NestedSubmenu, { CheckboxOptionRow } from "./NestedSubmenu";
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
  /** Trigger button label (e.g. "Filters", "More", "Add Filter"). */
  label: string;
  categories: FilterCategory[];
  onChange: (key: string, selectedValues: string[]) => void;
  onClearAll: () => void;
  testId?: string;
  /** Optional icon rendered before the label. Suppresses the chevron suffix when set. */
  prefixIcon?: ReactNode;
};

type CategoryRowButtonProps = {
  category: FilterCategory;
  onClick: () => void;
  isActive?: boolean;
};

const CategoryRowButton = ({ category, onClick, isActive = false }: CategoryRowButtonProps) => {
  const isEmpty = category.options.length === 0;
  const selectedCount = category.selectedValues.length;
  return (
    <button
      type="button"
      className={clsx(
        "flex w-full items-center gap-2 rounded-xl p-3 text-left select-none",
        "transition-[background-color] duration-200 ease-in-out",
        "text-text-primary hover:bg-core-primary-5 disabled:cursor-not-allowed disabled:opacity-50",
        { "bg-core-primary-5": isActive },
      )}
      onClick={onClick}
      disabled={isEmpty}
      aria-haspopup="dialog"
      aria-expanded={isActive}
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
  );
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
      <CategoryRowButton
        category={category}
        isActive={showNested}
        onClick={() => {
          if (!isEmpty) onRowEnter(category.key);
        }}
      />

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

type MobileCategoryListProps = {
  categories: FilterCategory[];
  onSelect: (key: string) => void;
};

const MobileCategoryList = ({ categories, onSelect }: MobileCategoryListProps) => (
  <>
    {categories.map((category, index) => (
      <div key={category.key}>
        <CategoryRowButton
          category={category}
          onClick={() => {
            if (category.options.length > 0) onSelect(category.key);
          }}
        />
        {index < categories.length - 1 ? <Divider className="px-0" /> : null}
      </div>
    ))}
  </>
);

type MobileOptionListProps = {
  category: FilterCategory;
  onBack: () => void;
  onToggleOption: (categoryKey: string, optionId: string) => void;
};

const MobileOptionList = ({ category, onBack, onToggleOption }: MobileOptionListProps) => (
  <>
    <button
      type="button"
      onClick={onBack}
      className={clsx(
        "flex w-full items-center gap-2 rounded-xl p-3 text-left select-none",
        "transition-[background-color] duration-200 ease-in-out",
        "text-text-primary hover:bg-core-primary-5",
      )}
      data-testid="nested-dropdown-filter-back"
    >
      <ChevronDown width="w-3" className="rotate-90 opacity-60" />
      <span className="truncate text-emphasis-300">{category.label}</span>
    </button>
    <Divider className="px-0" />
    {category.options.map((option, index) => (
      <div key={option.id}>
        <CheckboxOptionRow
          option={option}
          checked={category.selectedValues.includes(option.id)}
          onToggle={(id) => onToggleOption(category.key, id)}
        />
        {index < category.options.length - 1 ? <Divider className="px-0" /> : null}
      </div>
    ))}
  </>
);

const NestedDropdownFilterContent = ({
  label,
  categories,
  onChange,
  onClearAll,
  testId,
  prefixIcon,
}: NestedDropdownFilterProps) => {
  const [showPopover, setShowPopover] = useState(false);
  const { triggerRef } = usePopover();
  const parentPopoverRef = useRef<HTMLDivElement | null>(null);
  const { height: windowHeight, isPhone, isTablet } = useWindowDimensions();
  // Phone/tablet lack horizontal room for parent + side panel; the nested layout
  // collapses into a drilldown that swaps the parent content instead.
  const isMobile = isPhone || isTablet;
  const [popoverPosition, setPopoverPosition] = useState<Position>(positions["bottom right"]);
  const [optionsMaxHeight, setOptionsMaxHeight] = useState<number | undefined>();
  const [mobileSelectedKey, setMobileSelectedKey] = useState<string | null>(null);

  const closeOuterPopover = useCallback(() => {
    setShowPopover(false);
    setMobileSelectedKey(null);
  }, []);
  const { activeRowKey, handleRowEnter, scheduleClose, cancelClose, closeAll } =
    useNestedDropdownHoverState(closeOuterPopover);

  const handleMobileToggleOption = useCallback(
    (categoryKey: string, optionId: string) => {
      const category = categories.find((c) => c.key === categoryKey);
      if (!category) return;
      const next = category.selectedValues.includes(optionId)
        ? category.selectedValues.filter((id) => id !== optionId)
        : [...category.selectedValues, optionId];
      onChange(categoryKey, next);
    },
    [categories, onChange],
  );

  const mobileSelectedCategory = useMemo(
    () => (isMobile ? (categories.find((c) => c.key === mobileSelectedKey) ?? null) : null),
    [isMobile, categories, mobileSelectedKey],
  );

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
        onClick={() => {
          // Reset drilldown so reopening the popover starts at the category list.
          setMobileSelectedKey(null);
          setShowPopover((prev) => !prev);
        }}
        testId={testId ?? "nested-dropdown-filter"}
        prefixIcon={prefixIcon}
        suffixIcon={
          prefixIcon ? null : (
            <div
              className={clsx("opacity-60 transition-transform duration-200", {
                "rotate-180": showPopover,
              })}
            >
              <ChevronDown width="w-3" />
            </div>
          )
        }
      >
        <span>{label}</span>
      </Button>

      {showPopover ? (
        <Popover
          testId="nested-dropdown-filter-popover"
          position={popoverPosition}
          offset={8}
          freezePosition
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
              // React 19 cycles ref callbacks (node → null → node) on each render — only update on
              // non-null nodes so transient nulls don't leave the parent surface ref stale.
              if (node) {
                parentPopoverRef.current = (node.closest(".popover-content") as HTMLDivElement) ?? null;
              }
            }}
            className="space-y-0 overflow-y-auto overscroll-contain"
            style={{ maxHeight: optionsMaxHeight }}
          >
            {isMobile && mobileSelectedCategory ? (
              <MobileOptionList
                category={mobileSelectedCategory}
                onBack={() => setMobileSelectedKey(null)}
                onToggleOption={handleMobileToggleOption}
              />
            ) : isMobile ? (
              <MobileCategoryList categories={categories} onSelect={setMobileSelectedKey} />
            ) : (
              categories.map((category, index) => (
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
              ))
            )}
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
