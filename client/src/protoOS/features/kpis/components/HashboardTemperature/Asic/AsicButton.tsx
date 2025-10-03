import { Dispatch, SetStateAction } from "react";
import clsx from "clsx";

import AsicPopover from "./AsicPopover";
import { getAsicUniqueId } from "./utility";
import { useAsicColor } from "@/protoOS/features/kpis/hooks";
import { AsicData, getAsicName, useMinerHashboard } from "@/protoOS/store";
import { getCurrentValue } from "@/protoOS/store";
import { usePopover } from "@/shared/components/Popover";
import { usePreferences } from "@/shared/features/preferences";

interface AsicButtonProps {
  asic: AsicData;
  hashboardSerial: string;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
}

const AsicButton = ({
  asic,
  hashboardSerial,
  showPopover,
  setShowPopover,
}: AsicButtonProps) => {
  const { triggerRef: asicRef } = usePopover();
  const { temperatureUnits } = usePreferences();
  const hashboard = useMinerHashboard(hashboardSerial);

  const currentAsicId =
    asic.id !== undefined
      ? getAsicUniqueId(asic.id, hashboardSerial)
      : undefined;
  const shouldShowPopover =
    currentAsicId !== undefined && showPopover === currentAsicId;

  const backgroundColor = useAsicColor(asic);

  // Generate ASIC name using utility function
  const asicName =
    hashboard?.asicIds && asic.index
      ? getAsicName(hashboard.asicIds.length, asic.index)
      : "";

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
            {
              getCurrentValue(
                asic.temperature,
                temperatureUnits === "fahrenheit" ? "F" : "C",
                false,
              )?.formatted
            }
          </div>
        </div>
      </button>
    </div>
  );
};

export default AsicButton;
