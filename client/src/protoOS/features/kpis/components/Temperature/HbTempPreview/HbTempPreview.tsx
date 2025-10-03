import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";
import AsicTablePreview from "./AsicTablePreview";
import { useHashboardStatus } from "@/protoOS/api";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { criticalTemp } from "@/protoOS/features/kpis/constants";
import {
  convertAndFormatMeasurement,
  getCurrentValue,
  type HashboardData,
  useMinerHashboard,
} from "@/protoOS/store";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { TEMP_UNITS, usePreferences } from "@/shared/features/preferences/";

type HbTempPreviewProps = {
  hbData: HashboardData;
};

// TODO: [STRORE_REFACTOR] We should add an xsmall variant of share/componentes/Stat and use here
const TempDisplay = ({
  formattedTemp,
  label,
}: {
  formattedTemp: string | undefined;
  label: string;
}) => {
  return (
    <div className="flex flex-col">
      <div className="text-emphasis-300 text-text-primary-70">
        {formattedTemp ? formattedTemp : <SkeletonBar className="my-1" />}
      </div>
      <div className="text-xs text-text-primary-50">{label}</div>
    </div>
  );
};

const HbTempPreview = ({ hbData }: HbTempPreviewProps) => {
  const [isOverheating, setIsOverheating] = useState<boolean>(false);
  const { minerRoot } = useMinerHosting();
  const { temperatureUnits } = usePreferences();

  // TODO: [STORE_REFACTOR] Do we need this call?  can we move it up the tree to avoid multiple calls?
  // populates hardware store with AsicInfo[]
  // populates telemetry store with hashboard inlet, outlet, avg asic temp, and max asic temp
  useHashboardStatus({
    hashboardSerialNumber: hbData.serial,
    poll: true,
  });

  const hashboard = useMinerHashboard(hbData.serial);

  useEffect(() => {
    if (!hbData.temperature?.values?.length) return;

    const lastTemp = getCurrentValue(
      hbData.temperature,
      // TODO: [STORE_REFACTOR] clean this redundant expression up when we move preferences to zustand store,
      // and share the same unit types that telemetry uses. that way we just pass tempUnits
      temperatureUnits === TEMP_UNITS.fahrenheit ? "F" : "C",
      false,
    );
    setIsOverheating(!!lastTemp?.value && lastTemp.value > criticalTemp);
  }, [hbData, temperatureUnits]);

  return (
    <Link
      data-testid="hb-temp-preview"
      to={`${minerRoot}/temperature/${hbData.serial}`}
      className={clsx(
        "group block overflow-hidden phone:w-full phone:rounded-xl phone:border-1 phone:border-border-10",
        isOverheating
          ? "hover:bg-intent-critical-20"
          : "hover:bg-core-primary-2",
      )}
    >
      <div
        className={clsx(
          "relative flex justify-between px-4 py-1",
          isOverheating
            ? "bg-intent-critical-20 group-hover:bg-transparent"
            : "bg-core-primary-5",
        )}
      >
        <h3
          className={clsx(
            "text-emphasis-300",
            isOverheating
              ? "text-intent-critical-text"
              : "text-text-primary-70",
          )}
        >
          Hashboard {hbData.slot}
        </h3>
      </div>

      <div className="px-4 py-2">
        <div className="grid grid-cols-2 gap-x-4">
          <TempDisplay
            formattedTemp={convertAndFormatMeasurement(
              hashboard?.avgAsicTemp,
              temperatureUnits === TEMP_UNITS.fahrenheit ? "F" : "C",
              true,
            )}
            label="ASIC avg"
          />

          <TempDisplay
            formattedTemp={convertAndFormatMeasurement(
              hashboard?.maxAsicTemp,
              temperatureUnits === TEMP_UNITS.fahrenheit ? "F" : "C",
              true,
            )}
            label="ASIC high"
          />
        </div>
      </div>

      <div className="p-4">
        <AsicTablePreview hashboardSerial={hbData.serial} />
      </div>
    </Link>
  );
};

export default HbTempPreview;
