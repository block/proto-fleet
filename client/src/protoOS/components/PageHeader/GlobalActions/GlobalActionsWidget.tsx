import { useState } from "react";
import WidgetWrapper from "../WidgetWrapper";
import { GlobalActionsPopover } from "./GlobalActionsPopover";
import { Ellipsis } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { useResponsivePopover } from "@/shared/components/Popover";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface GlobalActionsWidgetProps {
  onBlinkLEDs: () => void;
  onDownloadLogs: () => void;
}

export const GlobalActionsWidget = ({ onBlinkLEDs, onDownloadLogs }: GlobalActionsWidgetProps) => {
  const { triggerRef: WidgetRef } = useResponsivePopover();
  const [isOpen, setIsOpen] = useState(false);

  useClickOutside({
    ref: WidgetRef,
    onClickOutside: () => setIsOpen(false),
    ignoreSelectors: [".popover-content"],
  });

  const handleBlinkButton = () => {
    setIsOpen(false);
    onBlinkLEDs();
  };

  const handleDownloadButton = () => {
    setIsOpen(false);
    onDownloadLogs();
  };

  return (
    <div className="relative" ref={WidgetRef}>
      <WidgetWrapper
        onClick={() => setIsOpen((prev) => !prev)}
        className="w-[28px] p-0 text-text-primary"
        isOpen={isOpen}
        ariaLabel="Global actions"
        ariaHasPopup="menu"
        ariaExpanded={isOpen}
      >
        <Ellipsis width={iconSizes.small} className="m-1" />
      </WidgetWrapper>
      {isOpen && <GlobalActionsPopover onBlinkLEDs={handleBlinkButton} onDownloadLogs={handleDownloadButton} />}
    </div>
  );
};
