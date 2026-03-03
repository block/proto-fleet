import { CSSProperties, useLayoutEffect, useRef, useState } from "react";
import clsx from "clsx";
import SegmentComponent from "@/shared/components/SegmentedControl/Segment";
import { type Segment } from "@/shared/components/SegmentedControl/types";

interface SegmentedControlProps {
  className?: string;
  segmentClassName?: string;
  segments: Segment[];
  initialSegmentKey?: string;
  onSelect: (selectedKey: string) => void;
}

const SegmentedControl = ({
  className,
  segmentClassName,
  segments,
  initialSegmentKey,
  onSelect,
}: SegmentedControlProps) => {
  const segmentRefs = useRef(new Map<string, HTMLButtonElement>());
  const [sliderStyle, setSliderStyle] = useState<CSSProperties>({});

  const [selectedSegment, setSelectedSegment] = useState<string | null>(initialSegmentKey ?? segments[0]?.key);

  useLayoutEffect(() => {
    const calculateSliderStyle = () => {
      if (!selectedSegment) {
        return;
      }
      const segmentElement = segmentRefs.current.get(selectedSegment);
      if (segmentElement === undefined) {
        return;
      }

      // subtract 2px to account for padding
      setSliderStyle({
        transform: `translateX(${segmentElement.offsetLeft - 2}px)`,
        width: `${segmentElement.offsetWidth}px`,
      });
    };

    const timeoutId = setTimeout(calculateSliderStyle, 100);
    return () => {
      clearTimeout(timeoutId);
    };
  }, [segments, selectedSegment]);

  const handleSelect = (selectedKey: string) => {
    setSelectedSegment(selectedKey);
    onSelect(selectedKey);
  };

  if (segments.length === 0) return null;

  return (
    <div
      className={clsx("relative flex h-full w-fit flex-row gap-2 rounded-3xl bg-core-primary-5 p-[2px]", className)}
      data-testid="segmented-control"
    >
      <div
        className={clsx(
          "absolute h-[calc(100%-theme(spacing.1))] rounded-3xl bg-surface-elevated-base shadow-100 transition-all duration-200",
        )}
        style={sliderStyle}
      />
      {segments.map((segment) => (
        <SegmentComponent
          key={segment.key}
          segmentRef={(node) => {
            if (node === null) {
              segmentRefs.current.delete(segment.key);
            } else {
              segmentRefs.current.set(segment.key, node);
            }
          }}
          className={segmentClassName}
          segment={segment}
          selected={selectedSegment === segment.key}
          onSelect={() => handleSelect(segment.key)}
        />
      ))}
    </div>
  );
};

export default SegmentedControl;
