import { Dispatch, SetStateAction, useMemo } from "react";
import clsx from "clsx";

import AsicPopover from "./AsicPopover";
import { getAsicUniqueId } from "./utility";
import { useAsicColor } from "@/protoOS/features/kpis/hooks";
import {
  AsicData,
  convertAndFormatMeasurement,
  getAsicName,
} from "@/protoOS/store";
import { useTemperatureUnit } from "@/protoOS/store";
import { usePopover } from "@/shared/components/Popover";

interface AsicButtonProps {
  asic: AsicData;
  hashboardSerial: string;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
  totalAsicCount: number; // Pass this in to avoid calling useMinerHashboard
}

const AsicButton = ({
  asic,
  hashboardSerial,
  showPopover,
  setShowPopover,
  totalAsicCount,
}: AsicButtonProps) => {
  const { triggerRef: asicRef } = usePopover();
  const temperatureUnit = useTemperatureUnit();

  const currentAsicId = useMemo(
    () =>
      asic.id !== undefined
        ? getAsicUniqueId(asic.id, hashboardSerial)
        : undefined,
    [asic.id, hashboardSerial],
  );

  const shouldShowPopover =
    currentAsicId !== undefined && showPopover === currentAsicId;

  const backgroundColor = useAsicColor(asic);

  // Generate ASIC name using utility function - now using passed-in totalAsicCount
  const asicName = useMemo(() => {
    return asic.index !== undefined
      ? getAsicName(totalAsicCount, asic.index)
      : "";
  }, [totalAsicCount, asic.index]);

  const temperatureDisplay = useMemo(
    () =>
      convertAndFormatMeasurement(
        asic.temperature?.latest,
        temperatureUnit,
        false,
      ),
    [asic.temperature, temperatureUnit],
  );

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
            <div className="text-text-primary-50">{asicName}</div>
            {temperatureDisplay}
          </div>
        </div>
      </button>
    </div>
  );
};

export default AsicButton;
