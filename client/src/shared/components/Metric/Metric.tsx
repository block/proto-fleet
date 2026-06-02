import { type ReactNode } from "react";
import clsx from "clsx";

import SkeletonBar from "@/shared/components/SkeletonBar";

interface MetricProps {
  label: string;
  // `undefined` shows a skeleton (loading), `null` renders the em dash, a
  // string renders verbatim. ReactNode is allowed so callers can compose a
  // value out of small spans for unit styling.
  value: ReactNode | undefined | null;
  testId?: string;
  className?: string;
}

const Metric = ({ label, value, testId, className }: MetricProps) => (
  <div className={clsx("flex flex-col gap-1", className)} data-testid={testId}>
    <div className="text-300 text-text-primary-50">{label}</div>
    <div className="text-heading-300 text-text-primary">
      {value === undefined ? <SkeletonBar className="h-7 w-24" /> : value === null ? <span>—</span> : value}
    </div>
  </div>
);

export default Metric;
