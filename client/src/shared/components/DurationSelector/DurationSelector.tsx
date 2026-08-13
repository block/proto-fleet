import { useState } from "react";
import clsx from "clsx";

import Button from "@/shared/components/Button";
import { durations as defaultDurations } from "@/shared/components/DurationSelector/constants";

interface DurationSelectorProps<T extends string> {
  ariaLabel?: string;
  className?: string;
  duration?: T;
  durations?: readonly T[];
  onSelect?: (duration: T) => void;
}

function DurationSelector<T extends string>({
  ariaLabel = "Time range",
  className,
  duration,
  // Type assertion is safe here: when T is not provided explicitly, it defaults to Duration
  // (the type of defaultDurations), so the cast is valid. When T is provided explicitly
  // (e.g., FleetDuration), callers must also provide a matching durations array.
  durations = defaultDurations as unknown as readonly T[],
  onSelect,
}: DurationSelectorProps<T>) {
  const [uncontrolledDuration, setUncontrolledDuration] = useState<T>(duration ?? durations[0]);
  const selectedDuration = duration ?? uncontrolledDuration;

  const handleSelect = (nextDuration: T) => {
    if (duration === undefined) setUncontrolledDuration(nextDuration);
    onSelect?.(nextDuration);
  };

  return (
    <div className={clsx("flex gap-1", className)} role="group" aria-label={ariaLabel}>
      {durations.map((option) => {
        const isSelected = option === selectedDuration;
        return (
          <Button
            key={option}
            variant={isSelected ? "primary" : "secondary"}
            size="compact"
            text={option}
            ariaPressed={isSelected}
            onClick={() => handleSelect(option)}
            className={clsx({ "hover:opacity-100!": isSelected })}
          />
        );
      })}
    </div>
  );
}

export default DurationSelector;
