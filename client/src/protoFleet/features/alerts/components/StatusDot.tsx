import type { ReactNode } from "react";
import clsx from "clsx";

// Shared status indicator (colored dot + label) for the notifications feature; used by channel validation and history badges.
const StatusDot = ({ dotClass, children }: { dotClass: string; children: ReactNode }) => (
  <span className="inline-flex items-center gap-2 text-300 text-text-primary-50">
    <span className={clsx("h-2 w-2 rounded-full", dotClass)} />
    {children}
  </span>
);

export default StatusDot;
