import type { ComponentType } from "react";
import { LEDIndicator, Terminal } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import type { IconProps } from "@/shared/assets/icons/types";
import Popover, { popoverSizes } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";

interface GlobalActionsPopoverProps {
  onBlinkLEDs: () => void;
  onDownloadLogs: () => void;
}

interface MenuItem {
  icon: ComponentType<IconProps>;
  label: string;
  onClick: () => void;
}

export const GlobalActionsPopover = ({ onBlinkLEDs, onDownloadLogs }: GlobalActionsPopoverProps) => {
  const menuItems: MenuItem[] = [
    {
      icon: LEDIndicator,
      label: "Blink LEDs",
      onClick: onBlinkLEDs,
    },
    {
      icon: Terminal,
      label: "Download logs",
      onClick: onDownloadLogs,
    },
  ];

  return (
    <Popover
      className="!space-y-0 px-4 pt-2 pb-1"
      position={positions["top right"]}
      size={popoverSizes.small}
      offset={8}
      testId="global-actions-popover"
    >
      {menuItems.map(({ icon: Icon, label, onClick }) => (
        <Row
          key={label}
          className="text-emphasis-300"
          prefixIcon={<Icon width={iconSizes.small} />}
          onClick={onClick}
          compact
          divider
        >
          {label}
        </Row>
      ))}
    </Popover>
  );
};
