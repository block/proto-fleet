import { useMemo } from "react";
import { Duration } from "./types";

import { durations } from "@/shared/components/DurationSelector/constants";
import SegmentedControl from "@/shared/components/SegmentedControl";
import type { Segment } from "@/shared/components/SegmentedControl/types";

interface DurationSelectorProps {
  className?: string;
  duration?: Duration;
  onSelect?: (duration: Duration) => void;
}

const DurationSelector = ({
  className,
  duration,
  onSelect,
}: DurationSelectorProps) => {
  const durationSegments = useMemo(() => {
    return durations.map((duration: Duration) => ({
      key: duration,
      title: duration,
    })) as Segment[];
  }, []);

  return (
    <SegmentedControl
      className={className}
      segmentClassName="uppercase"
      segments={durationSegments}
      initialSegmentKey={duration}
      onSelect={(key) => onSelect && onSelect(key as Duration)}
    />
  );
};

export default DurationSelector;
