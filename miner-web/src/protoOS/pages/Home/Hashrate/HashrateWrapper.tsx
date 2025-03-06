import { useEffect, useState } from "react";

import Hashrate from "./Hashrate";
import { Hashrates } from "./types";
import {
  aggregateHashrateValues,
  convertAggregateValues,
  convertHashrateValues,
} from "./utility";
import { useHashrate } from "@/protoOS/api";
import { HashrateResponseHashratedata } from "@/protoOS/api/types";
import { Duration } from "@/shared/components/DurationSelector";
import Spinner from "@/shared/components/Spinner";

interface HashrateProps {
  duration: Duration;
  hashboardSerials?: string[];
}

const HashrateWrapper = ({ duration, hashboardSerials }: HashrateProps) => {
  const { data: hashrateData, pending: pendingHashrateData } = useHashrate({
    duration,
    poll: true,
  });
  const [aggregates, setAggregates] = useState<
    HashrateResponseHashratedata["aggregates"]
  >({});
  const [hashrates, setHashrates] = useState<Hashrates>([]);

  useEffect(() => {
    setHashrates([]);
    setAggregates({});
  }, [duration]);

  useEffect(() => {
    if (
      !pendingHashrateData &&
      hashrateData?.data?.length &&
      hashrateData.duration === duration
    ) {
      const aggregatedHashrateValues = aggregateHashrateValues(
        hashrateData.data,
        duration,
      );
      setHashrates(convertHashrateValues(aggregatedHashrateValues));
      setAggregates(convertAggregateValues(hashrateData.aggregates));
    }
  }, [duration, hashrateData, pendingHashrateData]);

  return (
    <>
      {hashboardSerials ? (
        <Hashrate
          aggregates={aggregates}
          duration={duration}
          hashrates={hashrates}
          hashboardSerials={hashboardSerials.sort((a, b) => a.localeCompare(b))}
        />
      ) : (
        <div className="flex h-full items-center justify-center">
          <Spinner />
        </div>
      )}
    </>
  );
};

export default HashrateWrapper;
