import { BulkAction } from "./types";
import Popover, { popoverSizes } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";

interface BulkActionsPopoverProps<ActionType> {
  actions: BulkAction<ActionType>[];
  beforeEach: (requiresConfirmation: boolean) => void;
  testId: string;
}

interface ActionItemProps<ActionType> {
  action: BulkAction<ActionType>;
  onAction: (action: BulkAction<ActionType>) => void;
}

const ActionItem = <ActionType,>({ action, onAction }: ActionItemProps<ActionType>) => {
  return (
    <Row
      key={action.title}
      className="text-emphasis-300"
      prefixIcon={action.icon}
      testId={action.action + "-popover-button"}
      onClick={() => onAction(action)}
      compact
      divider
    >
      {action.title}
    </Row>
  );
};

const BulkActionsPopover = <ActionType,>({ actions, beforeEach, testId }: BulkActionsPopoverProps<ActionType>) => {
  const onAction = (action: BulkAction<ActionType>) => {
    beforeEach(action.requiresConfirmation);
    action.actionHandler();
  };
  return (
    <Popover
      className="-mr-3 !space-y-0 px-4 pt-2 pb-1 phone:w-[calc(100vw-theme(spacing.4))]"
      position={positions["top left"]}
      size={popoverSizes.medium}
      offset={20}
      testId={testId}
    >
      {actions.map((action) => (
        <ActionItem key={action.title} action={action} onAction={onAction} />
      ))}
    </Popover>
  );
};

export default BulkActionsPopover;
