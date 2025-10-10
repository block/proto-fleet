import { memo, useMemo } from "react";
import { useTemperatureUnit } from "@/protoFleet/features/fleetManagement/store/useFleetStore";
import TabMenu from "@/shared/components/TabMenu";
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
    const temperatureUnit = useTemperatureUnit();
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
            temperatureUnit === "F"
              ? convertCtoF(temperature ?? 0)
              : temperature,
          units: `°${temperatureUnit}`,
          path: "/temperature",
        },
      }),
      [
        hashrateValue,
        efficiency,
        powerUsage,
        temperature,
        hashUnit,
        temperatureUnit,
      ],
    );

    return <TabMenu items={tabItems} basePath={basePath} />;
  },
);

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
