import { RefObject } from "react";
import clsx from "clsx";

import { DropdownOption } from "./DropdownFilter";
import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Checkbox from "@/shared/components/Checkbox";
import Divider from "@/shared/components/Divider";
import Popover from "@/shared/components/Popover";

type DropdownFilterPopoverProps = {
  options: DropdownOption[];
  displaySelectedItems: string[];
  allSelected: boolean;
  partiallySelected: boolean;
  handleSelectAll: () => void;
  handleToggleItem: (itemId: string) => void;
  withButtons: boolean;
  handleReset: () => void;
  handleApply: () => void;
  popoverRef: RefObject<HTMLDivElement>;
};

const DropdownFilterPopover = ({
  options,
  displaySelectedItems,
  allSelected,
  partiallySelected,
  handleSelectAll,
  handleToggleItem,
  withButtons,
  handleReset,
  handleApply,
  popoverRef,
}: DropdownFilterPopoverProps) => {
  return (
    <Popover
      position="bottom right"
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
                variant: variants.accent,
                onClick: handleApply,
              },
            ]
          : undefined
      }
    >
      <div ref={popoverRef} className="space-y-0">
        <div
          className={clsx(
            "flex cursor-pointer items-center rounded-xl p-3 text-left select-none",
            "transition-[background-color] duration-200 ease-in-out",
            "text-text-primary hover:bg-core-primary-5",
          )}
          onClick={handleSelectAll}
        >
          <div className="grow text-emphasis-300">Select all</div>
          <Checkbox
            checked={allSelected}
            partiallyChecked={partiallySelected}
          />
        </div>
        <Divider className="px-0" />

        {options.map((item, index) => (
          <div key={item.id}>
            <div
              className={clsx(
                "flex cursor-pointer items-center rounded-xl p-3 text-left select-none",
                "transition-[background-color] duration-200 ease-in-out",
                "text-text-primary hover:bg-core-primary-5",
              )}
              onClick={() => handleToggleItem(item.id)}
            >
              <div className="grow text-emphasis-300">{item.label}</div>
              <Checkbox checked={displaySelectedItems.includes(item.id)} />
            </div>
            {index < options.length - 1 && <Divider className="px-0" />}
          </div>
        ))}
      </div>
    </Popover>
  );
};

export default DropdownFilterPopover;
