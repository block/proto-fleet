import { useEffect, useState } from "react";

import { useCoolingStatus, useHashboards, useTemperature } from "api";

import DurationSelector, {
  Duration,
  durations,
} from "components/DurationSelector";
import FanSpeedWidget from "components/InfoWidget/FanSpeedWidget";
import TempWidget, {
  mockTemperatureData,
} from "components/InfoWidget/TempWidget";
import Row from "components/Row";
import Spinner from "components/Spinner";
import Tabs from "components/Tab";

import AsicTable from "./Asic/AsicTable";

const Hardware = () => {
  const [duration, setDuration] = useState<Duration>(durations[0]);
  const [showPopover, setShowPopover] = useState<string | undefined>(undefined);
  const [temp, setTemp] = useState<number>();
  const [highestTemp, setHighestTemp] = useState<number>();
  const [hashboardSerials, setHashboardSerials] = useState<string[]>();
  const { data: tempData, pending: pendingTempData } = useTemperature({
    duration,
    poll: true,
  });
  const { data: hashboardsInfo, pending: pendingHashboardsInfo } =
    useHashboards({ poll: true });
  const { data: coolingStatus, pending: pendingCoolingStatus } =
    useCoolingStatus({ poll: true });

  useEffect(() => {
    if (hashboardsInfo) {
      setHashboardSerials(
        hashboardsInfo
          ?.map((hashboardInfo) => hashboardInfo.hb_sn)
          .filter(Boolean) as string[]
      );
    }
  }, [hashboardsInfo]);

  useEffect(() => {
    if (tempData && tempData.data?.length) {
      // TODO: remove else when mocks moved to swagger
      const apiData = tempData.data[0].datetime
        ? tempData
        : mockTemperatureData;
      setHighestTemp(apiData.aggregates?.max);
      setTemp(apiData.data?.[apiData.data.length - 1].value);
    }
  }, [tempData]);

  return (
    <div className="flex flex-col space-y-6 h-full">
      <div className="flex items-center">
        <div className="text-heading-300 grow">Hardware</div>
        <DurationSelector className="h-fit" onSelect={setDuration} />
      </div>

      <div className="flex space-x-6 w-full phone:flex-col phone:space-x-0 phone:space-y-6">
        <TempWidget
          temp={temp}
          hashboardSerials={hashboardSerials}
          highestTemp={highestTemp}
          loading={pendingTempData && !temp}
        />
        <FanSpeedWidget
          fanSpeeds={coolingStatus?.fans}
          loading={pendingCoolingStatus && !coolingStatus?.fans?.length}
        />
      </div>
      {pendingHashboardsInfo && !hashboardsInfo?.length && (
        <div className="flex justify-center items-center h-full">
          <Spinner />
        </div>
      )}
      {hashboardsInfo?.length && (
        <Tabs>
          {hashboardsInfo.map((hashboardInfo, index) => (
            <Tabs.Tab
              label={`Hashboard ${index + 1}`}
              key={hashboardInfo.hb_sn}
            >
              <Row compact className="-mt-6 flex">
                <div className="text-emphasis-300 grow">
                  {hashboardInfo.hb_sn &&
                    `Board ending in ${hashboardInfo.hb_sn.slice(-4)}`}
                </div>
                <div className="text-300 text-text-primary/50">
                  {hashboardInfo.port !== undefined
                    ? `Connected to port ${hashboardInfo.port}`
                    : null}
                </div>
              </Row>
              {hashboardInfo.hb_sn && (
                <AsicTable
                  duration={duration}
                  hashboardSerialNumber={hashboardInfo.hb_sn}
                  showPopover={showPopover}
                  setShowPopover={setShowPopover}
                />
              )}
            </Tabs.Tab>
          ))}
        </Tabs>
      )}
    </div>
  );
};

export default Hardware;
