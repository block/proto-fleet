import { useEffect, useState } from "react";

import { Api } from "Api";

import { addCommas } from "common/utils/stringUtils";

import InfoWidget, { InfoWidgetWrapper } from "components/InfoWidget";

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
      <InfoWidgetWrapper>
        <InfoWidget
          title="Current Power Usage"
          value={powerUsage && `${powerUsage} kW`}
        />
        <InfoWidget
          title="Average Fan Speed"
          value={avgFanSpeed && `${avgFanSpeed} RPM`}
        />
        <InfoWidget
          title="Average ASIC Temperature"
          value={asicTemp && `${asicTemp}&deg;c`}
        />
      </InfoWidgetWrapper>
    </>
  );
};

export default Performance;
