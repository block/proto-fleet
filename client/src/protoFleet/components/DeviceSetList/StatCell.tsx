import { type ReactNode, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { Info } from "@/shared/assets/icons";

const InfoTooltip = ({ heading, body }: { heading: string; body: string }) => {
  const iconRef = useRef<HTMLButtonElement>(null);
  const [visible, setVisible] = useState(false);
  const [position, setPosition] = useState({ top: 0, left: 0 });

  const handleMouseEnter = () => {
    if (iconRef.current) {
      const rect = iconRef.current.getBoundingClientRect();
      setPosition({ top: rect.bottom + 8, left: rect.right - 320 });
    }
    setVisible(true);
  };

  return (
    <>
      <button
        ref={iconRef}
        type="button"
        aria-label={`${heading}`}
        className="inline-flex appearance-none border-none bg-transparent p-0"
        onMouseEnter={handleMouseEnter}
        onMouseLeave={() => setVisible(false)}
        onFocus={handleMouseEnter}
        onBlur={() => setVisible(false)}
      >
        <Info className="shrink-0 cursor-default text-text-primary-30" />
      </button>
      {visible &&
        createPortal(
          <div
            role="tooltip"
            className="fixed z-50 w-80 rounded-lg bg-surface-base p-4 shadow-200"
            style={{ top: position.top, left: position.left }}
          >
            <div className="text-300 text-text-primary-50">{heading}</div>
            <div className="text-300 text-text-primary">{body}</div>
          </div>,
          document.body,
        )}
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
