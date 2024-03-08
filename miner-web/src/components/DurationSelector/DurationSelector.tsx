import { KeyboardEvent, useCallback, useState } from "react";
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

  const onKeyDown = useCallback((key: string, duration: Duration) => {
    if (key === "Enter") {
      setSelectedDuration(duration);
    }
  }, []);

  return (
    <div
      className={clsx(
        "flex bg-surface-5 rounded-[10px] w-fit p-[2px] text-200 text-text-primary/30 space-x-2",
        className
      )}
    >
      {durations.map((duration) => (
        <button
          key={duration}
          className={clsx("px-3 py-1 rounded-lg", {
            "text-text-emphasis text-emphasis-200 bg-surface-base shadow-100":
              duration === selectedDuration,
          })}
          onClick={() => setSelectedDuration(duration)}
          onKeyDown={(e: KeyboardEvent<HTMLButtonElement>) =>
            onKeyDown(e.key, duration)
          }
        >
          {duration}
        </button>
      ))}
    </div>
  );
};

export default DurationSelector;
