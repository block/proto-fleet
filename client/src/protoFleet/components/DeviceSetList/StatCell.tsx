import { type ReactNode } from "react";
import { createPortal } from "react-dom";

import { Info } from "@/shared/assets/icons";
import { useFloatingPosition } from "@/shared/hooks/useFloatingPosition";

const InfoTooltip = ({ heading, body }: { heading: string; body: string }) => {
  const { triggerRef, floatingStyle, isVisible, show, hide } = useFloatingPosition<HTMLButtonElement>({
    placement: "bottom-end",
    gap: 8,
    minWidth: 320,
  });

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        aria-label={`${heading}`}
        className="inline-flex appearance-none border-none bg-transparent p-0"
        onMouseEnter={show}
        onMouseLeave={hide}
        onFocus={show}
        onBlur={hide}
      >
        <Info className="shrink-0 cursor-default text-text-primary-30" />
      </button>
      {isVisible
        ? createPortal(
            <div
              role="tooltip"
              className="fixed z-50 w-80 rounded-lg bg-surface-base p-4 shadow-200"
              style={floatingStyle}
            >
              <div className="text-300 text-text-primary-50">{heading}</div>
              <div className="text-300 text-text-primary">{body}</div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
};

const StatCell = ({
  metricReportingCount,
  deviceCount,
  children,
}: {
  metricReportingCount: number;
  deviceCount: number;
  children: ReactNode;
}) => {
  if (metricReportingCount >= deviceCount) return <>{children}</>;

  return (
    <div className="flex items-center gap-1">
      {children}
      <InfoTooltip
        heading={`${metricReportingCount} of ${deviceCount} miners reporting`}
        body="Some devices do not make this data available."
      />
    </div>
  );
};

export default StatCell;
