import { useMemo } from "react";
import { type Measurement } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import { useMinerHashrate } from "@/protoFleet/store";
import HashRateValue from "@/shared/components/HashRateValue";
import SkeletonBar from "@/shared/components/SkeletonBar";
import Sparkline from "@/shared/components/Sparkline";
import { getLatestMeasurementWithData } from "@/shared/utils/measurementUtils";

type HashrateProps = {
  deviceIdentifier?: string;
  hashrate?: Measurement[];
};

const Hashrate = ({ deviceIdentifier, hashrate: hashrateProps }: HashrateProps) => {
  const hashrateFromStore = useMinerHashrate(deviceIdentifier || "");
  const hashrate = hashrateProps || hashrateFromStore;

  const latestMeasurement = useMemo(() => getLatestMeasurementWithData(hashrate), [hashrate]);

  const latestValue = latestMeasurement?.value;

  if (hashrate === undefined) return "N/A";

  return (
    <div className="relative inline-flex h-full w-full flex-row items-center gap-2 pr-6 whitespace-nowrap">
      {hashrate ? (
        <div>
          <HashRateValue value={latestValue} />
        </div>
      ) : (
        <SkeletonBar className="w-full" />
      )}
      <div className="h-5 w-10">
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
