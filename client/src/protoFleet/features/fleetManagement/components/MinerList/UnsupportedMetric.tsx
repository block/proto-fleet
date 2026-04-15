import { useFloatingPosition } from "@/shared/hooks/useFloatingPosition";

export interface UnsupportedMetricProps {
  message: string;
}

const UnsupportedMetric = ({ message }: UnsupportedMetricProps) => {
  const { triggerRef, floatingStyle, isVisible, show, hide } = useFloatingPosition<HTMLSpanElement>({
    placement: "top-center",
    gap: 8,
  });

  return (
    <>
      <span ref={triggerRef} className="text-text-primary-50" onMouseEnter={show} onMouseLeave={hide}>
        N/A
      </span>
      {isVisible && (
        <span
          className="pointer-events-none fixed z-50 w-max max-w-xs rounded-lg bg-surface-elevated-base px-3 py-2 text-300 text-text-primary shadow-300 transition-opacity duration-200"
          style={floatingStyle}
        >
          {message}
        </span>
      )}
    </>
  );
};

export default UnsupportedMetric;
