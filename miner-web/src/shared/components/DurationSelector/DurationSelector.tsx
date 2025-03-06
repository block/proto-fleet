import { KeyboardEvent, useEffect, useState } from "react";
import clsx from "clsx";

import { durations } from "./constants";
import { Duration } from "./types";

import "./style.css";

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
  const [selectedDuration, setSelectedDuration] = useState<Duration>(
    duration || durations[0],
  );
  const [slidingDuration, setSlidingDuration] = useState<Duration>(
    duration || durations[0],
  );

  const handleSelectDuration = (duration: Duration) => {
    setSlidingDuration(duration);
  };

  const onKeyDown = (key: string, duration: Duration) => {
    if (key === "Enter") {
      handleSelectDuration(duration);
    }
  };

  const selectedDurationIndex = durations.indexOf(selectedDuration);

  const slidingDurationIndex = durations.indexOf(slidingDuration);

  const distance = Math.abs(slidingDurationIndex - selectedDurationIndex);

  useEffect(() => {
    if (selectedDuration !== slidingDuration) {
      const timeoutDuration = 100 + distance * 50;
      setTimeout(() => {
        setSelectedDuration(slidingDuration);
        onSelect?.(slidingDuration);
      }, timeoutDuration);
    }
  }, [selectedDuration, slidingDuration, distance, onSelect]);

  // since the last item has a smaller width, we need a different translateX value
  const slidingToRightEnd1 =
    selectedDurationIndex === 2 && slidingDurationIndex === 3;
  const slidingToRightEnd2 =
    selectedDurationIndex === 1 && slidingDurationIndex === 3;

  return (
    <div
      className={clsx(
        "flex bg-core-primary-5 rounded-[10px] w-fit p-[2px] text-200 text-text-primary-30 space-x-2",
        className,
      )}
    >
      {durations.map((duration) => (
        <button
          key={duration}
          className={clsx("rounded-lg relative", {
            "text-text-primary text-emphasis-200": duration === slidingDuration,
          })}
          onMouseDown={() => handleSelectDuration(duration)}
          onKeyDown={(e: KeyboardEvent<HTMLButtonElement>) =>
            onKeyDown(e.key, duration)
          }
        >
          <div
            className={clsx("h-full absolute transition-[width]", {
              "bg-surface-elevated-base shadow-100 rounded-lg":
                duration === selectedDuration,
              "w-[46px]": slidingDuration === durations[0],
              "w-12":
                slidingDuration === durations[1] ||
                slidingDuration === durations[2],
              "w-10": slidingDuration === durations[3],
              "animate-slide-right-end1": slidingToRightEnd1,
              "animate-slide-right-end2": slidingToRightEnd2,
              [`animate-slide-right${distance}`]:
                selectedDurationIndex < slidingDurationIndex &&
                !slidingToRightEnd1 &&
                !slidingToRightEnd2,
              [`animate-slide-left${distance}`]:
                selectedDurationIndex > slidingDurationIndex,
            })}
          />
          <div className="px-3 py-1 relative z-10 uppercase">{duration}</div>
        </button>
      ))}
    </div>
  );
};

export default DurationSelector;
