import { useMemo } from "react";
import { type Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import { useMinerHashrate } from "@/protoFleet/features/fleetManagement/store/useFleetStore";

import SkeletonBar from "@/shared/components/SkeletonBar";
import Sparkline from "@/shared/components/Sparkline";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type HashrateProps = {
  deviceIdentifier?: string;
  hashrate?: Measurement[];
};

const Hashrate = ({
  deviceIdentifier,
  hashrate: hashrateProps,
}: HashrateProps) => {
  const hashrateFromStore = useMinerHashrate(deviceIdentifier || "");
  const hashrate = hashrateProps || hashrateFromStore;

  const latestMeasurement = useMemo(
    () => getLatestMeasurementWithData(hashrate),
    [hashrate],
  );

  const value = latestMeasurement?.value ?? 0;
  return (
    <div className="relative flex h-full w-full flex-row items-center justify-between pr-6 whitespace-nowrap">
      {hashrate ? (
        <div>{getDisplayValue(value)} TH/s</div>
      ) : (
        <SkeletonBar className="w-full" />
      )}
      <div className="h-5 w-14 pl-2">
        {hashrate && hashrate.length ? (
          <Sparkline
            data={hashrate
              .filter((h) => h.timestamp !== undefined)
              .map((h) => ({
                time: Number(h.timestamp?.seconds),
                y: h.value,
              }))}
            threshold={20}
          />
        ) : null}
      </div>
    </div>
  );
};

export default Hashrate;
