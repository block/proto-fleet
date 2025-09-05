import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";
import { criticalTemp } from "@/protoOS/features/kpis/constants";
import AsicTablePreview from "./AsicTablePreview";
import { useMinerHosting } from "@/protoOS/api";
import { type AsicStats } from "@/protoOS/api/types";
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
};

const convertedTemp = (temp: number, units: TemperatureUnits) => {
  const unitLabel = units === TEMP_UNITS.fahrenheit ? "ºF" : "ºC";
  const converted = units === TEMP_UNITS.fahrenheit ? convertCtoF(temp) : temp;
  return getDisplayValue(converted) + " " + unitLabel;
};

const HbTempPreview = ({ hbData, asics }: HbTempPreviewProps) => {
  const [isOverheating, setIsOverheating] = useState<boolean>(false);
  const { minerRoot } = useMinerHosting();
  const { temperatureUnits } = usePreferences();
  const [currentTemp, setCurrentTemp] = useState<string>();

  useEffect(() => {
    if (!hbData.data || !hbData.data.length) return;

    const lastTemp = hbData.data[hbData.data.length - 1].value || 0;
    setCurrentTemp(convertedTemp(lastTemp, temperatureUnits));
    setIsOverheating(lastTemp > criticalTemp);
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
          {hbData.name}
        </h3>
        {currentTemp && (
          <div className="text-emphasis-300 text-text-primary-50">
            {currentTemp}
          </div>
        )}
      </div>

      <div className="p-4">
        <AsicTablePreview asics={asics} />
      </div>
    </Link>
  );
};

export default HbTempPreview;
