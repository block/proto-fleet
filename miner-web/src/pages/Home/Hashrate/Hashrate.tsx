import { useEffect, useState } from "react";

import { useHashboardHashrate } from "api";
import { HashrateResponseHashratedata } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import InfoWidget from "components/InfoWidget";

import { getMockHashrateData, mockHashrateData1 } from "./constants";
import HashrateChart from "./HashrateChart";
import { Hashrates } from "./types";
import { convertHashrateValues } from "./utility";

interface HashrateProps {
  aggregates: HashrateResponseHashratedata["aggregates"];
  duration: HashrateResponseHashratedata["duration"];
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

  const { data: hashrate1Data } = useHashboardHashrate({
    duration,
    hashboardSerial: hashboardSerials[0],
    poll: true,
  });
  const { data: hashrate2Data } = useHashboardHashrate({
    duration,
    hashboardSerial: hashboardSerials[1],
    poll: true,
  });
  const { data: hashrate3Data } = useHashboardHashrate({
    duration,
    hashboardSerial: hashboardSerials[2],
    poll: true,
  });

  useEffect(() => {
    if (hashrate1Data?.data && hashrate1Data.data.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = hashrate1Data.data[0].datetime
        ? hashrate1Data
        : mockHashrateData1;
      setHashrate1(convertHashrateValues(apiData.data));
    }
  }, [hashrate1Data, hashboardSerials]);

  useEffect(() => {
    if (hashrate2Data?.data && hashrate2Data.data.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = hashrate2Data.data[0].datetime
        ? hashrate2Data
        : getMockHashrateData(10, 15);
      setHashrate2(convertHashrateValues(apiData.data));
    }
  }, [hashrate2Data]);

  useEffect(() => {
    if (hashrate3Data?.data && hashrate3Data.data.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = hashrate3Data.data[0].datetime
        ? hashrate3Data
        : getMockHashrateData(20, 30);
      setHashrate3(convertHashrateValues(apiData.data));
    }
  }, [hashrate3Data]);

  const currentValue = getDisplayValue(hashrates?.[hashrates.length - 1]?.value);
  const averageValue = getDisplayValue(aggregates?.avg);
  const lowestValue = getDisplayValue(aggregates?.min);
  const highestValue = getDisplayValue(aggregates?.max);

  return (
    <div className="space-y-6">
      <div className="flex space-x-6 w-full phone:flex-col phone:space-x-0 phone:space-y-6">
        {/* TODO: display hashrate values once API is implemented */}
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
        {hashrates?.length ? (
          <HashrateChart
            hashrate1={hashrate1}
            hashrate2={hashrate2}
            hashrate3={hashrate3}
            hashrates={hashrates}
            highestValue={highestValue}
          />
        ) : null}
      </div>
    </div>
  );
};

export default Hashrate;
