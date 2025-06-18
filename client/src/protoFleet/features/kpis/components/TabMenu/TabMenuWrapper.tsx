import { memo, useMemo } from "react";
import TabMenu from "@/shared/components/TabMenu";

type TabMenuWrapperProps = {
  hashrate?: number;
  efficiency?: number;
  powerUsage?: number;
  uptime?: number;
  basePath?: string; // Optional base path for navigation
};

const TabMenuWrapper = memo(
  ({
    hashrate,
    efficiency,
    powerUsage,
    uptime,
    basePath = "",
  }: TabMenuWrapperProps) => {
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
        uptime: {
          name: "Uptime",
          value: uptime,
          units: "%",
          path: "/uptime",
        },
      }),
      [hashrate, efficiency, powerUsage, uptime],
    );

    return <TabMenu items={tabItems} basePath={basePath} />;
  },
);

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
