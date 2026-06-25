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
        className="!h-8 !w-8 !p-0 text-text-primary"
        isOpen={isOpen}
        testId="global-actions-widget"
        ariaLabel="Global actions"
        ariaHasPopup="menu"
        ariaExpanded={isOpen}
      >
        <Ellipsis width={iconSizes.small} className="h-4 shrink-0" />
      </WidgetWrapper>
      {isOpen ? <GlobalActionsPopover onBlinkLEDs={handleBlinkButton} onDownloadLogs={handleDownloadButton} /> : null}
    </div>
  );
};
