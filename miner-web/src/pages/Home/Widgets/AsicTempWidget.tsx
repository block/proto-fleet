import { useMemo } from "react";

import InfoWidget, { Bar } from "components/InfoWidget";
import { getIntensity } from "./utility";

interface AsicTempWidgetProps {
  asicTemp?: string;
  loading?: boolean;
}

const AsicTempWidget = ({ asicTemp, loading }: AsicTempWidgetProps) => {
  // TODO: calculate intensity based on the actual data when API returns max value
  const max = 3100;

  const intensity = useMemo(() => getIntensity(asicTemp, max), [asicTemp]);

  return (
    <InfoWidget
      title="Avg. ASIC Temp"
      // \u00B0c is the degree symbol
      value={asicTemp && `${asicTemp}\u00B0c`}
      loading={loading}
      hasBorder
      stats={<Bar intensity={intensity} />}
    />
  );
};

export default AsicTempWidget;
