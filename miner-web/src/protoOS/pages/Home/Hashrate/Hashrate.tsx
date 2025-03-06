import { useEffect, useState } from "react";

import HashrateChart from "./HashrateChart";
import { Hashrates } from "./types";
import { aggregateHashrateValues, convertHashrateValues } from "./utility";
import { useHashboardHashrate } from "@/protoOS/api";
import { HashrateResponseHashratedata } from "@/protoOS/api/types";

import InfoWidget from "@/protoOS/components/InfoWidget";
import { Duration } from "@/shared/components/DurationSelector";
import Spinner from "@/shared/components/Spinner";
import { getDisplayValue } from "@/shared/utils/stringUtils";

interface HashrateProps {
  aggregates: HashrateResponseHashratedata["aggregates"];
  duration: Duration;
  hashrates: Hashrates;
  hashboardSerials: string[];
}

const Hashrate = ({
  aggregates,
  duration,
  hashrates,
  hashboardSerials,
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
  }, [duration]);

  useEffect(() => {
    if (
      !pendingHashrate1Data &&
      hashrate1Data?.data?.length &&
      hashrate1Data.duration === duration
    ) {
      const aggregatedHashrateValues = aggregateHashrateValues(
        hashrate1Data.data,
        duration,
      );
      setHashrate1(convertHashrateValues(aggregatedHashrateValues));
    }
  }, [duration, hashrate1Data, pendingHashrate1Data]);

  useEffect(() => {
    if (
      !pendingHashrate2Data &&
      hashrate2Data?.data?.length &&
      hashrate2Data.duration === duration
    ) {
      const aggregatedHashrateValues = aggregateHashrateValues(
        hashrate2Data.data,
        duration,
      );
      setHashrate2(convertHashrateValues(aggregatedHashrateValues));
    }
  }, [duration, hashrate2Data, pendingHashrate2Data]);

  useEffect(() => {
    if (
      !pendingHashrate3Data &&
      hashrate3Data?.data?.length &&
      hashrate3Data.duration === duration
    ) {
      const aggregatedHashrateValues = aggregateHashrateValues(
        hashrate3Data.data,
        duration,
      );
      setHashrate3(convertHashrateValues(aggregatedHashrateValues));
    }
  }, [duration, hashrate3Data, pendingHashrate3Data]);

  const currentValue = getDisplayValue(
    hashrates?.[hashrates.length - 1]?.value,
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
          title={`${duration.toUpperCase()} Average`}
          value={averageValue && `${averageValue} TH/s`}
          loading={false}
          wrapperClassName="w-full tablet:w-32"
        />
        <InfoWidget
          title={`${duration.toUpperCase()} Lowest`}
          value={lowestValue && `${lowestValue} TH/s`}
          loading={false}
          wrapperClassName="w-full tablet:w-32"
        />
        <InfoWidget
          title={`${duration.toUpperCase()} Highest`}
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
