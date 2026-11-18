import { type ReactNode, useId } from "react";
import { createPortal } from "react-dom";

import { Info } from "@/shared/assets/icons";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
import { useFloatingPosition } from "@/shared/hooks/useFloatingPosition";

export const InfoTooltip = ({ label, heading, body }: { label: string; heading?: string; body: ReactNode }) => {
  const tooltipId = useId();
  const { triggerRef, floatingStyle, isVisible, show, hide } = useFloatingPosition<HTMLButtonElement>({
    placement: "bottom-end",
    gap: 8,
    minWidth: 320,
  });
  useEscapeDismiss(isVisible ? hide : undefined);
  useClickOutside({ ref: triggerRef, onClickOutside: hide, enabled: isVisible });

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        aria-describedby={isVisible ? tooltipId : undefined}
        className="inline-flex appearance-none border-none bg-transparent p-0"
        onMouseEnter={show}
        onMouseLeave={hide}
        onFocus={show}
        onBlur={hide}
        onClick={show}
      >
        <Info className="shrink-0 cursor-default text-text-primary-30" />
      </button>
      {isVisible
        ? createPortal(
            <div
              id={tooltipId}
              role="tooltip"
              className="fixed z-50 w-80 rounded-lg bg-surface-base p-4 shadow-200"
              style={floatingStyle}
            >
              {heading ? <div className="text-300 text-text-primary-50">{heading}</div> : null}
              <div className="text-300 text-text-primary">{body}</div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
};
