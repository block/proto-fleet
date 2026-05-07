import { RefObject } from "react";
import clsx from "clsx";

import { DropdownOption } from "./DropdownFilter";
import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Checkbox from "@/shared/components/Checkbox";
import Divider from "@/shared/components/Divider";
import Popover, { popoverSizes } from "@/shared/components/Popover";
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
      size={popoverSizes.small}
      className="!space-y-0 !rounded-2xl px-2 pt-2 pb-1"
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
                "flex cursor-pointer items-center rounded-lg px-3 py-2 text-left select-none",
                "transition-[background-color] duration-200 ease-in-out",
                "text-text-primary hover:bg-core-primary-5",
              )}
              onClick={handleSelectAll}
            >
              <div className="grow text-emphasis-300">Select all</div>
              <Checkbox className="shrink-0" checked={allSelected} partiallyChecked={partiallySelected} />
            </div>
            <Divider className="my-1 px-0" dividerStyle="thick" />
          </>
        ) : null}

        {options.map((item) => (
          <div
            key={item.id}
            className={clsx(
              "flex cursor-pointer items-center rounded-lg px-3 py-2 text-left select-none",
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
        ))}
      </div>
    </Popover>
  );
};

export default DropdownFilterPopover;
