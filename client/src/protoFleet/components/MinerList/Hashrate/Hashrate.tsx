import { type Miner } from "../types";

import SkeletonBar from "@/shared/components/SkeletonBar";
import Sparkline from "@/shared/components/Sparkline";

type HashrateProps = {
  hashrate?: Miner["hashrate"];
};

const Hashrate = ({ hashrate }: HashrateProps) => {
  return (
    <div className="relative flex h-full w-full flex-row items-center justify-between pr-6 whitespace-nowrap">
      {hashrate ? (
        <div>hashrate[hashrate.length - 1].hashrate TH/s</div>
      ) : (
        <SkeletonBar className="w-full" />
      )}
      <div className="h-5 w-12">
        {hashrate && hashrate.length && (
          <Sparkline
            data={hashrate.map((h) => ({ time: h.time, y: h.hashrate }))}
            threshold={20}
          />
        )}
      </div>
    </div>
  );
};

export default Hashrate;
