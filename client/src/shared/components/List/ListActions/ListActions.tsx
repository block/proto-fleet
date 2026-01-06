import { useState } from "react";
import clsx from "clsx";
import { Ellipsis } from "@/shared/assets/icons";
import { ListAction } from "@/shared/components/List/types";
import Popover, { popoverSizes, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";

interface ListActionProps<ListItem> {
  item: ListItem;
  actions: ListAction<ListItem>[];
  disabled?: boolean;
}

const ListActions = <ListItem,>({ item, actions, disabled = false }: ListActionProps<ListItem>) => {
  const { triggerRef } = usePopover();

  const [actionsVisible, setActionsVisible] = useState<boolean>(false);

  if (!actions || actions.length === 0) {
    return null;
  }

  return (
    <div className="relative" ref={triggerRef}>
      <button
        className={clsx("align-middle", {
          "text-text-primary-30 hover:cursor-pointer hover:text-text-primary-50": !disabled,
          "cursor-not-allowed text-text-primary-30": disabled,
        })}
        data-testid="list-actions-trigger"
        onClick={() => !disabled && setActionsVisible(true)}
        disabled={disabled}
      >
        <Ellipsis />
      </button>
      {actionsVisible && !disabled && (
        <Popover
          className="!space-y-0 px-4 pt-2 pb-1"
          position={positions["bottom left"]}
          size={popoverSizes.small}
          closePopover={() => setActionsVisible(false)}
        >
          {actions.map((action, index) => (
            <Row
              key={action.title}
              className={clsx("text-emphasis-300", action.variant === "destructive" && "text-intent-critical-text")}
              prefixIcon={action.icon}
              onClick={() => {
                action.actionHandler(item);
                setActionsVisible(false);
              }}
              compact
              divider={index < actions.length - 1}
            >
              {action.title}
            </Row>
          ))}
        </Popover>
      )}
    </div>
  );
};

export default ListActions;
