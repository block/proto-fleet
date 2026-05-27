import type { ReactElement } from "react";
import clsx from "clsx";

interface CurtailmentStatusDotProps {
  className: string;
}

function CurtailmentStatusDot({ className }: CurtailmentStatusDotProps): ReactElement {
  return <span className={clsx("inline-block h-2 w-2 shrink-0 rounded-full", className)} />;
}

export default CurtailmentStatusDot;
