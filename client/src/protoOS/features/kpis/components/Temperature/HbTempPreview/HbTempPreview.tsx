import { Link } from "react-router-dom";
import clsx from "clsx";
import AsicTablePreview from "./AsicTablePreview";
import { useMinerHosting } from "@/protoOS/api";
import { type AsicStats } from "@/protoOS/api/types";
import { criticalTemp } from "@/protoOS/features/kpis/constants";
import { type HbTemperature } from "@/protoOS/features/kpis/hooks";
import {
  TEMP_UNITS,
  type TemperatureUnits,
  usePreferences,
} from "@/shared/features/preferences/";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertCtoF } from "@/shared/utils/utility";

type HbTempPreviewProps = {
  hbData: HbTemperature;
  asics?: AsicStats[];
  avgAsicTempC?: number;
  maxAsicTempC?: number;
};

const convertedTemp = (temp: number, units: TemperatureUnits) => {
  const unitLabel = units === TEMP_UNITS.fahrenheit ? "ºF" : "ºC";
  const converted = units === TEMP_UNITS.fahrenheit ? convertCtoF(temp) : temp;
  return getDisplayValue(converted) + " " + unitLabel;
};

const HbTempPreview = ({
  hbData,
  asics,
  avgAsicTempC,
  maxAsicTempC,
}: HbTempPreviewProps) => {
  const { minerRoot } = useMinerHosting();
  const { temperatureUnits } = usePreferences();
  const isOverheating = (maxAsicTempC || 0) > criticalTemp;

  const TempDisplay = ({ value, label }: { value?: number; label: string }) => (
    <div className="flex flex-col">
      <div className="text-emphasis-300 text-text-primary-70">
        {value ? convertedTemp(value, temperatureUnits) : "N/A"}
      </div>
      <div className="text-xs text-text-primary-50">{label}</div>
    </div>
  );

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
          {hbData.name}
        </h3>
      </div>

      <div className="px-4 py-2">
        <div className="grid grid-cols-2 gap-x-4">
          <TempDisplay value={avgAsicTempC} label="ASIC avg" />
          <TempDisplay value={maxAsicTempC} label="ASIC high" />
        </div>
      </div>

      <div className="p-4">
        <AsicTablePreview asics={asics} />
      </div>
    </Link>
  );
};

export default HbTempPreview;
