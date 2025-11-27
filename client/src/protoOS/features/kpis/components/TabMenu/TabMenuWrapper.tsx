import { memo, useMemo } from "react";
import { convertAndFormatMeasurement, useMiner, useTemperatureUnit } from "@/protoOS/store";
import TabMenu from "@/shared/components/TabMenu";

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
        value: convertAndFormatMeasurement(miner?.temperature?.latest, temperatureUnit, false),
        units: miner?.temperature ? temperatureUnit : undefined,
        path: "/temperature",
      },
    }),
    [miner, temperatureUnit],
  );

  return <TabMenu items={tabItems} basePath={basePath} />;
});

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
