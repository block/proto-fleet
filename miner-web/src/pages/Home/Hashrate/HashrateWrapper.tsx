import { useEffect, useState } from "react";

import { useHashrate } from "api";
import { HashrateResponseHashratedata } from "apiTypes";

import Spinner from "components/Spinner";

import { mockHashrateData } from "./constants";
import Hashrate from "./Hashrate";
import { Hashrates } from "./types";
import { convertAggregateValues, convertHashrateValues } from "./utility";

interface HashrateProps {
  duration: HashrateResponseHashratedata["duration"];
  hashboardSerials?: string[];
}

const HashrateWrapper = ({ duration, hashboardSerials }: HashrateProps) => {
  const { data: hashrateData } = useHashrate({ duration, poll: true });
  const [aggregates, setAggregates] = useState<
    HashrateResponseHashratedata["aggregates"]
  >({});
  const [hashrates, setHashrates] = useState<Hashrates>([]);

  useEffect(() => {
    setHashrates([]);
    setAggregates({});
  }, [duration]);

  useEffect(() => {
    if (hashrateData?.data && hashrateData.data.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = hashrateData.data[0].datetime
        ? hashrateData
        : mockHashrateData;
      setAggregates(convertAggregateValues(apiData.aggregates));
      setHashrates(convertHashrateValues(apiData.data));
    }
  }, [hashrateData]);

  return (
    <>
      {hashboardSerials ? (
        <Hashrate
          aggregates={aggregates}
          duration={duration}
          hashrates={hashrates}
          hashboardSerials={hashboardSerials}
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
