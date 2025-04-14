import { useState } from "react";
import { Ellipsis } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import ButtonGroup, {
  groupVariants,
  sizes,
} from "@/shared/components/ButtonGroup";
import { ListAction } from "@/shared/components/List/types";
import Popover, { popoverSizes, usePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

interface ListActionProps<ListItem> {
  item: ListItem;
  actions: ListAction<ListItem>[];
}

const ListActions = <ListItem,>({
  item,
  actions,
}: ListActionProps<ListItem>) => {
  const { triggerRef } = usePopover();

  const [actionsVisible, setActionsVisible] = useState<boolean>(false);

  if (!actions || actions.length === 0) {
    return null;
  }

  return (
    <div className="relative" ref={triggerRef}>
      <button
        className="align-middle text-text-primary-30 hover:cursor-pointer hover:text-text-primary-50"
        data-testid="list-actions-trigger"
        onClick={() => setActionsVisible(true)}
      >
        <Ellipsis />
      </button>
      {actionsVisible && (
        <Popover
          className="p-6"
          position={positions["bottom left"]}
          size={popoverSizes.small}
          closePopover={() => setActionsVisible(false)}
        >
          <div>
            <ButtonGroup
              size={sizes.base}
              variant={groupVariants.stack}
              buttons={actions.map((action) => ({
                text: action.title,
                onClick: () => action.actionHandler(item),
                variant: variants.secondary,
              }))}
            />
          </div>
        </Popover>
      )}
    </div>
  );
};

export default ListActions;
