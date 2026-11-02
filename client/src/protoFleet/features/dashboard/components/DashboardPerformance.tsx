import { useMemo } from "react";
import { EfficiencyPanel } from "./EfficiencyPanel";
import { HashratePanel } from "./HashratePanel";
import { PowerPanel } from "./PowerPanel";
import SectionHeading from "./SectionHeading";
import { TemperaturePanel } from "./TemperaturePanel";
import { UptimePanel } from "./UptimePanel";
import {
  MeasurementType,
  type Metric,
  type TemperatureStatusCount,
  type UptimeStatusCount,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { usePageBackground } from "@/protoFleet/hooks/usePageBackground";
import DurationSelector, { type FleetDuration, fleetDurations } from "@/shared/components/DurationSelector";
import { useStickyState } from "@/shared/hooks/useStickyState";

export type DashboardPerformanceProps = {
  duration: FleetDuration;
  onDurationChange: (duration: FleetDuration) => void;
  metrics: Metric[] | undefined;
  totalAssets: number;
  temperatureStatusCounts?: TemperatureStatusCount[];
  uptimeStatusCounts?: UptimeStatusCount[];
  uptimeExpectedAssetCount?: number;
  /** Account readings preserve allocation-scoped coverage and gaps. */
  accountReadings?: boolean;
  hasLegacyModuleReadings?: boolean;
  tooltipNotes?: Partial<Record<MeasurementType, Record<number, string>>>;
};

export default function DashboardPerformance({
  duration,
  onDurationChange,
  metrics,
  totalAssets,
  temperatureStatusCounts,
  uptimeStatusCounts,
  uptimeExpectedAssetCount,
  accountReadings = false,
  hasLegacyModuleReadings = false,
  tooltipNotes,
}: DashboardPerformanceProps) {
  const { refs } = useStickyState();
  const { bgClass } = usePageBackground();
  const grouped = useMemo(() => {
    const byType = (type: MeasurementType) => metrics?.filter((metric) => metric.measurementType === type);
    return {
      hashrate: byType(MeasurementType.HASHRATE),
      power: byType(MeasurementType.POWER),
      efficiency: byType(MeasurementType.EFFICIENCY),
    };
  }, [metrics]);
  const connectNulls = !accountReadings;
  // Account responses include every output bucket. Their hourly/daily resolution
  // can exceed the requested chart granularity, so client padding would add false gaps.
  return (
    <section aria-label="Performance" className="pb-6">
      <div ref={refs.vertical.start} />
      <div className={`${bgClass} sticky top-0 z-2 px-6 pt-6 pb-6 laptop:px-10 laptop:pt-10`}>
        <SectionHeading heading="Performance">
          <DurationSelector duration={duration} durations={fleetDurations} onSelect={onDurationChange} />
        </SectionHeading>
      </div>
      <div className="flex flex-col gap-4 px-6 pt-4 laptop:px-10">
        {hasLegacyModuleReadings ? (
          <p className="text-300 text-text-primary-70">
            This range includes legacy module data. New readings use miners and hashboards. Module/PSU power and asset
            counts can differ from board measurements.
          </p>
        ) : null}
        <HashratePanel
          duration={duration}
          metrics={grouped.hashrate}
          connectNulls={connectNulls}
          padMissingBuckets={!accountReadings}
          tooltipNotes={tooltipNotes?.[MeasurementType.HASHRATE]}
        />
        <UptimePanel
          duration={duration}
          uptimeStatusCounts={uptimeStatusCounts}
          audience={accountReadings ? "account" : "fleet"}
          expectedAssetCount={uptimeExpectedAssetCount}
          showActions={!accountReadings}
        />
        <TemperaturePanel
          duration={duration}
          temperatureStatusCounts={temperatureStatusCounts}
          audience={accountReadings ? "account" : "fleet"}
        />
        <div className="grid grid-cols-1 gap-4 laptop:grid-cols-2">
          <PowerPanel
            duration={duration}
            metrics={grouped.power}
            metricsInKW={accountReadings}
            totalMiners={totalAssets}
            connectNulls={connectNulls}
            padMissingBuckets={!accountReadings}
            tooltipNotes={tooltipNotes?.[MeasurementType.POWER]}
            reportingSubtitle={accountReadings ? "Measured equipment power" : undefined}
          />
          <EfficiencyPanel
            duration={duration}
            metrics={grouped.efficiency}
            totalMiners={totalAssets}
            connectNulls={connectNulls}
            padMissingBuckets={!accountReadings}
            tooltipNotes={tooltipNotes?.[MeasurementType.EFFICIENCY]}
            reportingSubtitle={accountReadings ? "Complete readings only" : undefined}
          />
        </div>
      </div>
      <p className="px-6 pt-6 text-300 text-text-primary laptop:px-10">
        {accountReadings
          ? "Charts include retained readings from established allocation periods. Gaps indicate missing or unauthorized readings. Power is measured equipment power; efficiency requires complete valid power and hashrate coverage."
          : "Some devices do not make all data available to Proto Fleet."}
      </p>
      {/* eslint-disable-next-line react-hooks/refs -- useStickyState supplies refs written by React during commit */}
      <div ref={refs.vertical.end} />
    </section>
  );
}
