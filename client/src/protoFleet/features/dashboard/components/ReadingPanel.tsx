import { useMemo } from "react";
import { padChartDataWithNulls } from "../utils/chartDataPadding";
import ChartWidget from "./ChartWidget";
import { AggregationType, type Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import LineChart from "@/protoFleet/components/LineChart";
import type { FleetDuration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

export default function ReadingPanel({
  label,
  units,
  duration,
  metrics,
  tooltipNotes,
}: {
  label: string;
  units: string;
  duration: FleetDuration;
  metrics: Metric[] | undefined;
  tooltipNotes?: Record<number, string>;
}) {
  const chartData = useMemo(
    () =>
      metrics === undefined
        ? undefined
        : padChartDataWithNulls(
            metrics.map((metric) => ({
              datetime: Number(metric.openTime?.seconds ?? 0n) * 1000,
              reading:
                metric.aggregatedValues.find((value) => value.aggregationType === AggregationType.AVERAGE)?.value ??
                null,
            })),
            duration,
          ),
    [metrics, duration],
  );
  const latest = chartData?.slice(-1)[0]?.reading;
  return (
    <ChartWidget
      stats={{
        label,
        value: chartData === undefined ? undefined : latest == null ? "Unavailable" : latest.toFixed(1),
        units: latest == null ? undefined : units,
      }}
    >
      {chartData === undefined ? (
        <SkeletonBar className="h-60 w-full" />
      ) : chartData.length > 0 ? (
        <LineChart
          chartData={chartData}
          aggregateKey="reading"
          activeKeys={["reading"]}
          units={units}
          duration={duration}
          connectNulls={false}
          tooltipNotes={tooltipNotes}
          heightClass="h-60"
          tickCount={5}
        />
      ) : null}
    </ChartWidget>
  );
}
