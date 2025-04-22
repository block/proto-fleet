import { BulkAction } from "../types";
import Popover, { popoverSizes } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface BulkActionsPopoverProps<ActionType> {
  actions: BulkAction<ActionType>[];
  beforeEach: (requiresConfirmation: boolean) => void;
  testId: string;
}

const BulkActionsPopover = <ActionType,>({
  actions,
  beforeEach,
  testId,
}: BulkActionsPopoverProps<ActionType>) => {
  const { isPhone } = useWindowDimensions();

  const onAction = (action: BulkAction<ActionType>) => {
    beforeEach(action.requiresConfirmation);
    action.actionHandler();
  };

  return (
    <Popover
      className="px-2 pt-2 pb-1 phone:w-[calc(100vw-theme(spacing.4))]"
      position={positions.top}
      size={popoverSizes.medium}
      offset={isPhone ? 20 : 8}
      testId={testId}
    >
      <div className="divide-y divide-border-5">
        {actions.map((action) => (
          <div
            key={action.title}
            className="flex cursor-pointer items-center space-x-3 rounded-lg px-2 py-2 hover:bg-core-primary-5"
            data-testid={action.action + "-popover-button"}
            onClick={() => onAction(action)}
          >
            {action.icon}
            <div>{action.title}</div>
          </div>
        ))}
      </div>
    </Popover>
  );
};

export default BulkActionsPopover;
