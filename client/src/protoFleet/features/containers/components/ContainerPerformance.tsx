import { useMemo } from "react";
import { AggregationType, MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import { useDuration, useSetDuration } from "@/protoFleet/store";
import DurationSelector, { fleetDurations } from "@/shared/components/DurationSelector";

const measurements = [
  MeasurementType.HASHRATE,
  MeasurementType.POWER,
  MeasurementType.TEMPERATURE,
  MeasurementType.EFFICIENCY,
];
const aggregations = [AggregationType.AVERAGE, AggregationType.MIN, AggregationType.MAX];

export default function ContainerPerformance({ identifier, siteId }: { identifier: string; siteId?: bigint }) {
  const duration = useDuration();
  const setDuration = useSetDuration();
  const options = useMemo(
    () => ({
      deviceIds: [identifier],
      siteIds: siteId ? [siteId] : [],
      includeUnassigned: !siteId,
      duration,
      enabled: true,
      measurementTypes: measurements,
      aggregations,
      pollIntervalMs: POLL_INTERVAL_MS,
    }),
    [identifier, siteId, duration],
  );
  const { data, hasLoaded, error } = useTelemetryMetrics(options);
  return (
    <section aria-label="Performance" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-heading-200">Performance</h2>
        <DurationSelector duration={duration} durations={fleetDurations} onSelect={setDuration} />
      </div>
      <p className="text-200 text-text-primary-70">
        Saved poll measurements: board hashrate, facility power, and module-average ASIC temperature. Earlier readings
        with different definitions are excluded.
      </p>
      {error ? <p role="alert">Could not refresh container history: {error.message}</p> : null}
      <DeviceSetPerformanceSection
        cardSurface="overlay"
        duration={duration}
        metrics={data?.metrics ?? (hasLoaded || error ? [] : undefined)}
      />
    </section>
  );
}
