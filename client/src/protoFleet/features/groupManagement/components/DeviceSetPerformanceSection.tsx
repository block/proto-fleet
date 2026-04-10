import { useMemo } from "react";

import { AggregationType, MeasurementType, type Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import LineChart from "@/protoFleet/components/LineChart";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { padChartDataWithNulls } from "@/protoFleet/features/dashboard/utils/chartDataPadding";
import {
  normalizeEfficiencyToJTH,
  normalizeHashrateToTHs,
  normalizePowerToKW,
} from "@/protoFleet/features/dashboard/utils/metricNormalization";
import { usePanelMetrics, useTemperatureUnit } from "@/protoFleet/store";
import { FleetDuration } from "@/shared/components/DurationSelector";
import type { ChartData } from "@/shared/components/LineChart/types";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertCtoF, TH_TO_PH_DIVISOR, TH_TO_PH_THRESHOLD } from "@/shared/utils/utility";

interface DeviceSetPerformanceSectionProps {
  duration: FleetDuration;
}

const COLOR_MAP = {
  avg: "--color-core-primary-fill",
  max: "--color-core-success-fill",
  min: "--color-core-warning-fill",
};

const ACTIVE_KEYS = ["avg", "max", "min"];
const TOOLTIP_KEYS = ["avg"];

function transformMetrics(metrics: Metric[], normalize: (value: number, deviceCount: number) => number): ChartData[] {
  return metrics.map((metric) => {
    const findAgg = (type: AggregationType) =>
      metric.aggregatedValues.find((agg) => agg.aggregationType === type)?.value ?? 0;

    const deviceCount = metric.deviceCount;
    return {
      datetime: Number(metric.openTime?.seconds ?? 0) * 1000,
      avg: normalize(findAgg(AggregationType.AVERAGE), deviceCount),
      max: normalize(findAgg(AggregationType.MAX), deviceCount),
      min: normalize(findAgg(AggregationType.MIN), deviceCount),
    };
  });
}

function transformEfficiencyMetrics(metrics: Metric[]): ChartData[] {
  return metrics.map((metric) => {
    const findAgg = (type: AggregationType) =>
      metric.aggregatedValues.find((agg) => agg.aggregationType === type)?.value ?? 0;

    return {
      datetime: Number(metric.openTime?.seconds ?? 0) * 1000,
      avg: normalizeEfficiencyToJTH(findAgg(AggregationType.AVERAGE)),
      max: normalizeEfficiencyToJTH(findAgg(AggregationType.MAX)),
      min: normalizeEfficiencyToJTH(findAgg(AggregationType.MIN)),
    };
  });
}

function transformTemperatureMetrics(metrics: Metric[]): ChartData[] {
  return metrics.map((metric) => {
    const findAgg = (type: AggregationType) =>
      metric.aggregatedValues.find((agg) => agg.aggregationType === type)?.value ?? 0;

    return {
      datetime: Number(metric.openTime?.seconds ?? 0) * 1000,
      avg: findAgg(AggregationType.AVERAGE),
      max: findAgg(AggregationType.MAX),
      min: findAgg(AggregationType.MIN),
    };
  });
}

function computeReferenceLines(chartData: ChartData[]): { value: number; color: string; strokeDasharray: string }[] {
  const avgValues: number[] = [];
  for (const d of chartData) {
    if (typeof d.avg === "number") avgValues.push(d.avg);
  }
  if (avgValues.length === 0) return [];
  return [
    { value: Math.min(...avgValues), color: "--color-intent-critical-fill", strokeDasharray: "1 6" },
    { value: Math.max(...avgValues), color: "--color-core-primary-50", strokeDasharray: "1 6" },
  ];
}

function ChartPanel({
  label,
  measurementType,
  units,
  duration,
  transform,
  formatStat,
}: {
  label: string;
  measurementType: MeasurementType;
  units: string;
  duration: FleetDuration;
  transform: (metrics: Metric[]) => { chartData: ChartData[]; units: string };
  formatStat: (data: ChartData[], units: string) => { value: string; units: string };
}) {
  const metrics = usePanelMetrics(measurementType);

  const { chartData, displayUnits } = useMemo(() => {
    if (metrics === undefined) return { chartData: undefined, displayUnits: units };
    if (metrics.length === 0) return { chartData: null, displayUnits: units };

    const result = transform(metrics);
    return {
      chartData: padChartDataWithNulls(result.chartData, duration),
      displayUnits: result.units,
    };
  }, [metrics, duration, transform, units]);

  const referenceLines = useMemo(() => {
    if (!chartData?.length) return undefined;
    return computeReferenceLines(chartData);
  }, [chartData]);

  const legendStats = useMemo(() => {
    if (!chartData?.length) return null;
    const avgValues: number[] = [];
    for (const d of chartData) {
      if (typeof d.avg === "number") avgValues.push(d.avg);
    }
    if (avgValues.length === 0) return null;
    const current = avgValues[avgValues.length - 1];
    const max = Math.max(...avgValues);
    const min = Math.min(...avgValues);
    const fmt = (v: number) => `${Number(v.toFixed(1))} ${displayUnits}`;
    return { current: fmt(current), max: fmt(max), min: fmt(min) };
  }, [chartData, displayUnits]);

  if (metrics === undefined) {
    return (
      <ChartWidget stats={{ label, value: undefined, units: "" }}>
        <SkeletonBar className="h-60 w-full" />
      </ChartWidget>
    );
  }

  if (!chartData || chartData.length === 0) {
    return <ChartWidget stats={{ label, value: "No data", units: "" }}>{null}</ChartWidget>;
  }

  const statDisplay = formatStat(chartData, displayUnits);

  return (
    <ChartWidget stats={{ label, value: statDisplay.value, units: statDisplay.units }}>
      <div className="flex w-full flex-col">
        <LineChart
          chartData={chartData}
          aggregateKey="avg"
          units={displayUnits}
          activeKeys={ACTIVE_KEYS}
          tooltipKeys={TOOLTIP_KEYS}
          colorMap={COLOR_MAP}
          heightClass="h-60"
          tickCount={5}
          duration={duration}
          referenceLines={referenceLines}
        />
        {legendStats && (
          <div className="flex items-center gap-6 px-2 pt-2 text-200 text-core-primary-50">
            <div className="flex items-center gap-2">
              <svg width="24" height="4">
                <line
                  x1="0"
                  y1="2"
                  x2="24"
                  y2="2"
                  stroke="var(--color-core-primary-fill)"
                  strokeWidth="3"
                  strokeLinecap="round"
                />
              </svg>
              <span>{legendStats.current}</span>
            </div>
            <div className="flex items-center gap-2">
              <svg width="24" height="4">
                <line
                  x1="0"
                  y1="2"
                  x2="24"
                  y2="2"
                  stroke="var(--color-core-primary-50)"
                  strokeWidth="3"
                  strokeLinecap="round"
                  strokeDasharray="1 6"
                  strokeOpacity="0.5"
                />
              </svg>
              <span>{legendStats.max}</span>
            </div>
            <div className="flex items-center gap-2">
              <svg width="24" height="4">
                <line
                  x1="0"
                  y1="2"
                  x2="24"
                  y2="2"
                  stroke="var(--color-intent-critical-fill)"
                  strokeWidth="3"
                  strokeLinecap="round"
                  strokeDasharray="1 6"
                  strokeOpacity="0.5"
                />
              </svg>
              <span>{legendStats.min}</span>
            </div>
          </div>
        )}
      </div>
    </ChartWidget>
  );
}

