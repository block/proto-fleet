import { useEffect, useState } from "react";

import { useHashboards, useHashrate } from "api";
import { HashrateResponseHashratedata } from "apiTypes";

import { mockHashrateData } from "./constants";
import Hashrate from "./Hashrate";
import { Hashrates } from "./types";
import { convertAggregateValues, convertHashrateValues } from "./utility";

interface HashrateProps {
  duration: HashrateResponseHashratedata["duration"];
}

const HashrateWrapper = ({ duration }: HashrateProps) => {
  const { data: hashrateData } = useHashrate({ duration, poll: true });
  const { data: hashboardsInfo } = useHashboards();
  const [aggregates, setAggregates] = useState<
    HashrateResponseHashratedata["aggregates"]
  >({});
  const [hashrates, setHashrates] = useState<Hashrates>([]);

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
      {hashboardsInfo ? (
        <Hashrate
          aggregates={aggregates}
          duration={duration}
          hashrates={hashrates}
          hashboardSerials={
            hashboardsInfo
              ?.map((hashboards) => hashboards.hb_sn)
              .filter(Boolean) as string[]
          }
        />
      ) : null}
    </>
  );
};

export default HashrateWrapper;
