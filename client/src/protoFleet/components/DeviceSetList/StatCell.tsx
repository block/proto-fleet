import { type ReactNode } from "react";

import { InfoTooltip } from "@/shared/components/Tooltip/InfoTooltip";

const StatCell = ({
  metricReportingCount,
  deviceCount,
  children,
}: {
  metricReportingCount: number;
  deviceCount: number;
  children: ReactNode;
}) => {
  if (metricReportingCount >= deviceCount) return <>{children}</>;

  const heading = `${metricReportingCount} of ${deviceCount} miners reporting`;
  return (
    <div className="flex items-center gap-1">
      {children}
      <InfoTooltip label={heading} heading={heading} body="Some devices do not make this data available." />
    </div>
  );
};

export default StatCell;
