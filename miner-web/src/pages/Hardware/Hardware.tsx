import { useEffect, useState } from "react";

import { useCoolingStatus, useHashboards, useMiningStatus } from "api";

import DurationSelector from "components/DurationSelector";
import AsicTempWidget from "components/InfoWidget/AsicTempWidget";
import FanSpeedWidget from "components/InfoWidget/FanSpeedWidget";
import Row from "components/Row";
import Tabs from "components/Tab";

import AsicTable from "./AsicTable";

const Hardware = () => {
  const [asicTemp, setAsicTemp] = useState<string>();
  // TODO: figure out how frequently we should be re-fetching this data
  const { data: miningStatus, pending: pendingMiningStatus } =
    useMiningStatus();
  const { data: hashboardsInfo, pending: pendingHashboardsInfo } =
    useHashboards();
  const { data: coolingStatus, pending: pendingCoolingStatus } =
    useCoolingStatus();

  useEffect(() => {
    if (miningStatus) {
      setAsicTemp(miningStatus.temp_c?.toFixed(2));
    }
  }, [miningStatus]);

  return (
    <div className="flex flex-col space-y-6">
      <div className="flex items-center">
        <div className="text-heading-300 grow">Hardware</div>
        <DurationSelector className="h-fit" />
      </div>

      <div className="flex space-x-6 w-full phone:flex-col phone:space-x-0 phone:space-y-6">
        <AsicTempWidget asicTemp={asicTemp} loading={pendingMiningStatus} />
        <FanSpeedWidget
          fanSpeeds={
            coolingStatus?.fans
              ? [
                  ...coolingStatus.fans,
                  // Remove these when we have real fan data
                  { rpm: 3049 },
                  { rpm: 6800 },
                  { rpm: 6730 },
                ]
              : undefined
          }
          loading={pendingCoolingStatus}
        />
      </div>
      {!pendingHashboardsInfo && hashboardsInfo?.length && (
        <Tabs>
          {[
            hashboardsInfo[0],
            // TODO: remove these when real hashboards data is available
            { hb_sn: "YWWLMMMMRRFS3024" },
            { hb_sn: "YWWLMMMMRRFS3025" },
          ].map((hashboardInfo, index) => (
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
                  {/* TODO: get port number from API when available */}
                  Connected to port {index + 1}
                </div>
              </Row>
              {hashboardInfo.hb_sn && <AsicTable hashboardSerialNumber={hashboardInfo.hb_sn} />}
            </Tabs.Tab>
          ))}
        </Tabs>
      )}
    </div>
  );
};

export default Hardware;
