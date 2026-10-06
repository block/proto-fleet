import clsx from "clsx";

import type { StatusTone } from "./rolloutStatus";

const StatusChip = ({ label, tone }: { label: string; tone: StatusTone | "info" }) => (
  <span
    className={clsx(
      "inline-flex items-center rounded-full px-2 py-0.5 text-200 whitespace-nowrap",
      tone === "success" && "bg-intent-success-10 text-text-primary",
      tone === "progress" && "bg-intent-warning-10 text-text-primary",
      tone === "critical" && "bg-intent-critical-10 text-text-critical",
      tone === "neutral" && "bg-core-primary-5 text-text-primary-70",
      tone === "info" && "bg-intent-info-10 text-intent-info-text",
    )}
  >
    {label}
  </span>
);

export default StatusChip;
