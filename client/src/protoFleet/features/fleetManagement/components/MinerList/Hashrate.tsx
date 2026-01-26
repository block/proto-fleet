import { INACTIVE_PLACEHOLDER } from "./constants";
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
  const hashrate = hashrateProps ?? hashrateFromStore;

  // undefined = telemetry not loaded yet (show skeleton)
  if (hashrate === undefined) {
    return (
      <div className="relative inline-flex h-full w-full flex-row items-center gap-2 pr-6 whitespace-nowrap">
        <SkeletonBar className="w-full" />
        <div className="h-5 w-10" />
      </div>
    );
  }

  // null = miner is inactive/offline (show placeholder)
  if (hashrate === null) {
    return <>{INACTIVE_PLACEHOLDER}</>;
  }

  // Empty array = empty cell for pool/auth required miners
  if (hashrate.length === 0) {
    return null;
  }

  const latestValue = getLatestMeasurementWithData(hashrate)?.value;

  return (
    <div className="relative inline-flex h-full w-full flex-row items-center gap-2 pr-6 whitespace-nowrap">
      <div>
        <HashRateValue value={latestValue} />
      </div>
      <div className="h-5 w-10">
        {hashrate.length > 0 && (
          <Sparkline
            data={hashrate
              .filter((h) => h.timestamp !== undefined)
              .map((h) => ({
                time: Number(h.timestamp?.seconds),
                y: h.value,
              }))}
            threshold={20}
          />
        )}
      </div>
    </div>
  );
};

export default Hashrate;
