import { type Measurement } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

import SkeletonBar from "@/shared/components/SkeletonBar";
import Sparkline from "@/shared/components/Sparkline";
import { getDisplayValue } from "@/shared/utils/stringUtils";

type HashrateProps = {
  hashrate?: Measurement[];
};

const Hashrate = ({ hashrate }: HashrateProps) => {
  return (
    <div className="relative flex h-full w-full flex-row items-center justify-between pr-6 whitespace-nowrap">
      {hashrate ? (
        <div>{getDisplayValue(hashrate[hashrate.length - 1].value)} TH/s</div>
      ) : (
        <SkeletonBar className="w-full" />
      )}
      <div className="h-5 w-12">
        {hashrate && hashrate.length && (
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
