import { memo, useMemo } from "react";
import TabMenu from "./TabMenu";

type TabMenuWrapperProps = {
  hashrate?: number;
  efficiency?: number;
  powerUsage?: number;
  temperature?: number;
};

// Use memo to prevent unnecessary re-renders of the TabMenuWrapper
const TabMenuWrapper = memo(
  ({ hashrate, efficiency, powerUsage, temperature }: TabMenuWrapperProps) => {
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
          value: temperature,
          units: "ºC",
          path: "/temperature",
        },
      }),
      [hashrate, efficiency, powerUsage, temperature],
    );

    return <TabMenu items={tabItems} />;
  },
);

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
