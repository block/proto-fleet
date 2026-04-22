import { useMemo } from "react";
import { MeasurementType, type Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useComponentErrors } from "@/protoFleet/api/useComponentErrors";
import useFleetCounts from "@/protoFleet/api/useFleetCounts";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { EfficiencyPanel } from "@/protoFleet/features/dashboard/components/EfficiencyPanel";
import FleetHealth from "@/protoFleet/features/dashboard/components/FleetHealth";
import { HashratePanel } from "@/protoFleet/features/dashboard/components/HashratePanel";
import { PowerPanel } from "@/protoFleet/features/dashboard/components/PowerPanel";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import { TemperaturePanel } from "@/protoFleet/features/dashboard/components/TemperaturePanel";
import { UptimePanel } from "@/protoFleet/features/dashboard/components/UptimePanel";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
import { MinersPage } from "@/protoFleet/features/onboarding";
import { CompleteSetup } from "@/protoFleet/features/onboarding/components/CompleteSetup";
import { useDuration, useSetDuration } from "@/protoFleet/store";
import DurationSelector, { fleetDurations } from "@/shared/components/DurationSelector";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useStickyState } from "@/shared/hooks/useStickyState";
import { buildVersionInfo } from "@/shared/utils/version";

// Constants for telemetry options - stable references to prevent unnecessary re-renders
const ALL_DEVICES: string[] = [];
const ALL_MEASUREMENT_TYPES: MeasurementType[] = [
  MeasurementType.HASHRATE,
  MeasurementType.POWER,
  MeasurementType.TEMPERATURE,
  MeasurementType.EFFICIENCY,
  MeasurementType.UPTIME,
];

const Dashboard = () => {
  const { devicePaired, statusLoaded } = useOnboardedStatus();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const currentYear = new Date().getFullYear();
  const { refs } = useStickyState();

  // Fleet counts — polled for fresh minerStateCounts
  const { totalMiners, stateCounts, hasLoaded: countsLoaded } = useFleetCounts({ pollIntervalMs: POLL_INTERVAL_MS });

  // Component errors — polled, local state (no store)
  const { controlBoardErrors, fanErrors, hashboardErrors, psuErrors } = useComponentErrors({
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  // Combined telemetry — polled, replaces data each cycle (no streaming merge)
  const telemetryOptions = useMemo(
    () => ({
      deviceIds: ALL_DEVICES,
      measurementTypes: ALL_MEASUREMENT_TYPES,
      duration,
      enabled: true,
      pollIntervalMs: POLL_INTERVAL_MS,
    }),
    [duration],
  );

  const { data: telemetryData } = useTelemetryMetrics(telemetryOptions);

  // Extract metrics for panels — filter by measurement type
  const allMetrics = telemetryData?.metrics;
  const hashrateMetrics = useMemo(
    () => allMetrics?.filter((m: Metric) => m.measurementType === MeasurementType.HASHRATE),
    [allMetrics],
  );
  const powerMetrics = useMemo(
    () => allMetrics?.filter((m: Metric) => m.measurementType === MeasurementType.POWER),
    [allMetrics],
  );
  const efficiencyMetrics = useMemo(
    () => allMetrics?.filter((m: Metric) => m.measurementType === MeasurementType.EFFICIENCY),
    [allMetrics],
  );
  const temperatureStatusCounts = telemetryData?.temperatureStatusCounts;
  const uptimeStatusCounts = telemetryData?.uptimeStatusCounts;

  if (!statusLoaded) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <div className="h-full">
      {devicePaired ? (
        <div className="flex flex-col">
          <CompleteSetup className="p-10 phone:p-6 tablet:p-6" />

          {/* Overview Section */}
          <section className="p-10 phone:p-6 tablet:p-6">
            <SectionHeading heading="Overview" />
            <div className="mt-6 flex flex-col gap-1">
              <FleetHealth
                fleetSize={countsLoaded ? totalMiners : undefined}
                healthyMiners={countsLoaded ? (stateCounts?.hashingCount ?? null) : undefined}
                needsAttentionMiners={countsLoaded ? (stateCounts?.brokenCount ?? null) : undefined}
                offlineMiners={countsLoaded ? (stateCounts?.offlineCount ?? null) : undefined}
                sleepingMiners={countsLoaded ? (stateCounts?.sleepingCount ?? null) : undefined}
              />
              <FleetErrors
                controlBoardErrors={controlBoardErrors}
                fanErrors={fanErrors}
                hashboardErrors={hashboardErrors}
                psuErrors={psuErrors}
              />
            </div>
          </section>

          {/* Performance Section */}
          <section className="pb-6">
            <div ref={refs.vertical.start} />
            <div className="sticky top-0 z-2 bg-surface-5 px-10 pt-10 pb-6 dark:bg-surface-base phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6">
              <SectionHeading heading="Performance">
                <DurationSelector duration={duration} durations={fleetDurations} onSelect={setDuration} />
              </SectionHeading>
            </div>

            <div className="flex flex-col gap-1 px-10 phone:px-6 tablet:px-6">
              <HashratePanel duration={duration} metrics={hashrateMetrics} />
              <UptimePanel duration={duration} uptimeStatusCounts={uptimeStatusCounts} />
              <TemperaturePanel duration={duration} temperatureStatusCounts={temperatureStatusCounts} />

              <div className="grid grid-cols-2 gap-1 phone:grid-cols-1 tablet:grid-cols-1">
                <PowerPanel duration={duration} metrics={powerMetrics} totalMiners={totalMiners} />
                <EfficiencyPanel duration={duration} metrics={efficiencyMetrics} totalMiners={totalMiners} />
              </div>
            </div>

            <p className="px-10 pt-6 text-300 text-text-primary phone:px-6 tablet:px-6">
              Some devices do not make all data available to Proto Fleet.
            </p>
            {}
            <div ref={refs.vertical.end} />
          </section>

          {/* Privacy Policy */}
          <footer className="px-10 pt-20 pb-6 text-300 phone:px-5 tablet:px-5">
            <p className="text-text-primary">
              Powerful mining tools. Built for decentralization.{" "}
              <span className="text-text-primary-50">
                Proto Fleet {buildVersionInfo.version} © {currentYear} Block, Inc.{" "}
                <a
                  href="https://proto.xyz/privacy-policy"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="hover:underline"
                >
                  Privacy Notice
                </a>
              </span>
            </p>
          </footer>
        </div>
      ) : (
        <MinersPage />
      )}
    </div>
  );
};

export default Dashboard;
