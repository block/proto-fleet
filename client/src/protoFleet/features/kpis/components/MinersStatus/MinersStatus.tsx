import { useMemo } from "react";
import {
  dangerInactivePercentage,
  dangerOfflinePercentage,
} from "@/protoFleet/features/kpis/constants";
import { type StatProps } from "@/shared/components/Stat";
import { chartStatus } from "@/shared/components/Stat/constants";
import Stats from "@/shared/features/kpis/components/Stats";
import { getDisplayValue } from "@/shared/utils/stringUtils";

interface MinersStatusProps {
  activeMiners: number;
  inactiveMiners: number;
  offlineMiners: number;
}

const MinersStatusMiners = ({
  activeMiners,
  inactiveMiners,
  offlineMiners,
}: MinersStatusProps) => {
  const minerStats = useMemo(() => {
    const fleetSize = activeMiners + inactiveMiners + offlineMiners;
    const inactivePercentage = (inactiveMiners / fleetSize) * 100;
    const offlinePercentage = (offlineMiners / fleetSize) * 100;
    // round to one decimal place so that displayed percentages add up to a 100%
    // toFixed returns string, so we would have to convert the result back to a number
    const activePercentage =
      100 -
      Math.round(inactivePercentage * 10) / 10 -
      Math.round(offlinePercentage * 10) / 10;

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
  }, [activeMiners, inactiveMiners, offlineMiners]);

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
