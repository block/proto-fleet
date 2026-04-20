import { memo, useMemo } from "react";
import { convertAndFormatMeasurement, convertValueUnits, useMiner, useTemperatureUnit } from "@/protoOS/store";
import TabMenu from "@/shared/components/TabMenu";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type TabMenuWrapperProps = {
  basePath?: string; // Optional base path for navigation
};

const TabMenuWrapper = memo(({ basePath }: TabMenuWrapperProps) => {
  const temperatureUnit = useTemperatureUnit();
  const miner = useMiner();

  const tabItems = useMemo(
    () => ({
      hashrate: {
        name: "Hashrate",
        value: convertAndFormatMeasurement(miner?.hashrate?.latest, "TH/S", false),
        units: "TH/S",
        path: "/hashrate",
      },
      efficiency: {
        name: "Efficiency",
        value: convertAndFormatMeasurement(miner?.efficiency?.latest, "J/TH", false),
        units: "J/TH",
        path: "/efficiency",
      },
      powerUsage: {
        name: "Power Usage",
        value: convertAndFormatMeasurement(miner?.power?.latest, "kW", false),
        units: "kW",
        path: "/power-usage",
      },
      temperature: {
        name: "Temperature",
        value: (() => {
          const latest = miner?.temperature?.latest;
          if (!latest) return undefined;
          if (latest.value === null) return "N/A";
          const converted = convertValueUnits(latest, temperatureUnit);
          return converted?.value === null || converted?.value === undefined ? "N/A" : getDisplayValue(converted.value);
        })(),
        units:
          miner?.temperature?.latest?.value === null
            ? undefined
            : miner?.temperature
              ? "°" + temperatureUnit
              : undefined,
        path: "/temperature",
      },
    }),
    [miner, temperatureUnit],
  );

  return <TabMenu items={tabItems} basePath={basePath} />;
});

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
