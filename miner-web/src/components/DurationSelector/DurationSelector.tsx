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
        "flex bg-foreground-20 rounded-lg w-fit p-1 text-body-regular font-medium text-foreground-60",
        className
      )}
    >
      {durations.map((duration) => (
        <div
          key={duration}
          className={clsx("px-3 py-[6px] hover:cursor-pointer", {
            "text-warning-100 bg-white-100 rounded font-bold selected":
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
