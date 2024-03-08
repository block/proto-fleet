import { useMemo } from "react";

import InfoWidget, { Bar } from "components/InfoWidget";
import { getIntensity } from "./utility";

interface PowerUsageWidgetProps {
  loading?: boolean;
  powerUsage?: string;
}

const PowerUsageWidget = ({ loading, powerUsage }: PowerUsageWidgetProps) => {
  // TODO: calculate intensity based on the actual data when API returns max value
  const max = 10;

  const intensity = useMemo(() => getIntensity(powerUsage, max), [powerUsage]);

  return (
    <InfoWidget
      title="Power Usage"
      value={powerUsage && `${powerUsage} kW`}
      loading={loading}
      hasBorder
      stats={<Bar intensity={intensity} />}
    />
  );
};

export default PowerUsageWidget;
