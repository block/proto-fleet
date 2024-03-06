import { useEffect, useState } from "react";

import { useCoolingStatus, useMiningStatus } from "api";

import { addCommas } from "common/utils/stringUtils";

import {
  AvgEfficiency,
  AvgHashrate,
  Efficiency,
  Hashrate,
} from "components/Chart/chart.stories";
import DurationSelector from "components/DurationSelector";
import InfoWidget, { InfoWidgetWrapper } from "components/InfoWidget";
import Tab, { TabWrapper } from "components/Tab";

const Home = () => {
  const [powerUsage, setPowerUsage] = useState<string>();
  const [asicTemp, setAsicTemp] = useState<string>();
  const [avgFanSpeed, setAvgFanSpeed] = useState<string | number>();
  // TODO: figure out how frequently we should be re-fetching this data
  const { data: miningStatus, pending: pendingMiningStatus } =
    useMiningStatus();
  const { data: coolingStatus, pending: pendingCoolingStatus } =
    useCoolingStatus();

  useEffect(() => {
    if (miningStatus) {
      if (miningStatus.power_usage_watts) {
        const powerUsageKw = miningStatus.power_usage_watts / 1000;
        const powerUsageRounded = powerUsageKw.toFixed(2);
        setPowerUsage(powerUsageRounded);
        setAsicTemp(miningStatus.temp_c?.toFixed(2));
      }
    }
  }, [miningStatus]);

  useEffect(() => {
    if (coolingStatus) {
      if (coolingStatus.fans) {
        const sum =
          coolingStatus.fans
            .map((fan) => fan.rpm)
            .reduce((a = 0, b = 0) => a + b, 0) || 0;
        const avg = sum / coolingStatus.fans.length;
        setAvgFanSpeed(addCommas(avg));
      }
    }
  }, [coolingStatus]);

  return (
    <>
      <div className="text-heading-300 mb-8">Home</div>

      <InfoWidgetWrapper className="mb-8">
        <InfoWidget
          title="Current Power Usage"
          value={powerUsage && `${powerUsage} kW`}
          loading={pendingMiningStatus}
        />
        {/* TODO: pass text-text-critical if fan speed outside of range once we know the range */}
        <InfoWidget
          title="Average Fan Speed"
          value={avgFanSpeed && `${avgFanSpeed} RPM`}
          loading={pendingCoolingStatus}
        />
        <InfoWidget
          title="Average ASIC Temperature"
          // \u00B0c is the degree symbol
          value={asicTemp && `${asicTemp}\u00B0c`}
          loading={pendingMiningStatus}
        />
      </InfoWidgetWrapper>

      <TabWrapper>
        <Tab label={"Efficiency"}>
          {/* TODO: BTCM-1145 - use efficiency data from API and hook up duration changes */}
          <Efficiency />
          <DurationSelector className="mt-10" />
        </Tab>
        <Tab label={"Hashrate"}>
          {/* TODO: BTCM-1147 - use hashrate data from API and hook up duration changes */}
          <Hashrate />
          <DurationSelector className="mt-10" />
        </Tab>
        <Tab label={"Compare"}>
          {/* TODO: BTCM-1145 - use efficiency data from API and hook up duration changes */}
          <AvgEfficiency height={200} />
          <div className="mb-6" />
          {/* TODO: BTCM-1147 - use hashrate data from API and hook up duration changes */}
          <AvgHashrate height={200} />
          <DurationSelector className="mt-10" />
        </Tab>
      </TabWrapper>
    </>
  );
};

export default Home;
