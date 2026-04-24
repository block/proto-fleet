import { BulkAction } from "./types";
import Divider from "@/shared/components/Divider";
import Popover, { popoverSizes } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { type Position, positions } from "@/shared/constants";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface BulkActionsPopoverProps<ActionType> {
  actions: BulkAction<ActionType>[];
  beforeEach: (requiresConfirmation: boolean) => void;
  testId: string;
  position?: Position;
  className?: string;
}

interface ActionItemProps<ActionType> {
  action: BulkAction<ActionType>;
  onAction: (action: BulkAction<ActionType>) => void;
}

const ActionItem = <ActionType,>({ action, onAction }: ActionItemProps<ActionType>) => {
  return (
    <>
      <div className="px-4">
        <Row
          className="text-emphasis-300"
          prefixIcon={action.icon}
          testId={action.action + "-popover-button"}
          onClick={() => onAction(action)}
          compact
          divider={false}
        >
          {action.title}
        </Row>
      </div>
      {action.showGroupDivider ? <Divider dividerStyle="thick" /> : null}
    </>
  );
};

const BulkActionsPopover = <ActionType,>({
  actions,
  beforeEach,
  testId,
  position = positions["top left"],
  className,
}: BulkActionsPopoverProps<ActionType>) => {
  const { isPhone, isTablet } = useWindowDimensions();
  const onAction = (action: BulkAction<ActionType>) => {
    beforeEach(action.requiresConfirmation);
    action.actionHandler();
  };
  return (
    <Popover
      className={className ?? "-mr-3 !space-y-0 !rounded-2xl px-0 pt-2 pb-1 phone:w-[calc(100vw-theme(spacing.4))]"}
      position={position}
      size={popoverSizes.small}
      offset={20}
      yOffset={isPhone || isTablet ? -32 : 0}
      testId={testId}
    >
      {actions.map((action) => (
        <ActionItem key={action.title} action={action} onAction={onAction} />
      ))}
    </Popover>
  );
};

export default BulkActionsPopover;
