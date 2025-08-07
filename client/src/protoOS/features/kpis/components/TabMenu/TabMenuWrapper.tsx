import { memo, useMemo } from "react";
import TabMenu from "@/shared/components/TabMenu";
import { TEMP_UNITS, usePreferences } from "@/shared/features/preferences";
import { getAsicTempValue } from "@/shared/utils/utility";

type TabMenuWrapperProps = {
  hashrate?: number;
  efficiency?: number;
  powerUsage?: number;
  temperature?: number;
  basePath?: string; // Optional base path for navigation
};

const TabMenuWrapper = memo(
  ({
    hashrate,
    efficiency,
    powerUsage,
    temperature,
    basePath = "",
  }: TabMenuWrapperProps) => {
    const { temperatureUnits } = usePreferences();
    const isFahrenheit = temperatureUnits === TEMP_UNITS.fahrenheit;
    const unit = isFahrenheit ? "ºF" : "ºC";
    const tabItems = useMemo(
      () => ({
        hashrate: {
          name: "Hashrate",
          value: hashrate,
          units: "TH/S",
          path: "/hashrate",
        },
        efficiency: {
          name: "Efficiency",
          value: efficiency,
          units: "J/TH",
          path: "/efficiency",
        },
        powerUsage: {
          name: "Power Usage",
          value: powerUsage,
          units: "kW",
          path: "/power-usage",
        },
        temperature: {
          name: "Temperature",
          value: getAsicTempValue(temperature, isFahrenheit),
          units: temperature ? unit : undefined,
          path: "/temperature",
        },
      }),
      [hashrate, efficiency, powerUsage, temperature, isFahrenheit, unit],
    );

    return <TabMenu items={tabItems} basePath={basePath} />;
  },
);

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
