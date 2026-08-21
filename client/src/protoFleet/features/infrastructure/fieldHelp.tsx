import { useEffect, useState } from "react";

import { Question } from "@/shared/assets/icons";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

export interface FieldHelpPopoverProps {
  ariaLabel: string;
  body: string;
  testId: string;
}

const FieldHelpPopoverContent = ({ ariaLabel, body, testId }: FieldHelpPopoverProps) => {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();

  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  return (
    <div ref={triggerRef}>
      <button
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="dialog"
        aria-expanded={isOpen}
        data-testid={testId}
        className="flex h-6 w-6 items-center justify-center rounded-full text-text-primary transition-colors hover:text-text-primary-70 focus-visible:ring-2 focus-visible:ring-core-primary-20 focus-visible:outline-hidden"
        onClick={(event) => {
          event.stopPropagation();
          setIsOpen((current) => !current);
        }}
      >
        <Question className="h-4 w-4" />
      </button>
      {isOpen ? (
        <Popover
          position={positions["top left"]}
          size={popoverSizes.normal}
          offset={8}
          className="!w-80 !space-y-0 !rounded-2xl !bg-surface-elevated-base !p-4 !shadow-300 !backdrop-blur-none"
          closePopover={() => setIsOpen(false)}
          closeIgnoreSelectors={[`[data-testid='${testId}']`]}
          testId={`${testId}-popover`}
        >
          <p className="text-300 leading-6 text-text-primary-70">{body}</p>
        </Popover>
      ) : null}
    </div>
  );
};

export const FieldHelpPopover = (props: FieldHelpPopoverProps) => (
  <PopoverProvider>
    <FieldHelpPopoverContent {...props} />
  </PopoverProvider>
);
