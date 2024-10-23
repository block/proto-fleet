import { useEffect, useState } from "react";

import {
  CoolingStatusCoolingstatus,
  FanInfo,
  HashboardsInfoHashboardsinfo,
  MiningStatusMiningstatus,
  TemperatureResponseTemperaturedata,
} from "apiTypes";

import { useLocalStorage } from "common/hooks/useLocalStorage";

import DurationSelector, {
  Duration,
} from "components/DurationSelector";
import FanSpeedWidget from "components/InfoWidget/FanSpeedWidget";
import TempWidget from "components/InfoWidget/TempWidget";
import Row from "components/Row";
import Spinner from "components/Spinner";
import Tabs from "components/Tab";

import AsicTable from "./Asic/AsicTableWrapper";
import { Granularity } from "./types";
import { sortHashboards } from "./utility";

interface TemperatureProps {
  coolingStatus?: CoolingStatusCoolingstatus;
  duration: Duration;
  hashboardsInfo?: HashboardsInfoHashboardsinfo[];
  miningStatus?: MiningStatusMiningstatus;
  pendingCoolingStatus: boolean;
  pendingHashboardsInfo: boolean;
  pendingTempData: boolean;
  setDuration: (duration: Duration) => void;
  tempData?: TemperatureResponseTemperaturedata;
}

const Temperature = ({
  coolingStatus,
  duration,
  hashboardsInfo,
  miningStatus,
  pendingCoolingStatus,
  pendingHashboardsInfo,
  pendingTempData,
  setDuration,
  tempData,
}: TemperatureProps) => {
  const { setItem } = useLocalStorage();
  const [showPopover, setShowPopover] = useState<string | undefined>(undefined);
  const [fanSpeeds, setFanSpeeds] = useState<FanInfo[]>();
  const [temp, setTemp] = useState<number>();
  const [highestTemp, setHighestTemp] = useState<number>();
  const [hashboardSerials, setHashboardSerials] = useState<string[]>();
  const [granularity, setGranularity] = useState<Granularity>("1m");

  useEffect(() => {
    setTemp(undefined);
    setItem("duration", duration);
  }, [duration, setItem]);

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
    if (
      !pendingTempData &&
      tempData?.data?.length &&
      tempData.duration === duration
    ) {
      setHighestTemp(tempData.aggregates?.max);
      setTemp(tempData.data?.[tempData.data.length - 1].value);
    }
  }, [duration, pendingTempData, tempData]);

  useEffect(() => {
    if (!pendingCoolingStatus && coolingStatus?.fans?.length) {
      setFanSpeeds(coolingStatus.fans);
    }
  }, [coolingStatus, pendingCoolingStatus]);

  useEffect(() => {
    const rebootUptimeInSeconds = miningStatus?.reboot_uptime_s;
    const miningUptimeInSeconds = miningStatus?.mining_uptime_s;
    if (rebootUptimeInSeconds === undefined && miningUptimeInSeconds === undefined) return;

    let uptimeInSeconds = 0;
    if (rebootUptimeInSeconds !== undefined && miningUptimeInSeconds !== undefined) {
      uptimeInSeconds = Math.min(rebootUptimeInSeconds, miningUptimeInSeconds);
    } else if (rebootUptimeInSeconds !== undefined) {
      uptimeInSeconds = rebootUptimeInSeconds;
    } else if (miningUptimeInSeconds !== undefined) {
      uptimeInSeconds = miningUptimeInSeconds;
    }

    const oneHourInSeconds = 60 * 60;
    const sixHoursInSeconds = oneHourInSeconds * 6;
    if (uptimeInSeconds > sixHoursInSeconds) {
      setGranularity("15m");
    } else if (uptimeInSeconds > oneHourInSeconds) {
      setGranularity("5m");
    } else {
      setGranularity("1m");
    }
  }, [miningStatus]);

  return (
    <div className="flex flex-col space-y-6 h-full">
      <div className="flex items-center">
        <div className="text-heading-300 grow">Temperature</div>
        <DurationSelector
          className="h-fit"
          duration={duration}
          onSelect={setDuration}
        />
      </div>

      <div className="flex space-x-6 w-full phone:flex-col phone:space-x-0 phone:space-y-6">
        <TempWidget
          duration={duration}
          temp={temp}
          hashboardSerials={hashboardSerials}
          highestTemp={highestTemp}
          loading={pendingTempData && !temp}
        />
        <FanSpeedWidget
          fanSpeeds={fanSpeeds}
          loading={pendingCoolingStatus && !fanSpeeds?.length}
        />
      </div>
      {pendingHashboardsInfo && !hashboardsInfo?.length && (
        <div className="flex justify-center items-center h-full">
          <Spinner />
        </div>
      )}
      {hashboardsInfo?.length ? (
        <Tabs>
          {sortHashboards(hashboardsInfo).map((hashboardInfo, index) => (
            <Tabs.Tab
              label={`Hashboard ${index + 1}`}
              key={hashboardInfo.hb_sn}
            >
              <Row compact className="-mt-6 flex">
                <div className="text-emphasis-300 grow">
                  {hashboardInfo.hb_sn &&
                    `Board ending in ${hashboardInfo.hb_sn.slice(-4)}`}
                </div>
                <div className="text-300 text-text-primary-50">
                  {hashboardInfo.port !== undefined
                    ? `Connected to port ${hashboardInfo.port}`
                    : null}
                </div>
              </Row>
              {hashboardInfo.hb_sn && (
                <AsicTable
                  duration={duration}
                  granularity={granularity}
                  hashboardSerialNumber={hashboardInfo.hb_sn}
                  showPopover={showPopover}
                  setShowPopover={setShowPopover}
                />
              )}
            </Tabs.Tab>
          ))}
        </Tabs>
      ) : null}
    </div>
  );
};

export default Temperature;
