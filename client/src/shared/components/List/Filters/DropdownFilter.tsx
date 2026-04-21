import { RefObject, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import DropdownFilterPopover from "./DropdownFilterPopover";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import { minimalMargin } from "@/shared/components/Popover/constants";
import { type Position, positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const popoverViewportPadding = minimalMargin * 2;
const POPOVER_CHROME_WITH_BUTTONS = 120;
const POPOVER_CHROME_BASE = 56;

export type DropdownOption = {
  id: string;
  label: string;
};

type DropdownFilterProps = {
  title: string;
  pluralTitle?: string;
  options: DropdownOption[];
  selectedOptions: string[];
  onSelect: (selectedItems: string[]) => void;
  withButtons?: boolean;
  showSelectAll?: boolean;
  className?: string;
  testId?: string;
};

const FilterContent = ({
  title,
  options,
  selectedOptions: externalSelectedItems,
  onSelect,
  withButtons = false,
  showSelectAll = true,
  className,
}: DropdownFilterProps) => {
  const [showPopover, setShowPopover] = useState(false);
  const { triggerRef } = usePopover();
  const { height: windowHeight } = useWindowDimensions();
  const popoverRef = useRef<HTMLDivElement>(null) as RefObject<HTMLDivElement>;
  const [optionsMaxHeight, setOptionsMaxHeight] = useState<number | undefined>();
  const [popoverPosition, setPopoverPosition] = useState<Position>(positions["bottom right"]);

  // Only use internal state when buttons are shown
  const [internalSelectedItems, setInternalSelectedItems] = useState<string[]>(externalSelectedItems);

  // When buttons are shown, update internal state when external changes
  useEffect(() => {
    if (withButtons) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setInternalSelectedItems(externalSelectedItems);
    }
  }, [externalSelectedItems, withButtons]);

  useEffect(() => {
    if (!showPopover || !triggerRef.current) {
      return;
    }

    const chromeHeight = withButtons ? POPOVER_CHROME_WITH_BUTTONS : POPOVER_CHROME_BASE;

    const updatePopoverLayout = () => {
      if (!triggerRef.current) return;
      const triggerRect = triggerRef.current.getBoundingClientRect();
      const viewportHeight = window.visualViewport?.height ?? windowHeight;
      const spaceAbove = triggerRect.top - popoverViewportPadding;
      const spaceBelow = viewportHeight - triggerRect.bottom - popoverViewportPadding;
      const shouldOpenAbove = spaceAbove > spaceBelow;
      const available = (shouldOpenAbove ? spaceAbove : spaceBelow) - chromeHeight;

      setPopoverPosition(shouldOpenAbove ? positions["top right"] : positions["bottom right"]);
      setOptionsMaxHeight(Math.max(available, 0));
    };

    updatePopoverLayout();

    window.visualViewport?.addEventListener("resize", updatePopoverLayout);

    return () => {
      window.visualViewport?.removeEventListener("resize", updatePopoverLayout);
    };
  }, [showPopover, triggerRef, withButtons, windowHeight]);

  useClickOutside({
    ref: triggerRef,
    onClickOutside: () => setShowPopover(false),
    ignoreSelectors: [".popover-content"],
  });

  const handleToggleItem = useCallback(
    (itemId: string) => {
      if (withButtons) {
        // With buttons - update internal state
        setInternalSelectedItems((prev) => {
          if (prev.includes(itemId)) {
            return prev.filter((id) => id !== itemId);
          }
          return [...prev, itemId];
        });
      } else {
        // Without buttons - toggle and call callback immediately
        const newSelection = externalSelectedItems.includes(itemId)
          ? externalSelectedItems.filter((id) => id !== itemId)
          : [...externalSelectedItems, itemId];
        onSelect(newSelection);
      }
    },
    [withButtons, onSelect, externalSelectedItems],
  );

  const handleSelectAll = useCallback(() => {
    if (withButtons) {
      // With buttons - update internal state
      if (internalSelectedItems.length === options.length) {
        setInternalSelectedItems([]);
      } else {
        setInternalSelectedItems(options.map((item) => item.id));
      }
    } else {
      // Without buttons - call callback immediately
      const shouldSelectAll = externalSelectedItems.length !== options.length;
      const newSelection = shouldSelectAll ? options.map((o) => o.id) : [];
      onSelect(newSelection);
    }
  }, [withButtons, externalSelectedItems, options, internalSelectedItems, onSelect]);

  const handleApply = useCallback(() => {
    onSelect(internalSelectedItems);
    setShowPopover(false);
  }, [internalSelectedItems, onSelect]);

  const handleReset = useCallback(() => {
    setInternalSelectedItems([]);
    onSelect([]);
  }, [onSelect]);

  // Use appropriate selected items based on whether buttons are shown
  const displaySelectedItems = withButtons ? internalSelectedItems : externalSelectedItems;

  const allSelected = displaySelectedItems.length === options.length;
  const partiallySelected = displaySelectedItems.length > 0 && displaySelectedItems.length < options.length;

  return (
    <div className={clsx("flex flex-col gap-2", className)}>
      <div ref={triggerRef} className="relative z-10">
        <Button
          variant={showPopover ? variants.secondary : variants.ghost}
          size={sizes.compact}
          textColor="text-text-primary"
          className="overflow-hidden !px-3"
          onClick={() => setShowPopover((prev) => !prev)}
          testId={`filter-dropdown-${title}`}
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
          {title}
        </Button>

        {showPopover && (
          <DropdownFilterPopover
            options={options}
            displaySelectedItems={displaySelectedItems}
            allSelected={allSelected}
            partiallySelected={partiallySelected}
            handleSelectAll={handleSelectAll}
            handleToggleItem={handleToggleItem}
            withButtons={withButtons}
            showSelectAll={showSelectAll}
            handleReset={handleReset}
            handleApply={handleApply}
            popoverRef={popoverRef}
            optionsMaxHeight={optionsMaxHeight}
            position={popoverPosition}
          />
        )}
      </div>
    </div>
  );
};

const DropdownFilter = (props: DropdownFilterProps) => {
  return (
    <PopoverProvider>
      <FilterContent {...props} />
    </PopoverProvider>
  );
};

export default DropdownFilter;
