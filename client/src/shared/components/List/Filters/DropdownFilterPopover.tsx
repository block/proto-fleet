import { RefObject } from "react";
import clsx from "clsx";

import { DropdownOption } from "./DropdownFilter";
import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Checkbox from "@/shared/components/Checkbox";
import Divider from "@/shared/components/Divider";
import Popover from "@/shared/components/Popover";
import { type Position } from "@/shared/constants";

type DropdownFilterPopoverProps = {
  options: DropdownOption[];
  displaySelectedItems: string[];
  allSelected: boolean;
  partiallySelected: boolean;
  handleSelectAll: () => void;
  handleToggleItem: (itemId: string) => void;
  withButtons: boolean;
  showSelectAll: boolean;
  handleReset: () => void;
  handleApply: () => void;
  popoverRef: RefObject<HTMLDivElement>;
  optionsMaxHeight?: number;
  position?: Position;
};

const DropdownFilterPopover = ({
  options,
  displaySelectedItems,
  allSelected,
  partiallySelected,
  handleSelectAll,
  handleToggleItem,
  withButtons,
  showSelectAll,
  handleReset,
  handleApply,
  popoverRef,
  optionsMaxHeight,
  position = "bottom right",
}: DropdownFilterPopoverProps) => {
  return (
    <Popover
      testId="dropdown-filter-popover"
      position={position}
      offset={8}
      buttonGroupVariant={groupVariants.fill}
      buttons={
        withButtons
          ? [
              {
                text: "Reset",
                variant: variants.secondary,
                onClick: handleReset,
              },
              {
                text: "Apply",
                variant: variants.primary,
                onClick: handleApply,
              },
            ]
          : undefined
      }
    >
      <div
        ref={popoverRef}
        className="space-y-0 overflow-y-auto overscroll-contain"
        style={{ maxHeight: optionsMaxHeight }}
      >
        {showSelectAll ? (
          <>
            <div
              className={clsx(
                "flex cursor-pointer items-center rounded-xl p-3 text-left select-none",
                "transition-[background-color] duration-200 ease-in-out",
                "text-text-primary hover:bg-core-primary-5",
              )}
              onClick={handleSelectAll}
            >
              <div className="grow text-emphasis-300">Select all</div>
              <Checkbox className="shrink-0" checked={allSelected} partiallyChecked={partiallySelected} />
            </div>
            <Divider className="px-0" />
          </>
        ) : null}

        {options.map((item, index) => (
          <div key={item.id}>
            <div
              className={clsx(
                "flex cursor-pointer items-center rounded-xl p-3 text-left select-none",
                "transition-[background-color] duration-200 ease-in-out",
                "text-text-primary hover:bg-core-primary-5",
              )}
              onClick={() => handleToggleItem(item.id)}
              data-testid={`filter-option-${item.id}`}
            >
              <div className="min-w-0 grow truncate text-emphasis-300" title={item.label}>
                {item.label}
              </div>
              <Checkbox className="shrink-0" checked={displaySelectedItems.includes(item.id)} />
            </div>
            {index < options.length - 1 ? <Divider className="px-0" /> : null}
          </div>
        ))}
      </div>
    </Popover>
  );
};

export default DropdownFilterPopover;
