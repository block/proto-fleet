import { type RefObject } from "react";
import clsx from "clsx";
import { createPortal } from "react-dom";

import { type DropdownOption } from "./DropdownFilter";
import { NESTED_POPOVER_WIDTH, type NestedPopoverPosition } from "./useFilterDropdownPosition";
import Checkbox from "@/shared/components/Checkbox";
// Outer panel uses `pt-2 pb-1` (12px total); subtract from the inner scroll
// cap when the outer is height-clipped so content doesn't bleed past chrome.
const PANEL_PADDING_TOTAL = 12;

type CheckboxOptionRowProps = {
  option: DropdownOption;
  checked: boolean;
  onToggle: (id: string) => void;
};

export const CheckboxOptionRow = ({ option, checked, onToggle }: CheckboxOptionRowProps) => (
  <div
    className={clsx(
      "flex cursor-pointer items-center rounded-lg px-3 py-2 text-left select-none",
      "transition-[background-color] duration-200 ease-in-out",
      "text-text-primary hover:bg-core-primary-5",
    )}
    onClick={() => onToggle(option.id)}
    data-testid={`filter-option-${option.id}`}
  >
    <div className="min-w-0 grow truncate text-emphasis-300" title={option.label}>
      {option.label}
    </div>
    <Checkbox className="shrink-0" checked={checked} />
  </div>
);

type NestedSubmenuProps = {
  /** Used in test ids and as a stable key. */
  categoryKey: string;
  options: DropdownOption[];
  selectedValues: string[];
  onToggleItem: (itemId: string) => void;
  onMouseEnter: () => void;
  onMouseLeave: () => void;
  /** Position from `useFilterDropdownPosition`. `null` while measurement is pending. */
  position: NestedPopoverPosition | null;
  /** Attach to the panel root so the position hook can measure its natural height. */
  panelRef: RefObject<HTMLDivElement | null>;
};

const NestedSubmenu = ({
  categoryKey,
  options,
  selectedValues,
  onToggleItem,
  onMouseEnter,
  onMouseLeave,
  position,
  panelRef,
}: NestedSubmenuProps) => {
  return createPortal(
    <div
      ref={panelRef}
      className="popover-content fixed z-50 space-y-0 rounded-2xl bg-surface-elevated-base/85 px-2 pt-2 pb-1 shadow-200 backdrop-blur-[7px]"
      style={{
        top: `${position?.top ?? 0}px`,
        left: `${position?.left ?? 0}px`,
        width: `${NESTED_POPOVER_WIDTH}px`,
        // Hide on first render until measurement completes so the user never sees the
        // panel pop in at an unmeasured location.
        visibility: position ? "visible" : "hidden",
        ...(position?.maxHeight !== undefined ? { maxHeight: `${position.maxHeight}px` } : {}),
      }}
      data-testid={`nested-dropdown-filter-submenu-${categoryKey}`}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
    >
      <div
        className="space-y-0 overflow-y-auto overscroll-contain"
        // Inner scroll caps to (outer max minus padding) only when the outer is actually
        // clipped; otherwise let the inner size to its content.
        style={
          position?.maxHeight !== undefined ? { maxHeight: `${position.maxHeight - PANEL_PADDING_TOTAL}px` } : undefined
        }
      >
        {options.map((option) => (
          <CheckboxOptionRow
            key={option.id}
            option={option}
            checked={selectedValues.includes(option.id)}
            onToggle={onToggleItem}
          />
        ))}
      </div>
    </div>,
    document.body,
  );
};

export default NestedSubmenu;
