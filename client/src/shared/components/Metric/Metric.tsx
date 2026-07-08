import { type ReactNode } from "react";
import clsx from "clsx";

import SkeletonBar from "@/shared/components/SkeletonBar";

// `default` is the large standalone metric (text-heading-300, roomy gap).
// `compact` is the tighter variant for dense cards and metric rows: a
// text-emphasis-400 value sitting close under its label.
type MetricVariant = "default" | "compact";

interface MetricProps {
  label: string;
  // `undefined` shows a skeleton (loading), `null` renders the em dash, a
  // string renders verbatim. ReactNode is allowed so callers can compose a
  // value out of small spans for unit styling.
  value: ReactNode | undefined | null;
  // Overrides the variant's value type scale when a caller needs a specific
  // size; otherwise the variant default applies.
  valueSize?: string;
  variant?: MetricVariant;
  testId?: string;
  className?: string;
}

const Metric = ({ label, value, valueSize, variant = "default", testId, className }: MetricProps) => {
  const compact = variant === "compact";
  const resolvedValueSize = valueSize ?? (compact ? "text-emphasis-400" : "text-heading-300");

  return (
    <div className={clsx("flex flex-col", compact ? "gap-0.5" : "gap-1", className)} data-testid={testId}>
      <div className="text-300 text-text-primary-50" data-testid={testId ? `${testId}-label` : undefined}>
        {label}
      </div>
      <div
        className={clsx(resolvedValueSize, "text-text-primary")}
        data-testid={testId ? `${testId}-value` : undefined}
      >
        {value === undefined ? (
          <SkeletonBar className={clsx("w-24", compact ? "h-4" : "h-7")} />
        ) : value === null ? (
          <span>—</span>
        ) : (
          value
        )}
      </div>
    </div>
  );
};

export default Metric;
