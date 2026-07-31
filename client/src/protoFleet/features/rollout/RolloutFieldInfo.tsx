import { type ReactElement, useEffect, useState } from "react";

import { Question } from "@/shared/assets/icons";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

interface RolloutFieldInfoProps {
  ariaLabel: string;
  body: string;
  testId?: string;
  popoverTestId?: string;
}

/**
 * A small question-mark info toggle that opens a popover with helper text,
 * intended as a field's `suffixAction`. Mirrors the `FieldInfoToggle` in
 * CurtailmentStartModal so rollout fields carry help the same way curtailment
 * fields do, keeping the field label itself uncluttered.
 */
function RolloutFieldInfoContent({ ariaLabel, body, testId, popoverTestId }: RolloutFieldInfoProps): ReactElement {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();

  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  return (
    <div ref={triggerRef} className="relative">
      <button
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="dialog"
        aria-expanded={isOpen}
        data-testid={testId}
        className="flex h-6 w-6 items-center justify-center rounded-full text-text-primary-50 transition-colors hover:text-text-primary-70 focus-visible:ring-2 focus-visible:ring-core-primary-20 focus-visible:outline-hidden"
        onClick={(event) => {
          event.stopPropagation();
          setIsOpen((current) => !current);
        }}
      >
        <Question className="h-4 w-4" />
      </button>
      {isOpen ? (
        <Popover
          position={positions["bottom right"]}
          size={popoverSizes.normal}
          offset={8}
          className="!space-y-0 !rounded-2xl !bg-surface-elevated-base !p-6 !shadow-300 !backdrop-blur-none"
          closePopover={() => setIsOpen(false)}
          closeIgnoreSelectors={testId ? [`[data-testid='${testId}']`] : undefined}
          testId={popoverTestId}
        >
          <p className="text-300 leading-6 text-text-primary-70">{body}</p>
        </Popover>
      ) : null}
    </div>
  );
}

function RolloutFieldInfo(props: RolloutFieldInfoProps): ReactElement {
  return (
    <PopoverProvider>
      <RolloutFieldInfoContent {...props} />
    </PopoverProvider>
  );
}

export default RolloutFieldInfo;
