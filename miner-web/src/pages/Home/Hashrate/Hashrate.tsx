import { useEffect, useState } from "react";

import { useHashboardHashrate } from "api";
import { HashrateResponseHashratedata } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import { Duration } from "components/DurationSelector";
import InfoWidget from "components/InfoWidget";
import Spinner from "components/Spinner";

import HashrateChart from "./HashrateChart";
import { Hashrates } from "./types";
import { convertHashrateValues } from "./utility";

interface HashrateProps {
  aggregates: HashrateResponseHashratedata["aggregates"];
  duration: Duration;
  hashrates: Hashrates;
  hashboardSerials: string[];
  isAggregatingHashrates: boolean;
}

const Hashrate = ({
  aggregates,
  duration,
  hashrates,
  hashboardSerials,
  isAggregatingHashrates,
}: HashrateProps) => {
  const [hashrate1, setHashrate1] = useState<Hashrates>([]);
  const [hashrate2, setHashrate2] = useState<Hashrates>([]);
  const [hashrate3, setHashrate3] = useState<Hashrates>([]);

  const { data: hashrate1Data, pending: pendingHashrate1Data } =
    useHashboardHashrate({
      duration,
      hashboardSerial: hashboardSerials[0],
      poll: true,
    });
  const { data: hashrate2Data, pending: pendingHashrate2Data } =
    useHashboardHashrate({
      duration,
      hashboardSerial: hashboardSerials[1],
      poll: true,
    });
  const { data: hashrate3Data, pending: pendingHashrate3Data } =
    useHashboardHashrate({
      duration,
      hashboardSerial: hashboardSerials[2],
      poll: true,
    });

  useEffect(() => {
    setHashrate1([]);
    setHashrate2([]);
    setHashrate3([]);
  }, [duration, isAggregatingHashrates]);

  useEffect(() => {
    if (
      !isAggregatingHashrates &&
      !pendingHashrate1Data &&
      hashrate1Data?.data?.length &&
      hashrate1Data.duration === duration
    ) {
      setHashrate1(convertHashrateValues(hashrate1Data.data));
    }
  }, [duration, hashrate1Data, isAggregatingHashrates, pendingHashrate1Data]);

  useEffect(() => {
    if (
      !isAggregatingHashrates &&
      !pendingHashrate2Data &&
      hashrate2Data?.data?.length &&
      hashrate2Data.duration === duration
    ) {
      setHashrate2(convertHashrateValues(hashrate2Data.data));
    }
  }, [duration, hashrate2Data, isAggregatingHashrates, pendingHashrate2Data]);

  useEffect(() => {
    if (
      !isAggregatingHashrates &&
      !pendingHashrate3Data &&
      hashrate3Data?.data?.length &&
      hashrate3Data.duration === duration
    ) {
      setHashrate3(convertHashrateValues(hashrate3Data.data));
    }
  }, [duration, hashrate3Data, isAggregatingHashrates, pendingHashrate3Data]);

  const currentValue = getDisplayValue(
    hashrates?.[hashrates.length - 1]?.value
  );
  const averageValue = getDisplayValue(aggregates?.avg);
  const lowestValue = getDisplayValue(aggregates?.min);
  const highestValue = getDisplayValue(aggregates?.max);

  return (
    <div className="space-y-6">
      <div className="flex space-x-6 w-full phone:flex-col phone:space-x-0 phone:space-y-6">
        <InfoWidget
          title="Current hashrate"
          value={currentValue && `${currentValue} TH/s`}
          loading={false}
          wrapperClassName="w-full tablet:w-32"
        />
        <InfoWidget
          title="Average"
          value={averageValue && `${averageValue} TH/s`}
          loading={false}
          wrapperClassName="w-full tablet:w-32"
        />
        <InfoWidget
          title="Lowest"
          value={lowestValue && `${lowestValue} TH/s`}
          loading={false}
          wrapperClassName="w-full tablet:w-32"
        />
        <InfoWidget
          title="Highest"
          value={highestValue && `${highestValue} TH/s`}
          loading={false}
          wrapperClassName="w-full tablet:w-32"
        />
      </div>

      <div className="h-[400px]">
        {hashrates.length ? (
          <HashrateChart
            duration={duration}
            // TODO: aggregate individual hashrates when API fixes timestamp mismatch issue
            hashrate1={hashrate1}
            hashrate2={hashrate2}
            hashrate3={hashrate3}
            hashrates={hashrates}
            highestValue={highestValue}
          />
        ) : (
          <div className="flex justify-center items-center h-full">
            <Spinner />
          </div>
        )}
      </div>
    </div>
  );
};

export default Hashrate;
