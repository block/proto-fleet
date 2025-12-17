import { useEffect, useMemo, useRef } from "react";
import { GetCombinedMetricsResponse, MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useComponentErrors } from "@/protoFleet/api/useComponentErrors";
import useFleetCounts from "@/protoFleet/api/useFleetCounts";
import { useStreamingTelemetryMetrics } from "@/protoFleet/api/useStreamingTelemetryMetrics";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
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
import {
  useAppendStreamingMetrics,
  useAppendStreamingTemperatureCounts,
  useAppendStreamingUptimeCounts,
  useClearMetrics,
  useDevicePaired,
  useDuration,
  useSetAllHistoricalData,
  useSetDashboardError,
  useSetDuration,
} from "@/protoFleet/store";
import DurationSelector from "@/shared/components/DurationSelector";
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
  const devicePaired = useDevicePaired();
  const { totalMiners, stateCounts } = useFleetCounts();
  const { controlBoardErrors, fanErrors, hashboardErrors, psuErrors } = useComponentErrors();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const currentYear = new Date().getFullYear();
  const { refs } = useStickyState();

  // Store action hooks
  const setAllHistoricalData = useSetAllHistoricalData();
  const appendStreamingMetrics = useAppendStreamingMetrics();
  const appendStreamingTemperatureCounts = useAppendStreamingTemperatureCounts();
  const appendStreamingUptimeCounts = useAppendStreamingUptimeCounts();
  const clearMetrics = useClearMetrics();
  const setError = useSetDashboardError();

  // Combined telemetry fetching for all measurement types - reduces from 5 API calls to 1
  const telemetryOptions = useMemo(
    () => ({
      deviceIds: ALL_DEVICES,
      measurementTypes: ALL_MEASUREMENT_TYPES,
      duration: duration,
      enabled: true,
    }),
    [duration],
  );

  const { data: historicalData, error } = useTelemetryMetrics(telemetryOptions);

  // Combined streaming for all measurement types - reduces from 5 streams to 1
  const streamingOptions = useMemo(
    () => ({
      deviceIds: ALL_DEVICES,
      measurementTypes: ALL_MEASUREMENT_TYPES,
      enabled: true,
    }),
    [],
  );

  const { latestData: streamingData } = useStreamingTelemetryMetrics(streamingOptions);

  // Track which data object we've loaded AND if we've loaded for current duration
  // This prevents both: loading stale data on duration change, and refetch overwrites
  const lastLoadedDataRef = useRef<GetCombinedMetricsResponse | null>(null);
  const hasLoadedForCurrentDurationRef = useRef(false);

  // Write historical data to store atomically to prevent race conditions
  // Only load historical data once per duration to preserve streaming updates
  useEffect(() => {
    if (!historicalData) return;

    // Skip if this is the same data object we already loaded (prevents loading stale data)
    if (historicalData === lastLoadedDataRef.current) {
      return;
    }

    // Skip if we've already loaded fresh data for current duration (preserves streaming)
    if (hasLoadedForCurrentDurationRef.current) {
      return;
    }

    lastLoadedDataRef.current = historicalData;
    hasLoadedForCurrentDurationRef.current = true;
    setAllHistoricalData(
      historicalData.metrics ?? [],
      historicalData.temperatureStatusCounts ?? [],
      historicalData.uptimeStatusCounts ?? [],
    );
  }, [historicalData, setAllHistoricalData]);

  // Write error state to store
  useEffect(() => {
    setError(error ?? null);
  }, [error, setError]);

  // Clear metrics immediately when duration changes to prevent stale streaming data accumulation
  // This runs before the historical data effect, ensuring clean state for new duration
  const prevDurationRef = useRef<typeof duration | undefined>(undefined);
  useEffect(() => {
    // Only clear if duration actually changed (not on initial mount)
    if (prevDurationRef.current !== undefined && prevDurationRef.current !== duration) {
      clearMetrics();
      hasLoadedForCurrentDurationRef.current = false; // Need to load for new duration
      // Note: lastLoadedDataRef stays the same to detect when NEW data arrives
    }
    prevDurationRef.current = duration;
  }, [duration, clearMetrics]);

  // Append streaming data - merge happens in store actions
  useEffect(() => {
    if (!streamingData) return;

    appendStreamingMetrics(streamingData.metrics ?? []);
    appendStreamingTemperatureCounts(streamingData.temperatureStatusCounts ?? []);
    appendStreamingUptimeCounts(streamingData.uptimeStatusCounts ?? []);
  }, [streamingData, appendStreamingMetrics, appendStreamingTemperatureCounts, appendStreamingUptimeCounts]);

  return (
    <div className="h-full">
      {devicePaired ? (
        <div className="flex flex-col">
          <CompleteSetup className="p-10 phone:p-6 tablet:p-6" />

          {/* Overview Section */}
          <section className="p-10 phone:p-6 tablet:p-6">
            <SectionHeading heading="Overview" />
            <div className="mt-6 flex flex-col gap-1">
              <FleetErrors
                controlBoardErrors={controlBoardErrors}
                fanErrors={fanErrors}
                hashboardErrors={hashboardErrors}
                psuErrors={psuErrors}
              />
              <FleetHealth
                fleetSize={totalMiners || 1} // prevent division by zero
                healthyMiners={stateCounts?.hashingCount ?? 0}
                needsAttentionMiners={stateCounts?.brokenCount ?? 0}
                offlineMiners={stateCounts?.offlineCount ?? 0}
                sleepingMiners={stateCounts?.sleepingCount ?? 0}
              />
            </div>
          </section>

          {/* Performance Section */}
          <section className="pb-6">
            <div ref={refs.vertical.start} />
            <div className="sticky top-0 z-2 bg-surface-5 px-10 pt-10 pb-6 dark:bg-surface-base phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6">
              <SectionHeading heading="Performance">
                <DurationSelector duration={duration} onSelect={setDuration} />
              </SectionHeading>
            </div>

            <div className="flex flex-col gap-1 px-10 phone:px-6 tablet:px-6">
              {/* Hashrate Panel - shows fleet hashrate over time */}
              <HashratePanel duration={duration} />

              {/* Uptime Panel - shows uptime status distribution */}
              <UptimePanel duration={duration} />

              {/* Temperature Panel - shows temperature status distribution */}
              <TemperaturePanel duration={duration} />

              {/* Power and Efficiency Panels - side by side */}
              <div className="grid grid-cols-2 gap-1 phone:grid-cols-1 tablet:grid-cols-1">
                {/* Power Panel - shows fleet power consumption over time */}
                <PowerPanel duration={duration} />

                {/* Efficiency Panel - shows fleet efficiency over time */}
                <EfficiencyPanel duration={duration} />
              </div>
            </div>

            <p className="px-10 pt-6 text-300 text-text-primary phone:px-6 tablet:px-6">
              Data gaps may occur where third-party miner telemetry is unavailable. Efficiency and power reports will
              not reflect Antminer devices.
            </p>
            {/* eslint-disable-next-line react-hooks/refs */}
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
