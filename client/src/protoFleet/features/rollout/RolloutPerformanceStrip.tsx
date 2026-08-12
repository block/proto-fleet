import type { ReactElement } from "react";
import clsx from "clsx";

import {
  formatRolloutMetric,
  rolloutActionNoun,
  rolloutErrorCount,
  type RolloutMetricDelta,
  rolloutMetricDelta,
  type RolloutMetricDeltaIntent,
} from "./rolloutDisplayUtils";
import type { RolloutEvent } from "./rolloutTypes";
import { useTemperatureUnit } from "@/protoFleet/store";

const deltaTextColor: Record<RolloutMetricDeltaIntent, string> = {
  positive: "text-intent-success-fill",
  negative: "text-intent-critical-fill",
  neutral: "text-text-primary-50",
};

function DeltaValue({ delta }: { delta: RolloutMetricDelta }): ReactElement {
  return <span className={deltaTextColor[delta.intent]}>{delta.deltaText}</span>;
}

/** Baseline-vs-current telemetry shared by every rollout process. */
export default function RolloutPerformanceStrip({
  event,
  embedded = false,
}: {
  event: RolloutEvent;
  embedded?: boolean;
}): ReactElement | null {
  const temperatureUnit = useTemperatureUnit();
  const performance = event.performance;
  const actionNoun = rolloutActionNoun(event.processType);
  const hasErrorSummary = event.errors !== undefined;
  const errorCount = rolloutErrorCount(event.errors);
  const hasMetrics = (performance?.metrics.length ?? 0) > 0;
  if (!performance || (performance.metrics.length === 0 && !hasErrorSummary)) {
    return null;
  }

  return (
    <div className="mt-5" data-testid="active-rollout-performance">
      <div
        className={clsx(
          "grid gap-y-5 text-text-primary",
          embedded ? "gap-x-8 tablet:grid-cols-2 laptop:grid-cols-4" : "gap-x-12 tablet:grid-cols-4",
        )}
      >
        {performance.metrics.map((metric) => {
          const value = formatRolloutMetric(metric, temperatureUnit);
          return (
            <div key={metric.label} className="min-w-0">
              <div className="text-200 text-text-primary-50">{metric.label}</div>
              <div className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-emphasis-300 text-text-primary">
                <span className={clsx("min-w-0", embedded ? "whitespace-nowrap" : "break-words")} title={value}>
                  {value}
                </span>
                <DeltaValue
                  delta={rolloutMetricDelta(
                    metric,
                    temperatureUnit,
                    event.processType,
                    event.curtailmentTelemetryPhase,
                  )}
                />
              </div>
            </div>
          );
        })}
        {hasErrorSummary ? (
          <div className="min-w-0">
            <div className="text-200 text-text-primary-50">Errors</div>
            <div className="mt-1 text-emphasis-300 text-text-primary">{errorCount.toLocaleString()}</div>
          </div>
        ) : null}
      </div>
      {hasMetrics ? (
        <div className="mt-3 text-200 text-text-primary-50">
          Compares the baseline before the {actionNoun} with telemetry after miners stabilize.
        </div>
      ) : null}
    </div>
  );
}
