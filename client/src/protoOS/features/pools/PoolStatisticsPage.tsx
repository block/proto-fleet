import { useState } from "react";
import PoolConfigurationStatisticsPage from "./PoolConfigurationStatisticsPage";
import PoolStatisticsView from "./PoolStatisticsView";
import { type PoolDuration, usePoolStatistics } from "./usePoolStatistics";
import { navigationItems } from "@/protoOS/components/NavigationMenu/constants";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useDeviceType } from "@/protoOS/store";

function LegacyPoolStatisticsPage() {
  const isContainer = useDeviceType() === "container";
  const [duration, setDuration] = useState<PoolDuration>("24h");
  const { minerRoot } = useMinerHosting();
  const state = usePoolStatistics(duration);
  return (
    <PoolStatisticsView
      {...state}
      usernameLabel={isContainer ? "Username" : "Worker"}
      duration={duration}
      onDurationChange={setDuration}
      configureHref={`${minerRoot}/${navigationItems.miningPools}`}
    />
  );
}

export default function PoolStatisticsPage() {
  const { adapter, isFleetHosted } = useMinerHosting();
  return adapter?.poolStatistics && !isFleetHosted ? (
    <PoolConfigurationStatisticsPage service={adapter.poolStatistics} legacy={<LegacyPoolStatisticsPage />} />
  ) : (
    <LegacyPoolStatisticsPage />
  );
}
