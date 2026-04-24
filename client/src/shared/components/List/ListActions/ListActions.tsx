import { useEffect, useState } from "react";
import clsx from "clsx";
import { Ellipsis } from "@/shared/assets/icons";
import { ListAction, resolveListActionValue } from "@/shared/components/List/types";
import Popover, { popoverSizes, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";

interface ListActionProps<ListItem> {
  item: ListItem;
  actions: ListAction<ListItem>[];
  disabled?: boolean;
}

const ListActions = <ListItem,>({ item, actions, disabled = false }: ListActionProps<ListItem>) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();

  const [actionsVisible, setActionsVisible] = useState<boolean>(false);

  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  if (!actions || actions.length === 0) {
    return null;
  }

  const resolvedActions = actions
    .filter((action) => !resolveListActionValue(action.hidden, item))
    .map((action, index, visibleActions) => ({
      action,
      title: resolveListActionValue(action.title, item),
      icon: resolveListActionValue(action.icon, item),
      variant: resolveListActionValue(action.variant, item),
      disabled: resolveListActionValue(action.disabled, item) === true,
      showDividerAfter: resolveListActionValue(action.showDividerAfter, item) ?? index < visibleActions.length - 1,
    }));

  if (resolvedActions.length === 0) {
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
      {actionsVisible && !disabled ? (
        <Popover
          className="!space-y-0 px-4 pt-2 pb-1"
          position={positions["bottom left"]}
          size={popoverSizes.small}
          closePopover={() => setActionsVisible(false)}
        >
          {resolvedActions.map(
            ({ action, title, icon, variant, disabled: actionDisabled, showDividerAfter }, index) => {
              const colorClass = clsx(
                variant === "destructive" && "text-intent-critical-fill",
                actionDisabled && "text-text-primary-50",
              );

              return (
                <Row
                  key={`${title}-${index}`}
                  className={clsx("text-emphasis-300", colorClass)}
                  prefixIcon={icon && colorClass ? <div className={colorClass}>{icon}</div> : icon}
                  onClick={
                    actionDisabled
                      ? undefined
                      : () => {
                          action.actionHandler(item);
                          setActionsVisible(false);
                        }
                  }
                  compact
                  divider={showDividerAfter}
                >
                  {title}
                </Row>
              );
            },
          )}
        </Popover>
      ) : null}
    </div>
  );
};

export default ListActions;
