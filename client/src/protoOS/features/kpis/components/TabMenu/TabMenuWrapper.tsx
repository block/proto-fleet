import { memo, useMemo } from "react";
import { convertAndFormatMeasurement, useMiner } from "@/protoOS/store";
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
        value: convertAndFormatMeasurement(
          miner?.hashrate?.latest,
          "TH/S",
          false,
        ),
        units: "TH/S",
        path: "/hashrate",
      },
      efficiency: {
        name: "Efficiency",
        value: convertAndFormatMeasurement(
          miner?.efficiency?.latest,
          "J/TH",
          false,
        ),
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
        value: convertAndFormatMeasurement(
          miner?.temperature?.latest,
          isFahrenheit ? "F" : "C",
          false,
        ),
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
