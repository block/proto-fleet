import type { ReactNode } from "react";

import StatusCircle from "./StatusCircle";
import type { StatusCircleProps } from "./types";

interface StatusLabelProps {
  status: StatusCircleProps["status"];
  children: ReactNode;
}

/** Text preceded by a small status dot. */
const StatusLabel = ({ status, children }: StatusLabelProps) => (
  <span className="inline-flex items-center gap-1.5">
    <StatusCircle status={status} variant="simple" width="w-1.5" removeMargin />
    {children}
  </span>
);

export default StatusLabel;
