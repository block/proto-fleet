import { useState } from "react";
import clsx from "clsx";
import { Duration } from "./types";

import Button from "@/shared/components/Button";
import { durations } from "@/shared/components/DurationSelector/constants";

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
  // Initialize with the provided duration or default to the first option
  const [selectedDuration, setSelectedDuration] = useState<Duration>(
    duration || durations[0],
  );

  const handleSelect = (d: Duration) => {
    setSelectedDuration(d);
    onSelect && onSelect(d);
  };

  return (
    <div className={clsx("flex gap-1", className)}>
      {durations.map((d) => {
        const isSelected = d === selectedDuration;
        return (
          <Button
            key={d}
            variant={isSelected ? "primary" : "secondary"}
            size="compact"
            text={d}
            onClick={() => handleSelect(d)}
          />
        );
      })}
    </div>
  );
};

export default DurationSelector;
