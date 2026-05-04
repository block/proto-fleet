import { type ReactNode, type RefObject, useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import NestedSubmenu, { CheckboxOptionRow } from "./NestedSubmenu";
import { type FilterCategory } from "./types";
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

export type { FilterCategory };

type NestedDropdownFilterProps = {
  /** Trigger button label (e.g. "Filters", "More", "Add Filter"). */
  label: string;
  categories: FilterCategory[];
  onCheckboxChange: (key: string, selectedValues: string[]) => void;
  /**
   * Click handler for non-checkbox kinds (numericRange, textareaList). Parent
   * is expected to launch a modal for that category. The popover closes
   * automatically when this fires so the modal isn't underneath it.
   */
  onRequestEdit: (key: string) => void;
  onClearAll: () => void;
  testId?: string;
  /** Optional icon rendered before the label. Suppresses the chevron suffix when set. */
  prefixIcon?: ReactNode;
};

const categorySelectedCount = (category: FilterCategory): number => {
  switch (category.kind) {
    case "checkbox":
      return category.selectedValues.length;
    case "numericRange":
      return category.value.min !== undefined || category.value.max !== undefined ? 1 : 0;
    case "textareaList":
      return category.value.length;
  }
};

const categoryIsEmpty = (category: FilterCategory): boolean =>
  category.kind === "checkbox" && category.options.length === 0;

type CategoryRowButtonProps = {
  category: FilterCategory;
  onClick: () => void;
  isActive?: boolean;
};

const CategoryRowButton = ({ category, onClick, isActive = false }: CategoryRowButtonProps) => {
  const isEmpty = categoryIsEmpty(category);
  const selectedCount = categorySelectedCount(category);
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
  onCheckboxChange: (key: string, selectedValues: string[]) => void;
  onRequestEdit: (key: string) => void;
  parentPopoverRef: RefObject<HTMLDivElement | null>;
  isActive: boolean;
  onRowEnter: (key: string) => void;
  onRowLeave: () => void;
  onNestedEnter: () => void;
  onNestedLeave: () => void;
};

const CategoryRow = ({
  category,
  onCheckboxChange,
  onRequestEdit,
  parentPopoverRef,
  isActive,
  onRowEnter,
  onRowLeave,
  onNestedEnter,
  onNestedLeave,
}: CategoryRowProps) => {
  const triggerRef = useRef<HTMLDivElement>(null);

  const isEmpty = categoryIsEmpty(category);
  // Only checkbox kinds open an inline submenu. Numeric and textareaList kinds
  // hand off to a parent-rendered modal on click so the form has a focused
  // surface (and isn't subject to the hover-driven popover state machine).
  const usesInlineSubmenu = category.kind === "checkbox";
  const showNested = isActive && !isEmpty && usesInlineSubmenu;

  const { position, nestedRef } = useFilterDropdownPosition({
    enabled: showNested,
    triggerRef,
    parentRef: parentPopoverRef,
  });

  const handleToggleItem = useCallback(
    (itemId: string) => {
      if (category.kind !== "checkbox") return;
      const next = category.selectedValues.includes(itemId)
        ? category.selectedValues.filter((id) => id !== itemId)
        : [...category.selectedValues, itemId];
      onCheckboxChange(category.key, next);
    },
    [category, onCheckboxChange],
  );

  const handleRowClick = () => {
    if (isEmpty) return;
    if (usesInlineSubmenu) {
      onRowEnter(category.key);
      return;
    }
    onRequestEdit(category.key);
  };

  return (
    <div
      ref={triggerRef}
      className="relative"
      onMouseEnter={() => {
        if (!isEmpty && usesInlineSubmenu) onRowEnter(category.key);
      }}
      onMouseLeave={usesInlineSubmenu ? onRowLeave : undefined}
    >
      <CategoryRowButton category={category} isActive={showNested} onClick={handleRowClick} />

      {showNested && category.kind === "checkbox" ? (
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
            if (!categoryIsEmpty(category)) onSelect(category.key);
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

const MobileOptionList = ({ category, onBack, onToggleOption }: MobileOptionListProps) => {
  // Drilldown only renders the option list for checkbox kinds. Numeric and
  // textareaList kinds short-circuit at MobileCategoryList by routing to the
  // modal via onRequestEdit, so this branch is a no-op for them.
  if (category.kind !== "checkbox") return null;
  return (
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
};

const NestedDropdownFilterContent = ({
  label,
  categories,
  onCheckboxChange,
  onRequestEdit,
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
      if (!category || category.kind !== "checkbox") return;
      const next = category.selectedValues.includes(optionId)
        ? category.selectedValues.filter((id) => id !== optionId)
        : [...category.selectedValues, optionId];
      onCheckboxChange(categoryKey, next);
    },
    [categories, onCheckboxChange],
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

  const activeCount = categories.reduce((acc, c) => acc + categorySelectedCount(c), 0);

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
              <MobileCategoryList
                categories={categories}
                onSelect={(key) => {
                  // Checkbox kinds drill down inside the popover; numeric/
                  // textareaList kinds dispatch to the parent-rendered modal.
                  const category = categories.find((c) => c.key === key);
                  if (!category) return;
                  if (category.kind === "checkbox") {
                    setMobileSelectedKey(key);
                  } else {
                    closeAll();
                    onRequestEdit(key);
                  }
                }}
              />
            ) : (
              categories.map((category, index) => (
                <div key={category.key}>
                  <CategoryRow
                    category={category}
                    onCheckboxChange={onCheckboxChange}
                    onRequestEdit={(key) => {
                      closeAll();
                      onRequestEdit(key);
                    }}
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
