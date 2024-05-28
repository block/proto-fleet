import { useEffect, useState } from "react";

import { useCoolingStatus, useHashboards, useMiningStatus } from "api";

import DurationSelector from "components/DurationSelector";
import AsicTempWidget from "components/InfoWidget/AsicTempWidget";
import FanSpeedWidget from "components/InfoWidget/FanSpeedWidget";
import Row from "components/Row";
import Tabs from "components/Tab";

import AsicTable from "./Asic/AsicTable";

const Hardware = () => {
  const [asicTemp, setAsicTemp] = useState<string>();
  const { data: miningStatus, pending: pendingMiningStatus } =
    useMiningStatus({ poll: true });
  const { data: hashboardsInfo, pending: pendingHashboardsInfo } =
    useHashboards({ poll: true });
  const { data: coolingStatus, pending: pendingCoolingStatus } =
    useCoolingStatus({ poll: true });

  useEffect(() => {
    if (miningStatus) {
      setAsicTemp(miningStatus.average_temp_c?.toFixed(2));
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
          fanSpeeds={coolingStatus?.fans}
          loading={pendingCoolingStatus}
        />
      </div>
      {!pendingHashboardsInfo && hashboardsInfo?.length && (
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
                  {/* TODO: get port number from API when available */}
                  {hashboardInfo.port !== undefined
                    ? `Connected to port ${hashboardInfo.port}`
                    : null}
                </div>
              </Row>
              {hashboardInfo.hb_sn && (
                <AsicTable hashboardSerialNumber={hashboardInfo.hb_sn} />
              )}
            </Tabs.Tab>
          ))}
        </Tabs>
      )}
    </div>
  );
};

export default Hardware;
