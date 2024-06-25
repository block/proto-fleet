import { useEffect, useState } from "react";

import { useHashrate } from "api";
import { HashrateResponseHashratedata } from "apiTypes";

import { Duration } from "components/DurationSelector";
import Spinner from "components/Spinner";

import Hashrate from "./Hashrate";
import { Hashrates } from "./types";
import {
  aggregateHashrateValues,
  convertAggregateValues,
  convertHashrateValues,
  shouldAggregateHashrateValues,
} from "./utility";

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
  const [isAggregatingHashrates, setIsAggregatingHashrates] = useState(false);

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
        duration
      );
      setHashrates(convertHashrateValues(aggregatedHashrateValues));
      setAggregates(convertAggregateValues(hashrateData.aggregates));
      setIsAggregatingHashrates(
        shouldAggregateHashrateValues(hashrateData.data, duration)
      );
    }
  }, [duration, hashrateData, pendingHashrateData]);

  return (
    <>
      {hashboardSerials ? (
        <Hashrate
          aggregates={aggregates}
          duration={duration}
          hashrates={hashrates}
          hashboardSerials={hashboardSerials}
          isAggregatingHashrates={isAggregatingHashrates}
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
