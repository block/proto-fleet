import { MouseEvent, useCallback, useState } from "react";

interface TooltipPosition {
  top: number;
  left: number;
}

const TOOLTIP_OFFSET_Y = 8;

export interface UnsupportedMetricProps {
  message: string;
}

const UnsupportedMetric = ({ message }: UnsupportedMetricProps) => {
  const [isVisible, setIsVisible] = useState(false);
  const [position, setPosition] = useState<TooltipPosition>({ top: 0, left: 0 });

  const handleMouseEnter = useCallback((e: MouseEvent<HTMLSpanElement>) => {
    const { top, left, width } = e.currentTarget.getBoundingClientRect();
    setPosition({ top: top - TOOLTIP_OFFSET_Y, left: left + width / 2 });
    setIsVisible(true);
  }, []);

  const handleMouseLeave = useCallback(() => {
    setIsVisible(false);
  }, []);

  return (
    <>
      <span className="text-text-primary-50" onMouseEnter={handleMouseEnter} onMouseLeave={handleMouseLeave}>
        N/A
      </span>
      {isVisible && (
        <span
          className="pointer-events-none fixed top-0 left-0 z-50 w-max max-w-xs rounded-lg bg-surface-elevated-base px-3 py-2 text-300 text-text-primary shadow-300 transition-opacity duration-200"
          style={{
            transform: `translate(${position.left}px, ${position.top}px) translate(-50%, -100%)`,
          }}
        >
          {message}
        </span>
      )}
    </>
  );
};

export default UnsupportedMetric;
