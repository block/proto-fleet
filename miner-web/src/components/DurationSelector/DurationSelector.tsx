import { useState } from "react";
import clsx from "clsx";

import { durations } from "./constants";
import { Duration } from "./types";

interface DurationSelectorProps {
  className?: string;
}

const DurationSelector = ({ className }: DurationSelectorProps) => {
  const [selectedDuration, setSelectedDuration] = useState<Duration>(
    durations[0]
  );

  return (
    <div
      className={clsx(
        "flex bg-surface-5 rounded-[10px] w-fit p-[2px] text-200 text-text-primary/30 space-x-2",
        className
      )}
    >
      {durations.map((duration) => (
        <div
          key={duration}
          className={clsx("px-3 py-1 hover:cursor-pointer", {
            "text-text-emphasis text-emphasis-200 bg-surface-base rounded-lg shadow-100":
              duration === selectedDuration,
          })}
          onClick={() => setSelectedDuration(duration)}
        >
          {duration}
        </div>
      ))}
    </div>
  );
};

export default DurationSelector;
