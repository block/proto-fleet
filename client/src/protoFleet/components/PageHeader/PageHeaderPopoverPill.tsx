import { type MouseEvent, type ReactNode, useState } from "react";
import clsx from "clsx";

import Button, { sizes, variants } from "@/shared/components/Button";
import Popover, { PopoverProvider, popoverSizes, useResponsivePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

interface PageHeaderPopoverPillProps {
  ariaLabel: string;
  children: (props: { closePopover: () => void }) => ReactNode;
  dotClassName: string;
  triggerClassName: string;
  triggerLabel: ReactNode;
}

function getClassNameSelectors(className: string): string[] {
  return className
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map((classNameToken) => `.${classNameToken}`);
}

function PageHeaderPopoverPillContent({
  ariaLabel,
  children,
  dotClassName,
  triggerClassName,
  triggerLabel,
}: PageHeaderPopoverPillProps) {
  const [isPopoverOpen, setIsPopoverOpen] = useState(false);
  const { triggerRef } = useResponsivePopover();
  const closeIgnoreSelectors = getClassNameSelectors(triggerClassName);

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
        prefixIcon={<span className={clsx("h-2.5 w-2.5 rounded-full", dotClassName)} />}
      >
        <span className="block max-w-56 truncate">{triggerLabel}</span>
      </Button>

      {isPopoverOpen ? (
        <Popover
          position={positions["bottom left"]}
          size={popoverSizes.small}
          className="!space-y-0 px-4 pt-4 pb-3"
          closePopover={closePopover}
          closeIgnoreSelectors={closeIgnoreSelectors}
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