export function DeviceSetPerformanceSection({ duration }: DeviceSetPerformanceSectionProps) {
  const temperatureUnit = useTemperatureUnit();
  const isFahrenheit = temperatureUnit === "F";

  const hashrateTransform = useMemo(
    () => (metrics: Metric[]) => {
      const chartData = transformMetrics(metrics, normalizeHashrateToTHs);
      const maxValue = Math.max(...chartData.map((d) => d.avg ?? 0));
      if (maxValue > TH_TO_PH_THRESHOLD) {
        return {
          chartData: chartData.map((d) => ({
            ...d,
            avg: d.avg !== null ? d.avg / TH_TO_PH_DIVISOR : null,
            max: d.max !== null ? d.max / TH_TO_PH_DIVISOR : null,
            min: d.min !== null ? d.min / TH_TO_PH_DIVISOR : null,
          })),
          units: "PH/S",
        };
      }
      return { chartData, units: "TH/S" };
    },
    [],
  );

  const temperatureTransform = useMemo(
    () => (metrics: Metric[]) => {
      const chartData = transformTemperatureMetrics(metrics);
      if (isFahrenheit) {
        return {
          chartData: chartData.map((d) => ({
            ...d,
            avg: d.avg !== null ? convertCtoF(d.avg) : null,
            max: d.max !== null ? convertCtoF(d.max) : null,
            min: d.min !== null ? convertCtoF(d.min) : null,
          })),
          units: "°F",
        };
      }
      return { chartData, units: "°C" };
    },
    [isFahrenheit],
  );

  const efficiencyTransform = useMemo(
    () => (metrics: Metric[]) => ({ chartData: transformEfficiencyMetrics(metrics), units: "J/TH" }),
    [],
  );

  const powerTransform = useMemo(
    () => (metrics: Metric[]) => ({ chartData: transformMetrics(metrics, normalizePowerToKW), units: "kW" }),
    [],
  );

  const defaultFormatStat = useMemo(
    () => (data: ChartData[], units: string) => {
      const last = data[data.length - 1];
      const value = last?.avg;
      return { value: value !== null && value !== undefined ? Number(value).toFixed(1) : "N/A", units };
    },
    [],
  );

  const temperatureFormatStat = useMemo(
    () => (data: ChartData[], units: string) => {
      const last = data[data.length - 1];
      const min = last?.min;
      const max = last?.max;
      if (min === null || min === undefined || max === null || max === undefined) {
        return { value: "N/A", units: "" };
      }
      const minFormatted = `${getDisplayValue(Number(min))} ${units}`;
      const maxFormatted = `${getDisplayValue(Number(max))} ${units}`;
      return { value: `${minFormatted} – ${maxFormatted}`, units: "" };
    },
    [],
  );

  return (
    <div className="grid grid-cols-2 gap-1 phone:grid-cols-1">
      <ChartPanel
        label="Hashrate"
        measurementType={MeasurementType.HASHRATE}
        units="TH/S"
        duration={duration}
        transform={hashrateTransform}
        formatStat={defaultFormatStat}
      />
      <ChartPanel
        label="Temperature"
        measurementType={MeasurementType.TEMPERATURE}
        units={isFahrenheit ? "°F" : "°C"}
        duration={duration}
        transform={temperatureTransform}
        formatStat={temperatureFormatStat}
      />
      <ChartPanel
        label="Avg efficiency"
        measurementType={MeasurementType.EFFICIENCY}
        units="J/TH"
        duration={duration}
        transform={efficiencyTransform}
        formatStat={defaultFormatStat}
      />
      <ChartPanel
        label="Total power"
        measurementType={MeasurementType.POWER}
        units="kW"
        duration={duration}
        transform={powerTransform}
        formatStat={defaultFormatStat}
      />
    </div>
  );
}
