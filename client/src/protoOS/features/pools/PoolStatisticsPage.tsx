import { useState } from "react";
import PoolStatisticsView from "./PoolStatisticsView";
import { type PoolDuration, usePoolStatistics } from "./usePoolStatistics";
import { navigationItems } from "@/protoOS/components/NavigationMenu/constants";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

export default function PoolStatisticsPage() {
  const [duration, setDuration] = useState<PoolDuration>("24h");
  const { minerRoot } = useMinerHosting();
  const state = usePoolStatistics(duration);
  return (
    <PoolStatisticsView
      {...state}
      duration={duration}
      onDurationChange={setDuration}
      configureHref={`${minerRoot}/${navigationItems.miningPools}`}
    />
  );
}
