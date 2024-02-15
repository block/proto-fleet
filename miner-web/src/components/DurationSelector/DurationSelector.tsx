import { useState } from "react";
import clsx from "clsx";

import { durations } from "./constants";
import { Duration } from "./types";

import "./style.css";

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
        "flex bg-surface-5 rounded-lg w-fit p-1 text-emphasis-200 text-text-primary/70",
        className
      )}
    >
      {durations.map((duration) => (
        <div
          key={duration}
          className={clsx("px-3 py-[6px] hover:cursor-pointer", {
            "text-text-emphasis bg-surface-base rounded font-bold selected":
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
