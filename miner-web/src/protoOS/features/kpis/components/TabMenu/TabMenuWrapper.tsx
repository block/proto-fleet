import { memo, useMemo } from "react";
import TabMenu from "./TabMenu";
import { TEMP_UNITS, usePreferences } from "@/shared/features/preferences";
import { convertCtoF } from "@/shared/utils/utility";

type TabMenuWrapperProps = {
  hashrate?: number;
  efficiency?: number;
  powerUsage?: number;
  temperature?: number;
};

const TabMenuWrapper = memo(
  ({ hashrate, efficiency, powerUsage, temperature }: TabMenuWrapperProps) => {
    const { temperatureUnits } = usePreferences();
    const isFahrenheit = temperatureUnits === TEMP_UNITS.fahrenheit;

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
          units: "kw/H",
          path: "/power-usage",
        },
        temperature: {
          name: "Temperature",
          value:
            isFahrenheit && temperature
              ? convertCtoF(temperature)
              : temperature,
          units: isFahrenheit ? "ºF" : "ºC",
          path: "/temperature",
        },
      }),
      [hashrate, efficiency, powerUsage, temperature, isFahrenheit],
    );

    return <TabMenu items={tabItems} />;
  },
);

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
