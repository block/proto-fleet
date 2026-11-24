import { type ReactNode, useCallback, useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";

import { Info } from "@/shared/assets/icons";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
import { useFloatingPosition } from "@/shared/hooks/useFloatingPosition";

const HOVER_CLOSE_DELAY_MS = 150;

export const InfoTooltip = ({ label, heading, body }: { label: string; heading?: string; body: ReactNode }) => {
  const tooltipId = useId();
  const tooltipRef = useRef<HTMLDivElement>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const interaction = useRef({ triggerHovered: false, tooltipHovered: false, triggerFocused: false });
  const { triggerRef, floatingStyle, isVisible, show, hide } = useFloatingPosition<HTMLButtonElement>({
    placement: "bottom-end",
    gap: 8,
    minWidth: 320,
  });
  const cancelClose = useCallback(() => {
    clearTimeout(closeTimer.current);
    closeTimer.current = undefined;
  }, []);
  useEffect(() => cancelClose, [cancelClose]);

  const dismiss = () => {
    cancelClose();
    interaction.current.tooltipHovered = false;
    hide();
  };
  const open = () => {
    cancelClose();
    show();
  };
  const setInteraction = (key: keyof typeof interaction.current, active: boolean) => {
    interaction.current[key] = active;
    if (active) {
      open();
    } else if (!Object.values(interaction.current).some(Boolean)) {
      cancelClose();
      closeTimer.current = setTimeout(dismiss, HOVER_CLOSE_DELAY_MS);
    }
  };

  useEscapeDismiss(isVisible ? dismiss : undefined);
  useClickOutside({
    ref: triggerRef,
    onClickOutside: dismiss,
    shouldIgnore: (event) => event.target instanceof Node && Boolean(tooltipRef.current?.contains(event.target)),
    enabled: isVisible,
  });

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        aria-describedby={isVisible ? tooltipId : undefined}
        className="inline-flex appearance-none border-none bg-transparent p-0"
        onMouseEnter={() => setInteraction("triggerHovered", true)}
        onMouseLeave={() => setInteraction("triggerHovered", false)}
        onFocus={() => setInteraction("triggerFocused", true)}
        onBlur={() => setInteraction("triggerFocused", false)}
        onClick={open}
      >
        <Info className="shrink-0 cursor-default text-text-primary-30" />
      </button>
      {isVisible
        ? createPortal(
            <div
              ref={tooltipRef}
              id={tooltipId}
              role="tooltip"
              className="fixed z-50 w-80 rounded-lg bg-surface-base p-4 shadow-200"
              style={floatingStyle}
              onMouseEnter={() => setInteraction("tooltipHovered", true)}
              onMouseLeave={() => setInteraction("tooltipHovered", false)}
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
