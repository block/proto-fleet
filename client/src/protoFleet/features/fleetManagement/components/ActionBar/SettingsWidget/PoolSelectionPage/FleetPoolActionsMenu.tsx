import { useCallback, useState } from "react";
import { Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Popover, { popoverSizes } from "@/shared/components/Popover";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface FleetPoolActionsMenuProps {
  onTestConnection: () => void;
  onRemove: () => void;
  poolId: string;
}

const FleetPoolActionsMenuInner = ({ onTestConnection, onRemove, poolId }: FleetPoolActionsMenuProps) => {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef } = usePopover();

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  const handleTestConnection = useCallback(() => {
    setIsOpen(false);
    onTestConnection();
  }, [onTestConnection]);

  const handleRemove = useCallback(() => {
    setIsOpen(false);
    onRemove();
  }, [onRemove]);

  return (
    <div className="relative" ref={triggerRef}>
      <Button
        size={sizes.compact}
        variant={variants.secondary}
        prefixIcon={<Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
        ariaLabel="Pool actions"
        testId={`pool-${poolId}-actions-menu-button`}
        onClick={(e) => {
          e.stopPropagation();
          setIsOpen((prev) => !prev);
        }}
      />
      {isOpen ? (
        <Popover
          className="!space-y-0 px-4 pt-2 pb-1"
          position={positions["bottom right"]}
          size={popoverSizes.small}
          offset={8}
          testId={`pool-${poolId}-actions-popover`}
        >
          <Row
            className="text-emphasis-300"
            testId={`pool-${poolId}-test-connection-action`}
            onClick={handleTestConnection}
            compact
            divider
          >
            Test connection
          </Row>
          <Row
            className="text-emphasis-300"
            testId={`pool-${poolId}-remove-action`}
            onClick={handleRemove}
            compact
            divider={false}
          >
            Remove
          </Row>
        </Popover>
      ) : null}
    </div>
  );
};

const FleetPoolActionsMenu = (props: FleetPoolActionsMenuProps) => (
  <PopoverProvider>
    <FleetPoolActionsMenuInner {...props} />
  </PopoverProvider>
);

export default FleetPoolActionsMenu;
