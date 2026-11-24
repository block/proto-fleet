import type { ReactNode } from "react";
import clsx from "clsx";

import Row from "@/shared/components/Row";

interface SummaryRowProps {
  label: string;
  children: ReactNode;
  className?: string;
  /** Shows the value in the critical text color, e.g. for an error message. */
  critical?: boolean;
  divider?: boolean;
}

/** A label and value on one line, as used in detail modals. */
const SummaryRow = ({ label, children, className, critical = false, divider = true }: SummaryRowProps) => (
  <Row compact divider={divider}>
    <div className="flex w-full items-center justify-between gap-4">
      <span className="shrink-0 text-300 text-text-primary-70">{label}</span>
      <span
        className={clsx(
          "min-w-0 text-right text-300",
          critical ? "text-intent-critical-text" : "text-text-primary",
          className,
        )}
      >
        {children}
      </span>
    </div>
  </Row>
);

export default SummaryRow;
