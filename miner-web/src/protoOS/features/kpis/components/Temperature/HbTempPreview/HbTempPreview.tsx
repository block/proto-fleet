import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";
import { criticalTemp } from "../../../constants";
import AsicTablePreview from "./AsicTablePreview";
import { useMinerHosting } from "@/protoOS/api";
import { type Aggregates, type AsicStats } from "@/protoOS/api/types";
import Stats from "@/protoOS/features/kpis/components/Stats";
import { type HbTemperature } from "@/protoOS/features/kpis/hooks";
import { type StatProps } from "@/shared/components/Stat";
import {
  TEMP_UNITS,
  type TemperatureUnits,
  usePreferences,
} from "@/shared/features/preferences/";
import { convertCtoF } from "@/shared/utils/utility";

type HbTempPreviewProps = {
  hbData: HbTemperature;
  asics?: AsicStats[];
};

const getStats = (stats: Aggregates, units: TemperatureUnits): StatProps[] => {
  const { avg, max, min } = stats;
  const isFahrenheit = units === TEMP_UNITS.fahrenheit;
  const unitLabel = isFahrenheit ? "ºF" : "ºC";

  return [
    {
      label: "Average",
      value: isFahrenheit && avg ? convertCtoF(avg) : avg,
      units: unitLabel,
      size: "small",
    },
    {
      label: "Highest",
      value: isFahrenheit && max ? convertCtoF(max) : max,
      units: unitLabel,
      size: "small",
    },
    {
      label: "Lowest",
      value: isFahrenheit && min ? convertCtoF(min) : min,
      units: unitLabel,
      size: "small",
    },
  ];
};

const HbTempPreview = ({ hbData, asics }: HbTempPreviewProps) => {
  const [isOverheating, setIsOverheating] = useState<boolean>(false);
  const { minerRoot } = useMinerHosting();
  const { temperatureUnits } = usePreferences();

  useEffect(() => {
    if (!hbData.data || !hbData.data.length) return;

    const lastTemp = hbData.data[hbData.data.length - 1].value || 0;
    setIsOverheating(lastTemp > criticalTemp);
  }, [hbData]);

  return (
    <Link
      data-testid="hb-temp-preview"
      to={`${minerRoot}/temperature/${hbData.serial}`}
      className={clsx(
        "group block w-[calc(50%-theme(spacing.6)/2)] overflow-hidden rounded-2xl border border-border-5 phone:w-full",
        isOverheating
          ? "hover:bg-intent-critical-20"
          : "hover:bg-core-primary-2",
      )}
    >
      <div
        className={clsx(
          "relative flex justify-between px-4 py-2",
          isOverheating
            ? "bg-intent-critical-20 group-hover:bg-transparent"
            : "bg-core-primary-2 group-hover:bg-transparent",
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
        {isOverheating && (
          <div className="text-emphasis-300 text-intent-critical-text opacity-50">
            Overheating
          </div>
        )}
      </div>

      <div className="p-4">
        <Stats
          stats={getStats(hbData.aggregates, temperatureUnits)}
          size="small"
          grid="grid-cols-3"
          gap="gap-2"
          padding="pb-4"
        />
        <AsicTablePreview asics={asics} />
      </div>
    </Link>
  );
};

export default HbTempPreview;
