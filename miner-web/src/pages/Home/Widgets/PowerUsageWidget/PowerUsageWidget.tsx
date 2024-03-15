import { useMemo } from "react";

import InfoWidget, { Bar } from "components/InfoWidget";

import { getDisplayValue, getIntensity } from "../utility";

interface PowerUsageWidgetProps {
  loading?: boolean;
  powerUsage?: string | null;
}

const PowerUsageWidget = ({ loading, powerUsage }: PowerUsageWidgetProps) => {
  // TODO: calculate intensity based on the actual data when API returns max value
  const max = 10;

  const intensity = useMemo(() => getIntensity(powerUsage, max), [powerUsage]);

  return (
    <InfoWidget
      title="Power Usage"
      value={powerUsage && `${getDisplayValue(powerUsage)} kW`}
      loading={loading}
      hasBorder
      stats={<Bar intensity={loading ? 0 : intensity} />}
    />
  );
};

export default PowerUsageWidget;
