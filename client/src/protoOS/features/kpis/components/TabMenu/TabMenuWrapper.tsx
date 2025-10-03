import { memo, useMemo } from "react";
import { getCurrentValue, useMiner } from "@/protoOS/store";
import TabMenu from "@/shared/components/TabMenu";
import { TEMP_UNITS, usePreferences } from "@/shared/features/preferences";

type TabMenuWrapperProps = {
  basePath?: string; // Optional base path for navigation
};

const TabMenuWrapper = memo(({ basePath }: TabMenuWrapperProps) => {
  const { temperatureUnits } = usePreferences();
  const miner = useMiner();
  const isFahrenheit = temperatureUnits === TEMP_UNITS.fahrenheit;
  const unit = isFahrenheit ? "F" : "C";
  const tabItems = useMemo(
    () => ({
      hashrate: {
        name: "Hashrate",
        value: getCurrentValue(miner?.hashrate, "TH/S", false)?.formatted,
        units: "TH/S",
        path: "/hashrate",
      },
      efficiency: {
        name: "Efficiency",
        value: getCurrentValue(miner?.efficiency, "J/TH", false)?.formatted,
        units: "J/TH",
        path: "/efficiency",
      },
      powerUsage: {
        name: "Power Usage",
        value: getCurrentValue(miner?.power, "kW", false)?.formatted,
        units: "kW",
        path: "/power-usage",
      },
      temperature: {
        name: "Temperature",
        value: getCurrentValue(
          miner?.temperature,
          isFahrenheit ? "F" : "C",
          false,
        )?.formatted,
        units: miner?.temperature ? unit : undefined,
        path: "/temperature",
      },
    }),
    [miner, isFahrenheit, unit],
  );

  return <TabMenu items={tabItems} basePath={basePath} />;
});

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
