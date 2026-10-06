import { useTankHistory } from "../hooks/useTankHistory";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import { useDuration, useSetDuration } from "@/protoFleet/store";
import Button, { variants } from "@/shared/components/Button";
import DurationSelector, { fleetDurations } from "@/shared/components/DurationSelector";

export default function TankPerformance({
  containerIdentifier,
  tankId,
  enabled,
}: {
  containerIdentifier: string;
  tankId: bigint;
  enabled: boolean;
}) {
  const duration = useDuration();
  const setDuration = useSetDuration();
  const { metrics, error, retry } = useTankHistory(containerIdentifier, tankId, duration, enabled);
  return (
    <section aria-label="Performance" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-heading-200">Performance</h2>
        <DurationSelector className="flex-wrap" duration={duration} durations={fleetDurations} onSelect={setDuration} />
      </div>
      {!enabled ? <p role="status">History unavailable until tank access can be refreshed.</p> : null}
      {error ? (
        <div>
          <p role="alert">Could not load tank history: {error}</p>
          <Button variant={variants.secondary} text="Retry history" onClick={retry} />
        </div>
      ) : null}
      <DeviceSetPerformanceSection duration={duration} metrics={metrics} connectNulls={false} inputUnits="display" />
    </section>
  );
}
