import { type MouseEvent, type ReactNode, useState } from "react";

import Button, { sizes, variants } from "@/shared/components/Button";
import Popover, { PopoverProvider, popoverSizes, useResponsivePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

interface PageHeaderPopoverPillProps {
  ariaLabel: string;
  children: (props: { closePopover: () => void }) => ReactNode;
  prefixIcon: ReactNode;
  triggerClassName: string;
  triggerContent: ReactNode;
}

function PageHeaderPopoverPillContent({
  ariaLabel,
  children,
  prefixIcon,
  triggerClassName,
  triggerContent,
}: PageHeaderPopoverPillProps) {
  const [isPopoverOpen, setIsPopoverOpen] = useState(false);
  const { triggerRef } = useResponsivePopover();

  function closePopover(): void {
    setIsPopoverOpen(false);
  }

  function handleTriggerClick(clickEvent: MouseEvent<HTMLButtonElement>): void {
    setIsPopoverOpen((current) => !current);

    if (clickEvent.detail > 0) {
      clickEvent.currentTarget.blur();
    }
  }

  return (
    <div className={`${triggerClassName} relative`} ref={triggerRef}>
      <Button
        variant={variants.secondary}
        size={sizes.compact}
        ariaHasPopup={true}
        ariaExpanded={isPopoverOpen}
        ariaLabel={ariaLabel}
        onClick={handleTriggerClick}
        prefixIcon={prefixIcon}
      >
        {triggerContent}
      </Button>

      {isPopoverOpen ? (
        <Popover
          position={positions["bottom left"]}
          size={popoverSizes.small}
          className="!space-y-0 px-4 pt-4 pb-3"
          closePopover={closePopover}
          closeIgnoreSelectors={[`.${triggerClassName}`]}
        >
          {children({ closePopover })}
        </Popover>
      ) : null}
    </div>
  );
}

function PageHeaderPopoverPill(props: PageHeaderPopoverPillProps) {
  return (
    <PopoverProvider>
      <PageHeaderPopoverPillContent {...props} />
    </PopoverProvider>
  );
}

export default PageHeaderPopoverPill;
