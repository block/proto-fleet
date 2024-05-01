import { useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { AsicStats } from "apiTypes";

import { useClickOutside } from "common/hooks/useClickOutside";

import { dangerTemp, warningTemp } from "../constants";
import AsicPopover from "./AsicPopover";

interface AsicButtonProps {
  asic: AsicStats;
}

const AsicButton = ({ asic }: AsicButtonProps) => {
  const asicRef = useRef<HTMLDivElement>(null);
  const [showPopover, setShowPopover] = useState(false);

  useClickOutside({
    ref: asicRef,
    onClickOutside: () => setShowPopover(false),
  });

  const temp = useMemo(() => asic.temp_c || 0, [asic.temp_c]);

  return (
    <div
      className={clsx(
        "basis-0 grow relative phone:static p-[2px] border-[3px] rounded-xl phone:truncate",
        {
          "border-transparent": !showPopover,
          "border-intent-info-fill": showPopover,
        }
      )}
      ref={asicRef}
    >
      {showPopover && <AsicPopover asic={asic} />}
      <button
        className={clsx(
          "text-mono-text-50 text-text-primary/90 text-center rounded-lg border border-border-primary/5 w-full truncate",
          {
            "bg-surface-base": temp < warningTemp,
            "bg-intent-warning-fill/50":
              temp >= warningTemp && temp < dangerTemp,
            "bg-intent-warning-fill": temp >= dangerTemp,
          }
        )}
        onClick={() => setShowPopover((prev) => !prev)}
      >
        <div className="bg-transparent hover:bg-surface-overlay">
          <div className="px-1 py-3">{asic.temp_c}º</div>
        </div>
      </button>
    </div>
  );
};

export default AsicButton;
