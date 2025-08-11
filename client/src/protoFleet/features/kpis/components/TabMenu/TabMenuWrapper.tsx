import { memo, useMemo } from "react";
import TabMenu from "@/shared/components/TabMenu";
import { formatHashrateWithUnit } from "@/shared/utils/utility";

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
        uptime: {
          name: "Uptime",
          value: uptime,
          units: "%",
          path: "/uptime",
        },
      }),
      [hashrateValue, efficiency, powerUsage, uptime, hashUnit],
    );

    return <TabMenu items={tabItems} basePath={basePath} />;
  },
);

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
