import { useMemo } from "react";
import {
  dangerInactivePercentage,
  dangerOfflinePercentage,
} from "@/protoFleet/features/kpis/constants";
import { type StatProps } from "@/shared/components/Stat";
import { chartStatus } from "@/shared/components/Stat/constants";
import Stats from "@/shared/components/Stats";
import { getDisplayValue } from "@/shared/utils/stringUtils";

interface MinersStatusProps {
  fleetSize: number;
  activeMiners: number;
  inactiveMiners: number;
  offlineMiners: number;
}

const MinersStatusMiners = ({
  fleetSize,
  activeMiners,
  inactiveMiners,
  offlineMiners,
}: MinersStatusProps) => {
  const minerStats = useMemo(() => {
    const activePercentage = (activeMiners / (fleetSize || 1)) * 100;
    const inactivePercentage = (inactiveMiners / (fleetSize || 1)) * 100;
    const offlinePercentage = (offlineMiners / (fleetSize || 1)) * 100;

    return [
      {
        value: activeMiners + " active miners",
        text: getDisplayValue(activePercentage) + "% of fleet",
        chartPercentage: activePercentage,
        chartStatus:
          activePercentage >
          100 - (dangerInactivePercentage + dangerOfflinePercentage) / 2
            ? chartStatus.success
            : activePercentage >
                100 - (dangerInactivePercentage + dangerOfflinePercentage)
              ? chartStatus.warning
              : chartStatus.critical,
      },
      {
        value: inactiveMiners + " inactive miners",
        text: getDisplayValue(inactivePercentage) + "% of fleet",
        chartPercentage: inactivePercentage,
        chartStatus:
          inactivePercentage == 0
            ? chartStatus.success
            : inactivePercentage < dangerInactivePercentage
              ? chartStatus.warning
              : chartStatus.critical,
      },
      {
        value: offlineMiners + " offline miners",
        text: getDisplayValue(offlinePercentage) + "% of fleet",
        chartPercentage: offlinePercentage,
        chartStatus:
          offlinePercentage == 0
            ? chartStatus.success
            : offlinePercentage < dangerInactivePercentage
              ? chartStatus.warning
              : chartStatus.critical,
      },
    ] as StatProps[];
  }, [fleetSize, activeMiners, inactiveMiners, offlineMiners]);

  return (
    <Stats
      size="small"
      grid="grid-cols-3 phone:grid-cols-2"
      gap="gap-x-6 phone:gap-x-4 phone:gap-y-4"
      stats={minerStats}
    />
  );
};

export default MinersStatusMiners;
