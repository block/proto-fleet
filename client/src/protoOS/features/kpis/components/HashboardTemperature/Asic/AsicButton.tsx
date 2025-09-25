import { Dispatch, SetStateAction } from "react";
import clsx from "clsx";

import AsicPopover from "./AsicPopover";
import { convertAndFormatTemperature } from "./AsicPopover/utility";
import { getAsicUniqueId } from "./utility";
import { AsicStats, GetAsicHashrateParams } from "@/protoOS/api/types";
import { useAsicColor } from "@/protoOS/features/kpis/hooks";
import useHashboardAsicStore from "@/protoOS/store/useHashboardAsicStore";
import { type Duration } from "@/shared/components/DurationSelector";
import { usePopover } from "@/shared/components/Popover";
import { usePreferences } from "@/shared/features/preferences";

interface AsicButtonProps {
  asic: AsicStats;
  duration: Duration;
  granularity: GetAsicHashrateParams["granularity"];
  hashboardSerial: string;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
}

// TODO: temporary work around to derive name from asic ID until name is included in API
const getAsicName = (
  idx: number | undefined,
  asicCount: number | undefined,
) => {
  if (idx === undefined || asicCount === undefined) {
    return "";
  }

  const group = idx + 1 > asicCount / 2 ? "B" : "A";
  const groupSize = Math.floor(asicCount / 2);

  return `${group}${idx % groupSize}`;
};

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
  const asicCount = useHashboardAsicStore((state) =>
    state.getAsicCount(hashboardSerial),
  );

  const currentAsicId =
    asic.id !== undefined
      ? getAsicUniqueId(asic.id, hashboardSerial)
      : undefined;
  const shouldShowPopover =
    currentAsicId !== undefined && showPopover === currentAsicId;

  const backgroundColor = useAsicColor(asic);

  return (
    <div
      className={clsx(
        "relative mb-1.5 grow basis-0 rounded-xl p-[2px] shadow-[0_0_0_3px] phone:truncate",
        {
          "shadow-transparent": !shouldShowPopover,
          "shadow-intent-info-fill": shouldShowPopover,
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
          closeIgnoreSelectors={[".asic-button"]}
        />
      ) : null}
      <button
        style={{ backgroundColor }}
        className="asic-button w-full cursor-default truncate rounded-lg border border-border-5 text-center font-mono text-mono-text-50 text-text-primary"

        // TODO: removed temporarily until asics have more data to show in the popover
        // onClick={() =>
        //   setShowPopover((prev) =>
        //     prev === currentAsicId ? undefined : currentAsicId,
        //   )
        // }
      >
        <div className="bg-transparent hover:bg-surface-overlay">
          <div className="flex flex-col items-center gap-1 px-1 py-3">
            <div className="text-text-primary-50">
              {getAsicName(asic.id, asicCount)}
            </div>
            {convertAndFormatTemperature(asic.temp_c, temperatureUnits, false)}
          </div>
        </div>
      </button>
    </div>
  );
};

export default AsicButton;
