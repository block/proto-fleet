import { memo, useMemo } from "react";
import TabMenu from "@/shared/components/TabMenu";
import { usePreferences } from "@/shared/features/preferences";
import { TEMP_UNITS } from "@/shared/features/preferences/constants";
import { convertCtoF, formatHashrateWithUnit } from "@/shared/utils/utility";

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
    const { value: hashrateValue, unit: hashUnit } = formatHashrateWithUnit(
      hashrate ?? 0,
    );

    const tabItems = useMemo(
      () => ({
        hashrate: {
          name: "Hashrate",
          value: hashrateValue,
          units: hashUnit,
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

        // TODO: switch to uptime when we are able to support it
        temperature: {
          name: "Temperature",
          value:
            temperatureUnits === TEMP_UNITS.fahrenheit
              ? convertCtoF(temperature ?? 0)
              : temperature,
          units: temperatureUnits === TEMP_UNITS.fahrenheit ? "°F" : "°C",
          path: "/temperature",
        },
      }),
      [
        hashrateValue,
        efficiency,
        powerUsage,
        temperature,
        hashUnit,
        temperatureUnits,
      ],
    );

    return <TabMenu items={tabItems} basePath={basePath} />;
  },
);

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
