import { useEffect, useState } from "react";

import { Api } from "Api";

import { addCommas } from "common/utils/stringUtils";

import { AvgEfficiency, AvgHashrate, Efficiency, Hashrate } from "components/Chart/chart.stories";
import DurationSelector from "components/DurationSelector";
import InfoWidget, { InfoWidgetWrapper } from "components/InfoWidget";
import Tab, { TabWrapper } from "components/Tab";

const { api } = new Api();

const Performance = () => {
  const [powerUsage, setPowerUsage] = useState<string>();
  const [asicTemp, setAsicTemp] = useState<string>();
  const [avgFanSpeed, setAvgFanSpeed] = useState<string>();

  useEffect(() => {
    api.getMiningStatus().then((res) => {
      if (res.data["mining-status"]) {
        const miningStatus = res.data["mining-status"];
        if (miningStatus.power_usage_watts) {
          const powerUsageKw = miningStatus.power_usage_watts / 1000;
          const powerUsageRounded = powerUsageKw.toFixed(2);
          setPowerUsage(powerUsageRounded);
          setAsicTemp(miningStatus.temp_c?.toFixed(2));
        }
      }
    });
    api.getCooling().then((res) => {
      if (res.data["cooling-status"]) {
        const cooling = res.data["cooling-status"];
        if (cooling.fans) {
          const sum =
            cooling.fans
              .map((fan) => fan.rpm)
              .reduce((a = 0, b = 0) => a + b, 0) || 0;
          const avg = sum / cooling.fans.length;
          setAvgFanSpeed(addCommas(avg));
        }
      }
    });
  }, []);

  return (
    <>
      <div className="text-title-1 mb-8">Performance</div>

      <InfoWidgetWrapper className="mb-8">
        <InfoWidget
          title="Current Power Usage"
          value={powerUsage && `${powerUsage} kW`}
        />
        {/* TODO: pass text-error-100 if fan speed outside of range once we know the range */}
        <InfoWidget
          title="Average Fan Speed"
          value={avgFanSpeed && `${avgFanSpeed} RPM`}
        />
        <InfoWidget
          title="Average ASIC Temperature"
          // \u00B0c is the degree symbol
          value={asicTemp && `${asicTemp}\u00B0c`}
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

export default Performance;
