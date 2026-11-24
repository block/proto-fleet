import { memo, useMemo } from "react";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useKpiViewSelection } from "@/protoOS/features/kpis/kpiViewSelection";
import {
  convertAndFormatMeasurement,
  convertValueUnits,
  temperatureDisplayUnit,
  useMiner,
  useTemperatureUnit,
} from "@/protoOS/store";
import TabMenu from "@/shared/components/TabMenu";
import { getDisplayValue } from "@/shared/utils/stringUtils";

const TabMenuWrapper = memo(() => {
  const temperatureUnit = useTemperatureUnit();
  const selection = useKpiViewSelection();
  const miner = useMiner(selection);
  // Prefix minerRoot so KPI tab navigation stays inside the embedded miner view
  // when fleet-hosted (minerRoot is "" in standalone protoOS). Without it the
  // absolute paths (e.g. "/hashrate") escape the embed and ProtoFleet's
  // "/:siteScope" route treats the tab segment as an unknown site, redirecting
  // back to the dashboard.
  const { minerRoot } = useMinerHosting();
  const activePathAliases = useMemo(
    () => ({
      hashrate: [minerRoot || "/"],
    }),
    [minerRoot],
  );

  const tabItems = useMemo(
    () => ({
      hashrate: {
        name: "Hashrate",
        value: convertAndFormatMeasurement(miner?.hashrate?.latest, "TH/s", false),
        units: "TH/s",
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
        value: (() => {
          const latest = miner?.temperature?.latest;
          if (!latest) return undefined;
          if (latest.value === null) return "N/A";
          const converted = convertValueUnits(latest, temperatureDisplayUnit(temperatureUnit));
          return converted?.value === null || converted?.value === undefined ? "N/A" : getDisplayValue(converted.value);
        })(),
        units:
          miner?.temperature?.latest?.value === null
            ? undefined
            : miner?.temperature
              ? temperatureDisplayUnit(temperatureUnit)
              : undefined,
        path: "/temperature",
      },
    }),
    [miner, temperatureUnit],
  );

  return <TabMenu items={tabItems} activePathAliases={activePathAliases} basePath={minerRoot} />;
});

TabMenuWrapper.displayName = "TabMenuWrapper";

export default TabMenuWrapper;
