import { Dispatch, SetStateAction } from "react";
import clsx from "clsx";

import { useAsicColor } from "../../../hooks";
import AsicPopover from "./AsicPopover";
import { getAsicUniqueId } from "./utility";
import { AsicStats, GetAsicHashrateParams } from "@/protoOS/api/types";
import { usePopover } from "@/shared/components/Popover";
import { TEMP_UNITS, usePreferences } from "@/shared/features/preferences";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertCtoF } from "@/shared/utils/utility";

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
  const { triggerRef: asicRef } = usePopover();
  const { temperatureUnits } = usePreferences();
  const isFahrenheit = temperatureUnits === TEMP_UNITS.fahrenheit;

  const shouldShowPopover =
    asic.id !== undefined &&
    showPopover === getAsicUniqueId(asic.id, hashboardSerial);

  const backgroundColor = useAsicColor(asic);

  return (
    <div
      className={clsx(
        "relative grow basis-0 rounded-xl border-[3px] p-[2px] phone:truncate",
        {
          "border-transparent": !shouldShowPopover,
          "border-intent-info-fill": shouldShowPopover,
        },
      )}
      ref={asicRef}
    >
      {shouldShowPopover ? (
        <AsicPopover
          asic={asic}
          duration={duration}
          granularity={granularity}
          hashboardSerial={hashboardSerial}
          closePopover={() => setShowPopover(undefined)}
        />
      ) : null}
      <button
        style={{ backgroundColor }}
        className="w-full truncate rounded-lg border border-border-5 text-center font-mono text-mono-text-50 text-text-primary"
        onClick={() =>
          setShowPopover((prev) =>
            prev || asic.id === undefined
              ? undefined
              : getAsicUniqueId(asic.id, hashboardSerial),
          )
        }
      >
        <div className="bg-transparent hover:bg-surface-overlay">
          <div className="px-1 py-3">
            {asic.temp_c && isFahrenheit
              ? getDisplayValue(convertCtoF(asic.temp_c))
              : getDisplayValue(asic.temp_c)}
            º
          </div>
        </div>
      </button>
    </div>
  );
};

export default AsicButton;
