import { useCallback, useEffect, useState } from "react";

import type { DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import { Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Popover, { popoverSizes } from "@/shared/components/Popover";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

type GroupNameCellProps = {
  group: DeviceCollection;
  onEdit: (group: DeviceCollection) => void;
};

const GroupNameCell = ({ group, onEdit }: GroupNameCellProps) => {
  return (
    <PopoverProvider>
      <GroupNameCellInner group={group} onEdit={onEdit} />
    </PopoverProvider>
  );
};

const GroupNameCellInner = ({ group, onEdit }: GroupNameCellProps) => {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();

  useEffect(() => {
    setPopoverRenderMode("portal-fixed");
  }, [setPopoverRenderMode]);

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  return (
    <div className="grid w-full grid-cols-[1fr_auto] items-center gap-3">
      <button
        type="button"
        className="min-w-0 cursor-pointer truncate text-left"
        title={group.label}
        onClick={() => onEdit(group)}
      >
        {group.label}
      </button>
      <div ref={triggerRef} className="relative">
        <Button
          size={sizes.compact}
          variant={variants.textOnly}
          ariaLabel="Group actions"
          prefixIcon={<Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
          onClick={(e) => {
            e.stopPropagation();
            setIsOpen((prev) => !prev);
          }}
        />
        {isOpen && (
          <Popover
            className="!space-y-0 !rounded-2xl px-0 pt-2 pb-1"
            position={positions["bottom right"]}
            size={popoverSizes.small}
            offset={8}
          >
            <div className="px-4">
              <Row
                className="text-emphasis-300"
                onClick={() => {
                  setIsOpen(false);
                  onEdit(group);
                }}
                compact
                divider={false}
              >
                Edit group
              </Row>
            </div>
          </Popover>
        )}
      </div>
    </div>
  );
};

export default GroupNameCell;
