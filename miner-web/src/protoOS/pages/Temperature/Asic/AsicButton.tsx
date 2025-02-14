import { Dispatch, SetStateAction, useMemo, useRef } from "react";
import clsx from "clsx";


import { dangerTemp, warningTemp } from "../constants";
import AsicPopover from "./AsicPopover";
import { getAsicUniqueId } from "./utility";
import { AsicStats, GetAsicHashrateParams } from "@/protoOS/api/types";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface AsicButtonProps {
  asic: AsicStats;
  duration: GetAsicHashrateParams["duration"];
  granularity: GetAsicHashrateParams["granularity"];
  hashboardSerial: string;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
}

const AsicButton = ({
  asic,
  duration,
  granularity,
  hashboardSerial,
  showPopover,
  setShowPopover,
}: AsicButtonProps) => {
  const asicRef = useRef<HTMLDivElement>(null);
  const shouldShowPopover =
    asic.id !== undefined &&
    showPopover === getAsicUniqueId(asic.id, hashboardSerial);

  useClickOutside({
    ref: asicRef,
    onClickOutside: () => setShowPopover(undefined),
  });

  const temp = useMemo(() => asic.temp_c || 0, [asic.temp_c]);

  return (
    <div
      className={clsx(
        "basis-0 grow relative phone:static p-[2px] border-[3px] rounded-xl phone:truncate",
        {
          "border-transparent": !shouldShowPopover,
          "border-intent-info-fill": shouldShowPopover,
        }
      )}
      ref={asicRef}
    >
      {shouldShowPopover ? (
        <AsicPopover
          asic={asic}
          duration={duration}
          granularity={granularity}
          hashboardSerial={hashboardSerial}
        />
      ) : null}
      <button
        className={clsx(
          "font-mono text-mono-text-50 text-text-primary text-center rounded-lg border border-border-5 w-full truncate",
          {
            "bg-surface-base": temp < warningTemp,
            "bg-intent-warning-50": temp >= warningTemp && temp < dangerTemp,
            "bg-intent-warning-fill": temp >= dangerTemp,
          }
        )}
        onClick={() =>
          setShowPopover((prev) =>
            prev || asic.id === undefined
              ? undefined
              : getAsicUniqueId(asic.id, hashboardSerial)
          )
        }
      >
        <div className="bg-transparent hover:bg-surface-overlay">
          <div className="px-1 py-3">{asic.temp_c}º</div>
        </div>
      </button>
    </div>
  );
};

export default AsicButton;
